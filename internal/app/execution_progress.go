package app

import (
	"fmt"
	"sync"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/execution"
	"github.com/ilyar/aiqa/internal/progress"
	"github.com/ilyar/aiqa/internal/protocol"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/scheduler"
	"github.com/ilyar/aiqa/internal/telemetry"
)

// executionProgress mirrors the already-authoritative frozen progress plan
// into aiqa.execution.v1. It deliberately does not infer attempts, waits, or
// terminal outcomes from logs: those producers remain transport-owned.
type executionProgress struct {
	sink    progress.Sink
	reducer *execution.Reducer
	builder *execution.SummaryBuilder
	runID   string
	started time.Time

	mu          sync.Mutex
	sequence    uint64
	lastOffset  time.Duration
	err         error
	frozen      bool
	known       map[string]struct{}
	jobTargets  map[string]string
	outcomes    []protocol.TerminalOutcome
	seenFacts   map[uint64]struct{}
	unavailable bool
}

type executionSnapshotObserver interface {
	ObserveExecutionSnapshot(execution.Snapshot)
}

func newExecutionProgress(runID string, capacity int, sink progress.Sink) (*executionProgress, error) {
	if sink == nil {
		return nil, fmt.Errorf("nil progress sink")
	}
	if _, ok := sink.(progress.FrozenSink); !ok {
		return nil, fmt.Errorf("progress sink does not support frozen plans")
	}
	reducer, err := execution.New(runID, nil)
	if err != nil {
		return nil, err
	}
	builder, err := execution.NewSummaryBuilder(execution.SummaryOptions{GlobalRequestCapacity: capacity})
	if err != nil {
		return nil, err
	}
	return &executionProgress{sink: sink, reducer: reducer, builder: builder, runID: runID, started: time.Now(), known: make(map[string]struct{}), jobTargets: make(map[string]string), seenFacts: make(map[uint64]struct{})}, nil
}

func (p *executionProgress) RunStarted(targets int) {
	p.sink.RunStarted(targets)
	p.apply(func(base execution.Base) execution.Event {
		return execution.RunPhaseChanged{Base: base, Phase: execution.PhasePreparing}
	})
}
func (p *executionProgress) DiscoveryStarted(target string) { p.sink.DiscoveryStarted(target) }
func (p *executionProgress) Plan(target string, total int) {
	if p.isFrozen() {
		return
	}
	p.sink.Plan(target, total)
}
func (p *executionProgress) RevisePlan(target string, total int) {
	if p.isFrozen() {
		return
	}
	p.sink.RevisePlan(target, total)
}
func (p *executionProgress) AddPlan(target string, delta int) {
	if p.isFrozen() {
		return
	}
	p.sink.AddPlan(target, delta)
}
func (p *executionProgress) Pacing(target string, waiting bool, duration time.Duration) {
	p.sink.Pacing(target, waiting, duration)
}
func (p *executionProgress) TargetFinished(target, outcome string) {
	p.sink.TargetFinished(target, outcome)
}
func (p *executionProgress) RunFinished()           { p.sink.RunFinished() }
func (p *executionProgress) Message(text string)    { p.sink.Message(text) }
func (p *executionProgress) Close()                 { p.sink.Close() }
func (p *executionProgress) Planning(status string) { p.frozenSink().Planning(status) }

func (p *executionProgress) Freeze(jobs []progress.Job) {
	p.frozenSink().Freeze(jobs)
	specs := make([]execution.JobSpec, 0, len(jobs))
	for _, job := range jobs {
		specs = append(specs, execution.JobSpec{ID: job.ID, TargetID: job.Target})
		p.known[job.ID] = struct{}{}
		p.jobTargets[job.ID] = job.Target
	}
	p.apply(func(base execution.Base) execution.Event {
		return execution.PlanFrozen{Base: base, Revision: 1, Jobs: specs}
	})
	p.mu.Lock()
	p.frozen = p.err == nil
	p.mu.Unlock()
}

