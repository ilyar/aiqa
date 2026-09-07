// Package progress renders operator-facing execution state. It is separate
// from private JSONL logging: progress is always visible, while --verbose
// controls forensic diagnostics only.
package progress

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	progressbar "github.com/schollz/progressbar/v3"

	"github.com/ilyar/aiqa/internal/telemetry"
)

const durablePacingNotice = 5 * time.Second

// Job is one report-visible logical probe outcome. It remains one job even if
// its implementation makes multiple HTTP requests. A conditional probe is
// reserved here and is completed with outcome "skipped" when inapplicable.
type Job struct {
	ID     string
	Target string
	Model  string
	Label  string
}

// Sink is the backward-compatible protocol-neutral progress contract.
//
// New callers that implement the two-phase plan use FrozenSink: before
// execution they publish planning status and then Freeze the full global job
// list. Freeze creates exactly one immutable total. Execute calls Start and
// Complete for the reserved ID; Complete is idempotent and treats all terminal
// outcomes, including skipped, equally. This makes 100% and ETA honest.
//
// Plan, RevisePlan and AddPlan are temporary source-compatibility helpers for
// runners not yet split into Prepare/Execute. They collect no rendered total;
// after Freeze they are ignored and cannot revise the published plan.
type Sink interface {
	telemetry.Observer
	RunStarted(targets int)
	DiscoveryStarted(target string)
	Plan(target string, total int)
	RevisePlan(target string, total int)
	AddPlan(target string, delta int)
	Pacing(target string, waiting bool, duration time.Duration)
	TargetFinished(target, outcome string)
	RunFinished()
	Message(text string)
	Close()
}

// FrozenSink is the execution-capable extension of the legacy Sink. Keep it
// separate while protocol runners migrate: existing embedders need not grow
// no-op methods merely to receive basic operator messages.
type FrozenSink interface {
	Sink
	Planning(status string)
	Freeze(jobs []Job)
	Start(id string)
	Complete(id, outcome string)
}

// Options makes output deterministic in tests. TTY selects the one-line
// progressbar/v3 renderer; false emits durable text without terminal controls.
type Options struct {
	Writer          io.Writer
	TTY             bool
	SpinnerInterval time.Duration
}

// Reporter serializes all calls, so workers can complete jobs concurrently.
// Exactly one progressbar/v3 determinate bar is created after Freeze.
type Reporter struct {
	writer io.Writer
	tty    bool

	mu sync.Mutex

	closed        bool
	frozen        bool
	spinner       *progressbar.ProgressBar
	spinnerStop   chan struct{}
	bar           *progressbar.ProgressBar
	barTerminated bool
	jobs          map[string]Job
	completed     map[string]struct{}
	legacyPlans   map[string]int
	total, done   int
	current       string
	pacing        map[string]pacingState
}

type pacingState struct {
	waiters int
	wait    time.Duration
	noticed bool
}

// New constructs a Reporter.
func New(options Options) *Reporter {
	if options.Writer == nil {
		options.Writer = io.Discard
	}
	r := &Reporter{
		writer: options.Writer, tty: options.TTY,
		jobs: map[string]Job{}, completed: map[string]struct{}{},
		legacyPlans: map[string]int{}, pacing: map[string]pacingState{},
	}
	if r.tty {
		interval := options.SpinnerInterval
		if interval == 0 {
			interval = 100 * time.Millisecond
		}
		r.spinner = progressbar.NewOptions(-1,
			progressbar.OptionSetWriter(r.writer),
			progressbar.OptionSetDescription("aiqa: Planning"),
			progressbar.OptionSpinnerType(14),
			// progressbar enables its own 100ms spinner ticker by default. It
			// cannot be stopped with Exit, so disable it in favour of spin.
			progressbar.OptionSetSpinnerChangeInterval(0),
			progressbar.OptionUseANSICodes(false),
		)
		r.spinnerStop = make(chan struct{})
		go r.spin(interval, r.spinnerStop)
	}
	return r
}

