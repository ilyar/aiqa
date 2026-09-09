package extsuite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/probe"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/progress"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/scheduler"
	"github.com/ilyar/aiqa/internal/telemetry"
)

func RegisterExecutables(ctx context.Context, registry *Registry, cfg config.Config, host HostInfo) error {
	executables, err := DiscoverExecutables(ctx, cfg.ExtSuitePaths, os.Getenv("PATH"), host)
	if err != nil {
		return err
	}
	for _, extension := range executables {
		if err := registry.Register(extension); err != nil {
			return err
		}
	}
	return nil
}

// ValidateExecutableSelection rejects built-ins from the external executable
// selector. Built-in SDK suites are selected only by test=sdk.
func ValidateExecutableSelection(registry *Registry, selected []string) error {
	for _, id := range selected {
		extension, ok := registry.Get(id)
		if !ok {
			return fmt.Errorf("selected extension %q is not installed", id)
		}
		if extension.Provenance().Source != "executable" {
			return fmt.Errorf("selected extension %q is not an external executable; use test=sdk", id)
		}
	}
	return nil
}

type invocation struct {
	extension Extension
	manifest  Manifest
	subject   result.SubjectRef
	suiteIDs  []string
}

// FrozenInvocations is the immutable extension selection produced before the
// application's global progress plan is frozen. Its contents intentionally
// remain private: callers may retain and pass the value, but cannot alter the
// selected extension, subject, or suite list after preparation.
type FrozenInvocations struct {
	invocations []invocation
	unsafeLocks *unsafeInvocationLocks
}

// FrozenInvocationBinding is the public, immutable identity of one suite
// selected during FreezeTarget. It deliberately contains no executable path,
// endpoint, credential, or invocation payload.
type FrozenInvocationBinding struct {
	JobID   string
	SuiteID string
	ModelID string
}

// InvocationBindingsForFrozen exposes the exact frozen suite identities for
// the run plan fingerprint without exposing the private invocation structs.
func InvocationBindingsForFrozen(target config.TargetConfig, frozen FrozenInvocations) []FrozenInvocationBinding {
	var bindings []FrozenInvocationBinding
	for _, planned := range frozen.invocations {
		for _, suiteID := range planned.suiteIDs {
			bindings = append(bindings, FrozenInvocationBinding{JobID: extensionJobID(target.ID, planned, suiteID), SuiteID: suiteID, ModelID: subjectModel(planned.subject)})
		}
	}
	return bindings
}

// unsafeInvocationLocks is deliberately owned by the per-run Registry and
// carried by each frozen target plan. Executable extensions which do not
// declare their own concurrency may not run concurrently, even when their
// target executions are parallel. It does not replace broker admission: the
// process still acquires its opaque capacity lease after it owns this lock.
type unsafeInvocationLocks struct {
	mu           sync.Mutex
	byExecutable map[string]*sync.Mutex
}

func (locks *unsafeInvocationLocks) lockFor(planned invocation) *sync.Mutex {
	if locks == nil {
		return nil
	}
	provenance := planned.extension.Provenance()
	key := provenance.Path
	if key == "" {
		key = provenance.ID
	}
	locks.mu.Lock()
	defer locks.mu.Unlock()
	if locks.byExecutable == nil {
		locks.byExecutable = map[string]*sync.Mutex{}
	}
	if locks.byExecutable[key] == nil {
		locks.byExecutable[key] = &sync.Mutex{}
	}
	return locks.byExecutable[key]
}

// FreezeTarget resolves the target-local extension selection exactly once.
// ExecuteFrozenTargetWithTelemetryAndProgress consumes this result instead of
// consulting configuration, profile bindings, or model capabilities again.
func FreezeTarget(registry *Registry, cfg config.Config, target config.TargetConfig, p profile.Profile, models []result.ModelReport) FrozenInvocations {
	return FrozenInvocations{invocations: cloneInvocations(planTarget(registry, cfg, target, p, models)), unsafeLocks: registry.unsafeLocks}
}

