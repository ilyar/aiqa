package app

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/execution"
	"github.com/ilyar/aiqa/internal/progress"
	"github.com/ilyar/aiqa/internal/scheduler"
	"github.com/ilyar/aiqa/internal/telemetry"
)

type executionFakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*executionFakeTimer
}

type executionFakeTimer struct {
	at time.Time
	ch chan time.Time
}

func newExecutionFakeClock(now time.Time) *executionFakeClock { return &executionFakeClock{now: now} }

func (c *executionFakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *executionFakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	at := c.now.Add(d)
	if !at.After(c.now) {
		ch <- c.now
		close(ch)
		return ch
	}
	c.timers = append(c.timers, &executionFakeTimer{at: at, ch: ch})
	return ch
}

func (c *executionFakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	remaining := c.timers[:0]
	for _, timer := range c.timers {
		if !timer.at.After(c.now) {
			timer.ch <- c.now
			close(timer.ch)
		} else {
			remaining = append(remaining, timer)
		}
	}
	c.timers = remaining
	c.mu.Unlock()
}

func TestExecutionProgressDoesNotTreatTraceOperationAsFrozenJob(t *testing.T) {
	var output bytes.Buffer
	sink := progress.New(progress.Options{Writer: &output})
	p, err := newExecutionProgress("run-lifecycle", 1, sink)
	if err != nil {
		t.Fatal(err)
	}
	p.RunStarted(1)
	p.Freeze([]progress.Job{{ID: "frozen-job", Target: "target"}})
	p.ObserveLifecycle(telemetry.LifecycleEvent{Name: "probe", Phase: "start", OperationID: "trace-operation"})
	p.ObserveLifecycle(telemetry.LifecycleEvent{Name: "probe", Phase: "end", OperationID: "trace-operation", Outcome: "completed"})
	if strings.Contains(output.String(), "unknown job") {
		t.Fatalf("trace operation was forwarded as a frozen job: %q", output.String())
	}
	if snapshot := p.reducer.Snapshot(); snapshot.FinishedJobs != 0 {
		t.Fatalf("trace operation changed frozen job state: %#v", snapshot)
	}
}

func TestExecutionProgressBuildsSummaryFromFrozenTerminalDelivery(t *testing.T) {
	sink := progress.New(progress.Options{Writer: io.Discard})
	p, err := newExecutionProgress("run-test", 2, sink)
	if err != nil {
		t.Fatal(err)
	}
	p.RunStarted(1)
	p.Freeze([]progress.Job{{ID: "job-1", Target: "target-1"}})
	p.Start("job-1")
	p.Complete("job-1", "completed")
	if got := p.reducer.Snapshot().Targets[0].Phase; got != execution.PhaseCompleted {
		t.Fatalf("target phase=%q, want completed", got)
	}
	if err := p.Finalize(); err != nil {
		t.Fatal(err)
	}
	summary, err := p.Summary()
	if err != nil {
		t.Fatal(err)
	}
	if summary == nil {
		t.Fatal("summary is nil")
	}
	if summary.SchemaVersion != execution.ExecutionSummarySchema || summary.Phase != string(execution.PhaseFinalizing) || summary.GlobalRequestCapacity != 2 || summary.PlannedJobs != 1 || summary.FinishedJobs != 1 || summary.CancelledJobs != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	p.RunCompleted()
	if got := p.reducer.Snapshot().Run.Phase; got != execution.PhaseCompleted {
		t.Fatalf("run phase=%q, want completed", got)
	}
}

