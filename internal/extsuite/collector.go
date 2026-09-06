package extsuite

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/telemetry"
)

type Collector struct {
	manifest    Manifest
	request     RunRequest
	profile     profile.Profile
	secret      string
	planned     map[string]int
	seen        map[string]bool
	results     []result.ExtensionSuiteResult
	logger      DiagnosticLogger
	trace       map[string]*extensionTraceSpan
	traceUsed   map[string]bool
	traceSuites map[string]int
	mu          sync.Mutex
}

type extensionTraceSpan struct {
	suiteID   string
	operation string
	op        *telemetry.Operation
	ctx       context.Context
}

func NewCollector(manifest Manifest, request RunRequest, p profile.Profile) (*Collector, error) {
	return NewCollectorWithDiagnostics(manifest, request, p, request.Runtime.DiagnosticLogger)
}

// NewCollectorWithDiagnostics adds host-owned diagnostics without modifying
// the wire contract presented to executable extensions.
func NewCollectorWithDiagnostics(manifest Manifest, request RunRequest, p profile.Profile, logger DiagnosticLogger) (*Collector, error) {
	if err := ValidateManifest(&manifest); err != nil {
		return nil, err
	}
	planned := make(map[string]int, len(request.SuiteIDs))
	known := map[string]SuiteDescriptor{}
	for _, suite := range manifest.Suites {
		known[suite.ID] = suite
	}
	for index, suiteID := range request.SuiteIDs {
		if _, duplicate := planned[suiteID]; duplicate {
			return nil, fmt.Errorf("duplicate planned extension suite %q", suiteID)
		}
		suite, ok := known[suiteID]
		if !ok {
			return nil, fmt.Errorf("unknown planned extension suite %q", suiteID)
		}
		if suite.Subject != request.Subject.Kind {
			return nil, fmt.Errorf("suite %q requires %s subject, got %s", suiteID, suite.Subject, request.Subject.Kind)
		}
		if binding, bound := p.ExtensionBinding(manifest.Extension.ID, suiteID); bound {
			if binding.Subject != suite.Subject {
				return nil, fmt.Errorf("profile binding for %q has subject %q, manifest declares %q", suiteID, binding.Subject, suite.Subject)
			}
			module, ok := p.Module(binding.Module)
			if !ok {
				return nil, fmt.Errorf("profile binding for %q references unknown module %q", suiteID, binding.Module)
			}
			if suite.Cost == "billable_artifact" && module.CostClass != "billable-artifact" {
				return nil, fmt.Errorf("billable suite %q is bound to module %q without billable-artifact cost metadata", suiteID, binding.Module)
			}
		}
		planned[suiteID] = index
	}
	return &Collector{manifest: manifest, request: request, profile: p, secret: request.Credential.Value, planned: planned, seen: map[string]bool{}, logger: logger, trace: map[string]*extensionTraceSpan{}, traceUsed: map[string]bool{}, traceSuites: map[string]int{}}, nil
}

func (c *Collector) Emit(event SuiteEvent) error {
	return c.emit(context.Background(), event)
}

// EmitContext records a built-in SDK suite result under the context that made
// its HTTP calls. It is intentionally not part of the executable extension
// wire contract: external extensions cannot provide host trace contexts.
func (c *Collector) EmitContext(ctx context.Context, event SuiteEvent) error {
	return c.emit(ctx, event)
}

