package assessment

import (
	"testing"

	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

func TestCoreFailureIsAuthoritative(t *testing.T) {
	r := reportWithTest(result.TestResult{ID: "models.list.schema", Name: "schema", Required: true, Status: result.StatusFail})
	Apply(&r, profile.Profile{})
	if got := r.Summary.Assessment.Verdict; got != "fail" {
		t.Fatalf("verdict=%q, want fail", got)
	}
	if r.Summary.Assessment.Core.Total != 1 || r.Summary.Assessment.Core.Fail != 1 {
		t.Fatalf("unexpected core measure: %+v", r.Summary.Assessment.Core)
	}
}

func TestCoreErrorIsInconclusiveAndNotRankable(t *testing.T) {
	r := reportWithTest(result.TestResult{ID: "models.list.http", Name: "HTTP", Required: true, Status: result.StatusError})
	Apply(&r, profile.Profile{})
	assessment := r.Summary.Assessment
	if assessment.Verdict != "inconclusive" || assessment.Comparison.Rankable || assessment.Comparison.VerdictRank != nil {
		t.Fatalf("unexpected assessment: %+v", assessment)
	}
}

func TestOptionalUnsupportedIsPassWithGaps(t *testing.T) {
	r := reportWithTest(result.TestResult{ID: "models.retrieve", Name: "retrieve", Status: result.StatusUnsupported})
	Apply(&r, profile.Profile{})
	if got := r.Summary.Assessment.Verdict; got != "pass-with-gaps" {
		t.Fatalf("verdict=%q, want pass-with-gaps", got)
	}
	if r.Summary.Assessment.Optional.Unsupported != 1 {
		t.Fatalf("unexpected optional measure: %+v", r.Summary.Assessment.Optional)
	}
}

func TestExternalCheckerNotSetupDoesNotCreateRequirement(t *testing.T) {
	r := result.Report{
		Config: result.PublicConfig{BaseURL: "https://example.test/v1"},
		Discovery: result.Discovery{External: []result.ExternalResult{{
			Tool: "official-SDK", Status: result.StatusSkip, Summary: "external checker disabled",
		}}},
	}
	Apply(&r, profile.Profile{})
	if len(r.Requirements) != 0 || r.Summary.Assessment.Verdict != "pass" {
		t.Fatalf("checker setup state affected conformance: requirements=%+v assessment=%+v", r.Requirements, r.Summary.Assessment)
	}
}

func TestExternalEvidenceJoinsCanonicalRequirement(t *testing.T) {
	r := reportWithTest(result.TestResult{ID: "chat.stream.sse", Name: "stream", Status: result.StatusPass})
	r.Models = []result.ModelReport{{
		ID:            "model-a",
		Compatibility: []result.TestResult{{ID: "chat.stream.sse", Name: "stream", Status: result.StatusPass}},
		External: []result.ExternalResult{{
			Tool: "official-SDK", Suite: "chat_completions_stream", Model: "model-a", Status: result.StatusFail,
			RequirementIDs: []string{"chat.stream.sse"},
		}},
	}}
	// Keep only the model-scoped native evidence for this assertion.
	r.Discovery.Tests = nil
	Apply(&r, profile.Profile{})
	if len(r.Requirements) != 1 {
		t.Fatalf("requirements=%d, want one canonical requirement: %+v", len(r.Requirements), r.Requirements)
	}
	requirement := r.Requirements[0]
	if len(requirement.Evidence) != 2 || requirement.Status != result.RequirementWarn || requirement.FailureKind != "evidence_conflict" {
		t.Fatalf("unexpected merged requirement: %+v", requirement)
	}
}

func TestOfficialSDKConflictDoesNotRewriteNativeCoreRequirement(t *testing.T) {
	r := result.Report{
		Config: result.PublicConfig{BaseURL: "https://example.test/v1"},
		Models: []result.ModelReport{{
			ID: "model-a",
			Compatibility: []result.TestResult{{
				ID: "chat.stream.sse", Name: "stream", Required: true, Status: result.StatusPass,
			}},
			External: []result.ExternalResult{{
				Tool: "official-SDK", Suite: "chat_completions_stream", Status: result.StatusFail,
				RequirementIDs: []string{"official-sdk.chat.stream"}, RelatedRequirementIDs: []string{"chat.stream.sse"},
			}},
		}},
	}
	Apply(&r, profile.Profile{})
	if got := r.Summary.Assessment.Verdict; got != "pass-with-gaps" {
		t.Fatalf("verdict=%q, want pass-with-gaps: %+v", got, r.Requirements)
	}
	if r.Summary.Assessment.Core.Pass != 1 || r.Summary.Assessment.Core.Warn != 0 {
		t.Fatalf("native core requirement changed: %+v", r.Summary.Assessment.Core)
	}
	var sdk *result.RequirementAssessment
	for i := range r.Requirements {
		if r.Requirements[i].ID == "official-sdk.chat.stream" {
			sdk = &r.Requirements[i]
		}
	}
	if sdk == nil || sdk.Status != result.RequirementWarn || sdk.FailureKind != "evidence_conflict" {
		t.Fatalf("SDK conflict was not represented as an optional diagnostic: %+v", sdk)
	}
}

func TestNonApplicableModuleDoesNotEnterMeasures(t *testing.T) {
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	r := result.Report{Config: result.PublicConfig{BaseURL: "https://example.test/v1"}, Models: []result.ModelReport{{
		ID:      "image-model",
		Modules: []result.ModuleAssessment{{ID: "chat-completions", Applicability: result.ApplicabilityNotApplicable}},
	}}}
	Apply(&r, p)
	assessment := r.Summary.Assessment
	if len(r.Requirements) == 0 || assessment.Core.Total != 0 || assessment.Optional.Total != 0 || assessment.Verdict != "pass" {
		t.Fatalf("N/A affected conformance: requirements=%+v assessment=%+v", r.Requirements, assessment)
	}
	for _, requirement := range r.Requirements {
		if requirement.Applicability != result.ApplicabilityNotApplicable || requirement.Status != result.RequirementNotApplicable {
			t.Fatalf("unexpected N/A requirement: %+v", requirement)
		}
	}
}

func reportWithTest(test result.TestResult) result.Report {
	return result.Report{
		Config:    result.PublicConfig{BaseURL: "https://example.test/v1"},
		Discovery: result.Discovery{Tests: []result.TestResult{test}},
	}
}
