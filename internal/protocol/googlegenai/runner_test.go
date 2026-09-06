package googlegenai

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

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/telemetry"
)

// TestRunAgainstDeterministicFixture exercises the native runner through a
// local Gemini-shaped service.  It intentionally covers the Interactions
// lifecycle, malformed JSON handling, structured output, tools, streaming,
// token counting, and metadata routing without making live or billable calls.
func TestRunAgainstDeterministicFixture(t *testing.T) {
	server := newGoogleFixture(t)
	defer server.Close()
	p, err := profile.LoadFile("google-genai-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	target := config.TargetConfig{
		ID: "google-genai", Protocol: config.ProtocolGoogleGenAI, BaseURL: server.URL,
		APIKey: "fixture-key", APIVersion: "v1beta", Models: []string{"fixture-text"}, Modules: []string{"auto"},
		RequestInterval: time.Millisecond,
	}
	cfg := config.Config{Tests: []string{"compat"}, Timeout: 3 * time.Second, Concurrency: 2, Modules: []string{"auto"}, FailOn: "core", Guidance: "off", MaxArtifactsPerModel: 1}
	report, err := Run(context.Background(), RunOptions{Target: target, Config: cfg, Profile: p})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.RequiredFail != 0 || report.Summary.Error != 0 {
		t.Fatalf("fixture produced core failure: %+v", report.Summary)
	}
	if len(report.Models) != 1 {
		t.Fatalf("selected model count=%d, want 1", len(report.Models))
	}
	seen := map[string]result.Status{}
	for _, probe := range report.Models[0].Compatibility {
		seen[probe.ID] = probe.Status
	}
	for _, id := range []string{
		"google.models.get", "google.models.metadata.limits", "google.models.metadata.methods",
		"google.interactions.basic", "google.interactions.schema", "google.interactions.store_false",
		"google.interactions.stream.syntax", "google.interactions.stream.event_type_match", "google.interactions.stream.sequence", "google.interactions.stream.completed", "google.interactions.stream.usage", "google.interactions.stream.done", "google.interactions.tools",
		"google.generate-content.stream.schema", "google.generate-content.stream.finish_reason", "google.generate-content.stream.usage",
		"google.interactions.structured-output", "google.tokens.count.basic", "google.error.invalid_model",
		"google.error.invalid_json",
	} {
		if seen[id] != result.StatusPass {
			t.Fatalf("probe %s=%q, want pass; all=%v", id, seen[id], seen)
		}
	}
}

// TestNativeTraceUsesConcreteProbeIDsBeforeHTTP protects the native trace
// invariant for composite Google checks: no capability-wide wrapper may own
// requests whose result row has a more specific probe ID.
func TestNativeTraceUsesConcreteProbeIDsBeforeHTTP(t *testing.T) {
	server := newGoogleFixture(t)
	defer server.Close()
	p, err := profile.LoadFile("google-genai-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	logger := logging.New(4)
	telemetryProvider := telemetry.New(logger)
	defer func() { _ = telemetryProvider.Shutdown(context.Background()) }()
	target := config.TargetConfig{ID: "google-genai", Protocol: config.ProtocolGoogleGenAI, BaseURL: server.URL, APIKey: "fixture-key", APIVersion: "v1beta", Models: []string{"fixture-text"}, Modules: []string{"auto", "google-genai.interactions-stateful"}, RequestInterval: time.Millisecond}
	cfg := config.Config{Tests: []string{"compat"}, Timeout: 3 * time.Second, Concurrency: 2, Modules: []string{"auto"}, FailOn: "core", Guidance: "off", MaxArtifactsPerModel: 1, AllowPersistentResources: true}
	if _, err := Run(context.Background(), RunOptions{Target: target, Config: cfg, Profile: p, Logger: logger, Telemetry: telemetryProvider}); err != nil {
		t.Fatal(err)
	}

	type record map[string]any
	var records []record
	for _, line := range logger.Lines() {
		var record record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode trace record: %v", err)
		}
		records = append(records, record)
	}
	probeSpans := map[string]string{}
	probeEnd := map[string]bool{}
	logicalChildren := map[string]int{}
	seenValidationDiagnostic := false
	for _, record := range records {
		event, _ := record["event"].(string)
		probeID, _ := record["probe"].(string)
		if event == "probe.start" && probeID != "" {
			probeSpans[probeID], _ = record["span_id"].(string)
		}
		if event == "probe.end" && probeID != "" {
			probeEnd[probeID] = true
		}
		if event == "http.logical_request.start" || event == "http.stream_logical_request.start" {
			parent, _ := record["parent_span_id"].(string)
			logicalChildren[parent]++
		}
		if event == "diagnostic.sse_validation" {
			if record["sse_validation_problem_codes"] != "-" || record["model"] != "fixture-text" || record["probe"] != "aiqa.native.google.interactions.stream.http" {
				t.Fatalf("unexpected SSE validation diagnostic: %#v", record)
			}
			seenValidationDiagnostic = true
		}
	}
	for _, id := range []string{
		"google.interactions.stream.http",
		"google.tokens.count.basic",
		"google.error.invalid_model",
		"google.error.invalid_json",
		"google.interactions.stateful.cleanup",
	} {
		probeID := "aiqa.native." + id
		span := probeSpans[probeID]
		if span == "" {
			t.Fatalf("missing concrete probe %q", probeID)
		}
		if logicalChildren[span] == 0 {
			t.Fatalf("probe %q has no direct logical request child", probeID)
		}
		if !probeEnd[probeID] {
			t.Fatalf("probe %q has no terminal event", probeID)
		}
	}
	if probeSpans["aiqa.native.google.interactions.stream"] != "" || probeSpans["aiqa.native.google.errors"] != "" {
		t.Fatalf("found legacy capability-wide composite probes: %#v", probeSpans)
	}
	if !seenValidationDiagnostic {
		t.Fatal("missing bounded SSE validation diagnostic")
	}
}

