package execution

import (
	"fmt"
	"sync"

	"github.com/ilyar/aiqa/internal/result"
)

// SummaryOptions identifies the run-owned scheduler policy used to produce a
// public execution aggregate. It intentionally has no target, endpoint, or
// provider fields: those are not public execution metadata.
type SummaryOptions struct {
	GlobalRequestCapacity int
}

const (
	ExecutionSummarySchema = "aiqa.execution-summary.v1"
	DefaultSchedulerPolicy = "fair-target-admission/v1"
	DefaultEstimateMethod  = "remaining-work-simulation/v1"
)

// SummaryBuilder converts reducer snapshots to the optional report aggregate.
// Call Observe for every snapshot delivered to a live consumer, then call
// Build with the final snapshot. Counters are monotonic: stale snapshots cannot
// decrease an observed peak or add negative wait time.
//
// SumRequestWaitMS is the accumulated waiter-duration between observations;
// WallTimeWithWaitersMS is the elapsed wall duration for intervals with one or
// more waiters. They are deliberately separate because overlapping waits must
// not be reported as wall elapsed.
type SummaryBuilder struct {
	mu       sync.Mutex
	options  SummaryOptions
	seen     bool
	lastAt   int64
	lastWait int
	peak     int
	waitMS   int64
	wallMS   int64
}

// NewSummaryBuilder rejects incomplete report metadata before any report is
// emitted. Policy/method are fixed protocol identifiers rather than caller
// strings, so this bridge cannot become a path for configuration secrets.
func NewSummaryBuilder(options SummaryOptions) (*SummaryBuilder, error) {
	if options.GlobalRequestCapacity < 1 {
		return nil, fmt.Errorf("execution summary: global request capacity must be positive")
	}
	return &SummaryBuilder{options: options}, nil
}

// Observe records live accounting from a reducer snapshot. It does not retain
// snapshot IDs, labels, requirements, or any potentially provider-owned data.
func (b *SummaryBuilder) Observe(snapshot Snapshot) error {
	if err := validSummarySnapshot(snapshot); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.observe(snapshot)
	return nil
}

// Build returns a report-safe aggregate. It observes the supplied final
// snapshot first, so callers do not need a special final-tick path.
func (b *SummaryBuilder) Build(snapshot Snapshot) (*result.ExecutionSummary, error) {
	if err := validSummarySnapshot(snapshot); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.observe(snapshot)

	finished := snapshot.FinishedJobs - snapshot.CancelledJobs
	if finished < 0 {
		return nil, fmt.Errorf("execution summary: cancelled jobs exceed finished jobs")
	}
	return &result.ExecutionSummary{
		SchemaVersion:         ExecutionSummarySchema,
		Phase:                 string(snapshot.Run.Phase),
		ElapsedMS:             durationMS(snapshot.Run.Offset),
		GlobalRequestCapacity: b.options.GlobalRequestCapacity,
		ObservedPeakInflight:  b.peak,
		PlannedJobs:           snapshot.PlannedJobs,
		FinishedJobs:          finished,
		CancelledJobs:         snapshot.CancelledJobs,
		PacingGroupCount:      len(snapshot.Groups),
		SchedulerPolicy:       DefaultSchedulerPolicy,
		EstimateMethod:        DefaultEstimateMethod,
		SumRequestWaitMS:      b.waitMS,
		WallTimeWithWaitersMS: b.wallMS,
	}, nil
}

func (b *SummaryBuilder) observe(snapshot Snapshot) {
	at := durationMS(snapshot.Run.Offset)
	if b.seen && at < b.lastAt {
		return // A stale UI read cannot make accounting regress.
	}
	if b.seen {
		delta := at - b.lastAt
		if b.lastWait > 0 {
			b.wallMS += delta
			b.waitMS += delta * int64(b.lastWait)
		}
	}
	inflight := 0
	for _, request := range snapshot.Requests {
		if request.State == RequestAdmittedState || request.State == RequestHeadersState {
			inflight++
		}
	}
	if inflight > b.peak {
		b.peak = inflight
	}
	b.seen, b.lastAt, b.lastWait = true, at, len(snapshot.Waits)
}

func validSummarySnapshot(snapshot Snapshot) error {
	if snapshot.SchemaVersion != SchemaVersion {
		return fmt.Errorf("execution summary: unsupported snapshot schema %q", snapshot.SchemaVersion)
	}
	if !validPhase(snapshot.Run.Phase) || snapshot.Run.Offset < 0 || snapshot.PlannedJobs < 0 || snapshot.FinishedJobs < 0 || snapshot.CancelledJobs < 0 || snapshot.FinishedJobs > snapshot.PlannedJobs || snapshot.CancelledJobs > snapshot.FinishedJobs {
		return fmt.Errorf("execution summary: invalid snapshot counters")
	}
	return nil
}

func durationMS(value interface{ Milliseconds() int64 }) int64 { return value.Milliseconds() }
