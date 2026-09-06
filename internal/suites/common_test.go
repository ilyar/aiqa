package suites

import (
	"testing"

	"github.com/ilyar/aiqa/internal/logging"
)

func TestEnvironmentLogfForwardsCategory(t *testing.T) {
	var gotLevel logging.Level
	var gotMessage string
	env := Environment{Logf: func(level logging.Level, format string, args ...any) {
		gotLevel = level
		gotMessage = format
	}}
	env.logf(logging.Trace, "planning probe")
	if gotLevel != logging.Trace || gotMessage != "planning probe" {
		t.Fatalf("log callback = (%q, %q), want (%q, %q)", gotLevel, gotMessage, logging.Trace, "planning probe")
	}
}
