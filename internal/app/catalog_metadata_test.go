package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRerankCatalogDoesNotProbeChatAndExplainsProfileMismatch(t *testing.T) {
	var unexpected atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			unexpected.Add(1)
			writeError(w, 500, "unexpected", "rerank-only catalog must not cause chat probes")
			return
		}
		writeJSON(w, 200, map[string]any{"object": "list", "data": []any{
			map[string]any{"id": "catalog-reranker", "object": "model", "created": 1, "owned_by": "fixture", "capabilities": []any{"rerank"}},
		}})
	}))
	defer server.Close()
	withTempWorkingDirectory(t)
	cfg := testConfig(server.URL+"/v1", []string{"compat"})
	cfg.Models = []string{"catalog-reranker"}
	cfg.Formats = []string{"markdown", "json"}
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if unexpected.Load() != 0 {
		t.Fatalf("unexpected model requests: %d", unexpected.Load())
	}
	if out.ExitCode != 1 || out.ReportV3.Targets[0].Summary.Assessment.Verdict != "inconclusive" {
		t.Fatalf("unexecuted profile must not pass: %+v", out.ReportV3.Targets[0].Summary)
	}
	if len(out.ReportV3.Targets[0].Models) != 1 {
		t.Fatal("catalog model disappeared")
	}
	for _, path := range []string{"report.md", "report.json"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "Select a compatibility profile that matches advertised APIs") {
			t.Fatalf("%s has no actionable profile recommendation", path)
		}
		if strings.Contains(string(data), "Resolve the selected model's capability metadata") {
			t.Fatalf("%s incorrectly recommends metadata already supplied", path)
		}
	}
}