func TestExecutionProgressBridgesSchedulerWaitsGroupsAndPhysicalTransport(t *testing.T) {
	sink := progress.New(progress.Options{Writer: io.Discard})
	p, err := newExecutionProgress("run-bridge", 2, sink)
	if err != nil {
		t.Fatal(err)
	}
	p.RunStarted(1)
	p.Freeze([]progress.Job{{ID: "job-1", Target: "target-1"}})
	owner := api.ExecutionOwner{TargetID: "target-1", JobID: "job-1"}
	at := p.started.Add(time.Second)
	p.ObserveSchedulerFact(scheduler.Fact{Kind: scheduler.FactGroupUpdated, At: at, Group: "group-1", Capacity: 1, Interval: time.Second, Revision: 3}, owner)
	p.ObserveSchedulerFact(scheduler.Fact{Kind: scheduler.FactWaitStarted, At: at, RequestID: "request-1", WaitID: "wait-1", Target: "target-1", Group: "group-1", Reservation: scheduler.ReservationPhysical, Reason: scheduler.WaitInitialPacing, EligibleAt: at.Add(time.Second), Revision: 3}, owner)
	p.ObserveSchedulerFact(scheduler.Fact{Kind: scheduler.FactWaitEnded, At: at.Add(time.Second), RequestID: "request-1", WaitID: "wait-1", Target: "target-1", Group: "group-1", Reservation: scheduler.ReservationPhysical, Reason: scheduler.WaitInitialPacing}, owner)
	// Artifact/memory/opaque reservations are resource accounting, not HTTP
	// attempts, and must not inflate the execution request cardinality.
	p.ObserveSchedulerFact(scheduler.Fact{Kind: scheduler.FactRequestAdmitted, At: at, RequestID: "artifact-1", Target: "target-1", Group: "group-1", Reservation: scheduler.ReservationArtifact}, owner)
	p.ObserveAPIExecution(api.ExecutionEvent{Kind: api.ExecutionRequestAdmitted, TargetID: owner.TargetID, JobID: owner.JobID, AttemptID: "attempt-1", GroupID: "group-1"})
	p.ObserveAPIExecution(api.ExecutionEvent{Kind: api.ExecutionRequestHeaders, TargetID: owner.TargetID, JobID: owner.JobID, AttemptID: "attempt-1", GroupID: "group-1"})
	p.ObserveAPIExecution(api.ExecutionEvent{Kind: api.ExecutionRequestTerminal, TargetID: owner.TargetID, JobID: owner.JobID, AttemptID: "attempt-1", GroupID: "group-1", Outcome: "succeeded"})
	snapshot := p.reducer.Snapshot()
	if len(snapshot.Waits) != 0 || len(snapshot.Groups) != 1 {
		t.Fatalf("scheduler bridge snapshot=%#v", snapshot)
	}
	if len(snapshot.Requests) != 1 || snapshot.Requests[0].ID != "attempt-1" || snapshot.Requests[0].State != execution.RequestTerminal {
		t.Fatalf("transport bridge snapshot=%#v", snapshot.Requests)
	}
	if got := snapshot.Requests[0].Outcome; got != execution.OutcomeSucceeded {
		t.Fatalf("transport success outcome=%q, want %q", got, execution.OutcomeSucceeded)
	}
	if p.err != nil {
		t.Fatalf("bridge reducer error=%v", p.err)
	}
}

func TestExecutionProgressClampsExpiredSchedulerCooldownAndPreservesRetryReason(t *testing.T) {
	sink := progress.New(progress.Options{Writer: io.Discard})
	p, err := newExecutionProgress("run-cooldown", 1, sink)
	if err != nil {
		t.Fatal(err)
	}
	at := p.started.Add(10 * time.Second)
	p.ObserveSchedulerFact(scheduler.Fact{Kind: scheduler.FactGroupUpdated, At: at, Group: "quota", Capacity: 1, Interval: time.Second, Cooldown: p.started.Add(5 * time.Second), Reason: scheduler.WaitRetryAfter}, api.ExecutionOwner{})
	if p.err != nil {
		t.Fatalf("expired cooldown caused reducer error: %v", p.err)
	}
	groups := p.reducer.Snapshot().Groups
	if len(groups) != 1 || groups[0].CooldownUntil != 10*time.Second || groups[0].CooldownReason != execution.WaitRetryAfter {
		t.Fatalf("cooldown snapshot=%+v", groups)
	}
}

