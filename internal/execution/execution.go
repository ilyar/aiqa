// Package execution owns the typed, privacy-safe live state of one AIQA run.
//
// It is intentionally independent of the terminal, logger and transport.  A
// caller submits facts as Events exactly once; the reducer turns them into
// immutable Snapshots for all consumers.  Events do not have fields for URLs,
// headers, request or response bodies, prompts, credentials, or provider
// errors, so subscribing to this package cannot accidentally expose them.
package execution

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const SchemaVersion = "aiqa.execution.v1"

var (
	ErrInvalidEvent       = errors.New("execution: invalid event")
	ErrUnknownEntity      = errors.New("execution: unknown entity")
	ErrTerminalConflict   = errors.New("execution: conflicting terminal outcome")
	ErrSequenceRegression = errors.New("execution: sequence regression")
)

// Clock deliberately separates correlation time from elapsed time.  Mono is
// run-relative and must be driven by a fake clock in tests; Snapshot never
// computes elapsed values from Wall.
type Clock interface {
	Wall() time.Time
	Mono() time.Duration
}

type realClock struct{ started time.Time }

func (c realClock) Wall() time.Time     { return time.Now().UTC() }
func (c realClock) Mono() time.Duration { return time.Since(c.started) }

// RealClock returns the production clock.  Its time.Since path preserves Go's
// monotonic measurement even if the system wall clock changes.
func RealClock() Clock { return realClock{started: time.Now()} }

type Phase string

const (
	PhaseValidating  Phase = "validating"
	PhasePreparing   Phase = "preparing"
	PhasePlanFrozen  Phase = "plan_frozen"
	PhaseRunning     Phase = "running"
	PhaseFinalizing  Phase = "finalizing"
	PhaseCompleted   Phase = "completed"
	PhaseInterrupted Phase = "interrupted"
	PhaseFailed      Phase = "failed_runtime"
)

type Outcome string

const (
	OutcomeSucceeded   Outcome = "succeeded"
	OutcomeFailed      Outcome = "failed"
	OutcomeSkipped     Outcome = "skipped"
	OutcomeBlocked     Outcome = "blocked"
	OutcomeRunnerError Outcome = "runner_error"
	OutcomeCancelled   Outcome = "cancelled"
)

type WaitReason string

const (
	WaitInitialPacing  WaitReason = "initial_pacing"
	WaitAdaptivePacing WaitReason = "adaptive_pacing"
	WaitRetryAfter     WaitReason = "retry_after"
	WaitRetryBackoff   WaitReason = "retry_backoff"
	WaitQuotaCapacity  WaitReason = "quota_capacity"
	WaitGlobalCapacity WaitReason = "global_capacity"
	WaitMemoryBudget   WaitReason = "memory_budget"
	WaitDependency     WaitReason = "dependency"
)

// WaitSource is deliberately closed so a raw HTTP header or provider message
// cannot become part of the observable event stream.
type WaitSource string

const (
	SourceInitialPacing  WaitSource = "initial_pacing"
	SourceAdaptivePacing WaitSource = "adaptive_pacing"
	SourceRetryAfterSecs WaitSource = "retry_after_seconds"
	SourceRetryAfterDate WaitSource = "retry_after_date"
	SourceRetryBackoff   WaitSource = "retry_backoff"
)

// Meta is embedded by every event.  Offset is the only time source used for
// wait elapsed/countdown values.  IDs are opaque safe identifiers, not labels.
type Meta struct {
	RunID    string
	Seq      uint64
	At       time.Time
	Offset   time.Duration
	TargetID string
	JobID    string
	// PreparationID identifies a registered pre-freeze owner. It is mutually
	// exclusive with JobID for request and wait events: preparation work is
	// observed, but is never fabricated as a frozen progress job.
	PreparationID string
	AttemptID     string
	GroupID       string
}

// Event is closed to this package: all state mutations are represented by the
// typed event families below, rather than free-form maps or log strings.
type Event interface {
	meta() Meta
	event()
}

