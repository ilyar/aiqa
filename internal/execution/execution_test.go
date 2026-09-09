package execution

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu   sync.Mutex
	wall time.Time
	mono time.Duration
}

func newFakeClock() *fakeClock           { return &fakeClock{wall: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)} }
func (c *fakeClock) Wall() time.Time     { c.mu.Lock(); defer c.mu.Unlock(); return c.wall }
func (c *fakeClock) Mono() time.Duration { c.mu.Lock(); defer c.mu.Unlock(); return c.mono }
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.mono += d
	c.wall = c.wall.Add(d)
	c.mu.Unlock()
}
func (c *fakeClock) jumpWall(d time.Duration) { c.mu.Lock(); c.wall = c.wall.Add(d); c.mu.Unlock() }

func meta(run string, seq uint64, offset time.Duration) Base {
	return Base{Meta: Meta{RunID: run, Seq: seq, At: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC), Offset: offset}}
}

func frozen(t *testing.T, r *Reducer, longID string) {
	t.Helper()
	if err := r.Apply(PlanFrozen{Base: meta("run-1", 1, 0), Revision: 1, Jobs: []JobSpec{
		{ID: longID, TargetID: "openai", RequirementIDs: []string{"r1", "r2"}},
	}}); err != nil {
		t.Fatal(err)
	}
}

