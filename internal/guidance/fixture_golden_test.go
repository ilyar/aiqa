package guidance

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ilyar/aiqa/internal/assessment"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

func TestVibecodeActionableFixtureGolden(t *testing.T) {
	report := vibecodeActionableFixture()
	runActionableFixture(t, &report)
	assertGolden(t, "vibecode-actions.golden.txt", actionableSnapshot(report))
}

func TestQwenActionableFixtureGolden(t *testing.T) {
	report := qwenActionableFixture()
	runActionableFixture(t, &report)
	assertGolden(t, "qwen-actions.golden.txt", actionableSnapshot(report))
}

func runActionableFixture(t *testing.T, report *result.Report) {
	t.Helper()
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	ExtractArtifacts(report)
	assessment.Apply(report, p)
	Apply(report, p, Options{Mode: "full", Pack: "generic", Locale: "en"})
}

func vibecodeActionableFixture() result.Report {
	providerTest := result.TestResult{
		ID: "models.list.schema", Name: "List models: schema", Required: true, Status: result.StatusFail,
		Summary: "model list schema differs from OpenAI",
		Signals: []result.EvidenceSignal{{Code: "contract.models.list.entry.created_missing", Parameters: map[string]any{
			"endpoint": "GET /v1/models", "json_pointer": "/data/*/created", "missing_entries": 30, "total_entries": 30,
		}}},
	}
	model := result.ModelReport{
		ID: "gpt-5.4-mini",
		Compatibility: []result.TestResult{
			{ID: "chat.stream.sse", Name: "Streaming SSE", Required: true, Status: result.StatusPass, Summary: "valid stream"},
			{ID: "chat.error.invalid_types", Name: "Invalid types", Status: result.StatusError, Summary: "invalid types returned 502", HTTP: result.HTTPObservation{StatusCode: 502}, Signals: []result.EvidenceSignal{{Code: "contract.chat.invalid_request.upstream_5xx", Parameters: map[string]any{"endpoint": "POST /v1/chat/completions", "status_code": 502}}}},
			{ID: "chat.logprobs", Name: "Log probabilities", Status: result.StatusFail, Summary: "request accepted but choices[0].logprobs is missing", Signals: []result.EvidenceSignal{{Code: "behavior.chat.logprobs.accepted_but_missing"}}},
			{ID: "models.retrieve", Name: "Retrieve model", Status: result.StatusUnsupported, Summary: "GET /models/{model} returned 404", Signals: []result.EvidenceSignal{{Code: "unsupported.models.retrieve"}}},
		},
	}
	return result.Report{
		Config:    result.PublicConfig{BaseURL: "https://vibecode.example/v1", Guidance: "full"},
		Discovery: result.Discovery{Tests: []result.TestResult{providerTest}},
		Models:    []result.ModelReport{model},
		Extensions: []result.ExtensionRun{
			{Provenance: result.ExtensionProvenance{ID: "official-sdk.openai"}, Subject: result.SubjectRef{Kind: "provider", ID: "https://vibecode.example/v1"}, Runner: result.ExtensionRunnerResult{Status: result.StatusError, Output: "extension: model object missing created"}},
			{Provenance: result.ExtensionProvenance{ID: "official-sdk.openai"}, Subject: result.SubjectRef{Kind: "model", ID: "gpt-5.4-mini"}, Runner: result.ExtensionRunnerResult{Status: result.StatusPass}, Suites: []result.ExtensionSuiteResult{{ExtensionID: "official-sdk.openai", SuiteID: "chat.stream", Subject: result.SubjectRef{Kind: "model", ID: "gpt-5.4-mini"}, Status: result.StatusFail, Bound: true, RequirementID: "official-sdk.openai.chat.stream", RelatedRequirementIDs: []string{"chat.stream.sse"}, Summary: "stream chunk missing choices"}}},
		},
	}
}

