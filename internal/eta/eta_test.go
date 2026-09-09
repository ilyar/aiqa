package eta

import (
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (c fakeClock) Now() time.Time { return c.now }

func TestReplayScenariosAThroughD(t *testing.T) {
	now := time.Unix(0, 0)
	makeJobs := func(groups []string, n int, service time.Duration) []Job {
		var jobs []Job
		for _, g := range groups {
			for i := 0; i < n; i++ {
				jobs = append(jobs, Job{ID: g + string(rune('a'+i)), Group: g, Service: Scenario{Low: service, Base: service, High: service}})
			}
		}
		return jobs
	}
	cases := []struct {
		name string
		in   Snapshot
		want time.Duration
	}{
		{"A independent", Snapshot{Now: now, GlobalCapacity: 16, Groups: map[string]Group{"a": {ID: "a", Capacity: 1}, "b": {ID: "b", Capacity: 1}, "c": {ID: "c", Capacity: 1}}, Jobs: makeJobs([]string{"a", "b", "c"}, 10, time.Second)}, 10 * time.Second},
		{"B global contention", Snapshot{Now: now, GlobalCapacity: 1, Groups: map[string]Group{"a": {ID: "a", Capacity: 1}, "b": {ID: "b", Capacity: 1}, "c": {ID: "c", Capacity: 1}}, Jobs: makeJobs([]string{"a", "b", "c"}, 10, time.Second)}, 30 * time.Second},
		{"C serial cooldown", Snapshot{Now: now, GlobalCapacity: 16, Groups: map[string]Group{"a": {ID: "a", Capacity: 1, StartInterval: 4 * time.Second, Cooldown: now.Add(61 * time.Second)}}, Jobs: makeJobs([]string{"a"}, 3, 5*time.Second), Finalization: 2 * time.Second}, 78 * time.Second},
		{"D paced concurrency", Snapshot{Now: now, GlobalCapacity: 16, Groups: map[string]Group{"a": {ID: "a", Capacity: 3, StartInterval: 4 * time.Second, Cooldown: now.Add(61 * time.Second)}}, Jobs: makeJobs([]string{"a"}, 3, 5*time.Second), Finalization: 2 * time.Second}, 76 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New(fakeClock{now}).Estimate(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != Estimated || got.Estimate == nil || *got.Estimate != tc.want {
				var actual time.Duration
				if got.Estimate != nil {
					actual = *got.Estimate
				}
				t.Fatalf("got state=%s estimate=%s want %s", got.State, actual, tc.want)
			}
		})
	}
}

func TestUnknownHasCooldownLowerBound(t *testing.T) {
	now := time.Unix(0, 0)
	got, err := New(fakeClock{now}).Estimate(Snapshot{Now: now, GlobalCapacity: 1, Groups: map[string]Group{"a": {ID: "a", Capacity: 1, Cooldown: now.Add(61 * time.Second)}}, Jobs: []Job{{ID: "adaptive", Group: "a", UnknownDuration: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != LowerBound || got.Estimate != nil || got.RangeLow == nil || *got.RangeLow != 61*time.Second {
		t.Fatalf("got %#v", got)
	}
}

func TestCooldownIsSharedAndChangesImmediately(t *testing.T) {
	now := time.Unix(0, 0)
	in := Snapshot{Now: now, GlobalCapacity: 8, Groups: map[string]Group{"a": {ID: "a", Capacity: 3, Cooldown: now.Add(time.Minute)}}, Jobs: []Job{{ID: "a", Group: "a", Service: Scenario{Base: time.Second, Low: time.Second, High: time.Second}}, {ID: "b", Group: "a", Service: Scenario{Base: time.Second, Low: time.Second, High: time.Second}}}}
	got, _ := New(fakeClock{now}).Estimate(in)
	if *got.Estimate != 61*time.Second {
		t.Fatalf("shared cooldown got %s", *got.Estimate)
	}
	in.Groups["a"] = Group{ID: "a", Capacity: 3, Cooldown: now.Add(2 * time.Minute)}
	got, _ = New(fakeClock{now}).Estimate(in)
	if *got.Estimate != 121*time.Second {
		t.Fatalf("changed cooldown got %s", *got.Estimate)
	}
}

func TestNoNegativeAndRunningUnknownDoesNotBecomeZeroEstimate(t *testing.T) {
	now := time.Unix(0, 0)
	got, err := New(nil).Estimate(Snapshot{Now: now, GlobalCapacity: 1, Groups: map[string]Group{"a": {ID: "a", Capacity: 1, Cooldown: now.Add(-time.Second)}}, Running: []Running{{ID: "r", Group: "a", Remaining: nil}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != LowerBound || got.RangeLow == nil || *got.RangeLow < 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestServiceSamplesExcludeNonServiceWorkAndActiveOldRequestIsUnknown(t *testing.T) {
	got := ServiceFromSamples([]time.Duration{5 * time.Second, time.Second, 3 * time.Second, 2 * time.Second, 4 * time.Second, 6 * time.Second, 7 * time.Second, 8 * time.Second})
	if got.Evidence != "sufficient" || got.Scenario.Base != 4*time.Second {
		t.Fatalf("unexpected sample estimate: %#v", got)
	}
	if got := ServiceFromSamples([]time.Duration{-time.Second}); got.Evidence != "none" {
		t.Fatalf("negative/non-service input must not create throughput: %#v", got)
	}
}

func TestRoundDisplay(t *testing.T) {
	if got := RoundDisplay(2*time.Minute + 3*time.Second); got != 2*time.Minute+5*time.Second {
		t.Fatalf("%s", got)
	}
	if got := RoundDisplay(11*time.Minute + 10*time.Second); got != 11*time.Minute {
		t.Fatalf("%s", got)
	}
}
