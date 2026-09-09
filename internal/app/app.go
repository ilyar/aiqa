package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/assessment"
	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/extsuite"
	extanthropic "github.com/ilyar/aiqa/internal/extsuite/builtin/anthropic"
	extgoogle "github.com/ilyar/aiqa/internal/extsuite/builtin/googlegenai"
	extopenai "github.com/ilyar/aiqa/internal/extsuite/builtin/openai"
	"github.com/ilyar/aiqa/internal/guidance"
	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/probe"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/progress"
	protocolpkg "github.com/ilyar/aiqa/internal/protocol"
	protocolanthropic "github.com/ilyar/aiqa/internal/protocol/anthropic"
	protocolgoogle "github.com/ilyar/aiqa/internal/protocol/googlegenai"
	protocolopenai "github.com/ilyar/aiqa/internal/protocol/openai"
	reportpkg "github.com/ilyar/aiqa/internal/report"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/scheduler"
	"github.com/ilyar/aiqa/internal/telemetry"
	"github.com/ilyar/aiqa/internal/traceverify"
)

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

type RunResult struct {
	ReportV3 *result.ReportV3
	Paths    []string
	// Stdout is an explicitly requested public report payload.  It is produced
	// from the same finalized snapshot that is committed to disk.
	Stdout   []byte
	ExitCode int
}

// attachForensicSink is a narrow seam for deterministic sidecar write-failure
// tests. Production always streams to the pre-opened 0600 file.
var attachForensicSink = func(logger *logging.Logger, file *os.File) (*logging.Sink, error) {
	return logger.AddSink(file, logging.SinkOptions{MaxBytes: -1})
}

