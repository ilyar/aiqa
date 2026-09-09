package progress

import (
	"bytes"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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

func TestFreezeKeepsLongOpaqueIDsDistinctWhileTruncatingOnlyDisplayLabels(t *testing.T) {
	var out bytes.Buffer
	r := New(Options{Writer: &out})
	t.Cleanup(r.Close)

	prefix := strings.Repeat("x", 128)
	ids := []string{
		strings.Repeat("a", 97),
		strings.Repeat("b", 128),
		strings.Repeat("c", 512),
		prefix + "-one",
		prefix + "-two",
		"модель/" + strings.Repeat("界", 40) + "/one",
		"модель/" + strings.Repeat("界", 40) + "/two",
	}
	jobs := make([]Job, 0, len(ids))
	for _, id := range ids {
		jobs = append(jobs, Job{ID: id, Target: "target", Label: strings.Repeat("界", 40)})
	}
	r.Freeze(jobs)
	for _, id := range ids {
		r.Start(id)
		r.Complete(id, "pass")
	}

	r.mu.Lock()
	total, done := r.total, r.done
	for _, id := range ids {
		if _, ok := r.jobs[id]; !ok {
			r.mu.Unlock()
			t.Fatalf("raw job ID was altered or truncated: %q", id)
		}
	}
	label := r.jobs[ids[0]].Label
	r.mu.Unlock()
	if total != len(ids) || done != len(ids) {
		t.Fatalf("opaque long IDs plan = %d/%d, want %d/%d", done, total, len(ids), len(ids))
	}
	if len(label) > 96 || !utf8.ValidString(label) {
		t.Fatalf("display label = %q: want valid UTF-8 at most 96 bytes", label)
	}
}

func TestUnknownAndConflictingTerminalEventsDoNotMutateFrozenPlan(t *testing.T) {
	var out bytes.Buffer
	r := New(Options{Writer: &out})
	t.Cleanup(r.Close)
	r.Freeze([]Job{{ID: "job"}, {ID: "requirement"}})

	r.Start("unknown")
	r.Complete("unknown", "pass")
	r.Complete("job", "pass")
	r.Complete("job", "pass") // same terminal outcome is idempotent
	r.Complete("job", "fail") // conflict must not overwrite the original
	r.Complete("requirement", "skipped")

	r.mu.Lock()
	total, done := r.total, r.done
	jobOutcome, requirementOutcome := r.completed["job"], r.completed["requirement"]
	r.mu.Unlock()
	if total != 2 || done != 2 {
		t.Fatalf("invalid lifecycle events mutated plan = %d/%d, want 2/2", done, total)
	}
	if jobOutcome != "pass" || requirementOutcome != "skipped" {
		t.Fatalf("terminal outcomes = job:%q requirement:%q", jobOutcome, requirementOutcome)
	}
	for _, want := range []string{
		"progress diagnostic: unknown job start",
		"progress diagnostic: unknown job completion",
		"progress diagnostic: conflicting job completion (pass != fail)",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing diagnostic %q in %q", want, out.String())
		}
	}
}

func TestTTYSwitchesPlanningLineToBuiltInBarAndFinalizes(t *testing.T) {
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
		t.Fatalf("fixed progress bar missing job/count: %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("TTY output was not finalized: %q", got)
	}
}

func TestTTYCompletionTerminatesBeforeNextMessage(t *testing.T) {
	var out bytes.Buffer
	r := New(Options{Writer: &out, TTY: true, SpinnerInterval: time.Hour})
	t.Cleanup(r.Close)
	r.Freeze([]Job{{ID: "one", Target: "openai", Label: "one"}})
	r.Start("one")
	r.Complete("one", "pass")
	r.Message("completed; reports: report.json")
	if !strings.Contains(out.String(), "\naiqa: completed; reports: report.json\n") {
		t.Fatalf("completion and message share a terminal line: %q", out.String())
	}
}

func TestTTYFreezeDoesNotNeedBackgroundSpinner(t *testing.T) {
	var out bytes.Buffer
	r := New(Options{Writer: &out, TTY: true})
	t.Cleanup(r.Close)
	r.RunStarted(1)
	beforeFreeze := out.Len()
	r.Freeze([]Job{{ID: "one", Target: "openai", Label: "one"}})
	afterFreeze := out.Len()
	if afterFreeze <= beforeFreeze {
		t.Fatalf("freeze did not render determinate state: bytes %d -> %d", beforeFreeze, afterFreeze)
	}
	if r.ttyRendered != true {
		t.Fatal("TTY line was not rendered")
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
