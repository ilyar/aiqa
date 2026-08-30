package api

import (
	"context"
	"testing"
	"time"
)

func TestPacerAdditiveIncreaseAndRateLimitDecrease(t *testing.T) {
	p := newPacer(time.Second)
	p.inflight = 1
	for i := 0; i < 5; i++ {
		p.success()
	}
	if got := p.snapshot().SafeConcurrency; got != 1 {
		t.Fatalf("concurrency=%d, want 1 until pacing has improved first", got)
	}
	for i := 0; i < 5; i++ {
		p.success()
	}
	if got := p.limit; got != 2 {
		t.Fatalf("configured concurrency=%d, want 2 after second quiet success window", got)
	}
	p.limited(0)
	stats := p.snapshot()
	if stats.SafeConcurrency != 1 {
		t.Fatalf("concurrency=%d, want 1 after 429", stats.SafeConcurrency)
	}
	if stats.RateLimited != 1 {
		t.Fatalf("rate limited=%d, want 1", stats.RateLimited)
	}
}

func TestPacerWaitHonorsBackoffAfterReservation(t *testing.T) {
	p := newPacer(time.Millisecond)
	// A caller reaching the scheduler while a 429 backoff is active must not
	// start until that backoff expires.
	p.mu.Lock()
	p.next = time.Now()
	p.mu.Unlock()
	p.limited(40 * time.Millisecond)
	started := time.Now()
	if err := p.wait(context.Background()); err != nil {
		t.Fatalf("wait: %v", err)
	}
	defer p.release()
	if elapsed := time.Since(started); elapsed < 30*time.Millisecond {
		t.Fatalf("wait returned after %s; backoff was bypassed", elapsed)
	}
}
