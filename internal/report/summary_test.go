package report

import (
	"testing"

	"github.com/ilyar/aiqa/internal/result"
)

func TestNotApplicableModulesDoNotAffectScore(t *testing.T) {
	report := result.Report{Models: []result.ModelReport{{
		ID: "image-only",
		Modules: []result.ModuleAssessment{{
			ID:            "chat-completions",
			Capability:    "chat.completions.text",
			Applicability: result.ApplicabilityNotApplicable,
			Reason:        "not a chat model",
		}},
	}}}
	Finalize(&report)
	if got := report.Models[0].Summary; got.Status != "not-evaluated" || got.Score != 0 || got.Pass+got.Fail+got.Warn+got.Unsupported+got.Skip+got.Error != 0 {
		t.Fatalf("N/A module affected model summary: %+v", got)
	}
	if report.Summary.Status != "not-evaluated" {
		t.Fatalf("inventory-only report status=%q, want not-evaluated", report.Summary.Status)
	}

	report.Models[0].Modules = append(report.Models[0].Modules, result.ModuleAssessment{
		ID: "future-module", Capability: "future", Applicability: result.ApplicabilityNotApplicable, Reason: "not applicable",
	})
	Finalize(&report)
	if got := report.Models[0].Summary; got.Status != "not-evaluated" || got.Score != 0 {
		t.Fatalf("new inapplicable module changed model score: %+v", got)
	}
}

func TestProviderExternalResultContributesOnlyToProviderSummary(t *testing.T) {
	report := result.Report{
		Discovery: result.Discovery{External: []result.ExternalResult{{Tool: "official-SDK", Suite: "models", Status: result.StatusFail, Summary: "schema mismatch"}}},
		Models:    []result.ModelReport{{ID: "inventory-only"}},
	}
	Finalize(&report)
	if report.Summary.Fail != 1 || report.Summary.Status != "partial" {
		t.Fatalf("provider result missing from report score: %+v", report.Summary)
	}
	if got := report.Models[0].Summary; got.Status != "not-evaluated" || got.Fail != 0 {
		t.Fatalf("provider failure leaked into model assessment: %+v", got)
	}
}