func TestLegacyProfileRunsGenerateContentSurface(t *testing.T) {
	server := newGoogleFixture(t)
	defer server.Close()
	p, err := profile.LoadFile("google-genai-generate-content-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	target := config.TargetConfig{ID: "google-genai", Protocol: config.ProtocolGoogleGenAI, BaseURL: server.URL, APIKey: "fixture-key", APIVersion: "v1beta", Models: []string{"fixture-text"}, Modules: []string{"auto"}, RequestInterval: time.Millisecond}
	cfg := config.Config{Tests: []string{"compat"}, Timeout: 3 * time.Second, Concurrency: 2, Modules: []string{"auto"}, FailOn: "core", Guidance: "off", MaxArtifactsPerModel: 1}
	report, err := Run(context.Background(), RunOptions{Target: target, Config: cfg, Profile: p})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.RequiredFail != 0 || report.Summary.Error != 0 {
		t.Fatalf("legacy fixture produced core failure: %+v", report.Summary)
	}
	seen := map[string]result.Status{}
	for _, probe := range report.Models[0].Compatibility {
		seen[probe.ID] = probe.Status
	}
	for _, id := range []string{"google.generate-content.basic", "google.generate-content.schema", "google.generate-content.stream.http", "google.generate-content.stream.schema", "google.generate-content.stream.sequence", "google.generate-content.stream.text", "google.generate-content.stream.finish_reason", "google.error.invalid_model", "google.error.invalid_json"} {
		if seen[id] != result.StatusPass {
			t.Fatalf("legacy probe %s=%q, want pass; all=%v", id, seen[id], seen)
		}
	}
}

func TestModelParameterNoticeIsRetainedWhenCombinedWithCompat(t *testing.T) {
	server := newGoogleFixture(t)
	defer server.Close()
	p, err := profile.LoadFile("google-genai-generate-content-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	target := config.TargetConfig{ID: "google-genai", Protocol: config.ProtocolGoogleGenAI, BaseURL: server.URL, APIKey: "fixture-key", APIVersion: "v1beta", Models: []string{"fixture-text"}, Modules: []string{"auto"}, RequestInterval: time.Millisecond}
	cfg := config.Config{Tests: []string{"compat", "model-param"}, Timeout: 3 * time.Second, Concurrency: 2, Modules: []string{"auto"}, FailOn: "core", Guidance: "off", MaxArtifactsPerModel: 1}
	report, err := Run(context.Background(), RunOptions{Target: target, Config: cfg, Profile: p})
	if err != nil {
		t.Fatal(err)
	}
	for _, probe := range report.Models[0].Compatibility {
		if probe.ID == "google.model-param.unsupported" && probe.Status == result.StatusUnsupported {
			return
		}
	}
	t.Fatal("combined compat/model-param run omitted the explicit unsupported notice")
}

func TestStatefulProbeUsesContinuationAndCleansUp(t *testing.T) {
	server := newGoogleFixture(t)
	defer server.Close()
	p, err := profile.LoadFile("google-genai-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	target := config.TargetConfig{
		ID: "google-genai", Protocol: config.ProtocolGoogleGenAI, BaseURL: server.URL,
		APIKey: "fixture-key", APIVersion: "v1beta", Models: []string{"fixture-text"}, Modules: []string{"google-genai.interactions-stateful"},
		RequestInterval: time.Millisecond,
	}
	cfg := config.Config{Tests: []string{"compat"}, Timeout: 3 * time.Second, Concurrency: 2, Modules: []string{"google-genai.interactions-stateful"}, AllowPersistentResources: true, FailOn: "core", Guidance: "off", MaxArtifactsPerModel: 1}
	report, err := Run(context.Background(), RunOptions{Target: target, Config: cfg, Profile: p})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]result.Status{}
	for _, probe := range report.Models[0].Compatibility {
		seen[probe.ID] = probe.Status
	}
	if seen["google.interactions.stateful"] != result.StatusPass || seen["google.interactions.stateful.cleanup"] != result.StatusPass {
		t.Fatalf("stateful probe did not complete and clean up: %v", seen)
	}
}

func TestValidateStructuredJSONUsesClosedLocalSchema(t *testing.T) {
	if !ValidateStructuredJSON(`{"ok":true,"value":"AIQA_OK"}`) {
		t.Fatal("valid structured response was rejected")
	}
	for _, raw := range []string{
		`{"ok":true}`,
		`{"ok":"true","value":"AIQA_OK"}`,
		`{"ok":true,"value":"AIQA_OK","extra":1}`,
		`{"ok":true,"value":"wrong"}`,
	} {
		if ValidateStructuredJSON(raw) {
			t.Fatalf("invalid structured response was accepted: %s", raw)
		}
	}
}

func TestGoogleStreamObservationDoesNotRenderRawModelOutput(t *testing.T) {
	observation := streamObservation(api.StreamResponse{
		StatusCode: 200, ContentType: "text/event-stream", RequestID: "req-1",
		RawSnippet: `data: {"type":"thought","text":"private chain"}`,
	})
	if observation.Snippet != "" {
		t.Fatalf("Google stream observation leaked raw output: %q", observation.Snippet)
	}
}

func TestSuccessfulMalformedJSONIsNotReportedAsPass(t *testing.T) {
	p, err := profile.LoadFile("google-genai-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	test := testBase(p, "google.models.get")
	setHTTPStatus(&test, api.Response{StatusCode: http.StatusOK}, fmt.Errorf("decode JSON response: malformed"), true)
	if test.Status != result.StatusError {
		t.Fatalf("malformed successful response status=%q, want error", test.Status)
	}
}

func TestStreamTransportErrorsRemainInconclusive(t *testing.T) {
	p, err := profile.LoadFile("google-genai-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		status   int
		optional bool
		want     result.Status
	}{
		{name: "successful response parse", status: http.StatusOK, want: result.StatusError},
		{name: "rate limited", status: http.StatusTooManyRequests, want: result.StatusError},
		{name: "optional rejection", status: http.StatusNotFound, optional: true, want: result.StatusUnsupported},
		{name: "required rejection", status: http.StatusNotFound, want: result.StatusFail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := testBase(p, "google.interactions.stream.http")
			setStreamError(&probe, fmt.Errorf("fixture stream error"), tc.status, tc.optional)
			if probe.Status != tc.want {
				t.Fatalf("status=%q, want %q", probe.Status, tc.want)
			}
		})
	}
}

func newGoogleFixture(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("x-goog-api-key") != "fixture-key" {
			writeGoogleError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing fixture key")
			return false
		}
		return true
	}
	mux.HandleFunc("/v1beta/models", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		writeGoogleJSON(w, http.StatusOK, map[string]any{"models": []any{map[string]any{
			"name": "models/fixture-text", "displayName": "Fixture text", "inputTokenLimit": 4096,
			"outputTokenLimit": 256, "supportedGenerationMethods": []any{"generateContent", "countTokens"},
		}}})
	})
	mux.HandleFunc("/v1beta/models/fixture-text", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		writeGoogleJSON(w, http.StatusOK, map[string]any{"name": "models/fixture-text", "inputTokenLimit": 4096, "outputTokenLimit": 256, "supportedGenerationMethods": []any{"generateContent", "countTokens"}})
	})
	mux.HandleFunc("/v1beta/models/aiqa-missing-model:generateContent", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		writeGoogleError(w, http.StatusNotFound, "NOT_FOUND", "model not found")
	})
	mux.HandleFunc("/v1beta/models/fixture-text:generateContent", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if json.Unmarshal(raw, &body) != nil {
			writeGoogleError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed JSON")
			return
		}
		parts := []any{map[string]any{"text": "AIQA_OK"}}
		if _, ok := body["tools"]; ok {
			parts = []any{map[string]any{"functionCall": map[string]any{"name": "get_status", "args": map[string]any{"resource_id": "resource-alpha"}}}}
		}
		if generation, ok := body["generationConfig"].(map[string]any); ok && generation["responseMimeType"] == "application/json" {
			parts = []any{map[string]any{"text": `{"ok":true,"value":"AIQA_OK"}`}}
		}
		writeGoogleJSON(w, http.StatusOK, map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": parts}, "finishReason": "STOP"}}, "usageMetadata": map[string]any{"promptTokenCount": 3, "candidatesTokenCount": 2, "totalTokenCount": 5}})
	})
	mux.HandleFunc("/v1beta/models/fixture-text:countTokens", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		writeGoogleJSON(w, http.StatusOK, map[string]any{"totalTokens": 5})
	})
	mux.HandleFunc("/v1beta/models/fixture-text:embedContent", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		writeGoogleJSON(w, http.StatusOK, map[string]any{"embedding": map[string]any{"values": []any{0.1, 0.2}}})
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
			encoded, _ := json.Marshal(map[string]any{"candidates": []any{candidate}})
			fmt.Fprintf(w, "data: %s\n\n", encoded)
		}
	})
	mux.HandleFunc("/v1beta/interactions", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		if r.URL.Query().Get("alt") == "sse" || strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			writeGoogleInteractionStream(w)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if json.Unmarshal(raw, &body) != nil {
			writeGoogleError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed JSON")
			return
		}
		if body["model"] == "aiqa-missing-model" {
			writeGoogleError(w, http.StatusNotFound, "NOT_FOUND", "model not found")
			return
		}
		text := "AIQA_OK"
		if _, ok := body["previous_interaction_id"]; ok {
			text = "AIQA_STATEFUL_OK"
		}
		steps := []any{map[string]any{"type": "model_output", "text": text}}
		if _, ok := body["tools"]; ok {
			steps = []any{map[string]any{"type": "function_call", "name": "get_status", "arguments": `{"resource_id":"resource-alpha"}`}}
		}
		if _, ok := body["response_format"]; ok {
			steps = []any{map[string]any{"type": "model_output", "text": `{"ok":true,"value":"AIQA_OK"}`}}
		}
		id := "fixture-interaction"
		if stored, ok := body["store"].(bool); ok && stored {
			id = "stateful-created"
		}
		writeGoogleJSON(w, http.StatusOK, map[string]any{"id": id, "object": "interaction", "status": "completed", "model": "fixture-text", "steps": steps, "usage": map[string]any{"input_tokens": 3, "output_tokens": 2, "total_tokens": 5}})
	})
	mux.HandleFunc("/v1beta/interactions/", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		if r.Method != http.MethodDelete || strings.TrimPrefix(r.URL.Path, "/v1beta/interactions/") != "stateful-created" {
			writeGoogleError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "unsupported fixture operation")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return httptest.NewServer(mux)
}

func writeGoogleInteractionStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	for _, event := range []struct {
		name string
		body map[string]any
	}{
		{"interaction.created", map[string]any{"event_type": "interaction.created", "interaction": map[string]any{"id": "fixture-interaction"}}},
		{"step.start", map[string]any{"event_type": "step.start", "interaction": map[string]any{"id": "fixture-interaction"}, "step": map[string]any{"index": 0}}},
		{"step.delta", map[string]any{"event_type": "step.delta", "interaction": map[string]any{"id": "fixture-interaction"}, "step": map[string]any{"index": 0}, "delta": map[string]any{"type": "output_text_delta", "text": "AIQA_OK"}}},
		{"step.stop", map[string]any{"event_type": "step.stop", "interaction": map[string]any{"id": "fixture-interaction"}, "step": map[string]any{"index": 0}}},
		{"interaction.completed", map[string]any{"event_type": "interaction.completed", "interaction": map[string]any{"id": "fixture-interaction", "usage": map[string]any{"input_tokens": 3, "output_tokens": 2, "total_tokens": 5}}}},
	} {
		encoded, _ := json.Marshal(event.body)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.name, encoded)
	}
	fmt.Fprint(w, "event: done\ndata: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func writeGoogleJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeGoogleError(w http.ResponseWriter, status int, code, message string) {
	writeGoogleJSON(w, status, map[string]any{"error": map[string]any{"code": status, "status": code, "message": message}})
}
