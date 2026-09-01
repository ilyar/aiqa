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

func TestUnboundExtensionDoesNotCreateRequirement(t *testing.T) {
	r := result.Report{
		Config:     result.PublicConfig{BaseURL: "https://example.test/v1"},
		Extensions: []result.ExtensionRun{{Suites: []result.ExtensionSuiteResult{{ExtensionID: "third-party.example", SuiteID: "extra", Bound: false, Status: result.StatusFail, Subject: result.SubjectRef{Kind: "provider", ID: "https://example.test/v1"}}}}},
	}
	Apply(&r, profile.Profile{})
	if len(r.Requirements) != 0 || r.Summary.Assessment.Verdict != "pass" {
		t.Fatalf("unbound evidence affected conformance: requirements=%+v assessment=%+v", r.Requirements, r.Summary.Assessment)
	}
}

func TestExtensionEvidenceCreatesProfileBoundRequirement(t *testing.T) {
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	r := result.Report{Config: result.PublicConfig{BaseURL: "https://example.test/v1"}}
	r.Models = []result.ModelReport{{
		ID:            "model-a",
		Compatibility: []result.TestResult{{ID: "chat.stream.sse", Name: "stream", Status: result.StatusPass}},
	}}
	r.Extensions = []result.ExtensionRun{{Suites: []result.ExtensionSuiteResult{{ExtensionID: "official-sdk.openai", SuiteID: "chat.stream", RequirementID: "official-sdk.openai.chat.stream", RelatedRequirementIDs: []string{"chat.stream.sse"}, Bound: true, Status: result.StatusPass, Subject: result.SubjectRef{Kind: "model", ID: "model-a"}}}}}
	Apply(&r, p)
	var found *result.RequirementAssessment
	for index := range r.Requirements {
		if r.Requirements[index].ID == "official-sdk.openai.chat.stream" {
			found = &r.Requirements[index]
		}
	}
	if found == nil || len(found.Evidence) != 1 || found.Status != result.RequirementPass {
		t.Fatalf("bound extension requirement missing: %+v", found)
	}
}

func TestRunnerFailureKeepsPassingSuiteButMakesEvidenceIncomplete(t *testing.T) {
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	r := result.Report{Config: result.PublicConfig{BaseURL: "https://example.test/v1"}, Extensions: []result.ExtensionRun{{
		Runner: result.ExtensionRunnerResult{Status: result.StatusError, Summary: "process exited after emitting results"},
		Suites: []result.ExtensionSuiteResult{{ExtensionID: "official-sdk.openai", SuiteID: "chat.stream", RequirementID: "official-sdk.openai.chat.stream", Bound: true, Status: result.StatusPass, Subject: result.SubjectRef{Kind: "model", ID: "model-a"}}},
	}}}
	Apply(&r, p)
	if r.Summary.Assessment.Verdict != "pass-with-gaps" || len(r.Requirements) != 1 || r.Requirements[0].Status != result.RequirementWarn || r.Requirements[0].FailureKind != "evidence_gap" || len(r.Requirements[0].Evidence) != 2 {
		t.Fatalf("runner failure did not remain inconclusive evidence: requirements=%+v assessment=%+v", r.Requirements, r.Summary.Assessment)
	}
}

func TestMultipleSuiteCasesCountOneCanonicalRequirement(t *testing.T) {
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	subject := result.SubjectRef{Kind: "model", ID: "model-a"}
	r := result.Report{Config: result.PublicConfig{BaseURL: "https://example.test/v1"}, Extensions: []result.ExtensionRun{{Suites: []result.ExtensionSuiteResult{
		{ExtensionID: "fixture.multi", SuiteID: "case-a", RequirementID: "official-sdk.openai.chat.stream", Bound: true, Status: result.StatusPass, Subject: subject},
		{ExtensionID: "fixture.multi", SuiteID: "case-b", RequirementID: "official-sdk.openai.chat.stream", Bound: true, Status: result.StatusPass, Subject: subject},
	}}}}
	Apply(&r, p)
	if len(r.Requirements) != 1 || len(r.Requirements[0].Evidence) != 2 || r.Summary.Assessment.Optional.Total != 1 || r.Summary.Assessment.Optional.Pass != 1 {
		t.Fatalf("suite cases changed canonical weighting: requirements=%+v assessment=%+v", r.Requirements, r.Summary.Assessment)
	}
}

func TestOfficialSDKConflictDoesNotRewriteNativeCoreRequirement(t *testing.T) {
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	r := result.Report{
		Config: result.PublicConfig{BaseURL: "https://example.test/v1"},
		Models: []result.ModelReport{{
			ID: "model-a",
			Compatibility: []result.TestResult{{
				ID: "chat.stream.sse", Name: "stream", Required: true, Status: result.StatusPass,
			}},
		}},
		Extensions: []result.ExtensionRun{{Suites: []result.ExtensionSuiteResult{{ExtensionID: "official-sdk.openai", SuiteID: "chat.stream", RequirementID: "official-sdk.openai.chat.stream", RelatedRequirementIDs: []string{"chat.stream.sse"}, Bound: true, Status: result.StatusFail, Subject: result.SubjectRef{Kind: "model", ID: "model-a"}}}}},
	}
	Apply(&r, p)
	if got := r.Summary.Assessment.Verdict; got != "pass-with-gaps" {
		t.Fatalf("verdict=%q, want pass-with-gaps: %+v", got, r.Requirements)
	}
	if r.Summary.Assessment.Core.Pass != 1 || r.Summary.Assessment.Core.Warn != 0 {
		t.Fatalf("native core requirement changed: %+v", r.Summary.Assessment.Core)
	}
	var sdk *result.RequirementAssessment
	for i := range r.Requirements {
		if r.Requirements[i].ID == "official-sdk.openai.chat.stream" {
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

func TestTargetModelVerdictIncludesMaterializedProfileGaps(t *testing.T) {
	p, err := profile.LoadFile("anthropic-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	r := result.Report{Config: result.PublicConfig{BaseURL: "https://example.test"}, Models: []result.ModelReport{{
		ID: "model-a", Assessment: result.ModelAssessment{Verdict: "pass"},
		Modules: []result.ModuleAssessment{{ID: "anthropic.messages", Applicability: result.ApplicabilityApplicable, Execution: result.ExecutionExecuted}},
	}}}
	ApplyTarget(&r, p, "anthropic", "anthropic")
	if r.Models[0].Assessment.Verdict != "inconclusive" {
		t.Fatalf("model verdict ignored missing executed-module evidence: model=%+v requirements=%+v", r.Models[0].Assessment, r.Requirements)
	}
}

func reportWithTest(test result.TestResult) result.Report {
	return result.Report{
		Config:    result.PublicConfig{BaseURL: "https://example.test/v1"},
		Discovery: result.Discovery{Tests: []result.TestResult{test}},
	}
}
