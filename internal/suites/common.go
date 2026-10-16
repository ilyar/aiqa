package suites

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/probe"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/progress"
	protocolpkg "github.com/ilyar/aiqa/internal/protocol"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/telemetry"
)

type Environment struct {
	TargetID  string
	Client    *api.Client
	Config    config.Config
	Profile   profile.Profile
	Logf      func(logging.Level, string, ...any)
	Probes    *probe.Executor
	Telemetry *telemetry.Provider
	// Progress receives no probe completions directly: telemetry remains the
	// single lifecycle source. PlanRevision is for work whose logical units
	// become known only immediately before a suite starts.
	Progress     progress.Sink
	PlanRevision func(int)
}

func (e Environment) revisePlan(units int) {
	if units > 0 && e.Progress != nil && e.PlanRevision != nil {
		e.PlanRevision(units)
	}
}

func (e Environment) traceTests(ctx context.Context, model string, tests []result.TestResult) {
	if e.Telemetry != nil {
		protocolpkg.LogTestResultsWithTelemetry(ctx, e.Telemetry, e.TargetID, model, tests)
		return
	}
	protocolpkg.LogTestResults(e.Logf, e.TargetID, model, tests)
}

func (e Environment) startSuite(ctx context.Context, suite string, fields logging.Fields) (context.Context, *telemetry.Operation) {
	ctx = logging.Scope(ctx, e.TargetID, "", suite, "", "")
	if e.Telemetry == nil {
		return ctx, nil
	}
	return e.Telemetry.Start(ctx, "suite", fields)
}

func (e Environment) startProvider(ctx context.Context) (context.Context, *telemetry.Operation) {
	ctx = logging.Scope(ctx, e.TargetID, "", "", "", "")
	if e.Telemetry == nil {
		return ctx, nil
	}
	return e.Telemetry.Start(ctx, "provider", logging.Fields{"target": e.TargetID})
}

// startProbe establishes the concrete probe boundary before any request is
// issued.  The transport deliberately receives this context so its logical
// request and attempt spans are children of the report-visible probe rather
// than of a generic transport fallback.
func (e Environment) startProbe(ctx context.Context, model, suite, probeID string) (context.Context, *telemetry.Operation) {
	ctx = logging.Scope(ctx, e.TargetID, model, suite, probeID, "")
	if e.Telemetry == nil {
		return ctx, nil
	}
	return e.Telemetry.Start(ctx, "probe", logging.Fields{
		"probe":    probeID,
		"probe_id": "aiqa.native." + probeID,
	})
}

func (e Environment) runTestProbe(ctx context.Context, model, suite, probeID string, fn func(context.Context) result.TestResult) result.TestResult {
	probeCtx, op := e.startProbe(ctx, model, suite, probeID)
	test := fn(probeCtx)
	fields := logging.Fields{
		"probe":         probeID,
		"status":        string(test.Status),
		"http_status":   test.HTTP.StatusCode,
		"duration_ms":   test.DurationMS,
		"error_present": test.Error != "",
	}
	if op != nil {
		// Validation is part of the concrete test lifecycle.  Do not defer it
		// to a post-execution result record, which would lose its ownership.
		op.Event(logging.Trace, "validator", fields)
	}
	endOperation(op, string(test.Status), fields)
	return test
}

func (e Environment) runTestsProbe(ctx context.Context, model, suite, probeID string, fn func(context.Context) []result.TestResult) []result.TestResult {
	probeCtx, op := e.startProbe(ctx, model, suite, probeID)
	tests := fn(probeCtx)
	outcome := protocolpkg.AggregateTestOutcome(tests)
	fields := logging.Fields{"probe": probeID, "test_count": len(tests), "status": outcome}
	if op != nil {
		for _, test := range tests {
			fields := logging.Fields{
				"probe": probeID, "result_id": test.ID, "status": string(test.Status),
				"http_status": test.HTTP.StatusCode, "duration_ms": test.DurationMS,
				"error_present": test.Error != "",
			}
			if test.DerivedFrom != "" {
				fields["derived_from"] = test.DerivedFrom
			}
			op.Event(logging.Trace, "validator", fields)
		}
	}
	endOperation(op, outcome, fields)
	return tests
}

func (e Environment) runParameterProbe(ctx context.Context, model, probeID string, fn func(context.Context) result.ParameterResult) result.ParameterResult {
	probeCtx, op := e.startProbe(ctx, model, "model-param", probeID)
	parameter := fn(probeCtx)
	fields := logging.Fields{
		"probe":       probeID,
		"status":      string(parameter.Status),
		"http_status": parameter.HTTP.StatusCode,
		"duration_ms": parameter.DurationMS,
	}
	if op != nil {
		op.Event(logging.Trace, "validator", fields)
	}
	endOperation(op, string(parameter.Status), fields)
	return parameter
}

