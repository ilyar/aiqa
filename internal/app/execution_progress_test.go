package app

import (
	"io"
	"testing"

	"github.com/ilyar/aiqa/internal/execution"
	"github.com/ilyar/aiqa/internal/progress"
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
