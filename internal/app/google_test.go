package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/result"
)

func TestGoogleNativeAndOfficialSDKUseOneTargetContract(t *testing.T) {
	server := newGoogleSDKFixture(t)
	defer server.Close()
	withTempWorkingDirectory(t)
	cfg := config.Config{
		Targets: []config.TargetConfig{{ID: "google-genai", Protocol: config.ProtocolGoogleGenAI, BaseURL: server.URL, APIKey: "fixture-key", APIVersion: "v1beta", Models: []string{"fixture-text"}, ProfileFile: "google-genai-compat-2026-09.json", Modules: []string{"auto"}, RequestInterval: time.Millisecond}},
		Tests:   []string{"compat", "ext-suite"}, ExtSuites: []string{"official-sdk.google-genai"}, Timeout: 5 * time.Second, Concurrency: 2,
		Modules: []string{"auto"}, MaxArtifactsPerModel: 1, FailOn: "core", Guidance: "off",
	}
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil || out.ReportV3 == nil || len(out.ReportV3.Targets) != 1 {
		t.Fatalf("Google combined run failed: exit=%d err=%v report=%+v", out.ExitCode, err, out.ReportV3)
	}
	target := out.ReportV3.Targets[0]
	if len(target.Notes) == 0 || !strings.Contains(target.Notes[0], "does not expose public Interactions") {
		t.Fatalf("Google target did not expose the SDK limitation note: %v", target.Notes)
	}
	if len(target.Extensions) == 0 {
		t.Fatal("Official Google SDK produced no extension runs")
	}
	seen := map[string]result.Status{}
	for _, run := range target.Extensions {
		if run.Provenance.ID != "official-sdk.google-genai" {
			t.Fatalf("unexpected extension provenance: %+v", run.Provenance)
		}
		for _, suite := range run.Suites {
			seen[suite.SuiteID] = suite.Status
		}
	}
	for _, suite := range []string{"models.list", "models.get", "generate-content", "generate-content-stream", "count-tokens", "embeddings", "tools", "structured-output", "multimodal"} {
		if seen[suite] != result.StatusPass {
			t.Fatalf("SDK suite %s=%q, want pass; all=%v", suite, seen[suite], seen)
		}
	}
	if _, present := seen["interactions"]; present {
		t.Fatalf("Official Google SDK must not claim an Interactions suite: %v", seen)
	}
	if target.Summary.RequiredFail != 0 || target.Summary.Error != 0 {
		t.Fatalf("combined Google run has core failure: %+v", target.Summary)
	}
}

