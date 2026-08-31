package suites

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

func TestDiscoverySchemaRequiresModelCreated(t *testing.T) {
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	resp := api.Response{
		StatusCode:  200,
		ContentType: "application/json",
		JSON: map[string]any{
			"object": "list",
			"data":   []any{map[string]any{"id": "test-model", "object": "model"}},
		},
	}
	tests := DiscoveryTests(p, resp, []api.Model{{ID: "test-model"}}, nil)
	if got := tests[1]; got.Status != result.StatusFail || !strings.Contains(got.Observation, "created is missing in 1/1") {
		t.Fatalf("missing model created must fail schema validation: %+v", got)
	}
}

func TestForcedToolChoiceDoesNotConflateParallelToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, present := body["parallel_tool_calls"]; present {
			http.Error(w, `{"error":{"message":"parallel_tool_calls unsupported"}}`, http.StatusBadRequest)
			return
		}
		choice, ok := body["tool_choice"].(map[string]any)
		if !ok || choice["type"] != "function" {
			t.Fatalf("expected forced function tool_choice, got %#v", body["tool_choice"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_status","arguments":"{\"resource_id\":\"resource-alpha\"}"}}]}}]}`))
	}))
	defer server.Close()

	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	client, err := api.NewClient(server.URL, "test-key", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	env := Environment{Client: client, Config: config.Config{Timeout: time.Second}, Profile: p}
	got, _ := probeForcedTool(context.Background(), env, "test-model")
	if got.Status != result.StatusPass {
		t.Fatalf("forced tool choice should pass independently of parallel_tool_calls: %+v", got)
	}
}

func TestStreamingRateLimitIsInconclusive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down"}}`))
	}))
	defer server.Close()

	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	client, err := api.NewClient(server.URL, "test-key", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	env := Environment{Client: client, Config: config.Config{Timeout: time.Second}, Profile: p}
	got := probeStreaming(context.Background(), env, "test-model", false)
	if got.Status != result.StatusError {
		t.Fatalf("status=%s, want error/inconclusive: %+v", got.Status, got)
	}
	if !strings.Contains(got.Summary, "rate limited") {
		t.Fatalf("summary=%q, want rate-limit explanation", got.Summary)
	}
}

func TestErrorProbeProviderFailuresAreInconclusive(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			test := evaluateErrorProbe(
				result.TestResult{Metrics: map[string]any{}},
				api.Response{StatusCode: status},
				nil,
				[]int{400},
				"malformed JSON",
			)
			if test.Status != result.StatusError {
				t.Fatalf("status=%s, want error/inconclusive: %+v", test.Status, test)
			}
		})
	}
}

func TestProvider5xxIsInconclusiveAcrossDiscoveryAndChat(t *testing.T) {
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	resp := api.Response{StatusCode: http.StatusBadGateway}
	discovery := DiscoveryTests(p, resp, nil, nil)
	if discovery[0].Status != result.StatusError {
		t.Fatalf("discovery status=%s, want error", discovery[0].Status)
	}
	chat := evaluateBasic(p, resp, nil)
	if chat.Status != result.StatusError {
		t.Fatalf("chat status=%s, want error", chat.Status)
	}
}