// EmitTrace validates a v2 cooperative extension record and maps it to a
// host-owned local telemetry operation. The executable supplies only local
// opaque IDs; it never controls host trace/span IDs or arbitrary log fields.
func (c *Collector) EmitTrace(event TraceEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateTraceEnvelope(event); err != nil {
		return err
	}
	if c.seen[event.SuiteID] {
		return fmt.Errorf("extension emitted trace after terminal suite result %q", event.SuiteID)
	}
	// start owns its ID until its matching end; validators are one-shot IDs.
	// An end deliberately reuses its corresponding start ID.
	if event.Phase != "end" {
		if c.traceUsed[event.EventID] {
			return fmt.Errorf("extension reused trace event id %q", event.EventID)
		}
		c.traceUsed[event.EventID] = true
	}
	c.traceSuites[event.SuiteID]++
	fields := logging.Fields{"extension": c.manifest.Extension.ID, "suite": event.SuiteID, "source": "executable"}
	if event.Method != "" {
		fields["method"] = event.Method
	}
	if event.Attempt != 0 {
		fields["attempt"] = event.Attempt
	}
	if event.Status != 0 {
		fields["http_status"] = event.Status
	}
	if event.DurationMS != 0 {
		fields["duration_ms"] = event.DurationMS
	}
	if event.Outcome != "" {
		fields["outcome"] = event.Outcome
	}
	switch event.Phase {
	case "start":
		parentCtx := c.request.Runtime.InvocationContext
		if event.ParentEventID != "" {
			parent, ok := c.trace[event.ParentEventID]
			if !ok {
				return fmt.Errorf("extension trace event %q references an unknown or closed parent %q", event.EventID, event.ParentEventID)
			}
			if parent.suiteID != event.SuiteID || !validTraceParent(parent.operation, event.Operation) {
				return fmt.Errorf("extension trace event %q has invalid parent ownership", event.EventID)
			}
			parentCtx = parent.ctx
		} else if event.Operation != "suite" {
			return fmt.Errorf("extension trace event %q requires a parent", event.EventID)
		}
		if c.request.Runtime.Telemetry != nil {
			_, op := c.request.Runtime.Telemetry.Start(parentCtx, hostTraceOperation(event.Operation), fields)
			c.trace[event.EventID] = &extensionTraceSpan{suiteID: event.SuiteID, operation: event.Operation, op: op, ctx: op.Context()}
		} else {
			c.trace[event.EventID] = &extensionTraceSpan{suiteID: event.SuiteID, operation: event.Operation, ctx: parentCtx}
		}
	case "end":
		span, ok := c.trace[event.EventID]
		if !ok || span.suiteID != event.SuiteID || span.operation != event.Operation {
			return fmt.Errorf("extension trace end %q has no matching active start", event.EventID)
		}
		if span.op != nil {
			span.op.End(traceOutcome(event), fields)
		}
		delete(c.trace, event.EventID)
	case "validator":
		parent, ok := c.trace[event.ParentEventID]
		if !ok || parent.suiteID != event.SuiteID || parent.operation != "probe" {
			return fmt.Errorf("extension validator %q requires an active probe parent", event.EventID)
		}
		if parent.op != nil {
			parent.op.Event(logging.Trace, "validator", fields)
		}
	default:
		return fmt.Errorf("extension trace event %q has invalid phase %q", event.EventID, event.Phase)
	}
	return nil
}

func (c *Collector) validateTraceEnvelope(event TraceEvent) error {
	if event.SchemaVersion != ProtocolVersion || event.Type != "trace_event" || event.RunID != c.request.RunID || event.ExtensionID != c.manifest.Extension.ID {
		return fmt.Errorf("invalid extension trace envelope")
	}
	if _, ok := c.planned[event.SuiteID]; !ok {
		return fmt.Errorf("extension trace references unplanned suite %q", event.SuiteID)
	}
	if !stableID.MatchString(event.EventID) || event.ParentEventID != "" && !stableID.MatchString(event.ParentEventID) {
		return fmt.Errorf("extension trace has invalid local event id")
	}
	if !validTraceOperation(event.Operation) || !validTracePhase(event.Phase) || event.Attempt < 0 || event.Status < 0 || event.Status > 599 || event.DurationMS < 0 || !validTraceMethod(event.Method) || !validTraceOutcome(event.Outcome) {
		return fmt.Errorf("extension trace event %q has unsafe or invalid fields", event.EventID)
	}
	if event.Phase == "validator" && event.Operation != "probe" {
		return fmt.Errorf("extension validator must use probe operation")
	}
	if event.Phase == "start" && event.ParentEventID == "" && event.Operation != "suite" {
		return fmt.Errorf("extension trace start %q requires suite root", event.EventID)
	}
	if event.Phase == "end" && event.ParentEventID != "" {
		return fmt.Errorf("extension trace end %q must not declare parent", event.EventID)
	}
	return nil
}

