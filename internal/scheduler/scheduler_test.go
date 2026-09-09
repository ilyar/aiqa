package scheduler

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}
type fakeTimer struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock      { return &fakeClock{now: time.Unix(0, 0)} }
func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	at := c.now.Add(d)
	if !at.After(c.now) {
		ch <- c.now
		close(ch)
		return ch
	}
	c.timers = append(c.timers, &fakeTimer{at: at, ch: ch})
	return ch
}
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	remaining := c.timers[:0]
	for _, timer := range c.timers {
		if !timer.at.After(c.now) {
			timer.ch <- c.now
			close(timer.ch)
		} else {
			remaining = append(remaining, timer)
		}
	}
	c.timers = remaining
	c.mu.Unlock()
}

func broker(t *testing.T, cfg Config) *Broker {
	t.Helper()
	b, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func eventually(t *testing.T, fn func() bool) {
	t.Helper()
	for range 10000 {
		if fn() {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("condition did not become true")
}
func acquireAsync(b *Broker, ctx context.Context, r Request) <-chan acquireResult {
	ch := make(chan acquireResult, 1)
	go func() { lease, err := b.Acquire(ctx, r); ch <- acquireResult{lease, err} }()
	return ch
}

func TestAtomicWeightedLeaseAndCancellation(t *testing.T) {
	b := broker(t, Config{Capacity: 2})
	first, err := b.Acquire(context.Background(), Request{Target: "a", Group: "g", Weight: 2})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	second := acquireAsync(b, ctx, Request{Target: "b", Group: "g", Weight: 2})
	eventually(t, func() bool { return len(b.Snapshot().Waiting) == 1 })
	if got := b.Snapshot().InFlight; got != 2 {
		t.Fatalf("inflight=%d", got)
	}
	cancel()
	if got := <-second; !errors.Is(got.err, context.Canceled) {
		t.Fatalf("cancellation=%v", got.err)
	}
	first.Release()
	if got := b.Snapshot().InFlight; got != 0 {
		t.Fatalf("leaked lease: %d", got)
	}
	if _, err := b.Acquire(context.Background(), Request{Target: "x", Group: "g", Weight: 3}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized=%v", err)
	}
}

func TestPacingAndCooldownDoNotBlockOtherGroups(t *testing.T) {
	clock := newFakeClock()
	b := broker(t, Config{Capacity: 2, Clock: clock, Groups: map[string]Group{"one": {Interval: time.Minute}}})
	first, err := b.Acquire(context.Background(), Request{Target: "openai", Group: "one"})
	if err != nil {
		t.Fatal(err)
	}
	waiting := acquireAsync(b, context.Background(), Request{Target: "openai", Group: "one"})
	eventually(t, func() bool { s := b.Snapshot(); return len(s.Waiting) == 1 && s.Waiting[0].Reason == WaitInitialPacing })
	other, err := b.Acquire(context.Background(), Request{Target: "anthropic", Group: "two"})
	if err != nil {
		t.Fatalf("independent group blocked: %v", err)
	}
	other.Release()
	first.Release()
	clock.Advance(time.Minute)
	got := <-waiting
	if got.err != nil {
		t.Fatal(got.err)
	}
	got.lease.Release()

	if err := b.ApplyCooldown("one", clock.Now().Add(time.Minute), WaitRetryAfter); err != nil {
		t.Fatal(err)
	}
	throttled := acquireAsync(b, context.Background(), Request{Target: "openai", Group: "one"})
	eventually(t, func() bool {
		s := b.Snapshot()
		return len(s.Waiting) == 1 && s.Waiting[0].Reason == WaitRetryAfter && !s.Waiting[0].EligibleAt.IsZero()
	})
	free, err := b.Acquire(context.Background(), Request{Target: "google", Group: "two"})
	if err != nil {
		t.Fatalf("cooldown leaked across groups: %v", err)
	}
	free.Release()
	clock.Advance(time.Minute)
	got = <-throttled
	if got.err != nil {
		t.Fatal(got.err)
	}
	got.lease.Release()
}

func TestFairTargetsAndNoSlotWhileWaiting(t *testing.T) {
	b := broker(t, Config{Capacity: 1})
	hold, err := b.Acquire(context.Background(), Request{Target: "a", Group: "g"})
	if err != nil {
		t.Fatal(err)
	}
	a := acquireAsync(b, context.Background(), Request{Target: "a", Group: "g"})
	bReq := acquireAsync(b, context.Background(), Request{Target: "b", Group: "g"})
	eventually(t, func() bool { return len(b.Snapshot().Waiting) == 2 })
	hold.Release()
	// a was served last, so the next round begins at b rather than allowing a
	// to monopolise the only slot.
	gotB := <-bReq
	if gotB.err != nil {
		t.Fatal(gotB.err)
	}
	gotB.lease.Release()
	gotA := <-a
	if gotA.err != nil {
		t.Fatal(gotA.err)
	}
	gotA.lease.Release()
}

func TestCancellationUnblocksNextFIFORequest(t *testing.T) {
	b := broker(t, Config{Capacity: 1})
	hold, err := b.Acquire(context.Background(), Request{Target: "a", Group: "g"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	first := acquireAsync(b, ctx, Request{Target: "a", Group: "g"})
	next := acquireAsync(b, context.Background(), Request{Target: "a", Group: "g"})
	eventually(t, func() bool { return len(b.Snapshot().Waiting) == 2 })
	cancel()
	if got := <-first; !errors.Is(got.err, context.Canceled) {
		t.Fatalf("cancel=%v", got.err)
	}
	hold.Release()
	got := <-next
	if got.err != nil {
		t.Fatal(got.err)
	}
	got.lease.Release()
}

func TestArtifactBudgetKeepsAmbiguousCreationAndRefundsPreDispatch(t *testing.T) {
	b := broker(t, Config{Capacity: 2, ArtifactBudget: 2})
	one, err := b.Acquire(context.Background(), Request{Target: "t", Group: "g", ArtifactKey: "t/model", Artifacts: 2})
	if err != nil {
		t.Fatal(err)
	}
	one.Release() // a timeout after dispatch must not create a free retry.
	blockedCtx, cancel := context.WithCancel(context.Background())
	blocked := acquireAsync(b, blockedCtx, Request{Target: "t", Group: "g", ArtifactKey: "t/model", Artifacts: 1})
	eventually(t, func() bool { return len(b.Snapshot().Waiting) == 1 })
	cancel()
	if got := <-blocked; !errors.Is(got.err, context.Canceled) {
		t.Fatalf("artifact wait=%v", got.err)
	}

	b2 := broker(t, Config{Capacity: 2, ArtifactBudget: 2})
	pre, err := b2.Acquire(context.Background(), Request{Target: "t", Group: "g", ArtifactKey: "t/model", Artifacts: 2})
	if err != nil {
		t.Fatal(err)
	}
	pre.CancelBeforeDispatch()
	again, err := b2.Acquire(context.Background(), Request{Target: "t", Group: "g", ArtifactKey: "t/model", Artifacts: 2})
	if err != nil {
		t.Fatalf("reservation was not refunded: %v", err)
	}
	again.Release()
}

func TestMemoryBudgetAllowsSmallUsefulWorkAndLeaseReleaseIsExactlyOnce(t *testing.T) {
	b := broker(t, Config{Capacity: 3, MemoryBudget: 10})
	large, err := b.Acquire(context.Background(), Request{Target: "a", Group: "g", MemoryBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	tooLarge := acquireAsync(b, context.Background(), Request{Target: "b", Group: "g", MemoryBytes: 9})
	eventually(t, func() bool { s := b.Snapshot(); return len(s.Waiting) == 1 && s.Waiting[0].Reason == WaitMemoryBudget })
	small, err := b.Acquire(context.Background(), Request{Target: "c", Group: "g", MemoryBytes: 2})
	if err != nil {
		t.Fatalf("small independent work blocked: %v", err)
	}
	small.Release()
	large.Release()
	large.Release()
	got := <-tooLarge
	if got.err != nil {
		t.Fatal(got.err)
	}
	got.lease.Release()
	got.lease.Release()
	s := b.Snapshot()
	if s.InFlight != 0 || s.MemoryReserved != 0 {
		t.Fatalf("double release leaked state: %+v", s)
	}
}
