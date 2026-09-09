package app

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/execution"
	"github.com/rivo/uniseg"
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
	r := newAppOperatorReporter(&tty, true, false, 1)
	r.ObserveExecutionSnapshot(snapshot)
	r.ObserveExecutionSnapshot(snapshot)
	if got := tty.String(); !strings.Contains(got, "\x1b[") || !strings.Contains(got, "AIQA") {
		t.Fatalf("TTY renderer output=%q", got)
	}

	var plain bytes.Buffer
	newAppOperatorReporter(&plain, false, false, 1).ObserveExecutionSnapshot(snapshot)
	if plain.Len() != 0 {
		t.Fatalf("plain snapshot leaked terminal controls: %q", plain.String())
	}
}

func TestOperatorReporterHonorsCurrentTerminalWidth(t *testing.T) {
	old := terminalSize
	terminalSize = func() (int, int, error) { return 48, 24, nil }
	t.Cleanup(func() { terminalSize = old })
	var output bytes.Buffer
	r := newAppOperatorReporter(&output, true, false, 1)
	r.ObserveExecutionSnapshot(execution.Snapshot{
		Run:         execution.RunSnapshot{ID: strings.Repeat("run", 32), Phase: execution.PhaseRunning},
		Targets:     []execution.TargetSnapshot{{ID: strings.Repeat("target", 32), Phase: execution.PhaseRunning}},
		Jobs:        []execution.JobSnapshot{{ID: "job", TargetID: strings.Repeat("target", 32), State: execution.JobRunning}},
		PlannedJobs: 1,
	})
	for _, line := range strings.Split(output.String(), "\n") {
		line = strings.ReplaceAll(line, "\r\x1b[2K", "")
		if uniseg.StringWidth(line) > 47 {
			t.Fatalf("line width=%d > 47: %q", uniseg.StringWidth(line), line)
		}
	}
}

func TestOperatorETAUsesSchedulerCooldownAsLowerBound(t *testing.T) {
	snapshot := execution.Snapshot{
		Run:         execution.RunSnapshot{Phase: execution.PhaseRunning},
		Groups:      []execution.GroupSnapshot{{ID: "quota", Capacity: 1, CooldownUntil: 61 * time.Second}},
		Jobs:        []execution.JobSnapshot{{ID: "job", TargetID: "target", State: execution.JobPlanned}},
		PlannedJobs: 1,
	}
	if got, want := etaText(snapshot, 1), ">= 1m1s"; got != want {
		t.Fatalf("ETA=%q, want %q", got, want)
	}
}

func TestTerminalTargetStateKeepsRuntimeOutcomesDistinct(t *testing.T) {
	for phase, want := range map[execution.Phase]string{
		execution.PhaseCompleted:   "finished",
		execution.PhaseFailed:      "failed",
		execution.PhaseInterrupted: "interrupted",
	} {
		if got := terminalTargetState(phase); got != want {
			t.Fatalf("phase %q renders as %q, want %q", phase, got, want)
		}
	}
}
