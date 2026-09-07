package openai

import (
	"testing"

	"github.com/ilyar/aiqa/internal/capability"
	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/suites"
)

func TestRepresentativeModelSkipsEarlierInapplicableModels(t *testing.T) {
	models := []string{"aaa-image", "bbb-unknown", "ccc-chat"}
	plans := [][]result.ModuleAssessment{
		{{ID: "chat-completions", Applicability: result.ApplicabilityNotApplicable}},
		{{ID: "chat-completions", Applicability: result.ApplicabilityUnknown}},
		{{ID: "chat-completions", Applicability: result.ApplicabilityApplicable, Execution: result.ExecutionExecuted}},
	}
	if got := representativeModel(models, plans, "chat-completions"); got != "ccc-chat" {
		t.Fatalf("representative model=%q, want ccc-chat", got)
	}
}

func TestPlannedNativeProbesCountsLifecycleUnitsOnly(t *testing.T) {
	cfg := config.Config{Tests: []string{"compat"}}
	plans := [][]result.ModuleAssessment{
		{{ID: capability.ModuleChat, Execution: result.ExecutionExecuted}, {ID: capability.ModuleImages, Execution: result.ExecutionExecuted}},
		{{ID: capability.ModuleChat, Execution: result.ExecutionExecuted}},
	}
	want := suites.EndpointCompatProbeCount + suites.ModelCompatProbeCount*2 + suites.ImageCompatProbeCount
	if got := plannedNativeProbes(cfg, plans, []bool{false, false}, "model-a"); got != want {
		t.Fatalf("planned probes = %d, want %d", got, want)
	}
	if got := plannedNativeProbes(cfg, plans, []bool{true, false}, "model-b"); got != suites.EndpointCompatProbeCount+suites.ModelCompatProbeCount {
		t.Fatalf("route-blocked plan = %d", got)
	}
}