func validTraceOperation(value string) bool {
	return value == "suite" || value == "probe" || value == "logical_request" || value == "attempt"
}
func validTracePhase(value string) bool {
	return value == "start" || value == "end" || value == "validator"
}
func validTraceMethod(value string) bool {
	return value == "" || value == "GET" || value == "POST" || value == "PUT" || value == "PATCH" || value == "DELETE" || value == "HEAD"
}
func validTraceOutcome(value string) bool {
	return value == "" || value == "pass" || value == "fail" || value == "error" || value == "skip" || value == "cancelled" || value == "timeout"
}
func validTraceParent(parent, child string) bool {
	return parent == "suite" && child == "probe" || parent == "probe" && child == "logical_request" || parent == "logical_request" && child == "attempt"
}
func hostTraceOperation(value string) string {
	if value == "logical_request" {
		return "sdk.logical_request"
	}
	if value == "attempt" {
		return "sdk.http_attempt"
	}
	return value
}
func traceOutcome(event TraceEvent) string {
	if event.Outcome != "" {
		return event.Outcome
	}
	return "completed"
}

func (c *Collector) emit(ctx context.Context, event SuiteEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if event.SchemaVersion != ProtocolVersion || event.Type != "suite_result" || event.RunID != c.request.RunID || event.ExtensionID != c.manifest.Extension.ID {
		return fmt.Errorf("invalid extension suite envelope")
	}
	if event.Subject != c.request.Subject {
		return fmt.Errorf("suite %q reported the wrong subject", event.SuiteID)
	}
	if _, ok := c.planned[event.SuiteID]; !ok {
		return fmt.Errorf("extension reported unplanned suite %q", event.SuiteID)
	}
	if c.seen[event.SuiteID] {
		return fmt.Errorf("extension reported suite %q more than once", event.SuiteID)
	}
	for _, trace := range c.trace {
		if trace.suiteID == event.SuiteID {
			return fmt.Errorf("extension reported terminal suite %q with unfinished trace spans", event.SuiteID)
		}
	}
	if !validOutcome(event.Outcome) || event.DurationMS < 0 || strings.TrimSpace(event.Summary) == "" {
		return fmt.Errorf("suite %q reported an invalid outcome, duration, or summary", event.SuiteID)
	}
	if event.HTTP.StatusCode < 0 || event.HTTP.StatusCode > 599 || event.Outcome == result.ExtensionOutcomeRequestRejected && event.HTTP.StatusCode < 100 {
		return fmt.Errorf("suite %q reported invalid HTTP evidence", event.SuiteID)
	}
	if !validErrorKind(event.ErrorKind) {
		return fmt.Errorf("suite %q reported invalid error kind %q", event.SuiteID, event.ErrorKind)
	}
	if !validUsage(event.Usage) {
		return fmt.Errorf("suite %q reported invalid usage", event.SuiteID)
	}
	for _, signal := range event.Signals {
		if !stableID.MatchString(signal.Code) {
			return fmt.Errorf("suite %q reported invalid signal code %q", event.SuiteID, signal.Code)
		}
	}
	c.seen[event.SuiteID] = true
	binding, bound := c.profile.ExtensionBinding(event.ExtensionID, event.SuiteID)
	item := result.ExtensionSuiteResult{
		ExtensionID: event.ExtensionID,
		SuiteID:     event.SuiteID,
		Subject:     event.Subject,
		Outcome:     event.Outcome,
		Status:      deriveStatus(event, binding, bound, c.profile),
		DurationMS:  event.DurationMS,
		DurationNS:  event.DurationNS,
		Summary:     redact(event.Summary, c.secret),
		HTTP:        event.HTTP,
		Usage:       event.Usage,
		Signals:     append([]result.EvidenceSignal(nil), event.Signals...),
		Bound:       bound,
		Error:       redact(event.Error, c.secret),
		ErrorKind:   event.ErrorKind,
		ErrorCode:   diagnosticErrorCode(event.ErrorKind, event.Error),
	}
	item.HTTP.Snippet = redact(item.HTTP.Snippet, c.secret)
	result.RedactSecrets(&item, c.secret)
	if bound {
		item.RequirementID = binding.RequirementID
		item.RelatedRequirementIDs = append([]string(nil), binding.RelatedRequirementIDs...)
	}
	c.results = append(c.results, item)
	fields := logging.Fields{"extension": c.manifest.Extension.ID, "suite": event.SuiteID, "subject_kind": event.Subject.Kind, "subject_ref": diagnosticSubjectRef(event.Subject), "outcome": event.Outcome, "status": item.Status, "duration_ms": event.DurationMS, "http_status": event.HTTP.StatusCode, "error_kind": orDash(event.ErrorKind), "error_code": item.ErrorCode, "error_present": event.Error != "", "signals": diagnosticSignals(event.Signals), "usage_fields": diagnosticUsageFields(event.Usage)}
	logDiagnosticContext(ctx, c.logger, logging.Trace, "extsuite.suite.result_parsed", fields)
	logDiagnosticContext(ctx, c.logger, logging.Debug, "extsuite.suite.completed", logging.Fields{"extension": c.manifest.Extension.ID, "suite": event.SuiteID, "subject_ref": diagnosticSubjectRef(event.Subject), "status": item.Status, "outcome": event.Outcome, "reason": diagnosticReason(event), "duration_ms": event.DurationMS})
	return nil
}