func endOperation(op *telemetry.Operation, outcome string, fields logging.Fields) {
	if op != nil {
		op.End(outcome, fields)
	}
}

func (e Environment) logf(level logging.Level, format string, args ...any) {
	if e.Logf != nil {
		e.Logf(level, format, args...)
	}
}

func (e Environment) probeContext(parent context.Context) (context.Context, context.CancelFunc) {
	return api.WithRequestTimeout(parent, e.Config.Timeout)
}

func testBase(p profile.Profile, id, suite string) result.TestResult {
	spec := p.Probe(id)
	return result.TestResult{
		ID:            id,
		RequirementID: id,
		ProbeID:       "aiqa.native." + id,
		Name:          spec.Name,
		Suite:         suite,
		Category:      spec.Category,
		Required:      spec.Required,
		Expected:      spec.Description,
		Metrics:       map[string]any{},
	}
}

func httpObservation(resp api.Response) result.HTTPObservation {
	headers := map[string]string{}
	for _, name := range []string{"server", "content-encoding", "openai-processing-ms", "x-ratelimit-limit-requests", "x-ratelimit-remaining-requests"} {
		for key, values := range resp.Header {
			if strings.EqualFold(key, name) && len(values) > 0 {
				headers[strings.ToLower(name)] = values[0]
			}
		}
	}
	if len(headers) == 0 {
		headers = nil
	}
	return result.HTTPObservation{
		StatusCode:  resp.StatusCode,
		ContentType: resp.ContentType,
		RequestID:   resp.RequestID,
		Headers:     headers,
		Snippet:     sanitizeSnippet(resp.Snippet(1200)),
		TraceID:     resp.TraceID,
	}
}

func streamObservation(resp api.StreamResponse) result.HTTPObservation {
	return result.HTTPObservation{
		StatusCode:  resp.StatusCode,
		ContentType: resp.ContentType,
		RequestID:   resp.RequestID,
		Snippet:     sanitizeSnippet(truncate(resp.RawSnippet, 1200)),
		TraceID:     resp.TraceID,
	}
}

func sanitizeSnippet(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return ""
	}
	var compact any
	if json.Unmarshal([]byte(s), &compact) == nil {
		if b, err := json.Marshal(compact); err == nil {
			s = string(b)
		}
	}
	return truncate(s, 1200)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return truncate(err.Error(), 1000)
}

// applyWorkLimit records a local execution guard before interpreting any HTTP
// response. A 2xx response may have been received before bounded SSE parsing
// or the run-wide transport budget stopped work; that is not provider evidence.
func applyWorkLimit(t *result.TestResult, err error) bool {
	if !errors.Is(err, api.ErrWorkLimit) {
		return false
	}
	t.Status = result.StatusError
	t.Summary = "local execution work limit reached; compatibility outcome is inconclusive"
	t.Error = errorText(err)
	t.Signals = append(t.Signals, result.EvidenceSignal{Code: "execution.work_limit"})
	return true
}

func statusForOptionalHTTP(resp api.Response, err error) result.Status {
	if errors.Is(err, api.ErrWorkLimit) {
		return result.StatusError
	}
	if err != nil && resp.StatusCode == 0 {
		return result.StatusError
	}
	if resp.Is2xx() {
		return result.StatusPass
	}
	switch {
	case resp.StatusCode == 400, resp.StatusCode == 404, resp.StatusCode == 405, resp.StatusCode == 409, resp.StatusCode == 415, resp.StatusCode == 422, resp.StatusCode == 501:
		return result.StatusUnsupported
	case resp.StatusCode == 408, resp.StatusCode == 429, resp.StatusCode >= 500:
		return result.StatusError
	default:
		return result.StatusFail
	}
}

func providerFailureStatusCode(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
}

func providerFailureSummary(subject string, status int) string {
	switch status {
	case http.StatusRequestTimeout:
		return subject + " timed out at the provider; compatibility outcome is inconclusive"
	case http.StatusTooManyRequests:
		return subject + " remained rate limited after adaptive retries; compatibility outcome is inconclusive"
	default:
		return fmt.Sprintf("%s returned provider HTTP %d; compatibility outcome is inconclusive", subject, status)
	}
}

func boolWord(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func metricString(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return d.Round(time.Millisecond).String()
}
