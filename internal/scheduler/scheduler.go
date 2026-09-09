// Package scheduler provides the run-owned admission broker for physical HTTP
// attempts.  It deliberately has no transport dependency: callers hold a
// Lease until a response body, stream, or cancelled attempt is closed.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Clock supplies monotonic scheduling time.  Production callers can use
// RealClock; tests should inject a fake Clock instead of sleeping.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

// RealClock is the production clock implementation.
type RealClock struct{}

func (RealClock) Now() time.Time                         { return time.Now() }
func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// WaitReason is deliberately closed: it is safe to expose in execution
// events without coupling callers to implementation-specific strings.
type WaitReason string

const (
	WaitInitialPacing  WaitReason = "initial_pacing"
	WaitAdaptivePacing WaitReason = "adaptive_pacing"
	WaitRetryAfter     WaitReason = "retry_after"
	WaitRetryBackoff   WaitReason = "retry_backoff"
	WaitQuotaCapacity  WaitReason = "quota_capacity"
	WaitGlobalCapacity WaitReason = "global_capacity"
	WaitMemoryBudget   WaitReason = "memory_budget"
	WaitDependency     WaitReason = "dependency"
)

var (
	ErrInvalidRequest = errors.New("scheduler: invalid request")
	ErrTooLarge       = errors.New("scheduler: reservation exceeds broker capacity")
	// ErrArtifactBudgetExhausted is terminal: previously committed creations
	// cannot be refunded, so waiting cannot make this request admissible.
	ErrArtifactBudgetExhausted = errors.New("scheduler: artifact budget exhausted")
)

// Group configures shared pacing and a learned/conservative in-flight limit.
// A zero Capacity inherits the broker capacity; a zero Interval has no pacing.
type Group struct {
	Capacity int
	Interval time.Duration
}

// Config is immutable after New.  Group names are safe aliases, never origins.
type Config struct {
	Capacity       int
	MemoryBudget   int64
	ArtifactBudget int
	Groups         map[string]Group
	Clock          Clock
}

// Request describes one physical attempt.  Target and Group are stable safe
// identifiers. ArtifactKey is normally "target/model" and shares a finite
// creation budget between native and SDK work.
type Request struct {
	Target      string
	Group       string
	Weight      int
	MemoryBytes int64
	ArtifactKey string
	Artifacts   int
}

// Event describes a wait lifecycle; it contains no URLs, payloads or tokens.
type Event struct {
	Kind       string // wait_started | wait_ended
	Target     string
	Group      string
	Reason     WaitReason
	At         time.Time
	EligibleAt time.Time
}

// Wait is a live snapshot of a request that has not received a lease.
type Wait struct {
	Target     string
	Group      string
	Reason     WaitReason
	StartedAt  time.Time
	EligibleAt time.Time
}

// Snapshot is suitable for the execution-state reducer.  Waiting does not
// contribute to InFlight.
type Snapshot struct {
	Capacity       int
	InFlight       int
	MemoryBudget   int64
	MemoryReserved int64
	ArtifactBudget int
	Artifacts      map[string]int
	Waiting        []Wait
	Groups         map[string]GroupSnapshot
}

type GroupSnapshot struct {
	InFlight int
	Capacity int
	NextAt   time.Time
	Cooldown time.Time
}

type Broker struct {
	mu  sync.Mutex
	cfg Config

	inFlight    int
	memory      int64
	groups      map[string]*groupState
	artifacts   map[string]int
	committed   map[string]int
	queue       []*pending
	memoryQueue []*memoryPending
	events      []Event
	nextID      uint64
	rrTarget    string
	// changed is closed and replaced whenever queue eligibility or capacity
	// changes. Waiters select on it so a request that was previously waiting
	// only for capacity re-reads a newly installed cooldown deadline.
	changed chan struct{}
}