func newGoogleSDKFixture(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("x-goog-api-key") != "fixture-key" {
			writeGoogleFixtureError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing fixture key")
			return false
		}
		return true
	}
	mux.HandleFunc("/v1beta/models", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		writeGoogleFixtureJSON(w, http.StatusOK, map[string]any{"models": []any{map[string]any{"name": "models/fixture-text", "inputTokenLimit": 4096, "outputTokenLimit": 256, "supportedGenerationMethods": []any{"generateContent", "countTokens", "embedContent"}, "inputModalities": []any{"text", "image"}}}})
	})
	mux.HandleFunc("/v1beta/models/fixture-text", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		writeGoogleFixtureJSON(w, http.StatusOK, map[string]any{"name": "models/fixture-text", "inputTokenLimit": 4096, "outputTokenLimit": 256, "supportedGenerationMethods": []any{"generateContent", "countTokens", "embedContent"}, "inputModalities": []any{"text", "image"}})
	})
	mux.HandleFunc("/v1beta/models/fixture-text:countTokens", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		writeGoogleFixtureJSON(w, http.StatusOK, map[string]any{"totalTokens": 5})
	})
	mux.HandleFunc("/v1beta/models/fixture-text:embedContent", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		writeGoogleFixtureJSON(w, http.StatusOK, map[string]any{"embedding": map[string]any{"values": []any{0.1, 0.2, 0.3}}})
	})
	mux.HandleFunc("/v1beta/models/fixture-text:batchEmbedContents", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		writeGoogleFixtureJSON(w, http.StatusOK, map[string]any{"embeddings": []any{map[string]any{"values": []any{0.1, 0.2, 0.3}}}})
	})
	mux.HandleFunc("/v1beta/models/fixture-text:streamGenerateContent", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for index, text := range []string{"AIQA_", "OK"} {
			candidate := map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": text}}}}
			if index == 1 {
				candidate["finishReason"] = "STOP"
				candidate["usageMetadata"] = map[string]any{"promptTokenCount": 3, "candidatesTokenCount": 2, "totalTokenCount": 5}
			}
			payload := map[string]any{"candidates": []any{candidate}}
			encoded, _ := json.Marshal(payload)
			fmt.Fprintf(w, "data: %s\n\n", encoded)
		}
	})
	mux.HandleFunc("/v1beta/models/fixture-text:generateContent", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if json.Unmarshal(raw, &body) != nil {
			writeGoogleFixtureError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed JSON")
			return
		}
		if body["model"] == "aiqa-missing-model" {
			writeGoogleFixtureError(w, http.StatusNotFound, "NOT_FOUND", "model not found")
			return
		}
		parts := []any{map[string]any{"text": "AIQA_OK"}}
		if _, ok := body["tools"]; ok {
			parts = []any{map[string]any{"functionCall": map[string]any{"name": "get_status", "args": map[string]any{"resource_id": "resource-alpha"}}}}
		}
		if generation, ok := body["generationConfig"].(map[string]any); ok && generation["responseMimeType"] == "application/json" {
			parts = []any{map[string]any{"text": `{"ok":true,"value":"AIQA_OK"}`}}
		}
		writeGoogleFixtureJSON(w, http.StatusOK, map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": parts}, "finishReason": "STOP"}}, "usageMetadata": map[string]any{"promptTokenCount": 3, "candidatesTokenCount": 2, "totalTokenCount": 5}})
	})
	mux.HandleFunc("/v1beta/interactions", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		if r.URL.Query().Get("alt") == "sse" || strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			writeGoogleFixtureInteractionStream(w)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if json.Unmarshal(raw, &body) != nil {
			writeGoogleFixtureError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed JSON")
			return
		}
		if body["model"] == "aiqa-missing-model" {
			writeGoogleFixtureError(w, http.StatusNotFound, "NOT_FOUND", "model not found")
			return
		}
		steps := []any{map[string]any{"type": "model_output", "text": "AIQA_OK"}}
		if _, ok := body["tools"]; ok {
			steps = []any{map[string]any{"type": "function_call", "name": "get_status", "arguments": `{"resource_id":"resource-alpha"}`}}
		}
		if _, ok := body["response_format"]; ok {
			steps = []any{map[string]any{"type": "model_output", "text": `{"ok":true,"value":"AIQA_OK"}`}}
		}
		writeGoogleFixtureJSON(w, http.StatusOK, map[string]any{"id": "fixture-interaction", "object": "interaction", "status": "completed", "model": "fixture-text", "steps": steps, "usage": map[string]any{"input_tokens": 3, "output_tokens": 2, "total_tokens": 5}})
	})
	return httptest.NewServer(mux)
}

func writeGoogleFixtureInteractionStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, event := range []struct {
		name string
		body map[string]any
	}{
		{"interaction.created", map[string]any{"type": "interaction.created", "id": "fixture-interaction"}},
		{"step.start", map[string]any{"type": "step.start", "step_id": "step-1"}},
		{"step.delta", map[string]any{"type": "step.delta", "step_id": "step-1", "text": "AIQA_OK"}},
		{"step.stop", map[string]any{"type": "step.stop", "step_id": "step-1"}},
		{"interaction.completed", map[string]any{"type": "interaction.completed", "usage": map[string]any{"input_tokens": 3, "output_tokens": 2, "total_tokens": 5}}},
	} {
		encoded, _ := json.Marshal(event.body)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.name, encoded)
	}
	fmt.Fprint(w, "event: done\ndata: [DONE]\n\n")
}

func writeGoogleFixtureJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeGoogleFixtureError(w http.ResponseWriter, status int, code, message string) {
	writeGoogleFixtureJSON(w, status, map[string]any{"error": map[string]any{"code": status, "status": code, "message": message}})
}
