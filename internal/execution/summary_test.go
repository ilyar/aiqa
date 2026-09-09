package execution

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSummaryBuilderUsesOnlySafeMonotonicAggregateAccounting(t *testing.T) {
	clock := newFakeClock()
	reducer, err := New("run-1", clock)
	if err != nil {
		t.Fatal(err)
	}
	if err := reducer.Apply(PlanFrozen{Base: meta("run-1", 1, 0), Revision: 1, Jobs: []JobSpec{{ID: "private-job-alpha", TargetID: "private-target-alpha"}, {ID: "private-job-beta", TargetID: "private-target-beta"}}}); err != nil {
		t.Fatal(err)
	}
	request := Meta{RunID: "run-1", Seq: 2, TargetID: "private-target-alpha", JobID: "private-job-alpha", AttemptID: "private-attempt-alpha", GroupID: "private-group-alpha"}
	if err := reducer.Apply(RequestQueued{Base: Base{Meta: request}}); err != nil {
		t.Fatal(err)
	}
	request.Seq = 3
	if err := reducer.Apply(RequestAdmitted{Base: Base{Meta: request}}); err != nil {
		t.Fatal(err)
	}
	if err := reducer.Apply(WaitStarted{Base: Base{Meta: Meta{RunID: "run-1", Seq: 4, TargetID: "private-target-beta", JobID: "private-job-beta", GroupID: "private-group-beta"}}, WaitID: "private-wait", Reason: WaitInitialPacing, Source: SourceInitialPacing, EligibleAt: time.Minute}); err != nil {
		t.Fatal(err)
	}
	builder, err := NewSummaryBuilder(SummaryOptions{GlobalRequestCapacity: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.Observe(reducer.Snapshot()); err != nil {
		t.Fatal(err)
	}
	clock.advance(10 * time.Second)
	request.Seq, request.Offset = 5, 10*time.Second
	if err := reducer.Apply(RequestCompleted{Base: Base{Meta: request}, Outcome: OutcomeSucceeded}); err != nil {
		t.Fatal(err)
	}
	if err := reducer.Apply(JobFinished{Base: Base{Meta: Meta{RunID: "run-1", Seq: 6, JobID: "private-job-alpha", Offset: 10 * time.Second}}, Outcome: OutcomeSucceeded}); err != nil {
		t.Fatal(err)
	}
	if err := reducer.Apply(JobCancelled{Base: Base{Meta: Meta{RunID: "run-1", Seq: 7, JobID: "private-job-beta", Offset: 10 * time.Second}}, Outcome: OutcomeCancelled}); err != nil {
		t.Fatal(err)
	}
	if err := reducer.Apply(GroupLimitsChanged{Base: Base{Meta: Meta{RunID: "run-1", Seq: 8, GroupID: "private-group-alpha", Offset: 10 * time.Second}}, Capacity: 1}); err != nil {
		t.Fatal(err)
	}
	summary, err := builder.Build(reducer.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if summary.ObservedPeakInflight != 1 || summary.SumRequestWaitMS != 10_000 || summary.WallTimeWithWaitersMS != 10_000 {
		t.Fatalf("unexpected accounting: %+v", summary)
	}
	if summary.PlannedJobs != 2 || summary.FinishedJobs != 1 || summary.CancelledJobs != 1 || summary.PacingGroupCount != 1 {
		t.Fatalf("unexpected job/group summary: %+v", summary)
	}
	if summary.SchemaVersion != ExecutionSummarySchema || summary.SchedulerPolicy != DefaultSchedulerPolicy || summary.EstimateMethod != DefaultEstimateMethod || summary.ElapsedMS != 10_000 {
		t.Fatalf("unexpected metadata: %+v", summary)
	}
	encoded := summaryString(summary)
	for _, forbidden := range []string{"private-target-alpha", "private-attempt-alpha", "private-group-alpha", "private-job-alpha"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("summary exposes internal identity %q: %s", forbidden, encoded)
		}
	}
}

func TestSummaryBuilderDoesNotRegressOnStaleSnapshot(t *testing.T) {
	builder, err := NewSummaryBuilder(SummaryOptions{GlobalRequestCapacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	newer := Snapshot{SchemaVersion: SchemaVersion, Run: RunSnapshot{Phase: PhaseRunning, Offset: 20 * time.Second}, Requests: []RequestSnapshot{{State: RequestHeadersState}}, Waits: []WaitSnapshot{{ID: "wait"}}}
	if err := builder.Observe(newer); err != nil {
		t.Fatal(err)
	}
	older := newer
	older.Run.Offset = 5 * time.Second
	older.Requests = nil
	older.Waits = nil
	if err := builder.Observe(older); err != nil {
		t.Fatal(err)
	}
	final := newer
	final.Run.Offset = 30 * time.Second
	final.Requests = nil
	final.Waits = nil
	summary, err := builder.Build(final)
	if err != nil {
		t.Fatal(err)
	}
	if summary.ObservedPeakInflight != 1 || summary.SumRequestWaitMS != 10_000 || summary.WallTimeWithWaitersMS != 10_000 {
		t.Fatalf("stale snapshot regressed counters: %+v", summary)
	}
}

func TestSummaryBuilderRejectsInvalidInputs(t *testing.T) {
	if _, err := NewSummaryBuilder(SummaryOptions{}); err == nil {
		t.Fatal("zero capacity unexpectedly accepted")
	}
	builder, err := NewSummaryBuilder(SummaryOptions{GlobalRequestCapacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.Observe(Snapshot{}); err == nil {
		t.Fatal("invalid schema unexpectedly accepted")
	}
}

func summaryString(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return err.Error()
	}
	return string(data)
}
