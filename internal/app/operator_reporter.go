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

	mu         sync.Mutex // serializes terminal writes
	lines      int
	stateMu    sync.Mutex
	verdicts   map[string]string
	runVerdict string
	snapshot   *execution.Snapshot
	snapshotAt time.Time
	refresh    terminalRefreshTicker
	stop       chan struct{}
	refreshWG  sync.WaitGroup
}

var terminalSize = func() (int, int, error) { return term.GetSize(2) }

const terminalRefreshInterval = time.Second

// operatorNow is a seam for deterministic terminal refresh tests. Production
// values carry Go's monotonic clock, so wall-clock changes do not perturb an
// in-progress elapsed duration.
var operatorNow = time.Now

type terminalRefreshTicker interface {
	Chan() <-chan time.Time
	Stop()
}

type realTerminalRefreshTicker struct{ *time.Ticker }

func (t realTerminalRefreshTicker) Chan() <-chan time.Time { return t.C }

var newTerminalRefreshTicker = func(interval time.Duration) terminalRefreshTicker {
	return realTerminalRefreshTicker{Ticker: time.NewTicker(interval)}
}

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
	return &operatorReporter{Reporter: progress.New(progress.Options{Writer: legacyWriter, TTY: false}), writer: writer, tty: tty, capacity: capacity, verdicts: map[string]string{}}
}

// SetCompatibilityVerdicts supplies canonical assessment results after the
// report has been assessed. Runtime state and conformance verdict intentionally
// remain separate fields in terminal.Target.
func (r *operatorReporter) SetCompatibilityVerdicts(verdicts map[string]string, runVerdict string) {
	if r == nil {
		return
	}
	copy := make(map[string]string, len(verdicts))
	for target, verdict := range verdicts {
		copy[target] = verdict
	}
	r.stateMu.Lock()
	r.verdicts, r.runVerdict = copy, runVerdict
	r.stateMu.Unlock()
}

// ObserveExecutionSnapshot draws an inline frame after an accepted reducer
// event. It is deliberately a small view adapter: scheduling and accounting
// remain entirely in execution.Reducer.
func (r *operatorReporter) ObserveExecutionSnapshot(snapshot execution.Snapshot) {
	if r == nil || !r.tty {
		return
	}
	r.stateMu.Lock()
	r.snapshot = &snapshot
	r.snapshotAt = operatorNow()
	if r.refresh == nil {
		r.refresh = newTerminalRefreshTicker(terminalRefreshInterval)
		r.stop = make(chan struct{})
		r.refreshWG.Add(1)
		go r.refreshLoop(r.refresh, r.stop)
	}
	r.stateMu.Unlock()
	r.renderLatest()
}

func (r *operatorReporter) refreshLoop(ticker terminalRefreshTicker, stop <-chan struct{}) {
	defer r.refreshWG.Done()
	for {
		select {
		case <-ticker.Chan():
			r.renderLatest()
		case <-stop:
			return
		}
	}
}

func (r *operatorReporter) renderLatest() {
	r.stateMu.Lock()
	if r.snapshot == nil {
		r.stateMu.Unlock()
		return
	}
	snapshot := *r.snapshot
	if elapsed := operatorNow().Sub(r.snapshotAt); elapsed > 0 {
		snapshot.Run.Offset += elapsed
	}
	verdicts, runVerdict := r.verdicts, r.runVerdict
	r.stateMu.Unlock()
	r.renderSnapshot(snapshot, verdicts, runVerdict)
}

func (r *operatorReporter) renderSnapshot(snapshot execution.Snapshot, verdicts map[string]string, runVerdict string) {
	width := 80
	if n, _, err := terminalSize(); err == nil && n > 1 {
		width = n
	}
	rows := make([]terminal.Target, 0, len(snapshot.Targets))
	waits := targetWaits(snapshot)
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
		waiting := waits[target.ID]
		state := terminalTargetState(target.Phase)
		if target.Phase == execution.PhaseRunning && waiting.count > 0 {
			state = "waiting"
		}
		rows = append(rows, terminal.Target{ID: target.ID, State: state, JobsDone: done, JobsTotal: total, Verdict: verdicts[target.ID]})
	}
	_, nextWait := waitSummary(snapshot)
	frame := terminal.Render(terminal.Snapshot{
		RunID: snapshot.Run.ID, Phase: string(snapshot.Run.Phase), Elapsed: snapshot.Run.Offset.Round(1e9).String(),
		ETA: etaText(snapshot, r.capacity), JobsDone: snapshot.FinishedJobs, JobsTotal: snapshot.PlannedJobs,
		Inflight: countInflight(snapshot), Concurrency: r.capacity, Waiting: len(snapshot.Waits), WaitingFor: nextWait, RPS: requestRate(snapshot),
		TargetsDone: terminalTargetsDone(snapshot), TargetsTotal: len(snapshot.Targets), Verdict: runVerdict, Rows: rows,
	}, width, 0, terminal.Options{})
	r.draw(frame)
}

