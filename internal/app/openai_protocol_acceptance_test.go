package app

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/result"
)

// TestOpenAIProtocolAcceptanceContract is intentionally a black-box test of
// the public multi-target runner. It freezes the observable OpenAI behavior so
// moving the implementation behind protocol.Registry cannot silently drop,
// duplicate, reclassify, or relocate compatibility evidence.
func TestOpenAIProtocolAcceptanceContract(t *testing.T) {
	server := newMockOpenAI(t)
	defer server.Close()

	cfg := testConfig("", []string{"compat"})
	cfg.Formats = nil
	cfg.Targets = []config.TargetConfig{{
		ID:              "openai-acceptance",
		Protocol:        config.ProtocolOpenAI,
		BaseURL:         server.URL + "/v1",
		APIKey:          "test-key",
		ProfileFile:     "openai-compat-2026-09.json",
		Modules:         []string{"auto"},
		UnknownModel:    "inventory",
		RequestInterval: time.Millisecond,
	}}

	out, err := Run(context.Background(), cfg, BuildInfo{Version: "acceptance"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ExitCode != 0 || out.ReportV3 == nil {
		t.Fatalf("OpenAI acceptance run failed: exit=%d report=%+v", out.ExitCode, out.ReportV3)
	}
	if len(out.ReportV3.Targets) != 1 {
		t.Fatalf("targets=%d, want 1", len(out.ReportV3.Targets))
	}
	target := out.ReportV3.Targets[0]
	if target.ID != "openai-acceptance" || target.Protocol != "openai" {
		t.Fatalf("unexpected target identity: id=%q protocol=%q", target.ID, target.Protocol)
	}
	if target.Profile.SchemaVersion != "aiqa.profile.v3" || target.Profile.ID != "openai-compat" || target.Profile.Protocol != "openai" {
		t.Fatalf("unexpected profile identity: %+v", target.Profile)
	}
	if target.Discovery.ModelCount != 1 || len(target.Models) != 1 || target.Models[0].ID != "vendor/model-a" {
		t.Fatalf("unexpected discovery/model selection: count=%d models=%+v", target.Discovery.ModelCount, target.Models)
	}

	assertPassingProbeIDs(t, target.Discovery.Tests, []string{
		"models.list.http",
		"models.list.schema",
	})
	assertPassingProbeIDs(t, target.Discovery.Compatibility, []string{
		"chat.error.invalid_auth",
		"chat.error.invalid_json",
		"chat.error.invalid_model",
		"chat.error.invalid_types",
	})
	assertPassingProbeIDs(t, target.Models[0].Compatibility, []string{
		"chat.basic",
		"chat.content_parts.text",
		"chat.logprobs",
		"chat.multi_turn",
		"chat.role.developer",
		"chat.role.system",
		"chat.schema",
		"chat.stream.sse",
		"chat.stream.usage",
		"chat.structured.json_object",
		"chat.structured.json_schema",
		"chat.tools.auto",
		"chat.tools.continuation",
		"chat.tools.forced",
		"chat.tools.none",
		"chat.tools.required",
		"chat.tools.stream",
		"chat.usage",
		"models.retrieve",
	})

	if target.Summary.Pass != 25 || target.Summary.RequiredPass != 8 || target.Summary.Fail != 0 || target.Summary.Warn != 0 || target.Summary.Unsupported != 0 || target.Summary.Skip != 0 || target.Summary.Error != 0 {
		t.Fatalf("OpenAI summary contract changed: %+v", target.Summary)
	}
	if target.Summary.Assessment == nil || target.Summary.Assessment.Verdict != "pass" || out.ReportV3.Summary.Verdict != "pass" {
		t.Fatalf("OpenAI conformance contract changed: target=%+v run=%+v", target.Summary.Assessment, out.ReportV3.Summary)
	}
	if target.Summary.Usage.TotalTokens == 0 {
		t.Fatalf("OpenAI usage was lost during aggregation: %+v", target.Summary.Usage)
	}
	if target.RateLimit.SuccessfulRequests == 0 || target.RateLimit.RateLimited != 0 {
		t.Fatalf("OpenAI transport telemetry changed unexpectedly: %+v", target.RateLimit)
	}
}

func TestNativeProtocolRegistryContainsEveryConfiguredProtocol(t *testing.T) {
	registry, err := newProtocolRegistry()
	if err != nil {
		t.Fatal(err)
	}
	want := []config.Protocol{config.ProtocolAnthropic, config.ProtocolGoogleGenAI, config.ProtocolOpenAI}
	if got := registry.Protocols(); !reflect.DeepEqual(got, want) {
		t.Fatalf("registered protocols changed: got=%v want=%v", got, want)
	}
}

func assertPassingProbeIDs(t *testing.T, tests []result.TestResult, want []string) {
	t.Helper()
	got := make([]string, len(tests))
	for index, test := range tests {
		got[index] = test.ID
		if test.Status != result.StatusPass {
			t.Fatalf("probe %s=%s, want pass: %s", test.ID, test.Status, test.Summary)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("probe IDs changed:\n got: %v\nwant: %v", got, want)
	}
}