// PlannedJobs returns the fixed host-owned logical units an extension phase
// will execute. It is intentionally based on prepared model candidates, so
// the application can freeze one global progress total before native probes
// begin. Executable extensions remain opaque: one declared suite is one job.
func PlannedJobs(registry *Registry, cfg config.Config, target config.TargetConfig, p profile.Profile, models []result.ModelReport) []progress.Job {
	return PlannedJobsForFrozen(target, FreezeTarget(registry, cfg, target, p, models))
}

// PlannedJobsForFrozen returns the exact jobs that a frozen extension
// invocation list can execute. It is the companion to FreezeTarget for a
// caller that owns one global progress freeze barrier.
func PlannedJobsForFrozen(target config.TargetConfig, frozen FrozenInvocations) []progress.Job {
	invocations := frozen.invocations
	jobs := make([]progress.Job, 0)
	for _, planned := range invocations {
		for _, suiteID := range planned.suiteIDs {
			jobs = append(jobs, progress.Job{
				ID: extensionJobID(target.ID, planned, suiteID), Target: target.ID,
				Model: subjectModel(planned.subject), Label: planned.manifest.Extension.ID + "." + suiteID,
			})
		}
	}
	return jobs
}

func extensionJobID(target string, planned invocation, suiteID string) string {
	return "extension:" + target + ":" + planned.manifest.Extension.ID + ":" + diagnosticSubjectRef(planned.subject) + ":" + suiteID
}

