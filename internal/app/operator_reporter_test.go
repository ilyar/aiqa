package app

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/execution"
	"github.com/ilyar/aiqa/internal/terminal"
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
	t.Cleanup(r.Close)
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
	t.Cleanup(r.Close)
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

func TestOperatorReporterClearsRowsRemovedByShorterFrame(t *testing.T) {
	var output bytes.Buffer
	r := newAppOperatorReporter(&output, true, false, 1)
	r.draw(terminal.Frame{Lines: []string{"header", "target-a", "target-b"}})
	r.draw(terminal.Frame{Lines: []string{"header"}})
	got := output.String()
	// The second frame moves back over all old rows and clears both rows that
	// are no longer part of the frame; plain output never uses this path.
	if !strings.Contains(got, "\x1b[3A") || strings.Count(got, "\x1b[2K") < 6 {
		t.Fatalf("shorter frame did not clear stale rows: %q", got)
	}
}

func TestOperatorReporterRefreshesElapsedAndETAWithoutExecutionEvent(t *testing.T) {
	oldNow := operatorNow
	now := time.Unix(100, 0)
	operatorNow = func() time.Time { return now }
	t.Cleanup(func() { operatorNow = oldNow })

	var output bytes.Buffer
	r := newAppOperatorReporter(&output, true, false, 1)
	t.Cleanup(r.Close)
	r.ObserveExecutionSnapshot(execution.Snapshot{
		Run:         execution.RunSnapshot{ID: "run", Phase: execution.PhaseRunning, Offset: 43 * time.Second},
		Targets:     []execution.TargetSnapshot{{ID: "target", Phase: execution.PhaseRunning}},
		Jobs:        []execution.JobSnapshot{{ID: "job", TargetID: "target", State: execution.JobPlanned}},
		Groups:      []execution.GroupSnapshot{{ID: "quota", Capacity: 1, CooldownUntil: 60 * time.Second}},
		PlannedJobs: 1,
	})

	// No reducer event arrives while a request is in cooldown. The refresh path
	// must nevertheless advance both elapsed time and the ETA countdown.
	now = now.Add(2 * time.Second)
	r.renderLatest()
	got := output.String()
	if !strings.Contains(got, "Elapsed 45s") || !strings.Contains(got, "ETA >= 15s") {
		t.Fatalf("refresh did not advance elapsed/ETA: %q", got)
	}
}

func TestOperatorReporterShowsSchedulerWaitsPerTarget(t *testing.T) {
	oldSize := terminalSize
	terminalSize = func() (int, int, error) { return 120, 24, nil }
	t.Cleanup(func() { terminalSize = oldSize })
	var output bytes.Buffer
	r := newAppOperatorReporter(&output, true, false, 1)
	t.Cleanup(r.Close)
	r.ObserveExecutionSnapshot(execution.Snapshot{
		Run:         execution.RunSnapshot{ID: "run", Phase: execution.PhaseRunning, Offset: 10 * time.Second},
		Targets:     []execution.TargetSnapshot{{ID: "google-genai", Phase: execution.PhaseRunning}},
		Jobs:        []execution.JobSnapshot{{ID: "job", TargetID: "google-genai", State: execution.JobRunning}},
		Waits:       []execution.WaitSnapshot{{ID: "wait", TargetID: "google-genai", EligibleAt: 40 * time.Second}},
		PlannedJobs: 1,
	})
	if got := output.String(); !strings.Contains(got, "Waiting 1") || !strings.Contains(got, "WAITING") || !strings.Contains(got, "next in 30s") {
		t.Fatalf("terminal frame does not explain scheduler wait: %q", got)
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

func TestOperatorReporterShowsCanonicalCompatibilityVerdict(t *testing.T) {
	var output bytes.Buffer
	r := newAppOperatorReporter(&output, true, false, 1)
	t.Cleanup(r.Close)
	r.SetCompatibilityVerdicts(map[string]string{"target": "pass"}, "pass")
	r.ObserveExecutionSnapshot(execution.Snapshot{
		Run:         execution.RunSnapshot{ID: "run", Phase: execution.PhaseCompleted},
		Targets:     []execution.TargetSnapshot{{ID: "target", Phase: execution.PhaseCompleted}},
		Jobs:        []execution.JobSnapshot{{ID: "job", TargetID: "target", State: execution.JobTerminal}},
		PlannedJobs: 1, FinishedJobs: 1,
	})
	if got := output.String(); !strings.Contains(got, "PASS") || !strings.Contains(got, "FINISHED") {
		t.Fatalf("terminal frame misses canonical verdict: %q", got)
	}
}
