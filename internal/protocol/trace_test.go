package protocol

import (
	"context"
	"strings"
	"testing"

	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/telemetry"
)

func TestLogTestResultsEmitsTerminalAndSafeDiagnostics(t *testing.T) {
	logger := logging.New(4)
	LogTestResults(func(level logging.Level, format string, args ...any) { logger.Logf(level, format, args...) }, "google", "gemini-test", []result.TestResult{{
		ID:          "google.interactions.schema",
		Suite:       "compat",
		Category:    "schema",
		Required:    true,
		Status:      result.StatusFail,
		DurationMS:  42,
		Summary:     "Interaction response schema is invalid",
		Observation: "id is missing",
		HTTP:        result.HTTPObservation{StatusCode: 200, RequestID: "req_123"},
		Metrics:     map[string]any{"sse_event_types": []string{"interaction.created", "step.start", "[DONE]"}},
	}})
	joined := strings.Join(logger.Lines(), "\n")
	for _, want := range []string{
		"[target=google model=gemini-test suite=compat probe=google.interactions.schema operation_id=] terminal status=fail",
		"duration_ms=42 http_status=200",
		"[target=google model=gemini-test suite=compat probe=google.interactions.schema] validation",
		"diagnostics=",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("log does not contain %q:\n%s", want, joined)
		}
	}
	for _, forbidden := range []string{"req_123", "Interaction response schema is invalid", "interaction.created", "step.start"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("legacy diagnostic log leaked provider-controlled value %q: %s", forbidden, joined)
		}
	}
}

func TestLogTestResultsWithTelemetryEmitsSafeSummaryWithoutObservation(t *testing.T) {
	logger := logging.New(4)
	provider := telemetry.New(logger)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	LogTestResultsWithTelemetry(logging.WithLogger(context.Background(), logger), provider, "openai", "model", []result.TestResult{{
		ID: "chat.stream", Suite: "compat", Status: result.StatusFail,
		Observation: "private model output", HTTP: result.HTTPObservation{StatusCode: 422},
		Metrics: map[string]any{"sse_event_types": []string{"response.delta", "[DONE]"}},
	}})
	joined := strings.Join(logger.Lines(), "\n")
	for _, want := range []string{`"event":"result.summary"`, `"status":"fail"`, `"http_status":422`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("telemetry is missing %s: %s", want, joined)
		}
	}
	if strings.Contains(joined, "private model output") {
		t.Fatalf("telemetry leaked observation: %s", joined)
	}
}

func TestLogTestResultsRedactsGeneratedObservation(t *testing.T) {
	logger := logging.New(4)
	LogTestResults(func(level logging.Level, format string, args ...any) { logger.Logf(level, format, args...) }, "openai", "test", []result.TestResult{{
		ID: "chat.basic", Suite: "compat", Status: result.StatusFail,
		Observation: "assistant content: private generated response",
	}})
	joined := strings.Join(logger.Lines(), "\n")
	if strings.Contains(joined, "private generated response") || !strings.Contains(joined, "diagnostics=present_sha256_12=") {
		t.Fatalf("unsafe diagnostic logging: %s", joined)
	}
}
