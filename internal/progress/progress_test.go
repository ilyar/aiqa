package progress

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func plan() []Job {
	return []Job{
		{ID: "openai/chat", Target: "openai", Model: "gpt-test", Label: "gpt-test:chat.basic"},
		{ID: "openai/tools", Target: "openai", Model: "gpt-test", Label: "gpt-test:tools.basic"},
		{ID: "anthropic/chat", Target: "anthropic", Model: "claude-test", Label: "claude-test:chat.basic"},
	}
}

func TestNonTTYPlanningAndFrozenPlanUseOnlyDurableLines(t *testing.T) {
	var out bytes.Buffer
	r := New(Options{Writer: &out})
	t.Cleanup(r.Close)
	r.RunStarted(2)
	r.DiscoveryStarted("openai")
	r.Plan("openai", 2)
	r.DiscoveryStarted("anthropic")
	r.Plan("anthropic", 1)
	r.Freeze(plan())
	r.Start("openai/chat")
	r.Complete("openai/chat", "pass")
	r.Complete("openai/chat", "pass") // duplicate terminal result
	r.Complete("openai/tools", "skipped")

	got := out.String()
	if strings.Contains(got, "\r") || strings.Contains(got, "\x1b") {
		t.Fatalf("non-TTY output contains terminal control: %q", got)
	}
	for _, want := range []string{
		"aiqa: Planning 2 target(s)",
		"aiqa: Discovering openai",
		"aiqa: Planned 3 logical probe(s)",
		"aiqa: 1/3 33%  gpt-test:chat.basic",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "3/3 100%") {
		t.Fatalf("unknown or duplicate completion advanced fixed plan: %q", got)
	}
}

func TestFreezeRejectsLatePlanRevisionsAndCompletionCannotExceedTotal(t *testing.T) {
	var out bytes.Buffer
	r := New(Options{Writer: &out})
	t.Cleanup(r.Close)
	r.Plan("openai", 3)
	r.Freeze(plan()[:2])
	r.RevisePlan("openai", 9)
	r.AddPlan("anthropic", 7)
	r.Freeze([]Job{{ID: "unexpected"}})
	r.Complete("openai/chat", "pass")
	r.Complete("openai/tools", "skipped")
	r.Complete("anthropic/chat", "pass")

	r.mu.Lock()
	total, done := r.total, r.done
	r.mu.Unlock()
	if total != 2 || done != 2 {
		t.Fatalf("frozen plan = %d/%d, want 2/2", done, total)
	}
	if strings.Contains(out.String(), "9 logical") || strings.Contains(out.String(), "unexpected") {
		t.Fatalf("late revision was rendered: %q", out.String())
	}
}

func TestFreezeDeduplicatesUnusableJobs(t *testing.T) {
	var out bytes.Buffer
	r := New(Options{Writer: &out})
	t.Cleanup(r.Close)
	r.Freeze([]Job{{}, {ID: "one"}, {ID: "one"}, {ID: "two"}})
	r.Complete("one", "pass")
	r.Complete("two", "skipped")
	r.mu.Lock()
	total, done := r.total, r.done
	r.mu.Unlock()
	if total != 2 || done != 2 {
		t.Fatalf("sanitized plan = %d/%d, want 2/2", done, total)
	}
	if !strings.Contains(out.String(), "Planned 2 logical probe(s)") {
		t.Fatalf("plan output = %q", out.String())
	}
}

func TestTTYSwitchesSpinnerToOneLibraryBarAndFinalizes(t *testing.T) {
	var out bytes.Buffer
	r := New(Options{Writer: &out, TTY: true, SpinnerInterval: time.Hour})
	r.RunStarted(1)
	r.DiscoveryStarted("openai")
	r.Freeze(plan()[:2])
	r.Start("openai/chat")
	r.Complete("openai/chat", "pass")
	r.Complete("openai/tools", "pass")
	r.Close()
	got := out.String()
	if !strings.Contains(got, "aiqa: Planning 1 target(s)") {
		t.Fatalf("spinner missing planning status: %q", got)
	}
	if !strings.Contains(got, "gpt-test:chat.basic") || !strings.Contains(got, "2/2") {
		t.Fatalf("fixed progressbar missing job/count: %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("TTY output was not finalized: %q", got)
	}
}

func TestPacingDoesNotCreatePlanOrDuplicateRoutineDurableMessages(t *testing.T) {
	var out bytes.Buffer
	r := New(Options{Writer: &out})
	t.Cleanup(r.Close)
	r.Pacing("openai", true, 10*time.Second) // ignored during planning
	r.Freeze(plan()[:2])
	r.Pacing("openai", true, 2*time.Second)
	r.Pacing("openai", true, 2*time.Second)
	r.Pacing("openai", false, time.Second)
	r.Pacing("openai", false, time.Second)
	if got := out.String(); strings.Count(got, "waiting for provider pacing") != 0 {
		t.Fatalf("routine pacing emitted durable lines: %q", got)
	}
	r.Pacing("openai", true, 8*time.Second)
	if got := out.String(); strings.Count(got, "waiting for provider pacing (8s)") != 1 {
		t.Fatalf("long pacing notice = %q", got)
	}
}
