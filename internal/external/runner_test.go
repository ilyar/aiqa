package external

import (
	"context"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/result"
)

func TestExplicitExternalModeFailsWhenToolIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, requested := range []string{"binary", "docker"} {
		t.Run(requested, func(t *testing.T) {
			mode, _, reason := resolveMode(context.Background(), requested, "example.invalid/test:latest")
			if mode != "error" {
				t.Fatalf("mode=%q reason=%q, want error", mode, reason)
			}
		})
	}
}

func TestAutoExternalModeSkipsWhenNothingIsInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	mode, _, _ := resolveMode(context.Background(), "auto", "example.invalid/test:latest")
	if mode != "off" {
		t.Fatalf("mode=%q, want off", mode)
	}
}

func TestParseSuiteResultsUsesSuiteDurations(t *testing.T) {
	output := `==> running suite "models"
    FAIL (504ms): models: model entry missing created
==> running suite "chat_completions"
    PASS (1.283s)
`
	got := parseSuiteResults(output, "test-model")
	if len(got) != 2 {
		t.Fatalf("got %d suite results, want 2: %#v", len(got), got)
	}
	if got[0].Tool != toolName || got[0].Suite != "models" || got[0].Status != result.StatusFail || got[0].DurationMS != 504 {
		t.Fatalf("unexpected first suite result: %+v", got[0])
	}
	if got[1].Suite != "chat_completions" || got[1].Status != result.StatusPass || got[1].DurationMS != (time.Second+283*time.Millisecond).Milliseconds() {
		t.Fatalf("unexpected second suite result: %+v", got[1])
	}
}