// spin is deliberately owned by Reporter. progressbar/v3's optional internal
// spinner ticker has no stop operation: Exit only prevents Add and leaves the
// goroutine redrawing after Freeze. Owning this ticker prevents a planning
// spinner from overwriting the determinate bar.
func (r *Reporter) spin(interval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			r.mu.Lock()
			if !r.closed && r.spinner != nil {
				_ = r.spinner.Add(1)
			}
			r.mu.Unlock()
		}
	}
}

func (r *Reporter) RunStarted(targets int) {
	r.Planning(fmt.Sprintf("Planning %d target(s)", max(0, targets)))
}

// Planning updates the spinner while targets/models/jobs are discovered.
func (r *Reporter) Planning(status string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.frozen {
		return
	}
	text := "aiqa: " + safe(status)
	if r.tty {
		r.spinner.Describe(text)
		return
	}
	r.durableLocked(text)
}

func (r *Reporter) DiscoveryStarted(target string) {
	r.Planning("Discovering " + safeTarget(target))
}

// Plan records provisional inventory only; it never changes an active bar.
func (r *Reporter) Plan(target string, total int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.frozen {
		return
	}
	key := safeTarget(target)
	r.legacyPlans[key] = max(0, total)
	if !r.tty {
		r.durableLocked(fmt.Sprintf("aiqa: %s: planned %d logical probe(s)", key, r.legacyPlans[key]))
	}
}

// RevisePlan is retained only until runners publish their whole job list.
func (r *Reporter) RevisePlan(target string, total int) { r.Plan(target, total) }

// AddPlan is retained only until runners publish their whole job list.
func (r *Reporter) AddPlan(target string, delta int) {
	if delta <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.frozen {
		return
	}
	key := safeTarget(target)
	r.legacyPlans[key] += delta
}

// Freeze publishes the global plan once. Invalid or duplicate IDs are omitted:
// a job cannot be completed reliably without a unique identity.
func (r *Reporter) Freeze(jobs []Job) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.frozen {
		return
	}
	for _, job := range jobs {
		job.ID, job.Target, job.Model, job.Label = safe(job.ID), safeTarget(job.Target), safe(job.Model), safe(job.Label)
		if job.ID == "" {
			continue
		}
		if job.Label == "" {
			job.Label = progressLabel(job.Model, "")
		}
		if _, exists := r.jobs[job.ID]; exists {
			continue
		}
		r.jobs[job.ID] = job
	}
	r.frozen, r.total = true, len(r.jobs)
	r.stopSpinnerLocked()
	if !r.tty {
		r.durableLocked(fmt.Sprintf("aiqa: Planned %d logical probe(s)", r.total))
		return
	}
	if r.total == 0 {
		return
	}
	r.bar = progressbar.NewOptions(r.total,
		progressbar.OptionSetWriter(r.writer),
		progressbar.OptionSetDescription("aiqa: Running"),
		progressbar.OptionSetWidth(20),
		progressbar.OptionSetTheme(progressbar.ThemeASCII),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionThrottle(100*time.Millisecond),
		progressbar.OptionUseANSICodes(false),
	)
	_ = r.bar.RenderBlank()
}

// Start updates the current label for a reserved job. Unknown IDs are ignored.
func (r *Reporter) Start(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || !r.frozen {
		return
	}
	job, ok := r.jobs[id]
	if !ok {
		return
	}
	r.current = job.Label
	r.renderLocked()
}

// Complete closes a reserved job exactly once. outcome is intentionally not
// displayed: pass/fail/skipped belongs to report assessment, not the progress.
func (r *Reporter) Complete(id, outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || !r.frozen {
		return
	}
	if _, exists := r.jobs[id]; !exists {
		return
	}
	if _, seen := r.completed[id]; seen {
		return
	}
	r.completed[id] = struct{}{}
	if r.done >= r.total {
		return
	}
	r.done++
	if r.bar != nil {
		_ = r.bar.Add(1)
	}
	r.renderLocked()
	if r.done == r.total {
		r.finishBarLocked()
	}
	if !r.tty {
		r.durableCheckpointLocked()
	}
}

