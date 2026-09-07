package telemetry

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ilyar/aiqa/internal/logging"
)

func TestOperationCorrelatesLifecycleWithoutPropagation(t *testing.T) {
	logger := logging.New(4)
	provider := New(logger)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, op := provider.Start(logging.Scope(context.Background(), "openai", "gpt-test", "compat", "chat.basic", ""), "probe", logging.Fields{"method": "POST"})
	op.Event(logging.Trace, "attempt", logging.Fields{"attempt": 1, "path": "/v1/responses"})
	op.End("success", logging.Fields{"http_status": 200})
	if got := len(logger.Lines()); got != 3 {
		t.Fatalf("events = %d, want 3", got)
	}
	var first, second map[string]any
	if err := json.Unmarshal([]byte(logger.Lines()[0]), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(logger.Lines()[1]), &second); err != nil {
		t.Fatal(err)
	}
	if first["event"] != "probe.start" || second["event"] != "probe.attempt" {
		t.Fatalf("unexpected events: %#v %#v", first, second)
	}
	if first["operation_id"] == "" || first["trace_id"] == "" || first["span_id"] == "" {
		t.Fatalf("missing correlation: %#v", first)
	}
	if first["operation_id"] != second["operation_id"] || first["trace_id"] != second["trace_id"] {
		t.Fatalf("correlation changed: %#v %#v", first, second)
	}
	if span := provider.TracerProvider(); span == nil {
		t.Fatal("nil tracer provider")
	}
	if got := logging.FieldsFromContext(ctx)["operation_id"]; got != first["operation_id"] {
		t.Fatalf("context operation_id = %#v, want %#v", got, first["operation_id"])
	}
}

func TestEndIsIdempotentAndPayloadIsRedacted(t *testing.T) {
	logger := logging.New(4)
	provider := New(logger)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	_, op := provider.Start(context.Background(), "http.attempt", logging.Fields{"body": "secret"})
	op.End("failure", logging.Fields{"response_body": "also-secret"})
	op.End("success", nil)
	if got := len(logger.Lines()); got != 2 {
		t.Fatalf("events = %d, want 2", got)
	}
	if string(logger.Lines()[0]) == "" || contains(logger.Lines()[0], "secret") || contains(logger.Lines()[0], `"body"`) {
		t.Fatalf("unsafe start: %s", logger.Lines()[0])
	}
}

func TestObserverReceivesLifecycleWhenDiagnosticLoggingIsQuiet(t *testing.T) {
	observer := &recordingObserver{}
	provider := New(logging.New(0), observer)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	_, op := provider.Start(logging.Scope(context.Background(), "openai", "", "compat", "chat.basic", ""), "probe", nil)
	op.End("pass", nil)
	if len(observer.events) != 2 {
		t.Fatalf("events = %d, want start and end", len(observer.events))
	}
	if got := observer.events[1]; got.Name != "probe" || got.Phase != "end" || got.OperationID == "" || got.Target != "openai" || got.Outcome != "pass" {
		t.Fatalf("unexpected observer event: %#v", got)
	}
	if got := len(provider.logger.Lines()); got != 0 {
		t.Fatalf("quiet logger wrote %d lines", got)
	}
}

type recordingObserver struct{ events []LifecycleEvent }

func (o *recordingObserver) ObserveLifecycle(event LifecycleEvent) {
	o.events = append(o.events, event)
}

func contains(text, needle string) bool {
	for index := 0; index+len(needle) <= len(text); index++ {
		if text[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}
