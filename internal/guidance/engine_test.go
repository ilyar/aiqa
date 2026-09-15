package guidance

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

func TestCreatedFindingIsDeduplicatedAndProjectsPartial(t *testing.T) {
	provider := result.SubjectRef{Kind: "provider", ID: "https://example.test/v1"}
	r := result.Report{
		Summary: result.Summary{Assessment: &result.ConformanceAssessment{Verdict: "fail"}},
		Requirements: []result.RequirementAssessment{
			{
				ID: "models.list.schema", Name: "schema", Level: result.RequirementCore, Subject: provider,
				Applicability: result.ApplicabilityApplicable, Status: result.RequirementFail,
				Description: "Every model entry includes created.",
				Evidence: []result.EvidenceRef{
					{ID: "native:models", Kind: "native", Status: result.StatusFail, Summary: "created missing", Signals: []result.EvidenceSignal{{Code: "contract.models.list.entry.created_missing", Parameters: map[string]any{"endpoint": "GET /v1/models", "json_pointer": "/data/*/created", "missing_entries": 30, "total_entries": 30}}}},
					{ID: "sdk:models", Kind: "official_sdk", Status: result.StatusFail, Summary: "SDK rejected model objects"},
				},
			},
			{ID: "chat.logprobs", Name: "logprobs", Level: result.RequirementOptional, Subject: result.SubjectRef{Kind: "model", ID: "model-a"}, Applicability: result.ApplicabilityApplicable, Status: result.RequirementFail, Evidence: []result.EvidenceRef{{ID: "native:logprobs", Kind: "native", Status: result.StatusFail}}},
		},
	}
	Apply(&r, testProfile(), Options{Mode: "full", Pack: "generic", Locale: "en"})
	if r.Guidance.Status != "complete" || len(r.Guidance.Findings) != 2 || len(r.Guidance.Recommendations) != 2 {
		t.Fatalf("unexpected guidance: %+v", r.Guidance)
	}
	recommendation := r.Guidance.Recommendations[0]
	if recommendation.Priority != "p0" || recommendation.Title != "Add `created` to every model object" {
		t.Fatalf("unexpected top recommendation: %+v", recommendation)
	}
	if recommendation.PotentialImpact.ProjectedVerdict == nil || *recommendation.PotentialImpact.ProjectedVerdict != "partial" {
		t.Fatalf("projected verdict was not derived from remaining requirements: %+v", recommendation.PotentialImpact)
	}
	created := findingByCode(r.Guidance.Findings, "contract.models.list.entry.created_missing")
	if created == nil || len(created.EvidenceRefs) != 2 || created.ObservedScope.Affected != 30 || created.ObservedScope.Total != 30 {
		t.Fatalf("created finding was not deduplicated: %+v", created)
	}
}

func TestToolChoiceFailuresGroupAndContinuationIsBlocked(t *testing.T) {
	subject := result.SubjectRef{Kind: "model", ID: "qwen"}
	var requirements []result.RequirementAssessment
	for _, item := range []struct{ id, mode string }{{"chat.tools.required", "required"}, {"chat.tools.forced", "forced"}, {"chat.tools.stream", "stream"}} {
		requirements = append(requirements, result.RequirementAssessment{
			ID: item.id, Name: item.id, Level: result.RequirementOptional, Subject: subject, Applicability: result.ApplicabilityApplicable, Status: result.RequirementUnsupported,
			Evidence: []result.EvidenceRef{{ID: item.id, Kind: "native", Status: result.StatusUnsupported, Signals: []result.EvidenceSignal{{Code: "configuration.chat.tools.tool_choice_constraint", Parameters: map[string]any{"mode": item.mode}}}}},
		})
	}
	requirements = append(requirements, result.RequirementAssessment{
		ID: "chat.tools.continuation", Name: "continuation", Level: result.RequirementOptional, Subject: subject, Applicability: result.ApplicabilityApplicable, Status: result.RequirementSkip, BlockedBy: []string{"chat.tools.forced"},
	})
	r := result.Report{Summary: result.Summary{Assessment: &result.ConformanceAssessment{Verdict: "pass-with-gaps"}}, Requirements: requirements}
	Apply(&r, testProfile(), Options{Pack: "generic"})
	if len(r.Guidance.Findings) != 1 || len(r.Guidance.Recommendations) != 1 {
		t.Fatalf("tool failures were not grouped: %+v", r.Guidance)
	}
	finding := r.Guidance.Findings[0]
	if len(finding.RequirementIDs) != 3 || len(finding.BlockedRequirementIDs) != 1 || finding.BlockedRequirementIDs[0] != "chat.tools.continuation" {
		t.Fatalf("blocked dependency was not linked: %+v", finding)
	}
	if r.Guidance.Recommendations[0].Priority != "p2" {
		t.Fatalf("optional constraint has wrong priority: %+v", r.Guidance.Recommendations[0])
	}
}

