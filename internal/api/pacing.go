package api

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// PacingStats describes observed client-side pacing, not a provider guarantee.
type PacingStats struct {
	LogicalRequests    int           `json:"logical_requests"`
	PhysicalAttempts   int           `json:"physical_attempts"`
	SuccessfulRequests int           `json:"successful_requests"`
	RateLimited        int           `json:"rate_limited_responses"`
	RetryAttempts      int           `json:"retry_attempts"`
	RetriesRecovered   int           `json:"retries_recovered"`
	RetriesExhausted   int           `json:"retries_exhausted"`
	Backoff            time.Duration `json:"backoff_duration"`
	SafeRate           float64       `json:"observed_safe_requests_per_second"`
	SafeConcurrency    int           `json:"observed_safe_concurrency"`
	LatencySamples     []int64       `json:"latency_samples_ms"`
}

type pacer struct {
	mu                    sync.Mutex
	interval              time.Duration
	nextStart             time.Time
	revision              uint64
	blockedUntil          time.Time
	successStreak         int
	quietWindows          int
	inflight              int
	limit                 int
	maxLimit              int
	maxSuccessfulInFlight int
	inflightByModel       map[string]int
	changed               chan struct{}
	scheduleChanged       chan struct{}
	stats                 PacingStats
	byModel               map[string]PacingStats
}

func newPacer(initial time.Duration) *pacer {
	if initial <= 0 {
		initial = 2 * time.Second
	}
	return &pacer{interval: initial, limit: 1, maxLimit: 4, changed: make(chan struct{}), scheduleChanged: make(chan struct{}), inflightByModel: map[string]int{}, byModel: map[string]PacingStats{}}
}

func (p *pacer) setConcurrency(limit int) {
	if limit < 1 {
		limit = 1
	}
	p.mu.Lock()
	p.maxLimit = limit
	// --concurrency is a ceiling for safely learned in-flight work, not an
	// instruction to burst that many requests at an unknown provider quota.
	// The pacer starts at one and grows additively after quiet successes.
	if p.limit > p.maxLimit {
		p.limit = p.maxLimit
	}
	p.revision++
	p.signalScheduleChangedLocked()
	p.mu.Unlock()
}

// admissionPolicy snapshots the conservative initial policy for the shared
// run-owned scheduler before a client is bound to it.
func (p *pacer) admissionPolicy() (time.Duration, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.interval, p.maxLimit
}

func (p *pacer) wait(ctx context.Context, model string, observers ...func(bool, time.Duration)) error {
	var observe func(bool, time.Duration)
	if len(observers) > 0 {
		observe = observers[0]
	}
	// A reservation can be revised and then wait for capacity before it is
	// admitted. Those are internal scheduler phases of one request, not separate
	// operator-visible waits. Emit one balanced lifecycle pair for the complete
	// admission wait; otherwise progress flickers 0 -> 1 -> 0 between phases.
	waiting := false
	var waitStarted time.Time
	beginWait := func(estimate time.Duration) {
		if waiting {
			return
		}
		waiting, waitStarted = true, time.Now()
		if observe != nil {
			observe(true, estimate)
		}
	}
	defer func() {
		if waiting && observe != nil {
			observe(false, time.Since(waitStarted))
		}
	}()
	for {
		p.mu.Lock()
		now := time.Now()
		start := p.nextStart
		if start.Before(now) {
			start = now
		}
		if start.Before(p.blockedUntil) {
			start = p.blockedUntil
		}
		p.nextStart = start.Add(p.interval)
		revision := p.revision
		scheduleChanged := p.scheduleChanged
		p.mu.Unlock()
		if delay := time.Until(start); delay > 0 {
			beginWait(delay)
			rescheduled, err := waitForPacer(ctx, delay, scheduleChanged)
			if err != nil {
				return err
			}
			// A rate-limit response changes the shared target schedule. Do not
			// keep consuming the old reservation while other work has already
			// entered cooldown; that was the source of queue-induced deadlines.
			if rescheduled {
				continue
			}
		}
		p.mu.Lock()
		// A 429 or adaptive adjustment invalidates reservations made under the
		// old interval. Requeue instead of releasing a stale burst afterwards.
		if revision != p.revision || time.Now().Before(p.blockedUntil) {
			p.mu.Unlock()
			continue
		}
		if p.inflight < p.limit {
			p.inflight++
			p.stats.PhysicalAttempts++
			if model != "" {
				s := p.byModel[model]
				s.PhysicalAttempts++
				p.byModel[model] = s
			}
			if model != "" {
				p.inflightByModel[model]++
			}
			p.mu.Unlock()
			return nil
		}
		changed := p.changed
		p.mu.Unlock()
		beginWait(0)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

// waitForPacer stops a stale reservation as soon as another request updates
// the target-wide schedule. The caller re-reserves against the new cooldown;
// it never sends a request merely because an obsolete timer expired.
func waitForPacer(ctx context.Context, delay time.Duration, changed <-chan struct{}) (bool, error) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-changed:
		return true, nil
	case <-timer.C:
		return false, nil
	}
}

