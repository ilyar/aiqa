package probe

import "sync"

// Executor bounds the number of native probes running at once. A single
// executor is shared by all model workers so --concurrency remains a global
// ceiling instead of being multiplied by the number of models or suites.
type Executor struct {
	sem chan struct{}
}

func NewExecutor(limit int) *Executor {
	if limit < 1 {
		limit = 1
	}
	return &Executor{sem: make(chan struct{}, limit)}
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
	for range slots {
		e.sem <- struct{}{}
	}
	defer func() {
		for range slots {
			<-e.sem
		}
	}()
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