// Base provides the common metadata for each typed event.
type Base struct{ Meta }

func (b Base) meta() Meta { return b.Meta }

type RunPhaseChanged struct {
	Base
	Phase Phase
}

func (RunPhaseChanged) event() {}

type TargetPhaseChanged struct {
	Base
	Phase Phase
}

func (TargetPhaseChanged) event() {}

// PreparationKind is intentionally closed. It records only the local role of
// a pre-freeze owner, never a provider endpoint, model name, or payload.
type PreparationKind string

const (
	PreparationDiscovery PreparationKind = "discovery"
	PreparationPreflight PreparationKind = "preflight"
)

// PreparationOwner is a registered owner for work which happens before the
// execution plan is frozen. A discovery request therefore has an explicit
// target-local identity without polluting frozen-job accounting.
type PreparationOwner struct {
	ID       string
	TargetID string
	Kind     PreparationKind
}

type PreparationRegistered struct {
	Base
	Owner PreparationOwner
}

func (PreparationRegistered) event() {}

// JobSpec is a display-independent frozen job descriptor. Requirement IDs are
// evidence links only and therefore never affect progress accounting.
type JobSpec struct {
	ID             string
	TargetID       string
	RequirementIDs []string
	Dependencies   []string
	EstimateClass  string
	Composite      bool
}
type PlanFrozen struct {
	Base
	Revision uint64
	Jobs     []JobSpec
}

func (PlanFrozen) event() {}

type JobQueued struct{ Base }

func (JobQueued) event() {}

type JobStarted struct{ Base }

func (JobStarted) event() {}

type JobProgress struct {
	Base
	CompletedSteps, TotalSteps int
}

func (JobProgress) event() {}

type JobFinished struct {
	Base
	Outcome Outcome
}

func (JobFinished) event() {}

type JobCancelled struct {
	Base
	Outcome Outcome
}

func (JobCancelled) event() {}

type RequestQueued struct{ Base }

func (RequestQueued) event() {}

type RequestAdmitted struct{ Base }

func (RequestAdmitted) event() {}

type RequestHeaders struct{ Base }

func (RequestHeaders) event() {}

type RequestCompleted struct {
	Base
	Outcome Outcome
}

func (RequestCompleted) event() {}

type RequestCancelled struct {
	Base
	Outcome Outcome
}

func (RequestCancelled) event() {}

// WaitStarted/Updated use a run-relative deadline. Source distinguishes an
// initial pacer decision from Retry-After without carrying response details.
type WaitStarted struct {
	Base
	WaitID           string
	Reason           WaitReason
	Source           WaitSource
	EligibleAt       time.Duration
	ScheduleRevision uint64
}

func (WaitStarted) event() {}

type WaitUpdated struct {
	Base
	WaitID           string
	Reason           WaitReason
	Source           WaitSource
	EligibleAt       time.Duration
	ScheduleRevision uint64
}

func (WaitUpdated) event() {}

type WaitEnded struct {
	Base
	WaitID string
}

func (WaitEnded) event() {}

type GroupLimitsChanged struct {
	Base
	Capacity int
	Interval time.Duration
}

func (GroupLimitsChanged) event() {}

type GroupCooldownChanged struct {
	Base
	Until  time.Duration
	Reason WaitReason
}

func (GroupCooldownChanged) event() {}

type ArtifactReserved struct {
	Base
	ArtifactID string
	Bytes      int64
}

func (ArtifactReserved) event() {}

type ArtifactCommitted struct {
	Base
	ArtifactID string
}

func (ArtifactCommitted) event() {}

type ArtifactReleased struct {
	Base
	ArtifactID string
}

func (ArtifactReleased) event() {}

type ReportWriteStarted struct{ Base }

func (ReportWriteStarted) event() {}

type ReportCommitted struct{ Base }

func (ReportCommitted) event() {}

type TraceVerifyStarted struct{ Base }

func (TraceVerifyStarted) event() {}

