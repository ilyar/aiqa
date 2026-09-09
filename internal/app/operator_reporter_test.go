package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ilyar/aiqa/internal/execution"
)

func TestOperatorReporterRendersExecutionSnapshotsOnlyInTTY(t *testing.T) {
	snapshot := execution.Snapshot{
		Run:         execution.RunSnapshot{ID: "run", Phase: execution.PhaseRunning},
		Targets:     []execution.TargetSnapshot{{ID: "target", Phase: execution.PhaseRunning}},
		Jobs:        []execution.JobSnapshot{{ID: "job", TargetID: "target", State: execution.JobRunning}},
		PlannedJobs: 1,
		RunningJobs: 1,
	}
	var tty bytes.Buffer
	r := newAppOperatorReporter(&tty, true, false)
	r.ObserveExecutionSnapshot(snapshot)
	r.ObserveExecutionSnapshot(snapshot)
	if got := tty.String(); !strings.Contains(got, "\x1b[") || !strings.Contains(got, "AIQA") {
		t.Fatalf("TTY renderer output=%q", got)
	}

	var plain bytes.Buffer
	newAppOperatorReporter(&plain, false, false).ObserveExecutionSnapshot(snapshot)
	if plain.Len() != 0 {
		t.Fatalf("plain snapshot leaked terminal controls: %q", plain.String())
	}
}
