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
	if !strings.Contains(got, "openai 2/2 100%") {
		t.Fatalf("completion = %q", got)
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