func (r *Reporter) Pacing(target string, waiting bool, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || !r.frozen {
		return
	}
	key := safeTarget(target)
	state := r.pacing[key]
	if waiting {
		state.waiters++
		if duration > state.wait {
			state.wait = duration
		}
	} else if state.waiters > 0 {
		state.waiters--
		if state.waiters == 0 {
			state.wait = 0
			state.noticed = false
		}
	}
	r.pacing[key] = state
	if state.waiters == 0 {
		r.renderLocked()
		return
	}
	r.current = key + ": waiting for provider pacing"
	if state.wait > 0 {
		r.current += " (" + state.wait.Round(time.Second).String() + ")"
	}
	r.renderLocked()
	if !r.tty && state.wait >= durablePacingNotice && !state.noticed {
		r.durableLocked("aiqa: " + r.current)
		state.noticed = true
		r.pacing[key] = state
	}
}

func (r *Reporter) TargetFinished(target, outcome string) {
	r.Message(fmt.Sprintf("%s: completed — %s", safeTarget(target), safe(outcome)))
}

func (r *Reporter) RunFinished() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.stopSpinnerLocked()
	r.finishBarLocked()
}

// Message prints a durable status after clearing the current redraw.
func (r *Reporter) Message(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.clearBarLocked()
	r.durableLocked("aiqa: " + safe(text))
	r.renderLocked()
}

// ObserveLifecycle retains telemetry compatibility until all runners call the
// explicit API. It only has an effect for IDs reserved by Freeze.
func (r *Reporter) ObserveLifecycle(event telemetry.LifecycleEvent) {
	if event.Name == "target" && event.Phase == "start" {
		r.DiscoveryStarted(event.Target)
		return
	}
	if event.Name == "target" && event.Phase == "end" {
		r.TargetFinished(event.Target, event.Outcome)
		return
	}
	if event.Name == "probe" && event.Phase == "start" {
		r.Start(event.OperationID)
		return
	}
	if (event.Name == "probe" || event.Name == "extension.suite") && event.Phase == "end" {
		r.Complete(event.OperationID, event.Outcome)
	}
}

func (r *Reporter) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	r.stopSpinnerLocked()
	r.finishBarLocked()
}

func (r *Reporter) renderLocked() {
	if r.bar == nil || r.current == "" || r.bar.IsFinished() {
		return
	}
	r.bar.Describe("aiqa: " + r.current)
}

func (r *Reporter) stopSpinnerLocked() {
	if r.spinner == nil {
		return
	}
	if r.spinnerStop != nil {
		close(r.spinnerStop)
		r.spinnerStop = nil
	}
	_ = r.spinner.Exit()
	_ = r.spinner.Clear()
	r.spinner = nil
}

func (r *Reporter) clearBarLocked() {
	if r.bar != nil && !r.bar.IsFinished() {
		_ = r.bar.Clear()
	}
}

func (r *Reporter) finishBarLocked() {
	if r.bar == nil || r.barTerminated {
		return
	}
	if !r.bar.IsFinished() {
		_ = r.bar.Exit()
	}
	fmt.Fprintln(r.writer)
	r.barTerminated = true
}

func (r *Reporter) durableCheckpointLocked() {
	if r.total == 0 || r.done == r.total || r.done == 1 || r.done*10/r.total != (r.done-1)*10/r.total {
		r.durableLocked(fmt.Sprintf("aiqa: %d/%d %d%%  %s", r.done, r.total, percentOf(r.done, r.total), r.current))
	}
}

func (r *Reporter) durableLocked(text string) { fmt.Fprintln(r.writer, text) }

func safeTarget(value string) string {
	if value = safe(value); value != "" {
		return value
	}
	return "target"
}

func safe(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	if len(value) > 96 {
		return value[:96]
	}
	return value
}

func progressLabel(model, probe string) string {
	model, probe = safe(model), safe(probe)
	if model == "" {
		return probe
	}
	if probe == "" {
		return model
	}
	return model + ":" + probe
}

func percentOf(done, total int) int {
	if total <= 0 || done <= 0 {
		return 0
	}
	if done >= total {
		return 100
	}
	return done * 100 / total
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
