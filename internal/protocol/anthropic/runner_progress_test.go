package anthropic

import (
	"testing"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/result"
)

func TestPlannedProbeCountCountsLogicalUnitsRatherThanResultRows(t *testing.T) {
	cfg := config.Config{Tests: []string{"compat"}}
	modules := [][]result.ModuleAssessment{{
		{ID: "anthropic.messages", Applicability: result.ApplicabilityApplicable, Execution: result.ExecutionExecuted},
		{ID: "anthropic.token-counting", Applicability: result.ApplicabilityApplicable, Execution: result.ExecutionExecuted},
	}}
	if got := plannedProbeCount(cfg, []string{"fixture"}, modules, []bool{false}); got != 19 {
		t.Fatalf("planned probe count = %d, want 19 (16 message lifecycles + 3 token probes)", got)
	}
}
