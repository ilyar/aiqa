package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
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
