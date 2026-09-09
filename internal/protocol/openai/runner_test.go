package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/capability"
	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/profile"
	protocolpkg "github.com/ilyar/aiqa/internal/protocol"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/suites"
)

func TestRepresentativeModelSkipsEarlierInapplicableModels(t *testing.T) {
	models := []string{"aaa-image", "bbb-unknown", "ccc-chat"}
	plans := [][]result.ModuleAssessment{
		{{ID: "chat-completions", Applicability: result.ApplicabilityNotApplicable}},
		{{ID: "chat-completions", Applicability: result.ApplicabilityUnknown}},
		{{ID: "chat-completions", Applicability: result.ApplicabilityApplicable, Execution: result.ExecutionExecuted}},
	}
	if got := representativeModel(models, plans, "chat-completions"); got != "ccc-chat" {
		t.Fatalf("representative model=%q, want ccc-chat", got)
	}
}

func TestRunTargetDiscoveryUsesFrozenInventoryJobOwner(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"opaque","object":"model","created":1}]}`))
	}))
	defer server.Close()
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	capabilityFile := filepath.Join(t.TempDir(), "capabilities.json")
	if err := os.WriteFile(capabilityFile, []byte(`{"models":{"opaque":["chat.completions.text"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var events []api.ExecutionEvent
	options := protocolpkg.RunOptions{
		Target: config.TargetConfig{ID: "fixture", Protocol: config.ProtocolOpenAI, BaseURL: server.URL, Models: []string{"opaque"}, CapabilityFile: capabilityFile},
		Config: config.Config{Concurrency: 1, Timeout: time.Second}, Profile: p,
		ExecutionHooks: protocolpkg.ExecutionHooks{Observe: func(event api.ExecutionEvent) {
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
		}},
	}
	if _, err := RunTarget(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 3 {
		t.Fatalf("events=%#v", events)
	}
	want := protocolpkg.JobID("fixture", "", "inventory")
	for _, event := range events {
		if event.JobID != want || event.PreparationID != "" {
			t.Fatalf("discovery did not retain frozen inventory owner: %#v", events)
		}
	}
}

func TestPlannedNativeProbesCountsLifecycleUnitsOnly(t *testing.T) {
	cfg := config.Config{Tests: []string{"compat"}}
	plans := [][]result.ModuleAssessment{
		{{ID: capability.ModuleChat, Execution: result.ExecutionExecuted}, {ID: capability.ModuleImages, Execution: result.ExecutionExecuted}},
		{{ID: capability.ModuleChat, Execution: result.ExecutionExecuted}},
	}
	want := suites.EndpointCompatProbeCount + suites.ModelCompatProbeCount*2 + suites.ImageCompatProbeCount
	if got := plannedNativeProbes(cfg, plans, []bool{false, false}, "model-a"); got != want {
		t.Fatalf("planned probes = %d, want %d", got, want)
	}
	if got := plannedNativeProbes(cfg, plans, []bool{true, false}, "model-b"); got != suites.EndpointCompatProbeCount+suites.ModelCompatProbeCount {
		t.Fatalf("route-blocked plan = %d", got)
	}
}

func TestPreparedTargetExecutesFrozenSelectionAfterOverrideMutation(t *testing.T) {
	var inventories atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("unexpected request after prepare: %s", r.URL.Path)
		}
		inventories.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"opaque","object":"model","created":1}]}`))
	}))
	defer server.Close()

	capabilityFile := filepath.Join(t.TempDir(), "capabilities.json")
	if err := os.WriteFile(capabilityFile, []byte(`{"models":{"opaque":["chat.completions.text"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	options := protocolpkg.RunOptions{
		Target: config.TargetConfig{ID: "fixture", Protocol: config.ProtocolOpenAI, BaseURL: server.URL, Models: []string{"opaque"}, CapabilityFile: capabilityFile},
		Config: config.Config{Concurrency: 1, Timeout: time.Second}, Profile: p,
	}
	prepared, err := PrepareTarget(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if got := prepared.Binding.SelectedModels; len(got) != 1 || got[0] != "opaque" {
		t.Fatalf("frozen selected models = %v, want opaque", got)
	}
	if got := prepared.Binding.ResolvedOverrides; len(got) != 1 || got[0] != "opaque=chat.completions.text" {
		t.Fatalf("frozen overrides = %v, want opaque=chat.completions.text", got)
	}
	if err := os.WriteFile(capabilityFile, []byte(`not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Execute(context.Background(), nil); err != nil {
		t.Fatalf("execute reread mutated override or selection: %v", err)
	}
	if got := inventories.Load(); got != 1 {
		t.Fatalf("model inventories = %d, want exactly one during prepare", got)
	}
}
