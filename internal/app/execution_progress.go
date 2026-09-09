package app

import (
	"fmt"
	"sync"
	"time"

	"github.com/ilyar/aiqa/internal/execution"
	"github.com/ilyar/aiqa/internal/progress"
	"github.com/ilyar/aiqa/internal/result"
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
	err         error
	frozen      bool
	known       map[string]struct{}
	unavailable bool
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
	return &executionProgress{sink: sink, reducer: reducer, builder: builder, runID: runID, started: time.Now(), known: make(map[string]struct{})}, nil
}

func (p *executionProgress) RunStarted(targets int) {
	p.sink.RunStarted(targets)
	p.apply(func(base execution.Base) execution.Event {
		return execution.RunPhaseChanged{Base: base, Phase: execution.PhasePreparing}
	})
}
func (p *executionProgress) DiscoveryStarted(target string) { p.sink.DiscoveryStarted(target) }
func (p *executionProgress) Plan(target string, total int)  { p.sink.Plan(target, total) }
func (p *executionProgress) RevisePlan(target string, total int) {
	p.sink.RevisePlan(target, total)
}
func (p *executionProgress) AddPlan(target string, delta int) { p.sink.AddPlan(target, delta) }
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
	}
	p.apply(func(base execution.Base) execution.Event {
		return execution.PlanFrozen{Base: base, Revision: 1, Jobs: specs}
	})
	p.mu.Lock()
	p.frozen = p.err == nil
	p.mu.Unlock()
}

func (p *executionProgress) Start(id string) {
	p.frozenSink().Start(id)
	if !p.isKnown(id) {
		return
	}
	p.apply(func(base execution.Base) execution.Event { return execution.JobStarted{Base: withJob(base, id)} })
	p.apply(func(base execution.Base) execution.Event {
		return execution.RunPhaseChanged{Base: base, Phase: execution.PhaseRunning}
	})
}

func (p *executionProgress) Complete(id, outcome string) {
	p.frozenSink().Complete(id, outcome)
	if !p.isKnown(id) {
		return
	}
	p.apply(func(base execution.Base) execution.Event {
		return execution.JobFinished{Base: withJob(base, id), Outcome: executionOutcome(outcome)}
	})
}

// Lifecycle operation IDs are not frozen job IDs. Keep legacy rendering, but
// do not turn those trace spans into unproven reducer terminal outcomes.
func (p *executionProgress) ObserveLifecycle(event telemetry.LifecycleEvent) {
	p.sink.ObserveLifecycle(event)
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
	if err != nil || !frozen || unavailable {
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

func (p *executionProgress) frozenSink() progress.FrozenSink {
	return p.sink.(progress.FrozenSink)
}

func (p *executionProgress) apply(event func(execution.Base) execution.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return
	}
	p.sequence++
	base := execution.Base{Meta: execution.Meta{RunID: p.runID, Seq: p.sequence, Offset: time.Since(p.started)}}
	if err := p.reducer.Apply(event(base)); err != nil {
		p.err = err
		return
	}
	// The public aggregate is derived from the same live reducer snapshots as
	// the UI.  Recording only the final snapshot loses intervals in which a
	// waiter existed, so it would under-report real scheduler delay.
	if err := p.builder.Observe(p.reducer.Snapshot()); err != nil {
		p.err = err
	}
}

func withJob(base execution.Base, id string) execution.Base {
	base.JobID = id
	return base
}

func executionOutcome(outcome string) execution.Outcome {
	switch outcome {
	case "completed", "success", "pass", "passed":
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
