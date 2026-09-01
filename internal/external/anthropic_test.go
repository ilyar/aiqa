package external

import (
	"runtime"
	"strings"
	"testing"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/result"
)

func TestParseAnthropicJSONLPreservesValidEntriesAroundBrokenLine(t *testing.T) {
	raw := "{\"schema_version\":\"aiqa.external.v1\",\"tool\":\"anthropic-official-sdk\",\"tool_version\":\"v1.68.0\",\"suite\":\"messages\",\"status\":\"pass\",\"duration_ms\":12,\"summary\":\"ok\"}\nnot-json\n{\"schema_version\":\"aiqa.external.v1\",\"tool\":\"anthropic-official-sdk\",\"tool_version\":\"v1.68.0\",\"suite\":\"count_tokens\",\"status\":\"fail\",\"summary\":\"secret failed\"}\n"
	got, err := parseAnthropicJSONL(raw, "model", "secret")
	if err == nil || len(got) != 2 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if got[0].Status != result.StatusPass || got[1].Summary != "[REDACTED] failed" {
		t.Fatalf("unexpected parsed entries: %+v", got)
	}
	if len(got[1].RequirementIDs) == 0 || len(got[1].RelatedRequirementIDs) == 0 {
		t.Fatalf("requirement mappings missing: %+v", got[1])
	}
}

func TestAnthropicDockerArgsUseHostNetworkForLinuxLoopback(t *testing.T) {
	args := anthropicDockerArgs(config.TargetConfig{BaseURL: "http://127.0.0.1:8080/anthropic", ExternalImage: "checker:test"})
	joined := strings.Join(args, " ")
	if runtime.GOOS == "linux" && !strings.Contains(joined, "--network host") {
		t.Fatalf("Linux loopback Docker args lack host networking: %q", joined)
	}
	if !strings.HasSuffix(joined, "checker:test") || !strings.Contains(joined, "-e ANTHROPIC_BASE_URL") {
		t.Fatalf("Docker args lost explicit environment wiring: %q", joined)
	}
	remote := strings.Join(anthropicDockerArgs(config.TargetConfig{BaseURL: "https://api.example.test", ExternalImage: "checker:test"}), " ")
	if strings.Contains(remote, "--network host") {
		t.Fatalf("remote Docker target unexpectedly uses host networking: %q", remote)
	}
}

func TestCleanAnthropicEnvironmentDropsAmbientSDKControls(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "ambient-token")
	t.Setenv("ANTHROPIC_BETA", "ambient-beta")
	t.Setenv("ANTHROPIC_API_KEY", "ambient-key")
	env := cleanAnthropicEnvironment(config.TargetConfig{BaseURL: "https://example.test", APIKey: "explicit-key"}, "messages", "model")
	joined := strings.Join(env, "\n")
	for _, forbidden := range []string{"ambient-token", "ambient-beta", "ambient-key"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("ambient value leaked into checker environment: %s", forbidden)
		}
	}
	for _, required := range []string{"ANTHROPIC_BASE_URL=https://example.test", "ANTHROPIC_API_KEY=explicit-key", "ANTHROPIC_MODEL=model", "AIQA_SUITES=messages"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("missing explicit value %q", required)
		}
	}
}

func TestParseAnthropicJSONLRejectsUnknownSuiteAndStatus(t *testing.T) {
	raw := strings.Join([]string{
		`{"schema_version":"aiqa.external.v1","tool":"anthropic-official-sdk","tool_version":"v1.68.0","suite":"future_suite","status":"pass","summary":"ok"}`,
		`{"schema_version":"aiqa.external.v1","tool":"anthropic-official-sdk","tool_version":"v1.68.0","suite":"messages","status":"maybe","summary":"ok"}`,
	}, "\n")
	got, err := parseAnthropicJSONL(raw, "model", "key")
	if err == nil || len(got) != 0 {
		t.Fatalf("got=%+v err=%v, want rejected envelopes", got, err)
	}
}
