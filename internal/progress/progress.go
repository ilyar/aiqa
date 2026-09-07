// Package progress renders the operator-facing execution state. It is kept
// separate from private JSONL logging: progress is always visible, while
// forensic diagnostics remain controlled by --verbose.
package progress

import (
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/ilyar/aiqa/internal/telemetry"
)

const defaultHeartbeat = 30 * time.Second

// Sink is the protocol-neutral progress contract. Native runners publish a
// plan after discovery; telemetry supplies terminal probe lifecycle events.
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

// Options makes rendering deterministic in tests without a terminal package.
type Options struct {
	Writer            io.Writer
	TTY               bool
	Now               func() time.Time
	HeartbeatInterval time.Duration
}

// Reporter is a concurrency-safe Sink implementation.
type Reporter struct {
	writer    io.Writer
	tty       bool
	now       func() time.Time
	heartbeat time.Duration
	mu        sync.Mutex
	targets   map[string]*targetState
	current   string
	lastLine  bool
	closed    bool
	stop      chan struct{}
	done      chan struct{}
}

type targetState struct {
	name                             string
	total, done                      int
	planned                          bool
	completed                        map[string]struct{}
	started, plannedAt, lastTerminal time.Time
	lastCheckpoint                   time.Time
	lastPercent                      int
	intervalEMA                      time.Duration
	terminals                        int
	finished                         bool
	pacingWaiters                    int
	wait                             time.Duration
	current                          string
}

// New constructs a reporter. TTY selects a single redrawable stderr line;
// false selects durable Unix-safe lines without ANSI sequences.
func New(options Options) *Reporter {
	if options.Writer == nil {
		options.Writer = io.Discard
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.HeartbeatInterval <= 0 {
		options.HeartbeatInterval = defaultHeartbeat
	}
	r := &Reporter{writer: options.Writer, tty: options.TTY, now: options.Now, heartbeat: options.HeartbeatInterval, targets: map[string]*targetState{}, stop: make(chan struct{}), done: make(chan struct{})}
	go r.heartbeatLoop()
	return r
}

func (r *Reporter) RunStarted(targets int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lineLocked(fmt.Sprintf("aiqa: Planning %d target(s)", max(0, targets)), !r.tty)
}
func (r *Reporter) DiscoveryStarted(target string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.stateLocked(target)
	s.started = r.now()
	r.current = s.name
	r.renderLocked(s, "Discovering "+s.name)
}

func (r *Reporter) Plan(target string, total int)       { r.setPlan(target, total, false) }
func (r *Reporter) RevisePlan(target string, total int) { r.setPlan(target, total, true) }

// AddPlan extends a target-owned known plan without requiring callers such as
// extension runners to know the native runner's current total.
func (r *Reporter) AddPlan(target string, delta int) {
	if delta <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.stateLocked(target)
	old := s.total
	s.total += delta
	if s.total < s.done {
		s.total = s.done
	}
	s.planned, s.plannedAt = true, r.now()
	r.current = s.name
	r.lineLocked(fmt.Sprintf("aiqa: %s: plan revised: %d -> %d probes", s.name, old, s.total), true)
	r.renderLocked(s, "")
}
func (r *Reporter) setPlan(target string, total int, revised bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.stateLocked(target)
	if total < s.done {
		total = s.done
	}
	old := s.total
	s.total, s.planned, s.plannedAt = total, true, r.now()
	r.current = s.name
	if revised {
		r.lineLocked(fmt.Sprintf("aiqa: %s: plan revised: %d -> %d probes", s.name, old, total), true)
	} else if !r.tty {
		r.lineLocked(fmt.Sprintf("aiqa: %s: planned %d probes", s.name, total), true)
	}
	r.renderLocked(s, "")
}

func (r *Reporter) Pacing(target string, waiting bool, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.stateLocked(target)
	r.current = s.name
	wasWaiting := s.pacingWaiters > 0
	if waiting {
		s.pacingWaiters++
		if duration > s.wait {
			s.wait = duration
		}
	} else if s.pacingWaiters > 0 {
		s.pacingWaiters--
	}
	isWaiting := s.pacingWaiters > 0
	if wasWaiting == isWaiting {
		return
	}
	if !isWaiting {
		s.wait = 0
	}
	// A target can have many requests queued in the shared pacer. Render only
	// the aggregate transition: per-request begin/end events otherwise make a
	// TTY flicker and flood durable output.
	r.pacingTransitionLocked(s)
}
func (r *Reporter) TargetFinished(target, outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.stateLocked(target)
	s.pacingWaiters = 0
	s.wait = 0
	s.finished = true
	r.finishLineLocked()
	r.lineLocked(fmt.Sprintf("aiqa: %s: completed — %s (%d/%d 100%%)", s.name, safe(outcome), s.done, s.total), true)
}
func (r *Reporter) RunFinished() { r.mu.Lock(); defer r.mu.Unlock(); r.finishLineLocked() }

// Message writes a durable operator message after finalizing any active TTY
// redraw. Callers use fixed safe text only.
func (r *Reporter) Message(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishLineLocked()
	r.lineLocked("aiqa: "+safe(text), true)
}

// ObserveLifecycle translates only typed lifecycle operations. Attempts,
// validators and slots never affect completion. Duplicate terminal events are
// ignored by operation ID, preserving done <= total.
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
		r.mu.Lock()
		defer r.mu.Unlock()
		s := r.stateLocked(event.Target)
		s.current = progressLabel(event.Model, event.Probe)
		r.current = s.name
		if s.planned {
			r.renderLocked(s, "")
		}
		return
	}
	if (event.Name != "probe" && event.Name != "extension.suite") || event.Phase != "end" || event.OperationID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.targets[safe(event.Target)]
	if !ok || !s.planned {
		return
	}
	if _, seen := s.completed[event.OperationID]; seen {
		return
	}
	s.completed[event.OperationID] = struct{}{}
	if s.done < s.total {
		s.done++
	}
	now := event.Time
	if now.IsZero() {
		now = r.now()
	}
	if s.terminals > 0 && !s.lastTerminal.IsZero() {
		gap := now.Sub(s.lastTerminal)
		if gap > 0 {
			if s.intervalEMA == 0 {
				s.intervalEMA = gap
			} else {
				s.intervalEMA = time.Duration(.25*float64(gap) + .75*float64(s.intervalEMA))
			}
		}
	}
	s.terminals++
	s.lastTerminal = now
	r.renderLocked(s, "")
}

