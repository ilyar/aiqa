package protocol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/telemetry"
)

// AggregateTestOutcome returns one lifecycle outcome for a composite probe.
// A probe may materialize several report results from one request; its terminal
// state must describe all of them, never whichever result happened to be first.
// StatusError is strongest because it means execution itself was not completed;
// then a conformance failure, then warning/unsupported/skip, and finally pass.
func AggregateTestOutcome(tests []result.TestResult) string {
	if len(tests) == 0 {
		return "completed"
	}
	best := result.StatusPass
	bestRank := -1
	for _, test := range tests {
		rank := testStatusRank(test.Status)
		if rank > bestRank {
			best, bestRank = test.Status, rank
		}
	}
	return string(best)
}

func testStatusRank(status result.Status) int {
	switch status {
	case result.StatusError:
		return 5
	case result.StatusFail:
		return 4
	case result.StatusWarn:
		return 3
	case result.StatusUnsupported:
		return 2
	case result.StatusSkip:
		return 1
	case result.StatusPass:
		return 0
	default:
		// An unknown status is evidence that the lifecycle cannot be claimed
		// successful. Keep it above pass without inventing a public status.
		return 3
	}
}

// LogTestResults emits a safe terminal record for every native probe. It is
// intentionally limited to identifiers and validator diagnostics: prompts,
// provider output, tool arguments, and response snippets must not enter the
// private verbose log.
func LogTestResults(logf func(logging.Level, string, ...any), target, model string, tests []result.TestResult) {
	if logf == nil {
		return
	}
	for _, test := range tests {
		logf(logging.Debug,
			"[target=%s model=%s suite=%s probe=%s operation_id=%s] terminal status=%s required=%t duration_ms=%d http_status=%d error_present=%t",
			target, model, test.Suite, test.ID, test.HTTP.TraceID, test.Status, test.Required, test.DurationMS, test.HTTP.StatusCode, test.Error != "",
		)
		if test.Status == result.StatusFail || test.Status == result.StatusError || test.Status == result.StatusWarn {
			logf(logging.Trace,
				"[target=%s model=%s suite=%s probe=%s] validation category=%q signals=%s metric_keys=%s diagnostics=%s",
				target, model, test.Suite, test.ID, test.Category, signalCodes(test.Signals), metricKeys(test.Metrics), safeDiagnostics(test),
			)
		}
	}
}

// LogTestResultsWithTelemetry emits post-execution result metadata. It must
// not create another `probe` span: native and SDK runners establish that span
// before issuing HTTP, and a second same-ID probe would corrupt reconstruction.
func LogTestResultsWithTelemetry(ctx context.Context, provider *telemetry.Provider, target, model string, tests []result.TestResult) {
	if provider == nil {
		return
	}
	logger := logging.FromContext(ctx)
	if logger == nil {
		return
	}
	for _, test := range tests {
		probeID := test.ProbeID
		if probeID == "" {
			probeID = test.ID
		}
		probeCtx := logging.Scope(ctx, target, model, test.Suite, probeID, "")
		fields := logging.Fields{"status": string(test.Status), "http_status": test.HTTP.StatusCode, "duration_ms": test.DurationMS, "error_present": test.Error != ""}
		if test.Error != "" {
			fields["error_kind"] = "probe_error"
		}
		// This is a terminal summary only. Validators are emitted by the
		// currently active report probe, before probe.end; creating a post-hoc
		// result span would manufacture an unowned lifecycle branch.
		logger.LogContext(probeCtx, logging.Debug, "result.summary", fields)
	}
}

func signalCodes(signals []result.EvidenceSignal) string {
	if len(signals) == 0 {
		return "none"
	}
	values := make([]string, 0, len(signals))
	for _, signal := range signals {
		values = append(values, signal.Code)
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}

func metricKeys(metrics map[string]any) string {
	if len(metrics) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(metrics))
	for key := range metrics {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func safeDiagnostics(test result.TestResult) string {
	// Observation may contain generated text, error bodies, or tool arguments.
	// Do not use heuristic allowlists here: the terminal record only needs a
	// stable correlation token, while the report retains the user-authorized
	// evidence. This prevents a provider response from entering verbose logs.
	observation := strings.TrimSpace(test.Observation)
	if observation == "" {
		return "none"
	}
	sum := sha256.Sum256([]byte(observation))
	return "present_sha256_12=" + hex.EncodeToString(sum[:6])
}
