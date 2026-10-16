package report

import (
	"strings"
	"testing"

	"github.com/ilyar/aiqa/internal/result"
)

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

func TestMarkdownLiteralMakesHostileModelIDsInert(t *testing.T) {
	hostile := "x`](https://evil.example) <img src=x>\n\u202epayload\u2028line\u2029paragraph\u0085next"
	got := escapeInline(hostile)
	for _, forbidden := range []string{"\n", "\u202e", "\u2028", "\u2029", "\u0085", " <img", "`](https://", "`"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("hostile Markdown remained active %q in %q", forbidden, got)
		}
	}
	if !strings.Contains(got, `\]`) || !strings.Contains(got, `\u202e`) || !strings.Contains(got, `\u0060`) {
		t.Fatalf("hostile Markdown was not visibly encoded: %q", got)
	}
	// A single pair of CommonMark code delimiters therefore has no attacker
	// controlled closing delimiter or active link/HTML syntax between it.
	codeSpan := "`" + got + "`"
	if strings.Count(codeSpan, "`") != 2 {
		t.Fatalf("hostile identifier can terminate a code span: %q", codeSpan)
	}
}

func TestMarkdownPutsActionsBeforeDiagnosticsAndCollapsesRawEvidence(t *testing.T) {
	partial := "partial"
	report := result.Report{
		Config: result.PublicConfig{Guidance: "full"},
		Summary: result.Summary{
			Pass: 1, Usage: result.Usage{PromptTokens: 12, CompletionTokens: 3, TotalTokens: 15},
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
	rateIndex := strings.Index(markdown, "## Rate-limit and latency")
	actionIndex := strings.Index(markdown, "## Fix first")
	methodIndex := strings.Index(markdown, "## Methodology and limitations")
	usageIndex := strings.Index(markdown, "## Observed API usage")
	if rateIndex < 0 || actionIndex < 0 || methodIndex < 0 || usageIndex < 0 || rateIndex > actionIndex || usageIndex < methodIndex {
		t.Fatalf("global telemetry must be near the top and API usage must be last:\n%s", markdown)
	}
	if strings.Count(markdown, "## Rate-limit and latency") != 1 || !strings.HasSuffix(markdown, "**12 prompt + 3 completion = 15 total tokens**") {
		t.Fatalf("global telemetry was duplicated or API usage is not the final field:\n%s", markdown)
	}
	if !strings.Contains(markdown, "| PRI | Recommendation | Scope | Expected impact | Confidence |") || !strings.Contains(markdown, "|:---:|---|---|---|---:|") {
		t.Fatalf("recommendation priority column must use PRI and centered alignment:\n%s", markdown)
	}
	if strings.Contains(markdown, "| Priority | Recommendation |") {
		t.Fatalf("legacy recommendation priority heading remains:\n%s", markdown)
	}
	if strings.Count(markdown, "unique raw log") != 1 {
		t.Fatalf("raw artifact must render exactly once:\n%s", markdown)
	}
	if !strings.Contains(markdown, "<details><summary>Show 1 passed checks</summary>") {
		t.Fatalf("passed checks must be collapsed:\n%s", markdown)
	}
	if strings.Contains(markdown, "Legacy scoring") || strings.Contains(markdown, "Legacy pass rate") {
		t.Fatalf("legacy scoring must not be rendered:\n%s", markdown)
	}
	if !strings.Contains(markdown, "#### Verification targets") || !strings.Contains(markdown, "Requirements: `models.list.schema`") {
		t.Fatalf("structured verification target is missing:\n%s", markdown)
	}
	if strings.Contains(markdown, "```bash") || strings.Contains(markdown, "aiqa test=compat") {
		t.Fatalf("report must not emit broad verification commands:\n%s", markdown)
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