func waitDuration(ctx context.Context, delay time.Duration) error {
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (p *pacer) release(model string) {
	p.mu.Lock()
	if p.inflight > 0 {
		p.inflight--
	}
	if model != "" && p.inflightByModel[model] > 0 {
		p.inflightByModel[model]--
	}
	p.signalChangedLocked()
	p.mu.Unlock()
}

func (p *pacer) signalChangedLocked() {
	close(p.changed)
	p.changed = make(chan struct{})
}

func (p *pacer) signalScheduleChangedLocked() {
	p.signalChangedLocked()
	close(p.scheduleChanged)
	p.scheduleChanged = make(chan struct{})
}

func (p *pacer) logical(model string) {
	p.mu.Lock()
	p.stats.LogicalRequests++
	if model != "" {
		s := p.byModel[model]
		s.LogicalRequests++
		p.byModel[model] = s
	}
	p.mu.Unlock()
}

func (p *pacer) retry(model string) {
	p.mu.Lock()
	p.stats.RetryAttempts++
	if model != "" {
		s := p.byModel[model]
		s.RetryAttempts++
		p.byModel[model] = s
	}
	p.mu.Unlock()
}

func (p *pacer) exhausted(model string) {
	p.mu.Lock()
	p.stats.RetriesExhausted++
	if model != "" {
		s := p.byModel[model]
		s.RetriesExhausted++
		p.byModel[model] = s
	}
	p.mu.Unlock()
}

func (p *pacer) success(model string, durations ...time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats.SuccessfulRequests++
	if len(durations) > 0 {
		p.stats.LatencySamples = append(p.stats.LatencySamples, durations[0].Milliseconds())
	}
	if model != "" {
		s := p.byModel[model]
		s.SuccessfulRequests++
		if len(durations) > 0 {
			s.LatencySamples = append(s.LatencySamples, durations[0].Milliseconds())
		}
		if p.inflightByModel[model] > s.SafeConcurrency {
			s.SafeConcurrency = p.inflightByModel[model]
		}
		p.byModel[model] = s
	}
	if p.inflight > p.maxSuccessfulInFlight {
		p.maxSuccessfulInFlight = p.inflight
	}
	p.successStreak++
	// Additive increase is governed by quiet successful requests, not by the
	// initial dispatch interval.  Otherwise an explicitly fast but safe local
	// interval (for example 1ms in a test provider) would remain permanently
	// serialized despite --concurrency allowing more in-flight work.
	if p.successStreak >= 5 {
		p.successStreak = 0
		p.quietWindows++
		if p.interval > 200*time.Millisecond {
			p.interval -= 100 * time.Millisecond
		}
		// Increase request density first, then admit another in-flight request
		// only after a second quiet window. This avoids treating five successes
		// as evidence that both rate and concurrency may safely double.
		if p.quietWindows >= 2 && p.limit < p.maxLimit {
			p.limit++
			p.quietWindows = 0
		}
		p.revision++
		p.signalScheduleChangedLocked()
	}
}

func (p *pacer) limited(model string, retryAfter time.Duration) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats.RateLimited++
	if model != "" {
		s := p.byModel[model]
		s.RateLimited++
		p.byModel[model] = s
	}
	p.successStreak = 0
	p.quietWindows = 0
	p.limit = 1
	p.interval *= 2
	if p.interval > 30*time.Second {
		p.interval = 30 * time.Second
	}
	delay := retryAfter
	if delay <= 0 {
		delay = p.interval + time.Duration(rand.Int63n(int64(250*time.Millisecond)))
	}
	p.stats.Backoff += delay
	if model != "" {
		s := p.byModel[model]
		s.Backoff += delay
		p.byModel[model] = s
	}
	if until := time.Now().Add(delay); until.After(p.blockedUntil) {
		p.blockedUntil = until
	}
	p.nextStart = p.blockedUntil
	p.revision++
	p.signalScheduleChangedLocked()
	return delay
}

