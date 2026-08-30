package external

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/profile"
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

func TestDockerRunArgsUsesHostNetworkForLoopbackEndpoint(t *testing.T) {
	args, baseURL := dockerRunArgs(config.Config{
		BaseURL:       "http://localhost:8080/v1",
		ExternalImage: "example.invalid/checker:latest",
	})
	if baseURL != "http://localhost:8080/v1" {
		t.Fatalf("base URL changed: %q", baseURL)
	}
	if runtime.GOOS == "linux" && !containsArgs(args, "--network", "host") {
		t.Fatalf("loopback endpoint requires host networking: %#v", args)
	}
	if !containsArgs(args, "-e", "OPENAI_BASE_URL") {
		t.Fatalf("missing base URL environment forwarding: %#v", args)
	}
}

func TestDockerRunArgsLeavesRemoteEndpointOnDefaultNetwork(t *testing.T) {
	args, _ := dockerRunArgs(config.Config{
		BaseURL:       "https://example.invalid/v1",
		ExternalImage: "example.invalid/checker:latest",
	})
	if containsArgs(args, "--network", "host") {
		t.Fatalf("remote endpoint must not use host networking: %#v", args)
	}
}

func containsArgs(args []string, pair ...string) bool {
	for i := 0; i+len(pair) <= len(args); i++ {
		matched := true
		for j := range pair {
			if args[i+j] != pair[j] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
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

func TestNormalizeAgainstProfileTreatsOptionalModelsGet404AsUnsupported(t *testing.T) {
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	results := normalizeAgainstProfile([]result.ExternalResult{{
		Suite:   "models_get",
		Status:  result.StatusFail,
		Summary: `models get request failed: GET "http://example.test/v1/models/model": 404 Not Found`,
	}}, p)
	if results[0].Status != result.StatusUnsupported {
		t.Fatalf("status=%q, want unsupported", results[0].Status)
	}
	if !containsHTTPStatus(results[0].Summary, 404) {
		t.Fatalf("normalized summary lost HTTP status: %q", results[0].Summary)
	}
}

func TestNormalizeAgainstProfileRetainsModelsGetFailuresOtherThan404(t *testing.T) {
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	results := normalizeAgainstProfile([]result.ExternalResult{{
		Suite:   "models_get",
		Status:  result.StatusFail,
		Summary: `models get request failed: 500 Internal Server Error`,
	}}, p)
	if results[0].Status != result.StatusFail {
		t.Fatalf("status=%q, want fail", results[0].Status)
	}
}
