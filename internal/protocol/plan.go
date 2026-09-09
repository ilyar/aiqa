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
	Label          string
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

// TargetExecutionPlan is the frozen, target-local execution view. It joins
// the binding resolved during Prepare with precisely the jobs Execute may
// claim for that target. Its exported values are copies: callers cannot alter
// the RunPlan after Freeze.
type TargetExecutionPlan struct {
	TargetID string
	Binding  TargetBinding
	Jobs     []JobSpec
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

// Job returns one exact frozen job by its full ID. It never abbreviates IDs,
// because progress and terminal-outcome reconciliation use the same value.
func (p *RunPlan) Job(id string) (JobSpec, bool) {
	if p == nil {
		return JobSpec{}, false
	}
	for _, job := range p.jobs {
		if job.ID == id {
			return cloneJobs([]JobSpec{job})[0], true
		}
	}
	return JobSpec{}, false
}

// Binding returns the exact target-local binding frozen during Prepare.
func (p *RunPlan) Binding(targetID string) (TargetBinding, bool) {
	if p == nil {
		return TargetBinding{}, false
	}
	for _, binding := range p.bindings {
		if binding.TargetID == targetID {
			return cloneBindings([]TargetBinding{binding})[0], true
		}
	}
	return TargetBinding{}, false
}

// TargetExecution returns the complete frozen execution view for one target.
// Consumers use this instead of independently deriving jobs from selection,
// profile, or extension configuration after Freeze.
func (p *RunPlan) TargetExecution(targetID string) (TargetExecutionPlan, bool) {
	binding, ok := p.Binding(targetID)
	if !ok {
		return TargetExecutionPlan{}, false
	}
	execution := TargetExecutionPlan{TargetID: targetID, Binding: binding}
	for _, job := range p.jobs {
		if job.TargetID == targetID {
			execution.Jobs = append(execution.Jobs, cloneJobs([]JobSpec{job})[0])
		}
	}
	return execution, true
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
		if err := validateFrozenID("target id", target); err != nil {
			return err
		}
		if _, exists := targets[target]; exists {
			return fmt.Errorf("duplicate target id %q", target)
		}
		targets[target] = struct{}{}
	}
	requirements := make(map[string]struct{}, len(p.requirements))
	for _, requirement := range p.requirements {
		if err := validateFrozenID("requirement id", requirement.ID); err != nil {
			return err
		}
		if _, exists := requirements[requirement.ID]; exists {
			return fmt.Errorf("duplicate requirement id %q", requirement.ID)
		}
		requirements[requirement.ID] = struct{}{}
	}
	jobs := make(map[string]JobSpec, len(p.jobs))
	for _, job := range p.jobs {
		if err := validateFrozenID("job id", job.ID); err != nil {
			return err
		}
		if err := validateFrozenID("job target id", job.TargetID); err != nil {
			return fmt.Errorf("job %q: %w", job.ID, err)
		}
		if err := validateFrozenID("job suite", job.Suite); err != nil {
			return fmt.Errorf("job %q: %w", job.ID, err)
		}
		if err := validateFrozenID("job kind", job.Kind); err != nil {
			return fmt.Errorf("job %q: %w", job.ID, err)
		}
		if job.ModelID != "" {
			if err := validateFrozenID("job model id", job.ModelID); err != nil {
				return fmt.Errorf("job %q: %w", job.ID, err)
			}
		}
		if _, exists := jobs[job.ID]; exists {
			return fmt.Errorf("duplicate job id %q", job.ID)
		}
		if _, exists := targets[job.TargetID]; !exists {
			return fmt.Errorf("job %q references unknown target %q", job.ID, job.TargetID)
		}
		for _, requirementID := range job.RequirementIDs {
			if _, exists := requirements[requirementID]; !exists {
				return fmt.Errorf("job %q references unknown requirement %q", job.ID, requirementID)
			}
		}
		jobs[job.ID] = job
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
	bindings := make(map[string]TargetBinding, len(p.bindings))
	bound := make(map[string]struct{}, len(p.jobs))
	for _, binding := range p.bindings {
		if err := validateFrozenID("binding target id", binding.TargetID); err != nil {
			return err
		}
		if _, exists := targets[binding.TargetID]; !exists {
			return fmt.Errorf("binding references unknown target %q", binding.TargetID)
		}
		if _, duplicate := bindings[binding.TargetID]; duplicate {
			return fmt.Errorf("duplicate binding for target %q", binding.TargetID)
		}
		if err := validateFrozenID("binding profile revision", binding.ProfileRevision); err != nil {
			return fmt.Errorf("binding %q: %w", binding.TargetID, err)
		}
		selectedModels := make(map[string]struct{}, len(binding.SelectedModels))
		for _, modelID := range binding.SelectedModels {
			if err := validateFrozenID("selected model id", modelID); err != nil {
				return fmt.Errorf("binding %q: %w", binding.TargetID, err)
			}
			if _, duplicate := selectedModels[modelID]; duplicate {
				return fmt.Errorf("binding %q has duplicate selected model %q", binding.TargetID, modelID)
			}
			selectedModels[modelID] = struct{}{}
		}
		for _, job := range jobs {
			if job.TargetID != binding.TargetID || job.ModelID == "" {
				continue
			}
			if _, selected := selectedModels[job.ModelID]; !selected {
				return fmt.Errorf("binding %q has job %q for unselected model %q", binding.TargetID, job.ID, job.ModelID)
			}
		}
		for _, invocation := range binding.SDKInvocations {
			if err := validateFrozenID("SDK invocation job id", invocation.JobID); err != nil {
				return fmt.Errorf("binding %q: %w", binding.TargetID, err)
			}
			if err := validateFrozenID("SDK invocation suite id", invocation.SuiteID); err != nil {
				return fmt.Errorf("binding %q: %w", binding.TargetID, err)
			}
			if invocation.ModelID != "" {
				if err := validateFrozenID("SDK invocation model id", invocation.ModelID); err != nil {
					return fmt.Errorf("binding %q: %w", binding.TargetID, err)
				}
			}
			job, exists := jobs[invocation.JobID]
			if !exists {
				return fmt.Errorf("binding %q references unknown job %q", binding.TargetID, invocation.JobID)
			}
			if job.TargetID != binding.TargetID {
				return fmt.Errorf("binding %q cannot invoke job %q for target %q", binding.TargetID, invocation.JobID, job.TargetID)
			}
			if job.Suite != invocation.SuiteID || job.ModelID != invocation.ModelID {
				return fmt.Errorf("binding %q invocation for job %q disagrees with frozen suite/model", binding.TargetID, invocation.JobID)
			}
			if invocation.ModelID != "" {
				if _, selected := selectedModels[invocation.ModelID]; !selected {
					return fmt.Errorf("binding %q invokes unselected model %q", binding.TargetID, invocation.ModelID)
				}
			}
			if _, duplicate := bound[invocation.JobID]; duplicate {
				return fmt.Errorf("job %q has duplicate execution binding", invocation.JobID)
			}
			bound[invocation.JobID] = struct{}{}
		}
		bindings[binding.TargetID] = binding
	}
	for targetID := range targets {
		if _, exists := bindings[targetID]; !exists {
			return fmt.Errorf("target %q has no frozen binding", targetID)
		}
	}
	return nil
}

func validateFrozenID(kind, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", kind)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not have leading or trailing whitespace", kind)
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