func (r *Reporter) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	close(r.stop)
	r.finishLineLocked()
	r.mu.Unlock()
	<-r.done
}
func (r *Reporter) heartbeatLoop() {
	ticker := time.NewTicker(r.heartbeat)
	defer ticker.Stop()
	defer close(r.done)
	for {
		select {
		case <-r.stop:
			return
		case <-ticker.C:
			r.Heartbeat()
		}
	}
}

// Heartbeat is public to let a deterministic owner drive it in tests.
func (r *Reporter) Heartbeat() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.current == "" {
		return
	}
	s := r.targets[r.current]
	if s == nil {
		return
	}
	if r.tty {
		r.renderLocked(s, "")
		return
	}
	now := r.now()
	if !s.lastCheckpoint.IsZero() && now.Sub(s.lastCheckpoint) < r.heartbeat {
		return
	}
	r.progressLineLocked(s)
	s.lastCheckpoint = now
}

func (r *Reporter) stateLocked(target string) *targetState {
	name := safe(target)
	if name == "" {
		name = "target"
	}
	if s := r.targets[name]; s != nil {
		return s
	}
	s := &targetState{name: name, completed: map[string]struct{}{}, lastPercent: -1}
	r.targets[name] = s
	return s
}
func (r *Reporter) renderLocked(s *targetState, override string) {
	if override != "" {
		r.lineLocked("aiqa: "+override, !r.tty)
		return
	}
	if !s.planned {
		r.lineLocked("aiqa: Discovering "+s.name, !r.tty)
		return
	}
	if r.tty {
		r.lineLocked(r.status(s), false)
		return
	}
	percent := percentOf(s.done, s.total)
	if percent/10 != s.lastPercent/10 || s.done == s.total {
		r.progressLineLocked(s)
		s.lastPercent = percent
		s.lastCheckpoint = r.now()
	}
}
func (r *Reporter) progressLineLocked(s *targetState) { r.lineLocked(r.status(s), true) }
func (r *Reporter) pacingTransitionLocked(s *targetState) {
	r.lineLocked(r.status(s), !r.tty)
}
func (r *Reporter) status(s *targetState) string {
	if !s.planned {
		return r.withPacing(s, "aiqa: Discovering "+s.name)
	}
	// Native classification can reveal a later SDK/extension phase. Until the
	// target itself ends, a completed current plan is not a completed target;
	// calling it 100% would be a false promise to the operator.
	if s.done >= s.total && !s.finished {
		return r.withPacing(s, fmt.Sprintf("aiqa: %s %d/%d known probes completed; preparing next phase", s.name, s.done, s.total))
	}
	p := percentOf(s.done, s.total)
	rate, eta := r.rateETA(s)
	text := fmt.Sprintf("aiqa: %s %d/%d %d%%", s.name, s.done, s.total, p)
	if r.tty {
		text = fmt.Sprintf("aiqa: %s [%s] %d/%d %d%%", s.name, bar(p), s.done, s.total, p)
	}
	if rate > 0 {
		text += fmt.Sprintf("  %.1f probe/s", rate)
	}
	text += "  ETA " + eta
	if s.current != "" {
		text += "  " + s.current
	}
	return r.withPacing(s, text)
}
func (r *Reporter) withPacing(s *targetState, text string) string {
	if s.pacingWaiters == 0 {
		return text
	}
	if s.wait >= time.Second {
		return fmt.Sprintf("%s  waiting for provider pacing (%s)", text, s.wait.Round(time.Second))
	}
	return text + "  waiting for provider pacing"
}
func (r *Reporter) rateETA(s *targetState) (float64, string) {
	if !s.planned || s.total <= s.done || s.terminals < 2 || s.intervalEMA <= 0 {
		return 0, "estimating"
	}
	rate := 1 / s.intervalEMA.Seconds()
	if math.IsInf(rate, 0) || math.IsNaN(rate) || rate <= 0 {
		return 0, "unknown"
	}
	return rate, (time.Duration(float64(s.total-s.done) * float64(s.intervalEMA))).Round(time.Second).String()
}
func (r *Reporter) lineLocked(text string, newline bool) {
	if r.closed {
		return
	}
	if r.tty {
		fmt.Fprint(r.writer, "\r"+text)
		r.lastLine = true
		if newline {
			fmt.Fprint(r.writer, "\n")
			r.lastLine = false
		}
		return
	}
	fmt.Fprintln(r.writer, text)
}
func (r *Reporter) finishLineLocked() {
	if r.tty && r.lastLine {
		fmt.Fprint(r.writer, "\n")
		r.lastLine = false
	}
}
func percentOf(done, total int) int {
	if total <= 0 {
		return 0
	}
	if done < 0 {
		return 0
	}
	if done >= total {
		return 100
	}
	return done * 100 / total
}
func bar(percent int) string {
	const width = 20
	filled := percent * width / 100
	return strings.Repeat("#", filled) + strings.Repeat("-", width-filled)
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
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