type groupState struct {
	Group
	inFlight         int
	nextAt, cooldown time.Time
	cooldownReason   WaitReason
}
type pending struct {
	id         uint64
	req        Request
	started    time.Time
	result     chan acquireResult
	waitReason WaitReason
	eligibleAt time.Time
	evented    bool
}
type acquireResult struct {
	lease *Lease
	err   error
}

type memoryPending struct {
	bytes  int64
	result chan memoryResult
}

type memoryResult struct {
	lease *MemoryLease
	err   error
}

// New validates all resource bounds before any request is submitted.
func New(cfg Config) (*Broker, error) {
	if cfg.Capacity < 1 {
		return nil, fmt.Errorf("%w: capacity must be positive", ErrInvalidRequest)
	}
	if cfg.MemoryBudget < 0 || cfg.ArtifactBudget < 0 {
		return nil, fmt.Errorf("%w: budgets must not be negative", ErrInvalidRequest)
	}
	if cfg.Clock == nil {
		cfg.Clock = RealClock{}
	}
	b := &Broker{cfg: cfg, groups: make(map[string]*groupState), artifacts: make(map[string]int), committed: make(map[string]int), changed: make(chan struct{})}
	for name, group := range cfg.Groups {
		if name == "" || group.Capacity < 0 || group.Interval < 0 {
			return nil, fmt.Errorf("%w: group %q", ErrInvalidRequest, name)
		}
		if group.Capacity == 0 {
			group.Capacity = cfg.Capacity
		}
		b.groups[name] = &groupState{Group: group}
	}
	return b, nil
}

// Acquire queues an attempt and returns only after all of its resources were
// granted atomically.  It never occupies a global slot while pacing or another
// resource blocks it.  Context cancellation removes it from the queue.
func (b *Broker) Acquire(ctx context.Context, req Request) (*Lease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	if err := ctx.Err(); err != nil {
		b.mu.Unlock()
		return nil, err
	}
	if err := b.validateLocked(req); err != nil {
		b.mu.Unlock()
		return nil, err
	}
	p := &pending{id: b.nextID, req: normalise(req), started: b.cfg.Clock.Now(), result: make(chan acquireResult, 1)}
	b.nextID++
	b.queue = append(b.queue, p)
	b.scheduleLocked()
	b.signalChangedLocked()
	b.mu.Unlock()

	for {
		b.mu.Lock()
		wait, hasWait := b.waitForLocked(p)
		changed := b.changed
		b.mu.Unlock()
		if !hasWait { // Result may have been delivered concurrently.
			select {
			case result := <-p.result:
				return result.lease, result.err
			default:
			}
		}
		var timer <-chan time.Time
		if !wait.IsZero() {
			d := wait.Sub(b.cfg.Clock.Now())
			if d < 0 {
				d = 0
			}
			timer = b.cfg.Clock.After(d)
		}
		select {
		case result := <-p.result:
			return result.lease, result.err
		case <-ctx.Done():
			b.mu.Lock()
			removed := b.removeLocked(p)
			if removed {
				if p.evented {
					b.events = append(b.events, Event{Kind: "wait_ended", Target: p.req.Target, Group: p.req.Group, Reason: p.waitReason, At: b.cfg.Clock.Now()})
				}
				b.scheduleLocked()
				b.signalChangedLocked()
			}
			b.mu.Unlock()
			if removed {
				return nil, ctx.Err()
			}
			result := <-p.result
			return result.lease, result.err
		case <-timer:
			b.mu.Lock()
			b.scheduleLocked()
			b.signalChangedLocked()
			b.mu.Unlock()
		case <-changed:
			// A release, cooldown or queue mutation may have changed the next
			// eligible deadline while this waiter had no timer. Re-evaluate it.
		}
	}
}

