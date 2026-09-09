package app

import (
	"io"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/execution"
	"github.com/ilyar/aiqa/internal/progress"
	"github.com/ilyar/aiqa/internal/scheduler"
)

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