func TestLongJobIDsAreOpaqueAndRequirementsDoNotChangeProgress(t *testing.T) {
	c := newFakeClock()
	r, err := New("run-1", c)
	if err != nil {
		t.Fatal(err)
	}
	prefix := strings.Repeat("x", 128)
	first, second := prefix+":first", prefix+":second"
	if err := r.Apply(PlanFrozen{Base: meta("run-1", 1, 0), Revision: 1, Jobs: []JobSpec{
		{ID: first, TargetID: "a", RequirementIDs: []string{"r1", "r2", "r3"}},
		{ID: second, TargetID: "a", RequirementIDs: []string{"r1"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Apply(JobFinished{Base: Base{Meta: Meta{RunID: "run-1", Seq: 2, JobID: first, Offset: time.Second}}, Outcome: OutcomeSucceeded}); err != nil {
		t.Fatal(err)
	}
	s := r.SnapshotAt(time.Second)
	if s.PlannedJobs != 2 || s.FinishedJobs != 1 {
		t.Fatalf("jobs=%d finished=%d, requirements must not count", s.PlannedJobs, s.FinishedJobs)
	}
	if len(s.Jobs) != 2 || s.Jobs[0].ID != first || s.Jobs[1].ID != second {
		t.Fatalf("long IDs were altered: %#v", s.Jobs)
	}
}

func TestTerminalDeliveryIsIdempotentAndConflictsDoNotMutate(t *testing.T) {
	r, err := New("run-1", newFakeClock())
	if err != nil {
		t.Fatal(err)
	}
	frozen(t, r, "job-"+strings.Repeat("q", 512))
	finish := JobFinished{Base: Base{Meta: Meta{RunID: "run-1", Seq: 2, JobID: "job-" + strings.Repeat("q", 512), Offset: time.Second}}, Outcome: OutcomeSucceeded}
	if err := r.Apply(finish); err != nil {
		t.Fatal(err)
	}
	// At-least-once delivery can retain its original sequence, too.
	if err := r.Apply(finish); err != nil {
		t.Fatalf("same terminal event must be idempotent: %v", err)
	}
	conflict := finish
	conflict.Outcome = OutcomeFailed
	conflict.Seq = 3
	if err := r.Apply(conflict); !errors.Is(err, ErrTerminalConflict) {
		t.Fatalf("conflict=%v", err)
	}
	if got := r.SnapshotAt(time.Second); got.FinishedJobs != 1 || got.Jobs[0].Outcome != OutcomeSucceeded {
		t.Fatalf("conflict mutated state: %#v", got.Jobs[0])
	}
	unknown := JobFinished{Base: Base{Meta: Meta{RunID: "run-1", Seq: 4, JobID: "missing", Offset: time.Second}}, Outcome: OutcomeFailed}
	if err := r.Apply(unknown); !errors.Is(err, ErrUnknownEntity) {
		t.Fatalf("unknown=%v", err)
	}
}

func TestWaitSnapshotUsesMonotonicTimeAndEnds(t *testing.T) {
	c := newFakeClock()
	r, err := New("run-1", c)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Apply(WaitStarted{Base: Base{Meta: Meta{RunID: "run-1", Seq: 1, TargetID: "openai", JobID: "job", GroupID: "group-3", Offset: 0}}, WaitID: "wait-12", Reason: WaitRetryAfter, Source: "retry_after_seconds", EligibleAt: 61 * time.Second, ScheduleRevision: 4}); err != nil {
		t.Fatal(err)
	}
	c.advance(8 * time.Second)
	c.jumpWall(24 * time.Hour) // Correlation time can jump; countdown must not.
	s := r.Snapshot()
	if len(s.Waits) != 1 || s.Waits[0].Waited != 8*time.Second || s.Waits[0].ResumesIn != 53*time.Second {
		t.Fatalf("wait=%#v", s.Waits)
	}
	if s.Waits[0].Reason != WaitRetryAfter || s.Waits[0].Source != "retry_after_seconds" {
		t.Fatalf("wait source lost: %#v", s.Waits[0])
	}
	if err := r.Apply(WaitUpdated{Base: Base{Meta: Meta{RunID: "run-1", Seq: 2, TargetID: "openai", JobID: "job", GroupID: "group-3", Offset: 8 * time.Second}}, WaitID: "wait-12", Reason: WaitInitialPacing, Source: "initial_pacing", EligibleAt: 70 * time.Second, ScheduleRevision: 5}); err != nil {
		t.Fatal(err)
	}
	c.advance(time.Second)
	if got := r.Snapshot().Waits[0]; got.ResumesIn != 61*time.Second || got.Reason != WaitInitialPacing {
		t.Fatalf("updated deadline=%#v", got)
	}
	if err := r.Apply(WaitEnded{Base: Base{Meta: Meta{RunID: "run-1", Seq: 3, Offset: 9 * time.Second}}, WaitID: "wait-12"}); err != nil {
		t.Fatal(err)
	}
	if got := r.Snapshot(); len(got.Waits) != 0 {
		t.Fatalf("ended wait remains: %#v", got.Waits)
	}
}

func TestSnapshotIsDeepCopyAndEventsRejectUnsafeFields(t *testing.T) {
	r, err := New("run-1", newFakeClock())
	if err != nil {
		t.Fatal(err)
	}
	frozen(t, r, "job-1")
	s := r.Snapshot()
	s.Jobs[0].RequirementIDs[0] = "mutated"
	if got := r.Snapshot().Jobs[0].RequirementIDs[0]; got != "r1" {
		t.Fatalf("snapshot leaked backing array: %q", got)
	}
	err = r.Apply(TargetPhaseChanged{Base: Base{Meta: Meta{RunID: "run-1", Seq: 2, TargetID: "token\nsecret"}}, Phase: PhasePreparing})
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("unsafe event=%v", err)
	}
	err = r.Apply(WaitStarted{Base: Base{Meta: Meta{RunID: "run-1", Seq: 3, TargetID: "openai", JobID: "job-1", GroupID: "group-1", Offset: time.Second}}, WaitID: "wait-unsafe", Reason: WaitRetryAfter, Source: WaitSource("raw-header-value"), EligibleAt: 2 * time.Second})
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("unsafe wait source=%v", err)
	}
}

func TestRequestAndArtifactState(t *testing.T) {
	r, err := New("run-1", newFakeClock())
	if err != nil {
		t.Fatal(err)
	}
	frozen(t, r, "job-1")
	m := Meta{RunID: "run-1", TargetID: "openai", JobID: "job-1", AttemptID: "attempt-1", GroupID: "group-1", Offset: time.Second}
	m.Seq = 2
	if err := r.Apply(RequestQueued{Base: Base{Meta: m}}); err != nil {
		t.Fatal(err)
	}
	m.Seq = 3
	if err := r.Apply(RequestAdmitted{Base: Base{Meta: m}}); err != nil {
		t.Fatal(err)
	}
	m.Seq = 4
	if err := r.Apply(RequestCompleted{Base: Base{Meta: m}, Outcome: OutcomeSucceeded}); err != nil {
		t.Fatal(err)
	}
	if err := r.Apply(ArtifactReserved{Base: meta("run-1", 5, time.Second), ArtifactID: "artifact-1", Bytes: 12}); err != nil {
		t.Fatal(err)
	}
	if err := r.Apply(ArtifactCommitted{Base: meta("run-1", 6, time.Second), ArtifactID: "artifact-1"}); err != nil {
		t.Fatal(err)
	}
	s := r.SnapshotAt(time.Second)
	if len(s.Requests) != 1 || s.Requests[0].State != RequestTerminal || len(s.Artifacts) != 1 || !s.Artifacts[0].Committed {
		t.Fatalf("snapshot=%#v", s)
	}
}
