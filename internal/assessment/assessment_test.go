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
		Config:     result.PublicConfig{BaseURL: "https://example.test/v1", Tests: []string{"ext-suite"}},
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
	r := result.Report{Config: result.PublicConfig{BaseURL: "https://example.test/v1", Tests: []string{"ext-suite"}}}
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
	r := result.Report{Config: result.PublicConfig{BaseURL: "https://example.test/v1", Tests: []string{"ext-suite"}}, Extensions: []result.ExtensionRun{{
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
	r := result.Report{Config: result.PublicConfig{BaseURL: "https://example.test/v1", Tests: []string{"ext-suite"}}, Extensions: []result.ExtensionRun{{Suites: []result.ExtensionSuiteResult{
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
		Config: result.PublicConfig{BaseURL: "https://example.test/v1", Tests: []string{"ext-suite"}},
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
	chatModule, ok := p.Module("chat-completions")
	if !ok {
		t.Fatal("chat module missing from profile")
	}
	p.Modules = []profile.Module{chatModule}
	r := result.Report{Config: result.PublicConfig{BaseURL: "https://example.test/v1"}, Models: []result.ModelReport{{
		ID:      "image-model",
		Modules: []result.ModuleAssessment{{ID: "chat-completions", Requested: true, Applicability: result.ApplicabilityNotApplicable}},
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
		Modules: []result.ModuleAssessment{{ID: "anthropic.messages", Requested: true, Applicability: result.ApplicabilityApplicable, Execution: result.ExecutionExecuted}},
	}}}
	ApplyTarget(&r, p, "anthropic", "anthropic")
	if r.Models[0].Assessment.Verdict != "inconclusive" {
		t.Fatalf("model verdict ignored missing executed-module evidence: model=%+v requirements=%+v", r.Models[0].Assessment, r.Requirements)
	}
}

func TestExecutedProviderCoverageGapIsAnInternalError(t *testing.T) {
	p, err := profile.LoadFile("openai-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	r := result.Report{
		Config: result.PublicConfig{BaseURL: "https://example.test/v1", Tests: []string{"compat"}},
		Models: []result.ModelReport{{ID: "model-a"}},
	}
	Apply(&r, p)
	for _, requirement := range r.Requirements {
		if requirement.Subject.Kind == "provider" && requirement.ID == "models.list.http" {
			if requirement.Status != result.RequirementError || requirement.FailureKind != "coverage_missing_probe" {
				t.Fatalf("provider coverage gap=%+v, want execution error", requirement)
			}
			return
		}
	}
	t.Fatal("missing provider coverage requirement")
}

func TestUnselectedModelParameterProbeDoesNotCreateCoverageGap(t *testing.T) {
	p, err := profile.LoadFile("google-genai-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	module, ok := p.Module("google-genai.interactions")
	if !ok {
		t.Fatal("Google Interactions module missing from profile")
	}
	r := result.Report{
		Config: result.PublicConfig{BaseURL: "https://example.test", Tests: []string{"compat"}},
		Models: []result.ModelReport{{
			ID:      "model-a",
			Modules: []result.ModuleAssessment{{ID: module.ID, Requested: true, Applicability: result.ApplicabilityApplicable, Execution: result.ExecutionExecuted}},
		}},
	}
	Apply(&r, p)
	for _, requirement := range r.Requirements {
		if requirement.ID == "model.parameter" {
			t.Fatalf("unselected model-parameter probe created a requirement: %+v", requirement)
		}
	}
}

func reportWithTest(test result.TestResult) result.Report {
	return result.Report{
		Config:    result.PublicConfig{BaseURL: "https://example.test/v1", Tests: []string{"ext-suite"}},
		Discovery: result.Discovery{Tests: []result.TestResult{test}},
	}
}

func TestUnknownRequestedCoreCoverageIsInconclusiveAndNotRankable(t *testing.T) {
	p, err := profile.LoadFile("openai-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	r := result.Report{Config: result.PublicConfig{BaseURL: "https://example.test/v1", Tests: []string{"compat"}}, Models: []result.ModelReport{{
		ID: "new-provider/arbitrary-model-937", Modules: []result.ModuleAssessment{{
			ID: "chat-completions", Requested: true, Applicability: result.ApplicabilityUnknown, Execution: result.ExecutionSkipped, Reason: "insufficient capability evidence",
		}},
	}}}
	ApplyTarget(&r, p, "one", "openai")
	got := r.Summary.Assessment
	if got.Verdict != "inconclusive" || got.Comparison.Rankable || got.Coverage != (result.CoverageMeasure{Expected: 1, Unresolved: 1, CoreUnresolved: 1}) {
		t.Fatalf("unknown requested coverage was not preserved: %+v", got)
	}
	if r.Models[0].Assessment.Verdict != "inconclusive" || r.Summary.Status != "inconclusive" {
		t.Fatalf("canonical target/model verdict not synchronized: model=%+v target=%+v", r.Models[0].Assessment, r.Summary)
	}
}

func TestUnknownUnrequestedModuleDoesNotCreateCoverageGap(t *testing.T) {
	p, err := profile.LoadFile("openai-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	r := result.Report{Config: result.PublicConfig{BaseURL: "https://example.test/v1", Tests: []string{"sdk"}}, Models: []result.ModelReport{{
		ID: "arbitrary", Modules: []result.ModuleAssessment{{ID: "images-generations", Applicability: result.ApplicabilityUnknown, Execution: result.ExecutionSkipped}},
	}}}
	ApplyTarget(&r, p, "one", "openai")
	if got := r.Summary.Assessment; got.Verdict != "pass" || got.Coverage.Expected != 0 || len(r.Requirements) != 0 {
		t.Fatalf("unrequested unknown module affected coverage: assessment=%+v requirements=%+v", got, r.Requirements)
	}
}

func TestGloballyUnmatchedRequestedScopeIsInconclusiveWithoutSyntheticModel(t *testing.T) {
	p, err := profile.LoadFile("openai-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	r := result.Report{Config: result.PublicConfig{
		BaseURL:         "https://example.test/v1",
		Tests:           []string{"compat"},
		RequestedModels: []string{"missing-model"},
	}}
	ApplyTarget(&r, p, "openai-a", "openai")
	if !ApplyGloballyUnmatchedRequestedScope(&r, "openai-a", "openai", true) {
		t.Fatal("global no-match guard was not applied")
	}
	if got := r.Summary.Assessment; got.Verdict != "inconclusive" || got.VerdictReason != "core_evidence_incomplete" || got.Comparison.Rankable || got.Core.Skip != 1 {
		t.Fatalf("no-match changed canonical verdict incorrectly: %+v", got)
	}
	if len(r.Requirements) != 1 || r.Requirements[0].Subject.Kind != "provider" || r.Requirements[0].ID != "selection.requested_models" || r.Requirements[0].FailureKind != "no_evaluated_requested_scope" {
		t.Fatalf("no-match requirement=%+v", r.Requirements)
	}
	if got := r.Requirements[0].Evidence[0]; got.Status != result.StatusSkip || got.Summary == "" || got.Signals[0].Code != "execution.requested_models.no_match" {
		t.Fatalf("no-match evidence=%+v", got)
	}
}

func TestGloballyUnmatchedRequestedScopeDoesNotPenalizePartialTargetMatch(t *testing.T) {
	r := result.Report{Config: result.PublicConfig{
		BaseURL:         "https://example.test/v1",
		RequestedModels: []string{"shared-model"},
	}}
	if ApplyGloballyUnmatchedRequestedScope(&r, "target-without-model", "openai", false) {
		t.Fatal("a target-local miss was treated as a global no-match")
	}
	if len(r.Requirements) != 0 || r.Summary.Assessment != nil {
		t.Fatalf("partial target miss mutated assessment: %+v", r)
	}
}

func TestGloballyUnmatchedRequestedScopeKeepsCoreFailureAuthoritative(t *testing.T) {
	p := profile.Profile{}
	r := result.Report{Config: result.PublicConfig{
		BaseURL:         "https://example.test/v1",
		RequestedModels: []string{"missing-model"},
	}, Discovery: result.Discovery{Tests: []result.TestResult{{ID: "provider.required", Required: true, Status: result.StatusFail}}}}
	ApplyTarget(&r, p, "one", "openai")
	ApplyGloballyUnmatchedRequestedScope(&r, "one", "openai", true)
	if got := r.Summary.Assessment; got.Verdict != "fail" || got.VerdictReason != "core_failure" || got.Comparison.Rankable {
		t.Fatalf("core failure was not preserved above no-match: %+v", got)
	}
}

func TestAnthropicModelParameterIsLocalImplementationGap(t *testing.T) {
	p, err := profile.LoadFile("anthropic-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	r := result.Report{Config: result.PublicConfig{BaseURL: "https://example.test", Tests: []string{"model-param"}}, Models: []result.ModelReport{{ID: "claude-fixture"}}}
	ApplyTarget(&r, p, "anthropic", "anthropic")
	if got := r.Summary.Assessment; got.Verdict != "pass-with-gaps" || got.Optional.Unsupported != 0 || got.Optional.Skip != 1 {
		t.Fatalf("implementation gap became provider support verdict: %+v", got)
	}
	if len(r.Requirements) != 1 {
		t.Fatalf("requirements=%+v", r.Requirements)
	}
	got := r.Requirements[0]
	if got.ID != "aiqa.model_param.implementation" || got.Status != result.RequirementSkip || got.FailureKind != "not_implemented_by_aiqa" || got.Module != "model-param" {
		t.Fatalf("unexpected local implementation gate: %+v", got)
	}
	if len(got.Evidence) != 1 || got.Evidence[0].Kind != "aiqa" || got.Evidence[0].Status != result.StatusSkip || got.Evidence[0].HTTPStatus != 0 {
		t.Fatalf("implementation gate claimed provider evidence: %+v", got.Evidence)
	}
}

func TestAnthropicModelParameterGapDoesNotInvalidateCompatibilityPass(t *testing.T) {
	p, err := profile.LoadFile("anthropic-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	// This case isolates compatibility evidence from provider-discovery coverage;
	// the latter belongs to the runner integration test.
	p.Modules = nil
	r := result.Report{Config: result.PublicConfig{BaseURL: "https://example.test", Tests: []string{"compat", "model-param"}}, Models: []result.ModelReport{{
		ID:            "claude-fixture",
		Compatibility: []result.TestResult{{ID: "anthropic.messages.basic", Required: true, Status: result.StatusPass}},
	}}}
	ApplyTarget(&r, p, "anthropic", "anthropic")
	if got := r.Summary.Assessment; got.Verdict != "pass-with-gaps" || got.Core.Fail != 0 || got.Optional.Skip != 1 {
		t.Fatalf("model-param gap altered unrelated compatibility evidence: %+v", got)
	}
}

func TestAnthropicModelParameterImplementationGateYieldsToActualRunnerEvidence(t *testing.T) {
	p, err := profile.LoadFile("anthropic-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	r := result.Report{Config: result.PublicConfig{BaseURL: "https://example.test", Tests: []string{"model-param"}}, Models: []result.ModelReport{{
		ID:         "claude-fixture",
		Parameters: []result.ParameterResult{{Name: "temperature", Status: result.StatusPass}},
	}}}
	ApplyTarget(&r, p, "anthropic", "anthropic")
	for _, requirement := range r.Requirements {
		if requirement.ID == "aiqa.model_param.implementation" {
			t.Fatalf("local stub hid actual runner evidence: %+v", r.Requirements)
		}
	}
}