// waitSummary is presentation-only: it reports the earliest future eligibility
// deadline among current waits without changing reducer or scheduler state.
func waitSummary(snapshot execution.Snapshot) (int, time.Duration) {
	var next time.Duration
	for _, wait := range snapshot.Waits {
		remaining := wait.EligibleAt - snapshot.Run.Offset
		if remaining > 0 && (next == 0 || remaining < next) {
			next = remaining
		}
	}
	return len(snapshot.Waits), next
}

type targetWait struct {
	count int
	next  time.Duration
}

func targetWaits(snapshot execution.Snapshot) map[string]targetWait {
	byTarget := make(map[string]targetWait)
	for _, wait := range snapshot.Waits {
		if wait.TargetID == "" {
			continue
		}
		remaining := wait.EligibleAt - snapshot.Run.Offset
		if remaining < 0 {
			remaining = 0
		}
		current, ok := byTarget[wait.TargetID]
		if !ok || remaining < current.next {
			current.next = remaining
		}
		current.count++
		byTarget[wait.TargetID] = current
	}
	return byTarget
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
	// The scheduler model deliberately refuses to invent a service duration for
	// opaque probes. Once jobs have completed, however, the run itself supplies
	// an observable end-to-end throughput sample (including real pacing and
	// provider latency). It is a forecast, not a measured service-time claim,
	// so retain the lower bound and label the extrapolation as approximate.
	if forecast, ok := throughputForecast(snapshot); ok {
		if forecast < *result.RangeLow {
			forecast = *result.RangeLow
		}
		return "~" + eta.RoundDisplay(forecast).String()
	}
	if result.State == eta.LowerBound {
		return ">= " + eta.RoundDisplay(*result.RangeLow).String()
	}
	if result.Estimate != nil {
		return eta.RoundDisplay(*result.Estimate).String()
	}
	return "estimating"
}

func throughputForecast(snapshot execution.Snapshot) (time.Duration, bool) {
	completed := snapshot.FinishedJobs - snapshot.CancelledJobs
	remaining := snapshot.PlannedJobs - snapshot.FinishedJobs
	if completed < 2 || remaining <= 0 || snapshot.Run.Offset <= 0 {
		return 0, false
	}
	perJob := snapshot.Run.Offset / time.Duration(completed)
	if perJob <= 0 {
		return 0, false
	}
	return perJob * time.Duration(remaining), true
}

func (r *operatorReporter) draw(frame terminal.Frame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	previousLines := r.lines
	if previousLines > 0 {
		_, _ = fmt.Fprintf(r.writer, "\x1b[%dA", previousLines)
	}
	for _, line := range frame.Lines {
		_, _ = fmt.Fprintf(r.writer, "\r\x1b[2K%s\n", line)
	}
	stale := previousLines - len(frame.Lines)
	for i := 0; i < stale; i++ {
		_, _ = io.WriteString(r.writer, "\r\x1b[2K\n")
	}
	// Clearing rows below a shorter frame moves the cursor. Restore it to the
	// line immediately after the new frame; otherwise the next redraw begins
	// too low and leaves stale headers visibly stacked above it.
	if stale > 0 {
		_, _ = fmt.Fprintf(r.writer, "\x1b[%dA", stale)
	}
	r.lines = len(frame.Lines)
}

func (r *operatorReporter) Close() {
	if r == nil {
		return
	}
	if r.tty {
		r.stateMu.Lock()
		ticker, stop := r.refresh, r.stop
		r.refresh, r.stop = nil, nil
		r.stateMu.Unlock()
		if ticker != nil {
			ticker.Stop()
			close(stop)
			r.refreshWG.Wait()
		}
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

// requestRate is the completed physical request rate over this run so far.
// There is deliberately no HTTP total: retries and SDK work make it unknown
// until the run has completed.
func requestRate(snapshot execution.Snapshot) float64 {
	if snapshot.Run.Offset <= 0 {
		return 0
	}
	return float64(terminalRequests(snapshot)) / snapshot.Run.Offset.Seconds()
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
