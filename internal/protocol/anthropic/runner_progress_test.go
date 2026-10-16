package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/api"
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

func TestRunTargetDiscoveryUsesFrozenInventoryJobOwner(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"opaque","type":"model"}],"has_more":false}`))
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
	var mu sync.Mutex
	var events []api.ExecutionEvent
	options := protocolpkg.RunOptions{Target: config.TargetConfig{ID: "fixture", Protocol: config.ProtocolAnthropic, BaseURL: server.URL, APIVersion: "2023-06-01", Models: []string{"opaque"}, CapabilityFile: capabilityFile}, Config: config.Config{Concurrency: 1, Timeout: time.Second}, Profile: p, ExecutionHooks: protocolpkg.ExecutionHooks{Observe: func(event api.ExecutionEvent) { mu.Lock(); events = append(events, event); mu.Unlock() }}}
	if _, err := RunTarget(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := protocolpkg.JobID("fixture", "", "inventory")
	if len(events) != 3 {
		t.Fatalf("events=%#v", events)
	}
	for _, event := range events {
		if event.JobID != want || event.PreparationID != "" {
			t.Fatalf("inventory owner=%#v", events)
		}
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

func TestStrictSchemaProbeSeparatesCompleteSchemaFailuresFromTruncation(t *testing.T) {
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		input       string
		secondInput string
		stopReason  string
		wantStatus  result.Status
		wantSignal  string
	}{
		{name: "complete valid input", input: `{"resource_id":"resource-alpha"}`, stopReason: "end_turn", wantStatus: result.StatusPass},
		{name: "complete invalid input", input: `{}`, stopReason: "end_turn", wantStatus: result.StatusFail},
		{name: "complete input with extra property", input: `{"resource_id":"resource-alpha","unexpected":true}`, stopReason: "end_turn", wantStatus: result.StatusFail},
		{name: "complete numeric input", input: `{"resource_id":123}`, stopReason: "end_turn", wantStatus: result.StatusFail},
		{name: "complete null input", input: `{"resource_id":null}`, stopReason: "end_turn", wantStatus: result.StatusFail},
		{name: "complete array input", input: `{"resource_id":["resource-alpha"]}`, stopReason: "end_turn", wantStatus: result.StatusFail},
		{name: "complete second malformed input", input: `{"resource_id":"resource-alpha"}`, secondInput: `{}`, stopReason: "end_turn", wantStatus: result.StatusFail},
		{name: "truncated input", input: `{}`, stopReason: "max_tokens", wantStatus: result.StatusError, wantSignal: "execution.output_truncated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var strictRequests int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/messages" {
					t.Fatalf("path=%q", r.URL.Path)
				}
				var request map[string]any
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				w.Header().Set("Content-Type", "application/json")
				if _, structured := request["output_config"]; structured {
					_, _ = w.Write([]byte(`{"model":"fixture","type":"message","role":"assistant","content":[{"type":"text","text":"{\"ok\":true,\"value\":\"AIQA_OK\"}"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
					return
				}
				tools, _ := request["tools"].([]any)
				tool, _ := tools[0].(map[string]any)
				if tool["strict"] != true {
					t.Fatalf("strict tool=%#v", tool)
				}
				if maxTokens, ok := request["max_tokens"].(float64); !ok || maxTokens != 128 {
					t.Fatalf("strict max_tokens=%#v, want 128", request["max_tokens"])
				}
				strictRequests++
				content := `[{"type":"tool_use","id":"toolu_fixture","name":"get_status","input":` + tc.input + `}]`
				if tc.secondInput != "" {
					content = `[{"type":"tool_use","id":"toolu_fixture","name":"get_status","input":` + tc.input + `},{"type":"tool_use","id":"toolu_second","name":"get_status","input":` + tc.secondInput + `}]`
				}
				_, _ = w.Write([]byte(`{"model":"fixture","type":"message","role":"assistant","content":` + content + `,"stop_reason":"` + tc.stopReason + `","usage":{"input_tokens":1,"output_tokens":1}}`))
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "key", StableAPIVersion, "aiqa/test", 0)
			if err != nil {
				t.Fatal(err)
			}
			got := runStructured(context.Background(), client, time.Second, p, "fixture", nil)
			strict := got[len(got)-1]
			if strict.Status != tc.wantStatus {
				t.Fatalf("strict status=%s summary=%q, want %s", strict.Status, strict.Summary, tc.wantStatus)
			}
			if tc.wantSignal != "" && (len(strict.Signals) != 1 || strict.Signals[0].Code != tc.wantSignal) {
				t.Fatalf("strict signals=%#v", strict.Signals)
			}
			if strictRequests != 1 {
				t.Fatalf("strict requests=%d, want 1", strictRequests)
			}
		})
	}
}
