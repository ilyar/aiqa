package suites

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/telemetry"
)

func TestEnvironmentLogfForwardsCategory(t *testing.T) {
	var gotLevel logging.Level
	var gotMessage string
	env := Environment{Logf: func(level logging.Level, format string, args ...any) {
		gotLevel = level
		gotMessage = format
	}}
	env.logf(logging.Trace, "planning probe")
	if gotLevel != logging.Trace || gotMessage != "planning probe" {
		t.Fatalf("log callback = (%q, %q), want (%q, %q)", gotLevel, gotMessage, logging.Trace, "planning probe")
	}
}

func TestRunTestProbeEstablishesNamedContextBeforeWorkAndEndsAfter(t *testing.T) {
	logger := logging.New(4)
	env := Environment{TargetID: "target-a", Telemetry: telemetry.New(logger)}
	var captured context.Context
	got := env.runTestProbe(context.Background(), "model-a", "compat", "chat.basic", func(ctx context.Context) result.TestResult {
		captured = ctx
		return result.TestResult{Status: result.StatusPass, HTTP: result.HTTPObservation{StatusCode: 200}, DurationMS: 7}
	})
	if got.Status != result.StatusPass || captured == nil {
		t.Fatalf("probe work did not run: %+v", got)
	}
	if got := logging.FieldsFromContext(captured)["probe"]; got != "chat.basic" {
		t.Fatalf("probe context=%q, want chat.basic", got)
	}
	if got := logging.FieldsFromContext(captured)["probe_id"]; got != "aiqa.native.chat.basic" {
		t.Fatalf("probe_id=%q", got)
	}
	lines := logger.Lines()
	if len(lines) != 3 {
		t.Fatalf("events=%d, want start/validator/end", len(lines))
	}
	var start, validator, end map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &start); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &validator); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[2]), &end); err != nil {
		t.Fatal(err)
	}
	if start["event"] != "probe.start" || validator["event"] != "probe.validator" || end["event"] != "probe.end" || end["outcome"] != "pass" {
		t.Fatalf("unexpected lifecycle: start=%v validator=%v end=%v", start, validator, end)
	}
	if start["span_id"] == "" || end["span_id"] != start["span_id"] {
		t.Fatalf("span correlation missing: start=%v end=%v", start, end)
	}
}

func TestRunTestsProbeRecordsDerivedResultLinkage(t *testing.T) {
	logger := logging.New(4)
	env := Environment{TargetID: "target-a", Telemetry: telemetry.New(logger)}
	env.runTestsProbe(context.Background(), "model-a", "compat", "chat.basic", func(context.Context) []result.TestResult {
		return []result.TestResult{
			{ID: "chat.basic", Status: result.StatusPass},
			{ID: "chat.schema", DerivedFrom: "chat.basic", Status: result.StatusPass},
		}
	})
	for _, line := range logger.Lines() {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event["event"] == "probe.validator" && event["result_id"] == "chat.schema" {
			if event["derived_from"] != "chat.basic" {
				t.Fatalf("derived linkage = %v, want chat.basic", event["derived_from"])
			}
			return
		}
	}
	t.Fatal("derived validator event not found")
}
