package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/traceverify"
)

// This is the failure boundary that previously turned an ordinary provider
// authorization rejection into a false forensic claim that the selected SDK
// executor was absent. The server is loopback-only: it never contacts a real
// provider or uses a real credential.
func TestAuthorizationBlockedTargetClosesSelectedSDKTraceCoverage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/anthropic/v1/models" {
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"authentication_error"}}`))
	}))
	defer server.Close()
	withTempWorkingDirectory(t)
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL+"/anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "local-test-key")
	cfg, err := config.Parse([]string{"test=compat,sdk", "--verbose=4", "--timeout=2", "--name=blocked-sdk", "--format=json"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err == nil {
		t.Fatal("Run unexpectedly succeeded after inventory authorization failure")
	}
	trace, err := os.ReadFile("blocked-sdk.log")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(trace, []byte("local-test-key")) {
		t.Fatal("private trace leaked loopback credential")
	}
	data, err := os.ReadFile("blocked-sdk.trace-verdict.json")
	if err != nil {
		t.Fatal(err)
	}
	var verdict traceverify.Verdict
	if err := json.Unmarshal(data, &verdict); err != nil {
		t.Fatal(err)
	}
	if verdict.Status != traceverify.Pass {
		t.Fatalf("blocked prerequisite must retain a passable trace, got %+v", verdict)
	}
	if offline := traceverify.Verify(bytes.NewReader(trace), "local-test-key"); !traceVerdictsEqual(offline, verdict) {
		t.Fatalf("offline verdict differs: offline=%+v saved=%+v", offline, verdict)
	}
	for _, item := range verdict.Coverage {
		if item.Target == "anthropic" && item.Protocol == "anthropic" && item.Executor == "sdk" {
			if item.Status != "blocked" || item.Count != 0 {
				t.Fatalf("sdk coverage=%+v, want blocked with no invented execution", item)
			}
			return
		}
	}
	t.Fatalf("SDK target coverage absent: %+v", verdict.Coverage)
}

func TestPrepareBarrierBlocksSelectedSDKForEveryTarget(t *testing.T) {
	openAI := newMockOpenAI(t)
	defer openAI.Close()
	deniedAnthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/anthropic/v1/models" {
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"authentication_error"}}`))
	}))
	defer deniedAnthropic.Close()
	withTempWorkingDirectory(t)
	t.Setenv("OPENAI_BASE_URL", openAI.URL+"/v1")
	t.Setenv("OPENAI_API_KEY", "openai-local-key")
	t.Setenv("ANTHROPIC_BASE_URL", deniedAnthropic.URL+"/anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-local-key")
	cfg, err := config.Parse([]string{"test=compat,sdk", "--verbose=4", "--timeout=2", "--name=prepare-barrier", "--format=json"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), cfg, BuildInfo{Version: "test"}); err == nil {
		t.Fatal("Run unexpectedly succeeded with one rejected inventory")
	}
	trace, err := os.ReadFile("prepare-barrier.log")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("prepare-barrier.trace-verdict.json")
	if err != nil {
		t.Fatal(err)
	}
	var verdict traceverify.Verdict
	if err := json.Unmarshal(data, &verdict); err != nil {
		t.Fatal(err)
	}
	if verdict.Status != traceverify.Pass {
		t.Fatalf("global barrier must close every selected branch, got %+v", verdict)
	}
	if offline := traceverify.Verify(bytes.NewReader(trace), "openai-local-key", "anthropic-local-key"); !traceVerdictsEqual(offline, verdict) {
		t.Fatalf("offline verdict differs: offline=%+v saved=%+v", offline, verdict)
	}
	for _, target := range []struct{ id, protocol string }{{"openai", "openai"}, {"anthropic", "anthropic"}} {
		found := false
		for _, item := range verdict.Coverage {
			if item.Target == target.id && item.Protocol == target.protocol && item.Executor == "sdk" {
				found = true
				if item.Status != "blocked" || item.Count != 0 {
					t.Fatalf("target %s SDK coverage=%+v, want blocked without invented execution", target.id, item)
				}
			}
		}
		if !found {
			t.Fatalf("target %s lacks SDK coverage: %+v", target.id, verdict.Coverage)
		}
	}
}

func TestMixedSDKAndExecutableSelectionSeparatesTraceCoverage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer server.Close()
	withTempWorkingDirectory(t)
	// DiscoverExecutables also searches PATH. Keep it empty so this run has
	// only the built-in SDK invocation even though ext-suite is selected.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("OPENAI_BASE_URL", server.URL+"/v1")
	t.Setenv("OPENAI_API_KEY", "local-test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	cfg, err := config.Parse([]string{"test=sdk,ext-suite", "--verbose=4", "--timeout=2", "--name=no-sdk-work", "--format=json"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), cfg, BuildInfo{Version: "test"}); err != nil {
		t.Fatalf("empty model selection must remain a reportable run: %v", err)
	}
	trace, err := os.ReadFile("no-sdk-work.log")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("no-sdk-work.trace-verdict.json")
	if err != nil {
		t.Fatal(err)
	}
	var verdict traceverify.Verdict
	if err := json.Unmarshal(data, &verdict); err != nil {
		t.Fatal(err)
	}
	if verdict.Status != traceverify.Pass {
		t.Fatalf("provider-level SDK selection must be trace-passable, got %+v", verdict)
	}
	if offline := traceverify.Verify(bytes.NewReader(trace), "local-test-key"); !traceVerdictsEqual(offline, verdict) {
		t.Fatalf("offline verdict differs: offline=%+v saved=%+v", offline, verdict)
	}
	coverage := map[string]traceverify.Coverage{}
	for _, item := range verdict.Coverage {
		if item.Target == "openai" && item.Protocol == "openai" {
			coverage[item.Executor] = item
		}
	}
	// A zero-model inventory is not a no-work SDK branch: its provider-level
	// models.list suite is frozen and executes. In contrast, ext-suite has no
	// executable selected, so it closes independently as no_work.
	if sdk := coverage["sdk"]; sdk.Status != "complete" || sdk.Count == 0 {
		t.Fatalf("sdk coverage=%+v, want complete provider lifecycle; all=%+v", sdk, verdict.Coverage)
	}
	if ext := coverage["extension"]; ext.Status != "no_work" || ext.Count != 0 {
		t.Fatalf("extension coverage=%+v, want no_work; all=%+v", ext, verdict.Coverage)
	}
}

func traceVerdictsEqual(left, right traceverify.Verdict) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}