func TestSameCodeWithDifferentRootParametersDoesNotCollapse(t *testing.T) {
	subject := result.SubjectRef{Kind: "provider", ID: "https://example.test/v1"}
	requirements := []result.RequirementAssessment{}
	for index, pointer := range []string{"/data/*/created", "/data/*/owned_by"} {
		requirements = append(requirements, result.RequirementAssessment{
			ID: fmt.Sprintf("models.schema.%d", index), Name: "Model schema", Level: result.RequirementOptional,
			Subject: subject, Applicability: result.ApplicabilityApplicable, Status: result.RequirementFail,
			Evidence: []result.EvidenceRef{{ID: fmt.Sprintf("native-%d", index), Kind: "native", Status: result.StatusFail, Signals: []result.EvidenceSignal{{Code: "contract.models.list.entry.created_missing", Parameters: map[string]any{"json_pointer": pointer}}}}},
		})
	}
	r := result.Report{Summary: result.Summary{Assessment: &result.ConformanceAssessment{Verdict: "partial"}}, Requirements: requirements}
	Apply(&r, testProfile(), Options{Pack: "generic"})
	if len(r.Guidance.Findings) != 2 {
		t.Fatalf("different root parameters collapsed into one finding: %+v", r.Guidance.Findings)
	}
}

func TestEvidenceConflictProducesInvestigationWithoutProjection(t *testing.T) {
	subject := result.SubjectRef{Kind: "model", ID: "model-a"}
	r := result.Report{
		Summary: result.Summary{Assessment: &result.ConformanceAssessment{Verdict: "pass-with-gaps"}},
		Requirements: []result.RequirementAssessment{{
			ID: "official-sdk.chat.stream", Name: "stream", Level: result.RequirementOptional, Subject: subject, Applicability: result.ApplicabilityApplicable,
			Status: result.RequirementWarn, FailureKind: "evidence_conflict",
			Evidence: []result.EvidenceRef{{ID: "native", Kind: "native", Status: result.StatusPass}, {ID: "sdk", Kind: "official_sdk", Status: result.StatusFail}},
		}},
	}
	Apply(&r, testProfile(), Options{Pack: "generic"})
	finding := r.Guidance.Findings[0]
	recommendation := r.Guidance.Recommendations[0]
	if finding.Class != "evidence_conflict" || finding.EvidenceState != "conflicting" || recommendation.ActionKind != "investigate" || recommendation.Priority != "p1" || recommendation.PotentialImpact.ProjectedVerdict != nil {
		t.Fatalf("unexpected conflict guidance: finding=%+v recommendation=%+v", finding, recommendation)
	}
}

