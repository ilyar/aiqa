package probe

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/telemetry"
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

func TestExecutorTracesSlotLifecycle(t *testing.T) {
	executor := NewExecutor(2)
	logger := logging.New(4)
	executor.SetLogger(logger)
	executor.SetLogFields(logging.Fields{"target": "openai", "suite": "compat"})
	executor.DoN(2, func() {})
	trace := strings.Join(logger.Lines(), "\n")
	for _, event := range []string{"probe.slots.wait", "probe.slots.acquired", "probe.slots.released"} {
		if !strings.Contains(trace, `"event":"`+event+`"`) {
			t.Fatalf("trace missing %s: %s", event, trace)
		}
	}
	if !strings.Contains(trace, `"target":"openai"`) || !strings.Contains(trace, `"capacity":2`) {
		t.Fatalf("trace missing scope: %s", trace)
	}
}

func TestExecutorTelemetryHasTerminalSlotOperation(t *testing.T) {
	executor := NewExecutor(1)
	logger := logging.New(4)
	provider := telemetry.New(logger)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	executor.SetLogger(logger)
	executor.SetTelemetry(provider)
	executor.SetLogFields(logging.Fields{"target": "openai", "suite": "compat"})
	executor.Do(func() {})
	trace := strings.Join(logger.Lines(), "\n")
	for _, event := range []string{"probe.slots.start", "probe.slots.acquired", "probe.slots.end"} {
		if !strings.Contains(trace, `"event":"`+event+`"`) {
			t.Fatalf("trace missing %s: %s", event, trace)
		}
	}
}
