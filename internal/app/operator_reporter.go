package app

import (
	"fmt"
	"io"
	"sync"

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
	writer io.Writer
	tty    bool

	mu    sync.Mutex
	lines int
}

func newAppOperatorReporter(writer io.Writer, tty bool, disabled bool) *operatorReporter {
	if disabled {
		return &operatorReporter{Reporter: progress.New(progress.Options{Writer: io.Discard})}
	}
	legacyWriter := writer
	if tty {
		legacyWriter = io.Discard
	}
	return &operatorReporter{Reporter: progress.New(progress.Options{Writer: legacyWriter, TTY: false}), writer: writer, tty: tty}
}

// ObserveExecutionSnapshot draws an inline frame after an accepted reducer
// event. It is deliberately a small view adapter: scheduling and accounting
// remain entirely in execution.Reducer.
func (r *operatorReporter) ObserveExecutionSnapshot(snapshot execution.Snapshot) {
	if r == nil || !r.tty {
		return
	}
	width := 80
	if n, _, err := term.GetSize(2); err == nil && n > 1 {
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
		rows = append(rows, terminal.Target{ID: target.ID, State: string(target.Phase), JobsDone: done, JobsTotal: total})
	}
	frame := terminal.Render(terminal.Snapshot{
		RunID: snapshot.Run.ID, Phase: string(snapshot.Run.Phase), Elapsed: snapshot.Run.Offset.Round(1e9).String(),
		ETA: "estimating", JobsDone: snapshot.FinishedJobs, JobsTotal: snapshot.PlannedJobs,
		Inflight: countInflight(snapshot), Concurrency: 1, HTTPDone: terminalRequests(snapshot),
		TargetsDone: terminalTargetsDone(snapshot), TargetsTotal: len(snapshot.Targets), Rows: rows,
	}, width, 0, terminal.Options{})
	r.draw(frame)
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
