package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

func TestDualTargetCommandWritesIndependentV2Report(t *testing.T) {
	openAI := newMockOpenAI(t)
	defer openAI.Close()
	anthropic := newMockAnthropic(t)
	defer anthropic.Close()
	schemaPath, err := filepath.Abs(filepath.Join("..", "..", "schema", "report-v2.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	withTempWorkingDirectory(t)
	t.Setenv("OPENAI_BASE_URL", openAI.URL+"/v1")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", anthropic.URL+"/anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-test-key")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "must-not-be-used")
	t.Setenv("ANTHROPIC_BETA", "must-not-be-used")
	cfg, err := config.Parse([]string{"test=compat", "--timeout=5", "--request-interval=1ms", "--external=off", "--format=markdown,json", "--name=dual"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ExitCode != 0 {
		t.Fatalf("exit=%d summary=%+v", out.ExitCode, out.ReportV2.Summary)
	}
	data, err := os.ReadFile("dual.json")
	if err != nil {
		t.Fatal(err)
	}
	var report result.ReportV2
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != "aiqa.report.v2" || len(report.Targets) != 2 {
		t.Fatalf("unexpected report: schema=%s targets=%d", report.SchemaVersion, len(report.Targets))
	}
	if report.Targets[0].Protocol != "openai" || report.Targets[1].Protocol != "anthropic" {
		t.Fatalf("target order/protocols: %+v", report.Targets)
	}
	for _, target := range report.Targets {
		if target.Summary.Assessment == nil || target.Summary.Assessment.Verdict != "pass" {
			var gaps []string
			for _, requirement := range target.Requirements {
				if requirement.Status != result.RequirementPass && requirement.Status != result.RequirementNotApplicable {
					gaps = append(gaps, fmt.Sprintf("%s=%s", requirement.ID, requirement.Status))
				}
			}
			t.Fatalf("target %s did not pass: %+v gaps=%v", target.ID, target.Summary, gaps)
		}
	}
	var anthropicTarget *result.TargetReport
	for index := range report.Targets {
		if report.Targets[index].Protocol == "anthropic" {
			anthropicTarget = &report.Targets[index]
		}
	}
	if anthropicTarget == nil {
		t.Fatal("Anthropic target is missing")
	}
	contract, err := profile.LoadFile("anthropic-messages-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	covered := map[string]bool{}
	for _, requirement := range anthropicTarget.Requirements {
		covered[requirement.ID] = true
		if requirement.Status == result.RequirementSkip {
			t.Fatalf("full mock left requirement %s skipped", requirement.ID)
		}
	}
	for _, probe := range contract.Probes {
		if !covered[probe.ID] {
			t.Fatalf("profile probe %s is absent from canonical requirements", probe.ID)
		}
	}
	if strings.Contains(string(data), "test-key") || strings.Contains(string(data), "anthropic-test-key") {
		t.Fatal("API key leaked into report")
	}
	program := `import json,sys
from jsonschema import Draft202012Validator,FormatChecker
schema=json.load(open(sys.argv[1])); Draft202012Validator.check_schema(schema)
Draft202012Validator(schema,format_checker=FormatChecker()).validate(json.load(open(sys.argv[2])))`
	if output, err := exec.Command("python3", "-c", program, schemaPath, "dual.json").CombinedOutput(); err != nil {
		t.Fatalf("v2 schema validation failed: %v\n%s", err, output)
	}
	markdown, err := os.ReadFile("dual.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Target: OpenAI", "## Target: Anthropic", "Percentages are not combined into a cross-protocol score"} {
		if !strings.Contains(string(markdown), want) {
			t.Fatalf("markdown missing %q", want)
		}
	}
}

func TestAnthropicThinkingRequiresOptInAndRedactsPrivateContent(t *testing.T) {
	server := newMockAnthropic(t)
	defer server.Close()
	withTempWorkingDirectory(t)
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL+"/anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-test-key")
	cfg, err := config.Parse([]string{
		"test=compat", "--external=off", "--format=json", "--name=thinking", "--timeout=5", "--request-interval=1ms",
		"--module=anthropic.thinking", "--allow-high-token-probes",
		"--anthropic-model-capability=claude-mock=anthropic.messages.text+anthropic.messages.thinking.manual+anthropic.messages.thinking.adaptive",
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ReportV2 == nil || len(out.ReportV2.Targets) != 1 {
		t.Fatalf("unexpected report: %+v", out.ReportV2)
	}
	seen := map[string]result.Status{}
	for _, probe := range out.ReportV2.Targets[0].Models[0].Compatibility {
		if strings.HasPrefix(probe.ID, "anthropic.thinking.") {
			seen[probe.ID] = probe.Status
		}
	}
	for _, id := range []string{"anthropic.thinking.adaptive", "anthropic.thinking.effort", "anthropic.thinking.manual", "anthropic.thinking.manual.validation", "anthropic.thinking.stream"} {
		if seen[id] != result.StatusPass {
			t.Fatalf("thinking probe %s=%s, all=%v model=%+v", id, seen[id], seen, out.ReportV2.Targets[0].Models[0])
		}
	}
	data, err := os.ReadFile("thinking.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private chain") || strings.Contains(string(data), "signature-secret") {
		t.Fatal("thinking text or signature leaked into report")
	}
}

func newMockAnthropic(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	check := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("x-api-key") != "anthropic-test-key" {
			anthropicError(w, 401, "authentication_error", "invalid key")
			return false
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			anthropicError(w, 400, "invalid_request_error", "invalid version")
			return false
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("Anthropic request leaked Authorization header")
		}
		if r.Header.Get("anthropic-beta") != "" {
			t.Errorf("stable Anthropic request inherited a beta header")
		}
		return true
	}
	mux.HandleFunc("/anthropic/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		writeJSON(w, 200, map[string]any{"data": []any{anthropicModel()}, "has_more": false, "first_id": "claude-mock", "last_id": "claude-mock"})
	})
	mux.HandleFunc("/anthropic/v1/models/", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/anthropic/v1/models/")
		if id != "claude-mock" {
			anthropicError(w, 404, "not_found_error", "model not found")
			return
		}
		writeJSON(w, 200, anthropicModel())
	})
	mux.HandleFunc("/anthropic/v1/messages/count_tokens", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		count := 12
		if _, ok := body["system"]; ok {
			count += 4
		}
		if _, ok := body["tools"]; ok {
			count += 8
		}
		writeJSON(w, 200, map[string]any{"input_tokens": count})
	})
	mux.HandleFunc("/anthropic/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		var body map[string]any
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&body); err != nil {
			anthropicError(w, 400, "invalid_request_error", "invalid JSON")
			return
		}
		if body["model"] != "claude-mock" {
			anthropicError(w, 404, "not_found_error", "model not found")
			return
		}
		if _, ok := body["max_tokens"].(float64); !ok {
			anthropicError(w, 400, "invalid_request_error", "max_tokens must be integer")
			return
		}
		thinking, _ := body["thinking"].(map[string]any)
		if thinking != nil {
			if budget, ok := thinking["budget_tokens"].(float64); ok && budget < 1024 {
				anthropicError(w, 400, "invalid_request_error", "thinking budget is too small")
				return
			}
		}
		if body["stream"] == true {
			if r.Header.Get("Accept") != "text/event-stream" {
				t.Errorf("Anthropic stream Accept=%q", r.Header.Get("Accept"))
			}
			if thinking != nil {
				writeAnthropicThinkingStream(w)
			} else if choice, _ := body["tool_choice"].(map[string]any); choice != nil && choice["type"] == "tool" {
				writeAnthropicToolStream(w)
			} else {
				writeAnthropicStream(w)
			}
			return
		}
		content := []any{map[string]any{"type": "text", "text": "AIQA_OK"}}
		if thinking != nil {
			content = []any{map[string]any{"type": "thinking", "thinking": "private chain", "signature": "signature-secret"}, map[string]any{"type": "text", "text": "AIQA_OK"}}
		}
		stop := "end_turn"
		stopSequence := any(nil)
		if sequences, _ := body["stop_sequences"].([]any); len(sequences) > 0 {
			stop = "stop_sequence"
			stopSequence = sequences[0]
		}
		if choice, _ := body["tool_choice"].(map[string]any); choice != nil && choice["type"] != "none" {
			content = []any{map[string]any{"type": "tool_use", "id": "toolu_mock", "name": "get_status", "input": map[string]any{"resource_id": "resource-alpha"}}}
			if tools, _ := body["tools"].([]any); len(tools) > 1 {
				content = append(content, map[string]any{"type": "tool_use", "id": "toolu_owner", "name": "get_owner", "input": map[string]any{"resource_id": "resource-alpha"}})
			}
			stop = "tool_use"
			stopSequence = nil
		} else if messages, _ := body["messages"].([]any); hasAnthropicToolResult(messages) {
			content = []any{map[string]any{"type": "text", "text": "resource-alpha is available"}}
		} else if _, ok := body["output_config"]; ok {
			content = []any{map[string]any{"type": "text", "text": `{"ok":true,"value":"AIQA_OK"}`}}
		}
		writeJSON(w, 200, map[string]any{"id": "msg_mock", "type": "message", "role": "assistant", "model": "claude-mock", "content": content, "stop_reason": stop, "stop_sequence": stopSequence, "usage": map[string]any{"input_tokens": 12, "output_tokens": 3}})
	})
	return httptest.NewServer(mux)
}

