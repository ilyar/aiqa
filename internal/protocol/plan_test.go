package protocol

import (
	"strings"
	"testing"
)

func validPlanInput() PlanInput {
	return PlanInput{
		ID:       "run-1",
		Revision: 1,
		Targets:  []string{"openai"},
		Requirements: []RequirementSpec{
			{ID: "chat.basic", Required: true},
			{ID: "chat.usage", Required: true},
		},
		Jobs: []JobSpec{
			{ID: "native:openai:model-a:compat", TargetID: "openai", ModelID: "model-a", Suite: "compat", Kind: "native_workflow", RequirementIDs: []string{"chat.basic", "chat.usage"}},
			{ID: "sdk:openai:model-a:chat", TargetID: "openai", ModelID: "model-a", Suite: "chat", Kind: "sdk_suite", Conditional: true},
		},
		Bindings: []TargetBinding{{
			TargetID: "openai", ProfileRevision: "2026-09", ResolvedOverrides: []string{"models=model-a"}, SelectedModels: []string{"model-a"},
			SDKInvocations: []SDKInvocation{{JobID: "sdk:openai:model-a:chat", SuiteID: "chat", ModelID: "model-a"}},
		}},
	}
}

func TestRunPlanSeparatesRequirementsFromJobsAndIsImmutable(t *testing.T) {
	input := validPlanInput()
	plan, err := NewRunPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Requirements()) != 2 || len(plan.Jobs()) != 2 {
		t.Fatalf("requirements and jobs must remain distinct: requirements=%d jobs=%d", len(plan.Requirements()), len(plan.Jobs()))
	}
	input.Jobs[0].ID = "changed-after-freeze"
	jobs := plan.Jobs()
	jobs[0].ID = "changed-through-getter"
	if got := plan.Jobs()[0].ID; got != "native:openai:model-a:compat" {
		t.Fatalf("plan was mutable: %q", got)
	}
}

func TestRunPlanFingerprintIsStableAcrossEquivalentInputOrder(t *testing.T) {
	first := validPlanInput()
	second := validPlanInput()
	second.Targets = []string{"openai"}
	second.Requirements[0], second.Requirements[1] = second.Requirements[1], second.Requirements[0]
	second.Jobs[0], second.Jobs[1] = second.Jobs[1], second.Jobs[0]
	second.Jobs[1].RequirementIDs = []string{"chat.usage", "chat.basic"}
	planA, err := NewRunPlan(first)
	if err != nil {
		t.Fatal(err)
	}
	planB, err := NewRunPlan(second)
	if err != nil {
		t.Fatal(err)
	}
	if planA.Fingerprint() != planB.Fingerprint() {
		t.Fatalf("equivalent frozen plans have different fingerprints: %s != %s", planA.Fingerprint(), planB.Fingerprint())
	}
	if err := planA.ValidateFingerprint(planB.Fingerprint()); err != nil {
		t.Fatalf("equivalent fingerprint rejected: %v", err)
	}
	if err := planA.ValidateFingerprint("changed"); err == nil {
		t.Fatal("changed selection fingerprint was accepted")
	}
}

func TestRunPlanRejectsInvalidBindingsBeforeExecution(t *testing.T) {
	input := validPlanInput()
	input.Bindings[0].SDKInvocations[0].JobID = "sdk:unplanned"
	_, err := NewRunPlan(input)
	if err == nil || !strings.Contains(err.Error(), "unknown job") {
		t.Fatalf("unplanned preflight route was accepted: %v", err)
	}
}

func TestRunPlanLongIDsRemainDistinct(t *testing.T) {
	input := validPlanInput()
	prefix := strings.Repeat("x", 96)
	input.Jobs[0].ID = prefix + "-first"
	input.Jobs[1].ID = prefix + "-second-\u65e5\u672c\u8a9e"
	input.Bindings[0].SDKInvocations[0].JobID = input.Jobs[1].ID
	plan, err := NewRunPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.ValidateTerminalOutcomes([]TerminalOutcome{{JobID: input.Jobs[0].ID, Outcome: "completed"}, {JobID: input.Jobs[1].ID, Outcome: "skipped"}}); err != nil {
		t.Fatalf("long IDs were not preserved: %v", err)
	}
}

func TestRunPlanTerminalOutcomesAreFrozenAndConditional(t *testing.T) {
	plan, err := NewRunPlan(validPlanInput())
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.ValidateTerminalOutcomes([]TerminalOutcome{{JobID: "native:openai:model-a:compat", Outcome: "completed"}}); err == nil || !strings.Contains(err.Error(), "conditional") {
		t.Fatalf("missing conditional outcome was accepted: %v", err)
	}
	if err := plan.ValidateTerminalOutcomes([]TerminalOutcome{{JobID: "sdk:openai:model-a:chat", Outcome: "skipped"}, {JobID: "sdk:openai:model-a:chat", Outcome: "skipped"}}); err != nil {
		t.Fatalf("duplicate same outcome must be idempotent: %v", err)
	}
	if err := plan.ValidateTerminalOutcomes([]TerminalOutcome{{JobID: "sdk:openai:model-a:chat", Outcome: "skipped"}, {JobID: "sdk:openai:model-a:chat", Outcome: "completed"}}); err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("conflicting terminal outcomes were accepted: %v", err)
	}
	if err := plan.ValidateTerminalOutcomes([]TerminalOutcome{{JobID: "unknown", Outcome: "completed"}, {JobID: "sdk:openai:model-a:chat", Outcome: "skipped"}}); err == nil || !strings.Contains(err.Error(), "unknown frozen") {
		t.Fatalf("unknown actual ID was accepted: %v", err)
	}
}
