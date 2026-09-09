package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// RequirementSpec is a profile-level expectation. Requirements are evidence
// obligations; unlike jobs, they are never used to calculate execution
// progress.
type RequirementSpec struct {
	ID       string
	Required bool
}

// JobSpec is one frozen, report-visible workflow. Its ID is deliberately kept
// verbatim: presentation code may shorten labels, but never identifiers.
type JobSpec struct {
	ID             string
	TargetID       string
	ModelID        string
	Suite          string
	Kind           string
	RequirementIDs []string
	Dependencies   []string
	AdmissionGroup string
	EstimateClass  string
	Composite      bool
	CostClass      string
	// Conditional marks a job reserved during Prepare for a branch whose
	// applicability is decided during Execute. Such a reservation must receive
	// an explicit terminal outcome, including skipped or blocked.
	Conditional bool
}

// SDKInvocation records the exact suite invocation selected during Prepare.
// Execute consumes this binding rather than independently planning suites.
type SDKInvocation struct {
	JobID   string
	SuiteID string
	ModelID string
}

// TargetBinding is the immutable, target-local result of selection and
// preflight. Values are strings so this foundation does not couple protocol
// planning to a particular provider SDK.
type TargetBinding struct {
	TargetID          string
	ProfileRevision   string
	ResolvedOverrides []string
	SelectedModels    []string
	Classification    string
	SDKInvocations    []SDKInvocation
}

// PlanInput is copied by NewRunPlan. Callers may safely reuse or mutate their
// input after construction.
type PlanInput struct {
	ID           string
	Revision     uint64
	Targets      []string
	Requirements []RequirementSpec
	Jobs         []JobSpec
	Bindings     []TargetBinding
}

// TerminalOutcome is the execution record used to reconcile a frozen plan.
// Repeated delivery of the same outcome is idempotent; conflicting delivery is
// rejected rather than changing progress silently.
type TerminalOutcome struct {
	JobID   string
	Outcome string
}

// RunPlan is the sole frozen plan for a run. Its fields stay private so callers
// cannot change IDs, selections, or bindings after validation.
type RunPlan struct {
	id           string
	revision     uint64
	targets      []string
	requirements []RequirementSpec
	jobs         []JobSpec
	bindings     []TargetBinding
	fingerprint  string
}

// NewRunPlan validates and freezes an execution plan.
func NewRunPlan(input PlanInput) (*RunPlan, error) {
	p := &RunPlan{
		id:           strings.TrimSpace(input.ID),
		revision:     input.Revision,
		targets:      cloneStrings(input.Targets),
		requirements: cloneRequirements(input.Requirements),
		jobs:         cloneJobs(input.Jobs),
		bindings:     cloneBindings(input.Bindings),
	}
	if err := ValidatePlan(p); err != nil {
		return nil, err
	}
	fingerprint, err := fingerprintPlan(p)
	if err != nil {
		return nil, fmt.Errorf("fingerprint plan: %w", err)
	}
	p.fingerprint = fingerprint
	return p, nil
}

func (p *RunPlan) ID() string {
	if p == nil {
		return ""
	}
	return p.id
}
func (p *RunPlan) Revision() uint64 {
	if p == nil {
		return 0
	}
	return p.revision
}
func (p *RunPlan) Fingerprint() string {
	if p == nil {
		return ""
	}
	return p.fingerprint
}
func (p *RunPlan) Targets() []string {
	if p == nil {
		return nil
	}
	return cloneStrings(p.targets)
}
func (p *RunPlan) Requirements() []RequirementSpec {
	if p == nil {
		return nil
	}
	return cloneRequirements(p.requirements)
}
func (p *RunPlan) Jobs() []JobSpec {
	if p == nil {
		return nil
	}
	return cloneJobs(p.jobs)
}
func (p *RunPlan) Bindings() []TargetBinding {
	if p == nil {
		return nil
	}
	return cloneBindings(p.bindings)
}

// ValidateFingerprint prevents Execute from silently using selection, override,
// or profile state different from the frozen plan.
func (p *RunPlan) ValidateFingerprint(fingerprint string) error {
	if p == nil {
		return fmt.Errorf("nil run plan")
	}
	if fingerprint != p.fingerprint {
		return fmt.Errorf("run plan fingerprint mismatch")
	}
	return nil
}

