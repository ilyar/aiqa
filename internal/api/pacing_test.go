package api

import (
	"testing"
	"time"
)

func TestPacerAdditiveIncreaseAndRateLimitDecrease(t *testing.T) {
	p := newPacer(time.Second)
	for i := 0; i < 5; i++ {
		p.success()
	}
	if got := p.snapshot().SafeConcurrency; got != 2 {
		t.Fatalf("concurrency=%d, want 2 after quiet success window", got)
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
