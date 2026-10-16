package app

import (
	"context"
	"net/http"
	"testing"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/result"
)

func TestAnthropicPartialCapabilitiesFreezeNativeAndSDKToolPlan(t *testing.T) {
	server := newMockAnthropicWithMiddleware(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/anthropic/v1/models" || r.URL.Path == "/anthropic/v1/models/claude-mock" {
				model := anthropicModel("claude-mock")
				delete(model["capabilities"].(map[string]any), "tools")
				if r.URL.Path == "/anthropic/v1/models" {
					writeJSON(w, http.StatusOK, map[string]any{"data": []any{model}, "has_more": false, "first_id": "claude-mock", "last_id": "claude-mock"})
				} else {
					writeJSON(w, http.StatusOK, model)
				}
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	defer server.Close()
	withTempWorkingDirectory(t)
	t.Setenv("ANTHROPIC_BASE_URL", server.URL+"/anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-test-key")
	cfg, err := config.Parse([]string{"test=compat", "test=sdk", "--timeout=5", "--request-interval=1ms"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AnthropicUnknownModel != "probe" {
		t.Fatalf("Anthropic probe policy=%q, want probe", cfg.AnthropicUnknownModel)
	}
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.ReportV3.Targets) != 1 || len(out.ReportV3.Targets[0].Models) != 1 {
		t.Fatalf("unexpected Anthropic report=%+v", out.ReportV3)
	}
	model := out.ReportV3.Targets[0].Models[0]
	for _, evidence := range model.Capabilities {
		if evidence.ID == "anthropic.messages.tools" {
			t.Fatalf("probe policy fabricated advertised tools support: %+v", model.Capabilities)
		}
	}
	tools := moduleByID(model.Modules, "anthropic.tools")
	if tools == nil || !tools.Requested || tools.Execution != result.ExecutionExecuted || tools.Source != "probe_policy" {
		t.Fatalf("frozen native tools plan=%+v", tools)
	}
	sdkTools, sdkToolsStream := false, false
	for _, extension := range out.ReportV3.Targets[0].Extensions {
		for _, suite := range extension.Suites {
			if suite.ExtensionID == "official-sdk.anthropic" && suite.SuiteID == "tools" && suite.Status == "pass" {
				sdkTools = true
			}
			if suite.ExtensionID == "official-sdk.anthropic" && suite.SuiteID == "tools.stream" && suite.Status == "pass" {
				sdkToolsStream = true
			}
		}
	}
	if !sdkTools || !sdkToolsStream {
		t.Fatalf("frozen SDK tools plan missing: tools=%t stream=%t extensions=%+v", sdkTools, sdkToolsStream, out.ReportV3.Targets[0].Extensions)
	}
}