func (p *executionProgress) Start(id string) {
	if !p.isKnown(id) {
		return
	}
	p.frozenSink().Start(id)
	if target := p.targetForJob(id); target != "" {
		p.apply(func(base execution.Base) execution.Event {
			base.TargetID = target
			return execution.TargetPhaseChanged{Base: base, Phase: execution.PhaseRunning}
		})
	}
	p.apply(func(base execution.Base) execution.Event { return execution.JobStarted{Base: withJob(base, id)} })
	p.apply(func(base execution.Base) execution.Event {
		return execution.RunPhaseChanged{Base: base, Phase: execution.PhaseRunning}
	})
}

func (p *executionProgress) Complete(id, outcome string) {
	if !p.isKnown(id) {
		return
	}
	p.frozenSink().Complete(id, outcome)
	p.mu.Lock()
	p.outcomes = append(p.outcomes, protocol.TerminalOutcome{JobID: id, Outcome: outcome})
	p.mu.Unlock()
	p.apply(func(base execution.Base) execution.Event {
		return execution.JobFinished{Base: withJob(base, id), Outcome: executionOutcome(outcome)}
	})
	if target := p.targetForJob(id); target != "" && p.targetComplete(target) {
		p.apply(func(base execution.Base) execution.Event {
			base.TargetID = target
			return execution.TargetPhaseChanged{Base: base, Phase: execution.PhaseCompleted}
		})
	}
}

func (p *executionProgress) TerminalOutcomes() []protocol.TerminalOutcome {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]protocol.TerminalOutcome(nil), p.outcomes...)
}

// Lifecycle operation IDs are trace identities, not frozen job IDs.  A
// telemetry span may cover discovery, a retry or an implementation detail and
// therefore must never be forwarded to the legacy progress sink, which treats
// OperationID as a job ID. Frozen runners publish Start/Complete themselves.
// Keeping these namespaces separate prevents a verbose trace from producing
// spurious unknown-job diagnostics or changing the fixed denominator.
func (p *executionProgress) ObserveLifecycle(event telemetry.LifecycleEvent) {
	_ = event
}

// RegisterPreparation binds a pre-freeze API owner to the execution reducer.
// Discovery therefore remains observable without being invented as a frozen
// progress job. Callers must register it before Freeze.
func (p *executionProgress) RegisterPreparation(owner api.ExecutionOwner, kind execution.PreparationKind) {
	if owner.TargetID == "" || owner.PreparationID == "" || owner.JobID != "" {
		p.markUnavailable()
		return
	}
	p.apply(func(base execution.Base) execution.Event {
		base.TargetID = owner.TargetID
		return execution.PreparationRegistered{Base: base, Owner: execution.PreparationOwner{ID: owner.PreparationID, TargetID: owner.TargetID, Kind: kind}}
	})
}

