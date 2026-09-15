package app

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// This catalog has the same metadata shape as Dahl: standard Model fields,
// without a provider-specific capabilities extension.
func TestMissingMetadataRunsNativeAndSDKWithAdvisory(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		name := "inventory selection"
		if explicit {
			name = "explicit selection"
		}
		t.Run(name, func(t *testing.T) {
			var chatCalls atomic.Int32
			server := newMockOpenAIWithMiddleware(t, func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/v1/models" {
						writeJSON(w, 200, map[string]any{"object": "list", "data": []any{map[string]any{
							"id": "vendor/model-a", "object": "model", "created": 1, "owned_by": "fixture",
						}}})
						return
					}
					if r.URL.Path == "/v1/chat/completions" {
						chatCalls.Add(1)
					}
					next.ServeHTTP(w, r)
				})
			})
			defer server.Close()
			withTempWorkingDirectory(t)
			cfg := testConfig(server.URL+"/v1", []string{"compat", "sdk"})
			cfg.UnknownModel = "probe"
			if explicit {
				cfg.Models = []string{"vendor/model-a"}
			}
			out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
			if err != nil {
				t.Fatal(err)
			}
			if out.ExitCode != 0 || chatCalls.Load() == 0 {
				t.Fatalf("metadata blocked tests: exit=%d calls=%d", out.ExitCode, chatCalls.Load())
			}
			target := out.ReportV3.Targets[0]
			if len(target.Models[0].Capabilities) != 0 {
				t.Fatal("probe eligibility fabricated confirmed capabilities")
			}
			chatSuitePassed := false
			for _, extension := range target.Extensions {
				for _, suite := range extension.Suites {
					if suite.SuiteID == "chat.completions" && suite.Status == "pass" {
						chatSuitePassed = true
					}
				}
			}
			if !chatSuitePassed {
				t.Fatalf("SDK only ran discovery: %+v", target.Extensions)
			}
			if len(target.Notes) != 1 || !strings.Contains(target.Notes[0], "capability metadata is absent") {
				t.Fatalf("missing advisory: %v", target.Notes)
			}
			for _, path := range []string{"report.json", "report.md"} {
				data, err := os.ReadFile(path)
				if err != nil || !strings.Contains(string(data), "capability metadata is absent") {
					t.Fatalf("advisory absent in %s: %v", path, err)
				}
			}
		})
	}
}

func TestInventoryPolicyStillLeavesOpaqueCatalogUntested(t *testing.T) {
	var unexpected atomic.Int32
	server := newMockOpenAIWithMiddleware(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/models" {
				writeJSON(w, 200, map[string]any{"object": "list", "data": []any{map[string]any{"id": "opaque", "object": "model", "created": 1, "owned_by": "fixture"}}})
				return
			}
			unexpected.Add(1)
			next.ServeHTTP(w, r)
		})
	})
	defer server.Close()
	withTempWorkingDirectory(t)
	cfg := testConfig(server.URL+"/v1", []string{"compat", "sdk"})
	cfg.UnknownModel = "inventory"
	cfg.Formats = nil
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if unexpected.Load() != 0 || out.ReportV3.Targets[0].Summary.Assessment.Verdict != "inconclusive" {
		t.Fatal("explicit inventory policy changed")
	}
}
