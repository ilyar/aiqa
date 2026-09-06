package logging

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResponseShapeFieldsAreVersionedAndDoNotRetainNestedPayloads(t *testing.T) {
	secret := "nested-provider-secret"
	fields := ResponseShapeFields(
		"POST", "/v1/messages", "application/json; charset=utf-8",
		[]byte(`{"model":"claude-test","messages":[{"content":"`+secret+`","tool":{"arguments":{"token":"`+secret+`"}}}],"prompt":"`+secret+`","unreviewed":"`+secret+`"}`),
		400, "application/json", "req_123", []byte(`{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"`+secret+`","details":[{"nested":"`+secret+`"}]},"unreviewed":{"array":["`+secret+`"]}}`),
	)
	logger := New(4)
	logger.Log(Trace, "diagnostic.response_shape", fields)
	line := logger.Lines()[0]
	if strings.Contains(line, secret) || strings.Contains(line, "prompt") {
		t.Fatalf("diagnostic leaked nested payload or prompt presence: %s", line)
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"diagnostic_schema": "aiqa.diagnostic.v1", "request_shape_keys": "messages,model", "request_shape_types": "messages:array,model:string",
		"request_unknown_key_count": float64(2), "response_shape_keys": "error", "response_unknown_key_count": float64(1),
		"error_present": true, "error_shape_keys": "code,details,message,status", "error_code": float64(400), "error_status": "invalid_argument",
	} {
		if event[key] != want {
			t.Fatalf("%s = %#v, want %#v; event=%#v", key, event[key], want, event)
		}
	}
	if _, ok := event["unreviewed"]; ok {
		t.Fatalf("unreviewed top-level field escaped logger allowlist: %#v", event)
	}
}

func TestSSEShapeFieldsNeverRetainUnapprovedNamesOrNestedPayloads(t *testing.T) {
	secret := "delta-secret"
	fields := SSEShapeFields([]SSEShapeEvent{
		{Event: "step.start", JSON: map[string]any{"event_type": "step.start", "step": map[string]any{"index": json.Number("0"), "text": secret}}},
		{Event: secret, JSON: map[string]any{"event_type": secret, "delta": map[string]any{"content": secret}}},
		{Done: true},
	}, 1, true)
	logger := New(4)
	logger.Log(Trace, "diagnostic.response_shape", fields)
	line := logger.Lines()[0]
	if strings.Contains(line, secret) {
		t.Fatalf("SSE diagnostic leaked payload or provider-controlled name: %s", line)
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"sse_event_names": "step.start,unknown", "sse_event_types": "step.start,unknown", "sse_correlation_fields": "index:number", "sse_terminal_markers": "done,stream_done", "sse_invalid_event_count": float64(1),
	} {
		if event[key] != want {
			t.Fatalf("%s = %#v, want %#v; event=%#v", key, event[key], want, event)
		}
	}
}

func TestDiagnosticUnknownFieldIsRejected(t *testing.T) {
	logger := New(4)
	fields := ResponseShapeFields("POST", "/v1/messages", "application/json", nil, 200, "application/json", "", nil)
	fields["raw_response"] = "must-not-be-written"
	logger.Log(Trace, "diagnostic.response_shape", fields)
	line := logger.Lines()[0]
	if strings.Contains(line, "raw_response") || strings.Contains(line, "must-not-be-written") {
		t.Fatalf("unknown diagnostic field was retained: %s", line)
	}
}