func TestExecutionErrorNeverBecomesCapabilityAdvice(t *testing.T) {
	subject := result.SubjectRef{Kind: "model", ID: "model-a"}
	r := result.Report{
		Summary: result.Summary{Assessment: &result.ConformanceAssessment{Verdict: "pass-with-gaps"}},
		Requirements: []result.RequirementAssessment{{
			ID: "chat.logprobs", Name: "logprobs", Level: result.RequirementOptional, Subject: subject, Applicability: result.ApplicabilityApplicable, Status: result.RequirementError,
			Evidence: []result.EvidenceRef{{ID: "native", Kind: "native", Status: result.StatusError}},
		}},
	}
	Apply(&r, testProfile(), Options{Pack: "generic"})
	finding := r.Guidance.Findings[0]
	recommendation := r.Guidance.Recommendations[0]
	if finding.Class != "execution_failure" || finding.EvidenceState != "inconclusive" || recommendation.ActionKind != "rerun" || recommendation.PotentialImpact.ProjectedVerdict != nil {
		t.Fatalf("execution failure was misclassified: finding=%+v recommendation=%+v", finding, recommendation)
	}
}

func TestRunnerFailureIsLocalizedToExternalChecker(t *testing.T) {
	r := result.Report{
		Summary: result.Summary{Assessment: &result.ConformanceAssessment{Verdict: "pass-with-gaps"}},
		Requirements: []result.RequirementAssessment{{
			ID: "official-sdk.runner", Name: "Official SDK runner", Level: result.RequirementOptional,
			Subject: result.SubjectRef{Kind: "provider", ID: "https://example.test/v1"}, Applicability: result.ApplicabilityApplicable, Status: result.RequirementError,
			Evidence: []result.EvidenceRef{{ID: "runner", Kind: "extension", Status: result.StatusError, Signals: []result.EvidenceSignal{{Code: "extension.runner.missing_result"}}}},
		}},
	}
	Apply(&r, testProfile(), Options{Pack: "generic"})
	finding, recommendation := r.Guidance.Findings[0], r.Guidance.Recommendations[0]
	if finding.Class != "runner_defect" || finding.Attribution.Component != "extension_runner" || recommendation.ActionKind != "rerun" || recommendation.PotentialImpact.ProjectedVerdict != nil {
		t.Fatalf("runner failure was misclassified: finding=%+v recommendation=%+v", finding, recommendation)
	}
}

func TestExtensionArtifactsAreDeduplicatedAndRedacted(t *testing.T) {
	secret := "secret-value"
	base64Payload := strings.Repeat("A", 300)
	output := "Authorization: Bearer " + secret +
		"\nCookie: session=" + secret +
		"\n{\"arguments\":\"" + secret + "\"}" +
		"\nhttps://cdn.example.test/output.png?signature=" + secret +
		"\n" + base64Payload +
		"\nchecker failed"
	r := result.Report{
		Extensions: []result.ExtensionRun{{Runner: result.ExtensionRunnerResult{Output: output}}, {Runner: result.ExtensionRunnerResult{Output: output}}},
	}
	ExtractArtifacts(&r)
	if len(r.Guidance.Artifacts) != 1 {
		t.Fatalf("artifacts=%d, want one", len(r.Guidance.Artifacts))
	}
	if r.Extensions[0].Runner.Output != "" || r.Extensions[1].Runner.Output != "" || r.Extensions[0].Runner.ArtifactRef != r.Extensions[1].Runner.ArtifactRef {
		t.Fatalf("inline output was not replaced by one reference: %+v %+v", r.Extensions[0].Runner, r.Extensions[1].Runner)
	}
	for _, artifact := range r.Guidance.Artifacts {
		if strings.Contains(artifact.Content, secret) || strings.Contains(artifact.Content, base64Payload) || !strings.Contains(artifact.Content, "[REDACTED]") {
			t.Fatalf("artifact was not redacted: %q", artifact.Content)
		}
	}
}

func TestGuidanceIsDeterministic(t *testing.T) {
	base := result.Report{
		Summary:      result.Summary{Assessment: &result.ConformanceAssessment{Verdict: "pass-with-gaps"}},
		Requirements: []result.RequirementAssessment{{ID: "models.retrieve", Name: "retrieve", Level: result.RequirementOptional, Subject: result.SubjectRef{Kind: "provider", ID: "https://example.test/v1"}, Applicability: result.ApplicabilityApplicable, Status: result.RequirementUnsupported, Evidence: []result.EvidenceRef{{ID: "native", Kind: "native", Status: result.StatusUnsupported, Signals: []result.EvidenceSignal{{Code: "unsupported.models.retrieve"}}}}}},
	}
	a, b := base, base
	Apply(&a, testProfile(), Options{Pack: "generic"})
	Apply(&b, testProfile(), Options{Pack: "generic"})
	left, _ := json.Marshal(a.Guidance)
	right, _ := json.Marshal(b.Guidance)
	if string(left) != string(right) {
		t.Fatalf("guidance is not deterministic:\n%s\n%s", left, right)
	}
}