func validUsage(usage result.Usage) bool {
	values := []int64{usage.InputTokens, usage.OutputTokens, usage.CacheCreationInputTokens, usage.CacheReadInputTokens, usage.ThinkingTokens, usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens, usage.ReasoningTokens}
	for _, value := range values {
		if value < 0 {
			return false
		}
	}
	for _, value := range usage.ProviderFields {
		if value < 0 {
			return false
		}
	}
	return true
}

func (c *Collector) Finish(provenance result.ExtensionProvenance, telemetry RunnerTelemetry, runErr error, started time.Time) result.ExtensionRun {
	problems := []string{}
	for eventID, trace := range c.trace {
		problems = append(problems, "extension trace did not finish "+eventID)
		if trace.op != nil {
			trace.op.End("error", logging.Fields{"error_present": true, "error_kind": "unfinished_extension_trace"})
		}
		delete(c.trace, eventID)
	}
	if runErr != nil {
		problems = append(problems, redact(runErr.Error(), c.secret))
	}
	runErrorKind := classifyRunError(runErr)
	for _, suiteID := range c.request.SuiteIDs {
		// Built-in SDK suites are instrumented by the host transport and do
		// not serialize cooperative IPC trace events. Only executable v2
		// runners are required to prove their lifecycle through the collector.
		if provenance.Source == "executable" && !c.traceUsedForSuite(suiteID) {
			problems = append(problems, "extension did not emit cooperative trace for planned suite "+suiteID)
		}
		if c.seen[suiteID] {
			continue
		}
		problems = append(problems, "extension did not report planned suite "+suiteID)
		binding, bound := c.profile.ExtensionBinding(c.manifest.Extension.ID, suiteID)
		missing := result.ExtensionSuiteResult{ExtensionID: c.manifest.Extension.ID, SuiteID: suiteID, Subject: c.request.Subject, Outcome: result.ExtensionOutcomeExecutionError, Status: result.StatusError, Summary: "Extension did not report the planned suite", Bound: bound, ErrorKind: runErrorKind, ErrorCode: diagnosticErrorCode(runErrorKind, ""), Signals: []result.EvidenceSignal{{Code: "extension.runner.missing_result"}}}
		if bound {
			missing.RequirementID = binding.RequirementID
			missing.RelatedRequirementIDs = append([]string(nil), binding.RelatedRequirementIDs...)
		}
		c.results = append(c.results, missing)
		logDiagnostic(c.logger, logging.Debug, "extsuite.suite.completed", logging.Fields{"extension": c.manifest.Extension.ID, "suite": suiteID, "subject_ref": diagnosticSubjectRef(c.request.Subject), "status": missing.Status, "outcome": missing.Outcome, "error_kind": missing.ErrorKind, "error_code": missing.ErrorCode, "reason": "missing_terminal_result", "duration_ms": 0})
	}
	sort.Slice(c.results, func(i, j int) bool { return c.planned[c.results[i].SuiteID] < c.planned[c.results[j].SuiteID] })
	if telemetry.TotalDuration <= 0 {
		telemetry.TotalDuration = time.Since(started)
	}
	pacingCompliance := "host_enforced"
	if provenance.Source == "executable" {
		pacingCompliance = "extension_required_unverified"
	}
	runner := result.ExtensionRunnerResult{Status: result.StatusPass, StartupDurationMS: telemetry.StartupDuration.Milliseconds(), TotalDurationMS: telemetry.TotalDuration.Milliseconds(), ExitCode: telemetry.ExitCode, PacingCompliance: pacingCompliance}
	if len(problems) > 0 {
		runner.Status = result.StatusError
		runner.Summary = "Extension runner did not complete cleanly"
		runner.Error = strings.Join(problems, "; ")
		runner.Output = telemetry.Stderr
	} else {
		runner.Summary = "Extension runner completed"
	}
	stderrBytes, stderrSHA256 := telemetry.StderrBytes, telemetry.StderrSHA256
	if stderrBytes == 0 && telemetry.Stderr != "" {
		// Test and built-in callers may only provide the already-sanitized
		// diagnostic. Executable processes always populate raw byte metadata.
		sum := sha256.Sum256([]byte(telemetry.Stderr))
		stderrBytes, stderrSHA256 = int64(len(telemetry.Stderr)), fmt.Sprintf("%x", sum[:])
	}
	terminal := logging.Fields{"extension": c.manifest.Extension.ID, "extension_id": c.manifest.Extension.ID, "suite_id": "-", "subject_ref": diagnosticSubjectRef(c.request.Subject), "outcome": runner.Status, "duration_ms": runner.TotalDurationMS, "http_status": 0, "error_kind": runErrorKind, "error_code": diagnosticErrorCode(runErrorKind, ""), "stdout_bytes": telemetry.StdoutBytes, "stdout_sha256": telemetry.StdoutSHA256, "stderr_bytes": int(stderrBytes), "stderr_sha256": stderrSHA256}
	logDiagnostic(c.logger, logging.Trace, "extsuite.runner.telemetry", logging.Fields{"extension": c.manifest.Extension.ID, "subject_ref": diagnosticSubjectRef(c.request.Subject), "status": runner.Status, "startup_ms": runner.StartupDurationMS, "total_ms": runner.TotalDurationMS, "exit_code": runner.ExitCode, "emitted_suites": len(c.seen), "planned_suites": len(c.request.SuiteIDs), "runner_error": runErr != nil})
	logDiagnostic(c.logger, logging.Debug, "extsuite.invocation.completed", terminal)
	return result.ExtensionRun{Provenance: provenance, TargetID: c.request.Target.ID, Subject: c.request.Subject, Runner: runner, Suites: append([]result.ExtensionSuiteResult(nil), c.results...)}
}