type TraceVerifyFinished struct {
	Base
	Outcome Outcome
}

func (TraceVerifyFinished) event() {}

type JobState string

const (
	JobPlanned     JobState = "planned"
	JobQueuedState JobState = "queued"
	JobRunning     JobState = "running"
	JobTerminal    JobState = "terminal"
)

type RequestState string

const (
	RequestQueuedState   RequestState = "queued"
	RequestAdmittedState RequestState = "admitted"
	RequestHeadersState  RequestState = "headers"
	RequestTerminal      RequestState = "terminal"
)

type RunSnapshot struct {
	ID                                             string
	Phase                                          Phase
	PlanRevision                                   uint64
	LastSequence                                   uint64
	Offset                                         time.Duration
	ReportWriting, ReportCommitted, TraceVerifying bool
	TraceOutcome                                   Outcome
}
type TargetSnapshot struct {
	ID    string
	Phase Phase
}
type PreparationSnapshot struct {
	ID, TargetID string
	Kind         PreparationKind
}
type JobSnapshot struct {
	ID, TargetID                 string
	RequirementIDs, Dependencies []string
	EstimateClass                string
	Composite                    bool
	State                        JobState
	Outcome                      Outcome
	CompletedSteps, TotalSteps   int
}
type RequestSnapshot struct {
	ID, TargetID, JobID, PreparationID, GroupID string
	State                                       RequestState
	Outcome                                     Outcome
}
type WaitSnapshot struct {
	ID, TargetID, JobID, PreparationID, GroupID string
	Reason                                      WaitReason
	Source                                      WaitSource
	ScheduleRevision                            uint64
	StartedAt, EligibleAt, Waited, ResumesIn    time.Duration
}
type GroupSnapshot struct {
	ID             string
	Capacity       int
	Interval       time.Duration
	CooldownUntil  time.Duration
	CooldownReason WaitReason
}
type ArtifactSnapshot struct {
	ID                  string
	Bytes               int64
	Reserved, Committed bool
}

// Snapshot is a deep immutable copy, ordered by opaque ID for deterministic
// renderers and tests.  PlannedJobs excludes requirements by construction.
type Snapshot struct {
	SchemaVersion                                                     string
	Run                                                               RunSnapshot
	Targets                                                           []TargetSnapshot
	Preparations                                                      []PreparationSnapshot
	Jobs                                                              []JobSnapshot
	Requests                                                          []RequestSnapshot
	Waits                                                             []WaitSnapshot
	Groups                                                            []GroupSnapshot
	Artifacts                                                         []ArtifactSnapshot
	PlannedJobs, FinishedJobs, RunningJobs, QueuedJobs, CancelledJobs int
}

type Reducer struct {
	mu           sync.RWMutex
	clock        Clock
	run          RunSnapshot
	targets      map[string]Phase
	preparations map[string]*preparation
	jobs         map[string]*job
	requests     map[string]*request
	waits        map[string]*wait
	groups       map[string]*group
	artifacts    map[string]*artifact
	seenTerminal map[string]Outcome
}
type job struct {
	spec             JobSpec
	state            JobState
	outcome          Outcome
	completed, total int
}
type request struct {
	id, target, job, preparation, group string
	state                               RequestState
	outcome                             Outcome
}
type wait struct {
	id, target, job, preparation, group string
	reason                              WaitReason
	source                              WaitSource
	revision                            uint64
	started, eligible                   time.Duration
}
type preparation struct {
	id, target string
	kind       PreparationKind
}
type group struct {
	id                 string
	capacity           int
	interval, cooldown time.Duration
	reason             WaitReason
}
type artifact struct {
	id                  string
	bytes               int64
	reserved, committed bool
}

