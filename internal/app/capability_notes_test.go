package app

import (
	"strings"
	"testing"

	"github.com/ilyar/aiqa/internal/result"
)

func TestProbePolicyNotesAreAdvisoryAndDeduplicated(t *testing.T) {
	target := result.TargetReport{
		Notes:   []string{"existing SDK limitation"},
		Summary: result.Summary{Assessment: &result.ConformanceAssessment{Verdict: "pass"}},
		Models: []result.ModelReport{
			{ID: "opaque", Modules: []result.ModuleAssessment{
				{ID: "chat", Source: "probe_policy", Requested: true},
				{ID: "tools", Source: "probe_policy", Requested: true},
			}},
			{ID: "known", Modules: []result.ModuleAssessment{{Source: "vendor_metadata", Requested: true}}},
			{ID: "unrequested", Modules: []result.ModuleAssessment{{Source: "probe_policy"}}},
		},
	}
	appendProbePolicyNotes(&target)
	appendProbePolicyNotes(&target)
	if len(target.Notes) != 2 || target.Notes[0] != "existing SDK limitation" || !strings.Contains(target.Notes[1], `Model "opaque"`) {
		t.Fatalf("unexpected notes: %v", target.Notes)
	}
	if target.Summary.Assessment.Verdict != "pass" || len(target.Requirements) != 0 {
		t.Fatal("advisory changed canonical assessment")
	}
}