func (c *Collector) traceUsedForSuite(suiteID string) bool {
	return c.traceSuites[suiteID] > 0
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func diagnosticReason(event SuiteEvent) string {
	if event.ErrorKind != "" {
		return event.ErrorKind
	}
	if event.HTTP.StatusCode != 0 {
		return fmt.Sprintf("http_%d", event.HTTP.StatusCode)
	}
	if event.Error != "" {
		return "extension_error"
	}
	return string(event.Outcome)
}

func validOutcome(outcome result.ExtensionOutcome) bool {
	switch outcome {
	case result.ExtensionOutcomePass, result.ExtensionOutcomeAssertionFailed, result.ExtensionOutcomeRequestRejected, result.ExtensionOutcomeExecutionError, result.ExtensionOutcomeSkip:
		return true
	default:
		return false
	}
}

func deriveStatus(event SuiteEvent, binding profile.ExtensionBinding, bound bool, p profile.Profile) result.Status {
	switch event.Outcome {
	case result.ExtensionOutcomePass:
		return result.StatusPass
	case result.ExtensionOutcomeAssertionFailed:
		return result.StatusFail
	case result.ExtensionOutcomeExecutionError:
		return result.StatusError
	case result.ExtensionOutcomeSkip:
		return result.StatusSkip
	case result.ExtensionOutcomeRequestRejected:
		status := event.HTTP.StatusCode
		if status == 408 || status == 429 || status >= 500 || status == 0 {
			return result.StatusError
		}
		if bound && event.ErrorKind == "capability_rejection" && (status == 400 || status == 404 || status == 422) && !p.Probe(binding.RequirementID).Required {
			return result.StatusUnsupported
		}
		return result.StatusFail
	default:
		return result.StatusError
	}
}

func redact(value, secret string) string {
	return sanitizeDiagnostic([]byte(value), secret)
}