// ObserveSchedulerFact maps broker state into execution facts. The owner is
// explicit because scheduler facts deliberately contain no job/preparation ID;
// guessing one would turn scheduler accounting into false report evidence.
// API AttemptIDs and scheduler RequestIDs are intentionally different, so API
// is the sole HTTP-request producer until a future explicit correlation key is
// added. Scheduler still contributes its authoritative waits and group facts.
// In particular, opaque/artifact/memory reservations never inflate HTTP
// request counts.
func (p *executionProgress) ObserveSchedulerFact(fact scheduler.Fact, owner api.ExecutionOwner) {
	if fact.Sequence != 0 {
		p.mu.Lock()
		if _, seen := p.seenFacts[fact.Sequence]; seen {
			p.mu.Unlock()
			return
		}
		p.seenFacts[fact.Sequence] = struct{}{}
		p.mu.Unlock()
	}
	offset := p.schedulerOffset(fact.At)
	eligible := p.schedulerOffset(fact.EligibleAt)
	if eligible < offset {
		// A broker may report an already-eligible waiter after a capacity
		// change. The reducer timeline is monotonic, so preserve the wait at
		// its observation boundary instead of inventing negative time.
		eligible = offset
	}
	switch fact.Kind {
	case scheduler.FactGroupUpdated:
		p.applyAt(offset, func(base execution.Base) execution.Event {
			base.GroupID = fact.Group
			return execution.GroupLimitsChanged{Base: base, Capacity: fact.Capacity, Interval: fact.Interval}
		})
		if !fact.Cooldown.IsZero() {
			p.applyAt(offset, func(base execution.Base) execution.Event {
				base.GroupID = fact.Group
				return execution.GroupCooldownChanged{Base: base, Until: p.schedulerOffset(fact.Cooldown), Reason: executionWaitReason(fact.Reason)}
			})
		}
		return
	case scheduler.FactWaitStarted, scheduler.FactWaitUpdated, scheduler.FactWaitEnded:
		if !p.validOwner(owner) || fact.WaitID == "" || fact.Group == "" {
			p.markUnavailable()
			return
		}
		p.applyAt(offset, func(base execution.Base) execution.Event {
			base = executionBaseForOwner(base, owner, fact.Group)
			base.AttemptID = fact.RequestID
			// applyAt serializes concurrent producers and can advance the
			// reducer offset after this scheduler fact was read. A wait cannot
			// become eligible before its own accepted event, so clamp against
			// the authoritative base offset at construction time as well.
			eligibleAt := eligible
			if eligibleAt < base.Meta.Offset {
				eligibleAt = base.Meta.Offset
			}
			switch fact.Kind {
			case scheduler.FactWaitStarted:
				return execution.WaitStarted{Base: base, WaitID: fact.WaitID, Reason: executionWaitReason(fact.Reason), Source: executionWaitSource(fact.Reason), EligibleAt: eligibleAt, ScheduleRevision: fact.Revision}
			case scheduler.FactWaitUpdated:
				return execution.WaitUpdated{Base: base, WaitID: fact.WaitID, Reason: executionWaitReason(fact.Reason), Source: executionWaitSource(fact.Reason), EligibleAt: eligibleAt, ScheduleRevision: fact.Revision}
			default:
				return execution.WaitEnded{Base: base, WaitID: fact.WaitID}
			}
		})
		return
	}
}

// ObserveAPIExecution maps transport-owned lifecycle facts. An admitted event
// bootstraps its reducer request with queued first because API observes the
// physical admission boundary, not the broker queue insertion.
func (p *executionProgress) ObserveAPIExecution(event api.ExecutionEvent) {
	owner := api.ExecutionOwner{TargetID: event.TargetID, JobID: event.JobID, PreparationID: event.PreparationID}
	// Runtime JobID assignment is deliberately deferred until the frozen plan
	// is the direct execution input. Do not manufacture a job identity from a
	// target or trace context, and do not poison the otherwise measured
	// preparation snapshot merely because an older runner has not migrated.
	if owner.JobID == "" && owner.PreparationID == "" {
		return
	}
	if !p.validOwner(owner) || event.AttemptID == "" || event.GroupID == "" {
		p.markUnavailable()
		return
	}
	base := func(base execution.Base) execution.Base {
		base = executionBaseForOwner(base, owner, event.GroupID)
		base.AttemptID = event.AttemptID
		return base
	}
	switch event.Kind {
	case api.ExecutionRequestAdmitted:
		p.apply(func(current execution.Base) execution.Event { return execution.RequestQueued{Base: base(current)} })
		p.apply(func(current execution.Base) execution.Event { return execution.RequestAdmitted{Base: base(current)} })
	case api.ExecutionRequestHeaders:
		p.apply(func(current execution.Base) execution.Event { return execution.RequestHeaders{Base: base(current)} })
	case api.ExecutionRequestTerminal:
		outcome := executionOutcome(event.Outcome)
		p.apply(func(current execution.Base) execution.Event {
			return execution.RequestCompleted{Base: base(current), Outcome: outcome}
		})
	}
}

func (p *executionProgress) schedulerOffset(at time.Time) time.Duration {
	if at.IsZero() || at.Before(p.started) {
		return 0
	}
	return at.Sub(p.started)
}

func (p *executionProgress) validOwner(owner api.ExecutionOwner) bool {
	return owner.TargetID != "" && ((owner.JobID != "") != (owner.PreparationID != ""))
}