func TestGuidanceGroupsUnknownModelMetadataSkips(t *testing.T) {
	p := profile.Profile{Protocol: "google-genai"}
	r := result.Report{Requirements: []result.RequirementAssessment{
		{ID: "google.generate-content.text", Subject: result.SubjectRef{Kind: "model", ID: "opaque", TargetID: "fixture", Protocol: "google-genai"}, Applicability: result.ApplicabilityUnknown, Status: result.RequirementSkip},
		{ID: "google.generate-content.tools", Subject: result.SubjectRef{Kind: "model", ID: "opaque", TargetID: "fixture", Protocol: "google-genai"}, Applicability: result.ApplicabilityUnknown, Status: result.RequirementSkip},
	}}
	Apply(&r, p, Options{Mode: "full"})
	if len(r.Guidance.Findings) != 1 || r.Guidance.Findings[0].Code != "metadata.model_capability_unknown" || len(r.Guidance.Findings[0].RequirementIDs) != 2 {
		t.Fatalf("unknown capability skips were not grouped: %+v", r.Guidance.Findings)
	}
	if len(r.Guidance.Recommendations) != 1 || r.Guidance.Recommendations[0].Source.ID != "metadata.model-capability-unknown" {
		t.Fatalf("missing metadata recommendation: %+v", r.Guidance.Recommendations)
	}
	if got := r.Guidance.Recommendations[0].Verification[0].IDs; len(got) != 2 || got[0] != "google.generate-content.text" {
		t.Fatalf("verification must use actual requirements, got %v", got)
	}
}

func TestGuidanceRendersTypedAnthropicMetadataDiagnostics(t *testing.T) {
	p := profile.Profile{Protocol: "anthropic"}
	r := result.Report{Requirements: []result.RequirementAssessment{
		{
			ID: "anthropic.models.list.schema", Subject: result.SubjectRef{Kind: "provider", ID: "fixture", TargetID: "fixture", Protocol: "anthropic"}, Status: result.RequirementFail,
			Evidence: []result.EvidenceRef{
				{ID: "models", Kind: "native", Status: result.StatusFail, Signals: []result.EvidenceSignal{
					{Code: "anthropic.models.list.invalid_envelope", Parameters: map[string]any{"diagnostics": []any{map[string]any{"path": "data[0].capabilities.batch.supported", "model_id": "vendor/model", "issue": "missing", "expected": "boolean"}}}},
				}},
			},
		},
	}}
	Apply(&r, p, Options{Mode: "full"})
	if len(r.Guidance.Recommendations) != 1 {
		t.Fatalf("recommendations=%+v", r.Guidance.Recommendations)
	}
	steps := r.Guidance.Recommendations[0].Steps
	if len(steps) < 2 || !strings.Contains(steps[0].Text, metadataLiteral("data[0].capabilities.batch.supported")) || !strings.Contains(steps[0].Text, "vendor/model") || !strings.Contains(steps[0].Text, "boolean") {
		t.Fatalf("typed diagnostic was not rendered: %+v", steps)
	}
}

