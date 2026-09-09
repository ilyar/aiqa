// Package eta estimates the remaining duration of a run without dispatching
// requests.  It is deliberately a pure scheduling model: callers provide an
// immutable broker snapshot and may use a fake clock in tests.
package eta

import (
	"fmt"
	"sort"
	"time"
)

const MethodVersion = "remaining-work-simulation/v1"

// Clock is intentionally small so an estimator never needs real time in a
// deterministic replay test.
type Clock interface{ Now() time.Time }

// State describes how much of the remaining makespan is known.
type State string

const (
	Estimating State = "estimating"
	Estimated  State = "estimated"
	LowerBound State = "lower_bound"
	Unknown    State = "unknown"
	Finalizing State = "finalizing"
	Complete   State = "complete"
)

// Scenario carries the service time used for a low, base or high simulation.
// Durations are request service time only; queue and pacing time are modeled by
// the simulator.
type Scenario struct{ Low, Base, High time.Duration }

// ServiceEstimate turns successful service observations into the three
// scenario inputs. It intentionally has no queue/cooldown fields: those are
// scheduler facts and must not be folded into request throughput.
type ServiceEstimate struct {
	Scenario Scenario
	Evidence string // none | low | sufficient
}

// ServiceFromSamples derives a deterministic service estimate. Samples must
// be successful attempt service durations only; callers must omit skipped,
// blocked, validation-only and timed-out attempts. With fewer than eight
// samples the estimate is explicitly low evidence. The implementation uses
// nearest-rank q25/q90 to avoid a statistics dependency and is replay-stable.
func ServiceFromSamples(samples []time.Duration) ServiceEstimate {
	if len(samples) == 0 {
		return ServiceEstimate{Evidence: "none"}
	}
	values := make([]time.Duration, 0, len(samples))
	for _, sample := range samples {
		if sample >= 0 {
			values = append(values, sample)
		}
	}
	if len(values) == 0 {
		return ServiceEstimate{Evidence: "none"}
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	rank := func(numerator, denominator int) time.Duration {
		index := (len(values)*numerator + denominator - 1) / denominator
		if index < 1 {
			index = 1
		}
		if index > len(values) {
			index = len(values)
		}
		return values[index-1]
	}
	evidence := "low"
	if len(values) >= 8 {
		evidence = "sufficient"
	}
	return ServiceEstimate{Scenario: Scenario{Low: rank(1, 4), Base: rank(1, 2), High: rank(9, 10)}, Evidence: evidence}
}

// Group is the scheduling part of an immutable broker snapshot.  Cooldown and
// NextDispatch are absolute monotonic-clock values (the same domain as Now).
type Group struct {
	ID            string
	Capacity      int
	StartInterval time.Duration
	NextDispatch  time.Time
	Cooldown      time.Time
}

// Running is an already admitted request.  A cooldown never delays it; its
// Remaining is intentionally explicit so a request older than all samples is
// not silently given a zero remaining duration.
type Running struct {
	ID        string
	Group     string
	Remaining *time.Duration
}

// Job is one remaining physical request. Dependencies refer to other Job IDs.
// UnknownDuration marks adaptive/opaque work whose upper duration is not known.
// ReadyAt is useful for a dependency snapshot produced by a runner.
type Job struct {
	ID              string
	Group           string
	Dependencies    []string
	ReadyAt         time.Time
	Service         Scenario
	UnknownDuration bool
}

// Snapshot is an immutable input.  The estimator never mutates its maps,
// slices, jobs or group values.
type Snapshot struct {
	Now            time.Time
	GlobalCapacity int
	Groups         map[string]Group
	Running        []Running
	Jobs           []Job
	Finalization   time.Duration
	Scope          string
}

// Result is safe to publish directly in an execution snapshot.  A nil
// Estimate/RangeHigh means that no upper ETA is known. RangeLow remains useful
// for an adaptive context search blocked by a known cooldown.
type Result struct {
	Scope         string
	MethodVersion string
	State         State
	Estimate      *time.Duration
	RangeLow      *time.Duration
	RangeHigh     *time.Duration
	Reason        string
	UpdatedAt     time.Time
}

// Estimator owns only its clock; every evaluation is otherwise a pure replay.
type Estimator struct{ clock Clock }

func New(clock Clock) *Estimator { return &Estimator{clock: clock} }

// Estimate evaluates low/base/high list schedules with the same global/group
// limits.  It never creates requests or waits. A nil clock is valid when a
// snapshot supplies Now; it is rejected only if no timestamp is available.
func (e *Estimator) Estimate(in Snapshot) (Result, error) {
	now := in.Now
	if now.IsZero() && e != nil && e.clock != nil {
		now = e.clock.Now()
	}
	if now.IsZero() {
		return Result{}, fmt.Errorf("eta: snapshot Now is required")
	}
	result := Result{Scope: in.Scope, MethodVersion: MethodVersion, UpdatedAt: now}
	if in.GlobalCapacity < 1 {
		return Result{}, fmt.Errorf("eta: global capacity must be positive")
	}
	if in.Finalization < 0 {
		return Result{}, fmt.Errorf("eta: finalization must not be negative")
	}
	if len(in.Jobs) == 0 && len(in.Running) == 0 {
		if in.Finalization > 0 {
			v := in.Finalization
			result.State, result.Estimate, result.RangeLow, result.RangeHigh = Finalizing, &v, &v, &v
			return result, nil
		}
		zero := time.Duration(0)
		result.State, result.Estimate, result.RangeLow, result.RangeHigh = Complete, &zero, &zero, &zero
		return result, nil
	}
	for id, g := range in.Groups {
		if id == "" || g.Capacity < 1 || g.StartInterval < 0 {
			return Result{}, fmt.Errorf("eta: invalid group %q", id)
		}
	}
	for _, j := range in.Jobs {
		if j.ID == "" || j.Group == "" {
			return Result{}, fmt.Errorf("eta: job ID and group are required")
		}
		if _, ok := in.Groups[j.Group]; !ok {
			return Result{}, fmt.Errorf("eta: job %q references unknown group %q", j.ID, j.Group)
		}
		if !j.UnknownDuration && (j.Service.Low < 0 || j.Service.Base < 0 || j.Service.High < 0) {
			return Result{}, fmt.Errorf("eta: negative service for %q", j.ID)
		}
	}
	for _, running := range in.Running {
		if running.Group == "" {
			return Result{}, fmt.Errorf("eta: running request %q has no group", running.ID)
		}
		if _, ok := in.Groups[running.Group]; !ok {
			return Result{}, fmt.Errorf("eta: running request %q references unknown group %q", running.ID, running.Group)
		}
		if running.Remaining != nil && *running.Remaining < 0 {
			return Result{}, fmt.Errorf("eta: negative remaining service for %q", running.ID)
		}
	}

	lower, err := simulate(in, func(s Scenario) time.Duration { return s.Low }, true)
	if err != nil {
		return Result{}, err
	}
	if hasUnknown(in) {
		result.State, result.RangeLow, result.Reason = LowerBound, durationPtr(nonNegative(lower.Sub(now))), "work duration unknown"
		return result, nil
	}
	base, err := simulate(in, func(s Scenario) time.Duration { return s.Base }, false)
	if err != nil {
		return Result{}, err
	}
	high, err := simulate(in, func(s Scenario) time.Duration { return s.High }, false)
	if err != nil {
		return Result{}, err
	}
	result.State = Estimated
	result.Estimate = durationPtr(nonNegative(base.Sub(now)))
	result.RangeLow = durationPtr(nonNegative(lower.Sub(now)))
	result.RangeHigh = durationPtr(nonNegative(high.Sub(now)))
	return result, nil
}

func hasUnknown(in Snapshot) bool {
	for _, r := range in.Running {
		if r.Remaining == nil {
			return true
		}
	}
	for _, j := range in.Jobs {
		if j.UnknownDuration {
			return true
		}
	}
	return false
}
func durationPtr(v time.Duration) *time.Duration { return &v }
func nonNegative(v time.Duration) time.Duration {
	if v < 0 {
		return 0
	}
	return v
}

type slot struct {
	at    time.Time
	group string
}
type jobState struct {
	job       Job
	done      time.Time
	scheduled bool
}

func simulate(in Snapshot, service func(Scenario) time.Duration, lowerOnly bool) (time.Time, error) {
	now := in.Now
	groupSlots := map[string][]time.Time{}
	next := map[string]time.Time{}
	for id, g := range in.Groups {
		n := maxTime(now, g.NextDispatch, g.Cooldown)
		next[id] = n
		groupSlots[id] = make([]time.Time, 0, g.Capacity)
	}
	global := make([]time.Time, 0, in.GlobalCapacity)
	for _, r := range in.Running {
		remaining := time.Duration(0)
		if r.Remaining != nil {
			remaining = nonNegative(*r.Remaining)
		}
		// Unknown running work is a lower-bound of its known elapsed gate: it
		// still occupies a slot only until now in the lower-bound schedule.
		finish := now.Add(remaining)
		global = append(global, finish)
		if _, ok := groupSlots[r.Group]; ok {
			groupSlots[r.Group] = append(groupSlots[r.Group], finish)
		}
	}
	states := make(map[string]*jobState, len(in.Jobs))
	for _, j := range in.Jobs {
		if _, exists := states[j.ID]; exists {
			return time.Time{}, fmt.Errorf("eta: duplicate job %q", j.ID)
		}
		states[j.ID] = &jobState{job: j}
	}
	remaining := len(states)
	last := now
	for remaining > 0 {
		var candidate *jobState
		var candidateStart time.Time
		for _, id := range sortedJobIDs(states) {
			s := states[id]
			if s.scheduled || !dependenciesDone(s, states) {
				continue
			}
			g := in.Groups[s.job.Group]
			ready := maxTime(now, s.job.ReadyAt, dependencyFinish(s, states), next[s.job.Group])
			start := maxTime(ready, nthAvailable(global, in.GlobalCapacity, now), nthAvailable(groupSlots[s.job.Group], g.Capacity, now))
			// List scheduling chooses the earliest dispatchable job. Stable IDs
			// make equally eligible work deterministic without serializing
			// independent groups behind an arbitrary map/order traversal.
			if candidate == nil || start.Before(candidateStart) {
				candidate, candidateStart = s, start
			}
		}
		if candidate == nil {
			return time.Time{}, fmt.Errorf("eta: cyclic or missing job dependency")
		}
		g := in.Groups[candidate.job.Group]
		start := candidateStart
		d := service(candidate.job.Service)
		if lowerOnly && candidate.job.UnknownDuration {
			d = 0
		}
		finish := start.Add(nonNegative(d))
		global = addSlot(global, finish, in.GlobalCapacity)
		groupSlots[candidate.job.Group] = addSlot(groupSlots[candidate.job.Group], finish, g.Capacity)
		next[candidate.job.Group] = start.Add(g.StartInterval)
		candidate.done, candidate.scheduled = finish, true
		if finish.After(last) {
			last = finish
		}
		remaining--
	}
	// Running requests can be the only remaining work.
	for _, at := range global {
		if at.After(last) {
			last = at
		}
	}
	return last.Add(in.Finalization), nil
}

func sortedJobIDs(states map[string]*jobState) []string {
	ids := make([]string, 0, len(states))
	for id := range states {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
func dependenciesDone(s *jobState, states map[string]*jobState) bool {
	for _, id := range s.job.Dependencies {
		d, ok := states[id]
		if !ok || !d.scheduled {
			return false
		}
	}
	return true
}
func dependencyFinish(s *jobState, states map[string]*jobState) time.Time {
	var out time.Time
	for _, id := range s.job.Dependencies {
		if d := states[id]; d.done.After(out) {
			out = d.done
		}
	}
	return out
}
func maxTime(values ...time.Time) time.Time {
	out := values[0]
	for _, v := range values[1:] {
		if v.After(out) {
			out = v
		}
	}
	return out
}
func nthAvailable(slots []time.Time, capacity int, now time.Time) time.Time {
	if len(slots) < capacity {
		return now
	}
	out := slots[0]
	for _, at := range slots[1:] {
		if at.Before(out) {
			out = at
		}
	}
	return maxTime(now, out)
}
func addSlot(slots []time.Time, finish time.Time, capacity int) []time.Time {
	if len(slots) < capacity {
		return append(slots, finish)
	}
	idx := 0
	for i := 1; i < len(slots); i++ {
		if slots[i].Before(slots[idx]) {
			idx = i
		}
	}
	slots[idx] = finish
	return slots
}

// RoundDisplay follows the public terminal contract. It does not alter the
// stored estimate: countdowns and elapsed values can still be rendered per s.
func RoundDisplay(d time.Duration) time.Duration {
	d = nonNegative(d)
	if d < 2*time.Minute {
		return d.Round(time.Second)
	}
	if d <= 10*time.Minute {
		return d.Round(5 * time.Second)
	}
	return d.Round(30 * time.Second)
}
