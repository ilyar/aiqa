package report

import (
	"strings"
	"testing"

	"github.com/ilyar/aiqa/internal/result"
)

func TestMarkdownV3RendersTargetActionsAndPerModelTelemetry(t *testing.T) {
	assessment := result.ConformanceAssessment{Verdict: "pass-with-gaps"}
	report := result.ReportV3{
		SchemaVersion: "aiqa.report.v3",
		Config: result.PublicConfigV3{Timeout: "1s", Targets: []result.TargetPublicConfig{{
			ID: "anthropic", Protocol: "anthropic", BaseURLNormalized: true,
		}}},
		Summary: result.RunSummary{Verdict: "pass-with-gaps"},
		Targets: []result.TargetReport{{
			ID: "anthropic", Protocol: "anthropic",
			Profile: result.ProfileInfo{ID: "anthropic-compat", Revision: "2026-09"},
			Summary: result.Summary{Assessment: &assessment, Usage: result.Usage{InputTokens: 15, OutputTokens: 28, CacheReadInputTokens: 144}},
			Models: []result.ModelReport{
				{ID: "model-a", Assessment: result.ModelAssessment{Verdict: "pass"}, RateLimit: &result.RateLimitStats{SuccessfulRequests: 2, SafeRate: 1.5, MeanLatencyMS: 10, P50LatencyMS: 9, P95LatencyMS: 12}},
				{ID: "model-b", Assessment: result.ModelAssessment{Verdict: "pass"}, RateLimit: &result.RateLimitStats{SuccessfulRequests: 3, SafeRate: 2.5, MeanLatencyMS: 20, P50LatencyMS: 19, P95LatencyMS: 22}},
			},
			Guidance: result.GuidanceReport{Recommendations: []result.Recommendation{{
				Title: "Fix message envelope", Priority: "p1", Confidence: "high", Why: "The SDK cannot decode it.",
				Target: result.RecommendationTarget{Component: "gateway"}, Steps: []result.RecommendationStep{{ID: "shape", Text: "Return type=message."}},
				Verification: []result.Verification{{TargetID: "anthropic", Protocol: "anthropic", Kind: "requirement", IDs: []string{"anthropic.messages.schema"}}},
			}}},
		}},
	}
	markdown := MarkdownV3(report)
	for _, want := range []string{"| PRI | Recommendation", "**Change or investigate:**", "Requirements: `anthropic.messages.schema`", "#### Per-model rate-limit and latency", "| `model-a` | 2 |", "normalized by removing one trailing `/v1`", "| Target | Input | Output | Cache creation | Cache read | Thinking/reasoning | Total |", "| `anthropic` | 15 | 28 | 0 | 144 | 0 | **187** |"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
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
			Provenance: result.ExtensionProvenance{ID: "official-sdk.openai", Name: "Official SDK — OpenAI Go", Version: "v3.54.0", Source: "executable", Path: "/private/bin/aiqa-openai-ext-suite"},
			Subject:    result.SubjectRef{Kind: "model", ID: "model-a"}, Runner: result.ExtensionRunnerResult{Status: result.StatusPass},
			Suites: []result.ExtensionSuiteResult{{SuiteID: "chat.stream", Subject: result.SubjectRef{Kind: "model", ID: "model-a"}, Status: result.StatusPass, DurationMS: 12, Summary: "SDK parsed the stream"}},
		}},
	}}}
	markdown := MarkdownV3(report)
	for _, want := range []string{"**External checks:** [Official SDK — OpenAI Go v3.54.0]", "#### Official SDK — OpenAI Go v3.54.0", "| Suite | Subject | Status | Duration | Summary |", "| `chat.stream` | `model:model-a` | PASS | 12ms |"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
	if strings.Contains(markdown, "/private/bin") || strings.Contains(markdown, "Runner issues") {
		t.Fatalf("Markdown exposed transport detail or a successful runner issue:\n%s", markdown)
	}
}