func TestUnknownMetadataRecommendationsNameProtocolFields(t *testing.T) {
	for _, tc := range []struct{ protocol, field string }{
		{"openai", "capabilities array"},
		{"anthropic", "capabilities.text.supported"},
		{"google-genai", "supportedGenerationMethods"},
	} {
		r := result.Report{Requirements: []result.RequirementAssessment{{
			ID: "fixture.check", Subject: result.SubjectRef{Kind: "model", ID: "unknown", Protocol: tc.protocol},
			Applicability: result.ApplicabilityUnknown, Status: result.RequirementSkip,
		}}}
		Apply(&r, profile.Profile{Protocol: tc.protocol}, Options{Mode: "full"})
		data, err := json.Marshal(r.Guidance.Recommendations)
		if err != nil || !strings.Contains(string(data), tc.field) {
			t.Fatalf("%s: missing actionable field %s: %s, %v", tc.protocol, tc.field, data, err)
		}
	}
}

func TestMetadataRecommendationKeepsProviderIDLiteral(t *testing.T) {
	id := "model\n![image](https://example.invalid/x)<script>`x`"
	diagnostic := map[string]any{"path": "data[0].capabilities.batch", "model_id": id, "expected": "object"}
	steps := metadataSchemaSteps(map[string]any{"diagnostics": []any{diagnostic}})
	if len(steps) == 0 {
		t.Fatal("missing recommendation")
	}
	text := steps[0].Text
	for _, unsafe := range []string{"\n", "![image]", "<script>", "`x`"} {
		if strings.Contains(text, unsafe) {
			t.Fatalf("unsafe markup %q in %q", unsafe, text)
		}
	}
	if diagnostic["model_id"] != id {
		t.Fatal("source diagnostic changed")
	}
}

func TestGuidanceEmitsTargetsWithoutBroadCommands(t *testing.T) {
	r := result.Report{
		Summary: result.Summary{Assessment: &result.ConformanceAssessment{Verdict: "partial"}},
		Requirements: []result.RequirementAssessment{{
			ID: "chat.logprobs", Name: "logprobs", Level: result.RequirementOptional,
			Subject: result.SubjectRef{TargetID: "anthropic", Protocol: "anthropic", Kind: "model", ID: "exact-model-id"}, Applicability: result.ApplicabilityApplicable, Status: result.RequirementFail,
			Evidence: []result.EvidenceRef{{ID: "native", Kind: "native", Status: result.StatusFail, Signals: []result.EvidenceSignal{{Code: "behavior.chat.logprobs.accepted_but_missing"}}}},
		}},
	}
	Apply(&r, testProfile(), Options{Pack: "generic"})
	if len(r.Guidance.Recommendations) != 1 {
		t.Fatalf("recommendations=%d, want one", len(r.Guidance.Recommendations))
	}
	verification := r.Guidance.Recommendations[0].Verification
	if len(verification) == 0 {
		t.Fatal("recommendation must retain structured verification targets")
	}
	for _, target := range verification {
		if target.Kind == "command" {
			t.Fatalf("broad command leaked into guidance: %+v", target)
		}
		if target.TargetID != "anthropic" || target.Protocol != "anthropic" {
			t.Fatalf("verification target lost protocol identity: %+v", target)
		}
	}
}

func TestArtifactReferencesAreQualifiedPerTarget(t *testing.T) {
	r := result.Report{Extensions: []result.ExtensionRun{{Runner: result.ExtensionRunnerResult{Output: "same output"}}}}
	ExtractArtifacts(&r)
	QualifyArtifacts(&r, "Anthropic Main")
	ref := r.Extensions[0].Runner.ArtifactRef
	if !strings.HasPrefix(ref, "target:anthropic-main:sha256:") {
		t.Fatalf("artifact ref=%q, want target-qualified digest", ref)
	}
	if _, ok := r.Guidance.Artifacts[ref]; !ok {
		t.Fatalf("qualified artifact key %q missing: %+v", ref, r.Guidance.Artifacts)
	}
}