// ReserveMemory reserves bytes before a caller allocates or serializes a
// request body. It deliberately consumes only MemoryBudget: global and group
// capacity, pacing, and artifact admission do not participate. The returned
// lease must be released when the buffer is no longer retained.
func (b *Broker) ReserveMemory(ctx context.Context, bytes int64) (*MemoryLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if bytes < 0 {
		return nil, ErrInvalidRequest
	}
	b.mu.Lock()
	if err := ctx.Err(); err != nil {
		b.mu.Unlock()
		return nil, err
	}
	if b.cfg.MemoryBudget > 0 && bytes > b.cfg.MemoryBudget {
		b.mu.Unlock()
		return nil, ErrTooLarge
	}
	p := &memoryPending{bytes: bytes, result: make(chan memoryResult, 1)}
	b.memoryQueue = append(b.memoryQueue, p)
	b.scheduleLocked()
	b.signalChangedLocked()
	b.mu.Unlock()

	for {
		b.mu.Lock()
		changed := b.changed
		b.mu.Unlock()
		select {
		case result := <-p.result:
			return result.lease, result.err
		case <-ctx.Done():
			b.mu.Lock()
			removed := b.removeMemoryLocked(p)
			if removed {
				b.scheduleLocked()
				b.signalChangedLocked()
			}
			b.mu.Unlock()
			if removed {
				return nil, ctx.Err()
			}
			result := <-p.result
			return result.lease, result.err
		case <-changed:
			// A physical or memory lease may have released bytes. Re-evaluate
			// under the broker lock rather than reserving unrelated resources.
		}
	}
}

func normalise(r Request) Request {
	if r.Weight == 0 {
		r.Weight = 1
	}
	return r
}
func (b *Broker) validateLocked(r Request) error {
	r = normalise(r)
	if r.Target == "" || r.Group == "" || r.Weight < 1 || r.MemoryBytes < 0 || r.Artifacts < 0 {
		return ErrInvalidRequest
	}
	if r.Weight > b.cfg.Capacity || (b.cfg.MemoryBudget > 0 && r.MemoryBytes > b.cfg.MemoryBudget) || (r.Artifacts > 0 && b.cfg.ArtifactBudget == 0) {
		return ErrTooLarge
	}
	groupCapacity := b.cfg.Capacity
	if g := b.groups[r.Group]; g != nil {
		groupCapacity = g.Capacity
	}
	if r.Weight > groupCapacity {
		return ErrTooLarge
	}
	if r.Artifacts > 0 && b.committed[r.ArtifactKey]+r.Artifacts > b.cfg.ArtifactBudget {
		return ErrArtifactBudgetExhausted
	}
	return nil
}

func (b *Broker) groupLocked(name string) *groupState {
	if g := b.groups[name]; g != nil {
		return g
	}
	g := &groupState{Group: Group{Capacity: b.cfg.Capacity}}
	b.groups[name] = g
	return g
}