// progressWriter and progressTTY are seams for deterministic CLI tests. They
// deliberately remain separate from the private logging sinks: operator
// progress is always visible, while --verbose only governs JSONL retention.
var progressWriter io.Writer = os.Stderr
var progressTTY = func() bool {
	// A pseudo-terminal is not sufficient when its consumer does not implement
	// carriage-return redraws (CI log collectors and `TERM=dumb` wrappers are
	// common examples). Prefer durable progress lines in those environments.
	if os.Getenv("CI") != "" || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	info, err := os.Stderr.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func Run(ctx context.Context, cfg config.Config, build BuildInfo) (RunResult, error) {
	if len(cfg.Targets) == 0 && cfg.BaseURL != "" {
		profileFile := cfg.OpenAIProfile
		if profileFile == "" {
			profileFile = profile.DefaultFile
		}
		cfg.Targets = []config.TargetConfig{{
			ID: "openai", Protocol: config.ProtocolOpenAI, BaseURL: cfg.BaseURL, APIKey: cfg.APIKey,
			Models: append([]string(nil), cfg.Models...), ProfileFile: profileFile, Modules: append([]string(nil), cfg.Modules...),
			ModelCapabilities: append([]string(nil), cfg.ModelCapabilities...), CapabilityFile: cfg.CapabilityFile, UnknownModel: cfg.UnknownModel,
			RequestInterval: cfg.RequestInterval,
		}}
	}
	return runMultiTarget(ctx, cfg, build)
}

func runMultiTarget(ctx context.Context, cfg config.Config, build BuildInfo) (out RunResult, runErr error) {
	logger := logging.New(cfg.Verbosity)
	operatorReporter := progress.New(progress.Options{Writer: progressWriter, TTY: progressTTY()})
	defer operatorReporter.Close()
	var reporter progress.FrozenSink = operatorReporter
	// The sidecar is deliberately opened before emitting the first lifecycle
	// event. O_EXCL prevents a pre-existing path (including a symlink) from
	// receiving private diagnostics; callers must explicitly rotate an old log.
	var sidecarPath, verdictPath string
	var verdictFile *os.File
	var sidecarWriteErr error
	var publicReport *result.ReportV3
	var reportsWritten bool
	var finalVerdict *traceverify.Verdict
	if cfg.Verbosity > 0 {
		sidecarPath = cfg.Name + ".log"
		file, err := os.OpenFile(sidecarPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return RunResult{ExitCode: 2}, fmt.Errorf("create private diagnostic log %q: %w", sidecarPath, err)
		}
		// A forensic execution trace must not silently omit its tail. The
		// application therefore opts into the explicit unbounded sink; the
		// logging package retains a defensive bounded default for embedders.
		sink, err := attachForensicSink(logger, file)
		if err != nil {
			_ = file.Close()
			return RunResult{ExitCode: 2}, fmt.Errorf("attach private diagnostic log %q: %w", sidecarPath, err)
		}
		if cfg.Verbosity >= 4 {
			verdictPath = cfg.Name + ".trace-verdict.json"
			verdictFile, err = os.OpenFile(verdictPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				_ = sink.Close()
				_ = file.Close()
				return RunResult{ExitCode: 2}, fmt.Errorf("create private trace verdict %q: %w", verdictPath, err)
			}
		}
		defer func() {
			if err := errors.Join(sink.Close(), file.Close()); err != nil {
				sidecarWriteErr = err
				err = fmt.Errorf("write private diagnostic log %q: %w", sidecarPath, err)
				if runErr == nil {
					runErr = err
					out.ExitCode = 2
				} else {
					runErr = errors.Join(runErr, err)
				}
			}
			if verdictFile == nil {
				return
			}
			trace, err := os.Open(sidecarPath)
			if err == nil {
				verdict := traceverify.Verify(trace)
				if sidecarWriteErr != nil {
					verdict.Status = traceverify.Inconclusive
					verdict.Violations = append(verdict.Violations, traceverify.Violation{
						Code: "trace_write_failed", Severity: "error",
						Remediation: "restore private trace sidecar writes before using forensic evidence",
					})
				}
				finalVerdict = &verdict
				err = trace.Close()
				if err == nil {
					data, marshalErr := json.MarshalIndent(verdict, "", "  ")
					if marshalErr != nil {
						err = marshalErr
					} else if _, writeErr := verdictFile.Write(append(data, '\n')); writeErr != nil {
						err = writeErr
					} else {
						err = verdictFile.Sync()
					}
				}
			}
			if closeErr := verdictFile.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				err = fmt.Errorf("write private trace verdict %q: %w", verdictPath, err)
				if runErr == nil {
					runErr = err
					out.ExitCode = 2
				} else {
					runErr = errors.Join(runErr, err)
				}
				return
			}
			if publicReport != nil && reportsWritten {
				// Public reports expose only the private verdict filename and its
				// final status. Rewriting after finalization prevents a stale
				// "pending" claim while retaining the trace's confidentiality.
				if finalVerdict == nil {
					runErr = errors.Join(runErr, errors.New("private trace verdict was not produced"))
					out.ExitCode = 2
					return
				}
				publicReport.ForensicTrace = &result.ForensicTrace{VerdictPath: verdictPath, Status: string(finalVerdict.Status)}
				// Re-finalize exactly once after every private artifact is closed.
				// The rewritten files and opt-in stdout therefore share the same
				// final forensic attachment; no pending snapshot can escape.
				final, finalizeErr := reportpkg.FinalizeV3(*publicReport, cfg)
				if finalizeErr == nil {
					var paths []string
					paths, finalizeErr = final.Commit(cfg.Name)
					if finalizeErr == nil {
						out.Paths = append(out.Paths[:0], paths...)
						if sidecarPath != "" {
							out.Paths = append(out.Paths, sidecarPath)
						}
						if verdictPath != "" {
							out.Paths = append(out.Paths, verdictPath)
						}
						if cfg.Stdout != "" {
							out.Stdout, finalizeErr = final.Bytes(cfg.Stdout)
						}
					}
				}
				if finalizeErr != nil {
					runErr = errors.Join(runErr, fmt.Errorf("finalize report with forensic trace verdict: %w", finalizeErr))
					out.ExitCode = 2
				}
			}
		}()
	}
	forensicRunID, err := newForensicRunID()
	if err != nil {
		return RunResult{ExitCode: 2}, fmt.Errorf("generate forensic trace run id: %w", err)
	}
	executionProgress, err := newExecutionProgress(forensicRunID, cfg.Concurrency, operatorReporter)
	if err != nil {
		return RunResult{ExitCode: 2}, fmt.Errorf("initialize execution accounting: %w", err)
	}
	reporter = executionProgress
	tracing := telemetry.New(logger, reporter)
	defer tracing.Shutdown(context.Background())
	ctx = logging.WithLogger(ctx, logger)
	ctx, runOp := tracing.Start(ctx, "run", logging.Fields{"run_id": forensicRunID, "suite_count": len(cfg.Tests), "target": "all"})
	runOutcome := "completed"
	runFields := logging.Fields{}
	// This defer is deliberately registered before target defers. On an early
	// return Go closes targets first (LIFO), then the run, preserving the graph
	// invariant that no child outlives its parent.
	defer func() { runOp.End(runOutcome, runFields) }()
	logf := func(level logging.Level, format string, args ...any) { logger.Logf(level, format, args...) }
	// SDK is a first-class test selector. Keep ExtSuites limited to explicitly
	// requested executable extensions; neither selector is rewritten into the
	// other for execution or in the public report.
	extensionCfg := cfg
	r := result.ReportV3{
		SchemaVersion: "aiqa.report.v3",
		GeneratedAt:   time.Now().UTC(),
		Tool:          result.ToolInfo{Name: "aiqa", Version: build.Version, Commit: build.Commit, BuildDate: build.Date, GoVersion: runtime.Version()},
		Config: result.PublicConfigV3{
			Tests: append([]string(nil), cfg.Tests...), Timeout: cfg.Timeout.String(), Formats: append([]string(nil), cfg.Formats...),
			FailOn: cfg.FailOn, Guidance: cfg.Guidance, ExtSuites: append([]string(nil), cfg.ExtSuites...),
			RequestedModels:      append([]string(nil), cfg.Models...),
			AllowHighTokenProbes: cfg.AllowHighTokenProbes, AllowPersistentResources: cfg.AllowPersistentResources,
		},
	}
	publicReport = &r
	addSidecarPath := func(result *RunResult) {
		if sidecarPath != "" {
			result.Paths = append(result.Paths, sidecarPath)
		}
		if verdictPath != "" {
			// The file is created before the first provider action. The deferred
			// finalizer fills it only after every lifecycle span has ended.
			result.Paths = append(result.Paths, verdictPath)
		}
	}
	fatal := func(out RunResult, cause error) (RunResult, error) {
		// A fatal path has not reached a trustworthy final execution state.
		// Never expose a partial aggregate as if it described a completed run.
		if publicReport != nil {
			publicReport.Execution = nil
		}
		// Runtime failures are emitted through the same filtered logger as every
		// other application message. This keeps stderr and the verbose sidecar in
		// sync, and intentionally leaves parse/configuration failures to main.
		// Provider errors can contain response bodies, request URLs or echoed
		// credentials. The trace records only this fixed safe category; the
		// returned error remains available to the interactive caller.
		logf(logging.Fatal, "run failed; inspect public report or safe error classification")
		reporter.Message("error: run failed; inspect public report or safe error classification")
		runOutcome = "error"
		runFields = logging.Fields{"status": "error", "error_present": true, "error_kind": "run_failure"}
		addSidecarPath(&out)
		return out, cause
	}
	extRegistry, err := extsuite.NewRegistry(extopenai.New(), extanthropic.New(), extgoogle.New())
	if err != nil {
		return fatal(RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("register built-in extension suites: %w", err))
	}
	nativeRegistry, err := newProtocolRegistry()
	if err != nil {
		return fatal(RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("register native protocol runners: %w", err))
	}
	if cfg.HasTest("ext-suite") {
		if err := extsuite.RegisterExecutables(ctx, extRegistry, extensionCfg, extsuite.HostInfo{Name: "aiqa", Version: build.Version}); err != nil {
			return fatal(RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("discover extension suites: %w", err))
		}
		if err := extsuite.ValidateExecutableSelection(extRegistry, extensionCfg.ExtSuites); err != nil {
			return fatal(RunResult{ReportV3: &r, ExitCode: 2}, err)
		}
	}
	var runErrors []error
	admission, err := newRunScheduler(cfg)
	if err != nil {
		return fatal(RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("configure request admission: %w", err))
	}
	pacingGroups := configuredPacingGroups(cfg)
	reporter.RunStarted(len(cfg.Targets))
	runOp.Event(logging.Info, "plan", logging.Fields{"suite_count": len(cfg.Tests), "target": "all"})
	runnerLogf := func(level logging.Level, format string, args ...any) { logf(level, format, args...) }
	targets := append([]config.TargetConfig(nil), cfg.Targets...)
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	preparedTargets := make([]protocolpkg.PreparedTarget, len(targets))
	allJobs := make([]progress.Job, 0)
	prepareErrs := make([]error, len(targets))
	parallelTargets(targetWorkflowLimit(cfg, len(targets)), len(targets), func(index int) {
		target := targets[index]
		reporter.Planning("Discovering " + target.ID)
		p, prepareErr := profile.LoadFile(target.ProfileFile)
		if prepareErr == nil && p.Protocol != string(target.Protocol) {
			prepareErr = fmt.Errorf("profile %s uses protocol %s, expected %s", target.ProfileFile, p.Protocol, target.Protocol)
		}
		preparer, ok := nativeRegistry.Preparer(target.Protocol)
		if prepareErr == nil && !ok {
			prepareErr = fmt.Errorf("unsupported target protocol %s", target.Protocol)
		}
		if prepareErr == nil {
			preparedTargets[index], prepareErr = preparer(logging.Scope(ctx, target.ID, "", "", "", ""), protocolpkg.RunOptions{Target: target, Config: cfg, Profile: p, UserAgent: "aiqa/" + build.Version, Logf: runnerLogf, Logger: logger, Telemetry: tracing, Scheduler: admission, PacingGroup: pacingGroups[target.ID]})
		}
		prepareErrs[index] = prepareErr
	})
	// Prepare is a global freeze barrier: no target may execute before every
	// target has either produced its immutable discovery result or failed.
	for index, prepareErr := range prepareErrs {
		if prepareErr != nil {
			return fatal(RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("prepare %s target: %w", targets[index].ID, prepareErr))
		}
	}
	for _, prepared := range preparedTargets {
		for _, job := range prepared.Jobs {
			allJobs = append(allJobs, progress.Job{ID: job.ID, Target: job.Target, Model: job.Model, Label: job.Label})
		}
		allJobs = append(allJobs, extsuite.PlannedJobs(extRegistry, extensionCfg, prepared.Target, prepared.Profile, prepared.Candidates)...)
	}
	plan, err := runPlanFromPrepared(forensicRunID, preparedTargets, allJobs)
	if err != nil {
		return fatal(RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("freeze execution plan: %w", err))
	}
	if err := protocolpkg.ValidatePlan(plan); err != nil {
		return fatal(RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("validate execution plan: %w", err))
	}
	reporter.Freeze(allJobs)
	for _, prepared := range preparedTargets {
		target := prepared.Target
		r.Config.Targets = append(r.Config.Targets, result.TargetPublicConfig{ID: target.ID, Protocol: string(target.Protocol), BaseURL: target.BaseURL, BaseURLNormalized: target.BaseURLNormalized, APIVersion: target.APIVersion})
	}
	type targetExecution struct {
		report       result.TargetReport
		runErr       error
		extensionErr error
		emptyReport  bool
	}
	executions := make([]targetExecution, len(preparedTargets))
	parallelTargets(targetWorkflowLimit(cfg, len(preparedTargets)), len(preparedTargets), func(index int) {
		prepared := preparedTargets[index]
		target, p := prepared.Target, prepared.Profile
		targetCtx := logging.Scope(ctx, target.ID, "", "", "", "")
		targetCtx, targetOp := tracing.Start(targetCtx, "target", logging.Fields{"target": target.ID, "protocol": string(target.Protocol)})
		targetOp.Event(logging.Trace, "plan", logging.Fields{
			"target": target.ID, "protocol": string(target.Protocol),
			"native_selected": cfg.HasTest("compat") || cfg.HasTest("model-param"),
			"sdk_selected":    cfg.HasTest("sdk"), "extension_selected": cfg.HasTest("ext-suite"),
		})
		native, executionErr := prepared.Execute(targetCtx, reporter)
		if native.Target.ID == "" {
			if executionErr == nil {
				executionErr = errors.New("native runner returned no target report")
			}
			targetOp.End("error", logging.Fields{"status": "error", "error_present": true, "error_kind": "native_runner"})
			executions[index] = targetExecution{runErr: executionErr, emptyReport: true}
			return
		}
		targetReport := native.Target
		var extensionErr error
		tmp := reportFromTarget(targetReport, target.BaseURL)
		guidance.Apply(&tmp, p, guidance.Options{Mode: cfg.Guidance, Pack: cfg.GuidancePack, Locale: cfg.GuidanceLocale})
		targetReport.Guidance = tmp.Guidance
		if executionErr != nil {
			logf(logging.Error, "[%s] target runner returned an error", target.ID)
		} else {
			targetReport, extensionErr = finishTargetExtensions(targetCtx, extensionCfg, target, p, targetReport, extRegistry, native.Runtime.Client, native.Runtime.Executor, logger, tracing, reporter)
			if extensionErr != nil {
				logf(logging.Error, "[%s] extension suites returned an error", target.ID)
			}
		}
		outcome := "completed"
		if executionErr != nil || extensionErr != nil {
			outcome = "error"
		}
		targetOp.End(outcome, logging.Fields{"status": outcome, "error_present": outcome == "error"})
		executions[index] = targetExecution{report: targetReport, runErr: executionErr, extensionErr: extensionErr}
	})
	for index, execution := range executions {
		if execution.emptyReport {
			return fatal(RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("run %s target: %w", targets[index].Protocol, execution.runErr))
		}
		r.Targets = append(r.Targets, execution.report)
		if execution.runErr != nil {
			runErrors = append(runErrors, fmt.Errorf("run %s target %s: %w", targets[index].Protocol, targets[index].ID, execution.runErr))
		}
		if execution.extensionErr != nil {
			runErrors = append(runErrors, fmt.Errorf("run extension suites for target %s: %w", targets[index].ID, execution.extensionErr))
		}
	}
	// A requested model selector is cross-target.  Only when no target matched
	// any requested model do we materialize the local no-match coverage gap;
	// one target's empty inventory must not penalize another target that did
	// evaluate the same requested scope.
	globallyUnmatched := len(cfg.Models) > 0
	if globallyUnmatched {
		for _, target := range r.Targets {
			if len(target.Models) > 0 {
				globallyUnmatched = false
				break
			}
		}
	}
	if globallyUnmatched {
		for index := range r.Targets {
			target := r.Targets[index]
			internal := reportFromTarget(target, targets[index].BaseURL)
			internal.Config.RequestedModels = append([]string(nil), cfg.Models...)
			if assessment.ApplyGloballyUnmatchedRequestedScope(&internal, target.ID, target.Protocol, true) {
				r.Targets[index].Requirements = internal.Requirements
				r.Targets[index].Summary = internal.Summary
			}
		}
	}
	// Build the optional aggregate only after every target completed without a
	// runtime error. Counters are mirrored from the frozen plan's actual
	// Start/Complete delivery; unfinished jobs are never synthesized here.
	if len(runErrors) == 0 {
		if summaryErr := executionProgress.Finalize(); summaryErr != nil {
			return fatal(RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("finalize execution accounting: %w", summaryErr))
		}
		r.Execution, err = executionProgress.Summary()
		if err != nil {
			return fatal(RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("build execution summary: %w", err))
		}
	}
	r.Summary = summarizeRun(r.Targets)
	finalReport, err := reportpkg.FinalizeV3(r, cfg)
	if err != nil {
		return fatal(RunResult{ReportV3: &r, ExitCode: 2}, err)
	}
	paths, err := finalReport.Commit(cfg.Name)
	if err != nil {
		return fatal(RunResult{ReportV3: &r, Paths: paths, ExitCode: 2}, err)
	}
	reportsWritten = true
	runOp.Event(logging.Info, "report_serialized", logging.Fields{"status": "completed"})
	var stdout []byte
	if cfg.Stdout != "" {
		stdout, err = finalReport.Bytes(cfg.Stdout)
		if err != nil {
			return fatal(RunResult{ReportV3: &r, Paths: paths, ExitCode: 2}, err)
		}
	}
	out = RunResult{ReportV3: &r, Paths: paths, Stdout: stdout, ExitCode: multiExitCode(cfg, r.Summary)}
	reporter.Message("completed; reports: " + strings.Join(paths, ", "))
	addSidecarPath(&out)
	if len(runErrors) > 0 {
		out.ExitCode = 2
		return fatal(out, errors.Join(runErrors...))
	}
	return out, nil
}