func TestGuidanceFailureDoesNotChangeConformance(t *testing.T) {
	r := result.Report{
		Summary: result.Summary{Status: "fail", Assessment: &result.ConformanceAssessment{Verdict: "fail", VerdictReason: "core_failure"}},
		Requirements: []result.RequirementAssessment{{
			ID: "chat.basic", Name: "Basic chat", Level: result.RequirementCore,
			Subject: result.SubjectRef{Kind: "model", ID: "model-a"}, Applicability: result.ApplicabilityApplicable, Status: result.RequirementFail,
		}},
	}
	want := r.Summary
	Apply(&r, testProfile(), Options{Pack: "missing-pack"})
	if r.Guidance.Status != "error" || r.Guidance.Error == "" {
		t.Fatalf("missing pack must be a contained guidance error: %+v", r.Guidance)
	}
	if !reflect.DeepEqual(r.Summary, want) {
		t.Fatalf("guidance changed conformance: want=%+v got=%+v", want, r.Summary)
	}
}

func TestGuidanceOffLeavesAssessmentUntouched(t *testing.T) {
	r := result.Report{
		Summary: result.Summary{Assessment: &result.ConformanceAssessment{Verdict: "fail"}},
		Requirements: []result.RequirementAssessment{{
			ID: "chat.basic", Name: "Basic chat", Level: result.RequirementCore,
			Subject: result.SubjectRef{Kind: "model", ID: "model-a"}, Applicability: result.ApplicabilityApplicable, Status: result.RequirementFail,
		}},
	}
	want := r.Summary
	Apply(&r, testProfile(), Options{Mode: "off"})
	if r.Guidance.Status != "disabled" || len(r.Guidance.Findings) != 0 || len(r.Guidance.Recommendations) != 0 || !reflect.DeepEqual(r.Summary, want) {
		t.Fatalf("disabled guidance changed the report: summary=%+v guidance=%+v", r.Summary, r.Guidance)
	}
}

func TestFindingFingerprintIgnoresVolatileCounts(t *testing.T) {
	p := testProfile()
	subject := result.SubjectRef{Kind: "provider", ID: "https://example.test/v1"}
	a := findingFingerprint(p, "contract.models.list.entry.created_missing", subject, map[string]any{"json_pointer": "/data/*/created", "missing_entries": 3, "total_entries": 3})
	b := findingFingerprint(p, "contract.models.list.entry.created_missing", subject, map[string]any{"json_pointer": "/data/*/created", "missing_entries": 30, "total_entries": 31})
	if a != b {
		t.Fatalf("volatile counts changed fingerprint: %s != %s", a, b)
	}
}

func TestFindingFingerprintIncludesTargetIdentity(t *testing.T) {
	p, _ := profile.Load()
	a := findingFingerprint(p, "contract.test", result.SubjectRef{TargetID: "openai", Protocol: "openai", Kind: "model", ID: "same"}, nil)
	b := findingFingerprint(p, "contract.test", result.SubjectRef{TargetID: "anthropic", Protocol: "anthropic", Kind: "model", ID: "same"}, nil)
	if a == b {
		t.Fatal("the same model ID on different targets produced one fingerprint")
	}
}

func BenchmarkGuidance1000Requirements(b *testing.B) {
	requirements := make([]result.RequirementAssessment, 1000)
	for i := range requirements {
		requirements[i] = result.RequirementAssessment{
			ID:            "chat.logprobs",
			Name:          "Log probabilities",
			Level:         result.RequirementOptional,
			Subject:       result.SubjectRef{Kind: "model", ID: fmt.Sprintf("model-%04d", i)},
			Applicability: result.ApplicabilityApplicable,
			Status:        result.RequirementUnsupported,
			Evidence: []result.EvidenceRef{{
				ID: "native", Kind: "native", Status: result.StatusUnsupported,
			}},
		}
	}
	base := result.Report{
		Summary:      result.Summary{Assessment: &result.ConformanceAssessment{Verdict: "pass-with-gaps"}},
		Requirements: requirements,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		report := base
		Apply(&report, testProfile(), Options{Pack: "generic"})
	}
}

func findingByCode(findings []result.Finding, code string) *result.Finding {
	for i := range findings {
		if findings[i].Code == code {
			return &findings[i]
		}
	}
	return nil
}

func testProfile() profile.Profile {
	return profile.Profile{ID: "openai-compat", Revision: "2026-09-01", Source: "https://github.com/openai/openai-openapi"}
}
