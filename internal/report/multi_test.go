package report

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ilyar/aiqa/internal/result"
)

func TestNormalizeV3CollectionsSerializesEmptyModelsAsArray(t *testing.T) {
	report := normalizeV3Collections(result.ReportV3{
		Targets: []result.TargetReport{{ID: "openai"}},
	})
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Targets []struct {
			Models json.RawMessage `json:"models"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Targets) != 1 || string(decoded.Targets[0].Models) != "[]" {
		t.Fatalf("models JSON = %s, want []", decoded.Targets[0].Models)
	}
}

func TestMarkdownV3RendersTargetActionsAndPerModelTelemetry(t *testing.T) {
	assessment := result.ConformanceAssessment{Verdict: "pass-with-gaps"}
	report := result.ReportV3{
		SchemaVersion: "aiqa.report.v3",
		Config: result.PublicConfigV3{Timeout: "1s", Targets: []result.TargetPublicConfig{{
			ID: "anthropic", Protocol: "anthropic", BaseURL: "https://gate.joingonka.ai/v1", BaseURLNormalized: true,
		}}},
		Summary: result.RunSummary{Verdict: "pass-with-gaps"},
		Targets: []result.TargetReport{{
			ID: "anthropic", Protocol: "anthropic",
			Profile: result.ProfileInfo{ID: "anthropic-compat", Revision: "2026-09"},
			Summary: result.Summary{Assessment: &assessment, Usage: result.Usage{InputTokens: 15, OutputTokens: 28, CacheReadInputTokens: 144}},
			Models: []result.ModelReport{
				{ID: "model-a", Assessment: result.ModelAssessment{Verdict: "pass"}, RateLimit: &result.RateLimitStats{LogicalRequests: 2, PhysicalAttempts: 2, SuccessfulRequests: 2, LatencySamples: 2, SafeRate: 1.5, MeanLatencyMS: 10, P50LatencyMS: 9, P95LatencyMS: 12}},
				{ID: "model-b", Assessment: result.ModelAssessment{Verdict: "pass"}, RateLimit: &result.RateLimitStats{LogicalRequests: 3, PhysicalAttempts: 3, SuccessfulRequests: 3, LatencySamples: 3, SafeRate: 2.5, MeanLatencyMS: 20, P50LatencyMS: 19, P95LatencyMS: 22}},
			},
			Guidance: result.GuidanceReport{Recommendations: []result.Recommendation{{
				Title: "Fix message envelope", Priority: "p1", Confidence: "high", Why: "The SDK cannot decode it.",
				Target: result.RecommendationTarget{Component: "gateway"}, Steps: []result.RecommendationStep{{ID: "shape", Text: "Return type=message."}},
				Verification: []result.Verification{{TargetID: "anthropic", Protocol: "anthropic", Kind: "requirement", IDs: []string{"anthropic.messages.schema"}}},
			}}},
		}},
	}
	markdown := MarkdownV3(report)
	for _, want := range []string{"| PRI | Recommendation", "**Change or investigate:**", "Requirements: `anthropic.messages.schema`", "#### Per-model rate-limit and latency", "| `model-a` | 2 | 2 | 2 |", "normalized by removing one trailing `/v1`", "**Service:** [gate.joingonka.ai](https://gate.joingonka.ai)", "| Target | Input | Output | Cache creation | Cache read | Thinking/reasoning | Total |", "| `anthropic` | 15 | 28 | 0 | 144 | 0 | **187** |"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
}

func TestMarkdownV3RendersServiceOriginWithoutAPIPath(t *testing.T) {
	report := result.ReportV3{Config: result.PublicConfigV3{Targets: []result.TargetPublicConfig{
		{ID: "openai", Protocol: "openai", BaseURL: "https://gate.joingonka.ai/v1"},
	}}, Targets: []result.TargetReport{{ID: "openai", Protocol: "openai"}}}
	markdown := MarkdownV3(report)
	if !strings.Contains(markdown, "- **Service:** [gate.joingonka.ai](https://gate.joingonka.ai)") {
		t.Fatalf("service origin missing from Markdown:\n%s", markdown)
	}
	if strings.Contains(markdown, "gate.joingonka.ai/v1") {
		t.Fatalf("API path leaked into service link:\n%s", markdown)
	}
}

func TestMarkdownV3ListsDistinctProtocolServices(t *testing.T) {
	report := result.ReportV3{Config: result.PublicConfigV3{Targets: []result.TargetPublicConfig{
		{ID: "openai", Protocol: "openai", BaseURL: "https://openai.example/v1"},
		{ID: "anthropic", Protocol: "anthropic", BaseURL: "https://anthropic.example/api"},
		{ID: "google-genai", Protocol: "google-genai", BaseURL: "https://gemini.example/v1beta"},
	}}}
	markdown := MarkdownV3(report)
	want := "- **Service:** Anthropic: [anthropic.example](https://anthropic.example), Google GenAI: [gemini.example](https://gemini.example), OpenAI: [openai.example](https://openai.example)"
	if !strings.Contains(markdown, want) {
		t.Fatalf("services = %q, want %q", serviceLine(markdown), want)
	}
}

func TestMarkdownV3ListsSameOriginProtocolServicesSeparately(t *testing.T) {
	report := result.ReportV3{Config: result.PublicConfigV3{Targets: []result.TargetPublicConfig{
		{ID: "google-genai", Protocol: "google-genai", BaseURL: "https://gateway.example/google/v1beta"},
		{ID: "openai", Protocol: "openai", BaseURL: "https://gateway.example/openai/v1"},
		{ID: "anthropic", Protocol: "anthropic", BaseURL: "https://gateway.example/anthropic"},
	}}}
	markdown := MarkdownV3(report)
	want := "- **Service:** Anthropic: [gateway.example](https://gateway.example), Google GenAI: [gateway.example](https://gateway.example), OpenAI: [gateway.example](https://gateway.example)"
	if !strings.Contains(markdown, want) {
		t.Fatalf("services = %q, want %q", serviceLine(markdown), want)
	}
	if strings.Contains(markdown, "gateway.example/openai") || strings.Contains(markdown, "gateway.example/anthropic") || strings.Contains(markdown, "gateway.example/google") {
		t.Fatalf("API path leaked into service links:\n%s", markdown)
	}
}

func serviceLine(markdown string) string {
	for _, line := range strings.Split(markdown, "\n") {
		if strings.HasPrefix(line, "- **Service:**") {
			return line
		}
	}
	return ""
}

func TestMarkdownV3RateLimitTableHasMatchingColumns(t *testing.T) {
	report := result.ReportV3{
		Targets: []result.TargetReport{{ID: "google-genai", Protocol: "google-genai", RateLimit: result.RateLimitStats{LogicalRequests: 10, PhysicalAttempts: 10, SuccessfulRequests: 6}}},
	}
	markdown := MarkdownV3(report)
	section := strings.SplitN(markdown, "## Rate-limit and latency\n\n", 2)
	if len(section) != 2 {
		t.Fatalf("rate-limit section is missing:\n%s", markdown)
	}
	lines := strings.SplitN(section[1], "\n\n", 2)[0]
	for _, line := range strings.Split(lines, "\n") {
		if got := strings.Count(line, "|") - 1; got != 14 {
			t.Fatalf("rate-limit table line has %d cells, want 14: %s", got, line)
		}
	}
}

func TestMarkdownV3NormalizesAndTotalsCrossProtocolUsage(t *testing.T) {
	report := result.ReportV3{
		Targets: []result.TargetReport{
			{ID: "openai", Protocol: "openai", Summary: result.Summary{Usage: result.Usage{PromptTokens: 1417, CompletionTokens: 199, TotalTokens: 1616}}},
			{ID: "anthropic", Protocol: "anthropic", Summary: result.Summary{Usage: result.Usage{InputTokens: 15, OutputTokens: 28, CacheReadInputTokens: 144}}},
		},
	}
	markdown := MarkdownV3(report)
	for _, want := range []string{
		"| `openai` | 1417 | 199 | 0 | 0 | 0 | **1616** |",
		"| `anthropic` | 15 | 28 | 0 | 144 | 0 | **187** |",
		"| **All targets** | **1432** | **227** | **0** | **144** | **0** | **1803** |",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
}

func TestMarkdownV3GroupsExtensionSuitesWithoutExecutablePath(t *testing.T) {
	report := result.ReportV3{Config: result.PublicConfigV3{Timeout: "1s"}, Summary: result.RunSummary{Verdict: "pass"}, Targets: []result.TargetReport{{
		ID: "openai", Protocol: "openai", Profile: result.ProfileInfo{ID: "openai-compat", Revision: "2026-09-01"},
		Extensions: []result.ExtensionRun{{
			Provenance: result.ExtensionProvenance{ID: "official-sdk.openai", Name: "Official SDK – OpenAI Go", Version: "v3.54.0", Source: "executable", Path: "/private/bin/aiqa-openai-ext-suite"},
			Subject:    result.SubjectRef{Kind: "model", ID: "model-a"}, Runner: result.ExtensionRunnerResult{Status: result.StatusPass},
			Suites: []result.ExtensionSuiteResult{{SuiteID: "chat.stream", Subject: result.SubjectRef{Kind: "model", ID: "model-a"}, Status: result.StatusPass, DurationMS: 12, Summary: "SDK parsed the stream"}},
		}},
	}}}
	markdown := MarkdownV3(report)
	for _, want := range []string{"**External checks:** [OpenAI Go SDK v3.54.0]", "#### OpenAI Go SDK v3.54.0", "| Suite | Subject | Status | Duration | Summary |", "| `chat.stream` | `model:model-a` | PASS | 12ms |"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
	if strings.Contains(markdown, "Official SDK") {
		t.Fatalf("redundant SDK label leaked into Markdown:\n%s", markdown)
	}
	if strings.Contains(markdown, "/private/bin") || strings.Contains(markdown, "Runner issues") {
		t.Fatalf("Markdown exposed transport detail or a successful runner issue:\n%s", markdown)
	}
}