func (p *pacer) recovered(model string) {
	p.mu.Lock()
	p.stats.RetriesRecovered++
	if model != "" {
		s := p.byModel[model]
		s.RetriesRecovered++
		p.byModel[model] = s
	}
	p.mu.Unlock()
}

func (p *pacer) backoff(model string, delay time.Duration) {
	if delay <= 0 {
		return
	}
	p.mu.Lock()
	p.stats.Backoff += delay
	if model != "" {
		s := p.byModel[model]
		s.Backoff += delay
		p.byModel[model] = s
	}
	p.mu.Unlock()
}

func (p *pacer) snapshot() PacingStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.stats
	if out.SuccessfulRequests > 0 && p.interval > 0 {
		concurrency := p.maxSuccessfulInFlight
		if concurrency < 1 {
			concurrency = 1
		}
		out.SafeRate = float64(concurrency) / p.interval.Seconds()
	}
	if out.SuccessfulRequests > 0 {
		out.SafeConcurrency = p.maxSuccessfulInFlight
	}
	out.LatencySamples = append([]int64(nil), p.stats.LatencySamples...)
	return out
}

func (p *pacer) constraints() PacingStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := PacingStats{SafeConcurrency: p.limit}
	if p.interval > 0 {
		out.SafeRate = float64(p.limit) / p.interval.Seconds()
	}
	return out
}

func (p *pacer) snapshotForModel(model string) PacingStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.byModel[model]
	if out.SuccessfulRequests > 0 && p.interval > 0 {
		concurrency := out.SafeConcurrency
		if concurrency < 1 {
			concurrency = 1
		}
		out.SafeRate = float64(concurrency) / p.interval.Seconds()
	}
	if out.SuccessfulRequests == 0 {
		out.SafeRate = 0
		out.SafeConcurrency = 0
	}
	out.LatencySamples = append([]int64(nil), out.LatencySamples...)
	return out
}

// DeltaPacingStats returns counters and latency samples observed after a
// target-scoped operation. The adaptive safe-rate fields intentionally come
// from the later snapshot because they describe the current shared pacer
// state, not a subtractable counter.
func DeltaPacingStats(before, after PacingStats) PacingStats {
	out := PacingStats{
		LogicalRequests:    after.LogicalRequests - before.LogicalRequests,
		PhysicalAttempts:   after.PhysicalAttempts - before.PhysicalAttempts,
		SuccessfulRequests: after.SuccessfulRequests - before.SuccessfulRequests,
		RateLimited:        after.RateLimited - before.RateLimited,
		RetryAttempts:      after.RetryAttempts - before.RetryAttempts,
		RetriesRecovered:   after.RetriesRecovered - before.RetriesRecovered,
		RetriesExhausted:   after.RetriesExhausted - before.RetriesExhausted,
		Backoff:            after.Backoff - before.Backoff,
	}
	if out.SuccessfulRequests > 0 {
		out.SafeRate = after.SafeRate
		out.SafeConcurrency = after.SafeConcurrency
	}
	if len(after.LatencySamples) >= len(before.LatencySamples) {
		out.LatencySamples = append([]int64(nil), after.LatencySamples[len(before.LatencySamples):]...)
	} else {
		out.LatencySamples = append([]int64(nil), after.LatencySamples...)
	}
	return out
}
