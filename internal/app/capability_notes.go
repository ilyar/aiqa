package app

import (
	"fmt"

	"github.com/ilyar/aiqa/internal/result"
)

// These notes describe test selection, not conformance requirements. Missing
// optional metadata must not turn successful API probes into a warning verdict.
func appendProbePolicyNotes(target *result.TargetReport) {
	seen := make(map[string]bool, len(target.Notes))
	for _, note := range target.Notes {
		seen[note] = true
	}
	for _, model := range target.Models {
		for _, module := range model.Modules {
			if module.Source != "probe_policy" || !module.Requested {
				continue
			}
			note := fmt.Sprintf("Model %q: capability metadata is absent. AIQA selects eligible probes from the requested profile to test support; it does not assume support. Existing cost and resource permissions still apply. This metadata advisory does not change the compatibility verdict.", model.ID)
			if !seen[note] {
				target.Notes = append(target.Notes, note)
				seen[note] = true
			}
			break
		}
	}
}
