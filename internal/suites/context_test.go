package suites

import (
	"math"
	"testing"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/result"
)

func TestHasAcceptedContextSample(t *testing.T) {
	samples := []result.ContextSample{
		{Accepted: true},
		{Accepted: false, Overflow: true},
	}
	if !hasAcceptedContextSample(samples) {
		t.Fatal("an earlier accepted sample must keep later size-only 4xx responses classifiable as overflow")
	}
	if hasAcceptedContextSample([]result.ContextSample{{Accepted: false}}) {
		t.Fatal("rejected samples alone must not establish a context boundary")
	}
}

func TestContextChargeNeverTrustsUnderreportedUsage(t *testing.T) {
	const payload = 3000
	floor := conservativeContextCharge(payload)
	if got := maxContextCharge(result.ContextSample{ReportedPromptTokens: 1}, payload); got != floor {
		t.Fatalf("underreported charge = %d, want conservative floor %d", got, floor)
	}
	if got := estimateTokensForBytes(payload, []result.ContextSample{{Accepted: true, PayloadBytes: payload, ReportedPromptTokens: 1}}); got != floor {
		t.Fatalf("learned estimate = %d, want floor %d", got, floor)
	}
}

func TestContextBudgetStopsBeforeFirstRequest(t *testing.T) {
	env := Environment{Config: config.Config{ContextBudget: 10, ContextRequestBudget: 1, ContextByteBudget: 1024}}
	if got := contextBudgetStop(env, 0, 0, 0, conservativeContextCharge(1024), 1024); got != "context probe stopped at the configured token budget" {
		t.Fatalf("budget stop = %q", got)
	}
}

func TestContextBudgetHasIndependentByteAndRequestLimits(t *testing.T) {
	env := Environment{Config: config.Config{ContextBudget: 999999, ContextRequestBudget: 1, ContextByteBudget: 1024}}
	if got := contextBudgetStop(env, 0, 0, 1, 1, 1); got != "context probe stopped at the configured request budget" {
		t.Fatalf("request stop = %q", got)
	}
	if got := contextBudgetStop(env, 0, 1024, 0, 1, 1); got != "context probe stopped at the configured cumulative byte budget" {
		t.Fatalf("byte stop = %q", got)
	}
}

func TestContextBudgetRejectsOverflowingTokenCharge(t *testing.T) {
	env := Environment{Config: config.Config{ContextBudget: 100, ContextRequestBudget: 1, ContextByteBudget: 1024}}
	if got := contextBudgetStop(env, math.MaxInt64, 0, 0, 1, 1); got != "context probe stopped at the configured token budget" {
		t.Fatalf("overflowing budget stop = %q", got)
	}
}

func TestContextBinarySearchAlignmentRefinesSmallWindows(t *testing.T) {
	if got := alignBytes(1536, 256); got != 1536 {
		t.Fatalf("small-window midpoint=%d, want 1536", got)
	}
	if got := alignBytes(1536, 1024); got != 1024 {
		t.Fatalf("sanity check: coarse midpoint=%d, want 1024", got)
	}
}