// ExecuteBuiltinSuites runs each built-in SDK suite under a distinct suite and
// named probe operation. The callback is invoked with that probe context, so
// the SDK's HTTP transport inherits the concrete suite ID instead of an
// invocation-wide context. Results are emitted before either operation ends,
// which keeps collector diagnostics and validator lifecycle within the same
// trace branch.
func ExecuteBuiltinSuites(ctx context.Context, request RunRequest, extensionID string, sink EventSink, run func(context.Context, string) SuiteEvent) error {
	executor := request.Runtime.Executor
	if executor == nil {
		executor = probe.NewExecutor(1)
	}
	type execution struct {
		ctx   context.Context
		suite *telemetry.Operation
		probe *telemetry.Operation
		event SuiteEvent
	}
	tasks := make([]func() execution, 0, len(request.SuiteIDs))
	for _, suiteID := range request.SuiteIDs {
		suiteID := suiteID
		tasks = append(tasks, func() execution {
			suiteCtx, suiteOp := startBuiltinOperation(ctx, request, "suite", logging.Fields{
				"extension": extensionID,
				"suite":     suiteID,
				"subject":   diagnosticSubjectRef(request.Subject),
			})
			probeCtx, probeOp := startBuiltinOperation(suiteCtx, request, "probe", logging.Fields{
				"extension": extensionID,
				"suite":     suiteID,
				"probe":     extensionID + "." + suiteID,
				"subject":   diagnosticSubjectRef(request.Subject),
			})
			return execution{ctx: probeCtx, suite: suiteOp, probe: probeOp, event: run(probeCtx, suiteID)}
		})
	}
	// probe.Map intentionally returns only after all workers complete, which is
	// right for ordered result slices but wrong for live suite terminals. Keep
	// the same globally bounded executor and consume each completion as it is
	// produced. Collector.Finish later restores the immutable planned order.
	completed := make(chan execution, len(tasks))
	for _, task := range tasks {
		task := task
		go func() {
			executor.Do(func() { completed <- task() })
		}()
	}
	var firstErr error
	for range tasks {
		executed := <-completed
		outcome := string(executed.event.Outcome)
		fields := logging.Fields{"extension": extensionID, "suite": executed.event.SuiteID, "outcome": outcome, "http_status": executed.event.HTTP.StatusCode, "duration_ms": executed.event.DurationMS, "error_present": executed.event.Error != ""}
		if executed.probe != nil {
			executed.probe.Event(logging.Trace, "validator", fields)
		}
		if err := emitSuiteEvent(executed.ctx, sink, executed.event); err != nil {
			if executed.probe != nil {
				executed.probe.End("error", logging.Fields{"reason": "collector_emit"})
			}
			if executed.suite != nil {
				executed.suite.End("error", logging.Fields{"reason": "collector_emit"})
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if executed.probe != nil {
			executed.probe.End(outcome, fields)
		}
		if executed.suite != nil {
			executed.suite.End(outcome, fields)
		}
	}
	return firstErr
}

func startBuiltinOperation(ctx context.Context, request RunRequest, name string, fields logging.Fields) (context.Context, *telemetry.Operation) {
	if request.Runtime.Telemetry == nil {
		return ctx, nil
	}
	return request.Runtime.Telemetry.Start(ctx, name, fields)
}

func emitSuiteEvent(ctx context.Context, sink EventSink, event SuiteEvent) error {
	if contextual, ok := sink.(interface {
		EmitContext(context.Context, SuiteEvent) error
	}); ok {
		return contextual.EmitContext(ctx, event)
	}
	return sink.Emit(event)
}

const maxExtensionDeadline = time.Hour
const extensionRetryLimit = 2

func ExecuteTarget(ctx context.Context, registry *Registry, cfg config.Config, target config.TargetConfig, p profile.Profile, models []result.ModelReport, client *api.Client, executor *probe.Executor) []result.ExtensionRun {
	return ExecuteTargetWithDiagnostics(ctx, registry, cfg, target, p, models, client, executor, nil)
}

// ExecuteTargetWithDiagnostics executes extension suites and emits a safe,
// host-owned timeline. It is additive so existing callers retain the original
// API until they opt into private verbose logging.
func ExecuteTargetWithDiagnostics(ctx context.Context, registry *Registry, cfg config.Config, target config.TargetConfig, p profile.Profile, models []result.ModelReport, client *api.Client, executor *probe.Executor, logger DiagnosticLogger) []result.ExtensionRun {
	return executeTarget(ctx, registry, cfg, target, p, models, client, executor, logger, nil, nil)
}

// ExecuteTargetWithTelemetry adds local-only operation correlation to extension
// invocations while preserving the legacy diagnostics API for direct callers.
func ExecuteTargetWithTelemetry(ctx context.Context, registry *Registry, cfg config.Config, target config.TargetConfig, p profile.Profile, models []result.ModelReport, client *api.Client, executor *probe.Executor, logger DiagnosticLogger, provider *telemetry.Provider) []result.ExtensionRun {
	return executeTarget(ctx, registry, cfg, target, p, models, client, executor, logger, provider, nil)
}

// ExecuteTargetWithTelemetryAndProgress adds declared extension-suite units to
// progress. Built-in suites with telemetry retain their concrete probe
// lifecycle; executable suites (and compatibility callers without telemetry)
// receive one host-owned terminal unit per declared suite. External HTTP is
// deliberately never inferred from extension process activity.
func ExecuteTargetWithTelemetryAndProgress(ctx context.Context, registry *Registry, cfg config.Config, target config.TargetConfig, p profile.Profile, models []result.ModelReport, client *api.Client, executor *probe.Executor, logger DiagnosticLogger, provider *telemetry.Provider, sink progress.Sink) []result.ExtensionRun {
	return executeTarget(ctx, registry, cfg, target, p, models, client, executor, logger, provider, sink)
}

// ExecuteFrozenTargetWithTelemetryAndProgress executes a selection previously
// returned by FreezeTarget. It deliberately does not call planTarget, so a
// post-freeze change to configuration, profile, or model capability data
// cannot alter the extension suites that run.
func ExecuteFrozenTargetWithTelemetryAndProgress(ctx context.Context, cfg config.Config, target config.TargetConfig, p profile.Profile, frozen FrozenInvocations, client *api.Client, executor *probe.Executor, logger DiagnosticLogger, provider *telemetry.Provider, sink progress.Sink) []result.ExtensionRun {
	return executeInvocations(ctx, cfg, target, p, frozen.invocations, frozen.unsafeLocks, client, executor, logger, provider, sink)
}

func executeTarget(ctx context.Context, registry *Registry, cfg config.Config, target config.TargetConfig, p profile.Profile, models []result.ModelReport, client *api.Client, executor *probe.Executor, logger DiagnosticLogger, provider *telemetry.Provider, sink progress.Sink) []result.ExtensionRun {
	frozen := FreezeTarget(registry, cfg, target, p, models)
	return executeInvocations(ctx, cfg, target, p, frozen.invocations, frozen.unsafeLocks, client, executor, logger, provider, sink)
}

func executeInvocations(ctx context.Context, cfg config.Config, target config.TargetConfig, p profile.Profile, invocations []invocation, unsafeLocks *unsafeInvocationLocks, client *api.Client, executor *probe.Executor, logger DiagnosticLogger, provider *telemetry.Provider, sink progress.Sink) []result.ExtensionRun {
	if len(invocations) == 0 {
		return nil
	}
	// Legacy callers can still build a target-local total. A FrozenSink was
	// already given these exact jobs by app during planning; late revisions are
	// forbidden there.
	if sink != nil {
		if _, frozen := sink.(progress.FrozenSink); frozen {
			// Nothing: the immutable global plan is already published.
		} else {
			total := 0
			for _, planned := range invocations {
				total += len(planned.suiteIDs)
			}
			sink.AddPlan(target.ID, total)
		}
	}
	runs := make([]result.ExtensionRun, len(invocations))
	var wg sync.WaitGroup
	for index, planned := range invocations {
		index, planned := index, planned
		wg.Add(1)
		go func() {
			defer wg.Done()
			frozen, frozenProgress := sink.(progress.FrozenSink)
			if frozenProgress {
				for _, suiteID := range planned.suiteIDs {
					frozen.Start(extensionJobID(target.ID, planned, suiteID))
				}
			}
			invocationCtx := logging.Scope(ctx, target.ID, subjectModel(planned.subject), "", "", "")
			var invocationEnd func(string, logging.Fields)
			var subjectEnd func(string, logging.Fields)
			if provider != nil {
				// SDK suites are still target-local tests. Establish the report
				// subject boundary before the runner grouping operation so the
				// graph remains target -> provider/model -> suite -> probe.
				subjectName := "provider"
				if planned.subject.Kind == "model" {
					subjectName = "model"
				}
				var subjectOp *telemetry.Operation
				invocationCtx, subjectOp = provider.Start(invocationCtx, subjectName, logging.Fields{"target": target.ID, "model": subjectModel(planned.subject)})
				subjectEnd = subjectOp.End
				var invocationOp *telemetry.Operation
				// An invocation only groups the runner process. Each planned SDK
				// suite below creates its own `suite` span; do not mislabel this
				// shared operation as a suite in the forensic graph.
				invocationCtx, invocationOp = provider.Start(invocationCtx, "extension.invocation", logging.Fields{"target": target.ID, "extension": planned.manifest.Extension.ID, "suite_count": len(planned.suiteIDs), "source": planned.extension.Provenance().Source})
				invocationEnd = invocationOp.End
			} else {
				invocationEnd = func(string, logging.Fields) {}
				subjectEnd = func(string, logging.Fields) {}
			}
			allowedConcurrency := cfg.Concurrency
			if planned.extension.Provenance().Source == "executable" {
				allowedConcurrency = declaredSlots(planned.manifest, planned.suiteIDs, cfg.Concurrency)
			}
			deadline := extensionDeadline(cfg.Timeout, len(planned.suiteIDs))
			pacing := api.PacingStats{}
			if client != nil {
				pacing = client.PacingConstraints()
			}
			request := RunRequest{
				SchemaVersion: ProtocolVersion,
				Type:          "run_request",
				RunID:         runID(target.ID, planned.manifest.Extension.ID, planned.subject),
				Target:        Target{ID: target.ID, Protocol: string(target.Protocol), BaseURL: target.BaseURL, APIVersion: target.APIVersion},
				Subject:       planned.subject,
				SuiteIDs:      append([]string(nil), planned.suiteIDs...),
				Profile:       ProfileRef{ID: p.ID, Revision: p.Revision},
				Limits: Limits{
					RequestTimeoutMS:              cfg.Timeout.Milliseconds(),
					DeadlineMS:                    deadline.Milliseconds(),
					MaxConcurrency:                allowedConcurrency,
					RequestIntervalMS:             target.RequestInterval.Milliseconds(),
					RetryLimit:                    extensionRetryLimit,
					ObservedSafeRequestsPerSecond: pacing.SafeRate,
					ObservedSafeConcurrency:       pacing.SafeConcurrency,
					MaxOutputBytes:                maxStdoutBytes,
				},
				Credential: Credential{Type: "api_key", Value: target.APIKey},
				Runtime:    RuntimeServices{Executor: executor, DiagnosticLogger: logger, Telemetry: provider, InvocationContext: invocationCtx},
			}
			if client != nil {
				request.Runtime.HTTPClient = client.PacedHTTPClient(planned.subject.ID)
			}
			logDiagnostic(logger, logging.Debug, "extsuite.invocation.started", logging.Fields{"target": target.ID, "extension": planned.manifest.Extension.ID, "source": planned.extension.Provenance().Source, "subject_kind": planned.subject.Kind, "subject_ref": diagnosticSubjectRef(planned.subject), "suite_count": len(planned.suiteIDs), "deadline_ms": deadline.Milliseconds(), "max_concurrency": allowedConcurrency})
			logDiagnostic(logger, logging.Trace, "extsuite.invocation.plan", logging.Fields{"target": target.ID, "extension": planned.manifest.Extension.ID, "subject_ref": diagnosticSubjectRef(planned.subject), "suites": append([]string(nil), planned.suiteIDs...), "request_timeout_ms": request.Limits.RequestTimeoutMS, "request_interval_ms": request.Limits.RequestIntervalMS, "retry_limit": request.Limits.RetryLimit, "pacing_safe_rps": request.Limits.ObservedSafeRequestsPerSecond, "pacing_safe_concurrency": request.Limits.ObservedSafeConcurrency})
			for _, suiteID := range planned.suiteIDs {
				logDiagnostic(logger, logging.Debug, "extsuite.suite.scheduled", logging.Fields{"extension": planned.manifest.Extension.ID, "suite": suiteID, "subject_ref": diagnosticSubjectRef(planned.subject)})
			}
			collector, err := NewCollectorWithDiagnostics(planned.manifest, request, p, logger)
			started := time.Now()
			if err != nil {
				provenance := planned.extension.Provenance()
				pacingCompliance := "host_enforced"
				if provenance.Source == "executable" {
					pacingCompliance = "extension_required_unverified"
				}
				runs[index] = result.ExtensionRun{Provenance: provenance, TargetID: target.ID, Subject: planned.subject, Runner: result.ExtensionRunnerResult{Status: result.StatusError, PacingCompliance: pacingCompliance, Summary: "Extension invocation could not be validated", Error: err.Error()}}
				reportRunnerErrorSuiteTerminals(sink, frozen, frozenProgress, target.ID, planned, time.Now())
				logDiagnostic(logger, logging.Error, "extsuite.invocation.rejected", logging.Fields{"target": target.ID, "extension": planned.manifest.Extension.ID, "subject_ref": diagnosticSubjectRef(planned.subject), "reason": "collector_validation"})
				invocationEnd("error", logging.Fields{"status": "error", "error_present": true, "error_kind": "collector_validation"})
				subjectEnd("error", logging.Fields{"status": "error", "error_present": true, "error_kind": "collector_validation"})
				return
			}
			// Deliver a suite terminal as soon as the host has validated it.
			// probe.Map retains final report ordering but waits for every worker;
			// tying lifecycle delivery to its return hides fast SDK completions.
			if err := collector.SetTerminalObserver(func(suite result.ExtensionSuiteResult) {
				reportSuiteTerminal(sink, frozen, frozenProgress, target.ID, planned, suite, string(suite.Status), time.Now())
			}); err != nil {
				runs[index] = result.ExtensionRun{Provenance: planned.extension.Provenance(), TargetID: target.ID, Subject: planned.subject, Runner: result.ExtensionRunnerResult{Status: result.StatusError, Summary: "Extension invocation could not initialize terminal delivery", Error: err.Error()}}
				reportRunnerErrorSuiteTerminals(sink, frozen, frozenProgress, target.ID, planned, time.Now())
				invocationEnd("error", logging.Fields{"status": "error", "error_present": true, "error_kind": "terminal_delivery"})
				subjectEnd("error", logging.Fields{"status": "error", "error_present": true, "error_kind": "terminal_delivery"})
				return
			}
			runCtx := invocationCtx
			cancel := func() {}
			if planned.extension.Provenance().Source == "executable" {
				runCtx, cancel = context.WithTimeout(invocationCtx, deadline)
			}
			defer cancel()
			var telemetry RunnerTelemetry
			var runErr error
			if planned.extension.Provenance().Source == "executable" {
				slots := declaredSlots(planned.manifest, planned.suiteIDs, cfg.Concurrency)
				if hasUnsafeConcurrency(planned.manifest, planned.suiteIDs) {
					lock := unsafeLocks.lockFor(planned)
					if lock != nil {
						lock.Lock()
						defer lock.Unlock()
					}
				}
				// An executable is opaque to the host: reserve its declared
				// capacity for the full process lifetime rather than pretending
				// to observe its individual HTTP attempts. This is run-wide when
				// the app has bound the client to its shared broker.
				var leaseErr error
				var lease *scheduler.Lease
				if client != nil {
					lease, leaseErr = client.AcquireOpaqueLease(runCtx, slots)
				}
				if leaseErr != nil {
					runErr = fmt.Errorf("executable admission unavailable: %w", leaseErr)
				} else {
					executor.DoN(slots, func() { telemetry, runErr = planned.extension.Run(runCtx, request, collector) })
					if lease != nil {
						lease.Release()
					}
				}
			} else {
				// A built-in SDK invocation owns the actual provider dispatch.  Its
				// declared billable-artifact suites therefore reserve the shared
				// target/model budget before entering extension code, then commit at
				// the precise boundary where that code may dispatch.  Do not make
				// executable extensions participate: their process is opaque and
				// cannot prove its provider-dispatch boundary to the host.
				if client != nil && invocationHasBillableArtifact(planned.manifest, planned.suiteIDs) {
					reservation, reservationErr := client.ReserveArtifact(runCtx, planned.subject.ID)
					if reservationErr != nil {
						runErr = fmt.Errorf("artifact admission unavailable: %w", reservationErr)
					} else {
						reservation.Commit()
						telemetry, runErr = planned.extension.Run(runCtx, request, collector)
					}
				} else {
					telemetry, runErr = planned.extension.Run(runCtx, request, collector)
				}
			}
			if runCtx.Err() != nil {
				runErr = fmt.Errorf("extension deadline exceeded: %w", runCtx.Err())
			}
			runs[index] = collector.Finish(planned.extension.Provenance(), telemetry, runErr, started)
			reportMissingSuiteTerminals(sink, frozen, frozenProgress, target.ID, planned, runs[index], time.Now())
			fields := logging.Fields{"status": string(runs[index].Runner.Status), "total_ms": runs[index].Runner.TotalDurationMS, "exit_code": runs[index].Runner.ExitCode, "error_present": runErr != nil}
			if runErr != nil {
				fields["error_kind"] = "extension_runner"
			}
			invocationEnd(string(runs[index].Runner.Status), fields)
			subjectEnd(string(runs[index].Runner.Status), fields)
		}()
	}
	wg.Wait()
	return runs
}

// invocationHasBillableArtifact is evaluated against the frozen invocation,
// rather than the full manifest: a selected text-only subset must not consume
// a model's artifact allowance merely because the extension supports one.
func invocationHasBillableArtifact(manifest Manifest, suiteIDs []string) bool {
	selected := make(map[string]struct{}, len(suiteIDs))
	for _, suiteID := range suiteIDs {
		selected[suiteID] = struct{}{}
	}
	for _, suite := range manifest.Suites {
		if suite.Cost != "billable_artifact" {
			continue
		}
		if _, ok := selected[suite.ID]; ok {
			return true
		}
	}
	return false
}

func reportSuiteTerminal(sink progress.Sink, frozen progress.FrozenSink, frozenProgress bool, target string, planned invocation, suite result.ExtensionSuiteResult, outcome string, at time.Time) {
	if frozenProgress {
		frozen.Complete(extensionJobID(target, planned, suite.SuiteID), outcome)
	}
	if sink == nil {
		return
	}
	sink.ObserveLifecycle(telemetry.LifecycleEvent{
		Time:        at,
		Name:        "extension.suite",
		Phase:       "end",
		OperationID: "extension:" + target + ":" + planned.manifest.Extension.ID + ":" + diagnosticSubjectRef(planned.subject) + ":" + suite.SuiteID,
		Target:      target,
		Model:       subjectModel(planned.subject),
		Suite:       suite.SuiteID,
		Outcome:     outcome,
	})
}

// reportMissingSuiteTerminals closes only suites with no locally validated
// result. A missing terminal is a runner_error, never a generic "completed"
// progress result, even if a sibling suite passed.
func reportMissingSuiteTerminals(sink progress.Sink, frozen progress.FrozenSink, frozenProgress bool, target string, planned invocation, run result.ExtensionRun, at time.Time) {
	for _, suite := range run.Suites {
		if !hasSignal(suite.Signals, "extension.runner.missing_result") {
			continue
		}
		reportSuiteTerminal(sink, frozen, frozenProgress, target, planned, suite, "runner_error", at)
	}
}

func reportRunnerErrorSuiteTerminals(sink progress.Sink, frozen progress.FrozenSink, frozenProgress bool, target string, planned invocation, at time.Time) {
	for _, suiteID := range planned.suiteIDs {
		reportSuiteTerminal(sink, frozen, frozenProgress, target, planned, result.ExtensionSuiteResult{SuiteID: suiteID}, "runner_error", at)
	}
}

func hasSignal(signals []result.EvidenceSignal, code string) bool {
	for _, signal := range signals {
		if signal.Code == code {
			return true
		}
	}
	return false
}

func subjectModel(subject result.SubjectRef) string {
	if subject.Kind == "model" {
		return subject.ID
	}
	return ""
}

func extensionDeadline(timeout time.Duration, suites int) time.Duration {
	count := time.Duration(max(1, suites))
	if timeout >= maxExtensionDeadline/count {
		return maxExtensionDeadline
	}
	return timeout * count
}

func hasUnsafeConcurrency(manifest Manifest, suiteIDs []string) bool {
	wanted := map[string]bool{}
	for _, id := range suiteIDs {
		wanted[id] = true
	}
	for _, suite := range manifest.Suites {
		if wanted[suite.ID] && suite.InternalConcurrency == 0 {
			return true
		}
	}
	return false
}

func cloneInvocations(in []invocation) []invocation {
	out := append([]invocation(nil), in...)
	for index := range out {
		out[index].suiteIDs = append([]string(nil), out[index].suiteIDs...)
	}
	return out
}

func planTarget(registry *Registry, cfg config.Config, target config.TargetConfig, p profile.Profile, models []result.ModelReport) []invocation {
	selected := map[string]bool{}
	for _, id := range cfg.ExtSuites {
		selected[id] = true
	}
	artifactBudgets := make([]int, len(models))
	for index, model := range models {
		artifactBudgets[index] = cfg.MaxArtifactsPerModel
		if cfg.HasTest("compat") {
			artifactBudgets[index] -= nativeArtifactReservations(p, model)
			if artifactBudgets[index] < 0 {
				artifactBudgets[index] = 0
			}
		}
	}
	var out []invocation
	for _, extension := range registry.All() {
		manifest := extension.Manifest()
		if !selectsExtension(cfg, extension, manifest, selected) || !supportsProtocol(manifest, string(target.Protocol)) || !contains(manifest.ProfileContracts, p.SchemaVersion) {
			continue
		}
		providerSuites := applicableSuites(manifest, cfg, p, nil, nil)
		if len(providerSuites) > 0 {
			out = append(out, invocation{extension: extension, manifest: manifest, subject: result.SubjectRef{TargetID: target.ID, Protocol: string(target.Protocol), Kind: "provider", ID: strings.TrimRight(target.BaseURL, "/")}, suiteIDs: providerSuites})
		}
		for index := range models {
			model := &models[index]
			suites := applicableSuites(manifest, cfg, p, model, &artifactBudgets[index])
			if len(suites) == 0 {
				continue
			}
			out = append(out, invocation{extension: extension, manifest: manifest, subject: result.SubjectRef{TargetID: target.ID, Protocol: string(target.Protocol), Kind: "model", ID: model.ID}, suiteIDs: suites})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.manifest.Extension.ID != b.manifest.Extension.ID {
			return a.manifest.Extension.ID < b.manifest.Extension.ID
		}
		if a.subject.Kind != b.subject.Kind {
			return a.subject.Kind < b.subject.Kind
		}
		return a.subject.ID < b.subject.ID
	})
	return out
}

// selectsExtension keeps sdk independent from ExtSuites. Built-in suites are
// selected directly by their provenance and protocol; explicit ext-suite
// selections are unioned with that set without producing duplicate invocations.
func selectsExtension(cfg config.Config, extension Extension, manifest Manifest, selected map[string]bool) bool {
	if cfg.HasTest("sdk") && extension.Provenance().Source == "builtin" {
		return true
	}
	if !cfg.HasTest("ext-suite") {
		return false
	}
	if extension.Provenance().Source != "executable" {
		return false
	}
	return len(selected) == 0 || selected[manifest.Extension.ID]
}

func applicableSuites(manifest Manifest, cfg config.Config, p profile.Profile, model *result.ModelReport, artifactBudget *int) []string {
	var out []string
	for _, suite := range manifest.Suites {
		if model == nil && suite.Subject != "provider" || model != nil && suite.Subject != "model" {
			continue
		}
		binding, bound := p.ExtensionBinding(manifest.Extension.ID, suite.ID)
		if model != nil {
			if !hasCapabilities(*model, suite.Capabilities) {
				continue
			}
			if bound && !moduleExecuted(*model, binding.Module) {
				continue
			}
		}
		if suite.Cost == "billable_artifact" {
			if !cfg.AllowBillableArtifacts || artifactBudget == nil || *artifactBudget < 1 {
				continue
			}
			*artifactBudget = *artifactBudget - 1
		}
		out = append(out, suite.ID)
	}
	sort.Strings(out)
	return out
}

func nativeArtifactReservations(p profile.Profile, model result.ModelReport) int {
	reserved := 0
	for _, assessment := range model.Modules {
		if assessment.Execution != result.ExecutionExecuted {
			continue
		}
		module, ok := p.Module(assessment.ID)
		if ok && module.CostClass == "billable-artifact" {
			reserved++
		}
	}
	return reserved
}

func hasCapabilities(model result.ModelReport, required []string) bool {
	available := map[string]bool{}
	for _, capability := range model.Capabilities {
		available[capability.ID] = true
	}
	for _, capability := range required {
		if !available[capability] {
			return false
		}
	}
	return true
}

func moduleExecuted(model result.ModelReport, moduleID string) bool {
	for _, module := range model.Modules {
		if module.ID == moduleID {
			return module.Execution == result.ExecutionExecuted
		}
	}
	return false
}

func declaredSlots(manifest Manifest, suiteIDs []string, ceiling int) int {
	slots := 1
	wanted := map[string]bool{}
	for _, id := range suiteIDs {
		wanted[id] = true
	}
	for _, suite := range manifest.Suites {
		if wanted[suite.ID] && suite.InternalConcurrency > slots {
			slots = suite.InternalConcurrency
		}
	}
	if slots > ceiling {
		return ceiling
	}
	return slots
}

func runID(parts ...any) string {
	hash := sha256.New()
	_, _ = fmt.Fprint(hash, time.Now().UnixNano())
	for _, part := range parts {
		_, _ = fmt.Fprint(hash, "\x00", part)
	}
	return hex.EncodeToString(hash.Sum(nil)[:16])
}
