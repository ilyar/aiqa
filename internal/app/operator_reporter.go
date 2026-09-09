package app

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/ilyar/aiqa/internal/eta"
	"github.com/ilyar/aiqa/internal/execution"
	"github.com/ilyar/aiqa/internal/progress"
	"github.com/ilyar/aiqa/internal/terminal"
	"golang.org/x/term"
)

// operatorReporter is the sole stderr owner for interactive runs. Plain runs
// retain the durable legacy reporter, including its independently tested
// heartbeat. The execution reducer supplies the TTY view; trace events never
// provide display job identities.
type operatorReporter struct {
	*progress.Reporter
	writer   io.Writer
	tty      bool
	capacity int

	mu    sync.Mutex
	lines int
}

var terminalSize = func() (int, int, error) { return term.GetSize(2) }

func newAppOperatorReporter(writer io.Writer, tty bool, disabled bool, capacity int) *operatorReporter {
	if disabled {
		return &operatorReporter{Reporter: progress.New(progress.Options{Writer: io.Discard})}
	}
	legacyWriter := writer
	if tty {
		legacyWriter = io.Discard
	}
	if capacity < 1 {
		capacity = 1
	}
	return &operatorReporter{Reporter: progress.New(progress.Options{Writer: legacyWriter, TTY: false}), writer: writer, tty: tty, capacity: capacity}
}

// ObserveExecutionSnapshot draws an inline frame after an accepted reducer
// event. It is deliberately a small view adapter: scheduling and accounting
// remain entirely in execution.Reducer.
func (r *operatorReporter) ObserveExecutionSnapshot(snapshot execution.Snapshot) {
	if r == nil || !r.tty {
		return
	}
	width := 80
	if n, _, err := terminalSize(); err == nil && n > 1 {
		width = n
	}
	rows := make([]terminal.Target, 0, len(snapshot.Targets))
	for _, target := range snapshot.Targets {
		done, total := 0, 0
		for _, job := range snapshot.Jobs {
			if job.TargetID != target.ID {
				continue
			}
			total++
			if job.State == execution.JobTerminal {
				done++
			}
		}
		rows = append(rows, terminal.Target{ID: target.ID, State: terminalTargetState(target.Phase), JobsDone: done, JobsTotal: total})
	}
	frame := terminal.Render(terminal.Snapshot{
		RunID: snapshot.Run.ID, Phase: string(snapshot.Run.Phase), Elapsed: snapshot.Run.Offset.Round(1e9).String(),
		ETA: etaText(snapshot, r.capacity), JobsDone: snapshot.FinishedJobs, JobsTotal: snapshot.PlannedJobs,
		Inflight: countInflight(snapshot), Concurrency: r.capacity, HTTPDone: terminalRequests(snapshot),
		TargetsDone: terminalTargetsDone(snapshot), TargetsTotal: len(snapshot.Targets), Rows: rows,
	}, width, 0, terminal.Options{})
	r.draw(frame)
}

func terminalTargetState(phase execution.Phase) string {
	switch phase {
	case execution.PhaseCompleted:
		return "finished"
	case execution.PhaseFailed:
		return "failed"
	case execution.PhaseInterrupted:
		return "interrupted"
	case execution.PhaseRunning:
		return "running"
	default:
		return "preparing"
	}
}

func etaText(snapshot execution.Snapshot, capacity int) string {
	if snapshot.Run.Phase == execution.PhasePreparing || snapshot.PlannedJobs == 0 {
		return "estimating"
	}
	base := time.Unix(0, 0)
	now := base.Add(snapshot.Run.Offset)
	groups := make(map[string]eta.Group, len(snapshot.Groups))
	for _, group := range snapshot.Groups {
		cooldown := time.Time{}
		if group.CooldownUntil > 0 {
			cooldown = base.Add(group.CooldownUntil)
		}
		groups[group.ID] = eta.Group{ID: group.ID, Capacity: group.Capacity, StartInterval: group.Interval, Cooldown: cooldown}
	}
	if len(groups) == 0 {
		return "estimating"
	}
	jobGroups := map[string]string{}
	for _, request := range snapshot.Requests {
		if request.JobID != "" && request.GroupID != "" {
			jobGroups[request.JobID] = request.GroupID
		}
	}
	for _, wait := range snapshot.Waits {
		if wait.JobID != "" && wait.GroupID != "" {
			jobGroups[wait.JobID] = wait.GroupID
		}
	}
	defaultGroup := ""
	for id := range groups {
		defaultGroup = id
		break
	}
	jobs := make([]eta.Job, 0, len(snapshot.Jobs))
	for _, job := range snapshot.Jobs {
		if job.State == execution.JobTerminal {
			continue
		}
		group := jobGroups[job.ID]
		if group == "" {
			group = defaultGroup
		}
		jobs = append(jobs, eta.Job{ID: job.ID, Group: group, UnknownDuration: true})
	}
	result, err := eta.New(nil).Estimate(eta.Snapshot{Now: now, GlobalCapacity: max(1, capacity), Groups: groups, Jobs: jobs})
	if err != nil || result.RangeLow == nil {
		return "estimating"
	}
	if result.State == eta.LowerBound {
		return ">= " + eta.RoundDisplay(*result.RangeLow).String()
	}
	if result.Estimate != nil {
		return eta.RoundDisplay(*result.Estimate).String()
	}
	return "estimating"
}

func (r *operatorReporter) draw(frame terminal.Frame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lines > 0 {
		_, _ = fmt.Fprintf(r.writer, "\x1b[%dA", r.lines)
	}
	for _, line := range frame.Lines {
		_, _ = fmt.Fprintf(r.writer, "\r\x1b[2K%s\n", line)
	}
	for i := len(frame.Lines); i < r.lines; i++ {
		_, _ = io.WriteString(r.writer, "\r\x1b[2K\n")
	}
	r.lines = len(frame.Lines)
}

func (r *operatorReporter) Close() {
	if r != nil && r.tty {
		r.mu.Lock()
		if r.lines > 0 {
			_, _ = io.WriteString(r.writer, "\n")
			r.lines = 0
		}
		r.mu.Unlock()
	}
	r.Reporter.Close()
}

func countInflight(snapshot execution.Snapshot) int {
	n := 0
	for _, request := range snapshot.Requests {
		if request.State != execution.RequestTerminal {
			n++
		}
	}
	return n
}

func terminalRequests(snapshot execution.Snapshot) int {
	n := 0
	for _, request := range snapshot.Requests {
		if request.State == execution.RequestTerminal {
			n++
		}
	}
	return n
}

func terminalTargetsDone(snapshot execution.Snapshot) int {
	n := 0
	for _, target := range snapshot.Targets {
		if target.Phase == execution.PhaseCompleted || target.Phase == execution.PhaseFailed || target.Phase == execution.PhaseInterrupted {
			n++
		}
	}
	return n
}
