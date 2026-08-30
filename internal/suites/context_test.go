package suites

import (
	"testing"

	"github.com/ilyar/aiqa/internal/result"
)

func TestHasAcceptedContextSample(t *testing.T) {
	samples := []result.ContextSample{
		{Accepted: true},
		{Accepted: false, Overflow: true},
	}
	if !hasAcceptedContextSample(samples) {
		t.Fatal("an earlier accepted sample must keep later size-only 4xx responses classifiable as overflow")
	}
	if hasAcceptedContextSample([]result.ContextSample{{Accepted: false}}) {
		t.Fatal("rejected samples alone must not establish a context boundary")
	}
}

func TestContextBinarySearchAlignmentRefinesSmallWindows(t *testing.T) {
	if got := alignBytes(1536, 256); got != 1536 {
		t.Fatalf("small-window midpoint=%d, want 1536", got)
	}
	if got := alignBytes(1536, 1024); got != 1024 {
		t.Fatalf("sanity check: coarse midpoint=%d, want 1024", got)
	}
}
