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

func reserveMemoryAsync(b *Broker, ctx context.Context, bytes int64) <-chan memoryResult {
	ch := make(chan memoryResult, 1)
	go func() { lease, err := b.ReserveMemory(ctx, bytes); ch <- memoryResult{lease, err} }()
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

func TestAdaptiveGroupSharesLearnedCapacityAndInterval(t *testing.T) {
	clock := newFakeClock()
	b := broker(t, Config{Capacity: 4, Clock: clock, Groups: map[string]Group{
		"shared": {Capacity: 3, Adaptive: &AdaptiveGroup{InitialInterval: 200 * time.Millisecond}},
	}})
	for i := 0; i < 10; i++ {
		lease, err := b.Acquire(context.Background(), Request{Target: "target", Group: "shared"})
		if err != nil {
			t.Fatal(err)
		}
		lease.Succeeded()
		lease.Release()
		if i != 9 {
			clock.Advance(200 * time.Millisecond)
		}
	}
	s := b.Snapshot().Groups["shared"]
	if s.Capacity != 3 || s.LearnedCapacity != 2 {
		t.Fatalf("capacity snapshot=%+v, want configured=3 learned=2", s)
	}
	if s.InitialInterval != 200*time.Millisecond || s.Interval != 200*time.Millisecond || s.Revision == 0 {
		t.Fatalf("adaptive state snapshot=%+v", s)
	}
}

func TestAdaptiveRateLimitRevisesGroupBeforeLeaseRelease(t *testing.T) {
	clock := newFakeClock()
	b := broker(t, Config{Capacity: 2, Clock: clock, Groups: map[string]Group{
		"shared": {Capacity: 2, Adaptive: &AdaptiveGroup{InitialInterval: 200 * time.Millisecond}},
	}})
	first, err := b.Acquire(context.Background(), Request{Target: "one", Group: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(200 * time.Millisecond) // clear the initial start interval
	waiting := acquireAsync(b, context.Background(), Request{Target: "two", Group: "shared"})
	eventually(t, func() bool {
		s := b.Snapshot()
		return len(s.Waiting) == 1 && s.Waiting[0].Reason == WaitQuotaCapacity
	})
	if got := first.RateLimited(0); got != 400*time.Millisecond {
		t.Fatalf("no-header cooldown=%s, want 400ms", got)
	}
	beforeRelease := b.Snapshot().Groups["shared"]
	if beforeRelease.LearnedCapacity != 1 || beforeRelease.Interval != 400*time.Millisecond || beforeRelease.Cooldown != clock.Now().Add(400*time.Millisecond) || beforeRelease.Revision == 0 {
		t.Fatalf("feedback was not recorded before release: %+v", beforeRelease)
	}
	first.Release()
	eventually(t, func() bool {
		s := b.Snapshot()
		return len(s.Waiting) == 1 && s.Waiting[0].Reason == WaitAdaptivePacing && s.Waiting[0].EligibleAt == clock.Now().Add(400*time.Millisecond)
	})
	clock.Advance(400 * time.Millisecond)
	got := <-waiting
	if got.err != nil {
		t.Fatal(got.err)
	}
	got.lease.Release()
}

// A request can initially be blocked only by global capacity, so it has no
// timer.  If a Retry-After cooldown arrives before that capacity is released,
// releasing the slot must wake it to observe the cooldown and install its own
// timer.  Otherwise it remains asleep forever after the fake clock advances.
func TestCapacityWaiterRechecksNewCooldown(t *testing.T) {
	clock := newFakeClock()
	b := broker(t, Config{Capacity: 1, Clock: clock})
	hold, err := b.Acquire(context.Background(), Request{Target: "a", Group: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	waiting := acquireAsync(b, context.Background(), Request{Target: "b", Group: "shared"})
	eventually(t, func() bool {
		s := b.Snapshot()
		return len(s.Waiting) == 1 && s.Waiting[0].Reason == WaitGlobalCapacity && s.Waiting[0].EligibleAt.IsZero()
	})
	if err := b.ApplyCooldown("shared", clock.Now().Add(time.Minute), WaitRetryAfter); err != nil {
		t.Fatal(err)
	}
	hold.Release()
	eventually(t, func() bool {
		s := b.Snapshot()
		return len(s.Waiting) == 1 && s.Waiting[0].Reason == WaitRetryAfter && !s.Waiting[0].EligibleAt.IsZero()
	})
	clock.Advance(time.Minute)
	got := <-waiting
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
	if _, err := b.Acquire(context.Background(), Request{Target: "t", Group: "g", ArtifactKey: "t/model", Artifacts: 1}); !errors.Is(err, ErrArtifactBudgetExhausted) {
		t.Fatalf("spent artifact budget error=%v", err)
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

func TestReserveMemoryConsumesOnlyMemoryBudget(t *testing.T) {
	clock := newFakeClock()
	b := broker(t, Config{
		Capacity:       1,
		MemoryBudget:   10,
		ArtifactBudget: 0,
		Clock:          clock,
		Groups:         map[string]Group{"paced": {Capacity: 1, Interval: time.Hour}},
	})
	hold, err := b.Acquire(context.Background(), Request{Target: "target", Group: "paced"})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.ApplyCooldown("paced", clock.Now().Add(time.Hour), WaitRetryAfter); err != nil {
		t.Fatal(err)
	}
	reserved, err := b.ReserveMemory(context.Background(), 10)
	if err != nil {
		t.Fatalf("memory reservation incorrectly used request admission: %v", err)
	}
	s := b.Snapshot()
	if s.InFlight != 1 || s.MemoryReserved != 10 || len(s.Artifacts) != 0 {
		t.Fatalf("memory reservation changed unrelated resources: %+v", s)
	}

	if _, err := b.ReserveMemory(context.Background(), 11); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized memory reservation=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	waiting := reserveMemoryAsync(b, ctx, 1)
	eventually(t, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return len(b.memoryQueue) == 1
	})
	cancel()
	if got := <-waiting; !errors.Is(got.err, context.Canceled) {
		t.Fatalf("memory cancellation=%v", got.err)
	}

	next := reserveMemoryAsync(b, context.Background(), 10)
	eventually(t, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return len(b.memoryQueue) == 1
	})
	reserved.Release()
	reserved.Release()
	got := <-next
	if got.err != nil {
		t.Fatalf("memory waiter was not released: %v", got.err)
	}
	got.lease.Release()
	got.lease.Release()
	hold.Release()
	s = b.Snapshot()
	if s.InFlight != 0 || s.MemoryReserved != 0 {
		t.Fatalf("memory release was not exactly once: %+v", s)
	}
}

func TestEnsureAdaptiveGroupMergesSharedConservativeBounds(t *testing.T) {
	b := broker(t, Config{Capacity: 8})
	if err := b.EnsureAdaptiveGroup("shared", AdaptiveGroup{InitialInterval: time.Second, MaxCapacity: 6}); err != nil {
		t.Fatal(err)
	}
	if err := b.EnsureAdaptiveGroup("shared", AdaptiveGroup{InitialInterval: 3 * time.Second, MaxCapacity: 3}); err != nil {
		t.Fatal(err)
	}
	group := b.Snapshot().Groups["shared"]
	if group.LearnedCapacity != 1 || group.Capacity != 8 || group.InitialInterval != 3*time.Second || group.Interval != 3*time.Second {
		t.Fatalf("unexpected merged group: %+v", group)
	}
}

func TestFactObserverPublishesStablePhysicalRequestAndWaitFactsAfterUnlock(t *testing.T) {
	clock := newFakeClock()
	b := broker(t, Config{Capacity: 1, Clock: clock, Groups: map[string]Group{"shared": {Interval: time.Minute}}})
	var mu sync.Mutex
	var facts []Fact
	b.SetFactObserver(func(f Fact) {
		// Snapshot takes the scheduler lock. This would deadlock if facts were
		// delivered under that lock rather than after the mutation.
		_ = b.Snapshot()
		mu.Lock()
		facts = append(facts, f)
		mu.Unlock()
	})
	first, err := b.Acquire(context.Background(), Request{Target: "target", Group: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	second := acquireAsync(b, context.Background(), Request{Target: "target", Group: "shared"})
	eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, f := range facts {
			if f.Kind == FactWaitStarted {
				return true
			}
		}
		return false
	})
	first.Release()
	clock.Advance(time.Minute)
	got := <-second
	if got.err != nil {
		t.Fatal(got.err)
	}
	got.lease.Release()

	mu.Lock()
	defer mu.Unlock()
	var requestID, waitID string
	seen := map[FactKind]bool{}
	for index, f := range facts {
		if f.Sequence != uint64(index+1) {
			t.Fatalf("fact sequence out of order: %#v", facts)
		}
		if f.Kind != FactGroupUpdated && f.Reservation != ReservationPhysical {
			t.Fatalf("physical request fact has reservation %q: %#v", f.Reservation, f)
		}
		seen[f.Kind] = true
		if f.Kind == FactWaitStarted {
			requestID, waitID = f.RequestID, f.WaitID
		}
		if f.Kind == FactWaitEnded && (f.RequestID != requestID || f.WaitID != waitID) {
			t.Fatalf("wait identity changed: start=%q/%q end=%q/%q", requestID, waitID, f.RequestID, f.WaitID)
		}
	}
	for _, kind := range []FactKind{FactRequestQueued, FactRequestAdmitted, FactWaitStarted, FactWaitEnded, FactRequestReleased, FactGroupUpdated} {
		if !seen[kind] {
			t.Fatalf("missing %s in %#v", kind, facts)
		}
	}
}

func TestFactObserverDistinguishesArtifactAndMemoryReservations(t *testing.T) {
	b := broker(t, Config{Capacity: 2, MemoryBudget: 64, ArtifactBudget: 1})
	var facts []Fact
	b.SetFactObserver(func(f Fact) { facts = append(facts, f) })
	artifact, err := b.Acquire(context.Background(), Request{Target: "target", Group: "shared", ArtifactKey: "opaque-bucket", Artifacts: 1})
	if err != nil {
		t.Fatal(err)
	}
	artifact.Release()
	memory, err := b.ReserveMemory(context.Background(), 16)
	if err != nil {
		t.Fatal(err)
	}
	memory.Release()

	seenArtifact, seenCommitted, seenMemoryReserved, seenMemoryReleased := false, false, false, false
	for _, f := range facts {
		switch f.Kind {
		case FactRequestAdmitted:
			seenArtifact = seenArtifact || f.Reservation == ReservationArtifact
		case FactArtifactCommitted:
			seenCommitted = f.Reservation == ReservationArtifact
		case FactMemoryReserved:
			seenMemoryReserved = f.Reservation == ReservationMemory && f.RequestID != ""
		case FactMemoryReleased:
			seenMemoryReleased = f.Reservation == ReservationMemory && f.RequestID != ""
		}
	}
	if !seenArtifact || !seenCommitted || !seenMemoryReserved || !seenMemoryReleased {
		t.Fatalf("reservation facts missing or misclassified: %#v", facts)
	}
}

func TestFactObserverPreservesOnlyValidatedPhysicalOwner(t *testing.T) {
	b := broker(t, Config{Capacity: 1})
	var facts []Fact
	b.SetFactObserver(func(f Fact) { facts = append(facts, f) })
	owner := "owner-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	lease, err := b.Acquire(context.Background(), Request{Target: "target", Group: "group", Owner: owner})
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	for _, fact := range facts {
		if fact.Kind == FactRequestQueued || fact.Kind == FactRequestAdmitted || fact.Kind == FactRequestReleased {
			if fact.Owner != owner {
				t.Fatalf("owner lost from physical fact: %#v", fact)
			}
		}
	}
	if _, err := b.Acquire(context.Background(), Request{Target: "target", Group: "group", Owner: "untrusted"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unsafe owner error=%v", err)
	}
	if _, err := b.Acquire(context.Background(), Request{Target: "target", Group: "group", Reservation: ReservationOpaque, Owner: owner}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("opaque reservation accepted owner: %v", err)
	}
}