func TestExecutionProgressBridgesFirst429FactThenSuccessfulJob(t *testing.T) {
	sink := progress.New(progress.Options{Writer: io.Discard})
	p, err := newExecutionProgress("run-first-429", 1, sink)
	if err != nil {
		t.Fatal(err)
	}
	p.RunStarted(1)
	p.Freeze([]progress.Job{{ID: "job-1", Target: "target-1"}})
	owner := api.ExecutionOwner{TargetID: "target-1", JobID: "job-1"}
	clock := newExecutionFakeClock(p.started)
	broker, err := scheduler.New(scheduler.Config{Capacity: 1, Clock: clock, Groups: map[string]scheduler.Group{
		"quota": {Capacity: 1, Adaptive: &scheduler.AdaptiveGroup{InitialInterval: time.Second, MaxCapacity: 1}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	broker.SetFactObserver(func(fact scheduler.Fact) { p.ObserveSchedulerFact(fact, owner) })
	lease, err := broker.Acquire(context.Background(), scheduler.Request{Target: owner.TargetID, Group: "quota"})
	if err != nil {
		t.Fatal(err)
	}
	p.Start(owner.JobID)
	lease.RateLimited(time.Minute)
	lease.Release()
	clock.Advance(time.Minute)
	retry, err := broker.Acquire(context.Background(), scheduler.Request{Target: owner.TargetID, Group: "quota"})
	if err != nil {
		t.Fatal(err)
	}
	retry.Succeeded()
	retry.Release()
	p.Complete(owner.JobID, "completed")
	snapshot := p.reducer.Snapshot()
	if p.err != nil || snapshot.FinishedJobs != 1 {
		t.Fatalf("429 bridge failed before successful completion: err=%v snapshot=%+v", p.err, snapshot.Run)
	}
	if len(snapshot.Groups) != 1 || snapshot.Groups[0].CooldownReason != execution.WaitRetryAfter {
		t.Fatalf("retry-after reason was not preserved: %+v", snapshot.Groups)
	}
}

type unavailableTestSink struct {
	*progress.Reporter
	errs []error
}

func (s *unavailableTestSink) ObserveExecutionUnavailable(err error) { s.errs = append(s.errs, err) }

func TestExecutionProgressNotifiesSinkOnceOnReducerError(t *testing.T) {
	sink := &unavailableTestSink{Reporter: progress.New(progress.Options{Writer: io.Discard})}
	p, err := newExecutionProgress("run-unavailable", 1, sink)
	if err != nil {
		t.Fatal(err)
	}
	p.apply(func(base execution.Base) execution.Event {
		return execution.GroupCooldownChanged{Base: base, Until: time.Second, Reason: execution.WaitRetryAfter}
	})
	p.apply(func(base execution.Base) execution.Event {
		return execution.RunPhaseChanged{Base: base, Phase: execution.PhaseRunning}
	})
	if len(sink.errs) != 1 || sink.errs[0] == nil {
		t.Fatalf("unavailable notifications=%v", sink.errs)
	}
	if _, err := p.Summary(); err == nil {
		t.Fatal("reducer error was not retained for Summary")
	}
}

func TestExecutionProgressBridgesRegisteredPreparationWithoutFrozenJob(t *testing.T) {
	sink := progress.New(progress.Options{Writer: io.Discard})
	p, err := newExecutionProgress("run-preparation", 1, sink)
	if err != nil {
		t.Fatal(err)
	}
	p.RunStarted(1)
	owner := api.ExecutionOwner{TargetID: "target-discovery", PreparationID: "models-discovery"}
	p.RegisterPreparation(owner, execution.PreparationDiscovery)
	p.ObserveAPIExecution(api.ExecutionEvent{Kind: api.ExecutionRequestAdmitted, TargetID: owner.TargetID, PreparationID: owner.PreparationID, AttemptID: "discovery-attempt", GroupID: "group-discovery"})
	p.ObserveAPIExecution(api.ExecutionEvent{Kind: api.ExecutionRequestTerminal, TargetID: owner.TargetID, PreparationID: owner.PreparationID, AttemptID: "discovery-attempt", GroupID: "group-discovery", Outcome: "succeeded"})
	snapshot := p.reducer.Snapshot()
	if len(snapshot.Preparations) != 1 || len(snapshot.Jobs) != 0 || len(snapshot.Requests) != 1 || snapshot.Requests[0].PreparationID != owner.PreparationID {
		t.Fatalf("preparation bridge fabricated frozen job: %#v", snapshot)
	}
	if p.err != nil {
		t.Fatalf("preparation bridge error=%v", p.err)
	}
}

func TestExecutionProgressOmitsSummaryForUnfrozenJobDelivery(t *testing.T) {
	sink := progress.New(progress.Options{Writer: io.Discard})
	p, err := newExecutionProgress("run-test", 1, sink)
	if err != nil {
		t.Fatal(err)
	}
	p.RunStarted(1)
	p.Freeze([]progress.Job{{ID: "known", Target: "target"}})
	p.Start("unknown")
	p.Complete("known", "completed")
	if err := p.Finalize(); err != nil {
		t.Fatal(err)
	}
	summary, err := p.Summary()
	if err != nil {
		t.Fatal(err)
	}
	if summary != nil {
		t.Fatalf("summary=%+v, want nil after accounting mismatch", summary)
	}
}
