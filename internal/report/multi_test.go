package report

import (
	"strings"
	"testing"

	"github.com/ilyar/aiqa/internal/result"
)

func TestMarkdownV2RendersTargetActionsAndPerModelTelemetry(t *testing.T) {
	assessment := result.ConformanceAssessment{Verdict: "pass-with-gaps"}
	report := result.ReportV2{
		SchemaVersion: "aiqa.report.v2",
		Config: result.PublicConfigV2{Timeout: "1s", Targets: []result.TargetPublicConfig{{
			ID: "anthropic", Protocol: "anthropic", BaseURLNormalized: true,
		}}},
		Summary: result.RunSummary{Verdict: "pass-with-gaps"},
		Targets: []result.TargetReport{{
			ID: "anthropic", Protocol: "anthropic",
			Profile: result.ProfileInfo{ID: "anthropic-messages-compat", Revision: "2026-09"},
			Summary: result.Summary{Assessment: &assessment},
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
	markdown := MarkdownV2(report)
	for _, want := range []string{"| PRI | Recommendation", "**Change or investigate:**", "Requirements: `anthropic.messages.schema`", "#### Per-model rate-limit and latency", "| `model-a` | 2 |", "normalized by removing one trailing `/v1`"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
}
