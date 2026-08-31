package report

import (
	"strings"
	"testing"

	"github.com/ilyar/aiqa/internal/result"
)

func TestMarkdownRendersOfficialSDKWithoutToolColumn(t *testing.T) {
	report := result.Report{Models: []result.ModelReport{{
		ID: "test-model",
		External: []result.ExternalResult{{
			Tool:       "official-SDK",
			Suite:      "models",
			Status:     result.StatusPass,
			DurationMS: 12,
			Summary:    "pass",
		}},
	}}}
	markdown := Markdown(report)
	for _, want := range []string{
		"- **External checks:** Official SDK",
		"#### Official SDK",
		"| Suite | Status | Summary | Duration |",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown is missing %q:\n%s", want, markdown)
		}
	}
	if strings.Contains(markdown, "| Tool |") {
		t.Fatalf("external table must not render a Tool column:\n%s", markdown)
	}
}

func TestMarkdownMarksUnavailableExternalChecksAsNotSetup(t *testing.T) {
	markdown := Markdown(result.Report{Models: []result.ModelReport{{
		External: []result.ExternalResult{{Tool: "official-SDK", Status: result.StatusSkip}},
	}}})
	if !strings.Contains(markdown, "- **External checks:** not setup") {
		t.Fatalf("unexpected external check summary:\n%s", markdown)
	}
}

func TestMarkdownRendersPerModelPacingOnlyForMultipleModels(t *testing.T) {
	stats := &result.RateLimitStats{SuccessfulRequests: 3, LatencySamples: 3, MeanLatencyMS: 10}
	multi := Markdown(result.Report{Models: []result.ModelReport{{ID: "a", RateLimit: stats}, {ID: "b", RateLimit: stats}}})
	if strings.Count(multi, "#### Rate-limit and latency") != 2 {
		t.Fatalf("per-model pacing blocks missing:\n%s", multi)
	}
	single := Markdown(result.Report{Models: []result.ModelReport{{ID: "a", RateLimit: stats}}})
	if strings.Contains(single, "#### Rate-limit and latency") {
		t.Fatalf("single-model report must not duplicate global pacing block:\n%s", single)
	}
}

func TestMarkdownPutsActionsBeforeDiagnosticsAndCollapsesRawEvidence(t *testing.T) {
	partial := "partial"
	report := result.Report{
		Config: result.PublicConfig{Guidance: "full"},
		Summary: result.Summary{
			Pass: 1, Score: 50, ScoreDeprecated: true,
			Assessment: &result.ConformanceAssessment{
				Verdict:  "fail",
				Core:     result.RequirementMeasure{Total: 1, Fail: 1, Percent: floatPointer(0)},
				Optional: result.RequirementMeasure{Total: 1, Pass: 1, Percent: floatPointer(100)},
				Evidence: result.EvidenceMeasure{Total: 2, Executed: 2, Conclusive: 2, ConclusivePercent: floatPointer(100)},
			},
		},
		Discovery: result.Discovery{Tests: []result.TestResult{{ID: "models.list.http", Name: "Models", Status: result.StatusPass, Summary: "pass"}}},
		Requirements: []result.RequirementAssessment{{
			ID: "models.list.schema", Name: "Model schema", Level: result.RequirementCore,
			Subject: result.SubjectRef{Kind: "provider", ID: "https://example.test/v1"}, Applicability: result.ApplicabilityApplicable, Status: result.RequirementFail,
		}},
		Guidance: result.GuidanceReport{
			Status:   "complete",
			Summary:  result.GuidanceSummary{ByPriority: result.PriorityCounts{P0: 1}},
			Findings: []result.Finding{{ID: "finding:test", Code: "contract.test", Subject: result.SubjectRef{Kind: "provider", ID: "https://example.test/v1"}, EvidenceRefs: []string{"native:test"}, RequirementIDs: []string{"models.list.schema"}}},
			Recommendations: []result.Recommendation{{
				ID: "recommendation:test", FindingIDs: []string{"finding:test"}, Title: "Fix the model schema", Priority: "p0", ActionKind: "fix", GuidanceLevel: 3, Confidence: "high",
				Target: result.RecommendationTarget{Component: "provider_gateway"}, Why: "The core schema failed.",
				Steps: []result.RecommendationStep{{ID: "fix", Text: "Add the required field."}}, Verification: []result.Verification{{Kind: "requirement", IDs: []string{"models.list.schema"}}},
				PotentialImpact: result.PotentialImpact{CurrentVerdict: "fail", ProjectedVerdict: &partial, CoreRequirementsRecovered: 1},
			}},
			Artifacts: map[string]result.GuidanceArtifact{"sha256:abc": {Kind: "external_checker_log", Bytes: 12, Content: "unique raw log"}},
		},
	}
	markdown := Markdown(report)
	for _, heading := range []string{"## Summary", "## Fix first", "## Model and module details", "## Passed checks", "## Raw evidence", "## Methodology and limitations"} {
		if !strings.Contains(markdown, heading) {
			t.Fatalf("missing %q:\n%s", heading, markdown)
		}
	}
	if strings.Index(markdown, "## Fix first") > strings.Index(markdown, "## Model and module details") {
		t.Fatalf("actions must precede raw diagnostics:\n%s", markdown)
	}
	if strings.Count(markdown, "unique raw log") != 1 {
		t.Fatalf("raw artifact must render exactly once:\n%s", markdown)
	}
	if !strings.Contains(markdown, "<details><summary>Show 1 passed checks</summary>") || !strings.Contains(markdown, "<details><summary>Legacy scoring</summary>") {
		t.Fatalf("passed checks and legacy score must be collapsed:\n%s", markdown)
	}
}

func floatPointer(value float64) *float64 { return &value }

func TestMarkdownSummaryGuidanceOmitsRecommendationDetails(t *testing.T) {
	report := result.Report{
		Config: result.PublicConfig{Guidance: "summary"},
		Guidance: result.GuidanceReport{
			Status: "complete",
			Recommendations: []result.Recommendation{{
				ID: "recommendation:test", Title: "Fix the schema", Priority: "p0", ActionKind: "fix", Confidence: "high",
				Target: result.RecommendationTarget{Component: "provider_gateway"}, Why: "detail that should be omitted",
			}},
		},
	}
	markdown := Markdown(report)
	if !strings.Contains(markdown, "Fix the schema") || strings.Contains(markdown, "detail that should be omitted") {
		t.Fatalf("summary guidance rendered the wrong detail level:\n%s", markdown)
	}
}
