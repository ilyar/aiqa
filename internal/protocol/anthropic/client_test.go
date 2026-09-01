package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientBuildsAnthropicPathsAndHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/anthropic/v1/models/provider%2Fmodel" {
			t.Errorf("escaped path=%q", r.URL.EscapedPath())
		}
		if r.Header.Get("x-api-key") != "key" || r.Header.Get("anthropic-version") != StableAPIVersion {
			t.Errorf("missing Anthropic headers: %v", r.Header)
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "provider/model", "type": "model", "display_name": "Model", "created_at": "2026-09-01T00:00:00Z"})
	}))
	defer server.Close()
	client, err := NewClient(server.URL+"/anthropic/v1/", "key", StableAPIVersion, "aiqa/test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.RetrieveModel(context.Background(), "provider/model")
	if err != nil || !resp.Is2xx() {
		t.Fatalf("retrieve model: status=%d err=%v", resp.StatusCode, err)
	}
}

func TestNormalizeBaseURLRemovesOnlyTrailingV1(t *testing.T) {
	for input, want := range map[string]string{
		"http://127.0.0.1/anthropic":     "http://127.0.0.1/anthropic",
		"http://127.0.0.1/anthropic/":    "http://127.0.0.1/anthropic",
		"http://127.0.0.1/anthropic/v1":  "http://127.0.0.1/anthropic",
		"http://127.0.0.1/anthropic/v1/": "http://127.0.0.1/anthropic",
	} {
		if got := NormalizeBaseURL(input); got != want {
			t.Errorf("NormalizeBaseURL(%q)=%q, want %q", input, got, want)
		}
	}
}
