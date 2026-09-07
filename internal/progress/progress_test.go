package progress

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/telemetry"
)

func TestNonTTYPlansDeduplicatesAndDoesNotEmitControlSequences(t *testing.T) {
	var out bytes.Buffer
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	r := New(Options{Writer: &out, Now: func() time.Time { return now }, HeartbeatInterval: time.Hour})
	t.Cleanup(r.Close)
	r.RunStarted(1)
	r.DiscoveryStarted("openai")
	r.Plan("openai", 2)
	r.ObserveLifecycle(telemetry.LifecycleEvent{Time: now.Add(time.Second), Name: "probe", Phase: "end", Target: "openai", OperationID: "one"})
	r.ObserveLifecycle(telemetry.LifecycleEvent{Time: now.Add(2 * time.Second), Name: "probe", Phase: "end", Target: "openai", OperationID: "one"})
	r.ObserveLifecycle(telemetry.LifecycleEvent{Time: now.Add(3 * time.Second), Name: "probe", Phase: "end", Target: "openai", OperationID: "two"})
	got := out.String()
	if strings.Contains(got, "\r") || strings.Contains(got, "\x1b") {
		t.Fatalf("non-TTY output contains terminal control: %q", got)
	}
	if !strings.Contains(got, "openai 2/2 known probes completed; preparing next phase") {
		t.Fatalf("intermediate completion = %q", got)
	}
	if strings.Contains(got, "100%") {
		t.Fatalf("target progress claimed 100%% before target completion: %q", got)
	}
	r.TargetFinished("openai", "completed")
	if !strings.Contains(out.String(), "openai: completed — completed (2/2 100%)") {
		t.Fatalf("target completion = %q", out.String())
	}
	if strings.Count(got, "openai 2/2") != 1 {
		t.Fatalf("duplicate terminal lifecycle counted: %q", got)
	}
}

func TestTTYCloseFinalizesLineAndPacingIsVisible(t *testing.T) {
	var out bytes.Buffer
	r := New(Options{Writer: &out, TTY: true, HeartbeatInterval: time.Hour})
	r.RunStarted(1)
	r.DiscoveryStarted("google-genai")
	r.Plan("google-genai", 4)
	r.Pacing("google-genai", true, 18*time.Second)
	r.Close()
	got := out.String()
	if !strings.Contains(got, "waiting for provider pacing (18s)") {
		t.Fatalf("pacing state = %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("TTY line was not finalized: %q", got)
	}
}

func TestPacingAggregatesConcurrentWaitersWithoutIntermediateRenders(t *testing.T) {
	var out bytes.Buffer
	r := New(Options{Writer: &out, TTY: false, HeartbeatInterval: time.Hour})
	t.Cleanup(r.Close)
	r.Plan("openai", 10)
	r.ObserveLifecycle(telemetry.LifecycleEvent{Name: "probe", Phase: "start", Target: "openai", Model: "gpt-test", Probe: "chat.basic"})
	out.Reset()

	r.Pacing("openai", true, 18*time.Second)
	afterFirstWait := out.String()
	if !strings.Contains(afterFirstWait, "openai 0/10 0%") || !strings.Contains(afterFirstWait, "gpt-test:chat.basic") || !strings.Contains(afterFirstWait, "waiting for provider pacing (18s)") {
		t.Fatalf("pacing status did not preserve progress context: %q", afterFirstWait)
	}
	r.Pacing("openai", true, 4*time.Second)
	r.Pacing("openai", false, time.Second)
	if got := out.String(); got != afterFirstWait {
		t.Fatalf("intermediate pacing event rendered output: %q", got)
	}
	r.mu.Lock()
	waiters := r.targets["openai"].pacingWaiters
	r.mu.Unlock()
	if got := waiters; got != 1 {
		t.Fatalf("pacing waiters=%d, want 1", got)
	}

	r.Pacing("openai", false, 2*time.Second)
	got := out.String()
	if strings.Count(got, "\n") != 2 {
		t.Fatalf("pacing transitions rendered %d lines, want 2: %q", strings.Count(got, "\n"), got)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if strings.Contains(lines[1], "waiting for provider pacing") || !strings.Contains(lines[1], "gpt-test:chat.basic") {
		t.Fatalf("pacing clear did not restore progress context: %q", lines[1])
	}
}

func TestTTYPacingRendersOnlyAggregateTransitions(t *testing.T) {
	var out bytes.Buffer
	r := New(Options{Writer: &out, TTY: true, HeartbeatInterval: time.Hour})
	t.Cleanup(r.Close)
	r.Plan("anthropic", 1)
	out.Reset()

	r.Pacing("anthropic", true, 2*time.Second)
	first := out.String()
	r.Pacing("anthropic", true, time.Second)
	r.Pacing("anthropic", false, time.Second)
	if got := out.String(); got != first {
		t.Fatalf("intermediate TTY pacing event rendered output: %q", got)
	}
	r.Pacing("anthropic", false, time.Second)
	if got := strings.Count(out.String(), "\r"); got != 2 {
		t.Fatalf("TTY pacing transitions rendered %d redraws, want 2: %q", got, out.String())
	}
}

func TestPlanRevisionNeverMovesTotalBelowCompleted(t *testing.T) {
	var out bytes.Buffer
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	r := New(Options{Writer: &out, Now: func() time.Time { return now }, HeartbeatInterval: time.Hour})
	t.Cleanup(r.Close)
	r.Plan("anthropic", 2)
	r.ObserveLifecycle(telemetry.LifecycleEvent{Time: now.Add(time.Second), Name: "probe", Phase: "end", Target: "anthropic", OperationID: "a"})
	r.ObserveLifecycle(telemetry.LifecycleEvent{Time: now.Add(2 * time.Second), Name: "probe", Phase: "end", Target: "anthropic", OperationID: "b"})
	r.RevisePlan("anthropic", 1)
	if strings.Contains(out.String(), "2/1") {
		t.Fatalf("invalid count after revision: %q", out.String())
	}
	if !strings.Contains(out.String(), "plan revised: 2 -> 2 probes") {
		t.Fatalf("revision was not clamped: %q", out.String())
	}
}

func TestProbeStartShowsSafeCurrentModelAndProbe(t *testing.T) {
	var out bytes.Buffer
	r := New(Options{Writer: &out, TTY: true, HeartbeatInterval: time.Hour})
	t.Cleanup(r.Close)
	r.Plan("openai", 1)
	r.ObserveLifecycle(telemetry.LifecycleEvent{Name: "probe", Phase: "start", Target: "openai", Model: "gpt-test", Probe: "chat.basic\nsecret"})
	got := out.String()
	if !strings.Contains(got, "gpt-test:chat.basicsecret") || strings.Contains(got, "\nsecret") {
		t.Fatalf("unsafe or missing current probe label: %q", got)
	}
}
