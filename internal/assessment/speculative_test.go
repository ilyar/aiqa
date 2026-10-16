package assessment

import (
	"testing"

	"github.com/ilyar/aiqa/internal/result"
)

func TestSpeculativeImageRejectionRetainsEvidenceAndRejectsConflicts(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		extra        result.Status
		exclude      bool
	}{
		{"trial rejection", "probe_policy", "", true},
		{"advertised support", "vendor_metadata", "", false},
		{"SDK success contradicts rejection", "probe_policy", result.StatusPass, false},
		{"SDK failure must remain visible", "probe_policy", result.StatusFail, false},
		{"SDK unavailable", "probe_policy", result.StatusError, false},
		{"SDK confirms unsupported", "probe_policy", result.StatusUnsupported, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			subject := result.SubjectRef{Kind: "model", ID: "opaque"}
			basic := &result.RequirementAssessment{ID: "images.generate.basic", Module: "images-generations", Subject: subject, Applicability: result.ApplicabilityApplicable, Evidence: []result.EvidenceRef{{Kind: "native", Status: result.StatusUnsupported, Signals: []result.EvidenceSignal{{Code: "capability.probe.unsupported"}}}}}
			items := map[requirementKey]*result.RequirementAssessment{{subjectKind: "model", subjectID: "opaque", id: basic.ID}: basic}
			if tc.extra != "" {
				items[requirementKey{subjectKind: "model", subjectID: "opaque", id: "sdk"}] = &result.RequirementAssessment{ID: "sdk", Module: "images-generations", Subject: subject, Evidence: []result.EvidenceRef{{Kind: "extension", Status: tc.extra}}}
			}
			r := result.Report{Models: []result.ModelReport{{ID: "opaque", Modules: []result.ModuleAssessment{{ID: "images-generations", Source: tc.source, Requested: true, Applicability: result.ApplicabilityApplicable, Execution: result.ExecutionExecuted}}}}}
			reconcileSpeculativeImageCoverage(items, &r)
			if got := basic.Applicability == result.ApplicabilityNotApplicable; got != tc.exclude {
				t.Fatalf("excluded=%v want %v", got, tc.exclude)
			}
			if len(basic.Evidence) != 1 || basic.Evidence[0].Status != result.StatusUnsupported || r.Models[0].Modules[0].Execution != result.ExecutionExecuted {
				t.Fatal("raw observation or execution erased")
			}
		})
	}
}
