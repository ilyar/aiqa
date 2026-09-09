package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/profile"
	protocolpkg "github.com/ilyar/aiqa/internal/protocol"
	"github.com/ilyar/aiqa/internal/result"
)

func TestPlannedProbeCountCountsLogicalUnitsRatherThanResultRows(t *testing.T) {
	cfg := config.Config{Tests: []string{"compat"}}
	modules := [][]result.ModuleAssessment{{
		{ID: "anthropic.messages", Applicability: result.ApplicabilityApplicable, Execution: result.ExecutionExecuted},
		{ID: "anthropic.token-counting", Applicability: result.ApplicabilityApplicable, Execution: result.ExecutionExecuted},
	}}
	if got := plannedProbeCount(cfg, []string{"fixture"}, modules, []bool{false}); got != 19 {
		t.Fatalf("planned probe count = %d, want 19 (16 message lifecycles + 3 token probes)", got)
	}
}

func TestPreparedTargetExecutesFrozenSelectionAfterOverrideMutation(t *testing.T) {
	var inventories atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected request after prepare: %s", r.URL.Path)
		}
		inventories.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"opaque","type":"model","display_name":"Opaque","created_at":"2026-01-01T00:00:00Z"}],"has_more":false}`))
	}))
	defer server.Close()

	capabilityFile := filepath.Join(t.TempDir(), "capabilities.json")
	if err := os.WriteFile(capabilityFile, []byte(`{"models":{"opaque":["anthropic.messages.text"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	options := protocolpkg.RunOptions{Target: config.TargetConfig{ID: "fixture", Protocol: config.ProtocolAnthropic, BaseURL: server.URL, APIVersion: "2023-06-01", Models: []string{"opaque"}, CapabilityFile: capabilityFile}, Config: config.Config{Concurrency: 1, Timeout: time.Second}, Profile: p}
	prepared, err := PrepareTarget(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if got := prepared.Binding.SelectedModels; len(got) != 1 || got[0] != "opaque" {
		t.Fatalf("frozen selected models = %v, want opaque", got)
	}
	if got := prepared.Binding.ResolvedOverrides; len(got) != 1 || got[0] != "opaque=anthropic.messages.text" {
		t.Fatalf("frozen overrides = %v", got)
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

func TestPreparedTargetDoesNotRepeatFrozenPreflight(t *testing.T) {
	var inventories, messages atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			inventories.Add(1)
			_, _ = w.Write([]byte(`{"data":[{"id":"opaque","type":"model","display_name":"Opaque","created_at":"2026-01-01T00:00:00Z"}],"has_more":false}`))
		case "/v1/messages":
			messages.Add(1)
			_, _ = w.Write([]byte(`{"id":"msg_fixture","type":"message","role":"assistant","content":[{"type":"text","text":"AIQA_OK"}],"model":"opaque","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	options := protocolpkg.RunOptions{Target: config.TargetConfig{ID: "fixture", Protocol: config.ProtocolAnthropic, BaseURL: server.URL, APIVersion: "2023-06-01", Models: []string{"opaque"}}, Config: config.Config{Concurrency: 1, Timeout: time.Second}, Profile: p}
	prepared, err := PrepareTarget(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Execute(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := inventories.Load(); got != 1 {
		t.Fatalf("inventories=%d, want 1", got)
	}
	if got := messages.Load(); got != 1 {
		t.Fatalf("preflight requests=%d, want only Prepare's frozen request", got)
	}
}
