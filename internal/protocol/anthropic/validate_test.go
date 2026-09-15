package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/result"
)

func TestValidateModelsAndMessages(t *testing.T) {
	models := map[string]any{
		"data":     []any{validModel("vendor/model")},
		"has_more": false, "first_id": "vendor/model", "last_id": "vendor/model",
	}
	if problems := ValidateModels(models); len(problems) != 0 {
		t.Fatalf("valid models rejected: %v", problems)
	}
	models["data"] = []any{map[string]any{"id": "duplicate", "type": "wrong", "created_at": "bad"}, validModel("duplicate")}
	if problems := ValidateModels(models); len(problems) < 4 {
		t.Fatalf("invalid models underdiagnosed: %v", problems)
	}

	message := map[string]any{"id": "msg", "type": "message", "role": "assistant", "model": "vendor/model", "content": []any{map[string]any{"type": "text", "text": "ok"}, map[string]any{"type": "future_block", "value": true}}, "stop_reason": "future_reason", "usage": map[string]any{"input_tokens": json.Number("2"), "output_tokens": json.Number("1")}}
	if problems := ValidateMessage(message); len(problems) != 0 {
		t.Fatalf("valid additive message rejected: %v", problems)
	}
	delete(message, "usage")
	message["content"] = []any{map[string]any{"type": "text", "text": 1}}
	if problems := ValidateMessage(message); len(problems) < 2 {
		t.Fatalf("invalid message underdiagnosed: %v", problems)
	}
}

func TestValidateModelsRequiresPinnedSDKModelInfoGroups(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(map[string]any)
		want string
	}{
		{name: "capabilities", edit: func(model map[string]any) { delete(model, "capabilities") }, want: ".capabilities"},
		{name: "max input", edit: func(model map[string]any) { delete(model, "max_input_tokens") }, want: ".max_input_tokens"},
		{name: "batch supported", edit: func(model map[string]any) {
			delete(model["capabilities"].(map[string]any)["batch"].(map[string]any), "supported")
		}, want: ".batch.supported"},
		{name: "context management", edit: func(model map[string]any) {
			delete(model["capabilities"].(map[string]any)["context_management"].(map[string]any), "compact_20260112")
		}, want: "compact_20260112"},
		{name: "effort", edit: func(model map[string]any) {
			delete(model["capabilities"].(map[string]any)["effort"].(map[string]any), "xhigh")
		}, want: ".effort.xhigh"},
		{name: "thinking types", edit: func(model map[string]any) {
			delete(model["capabilities"].(map[string]any)["thinking"].(map[string]any), "types")
		}, want: ".thinking.types"},
		{name: "thinking enabled", edit: func(model map[string]any) {
			delete(model["capabilities"].(map[string]any)["thinking"].(map[string]any)["types"].(map[string]any), "enabled")
		}, want: ".thinking.types.enabled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := validModel("vendor/model")
			test.edit(model)
			root := map[string]any{"data": []any{model}, "has_more": false, "first_id": "vendor/model", "last_id": "vendor/model"}
			problems := strings.Join(ValidateModels(root), "; ")
			if !strings.Contains(problems, test.want) {
				t.Fatalf("validation did not identify %q: %s", test.want, problems)
			}
		})
	}
}

func TestModelSchemaDiagnosticsPreservePathModelAndExpectation(t *testing.T) {
	model := validModel("vendor/model")
	delete(model["capabilities"].(map[string]any)["batch"].(map[string]any), "supported")
	root := map[string]any{"data": []any{model}, "has_more": false, "first_id": "vendor/model", "last_id": "vendor/model"}
	diagnostics := ModelSchemaDiagnostics(root)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics=%+v", diagnostics)
	}
	got := diagnostics[0]
	if got.Path != "data[0].capabilities.batch.supported" || got.ModelID != "vendor/model" || got.Issue != "missing" || got.Expected != "boolean" {
		t.Fatalf("diagnostic=%+v", got)
	}
}

func validModel(id string) map[string]any {
	support := func() map[string]any { return map[string]any{"supported": true} }
	return map[string]any{
		"id": id, "type": "model", "display_name": "Model", "created_at": "2026-09-01T00:00:00Z",
		"max_input_tokens": 200000, "max_tokens": 8192,
		"capabilities": map[string]any{
			"batch": support(), "citations": support(), "code_execution": support(), "image_input": support(), "pdf_input": support(), "structured_outputs": support(),
			"context_management": map[string]any{"clear_thinking_20251015": support(), "clear_tool_uses_20250919": support(), "compact_20260112": support(), "supported": true},
			"effort":             map[string]any{"high": support(), "low": support(), "max": support(), "medium": support(), "xhigh": support(), "supported": true},
			"thinking":           map[string]any{"supported": true, "types": map[string]any{"adaptive": support(), "enabled": support()}},
		},
	}
}

func TestValidateErrorAndCountTokenUsage(t *testing.T) {
	if problems := ValidateError(map[string]any{"type": "error", "error": map[string]any{"type": "invalid_request_error", "message": "bad"}}); len(problems) != 0 {
		t.Fatalf("valid error rejected: %v", problems)
	}
	if problems := ValidateError(map[string]any{"type": "error", "error": map[string]any{"type": "invalid_request_error"}}); len(problems) == 0 {
		t.Fatal("missing error.message accepted")
	}
	usage := ParseUsage(map[string]any{"usage": map[string]any{"input_tokens": json.Number("7"), "output_tokens": json.Number("2"), "cache_creation_input_tokens": json.Number("3"), "cache_read_input_tokens": json.Number("4"), "output_tokens_details": map[string]any{"thinking_tokens": json.Number("1")}}})
	if usage.InputTokens != 7 || usage.OutputTokens != 2 || usage.CacheCreationInputTokens != 3 || usage.CacheReadInputTokens != 4 || usage.ThinkingTokens != 1 || usage.TotalTokens != 16 {
		t.Fatalf("usage mapping distorted Anthropic fields: %+v", usage)
	}
}