func newForensicRunID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "run-" + hex.EncodeToString(bytes), nil
}

// targetWorkflowLimit controls application-level target orchestration only.
// It deliberately does not claim to be a process-wide HTTP admission limit:
// transport integration owns that invariant. Auto mode activates every
// selected target workflow; physical attempts remain broker-governed.
func targetWorkflowLimit(cfg config.Config, targets int) int {
	if targets < 1 {
		return 1
	}
	limit := cfg.TargetConcurrency
	if limit == 0 {
		// Auto means every selected target may make progress; only the shared
		// transport broker governs physical HTTP concurrency.
		limit = targets
	}
	if limit < 1 {
		limit = 1
	}
	if limit > targets {
		return targets
	}
	return limit
}

// newRunScheduler is the sole owner of request capacity for a run.  Target
// workflow parallelism deliberately does not reserve a request slot; clients
// acquire one only immediately before a physical HTTP attempt.
func newRunScheduler(cfg config.Config) (*scheduler.Broker, error) {
	return scheduler.New(scheduler.Config{
		Capacity:       cfg.Concurrency,
		MemoryBudget:   cfg.RequestMemoryBudget,
		ArtifactBudget: cfg.MaxArtifactsPerModel,
	})
}

func configuredPacingGroups(cfg config.Config) map[string]string {
	groups := make(map[string]string, len(cfg.PacingGroups))
	for _, override := range cfg.PacingGroups {
		parts := strings.SplitN(override, "=", 2)
		if len(parts) == 2 {
			groups[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return groups
}

// parallelTargets runs independent target-local work under one bounded pool.
// Every caller writes only its own indexed result, then aggregates in index
// order after this function returns.
func parallelTargets(limit, targets int, work func(int)) {
	if targets == 0 {
		return
	}
	if limit < 1 {
		limit = 1
	}
	if limit > targets {
		limit = targets
	}
	indices := make(chan int)
	var workers sync.WaitGroup
	workers.Add(limit)
	for worker := 0; worker < limit; worker++ {
		go func() {
			defer workers.Done()
			for index := range indices {
				work(index)
			}
		}()
	}
	for index := 0; index < targets; index++ {
		indices <- index
	}
	close(indices)
	workers.Wait()
}

// runPlanFromPrepared is the application boundary that freezes discovery.
// Native runners receive the PreparedTarget closures that captured this exact
// inventory, so Execute cannot recalculate selection or discover a second time.
func runPlanFromPrepared(id string, prepared []protocolpkg.PreparedTarget, jobs []progress.Job) (*protocolpkg.RunPlan, error) {
	input := protocolpkg.PlanInput{ID: id, Revision: 1}
	for _, target := range prepared {
		input.Targets = append(input.Targets, target.Target.ID)
		binding := target.Binding
		binding.TargetID = target.Target.ID
		if binding.ProfileRevision == "" {
			binding.ProfileRevision = target.Profile.Revision
		}
		if len(binding.SelectedModels) == 0 {
			for _, candidate := range target.Candidates {
				binding.SelectedModels = append(binding.SelectedModels, candidate.ID)
			}
		}
		input.Bindings = append(input.Bindings, binding)
		for _, probe := range target.Profile.Probes {
			input.Requirements = append(input.Requirements, protocolpkg.RequirementSpec{
				ID: target.Target.ID + ":" + probe.ID, Required: probe.Required,
			})
		}
	}
	for _, job := range jobs {
		input.Jobs = append(input.Jobs, protocolpkg.JobSpec{
			ID: job.ID, TargetID: job.Target, ModelID: job.Model, Suite: "planned", Kind: "workflow",
		})
	}
	return protocolpkg.NewRunPlan(input)
}

func newProtocolRegistry() (*protocolpkg.Registry, error) {
	registry := protocolpkg.NewRegistry()
	for _, item := range []struct {
		protocol config.Protocol
		runner   protocolpkg.Runner
		preparer protocolpkg.Preparer
	}{
		{config.ProtocolOpenAI, protocolopenai.RunTarget, protocolopenai.PrepareTarget},
		{config.ProtocolAnthropic, protocolanthropic.RunTarget, protocolanthropic.PrepareTarget},
		{config.ProtocolGoogleGenAI, protocolgoogle.RunTarget, protocolgoogle.PrepareTarget},
	} {
		if err := registry.Register(item.protocol, item.runner); err != nil {
			return nil, err
		}
		if err := registry.RegisterPreparer(item.protocol, item.preparer); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func reportFromTarget(target result.TargetReport, baseURL string) result.Report {
	return result.Report{SchemaVersion: "aiqa.report.internal", Profile: target.Profile, Config: result.PublicConfig{BaseURL: baseURL}, Discovery: target.Discovery, Models: target.Models, Requirements: target.Requirements, Summary: target.Summary, Guidance: target.Guidance, RateLimit: target.RateLimit, ExtensionRateLimit: target.ExtensionRateLimit, Extensions: target.Extensions}
}

func finishTargetExtensions(ctx context.Context, cfg config.Config, target config.TargetConfig, p profile.Profile, targetReport result.TargetReport, registry *extsuite.Registry, client *api.Client, executor *probe.Executor, logger *logging.Logger, tracing *telemetry.Provider, reporter progress.Sink) (result.TargetReport, error) {
	if !cfg.HasExtensionTests() {
		return targetReport, nil
	}
	if client == nil || executor == nil {
		return targetReport, fmt.Errorf("prepare %s extension runtime: native target runtime is unavailable", target.ID)
	}
	before := client.PacingStats()
	targetReport.Extensions = extsuite.ExecuteTargetWithTelemetryAndProgress(ctx, registry, cfg, target, p, targetReport.Models, client, executor, logger, tracing, reporter)
	if hasBuiltInExtension(targetReport.Extensions) {
		after := client.PacingStats()
		delta := api.DeltaPacingStats(before, after)
		targetReport.ExtensionRateLimit = protocolpkg.RateLimitStats(delta)
	}
	tmp := reportFromTarget(targetReport, target.BaseURL)
	tmp.Config.Tests = append([]string(nil), cfg.Tests...)
	reportpkg.Finalize(&tmp)
	guidance.ExtractArtifacts(&tmp)
	guidance.QualifyArtifacts(&tmp, target.ID)
	assessment.ApplyTarget(&tmp, p, target.ID, string(target.Protocol))
	guidance.Apply(&tmp, p, guidance.Options{Mode: cfg.Guidance, Pack: cfg.GuidancePack, Locale: cfg.GuidanceLocale})
	targetReport.Models = tmp.Models
	targetReport.Requirements = tmp.Requirements
	targetReport.Summary = tmp.Summary
	targetReport.Guidance = tmp.Guidance
	targetReport.ExtensionRateLimit = tmp.ExtensionRateLimit
	return targetReport, nil
}

func hasBuiltInExtension(runs []result.ExtensionRun) bool {
	for _, run := range runs {
		if run.Provenance.Source == "builtin" {
			return true
		}
	}
	return false
}

func summarizeRun(targets []result.TargetReport) result.RunSummary {
	out := result.RunSummary{Verdict: "pass", TargetCounts: map[string]int{"pass": 0, "pass-with-gaps": 0, "partial": 0, "fail": 0, "inconclusive": 0}}
	rank := map[string]int{"pass": 0, "pass-with-gaps": 1, "partial": 2, "inconclusive": 3, "fail": 4}
	best := 0
	for _, target := range targets {
		verdict := target.Summary.Status
		if target.Summary.Assessment != nil {
			verdict = target.Summary.Assessment.Verdict
		}
		if verdict == "" {
			verdict = "inconclusive"
		}
		if _, known := rank[verdict]; !known {
			verdict = "inconclusive"
		}
		out.TargetCounts[verdict]++
		if rank[verdict] > best {
			best, out.Verdict = rank[verdict], verdict
		}
		if verdict == "fail" || verdict == "inconclusive" {
			out.BlockingTargets = append(out.BlockingTargets, target.ID)
		}
	}
	switch out.Verdict {
	case "fail":
		out.VerdictReason = "one_or_more_targets_failed_core_conformance"
	case "inconclusive":
		out.VerdictReason = "one_or_more_targets_have_inconclusive_core_evidence"
	case "partial":
		out.VerdictReason = "one_or_more_targets_have_partial_conformance"
	case "pass-with-gaps":
		out.VerdictReason = "one_or_more_targets_have_optional_gaps"
	default:
		out.VerdictReason = "all_targets_passed"
	}
	return out
}

func multiExitCode(cfg config.Config, summary result.RunSummary) int {
	if cfg.FailOn == "never" {
		return 0
	}
	if cfg.FailOn == "any" && summary.Verdict != "pass" {
		return 1
	}
	if cfg.FailOn == "core" && (summary.Verdict == "fail" || summary.Verdict == "inconclusive") {
		return 1
	}
	return 0
}

func exitCode(cfg config.Config, s result.Summary) int {
	if s.Assessment == nil {
		switch cfg.FailOn {
		case "never":
			return 0
		case "any":
			if s.Fail > 0 || s.Error > 0 || s.Warn > 0 || s.Unsupported > 0 {
				return 1
			}
			return 0
		case "core":
			if s.RequiredFail > 0 || s.Fail > 0 || s.Error > 0 {
				return 1
			}
			return 0
		}
	}
	verdict := s.Assessment.Verdict
	switch cfg.FailOn {
	case "never":
		return 0
	case "any":
		if verdict != "pass" {
			return 1
		}
		return 0
	case "core":
		if verdict == "fail" || verdict == "inconclusive" {
			return 1
		}
		return 0
	default:
		return 2
	}
}
