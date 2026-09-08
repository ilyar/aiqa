package api

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPacerWaitObserverSpansReservationRevisionAndCapacity(t *testing.T) {
	p := newPacer(80 * time.Millisecond)
	if err := p.wait(context.Background(), ""); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	finished := make(chan error, 1)
	var mu sync.Mutex
	var events []bool
	go func() {
		finished <- p.wait(context.Background(), "", func(waiting bool, _ time.Duration) {
			mu.Lock()
			events = append(events, waiting)
			mu.Unlock()
			if waiting {
				close(started)
			}
		})
	}()
	<-started
	// A rate-limit revision wakes the old reservation. The waiter then has to
	// reserve again and wait for the still-in-flight first request.
	p.limited("", 5*time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	p.release("")
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || !events[0] || events[1] {
		t.Fatalf("observer events=%v, want one true/false pair", events)
	}
}

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
	if got := p.limit; got != 1 {
		t.Fatalf("initial concurrency=%d, want 1", got)
	}
	// A configured value is only a ceiling. Grow to it through the same quiet
	// success windows used in production before checking that concurrent work is
	// still globally paced.
	for range 20 {
		p.success("")
	}
	if got := p.limit; got != 3 {
		t.Fatalf("warmed concurrency=%d, want configured ceiling 3", got)
	}
	started := make(chan time.Time, 3)
	release := make(chan struct{})
	for range 3 {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := p.wait(ctx, ""); err != nil {
				return
			}
			started <- time.Now()
			<-release
			p.release("")
		}()
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	nextStarted := func() time.Time {
		t.Helper()
		select {
		case startedAt := <-started:
			return startedAt
		case <-deadline.C:
			t.Fatal("timed out waiting for paced request admission")
			return time.Time{}
		}
	}
	first := nextStarted()
	last := first
	for range 2 {
		if next := nextStarted(); next.After(last) {
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
