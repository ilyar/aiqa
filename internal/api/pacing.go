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
	mu                    sync.Mutex
	interval              time.Duration
	laneNext              []time.Time
	blockedUntil          time.Time
	successStreak         int
	quietWindows          int
	inflight              int
	limit                 int
	maxLimit              int
	maxSuccessfulInFlight int
	inflightByModel       map[string]int
	changed               chan struct{}
	stats                 PacingStats
	byModel               map[string]PacingStats
}

func newPacer(initial time.Duration) *pacer {
	if initial <= 0 {
		initial = 2 * time.Second
	}
	return &pacer{interval: initial, limit: 1, maxLimit: 4, laneNext: make([]time.Time, 4), changed: make(chan struct{}), inflightByModel: map[string]int{}, byModel: map[string]PacingStats{}}
}

func (p *pacer) setConcurrency(limit int) {
	if limit < 1 {
		limit = 1
	}
	p.mu.Lock()
	p.limit = limit
	p.maxLimit = limit
	if len(p.laneNext) < limit {
		p.laneNext = append(p.laneNext, make([]time.Time, limit-len(p.laneNext))...)
	}
	close(p.changed)
	p.changed = make(chan struct{})
	p.mu.Unlock()
}

func (p *pacer) wait(ctx context.Context, model string) error {
	p.mu.Lock()
	now := time.Now()
	lanes := p.limit
	if lanes < 1 {
		lanes = 1
	}
	if len(p.laneNext) < lanes {
		p.laneNext = append(p.laneNext, make([]time.Time, lanes-len(p.laneNext))...)
	}
	lane := 0
	for index := 1; index < lanes; index++ {
		if p.laneNext[index].Before(p.laneNext[lane]) {
			lane = index
		}
	}
	if p.laneNext[lane].Before(now) {
		p.laneNext[lane] = now
	}
	start := p.laneNext[lane]
	p.laneNext[lane] = start.Add(p.interval)
	p.mu.Unlock()
	if delay := time.Until(start); delay > 0 {
		if err := waitDuration(ctx, delay); err != nil {
			return err
		}
	}
	// Requests that reserved an earlier start must still observe a 429 backoff
	// imposed while they were waiting. This bounds the small race introduced by
	// multiple queued callers without discarding their pacing reservation.
	for {
		p.mu.Lock()
		blockedUntil := p.blockedUntil
		p.mu.Unlock()
		if delay := time.Until(blockedUntil); delay > 0 {
			if err := waitDuration(ctx, delay); err != nil {
				return err
			}
			continue
		}
		break
	}
	for {
		p.mu.Lock()
		blockedUntil := p.blockedUntil
		if delay := time.Until(blockedUntil); delay > 0 {
			p.mu.Unlock()
			if err := waitDuration(ctx, delay); err != nil {
				return err
			}
			continue
		}
		if p.inflight < p.limit {
			p.inflight++
			if model != "" {
				p.inflightByModel[model]++
			}
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
	close(p.changed)
	p.changed = make(chan struct{})
	p.mu.Unlock()
}

func (p *pacer) success(model string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats.SuccessfulRequests++
	if model != "" {
		s := p.byModel[model]
		s.SuccessfulRequests++
		if p.inflightByModel[model] > s.SafeConcurrency {
			s.SafeConcurrency = p.inflightByModel[model]
		}
		p.byModel[model] = s
	}
	if p.inflight > p.maxSuccessfulInFlight {
		p.maxSuccessfulInFlight = p.inflight
	}
	p.successStreak++
	// Additive increase only after a quiet window; never go faster than 5 req/s.
	if p.successStreak >= 5 && p.interval > 200*time.Millisecond {
		p.interval -= 100 * time.Millisecond
		p.successStreak = 0
		p.quietWindows++
		if p.quietWindows >= 2 && p.limit < p.maxLimit {
			p.limit++
			p.quietWindows = 0
		}
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
	for index := range p.laneNext {
		if p.blockedUntil.After(p.laneNext[index]) {
			p.laneNext[index] = p.blockedUntil
		}
	}
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
func (p *pacer) snapshot() PacingStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.stats
	if p.interval > 0 {
		concurrency := p.maxSuccessfulInFlight
		if concurrency < 1 {
			concurrency = 1
		}
		out.SafeRate = float64(concurrency) / p.interval.Seconds()
	}
	out.SafeConcurrency = p.maxSuccessfulInFlight
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
	if p.interval > 0 {
		concurrency := out.SafeConcurrency
		if concurrency < 1 {
			concurrency = 1
		}
		out.SafeRate = float64(concurrency) / p.interval.Seconds()
	}
	return out
}
