package probe

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/telemetry"
)

func TestMapBoundsConcurrencyAndPreservesOrder(t *testing.T) {
	executor := NewExecutor(3)
	var active atomic.Int32
	var maximum atomic.Int32
	started := make(chan struct{}, 3)
	release := make(chan struct{})
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
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			active.Add(-1)
			return index
		}
	}
	result := make(chan []int, 1)
	go func() { result <- Map(executor, tasks...) }()
	for range 3 {
		<-started
	}
	if got := executor.InFlight(); got != 3 {
		t.Fatalf("inflight=%d, want 3", got)
	}
	close(release)
	got := <-result
	for index := range got {
		if got[index] != index {
			t.Fatalf("result[%d]=%d, want %d", index, got[index], index)
		}
	}
	if maximum.Load() != 3 {
		t.Fatalf("maximum concurrency=%d, want 3", maximum.Load())
	}
}

func TestReserveIsAtomicAndDoesNotClamp(t *testing.T) {
	executor := NewExecutor(2)
	if _, err := executor.Reserve(context.Background(), 3); !errors.Is(err, ErrReservationTooLarge) {
		t.Fatalf("Reserve oversized error=%v, want ErrReservationTooLarge", err)
	}
	firstReady := make(chan *Reservation, 1)
	secondReady := make(chan *Reservation, 1)
	go func() { reservation, _ := executor.Reserve(context.Background(), 2); firstReady <- reservation }()
	first := <-firstReady
	if got := executor.InFlight(); got != 2 {
		t.Fatalf("first reservation inflight=%d, want 2", got)
	}
	go func() { reservation, _ := executor.Reserve(context.Background(), 2); secondReady <- reservation }()
	select {
	case <-secondReady:
		t.Fatal("second weighted reservation acquired while all capacity held")
	default:
	}
	first.Release()
	second := <-secondReady
	if got := executor.InFlight(); got != 2 {
		t.Fatalf("second reservation inflight=%d, want 2", got)
	}
	second.Release()
}

func TestReserveRespectsCancelledContextWithoutLeakingSlots(t *testing.T) {
	executor := NewExecutor(1)
	hold, err := executor.Reserve(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := executor.Reserve(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("Reserve cancelled error=%v, want context.Canceled", err)
	}
	if got := executor.InFlight(); got != 1 {
		t.Fatalf("cancelled waiter changed inflight=%d, want 1", got)
	}
	hold.Release()
	if got := executor.InFlight(); got != 0 {
		t.Fatalf("release inflight=%d, want 0", got)
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