// ValidatePlan verifies all local invariants before API execution.
func ValidatePlan(p *RunPlan) error {
	if p == nil {
		return fmt.Errorf("nil run plan")
	}
	if p.id == "" {
		return fmt.Errorf("plan id is required")
	}
	if p.revision == 0 {
		return fmt.Errorf("plan revision is required")
	}
	targets := make(map[string]struct{}, len(p.targets))
	for _, target := range p.targets {
		target = strings.TrimSpace(target)
		if target == "" {
			return fmt.Errorf("plan contains empty target id")
		}
		if _, exists := targets[target]; exists {
			return fmt.Errorf("duplicate target id %q", target)
		}
		targets[target] = struct{}{}
	}
	requirements := make(map[string]struct{}, len(p.requirements))
	for _, requirement := range p.requirements {
		id := strings.TrimSpace(requirement.ID)
		if id == "" {
			return fmt.Errorf("plan contains empty requirement id")
		}
		if _, exists := requirements[id]; exists {
			return fmt.Errorf("duplicate requirement id %q", id)
		}
		requirements[id] = struct{}{}
	}
	jobs := make(map[string]JobSpec, len(p.jobs))
	for _, job := range p.jobs {
		id := strings.TrimSpace(job.ID)
		if id == "" {
			return fmt.Errorf("plan contains empty job id")
		}
		if _, exists := jobs[id]; exists {
			return fmt.Errorf("duplicate job id %q", id)
		}
		if _, exists := targets[job.TargetID]; !exists {
			return fmt.Errorf("job %q references unknown target %q", id, job.TargetID)
		}
		for _, requirementID := range job.RequirementIDs {
			if _, exists := requirements[requirementID]; !exists {
				return fmt.Errorf("job %q references unknown requirement %q", id, requirementID)
			}
		}
		jobs[id] = job
	}
	for id, job := range jobs {
		for _, dependency := range job.Dependencies {
			if dependency == id {
				return fmt.Errorf("job %q depends on itself", id)
			}
			if _, exists := jobs[dependency]; !exists {
				return fmt.Errorf("job %q references unknown dependency %q", id, dependency)
			}
		}
	}
	bound := make(map[string]struct{}, len(p.jobs))
	for _, binding := range p.bindings {
		if _, exists := targets[binding.TargetID]; !exists {
			return fmt.Errorf("binding references unknown target %q", binding.TargetID)
		}
		for _, invocation := range binding.SDKInvocations {
			job, exists := jobs[invocation.JobID]
			if !exists {
				return fmt.Errorf("binding %q references unknown job %q", binding.TargetID, invocation.JobID)
			}
			if job.TargetID != binding.TargetID {
				return fmt.Errorf("binding %q cannot invoke job %q for target %q", binding.TargetID, invocation.JobID, job.TargetID)
			}
			if _, duplicate := bound[invocation.JobID]; duplicate {
				return fmt.Errorf("job %q has duplicate execution binding", invocation.JobID)
			}
			bound[invocation.JobID] = struct{}{}
		}
	}
	return nil
}

// ValidateTerminalOutcomes reconciles actual execution with frozen IDs. It
// accepts retries of an already delivered terminal outcome but catches unknown
// IDs, contradictory outcomes, and unresolved conditional reservations.
func (p *RunPlan) ValidateTerminalOutcomes(outcomes []TerminalOutcome) error {
	if err := ValidatePlan(p); err != nil {
		return err
	}
	jobs := make(map[string]JobSpec, len(p.jobs))
	for _, job := range p.jobs {
		jobs[job.ID] = job
	}
	terminal := make(map[string]string, len(outcomes))
	for _, actual := range outcomes {
		if strings.TrimSpace(actual.Outcome) == "" {
			return fmt.Errorf("job %q has empty terminal outcome", actual.JobID)
		}
		if _, exists := jobs[actual.JobID]; !exists {
			return fmt.Errorf("terminal outcome references unknown frozen job %q", actual.JobID)
		}
		if previous, exists := terminal[actual.JobID]; exists && previous != actual.Outcome {
			return fmt.Errorf("job %q has conflicting terminal outcomes %q and %q", actual.JobID, previous, actual.Outcome)
		}
		terminal[actual.JobID] = actual.Outcome
	}
	for id, job := range jobs {
		if job.Conditional && terminal[id] == "" {
			return fmt.Errorf("conditional job %q has no terminal outcome", id)
		}
	}
	return nil
}

func fingerprintPlan(p *RunPlan) (string, error) {
	type fingerprint struct {
		ID           string
		Revision     uint64
		Targets      []string
		Requirements []RequirementSpec
		Jobs         []JobSpec
		Bindings     []TargetBinding
	}
	f := fingerprint{p.id, p.revision, cloneStrings(p.targets), cloneRequirements(p.requirements), cloneJobs(p.jobs), cloneBindings(p.bindings)}
	sort.Strings(f.Targets)
	sort.Slice(f.Requirements, func(i, j int) bool { return f.Requirements[i].ID < f.Requirements[j].ID })
	sort.Slice(f.Jobs, func(i, j int) bool { return f.Jobs[i].ID < f.Jobs[j].ID })
	sort.Slice(f.Bindings, func(i, j int) bool { return f.Bindings[i].TargetID < f.Bindings[j].TargetID })
	for i := range f.Jobs {
		sort.Strings(f.Jobs[i].RequirementIDs)
		sort.Strings(f.Jobs[i].Dependencies)
	}
	for i := range f.Bindings {
		sort.Strings(f.Bindings[i].ResolvedOverrides)
		sort.Strings(f.Bindings[i].SelectedModels)
		sort.Slice(f.Bindings[i].SDKInvocations, func(a, b int) bool {
			return f.Bindings[i].SDKInvocations[a].JobID < f.Bindings[i].SDKInvocations[b].JobID
		})
	}
	data, err := json.Marshal(f)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func cloneStrings(in []string) []string { return append([]string(nil), in...) }
func cloneRequirements(in []RequirementSpec) []RequirementSpec {
	return append([]RequirementSpec(nil), in...)
}
func cloneJobs(in []JobSpec) []JobSpec {
	out := append([]JobSpec(nil), in...)
	for i := range out {
		out[i].RequirementIDs = cloneStrings(out[i].RequirementIDs)
		out[i].Dependencies = cloneStrings(out[i].Dependencies)
	}
	return out
}
func cloneBindings(in []TargetBinding) []TargetBinding {
	out := append([]TargetBinding(nil), in...)
	for i := range out {
		out[i].ResolvedOverrides = cloneStrings(out[i].ResolvedOverrides)
		out[i].SelectedModels = cloneStrings(out[i].SelectedModels)
		out[i].SDKInvocations = append([]SDKInvocation(nil), out[i].SDKInvocations...)
	}
	return out
}
