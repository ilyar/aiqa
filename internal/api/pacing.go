package api

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// PacingStats describes observed client-side pacing, not a provider guarantee.
type PacingStats struct {
	SuccessfulRequests int           `json:"successful_requests"`
	RateLimited        int           `json:"rate_limited_responses"`
	RetriesRecovered   int           `json:"retries_recovered"`
	Backoff            time.Duration `json:"backoff_duration"`
	SafeRate           float64       `json:"observed_safe_requests_per_second"`
	SafeConcurrency    int           `json:"observed_safe_concurrency"`
}

type pacer struct {
	mu            sync.Mutex
	interval      time.Duration
	next          time.Time
	successStreak int
	inflight      int
	limit         int
	changed       chan struct{}
	stats         PacingStats
}

func newPacer(initial time.Duration) *pacer {
	if initial <= 0 {
		initial = time.Second
	}
	return &pacer{interval: initial, limit: 1, changed: make(chan struct{})}
}

func (p *pacer) wait(ctx context.Context) error {
	p.mu.Lock()
	now := time.Now()
	if p.next.Before(now) {
		p.next = now
	}
	start := p.next
	p.next = start.Add(p.interval)
	p.mu.Unlock()
	if delay := time.Until(start); delay > 0 {
		t := time.NewTimer(delay)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	for {
		p.mu.Lock()
		if p.inflight < p.limit {
			p.inflight++
			p.mu.Unlock()
			return nil
		}
		changed := p.changed
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (p *pacer) release() {
	p.mu.Lock()
	if p.inflight > 0 {
		p.inflight--
	}
	close(p.changed)
	p.changed = make(chan struct{})
	p.mu.Unlock()
}

func (p *pacer) success() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats.SuccessfulRequests++
	p.successStreak++
	// Additive increase only after a quiet window; never go faster than 5 req/s.
	if p.successStreak >= 5 && p.interval > 200*time.Millisecond {
		p.interval -= 100 * time.Millisecond
		p.successStreak = 0
		if p.limit < 4 {
			p.limit++
		}
	}
}

func (p *pacer) limited(retryAfter time.Duration) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats.RateLimited++
	p.successStreak = 0
	p.limit = 1
	if p.interval < 2*time.Second {
		p.interval *= 2
	}
	if p.interval > 30*time.Second {
		p.interval = 30 * time.Second
	}
	delay := retryAfter
	if delay <= 0 {
		delay = p.interval + time.Duration(rand.Int63n(int64(250*time.Millisecond)))
	}
	p.stats.Backoff += delay
	if until := time.Now().Add(delay); until.After(p.next) {
		p.next = until
	}
	return delay
}

func (p *pacer) recovered() { p.mu.Lock(); p.stats.RetriesRecovered++; p.mu.Unlock() }
func (p *pacer) snapshot() PacingStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.stats
	if p.interval > 0 {
		out.SafeRate = 1 / p.interval.Seconds()
	}
	out.SafeConcurrency = p.limit
	return out
}
