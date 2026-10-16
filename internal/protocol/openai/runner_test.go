package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestPrepareRejectsOversizedInventoryBeforePlanning(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}` + strings.Repeat(" ", 1024)))
	}))
	defer server.Close()
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	_, err = PrepareTarget(context.Background(), protocolpkg.RunOptions{
		Target: config.TargetConfig{ID: "fixture", Protocol: config.ProtocolOpenAI, BaseURL: server.URL, APIKey: "fixture-key"},
		Config: config.Config{Tests: []string{"compat"}, Concurrency: 1, Timeout: time.Second, InventoryByteBudget: 64}, Profile: p,
	})
	if err == nil {
		t.Fatal("oversized whitespace inventory was accepted")
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d, want only inventory request", requests.Load())
	}
}

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

func TestProbePolicyImageRejectionLeavesChatEvidenceAndReclassifiesOnlyImages(t *testing.T) {
	modules := []result.ModuleAssessment{
		{ID: capability.ModuleChat, Applicability: result.ApplicabilityApplicable, Execution: result.ExecutionExecuted, Source: "probe_policy"},
		{ID: capability.ModuleImages, Applicability: result.ApplicabilityApplicable, Execution: result.ExecutionExecuted, Source: "probe_policy"},
	}
	chat := result.TestResult{ID: "chat.completions.basic", Status: result.StatusPass}
	image := []result.TestResult{{
		ID: "images.generate.basic", Status: result.StatusFail,
		HTTP: result.HTTPObservation{StatusCode: http.StatusBadRequest, Snippet: `{"error":{"code":"invalid_value","param":"model","type":"image_generation_user_error"}}`},
	}}
	imageModule := capability.Module(modules, capability.ModuleImages)
	if !resolveProbePolicyImageRejection(capability.Classification{ProbeEligible: true}, imageModule, image) {
		t.Fatal("structured image rejection was not recognized")
	}
	if chat.Status != result.StatusPass || modules[0].Applicability != result.ApplicabilityApplicable || modules[1].Applicability != result.ApplicabilityNotApplicable || modules[1].Execution != result.ExecutionExecuted || modules[1].Source != "probe_policy" {
		t.Fatalf("unexpected module/test state: chat=%+v modules=%+v image=%+v", chat, modules, image)
	}
}

func TestProbePolicyImageRejectionDoesNotApplyToPositiveEvidence(t *testing.T) {
	modules := []result.ModuleAssessment{{ID: capability.ModuleImages, Applicability: result.ApplicabilityApplicable, Execution: result.ExecutionExecuted, Source: "model_id"}}
	image := []result.TestResult{{
		ID: "images.generate.basic", Status: result.StatusFail,
		HTTP: result.HTTPObservation{StatusCode: http.StatusBadRequest, Snippet: `{"error":{"code":"invalid_value","param":"model","type":"image_generation_user_error"}}`},
	}}
	imageModule := capability.Module(modules, capability.ModuleImages)
	if resolveProbePolicyImageRejection(capability.Classification{ProbeEligible: true}, imageModule, image) {
		t.Fatal("positive evidence would have been reclassified")
	}
	if image[0].Status != result.StatusFail || modules[0].Applicability != result.ApplicabilityApplicable {
		t.Fatalf("positive-evidence rejection was altered: module=%+v test=%+v", modules[0], image[0])
	}
}

func TestPrepareUnknownInventoryModelUsesProbePolicyWithoutPreflight(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("probe policy unexpectedly preflighted %q", r.URL.Path)
		}
		requests.Add(1)
		_, _ = w.Write([]byte(`{"data":[{"id":"opaque","object":"model","created":1}]}`))
	}))
	defer server.Close()
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareTarget(context.Background(), protocolpkg.RunOptions{
		Target: config.TargetConfig{ID: "fixture", Protocol: config.ProtocolOpenAI, BaseURL: server.URL, APIKey: "fixture-key", Models: []string{"opaque"}, UnknownModel: "probe"},
		Config: config.Config{Tests: []string{"compat"}, Concurrency: 1, Timeout: time.Second}, Profile: p,
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d, want inventory only", requests.Load())
	}
	foundCompat := false
	for _, job := range prepared.Jobs {
		foundCompat = foundCompat || job.ID == protocolpkg.JobID("fixture", "opaque", "compat")
	}
	if !foundCompat {
		t.Fatalf("probe-policy native job was not frozen: %+v", prepared.Jobs)
	}
	if len(prepared.Candidates) != 1 || len(prepared.Candidates[0].Capabilities) != 0 {
		t.Fatalf("probe policy fabricated model capabilities: %+v", prepared.Candidates)
	}
	var chat *result.ModuleAssessment
	for index := range prepared.Candidates[0].Modules {
		if prepared.Candidates[0].Modules[index].ID == "chat-completions" {
			chat = &prepared.Candidates[0].Modules[index]
		}
	}
	if chat == nil || chat.Source != "probe_policy" || !chat.Requested || chat.Execution != result.ExecutionExecuted {
		t.Fatalf("probe-policy chat module=%+v", chat)
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
