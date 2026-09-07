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
	var firstErr error
	for _, executed := range probe.Map(executor, tasks...) {
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

func executeTarget(ctx context.Context, registry *Registry, cfg config.Config, target config.TargetConfig, p profile.Profile, models []result.ModelReport, client *api.Client, executor *probe.Executor, logger DiagnosticLogger, provider *telemetry.Provider, sink progress.Sink) []result.ExtensionRun {
	invocations := planTarget(registry, cfg, target, p, models)
	if len(invocations) == 0 {
		return nil
	}
	// Native work may already be complete when SDK/extensions are scheduled.
	// Reserve every declared host suite before the first invocation goroutine
	// can create a probe, so the operator never sees a false 100% while an SDK
	// suite is about to run. Built-ins expose one concrete probe per declared
	// suite; opaque paths use the synthetic terminal below.
	if sink != nil {
		total := 0
		for _, planned := range invocations {
			total += len(planned.suiteIDs)
		}
		sink.AddPlan(target.ID, total)
	}
	runs := make([]result.ExtensionRun, len(invocations))
	var wg sync.WaitGroup
	unsafeLocks := map[string]*sync.Mutex{}
	for _, planned := range invocations {
		if planned.extension.Provenance().Source == "executable" && hasUnsafeConcurrency(planned.manifest, planned.suiteIDs) {
			if unsafeLocks[planned.manifest.Extension.ID] == nil {
				unsafeLocks[planned.manifest.Extension.ID] = &sync.Mutex{}
			}
		}
	}
	for index, planned := range invocations {
		index, planned := index, planned
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Built-ins produce concrete host probe operations when telemetry is
			// available. Every other invocation needs a single declared-suite
			// terminal so progress does not mistake opaque extension work for HTTP.
			syntheticProgress := sink != nil && (provider == nil || planned.extension.Provenance().Source == "executable")
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
				if syntheticProgress {
					reportDeclaredSuiteTerminals(sink, target.ID, planned, runs[index], time.Now())
				}
				logDiagnostic(logger, logging.Error, "extsuite.invocation.rejected", logging.Fields{"target": target.ID, "extension": planned.manifest.Extension.ID, "subject_ref": diagnosticSubjectRef(planned.subject), "reason": "collector_validation"})
				invocationEnd("error", logging.Fields{"status": "error", "error_present": true, "error_kind": "collector_validation"})
				subjectEnd("error", logging.Fields{"status": "error", "error_present": true, "error_kind": "collector_validation"})
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
				if lock := unsafeLocks[planned.manifest.Extension.ID]; lock != nil {
					lock.Lock()
					defer lock.Unlock()
				}
				executor.DoN(slots, func() { telemetry, runErr = planned.extension.Run(runCtx, request, collector) })
			} else {
				telemetry, runErr = planned.extension.Run(runCtx, request, collector)
			}
			if runCtx.Err() != nil {
				runErr = fmt.Errorf("extension deadline exceeded: %w", runCtx.Err())
			}
			runs[index] = collector.Finish(planned.extension.Provenance(), telemetry, runErr, started)
			if syntheticProgress {
				reportDeclaredSuiteTerminals(sink, target.ID, planned, runs[index], time.Now())
			}
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

func reportDeclaredSuiteTerminals(sink progress.Sink, target string, planned invocation, run result.ExtensionRun, at time.Time) {
	if sink == nil {
		return
	}
	statuses := make(map[string]result.Status, len(run.Suites))
	for _, suite := range run.Suites {
		statuses[suite.SuiteID] = suite.Status
	}
	for _, suiteID := range planned.suiteIDs {
		outcome := string(statuses[suiteID])
		if outcome == "" {
			outcome = string(run.Runner.Status)
		}
		sink.ObserveLifecycle(telemetry.LifecycleEvent{
			Time:        at,
			Name:        "extension.suite",
			Phase:       "end",
			OperationID: "extension:" + target + ":" + planned.manifest.Extension.ID + ":" + diagnosticSubjectRef(planned.subject) + ":" + suiteID,
			Target:      target,
			Model:       subjectModel(planned.subject),
			Suite:       suiteID,
			Outcome:     outcome,
		})
	}
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
