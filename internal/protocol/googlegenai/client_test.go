package googlegenai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientUsesVersionPrefixAndHeaderAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "test-key" {
			t.Errorf("x-goog-api-key=%q", r.Header.Get("x-goog-api-key"))
		}
		if r.URL.Path != "/gateway/v1beta/models" {
			t.Errorf("path=%q", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("unexpected query=%q", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]any{"name": "models/gemini-test"}}})
	}))
	defer server.Close()
	client, err := NewClient(server.URL+"/gateway", "test-key", "v1beta", "test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	models, _, err := client.ListModels(context.Background())
	if err != nil || len(models) != 1 || models[0].ID != "gemini-test" {
		t.Fatalf("list models: models=%v err=%v", models, err)
	}
}

func TestClientDoesNotDuplicateVersionSuffix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gateway/v1beta/models" {
			t.Errorf("path=%q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]any{"name": "models/gemini-test"}}})
	}))
	defer server.Close()
	client, err := NewClient(server.URL+"/gateway/v1beta/", "test-key", "v1beta", "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClientStreamingPreservesQueryAndUsesSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/interactions" || r.URL.Query().Get("alt") != "sse" {
			t.Errorf("stream URL=%s", r.URL.String())
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: interaction.created\ndata: {\"id\":\"i1\"}\n\n")
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "test-key", "v1beta", "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.StreamInteraction(context.Background(), map[string]any{"model": "gemini-test", "input": "hello"})
	if err != nil || stream.StatusCode != http.StatusOK || len(stream.Events) != 1 {
		t.Fatalf("stream: %+v err=%v", stream, err)
	}
}