func qwenActionableFixture() result.Report {
	coreProvider := []result.TestResult{
		{ID: "models.list.http", Name: "List models HTTP", Required: true, Status: result.StatusPass, Summary: "pass"},
		{ID: "models.list.schema", Name: "List models schema", Required: true, Status: result.StatusPass, Summary: "pass"},
	}
	coreModel := []result.TestResult{
		{ID: "chat.basic", Name: "Basic chat", Required: true, Status: result.StatusPass, Summary: "pass"},
		{ID: "chat.schema", Name: "Chat schema", Required: true, Status: result.StatusPass, Summary: "pass"},
		{ID: "chat.usage", Name: "Usage", Required: true, Status: result.StatusPass, Summary: "pass"},
		{ID: "chat.stream.sse", Name: "Streaming SSE", Required: true, Status: result.StatusPass, Summary: "pass"},
		{ID: "chat.error.invalid_model", Name: "Invalid model", Required: true, Status: result.StatusPass, Summary: "pass"},
		{ID: "chat.error.invalid_json", Name: "Invalid JSON", Required: true, Status: result.StatusPass, Summary: "pass"},
	}
	for _, item := range []struct{ id, mode string }{
		{"chat.tools.required", "required"},
		{"chat.tools.forced", "forced"},
		{"chat.tools.stream", "stream"},
	} {
		coreModel = append(coreModel, result.TestResult{
			ID: item.id, Name: item.id, Status: result.StatusUnsupported, Summary: item.mode + " tool choice is unavailable in thinking mode",
			Signals: []result.EvidenceSignal{{Code: "configuration.chat.tools.tool_choice_constraint", Parameters: map[string]any{"mode": item.mode}}},
		})
	}
	coreModel = append(coreModel,
		result.TestResult{ID: "chat.tools.continuation", Name: "Tool continuation", Status: result.StatusSkip, Summary: "blocked by forced tool choice", Signals: []result.EvidenceSignal{{Code: "blocked.chat.tools.continuation", Parameters: map[string]any{"blocked_by_requirement": "chat.tools.forced"}}}},
		result.TestResult{ID: "chat.role.developer", Name: "Developer role", Status: result.StatusUnsupported, Summary: "developer role rejected", Signals: []result.EvidenceSignal{{Code: "unsupported.chat.role.developer"}}},
	)
	return result.Report{
		Config:    result.PublicConfig{BaseURL: "https://dashscope.example/compatible-mode/v1", Guidance: "full"},
		Discovery: result.Discovery{Tests: coreProvider},
		Models: []result.ModelReport{{
			ID:            "qwen3.8-flash",
			Compatibility: coreModel,
		}},
		Extensions: []result.ExtensionRun{{Provenance: result.ExtensionProvenance{ID: "official-sdk.openai"}, Subject: result.SubjectRef{Kind: "model", ID: "qwen3.8-flash"}, Runner: result.ExtensionRunnerResult{Status: result.StatusPass}, Suites: []result.ExtensionSuiteResult{{ExtensionID: "official-sdk.openai", SuiteID: "chat.stream", Subject: result.SubjectRef{Kind: "model", ID: "qwen3.8-flash"}, Status: result.StatusFail, Bound: true, RequirementID: "official-sdk.openai.chat.stream", RelatedRequirementIDs: []string{"chat.stream.sse"}, Summary: "stream parser rejected an empty choices chunk"}}}},
	}
}

func actionableSnapshot(report result.Report) string {
	assessment := report.Summary.Assessment
	var b strings.Builder
	fmt.Fprintf(&b, "verdict=%s core=%d/%d optional=%d/%d evidence=%d/%d\n",
		assessment.Verdict, assessment.Core.Pass, assessment.Core.Total, assessment.Optional.Pass, assessment.Optional.Total, assessment.Evidence.Conclusive, assessment.Evidence.Total)
	findings := append([]result.Finding(nil), report.Guidance.Findings...)
	sort.Slice(findings, func(i, j int) bool { return findings[i].Code < findings[j].Code })
	for _, finding := range findings {
		fmt.Fprintf(&b, "finding %s class=%s state=%s subject=%s:%s requirements=%s blocked=%s evidence=%d\n",
			finding.Code, finding.Class, finding.EvidenceState, finding.Subject.Kind, finding.Subject.ID,
			strings.Join(finding.RequirementIDs, ","), strings.Join(finding.BlockedRequirementIDs, ","), len(finding.EvidenceRefs))
	}
	for _, recommendation := range report.Guidance.Recommendations {
		projected := "null"
		if recommendation.PotentialImpact.ProjectedVerdict != nil {
			projected = *recommendation.PotentialImpact.ProjectedVerdict
		}
		fmt.Fprintf(&b, "recommendation %s %s level=%d confidence=%s projected=%s title=%s\n",
			strings.ToUpper(recommendation.Priority), recommendation.ActionKind, recommendation.GuidanceLevel,
			recommendation.Confidence, projected, recommendation.Title)
	}
	fmt.Fprintf(&b, "artifacts=%d coverage=%.1f actionable=%.1f\n", len(report.Guidance.Artifacts), report.Guidance.Summary.GuidanceCoveragePercent, report.Guidance.Summary.ActionableCoveragePercent)
	return b.String()
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v\nactual:\n%s", path, err, got)
	}
	if got != string(want) {
		t.Fatalf("golden mismatch for %s\nwant:\n%s\ngot:\n%s", name, want, got)
	}
}