func New(runID string, clock Clock) (*Reducer, error) {
	if err := validateID("run_id", runID); err != nil {
		return nil, err
	}
	if clock == nil {
		clock = RealClock()
	}
	return &Reducer{clock: clock, run: RunSnapshot{ID: runID, Phase: PhaseValidating}, targets: map[string]Phase{}, preparations: map[string]*preparation{}, jobs: map[string]*job{}, requests: map[string]*request{}, waits: map[string]*wait{}, groups: map[string]*group{}, artifacts: map[string]*artifact{}, seenTerminal: map[string]Outcome{}}, nil
}

// Apply changes the state once. Terminal duplicates with an identical outcome
// are accepted without modifying counters; conflicting terminal delivery is an
// error and leaves the reducer unchanged.
func (r *Reducer) Apply(e Event) error {
	if e == nil {
		return fmt.Errorf("%w: nil", ErrInvalidEvent)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	m := e.meta()
	if err := r.validateMeta(m); err != nil {
		return err
	}
	// Equal sequence is allowed for at-least-once re-delivery.  Terminal
	// handlers below make that delivery idempotent and reject conflicts; older
	// sequences are never replayed silently.
	if m.Seq > 0 && m.Seq < r.run.LastSequence {
		return fmt.Errorf("%w: got %d after %d", ErrSequenceRegression, m.Seq, r.run.LastSequence)
	}
	if err := r.apply(e, m); err != nil {
		return err
	}
	if m.Seq > 0 {
		r.run.LastSequence = m.Seq
	}
	if m.Offset > r.run.Offset {
		r.run.Offset = m.Offset
	}
	return nil
}

func (r *Reducer) validateMeta(m Meta) error {
	if m.RunID != r.run.ID {
		return fmt.Errorf("%w: run id", ErrInvalidEvent)
	}
	if m.Offset < 0 {
		return fmt.Errorf("%w: negative monotonic offset", ErrInvalidEvent)
	}
	for n, v := range map[string]string{"target_id": m.TargetID, "job_id": m.JobID, "preparation_id": m.PreparationID, "attempt_id": m.AttemptID, "group_id": m.GroupID} {
		if v != "" {
			if err := validateID(n, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Reducer) apply(e Event, m Meta) error {
	switch v := e.(type) {
	case RunPhaseChanged:
		if !validPhase(v.Phase) {
			return bad("run phase")
		}
		r.run.Phase = v.Phase
	case TargetPhaseChanged:
		if m.TargetID == "" || !validPhase(v.Phase) {
			return bad("target phase")
		}
		r.targets[m.TargetID] = v.Phase
	case PreparationRegistered:
		return r.registerPreparation(v, m)
	case PlanFrozen:
		return r.freeze(v, m)
	case JobQueued:
		return r.jobState(m, JobQueuedState)
	case JobStarted:
		return r.jobState(m, JobRunning)
	case JobProgress:
		return r.progress(m, v)
	case JobFinished:
		return r.jobTerminal(m, v.Outcome)
	case JobCancelled:
		return r.jobTerminal(m, v.Outcome)
	case RequestQueued:
		return r.requestState(m, RequestQueuedState, "")
	case RequestAdmitted:
		return r.requestState(m, RequestAdmittedState, "")
	case RequestHeaders:
		return r.requestState(m, RequestHeadersState, "")
	case RequestCompleted:
		return r.requestState(m, RequestTerminal, v.Outcome)
	case RequestCancelled:
		return r.requestState(m, RequestTerminal, v.Outcome)
	case WaitStarted:
		return r.waitStart(v, m)
	case WaitUpdated:
		return r.waitUpdate(v, m)
	case WaitEnded:
		return r.waitEnd(v)
	case GroupLimitsChanged:
		return r.groupLimits(v, m)
	case GroupCooldownChanged:
		return r.groupCooldown(v, m)
	case ArtifactReserved:
		return r.artifactReserve(v)
	case ArtifactCommitted:
		return r.artifactCommit(v)
	case ArtifactReleased:
		return r.artifactRelease(v)
	case ReportWriteStarted:
		r.run.ReportWriting = true
	case ReportCommitted:
		r.run.ReportWriting = false
		r.run.ReportCommitted = true
	case TraceVerifyStarted:
		r.run.TraceVerifying = true
	case TraceVerifyFinished:
		if !validOutcome(v.Outcome) {
			return bad("trace outcome")
		}
		r.run.TraceVerifying = false
		r.run.TraceOutcome = v.Outcome
	default:
		return fmt.Errorf("%w: unsupported event", ErrInvalidEvent)
	}
	return nil
}

func (r *Reducer) freeze(v PlanFrozen, m Meta) error {
	if v.Revision == 0 || r.run.PlanRevision != 0 {
		return bad("plan revision")
	}
	seen := map[string]bool{}
	for _, s := range v.Jobs {
		if err := validateID("job_id", s.ID); err != nil {
			return err
		}
		if s.TargetID == "" {
			return bad("job target")
		}
		if seen[s.ID] {
			return fmt.Errorf("%w: duplicate job %q", ErrInvalidEvent, s.ID)
		}
		seen[s.ID] = true
		r.jobs[s.ID] = &job{spec: cloneJobSpec(s), state: JobPlanned}
		if _, ok := r.targets[s.TargetID]; !ok {
			r.targets[s.TargetID] = PhasePreparing
		}
	}
	r.run.PlanRevision = v.Revision
	r.run.Phase = PhasePlanFrozen
	return nil
}
func (r *Reducer) registerPreparation(v PreparationRegistered, m Meta) error {
	if r.run.PlanRevision != 0 {
		return bad("preparation registered after plan freeze")
	}
	owner := v.Owner
	if m.TargetID == "" || m.TargetID != owner.TargetID || m.JobID != "" || m.PreparationID != "" {
		return bad("preparation identity")
	}
	if err := validateID("preparation_id", owner.ID); err != nil {
		return err
	}
	if err := validateID("preparation target_id", owner.TargetID); err != nil {
		return err
	}
	if !validPreparationKind(owner.Kind) {
		return bad("preparation kind")
	}
	if _, exists := r.preparations[owner.ID]; exists {
		return fmt.Errorf("%w: duplicate preparation %q", ErrInvalidEvent, owner.ID)
	}
	r.preparations[owner.ID] = &preparation{id: owner.ID, target: owner.TargetID, kind: owner.Kind}
	if _, exists := r.targets[owner.TargetID]; !exists {
		r.targets[owner.TargetID] = PhasePreparing
	}
	return nil
}

// jobForMeta enforces that every runtime JobID is one of the frozen plan's
// opaque identifiers. Preparation owners use PreparationID instead and cannot
// bypass this check by supplying both fields.
func (r *Reducer) jobForMeta(m Meta) (*job, error) {
	if m.JobID == "" || m.PreparationID != "" {
		return nil, bad("job identity")
	}
	j := r.jobs[m.JobID]
	if j == nil {
		return nil, unknown("job", m.JobID)
	}
	if m.TargetID != "" && m.TargetID != j.spec.TargetID {
		return nil, bad("job target")
	}
	return j, nil
}

func (r *Reducer) ownerForRequest(m Meta) error {
	if m.TargetID == "" || m.GroupID == "" || (m.JobID == "" && m.PreparationID == "") || (m.JobID != "" && m.PreparationID != "") {
		return bad("request owner")
	}
	if m.JobID != "" {
		_, err := r.jobForMeta(m)
		return err
	}
	p := r.preparations[m.PreparationID]
	if p == nil {
		return unknown("preparation", m.PreparationID)
	}
	if p.target != m.TargetID {
		return bad("preparation target")
	}
	return nil
}

func (r *Reducer) jobState(m Meta, state JobState) error {
	j, err := r.jobForMeta(m)
	if err != nil {
		return err
	}
	if j.state == JobTerminal {
		return fmt.Errorf("%w: job %q already terminal", ErrInvalidEvent, m.JobID)
	}
	j.state = state
	return nil
}
func (r *Reducer) progress(m Meta, v JobProgress) error {
	j, err := r.jobForMeta(m)
	if err != nil {
		return err
	}
	if j.state == JobTerminal || v.CompletedSteps < 0 || v.TotalSteps < 0 || (v.TotalSteps > 0 && v.CompletedSteps > v.TotalSteps) {
		return bad("job progress")
	}
	j.completed, j.total = v.CompletedSteps, v.TotalSteps
	return nil
}
func (r *Reducer) jobTerminal(m Meta, o Outcome) error {
	if !validOutcome(o) {
		return bad("job outcome")
	}
	j, err := r.jobForMeta(m)
	if err != nil {
		return err
	}
	key := "job:" + m.JobID
	if prior, ok := r.seenTerminal[key]; ok {
		if prior == o {
			return nil
		}
		return fmt.Errorf("%w: job %q has %q and %q", ErrTerminalConflict, m.JobID, prior, o)
	}
	r.seenTerminal[key] = o
	j.state = JobTerminal
	j.outcome = o
	return nil
}
func (r *Reducer) requestState(m Meta, state RequestState, o Outcome) error {
	if m.AttemptID == "" {
		return bad("request identity")
	}
	x := r.requests[m.AttemptID]
	if x == nil {
		if state != RequestQueuedState {
			return unknown("request", m.AttemptID)
		}
		if err := r.ownerForRequest(m); err != nil {
			return err
		}
		x = &request{id: m.AttemptID, target: m.TargetID, job: m.JobID, preparation: m.PreparationID, group: m.GroupID}
		r.requests[x.id] = x
	} else if x.target != m.TargetID || x.job != m.JobID || x.preparation != m.PreparationID || x.group != m.GroupID {
		return bad("request owner changed")
	}
	if state == RequestTerminal {
		if !validOutcome(o) {
			return bad("request outcome")
		}
		key := "request:" + x.id
		if prior, ok := r.seenTerminal[key]; ok {
			if prior == o {
				return nil
			}
			return fmt.Errorf("%w: request %q", ErrTerminalConflict, x.id)
		}
		r.seenTerminal[key] = o
		x.outcome = o
	}
	x.state = state
	return nil
}
func (r *Reducer) waitStart(v WaitStarted, m Meta) error {
	if err := r.ownerForRequest(m); err != nil {
		return fmt.Errorf("%w: wait: %v", ErrInvalidEvent, err)
	}
	if err := validateWait(v.WaitID, v.Reason, v.Source, v.EligibleAt, m.Offset); err != nil {
		return err
	}
	if _, ok := r.waits[v.WaitID]; ok {
		return bad("duplicate wait")
	}
	r.waits[v.WaitID] = &wait{id: v.WaitID, target: m.TargetID, job: m.JobID, preparation: m.PreparationID, group: m.GroupID, reason: v.Reason, source: v.Source, revision: v.ScheduleRevision, started: m.Offset, eligible: v.EligibleAt}
	return nil
}
func (r *Reducer) waitUpdate(v WaitUpdated, m Meta) error {
	if err := validateWait(v.WaitID, v.Reason, v.Source, v.EligibleAt, m.Offset); err != nil {
		return err
	}
	w := r.waits[v.WaitID]
	if w == nil {
		return unknown("wait", v.WaitID)
	}
	if (m.TargetID != "" && m.TargetID != w.target) || (m.JobID != "" && m.JobID != w.job) || (m.PreparationID != "" && m.PreparationID != w.preparation) || (m.GroupID != "" && m.GroupID != w.group) {
		return bad("wait owner changed")
	}
	w.reason, w.source, w.revision, w.eligible = v.Reason, v.Source, v.ScheduleRevision, v.EligibleAt
	return nil
}
func (r *Reducer) waitEnd(v WaitEnded) error {
	if err := validateID("wait_id", v.WaitID); err != nil {
		return err
	}
	if _, ok := r.waits[v.WaitID]; !ok {
		return unknown("wait", v.WaitID)
	}
	delete(r.waits, v.WaitID)
	return nil
}
func (r *Reducer) groupLimits(v GroupLimitsChanged, m Meta) error {
	if m.GroupID == "" || v.Capacity < 0 || v.Interval < 0 {
		return bad("group limits")
	}
	g := r.ensureGroup(m.GroupID)
	g.capacity, g.interval = v.Capacity, v.Interval
	return nil
}
func (r *Reducer) groupCooldown(v GroupCooldownChanged, m Meta) error {
	if m.GroupID == "" || v.Until < m.Offset || !validWaitReason(v.Reason) {
		return bad("group cooldown")
	}
	g := r.ensureGroup(m.GroupID)
	g.cooldown, g.reason = v.Until, v.Reason
	return nil
}
func (r *Reducer) ensureGroup(id string) *group {
	g := r.groups[id]
	if g == nil {
		g = &group{id: id}
		r.groups[id] = g
	}
	return g
}
func (r *Reducer) artifactReserve(v ArtifactReserved) error {
	if err := validateID("artifact_id", v.ArtifactID); err != nil {
		return err
	}
	if v.Bytes < 0 {
		return bad("artifact bytes")
	}
	if _, ok := r.artifacts[v.ArtifactID]; ok {
		return bad("duplicate artifact")
	}
	r.artifacts[v.ArtifactID] = &artifact{id: v.ArtifactID, bytes: v.Bytes, reserved: true}
	return nil
}
func (r *Reducer) artifactCommit(v ArtifactCommitted) error {
	a := r.artifacts[v.ArtifactID]
	if a == nil {
		return unknown("artifact", v.ArtifactID)
	}
	a.committed = true
	return nil
}
func (r *Reducer) artifactRelease(v ArtifactReleased) error {
	if _, ok := r.artifacts[v.ArtifactID]; !ok {
		return unknown("artifact", v.ArtifactID)
	}
	delete(r.artifacts, v.ArtifactID)
	return nil
}

func (r *Reducer) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshot(r.clock.Mono())
}
func (r *Reducer) SnapshotAt(now time.Duration) Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshot(now)
}
func (r *Reducer) snapshot(now time.Duration) Snapshot {
	s := Snapshot{SchemaVersion: SchemaVersion, Run: r.run}
	for id, p := range r.targets {
		s.Targets = append(s.Targets, TargetSnapshot{ID: id, Phase: p})
	}
	for _, p := range r.preparations {
		s.Preparations = append(s.Preparations, PreparationSnapshot{ID: p.id, TargetID: p.target, Kind: p.kind})
	}
	for _, j := range r.jobs {
		x := JobSnapshot{ID: j.spec.ID, TargetID: j.spec.TargetID, RequirementIDs: append([]string(nil), j.spec.RequirementIDs...), Dependencies: append([]string(nil), j.spec.Dependencies...), EstimateClass: j.spec.EstimateClass, Composite: j.spec.Composite, State: j.state, Outcome: j.outcome, CompletedSteps: j.completed, TotalSteps: j.total}
		s.Jobs = append(s.Jobs, x)
		s.PlannedJobs++
		switch j.state {
		case JobTerminal:
			s.FinishedJobs++
			if j.outcome == OutcomeCancelled {
				s.CancelledJobs++
			}
		case JobRunning:
			s.RunningJobs++
		case JobQueuedState:
			s.QueuedJobs++
		}
	}
	for _, x := range r.requests {
		s.Requests = append(s.Requests, RequestSnapshot{ID: x.id, TargetID: x.target, JobID: x.job, PreparationID: x.preparation, GroupID: x.group, State: x.state, Outcome: x.outcome})
	}
	for _, w := range r.waits {
		waited := now - w.started
		if waited < 0 {
			waited = 0
		}
		resume := w.eligible - now
		if resume < 0 {
			resume = 0
		}
		s.Waits = append(s.Waits, WaitSnapshot{ID: w.id, TargetID: w.target, JobID: w.job, PreparationID: w.preparation, GroupID: w.group, Reason: w.reason, Source: w.source, ScheduleRevision: w.revision, StartedAt: w.started, EligibleAt: w.eligible, Waited: waited, ResumesIn: resume})
	}
	for _, g := range r.groups {
		s.Groups = append(s.Groups, GroupSnapshot{ID: g.id, Capacity: g.capacity, Interval: g.interval, CooldownUntil: g.cooldown, CooldownReason: g.reason})
	}
	for _, a := range r.artifacts {
		s.Artifacts = append(s.Artifacts, ArtifactSnapshot{ID: a.id, Bytes: a.bytes, Reserved: a.reserved, Committed: a.committed})
	}
	sort.Slice(s.Targets, func(i, j int) bool { return s.Targets[i].ID < s.Targets[j].ID })
	sort.Slice(s.Preparations, func(i, j int) bool { return s.Preparations[i].ID < s.Preparations[j].ID })
	sort.Slice(s.Jobs, func(i, j int) bool { return s.Jobs[i].ID < s.Jobs[j].ID })
	sort.Slice(s.Requests, func(i, j int) bool { return s.Requests[i].ID < s.Requests[j].ID })
	sort.Slice(s.Waits, func(i, j int) bool { return s.Waits[i].ID < s.Waits[j].ID })
	sort.Slice(s.Groups, func(i, j int) bool { return s.Groups[i].ID < s.Groups[j].ID })
	sort.Slice(s.Artifacts, func(i, j int) bool { return s.Artifacts[i].ID < s.Artifacts[j].ID })
	return s
}

func validateID(name, v string) error {
	if strings.TrimSpace(v) == "" || len(v) > 16384 {
		return fmt.Errorf("%w: %s", ErrInvalidEvent, name)
	}
	for _, r := range v {
		if unicode.IsControl(r) || r == '\u202e' || r == '\u202d' || r == '\u2066' || r == '\u2067' || r == '\u2068' || r == '\u2069' {
			return fmt.Errorf("%w: unsafe %s", ErrInvalidEvent, name)
		}
	}
	return nil
}
func validateWait(id string, reason WaitReason, source WaitSource, eligible, offset time.Duration) error {
	if err := validateID("wait_id", id); err != nil {
		return err
	}
	if !validWaitReason(reason) || !validWaitSource(source) || eligible < offset {
		return bad("wait")
	}
	return nil
}
func validPhase(p Phase) bool {
	switch p {
	case PhaseValidating, PhasePreparing, PhasePlanFrozen, PhaseRunning, PhaseFinalizing, PhaseCompleted, PhaseInterrupted, PhaseFailed:
		return true
	}
	return false
}
func validOutcome(o Outcome) bool {
	switch o {
	case OutcomeSucceeded, OutcomeFailed, OutcomeSkipped, OutcomeBlocked, OutcomeRunnerError, OutcomeCancelled:
		return true
	}
	return false
}
func validPreparationKind(k PreparationKind) bool {
	switch k {
	case PreparationDiscovery, PreparationPreflight:
		return true
	}
	return false
}
func validWaitReason(w WaitReason) bool {
	switch w {
	case WaitInitialPacing, WaitAdaptivePacing, WaitRetryAfter, WaitRetryBackoff, WaitQuotaCapacity, WaitGlobalCapacity, WaitMemoryBudget, WaitDependency:
		return true
	}
	return false
}

func validWaitSource(s WaitSource) bool {
	switch s {
	case SourceInitialPacing, SourceAdaptivePacing, SourceRetryAfterSecs, SourceRetryAfterDate, SourceRetryBackoff:
		return true
	}
	return false
}
func cloneJobSpec(j JobSpec) JobSpec {
	j.RequirementIDs = append([]string(nil), j.RequirementIDs...)
	j.Dependencies = append([]string(nil), j.Dependencies...)
	return j
}
func bad(what string) error         { return fmt.Errorf("%w: %s", ErrInvalidEvent, what) }
func unknown(kind, id string) error { return fmt.Errorf("%w: %s %q", ErrUnknownEntity, kind, id) }
