package protocol

import (
	"testing"

	"github.com/ilyar/aiqa/internal/result"
)

func TestAggregateTestOutcomeUsesStrongestDerivedResult(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []result.Status
		want string
	}{
		{name: "empty", want: "completed"},
		{name: "pass", in: []result.Status{result.StatusPass}, want: "pass"},
		{name: "derived failure", in: []result.Status{result.StatusPass, result.StatusFail}, want: "fail"},
		{name: "execution beats failure", in: []result.Status{result.StatusPass, result.StatusFail, result.StatusError}, want: "error"},
		{name: "skip does not hide warning", in: []result.Status{result.StatusSkip, result.StatusWarn}, want: "warn"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tests := make([]result.TestResult, len(tc.in))
			for i, status := range tc.in {
				tests[i].Status = status
			}
			if got := AggregateTestOutcome(tests); got != tc.want {
				t.Fatalf("AggregateTestOutcome() = %q, want %q", got, tc.want)
			}
		})
	}
}