func TestHTTPClassificationKeepsRuntimeErrorsInconclusive(t *testing.T) {
	for _, status := range []int{408, 429, 500, 529} {
		probe := result.TestResult{}
		setHTTPStatus(&probe, api.Response{StatusCode: status}, nil, true)
		if probe.Status != result.StatusError {
			t.Fatalf("HTTP %d classified as %s, want error", status, probe.Status)
		}
	}
	optional := result.TestResult{}
	setHTTPStatus(&optional, api.Response{StatusCode: 404}, nil, true)
	if optional.Status != result.StatusUnsupported {
		t.Fatalf("optional 404=%s, want unsupported", optional.Status)
	}
	required := result.TestResult{}
	setHTTPStatus(&required, api.Response{StatusCode: 404}, nil, false)
	if required.Status != result.StatusFail {
		t.Fatalf("required 404=%s, want fail", required.Status)
	}
}

func TestValidateStreamAcceptsNamedAnthropicSequence(t *testing.T) {
	events := []api.SSEEvent{
		event("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":4,"output_tokens":0}}}`),
		event("ping", `{"type":"ping"}`),
		event("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		event("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"AIQA_OK"}}`),
		event("content_block_stop", `{"type":"content_block_stop","index":0}`),
		event("future_event", `{"type":"future_event","value":1}`),
		event("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":5,"output_tokens":2,"cache_creation_input_tokens":3,"cache_read_input_tokens":4,"output_tokens_details":{"thinking_tokens":1}}}`),
		event("message_stop", `{"type":"message_stop"}`),
	}
	got := ValidateStream(api.StreamResponse{Events: events})
	if len(got.Problems) > 0 || got.Text != "AIQA_OK" || !got.SawMessageStop || len(got.Unknown) != 1 || got.Usage.TotalTokens != 14 || got.Usage.ThinkingTokens != 1 {
		t.Fatalf("unexpected validation: %+v", got)
	}
}

func TestValidateStreamRejectsIllegalKnownOrdering(t *testing.T) {
	got := ValidateStream(api.StreamResponse{Events: []api.SSEEvent{event("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"x"}}`)}})
	if len(got.Problems) == 0 {
		t.Fatal("expected ordering errors")
	}
}

func TestValidateStreamRejectsDuplicateLifecycleEvents(t *testing.T) {
	got := ValidateStream(api.StreamResponse{Events: []api.SSEEvent{
		event("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":1,"output_tokens":0}}}`),
		event("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":1,"output_tokens":0}}}`),
		event("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		event("content_block_stop", `{"type":"content_block_stop","index":0}`),
		event("content_block_stop", `{"type":"content_block_stop","index":0}`),
		event("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`),
		event("message_stop", `{"type":"message_stop"}`),
		event("message_stop", `{"type":"message_stop"}`),
	}})
	if len(got.Problems) < 3 {
		t.Fatalf("duplicate lifecycle events underdiagnosed: %+v", got)
	}
}

func TestValidateStreamTreatsProviderErrorSeparately(t *testing.T) {
	got := ValidateStream(api.StreamResponse{Events: []api.SSEEvent{event("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":1,"output_tokens":0}}}`), event("error", `{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`)}})
	if !got.ProviderError {
		t.Fatalf("provider error was not detected: %+v", got)
	}
}

func TestValidateStreamRejectsTypeMismatchAndInvalidToolJSON(t *testing.T) {
	got := ValidateStream(api.StreamResponse{Events: []api.SSEEvent{
		event("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":1,"output_tokens":0}}}`),
		event("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu","name":"tool","input":{}}}`),
		event("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{broken"}}`),
		event("content_block_stop", `{"type":"content_block_stop","index":0}`),
		event("message_delta", `{"type":"wrong_type","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`),
		event("message_stop", `{"type":"message_stop"}`),
	}})
	if len(got.Problems) < 2 {
		t.Fatalf("expected type and tool JSON problems: %+v", got)
	}
}

func TestValidateStreamDistinguishesIncompleteEOFAndDoneSentinel(t *testing.T) {
	got := ValidateStream(api.StreamResponse{Events: []api.SSEEvent{
		event("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":1,"output_tokens":0}}}`),
		{Event: "", Data: "[DONE]"},
	}})
	if !got.Incomplete || !got.SawDoneSentinel || len(got.Problems) != 0 {
		t.Fatalf("unexpected incomplete stream classification: %+v", got)
	}
}

func TestValidateStreamAcceptsMultipleBlocksWithoutRetainingThinkingText(t *testing.T) {
	got := ValidateStream(api.StreamResponse{Events: []api.SSEEvent{
		event("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":1,"output_tokens":0}}}`),
		event("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`),
		event("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"private chain"}}`),
		event("content_block_stop", `{"type":"content_block_stop","index":0}`),
		event("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`),
		event("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"AIQA_OK"}}`),
		event("content_block_stop", `{"type":"content_block_stop","index":1}`),
		event("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`),
		event("message_stop", `{"type":"message_stop"}`),
	}})
	if len(got.Problems) != 0 || got.Incomplete || got.Text != "AIQA_OK" || strings.Contains(got.Text, "private") {
		t.Fatalf("thinking stream was retained or rejected: %+v", got)
	}
}

func event(name, raw string) api.SSEEvent {
	var data map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	_ = decoder.Decode(&data)
	return api.SSEEvent{Event: name, Data: raw, JSON: data}
}
