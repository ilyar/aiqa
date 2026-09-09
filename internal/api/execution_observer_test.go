package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/scheduler"
)

func TestExecutionObserverRawAttemptHeadersThenOneTerminal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL+"/v1", "", "test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var events []ExecutionEvent
	client.SetExecutionObserver(func(event ExecutionEvent) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})
	ctx := WithExecutionOwner(context.Background(), ExecutionOwner{TargetID: "target-a", PreparationID: "models-discovery"})
	if _, err := client.DoJSON(ctx, http.MethodGet, "models", nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 3 {
		t.Fatalf("events=%#v", events)
	}
	if events[0].Kind != ExecutionRequestAdmitted || events[1].Kind != ExecutionRequestHeaders || events[2].Kind != ExecutionRequestTerminal {
		t.Fatalf("lifecycle=%#v", events)
	}
	if events[0].AttemptID == "" || events[0].AttemptID != events[1].AttemptID || events[1].AttemptID != events[2].AttemptID {
		t.Fatalf("attempt identity not stable: %#v", events)
	}
	if events[2].Outcome != "succeeded" || events[2].TargetID != "target-a" || events[2].PreparationID != "models-discovery" || events[2].JobID != "" {
		t.Fatalf("unsafe/incorrect owner or terminal: %#v", events[2])
	}
}

func TestExecutionObserverSDKTerminalFollowsBodyClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "", "test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var events []ExecutionEvent
	client.SetExecutionObserver(func(event ExecutionEvent) { mu.Lock(); events = append(events, event); mu.Unlock() })
	request, err := http.NewRequestWithContext(WithExecutionOwner(context.Background(), ExecutionOwner{TargetID: "sdk-target", JobID: "frozen-job"}), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.PacedHTTPClient("model").Do(request)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(events) != 2 || events[0].Kind != ExecutionRequestAdmitted || events[1].Kind != ExecutionRequestHeaders {
		mu.Unlock()
		t.Fatalf("terminal emitted before body close: %#v", events)
	}
	mu.Unlock()
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 3 || events[2].Kind != ExecutionRequestTerminal || events[2].Outcome != "succeeded" || events[2].AttemptID != events[0].AttemptID || events[2].JobID != "frozen-job" {
		t.Fatalf("SDK lifecycle=%#v", events)
	}
}

func TestExecutionObserverSSETerminatesOnceAfterConsumption(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "", "test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var events []ExecutionEvent
	client.SetExecutionObserver(func(event ExecutionEvent) { mu.Lock(); events = append(events, event); mu.Unlock() })
	ctx := WithExecutionOwner(context.Background(), ExecutionOwner{TargetID: "stream-target", JobID: "stream-job"})
	if _, err := client.StreamJSONForModel(ctx, "stream", map[string]any{"model": "model"}, "model"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 3 || events[0].Kind != ExecutionRequestAdmitted || events[1].Kind != ExecutionRequestHeaders || events[2].Kind != ExecutionRequestTerminal || events[2].Outcome != "succeeded" {
		t.Fatalf("SSE lifecycle=%#v", events)
	}
	if events[0].AttemptID == "" || events[0].AttemptID != events[1].AttemptID || events[1].AttemptID != events[2].AttemptID {
		t.Fatalf("SSE attempt identity=%#v", events)
	}
}

func TestSchedulerFactObserverDecodesOpaqueOwnerForPhysicalAttempt(t *testing.T) {
	broker, err := scheduler.New(scheduler.Config{Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient("https://provider.example/v1", "", "test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetScheduler(SchedulerConfig{Broker: broker, Target: "target-a", Group: "group-a"}); err != nil {
		t.Fatal(err)
	}
	var events []ExecutionEvent
	client.SetExecutionObserver(func(event ExecutionEvent) { events = append(events, event) })
	broker.SetFactObserver(client.SchedulerFactObserver())
	owner := ExecutionOwner{TargetID: "target-a", JobID: "native:target-a:model:compat"}
	lease, err := client.acquireScheduler(WithExecutionOwner(context.Background(), owner), 0)
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	if len(events) != 2 || events[0].Kind != ExecutionRequestAdmitted || events[1].Kind != ExecutionRequestTerminal {
		t.Fatalf("scheduler bridge events=%#v", events)
	}
	for _, event := range events {
		if event.TargetID != owner.TargetID || event.JobID != owner.JobID || event.PreparationID != "" {
			t.Fatalf("opaque owner did not decode: %#v", events)
		}
	}
}
