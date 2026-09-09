package probe

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/telemetry"
)

var ErrReservationTooLarge = errors.New("probe reservation exceeds executor capacity")

// Executor accounts for native probe workflows. It is not an HTTP admission
// controller: physical attempts are admitted by internal/scheduler at the
// transport boundary.
type Executor struct {
	mu       sync.Mutex
	capacity int
	inFlight int
	wake     chan struct{}

	logMu     sync.RWMutex
	logger    *logging.Logger
	telemetry *telemetry.Provider
	logFields logging.Fields
}

func NewExecutor(limit int) *Executor {
	if limit < 1 {
		limit = 1
	}
	return &Executor{capacity: limit, wake: make(chan struct{}), logFields: logging.Fields{}}
}

func (e *Executor) SetLogger(logger *logging.Logger) {
	e.logMu.Lock()
	e.logger = logger
	e.logMu.Unlock()
}
func (e *Executor) SetTelemetry(provider *telemetry.Provider) {
	e.logMu.Lock()
	e.telemetry = provider
	e.logMu.Unlock()
}
func (e *Executor) SetLogFields(fields logging.Fields) {
	e.logMu.Lock()
	e.logFields = cloneFields(fields)
	e.logMu.Unlock()
}

func (e *Executor) trace(event string, fields logging.Fields) {
	e.logMu.RLock()
	logger, base := e.logger, cloneFields(e.logFields)
	e.logMu.RUnlock()
	if logger == nil {
		return
	}
	for key, value := range fields {
		base[key] = value
	}
	logger.Log(logging.Trace, event, base)
}

func cloneFields(fields logging.Fields) logging.Fields {
	copy := make(logging.Fields, len(fields))
	for key, value := range fields {
		copy[key] = value
	}
	return copy
}

// Reservation owns an all-or-none set of workflow slots. Release is safe to
// call repeatedly, including from cancellation cleanup.
type Reservation struct {
	executor  *Executor
	slots     int
	fields    logging.Fields
	operation *telemetry.Operation
	once      sync.Once
}

func (r *Reservation) Release() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		r.executor.mu.Lock()
		r.executor.inFlight -= r.slots
		inFlight := r.executor.inFlight
		r.executor.signalLocked()
		r.executor.mu.Unlock()
		fields := cloneFields(r.fields)
		fields["inflight"] = inFlight
		r.executor.trace("probe.slots.released", fields)
		if r.operation != nil {
			r.operation.End("released", logging.Fields{"inflight": inFlight})
		}
	})
}

// Reserve waits for an atomic workflow reservation. Queue waiting consumes no
// slot and respects ctx. A reservation larger than capacity is never clamped.
func (e *Executor) Reserve(ctx context.Context, slots int) (*Reservation, error) {
	if slots < 1 {
		return nil, fmt.Errorf("invalid probe reservation: %d", slots)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	capacity := e.capacity
	e.mu.Unlock()
	if slots > capacity {
		return nil, fmt.Errorf("%w: requested=%d capacity=%d", ErrReservationTooLarge, slots, capacity)
	}

	e.logMu.RLock()
	provider, fields := e.telemetry, cloneFields(e.logFields)
	e.logMu.RUnlock()
	fields["slots"], fields["capacity"] = slots, capacity
	var operation *telemetry.Operation
	if provider != nil {
		_, operation = provider.Start(ctx, "probe.slots", fields)
	}
	e.trace("probe.slots.wait", fields)
	for {
		e.mu.Lock()
		if e.capacity-e.inFlight >= slots {
			e.inFlight += slots
			inFlight := e.inFlight
			e.mu.Unlock()
			acquired := cloneFields(fields)
			acquired["inflight"] = inFlight
			e.trace("probe.slots.acquired", acquired)
			if operation != nil {
				operation.Event(logging.Trace, "acquired", logging.Fields{"inflight": inFlight})
			}
			return &Reservation{executor: e, slots: slots, fields: acquired, operation: operation}, nil
		}
		wake := e.wake
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			if operation != nil {
				operation.End("cancelled", logging.Fields{})
			}
			return nil, ctx.Err()
		case <-wake:
		}
	}
}

func (e *Executor) signalLocked() { close(e.wake); e.wake = make(chan struct{}) }

// InFlight reports workflow reservations only, never physical HTTP attempts.
func (e *Executor) InFlight() int { e.mu.Lock(); defer e.mu.Unlock(); return e.inFlight }

func (e *Executor) Do(fn func()) { e.DoN(1, fn) }

// DoN is retained for callers that cannot return an error. Invalid declared
// concurrency panics rather than silently clamping. New code should use
// DoNContext or Reserve so configuration errors reach the run result.
func (e *Executor) DoN(slots int, fn func()) {
	if err := e.DoNContext(context.Background(), slots, fn); err != nil {
		panic(err)
	}
}

func (e *Executor) DoNContext(ctx context.Context, slots int, fn func()) error {
	reservation, err := e.Reserve(ctx, slots)
	if err != nil {
		return err
	}
	defer reservation.Release()
	fn()
	return nil
}

// Map runs independent probes concurrently and returns their values in input order.
func Map[T any](e *Executor, tasks ...func() T) []T {
	if len(tasks) == 0 {
		return nil
	}
	if e == nil {
		e = NewExecutor(1)
	}
	out := make([]T, len(tasks))
	var wg sync.WaitGroup
	for index, task := range tasks {
		index, task := index, task
		wg.Add(1)
		go func() { defer wg.Done(); e.Do(func() { out[index] = task() }) }()
	}
	wg.Wait()
	return out
}
