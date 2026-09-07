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
		p.success("")
	}
	if got := p.snapshot().SafeConcurrency; got != 1 {
		t.Fatalf("concurrency=%d, want 1 until pacing has improved first", got)
	}
	for i := 0; i < 5; i++ {
		p.success("")
	}
	if got := p.limit; got != 2 {
		t.Fatalf("configured concurrency=%d, want 2 after second quiet success window", got)
	}
	p.limited("", 0)
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
	p.limited("", 40*time.Millisecond)
	started := time.Now()
	if err := p.wait(context.Background(), ""); err != nil {
		t.Fatalf("wait: %v", err)
	}
	defer p.release("")
	if elapsed := time.Since(started); elapsed < 30*time.Millisecond {
		t.Fatalf("wait returned after %s; backoff was bypassed", elapsed)
	}
}

func TestPacerConfiguredConcurrencyIsCeilingNotInitialBurst(t *testing.T) {
	p := newPacer(100 * time.Millisecond)
	p.setConcurrency(3)
	started := make(chan time.Time, 3)
	release := make(chan struct{})
	for range 3 {
		go func() {
			if err := p.wait(context.Background(), ""); err != nil {
				return
			}
			started <- time.Now()
			<-release
			p.release("")
		}()
	}
	first := <-started
	last := first
	for range 2 {
		if next := <-started; next.After(last) {
			last = next
		}
	}
	close(release)
	if spread := last.Sub(first); spread < 150*time.Millisecond {
		t.Fatalf("three configured requests started only %s apart; want global pacing", spread)
	}
}

func TestPacerRateLimitRequeuesStaleReservations(t *testing.T) {
	p := newPacer(2 * time.Second)
	firstReady := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = p.wait(context.Background(), "")
		close(firstReady)
		<-release
		p.release("")
	}()
	<-firstReady
	p.mu.Lock()
	firstReservation := p.nextStart
	p.mu.Unlock()
	second := make(chan time.Time, 1)
	go func() {
		_ = p.wait(context.Background(), "")
		second <- time.Now()
		p.release("")
	}()
	deadline := time.After(time.Second)
	for {
		p.mu.Lock()
		reserved := p.nextStart.After(firstReservation)
		p.mu.Unlock()
		if reserved {
			break
		}
		select {
		case <-deadline:
			t.Fatal("second waiter did not reserve the old schedule")
		case <-time.After(time.Millisecond):
		}
	}
	// The second request has reserved a long old interval. A 429 must wake it
	// promptly, then make it reserve against the cooldown rather than wasting
	// the stale two-second slot (which consumes probe deadline budget).
	limitedAt := time.Now()
	p.limited("", 60*time.Millisecond)
	close(release)
	started := <-second
	if elapsed := started.Sub(limitedAt); elapsed > 500*time.Millisecond {
		t.Fatalf("stale reservation was not promptly requeued: %s", elapsed)
	}
	if elapsed := started.Sub(limitedAt); elapsed < 50*time.Millisecond {
		t.Fatalf("stale reservation started after %s, want cooldown", elapsed)
	}
}