// ApplyCooldown prevents new starts in one pacing group only. source must be
// a pacing reason; it is published in snapshots/events of blocked requests.
func (b *Broker) ApplyCooldown(group string, until time.Time, source WaitReason) error {
	if group == "" || (source != WaitRetryAfter && source != WaitRetryBackoff && source != WaitAdaptivePacing) {
		return ErrInvalidRequest
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	g := b.groupLocked(group)
	if until.After(g.cooldown) {
		g.cooldown, g.cooldownReason = until, source
	}
	b.scheduleLocked()
	b.signalChangedLocked()
	return nil
}

func (b *Broker) signalChangedLocked() {
	close(b.changed)
	b.changed = make(chan struct{})
}

func (b *Broker) scheduleLocked() {
	for {
		p := b.pickLocked()
		if p != nil {
			b.grantLocked(p)
			continue
		}
		pMemory := b.pickMemoryLocked()
		if pMemory == nil {
			return
		}
		b.grantMemoryLocked(pMemory)
	}
}

func (b *Broker) pickMemoryLocked() *memoryPending {
	for _, p := range b.memoryQueue {
		if b.cfg.MemoryBudget == 0 || b.memory+p.bytes <= b.cfg.MemoryBudget {
			return p
		}
	}
	return nil
}

func (b *Broker) grantMemoryLocked(p *memoryPending) {
	b.memory += p.bytes
	b.removeMemoryLocked(p)
	p.result <- memoryResult{lease: &MemoryLease{broker: b, bytes: p.bytes}}
}

func (b *Broker) pickLocked() *pending {
	if len(b.queue) == 0 {
		return nil
	}
	now := b.cfg.Clock.Now()
	targets := make([]string, 0, len(b.queue))
	seen := map[string]bool{}
	for _, p := range b.queue {
		if !seen[p.req.Target] {
			seen[p.req.Target] = true
			targets = append(targets, p.req.Target)
		}
	}
	sort.Strings(targets)
	start := 0
	for i, t := range targets {
		if t > b.rrTarget {
			start = i
			break
		}
		if i == len(targets)-1 {
			start = 0
		}
	}
	for offset := range targets {
		target := targets[(start+offset)%len(targets)]
		for _, p := range b.queue { // stable FIFO within a target
			if p.req.Target != target {
				continue
			}
			reason, eligible, ok := b.admissibleLocked(p, now)
			if !ok {
				b.setWaitLocked(p, reason, eligible)
				break
			}
			b.rrTarget = target
			return p
		}
	}
	return nil
}

func (b *Broker) admissibleLocked(p *pending, now time.Time) (WaitReason, time.Time, bool) {
	g := b.groupLocked(p.req.Group)
	eligible, pacingReason := g.nextAt, WaitInitialPacing
	if !g.cooldown.IsZero() && !g.cooldown.Before(eligible) {
		eligible, pacingReason = g.cooldown, g.cooldownReason
	}
	if eligible.After(now) {
		return pacingReason, eligible, false
	}
	if b.inFlight+p.req.Weight > b.cfg.Capacity {
		return WaitGlobalCapacity, time.Time{}, false
	}
	if g.inFlight+p.req.Weight > g.Capacity {
		return WaitQuotaCapacity, time.Time{}, false
	}
	if b.cfg.MemoryBudget > 0 && b.memory+p.req.MemoryBytes > b.cfg.MemoryBudget {
		return WaitMemoryBudget, time.Time{}, false
	}
	if p.req.Artifacts > 0 && b.artifacts[p.req.ArtifactKey]+p.req.Artifacts > b.cfg.ArtifactBudget {
		// Artifact creation is a quota-like bounded resource.  The public wait
		// enum intentionally remains the versioned set from the execution
		// contract, so it is represented as quota_capacity rather than a new
		// ad-hoc reason.
		return WaitQuotaCapacity, time.Time{}, false
	}
	return "", time.Time{}, true
}

func (b *Broker) setWaitLocked(p *pending, reason WaitReason, eligible time.Time) {
	if p.waitReason == reason && p.eligibleAt.Equal(eligible) {
		return
	}
	p.waitReason, p.eligibleAt = reason, eligible
	if !p.evented {
		b.events = append(b.events, Event{Kind: "wait_started", Target: p.req.Target, Group: p.req.Group, Reason: reason, At: b.cfg.Clock.Now(), EligibleAt: eligible})
		p.evented = true
	}
}

func (b *Broker) grantLocked(p *pending) {
	g := b.groupLocked(p.req.Group)
	b.inFlight += p.req.Weight
	b.memory += p.req.MemoryBytes
	g.inFlight += p.req.Weight
	if g.Interval > 0 {
		g.nextAt = b.cfg.Clock.Now().Add(g.Interval)
	}
	if p.req.Artifacts > 0 {
		b.artifacts[p.req.ArtifactKey] += p.req.Artifacts
	}
	b.removeLocked(p)
	if p.evented {
		b.events = append(b.events, Event{Kind: "wait_ended", Target: p.req.Target, Group: p.req.Group, Reason: p.waitReason, At: b.cfg.Clock.Now()})
	}
	p.result <- acquireResult{lease: &Lease{broker: b, req: p.req}}
}

func (b *Broker) removeLocked(p *pending) bool {
	for i, q := range b.queue {
		if q == p {
			b.queue = append(b.queue[:i], b.queue[i+1:]...)
			return true
		}
	}
	return false
}

func (b *Broker) removeMemoryLocked(p *memoryPending) bool {
	for i, q := range b.memoryQueue {
		if q == p {
			b.memoryQueue = append(b.memoryQueue[:i], b.memoryQueue[i+1:]...)
			return true
		}
	}
	return false
}

func (b *Broker) waitForLocked(p *pending) (time.Time, bool) {
	for _, q := range b.queue {
		if q == p {
			return q.eligibleAt, true
		}
	}
	return time.Time{}, false
}

// Lease holds all resources granted to one physical attempt.
type Lease struct {
	broker *Broker
	req    Request
	once   sync.Once
}

// MemoryLease holds only a MemoryBudget reservation. Release is idempotent.
type MemoryLease struct {
	broker *Broker
	bytes  int64
	once   sync.Once
}

func (l *MemoryLease) Release() {
	l.once.Do(func() {
		b := l.broker
		b.mu.Lock()
		b.memory -= l.bytes
		b.scheduleLocked()
		b.signalChangedLocked()
		b.mu.Unlock()
	})
}

// Release is idempotent and must be called after body EOF/Close or cancellation.
// An artifact-bearing lease is thereafter treated as committed: only
// CancelBeforeDispatch can prove that no remote creation was attempted.
func (l *Lease) Release() { l.release(false) }

// CancelBeforeDispatch releases the lease and refunds an artifact reservation.
// It is only valid before a remote creation could have been initiated.
func (l *Lease) CancelBeforeDispatch() { l.release(true) }
func (l *Lease) release(refundArtifacts bool) {
	l.once.Do(func() {
		b := l.broker
		b.mu.Lock()
		b.inFlight -= l.req.Weight
		b.memory -= l.req.MemoryBytes
		b.groupLocked(l.req.Group).inFlight -= l.req.Weight
		if refundArtifacts && l.req.Artifacts > 0 {
			b.artifacts[l.req.ArtifactKey] -= l.req.Artifacts
			if b.artifacts[l.req.ArtifactKey] == 0 {
				delete(b.artifacts, l.req.ArtifactKey)
			}
		} else if l.req.Artifacts > 0 {
			b.committed[l.req.ArtifactKey] += l.req.Artifacts
		}
		b.scheduleLocked()
		b.signalChangedLocked()
		b.mu.Unlock()
	})
}

// Snapshot returns a copy, ordered deterministically for renderers and tests.
func (b *Broker) Snapshot() Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := Snapshot{Capacity: b.cfg.Capacity, InFlight: b.inFlight, MemoryBudget: b.cfg.MemoryBudget, MemoryReserved: b.memory, ArtifactBudget: b.cfg.ArtifactBudget, Artifacts: make(map[string]int), Groups: make(map[string]GroupSnapshot)}
	for k, v := range b.artifacts {
		s.Artifacts[k] = v
	}
	for k, g := range b.groups {
		s.Groups[k] = GroupSnapshot{InFlight: g.inFlight, Capacity: g.Capacity, NextAt: g.nextAt, Cooldown: g.cooldown}
	}
	for _, p := range b.queue {
		s.Waiting = append(s.Waiting, Wait{Target: p.req.Target, Group: p.req.Group, Reason: p.waitReason, StartedAt: p.started, EligibleAt: p.eligibleAt})
	}
	sort.Slice(s.Waiting, func(i, j int) bool {
		if s.Waiting[i].Target != s.Waiting[j].Target {
			return s.Waiting[i].Target < s.Waiting[j].Target
		}
		return s.Waiting[i].Group < s.Waiting[j].Group
	})
	return s
}

// Events returns a copy of wait lifecycle events in their deterministic
// broker order.  A later typed execution bus can consume these without a
// callback under the scheduler lock.
func (b *Broker) Events() []Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]Event(nil), b.events...)
}
