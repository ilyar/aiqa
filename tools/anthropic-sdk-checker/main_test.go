package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func TestAllStableSuitesUseOfficialSDK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "key" {
			sdkJSON(w, 401, map[string]any{"type": "error", "error": map[string]any{"type": "authentication_error", "message": "bad key"}})
			return
		}
		switch {
		case r.URL.Path == "/v1/models":
			sdkJSON(w, 200, map[string]any{"data": []any{map[string]any{"id": "claude-mock", "type": "model", "display_name": "Claude Mock", "created_at": "2026-09-01T00:00:00Z"}}, "has_more": false, "first_id": "claude-mock", "last_id": "claude-mock"})
		case r.URL.Path == "/v1/models/claude-mock":
			sdkJSON(w, 200, map[string]any{"id": "claude-mock", "type": "model", "display_name": "Claude Mock", "created_at": "2026-09-01T00:00:00Z"})
		case r.URL.Path == "/v1/messages/count_tokens":
			sdkJSON(w, 200, map[string]any{"input_tokens": 7})
		case r.URL.Path == "/v1/messages":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["model"] == "aiqa-missing-model" {
				sdkJSON(w, 404, map[string]any{"type": "error", "error": map[string]any{"type": "not_found_error", "message": "model not found"}, "request_id": "req_mock"})
				return
			}
			if body["stream"] == true {
				if _, ok := body["tool_choice"]; ok {
					sdkToolStream(w)
				} else {
					sdkStream(w)
				}
				return
			}
			if _, ok := body["output_config"]; ok {
				sdkJSON(w, 200, sdkTextMessage(`{"ok":true}`))
				return
			}
			if sdkHasToolResult(body["messages"]) {
				sdkJSON(w, 200, sdkTextMessage("resource-alpha is available"))
				return
			}
			if _, ok := body["tool_choice"]; ok {
				sdkJSON(w, 200, sdkToolMessage())
				return
			}
			sdkJSON(w, 200, sdkMessage())
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := anthropic.NewClient(option.WithBaseURL(server.URL+"/"), option.WithAPIKey("key"), option.WithMaxRetries(0), option.WithHeader("anthropic-version", "2023-06-01"))
	for _, suite := range []string{"models_list", "models_get", "messages", "messages_stream", "system", "multi_turn", "tools", "tools_stream", "count_tokens", "structured_output", "error_responses"} {
		t.Run(suite, func(t *testing.T) {
			if err := run(context.Background(), &client, suite, "claude-mock"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSuiteErrorClassification(t *testing.T) {
	for _, test := range []struct {
		name, suite string
		err         error
		want        string
	}{
		{name: "transport", suite: "messages", err: errors.New("connection reset"), want: "error"},
		{name: "overloaded", suite: "messages", err: &anthropic.Error{StatusCode: 529}, want: "error"},
		{name: "optional missing", suite: "models_get", err: &anthropic.Error{StatusCode: 404}, want: "unsupported"},
		{name: "core rejection", suite: "messages", err: &anthropic.Error{StatusCode: 400}, want: "fail"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := classifySuiteError(test.suite, test.err); got != test.want {
				t.Fatalf("status=%q, want %q", got, test.want)
			}
		})
	}
}

func TestModelsGetEscapesSlashInModelID(t *testing.T) {
	var escapedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escapedPath = r.URL.EscapedPath()
		sdkJSON(w, 200, map[string]any{"id": "vendor/claude-mock", "type": "model", "display_name": "Claude Mock", "created_at": "2026-09-01T00:00:00Z"})
	}))
	defer server.Close()
	client := anthropic.NewClient(option.WithBaseURL(server.URL+"/"), option.WithAPIKey("key"), option.WithMaxRetries(0), option.WithMiddleware(escapeModelPathSegments))
	if err := run(context.Background(), &client, "models_get", "vendor/claude-mock"); err != nil {
		t.Fatal(err)
	}
	if escapedPath != "/v1/models/vendor%2Fclaude-mock" {
		t.Fatalf("escaped path=%q", escapedPath)
	}
}

func sdkMessage() map[string]any {
	return map[string]any{"id": "msg_mock", "type": "message", "role": "assistant", "model": "claude-mock", "content": []any{map[string]any{"type": "text", "text": "AIQA_OK"}}, "stop_reason": "end_turn", "stop_sequence": nil, "usage": map[string]any{"input_tokens": 7, "output_tokens": 2, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}}
}
func sdkTextMessage(text string) map[string]any {
	message := sdkMessage()
	message["content"] = []any{map[string]any{"type": "text", "text": text}}
	return message
}
func sdkToolMessage() map[string]any {
	message := sdkMessage()
	message["content"] = []any{map[string]any{"type": "tool_use", "id": "toolu_mock", "name": "get_status", "input": map[string]any{"resource_id": "resource-alpha"}}}
	message["stop_reason"] = "tool_use"
	return message
}
func sdkHasToolResult(raw any) bool {
	messages, _ := raw.([]any)
	for _, rawMessage := range messages {
		message, _ := rawMessage.(map[string]any)
		blocks, _ := message["content"].([]any)
		for _, rawBlock := range blocks {
			block, _ := rawBlock.(map[string]any)
			if block["type"] == "tool_result" {
				return true
			}
		}
	}
	return false
}
func sdkJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func sdkStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	events := []struct{ name, body string }{{"message_start", `{"type":"message_start","message":{"id":"msg_mock","type":"message","role":"assistant","model":"claude-mock","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":7,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`}, {"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`}, {"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"AIQA_OK"}}`}, {"content_block_stop", `{"type":"content_block_stop","index":0}`}, {"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}`}, {"message_stop", `{"type":"message_stop"}`}}
	for _, event := range events {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.name, strings.TrimSpace(event.body))
	}
}

func sdkToolStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	events := []struct{ name, body string }{
		{"message_start", `{"type":"message_start","message":{"id":"msg_mock","type":"message","role":"assistant","model":"claude-mock","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":7,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`},
		{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_mock","name":"get_status","input":{}}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"resource_id\":\"resource-alpha\"}"}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":0}`},
		{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":3}}`},
		{"message_stop", `{"type":"message_stop"}`},
	}
	for _, event := range events {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.name, strings.TrimSpace(event.body))
	}
}
