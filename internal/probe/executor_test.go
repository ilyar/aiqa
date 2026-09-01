package probe

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestMapBoundsConcurrencyAndPreservesOrder(t *testing.T) {
	executor := NewExecutor(3)
	var active atomic.Int32
	var maximum atomic.Int32
	tasks := make([]func() int, 8)
	for index := range tasks {
		index := index
		tasks[index] = func() int {
			current := active.Add(1)
			for {
				seen := maximum.Load()
				if current <= seen || maximum.CompareAndSwap(seen, current) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			active.Add(-1)
			return index
		}
	}

	got := Map(executor, tasks...)
	for index := range got {
		if got[index] != index {
			t.Fatalf("result[%d]=%d, want %d", index, got[index], index)
		}
	}
	if maximum.Load() != 3 {
		t.Fatalf("maximum concurrency=%d, want 3", maximum.Load())
	}
}