func (p *executionProgress) markUnavailable() {
	p.mu.Lock()
	p.unavailable = true
	p.mu.Unlock()
}

func executionBaseForOwner(base execution.Base, owner api.ExecutionOwner, group string) execution.Base {
	base.TargetID, base.JobID, base.PreparationID, base.GroupID = owner.TargetID, owner.JobID, owner.PreparationID, group
	return base
}

func executionWaitReason(reason scheduler.WaitReason) execution.WaitReason {
	return execution.WaitReason(reason)
}

func executionWaitSource(reason scheduler.WaitReason) execution.WaitSource {
	switch reason {
	case scheduler.WaitRetryAfter:
		return execution.SourceRetryAfterSecs
	case scheduler.WaitRetryBackoff:
		return execution.SourceRetryBackoff
	case scheduler.WaitAdaptivePacing:
		return execution.SourceAdaptivePacing
	default:
		return execution.SourceInitialPacing
	}
}

func (p *executionProgress) Finalize() error {
	p.apply(func(base execution.Base) execution.Event {
		return execution.RunPhaseChanged{Base: base, Phase: execution.PhaseFinalizing}
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.frozen || p.err != nil || p.unavailable {
		return nil
	}
	return nil
}

func (p *executionProgress) Summary() (*result.ExecutionSummary, error) {
	p.mu.Lock()
	err, frozen, unavailable := p.err, p.frozen, p.unavailable
	p.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("reduce execution events: %w", err)
	}
	if !frozen || unavailable {
		// The block is optional. A runner event for a job not present in the
		// frozen plan is an accounting mismatch, not permission to fabricate a
		// terminal result or to fail an otherwise valid provider report.
		return nil, nil
	}
	return p.builder.Build(p.reducer.Snapshot())
}

func (p *executionProgress) isKnown(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.known[id]; ok {
		return true
	}
	p.unavailable = true
	return false
}

func (p *executionProgress) isFrozen() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.frozen
}

func (p *executionProgress) targetForJob(id string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.jobTargets[id]
}

func (p *executionProgress) targetComplete(target string) bool {
	snapshot := p.reducer.Snapshot()
	seen := false
	for _, job := range snapshot.Jobs {
		if job.TargetID != target {
			continue
		}
		seen = true
		if job.State != execution.JobTerminal {
			return false
		}
	}
	return seen
}

func (p *executionProgress) frozenSink() progress.FrozenSink {
	return p.sink.(progress.FrozenSink)
}

func (p *executionProgress) apply(event func(execution.Base) execution.Event) {
	p.applyAt(time.Since(p.started), event)
}

func (p *executionProgress) applyAt(offset time.Duration, event func(execution.Base) execution.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return
	}
	if offset < p.lastOffset {
		offset = p.lastOffset
	}
	p.sequence++
	base := execution.Base{Meta: execution.Meta{RunID: p.runID, Seq: p.sequence, Offset: offset}}
	if err := p.reducer.Apply(event(base)); err != nil {
		p.err = err
		return
	}
	p.lastOffset = offset
	// The public aggregate is derived from the same live reducer snapshots as
	// the UI.  Recording only the final snapshot loses intervals in which a
	// waiter existed, so it would under-report real scheduler delay.
	if err := p.builder.Observe(p.reducer.Snapshot()); err != nil {
		p.err = err
	}
	if observer, ok := p.sink.(executionSnapshotObserver); ok && p.err == nil {
		observer.ObserveExecutionSnapshot(p.reducer.Snapshot())
	}
}

func withJob(base execution.Base, id string) execution.Base {
	base.JobID = id
	return base
}

func executionOutcome(outcome string) execution.Outcome {
	switch outcome {
	case "completed", "success", "succeeded", "pass", "passed":
		return execution.OutcomeSucceeded
	case "skipped", "skip":
		return execution.OutcomeSkipped
	case "blocked":
		return execution.OutcomeBlocked
	case "cancelled", "canceled":
		return execution.OutcomeCancelled
	case "runner_error":
		return execution.OutcomeRunnerError
	default:
		return execution.OutcomeFailed
	}
}
