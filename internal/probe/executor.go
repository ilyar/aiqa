package probe

import (
	"context"
	"sync"

	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/telemetry"
)

// Executor bounds the number of native probes running at once. A single
// executor is shared by all model workers so --concurrency remains a global
// ceiling instead of being multiplied by the number of models or suites.
type Executor struct {
	sem       chan struct{}
	logMu     sync.RWMutex
	logger    *logging.Logger
	telemetry *telemetry.Provider
	logFields logging.Fields
}

func NewExecutor(limit int) *Executor {
	if limit < 1 {
		limit = 1
	}
	return &Executor{sem: make(chan struct{}, limit), logFields: logging.Fields{}}
}

// SetLogger enables structured lifecycle events for native probe slots.
func (e *Executor) SetLogger(logger *logging.Logger) {
	e.logMu.Lock()
	e.logger = logger
	e.logMu.Unlock()
}

// SetTelemetry adds local-only lifecycle spans for global probe-slot work.
// It does not create or propagate outbound HTTP headers.
func (e *Executor) SetTelemetry(provider *telemetry.Provider) {
	e.logMu.Lock()
	e.telemetry = provider
	e.logMu.Unlock()
}

// SetLogFields establishes safe execution scope, such as target or suite.
func (e *Executor) SetLogFields(fields logging.Fields) {
	e.logMu.Lock()
	e.logFields = cloneFields(fields)
	e.logMu.Unlock()
}

func (e *Executor) trace(event string, fields logging.Fields) {
	e.logMu.RLock()
	logger := e.logger
	base := cloneFields(e.logFields)
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

func (e *Executor) Do(fn func()) {
	e.DoN(1, fn)
}

// DoN reserves several global probe slots for one operation. Extension
// executables use this to account for their declared internal concurrency.
func (e *Executor) DoN(slots int, fn func()) {
	if slots < 1 {
		slots = 1
	}
	if slots > cap(e.sem) {
		slots = cap(e.sem)
	}
	e.logMu.RLock()
	provider := e.telemetry
	fields := cloneFields(e.logFields)
	e.logMu.RUnlock()
	fields["slots"] = slots
	fields["capacity"] = cap(e.sem)
	ctx := context.Background()
	var operation *telemetry.Operation
	if provider != nil {
		ctx, operation = provider.Start(ctx, "probe.slots", fields)
	}
	e.trace("probe.slots.wait", fields)
	for range slots {
		e.sem <- struct{}{}
	}
	fields["inflight"] = len(e.sem)
	e.trace("probe.slots.acquired", fields)
	if operation != nil {
		operation.Event(logging.Trace, "acquired", logging.Fields{"inflight": len(e.sem)})
	}
	defer func() {
		for range slots {
			<-e.sem
		}
		fields["inflight"] = len(e.sem)
		e.trace("probe.slots.released", fields)
		if operation != nil {
			operation.End("released", logging.Fields{"inflight": len(e.sem)})
		}
	}()
	_ = ctx // slot callbacks have no context parameter; network spans use caller context.
	fn()
}

// Map runs independent probes concurrently and returns their values in input
// order, keeping report output deterministic.
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
		go func() {
			defer wg.Done()
			e.Do(func() { out[index] = task() })
		}()
	}
	wg.Wait()
	return out
}
