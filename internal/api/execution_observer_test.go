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

func TestExecutionAttemptIDsDoNotCollideAcrossClientClones(t *testing.T) {
	client, err := NewClient("https://provider.example/v1", "", "test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	clone := client.CloneWithHeaders(http.Header{"X-Test": []string{"clone"}})
	client.SetExecutionObserver(func(ExecutionEvent) {})
	clone.SetExecutionObserver(func(ExecutionEvent) {})
	owner := WithExecutionOwner(context.Background(), ExecutionOwner{TargetID: "target-a", JobID: "frozen-job"})
	first, second := client.newExecutionAttempt(owner), clone.newExecutionAttempt(owner)
	if first == nil || second == nil || first.event.AttemptID == second.event.AttemptID {
		t.Fatalf("clone attempt IDs collide: first=%#v second=%#v", first, second)
	}
}

func TestSchedulerFactObserverDecodesOpaqueOwnerForWait(t *testing.T) {
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
	var got []struct {
		fact  scheduler.Fact
		owner ExecutionOwner
	}
	client.SetSchedulerExecutionObserver(func(fact scheduler.Fact, owner ExecutionOwner) {
		got = append(got, struct {
			fact  scheduler.Fact
			owner ExecutionOwner
		}{fact, owner})
	})
	owner := ExecutionOwner{TargetID: "target-a", JobID: "native:target-a:model:compat"}
	key := client.schedulerOwner(WithExecutionOwner(context.Background(), owner), owner.TargetID)
	client.SchedulerFactObserver()(scheduler.Fact{Kind: scheduler.FactWaitStarted, Owner: key, WaitID: "wait-1", Group: "group-a"})
	if len(got) != 1 || got[0].owner != owner || got[0].fact.WaitID != "wait-1" {
		t.Fatalf("scheduler wait bridge=%#v", got)
	}
}