func anthropicModel() map[string]any {
	return map[string]any{"id": "claude-mock", "type": "model", "display_name": "Claude Mock", "created_at": time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339), "debug_echo": "anthropic-test-key", "capabilities": map[string]any{"image_input": map[string]any{"supported": true}}}
}
func anthropicError(w http.ResponseWriter, status int, typ, message string) {
	writeJSON(w, status, map[string]any{"type": "error", "error": map[string]any{"type": typ, "message": message}, "request_id": "req_mock"})
}
func hasAnthropicToolResult(messages []any) bool {
	for _, raw := range messages {
		m, _ := raw.(map[string]any)
		blocks, _ := m["content"].([]any)
		for _, blockRaw := range blocks {
			block, _ := blockRaw.(map[string]any)
			if block["type"] == "tool_result" {
				return true
			}
		}
	}
	return false
}
func writeAnthropicStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	events := []struct {
		name string
		data map[string]any
	}{
		{"message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_stream", "type": "message", "role": "assistant", "model": "claude-mock", "content": []any{}, "stop_reason": nil, "usage": map[string]any{"input_tokens": 12, "output_tokens": 0}}}},
		{"content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}}},
		{"content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": "AIQA_OK"}}},
		{"content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}},
		{"message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 3}}},
		{"message_stop", map[string]any{"type": "message_stop"}},
	}
	for _, event := range events {
		raw, _ := json.Marshal(event.data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.name, raw)
	}
}

func writeAnthropicToolStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	events := []struct {
		name string
		data map[string]any
	}{
		{"message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_tool", "type": "message", "role": "assistant", "model": "claude-mock", "content": []any{}, "stop_reason": nil, "usage": map[string]any{"input_tokens": 12, "output_tokens": 0}}}},
		{"content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": "toolu_mock", "name": "get_status", "input": map[string]any{}}}},
		{"content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": "{\"resource_id\":\"resource-alpha\"}"}}},
		{"content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}},
		{"message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use"}, "usage": map[string]any{"output_tokens": 4}}},
		{"message_stop", map[string]any{"type": "message_stop"}},
	}
	for _, event := range events {
		raw, _ := json.Marshal(event.data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.name, raw)
	}
}

func writeAnthropicThinkingStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	events := []struct {
		name string
		data map[string]any
	}{
		{"message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_thinking", "type": "message", "role": "assistant", "model": "claude-mock", "content": []any{}, "stop_reason": nil, "usage": map[string]any{"input_tokens": 12, "output_tokens": 0}}}},
		{"content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "thinking", "thinking": ""}}},
		{"content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "thinking_delta", "thinking": "private chain"}}},
		{"content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "signature_delta", "signature": "signature-secret"}}},
		{"content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}},
		{"content_block_start", map[string]any{"type": "content_block_start", "index": 1, "content_block": map[string]any{"type": "text", "text": ""}}},
		{"content_block_delta", map[string]any{"type": "content_block_delta", "index": 1, "delta": map[string]any{"type": "text_delta", "text": "AIQA_OK"}}},
		{"content_block_stop", map[string]any{"type": "content_block_stop", "index": 1}},
		{"message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 5}}},
		{"message_stop", map[string]any{"type": "message_stop"}},
	}
	for _, event := range events {
		raw, _ := json.Marshal(event.data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.name, raw)
	}
}
