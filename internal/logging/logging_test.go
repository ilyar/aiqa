package logging

import (
	"reflect"
	"sync"
	"testing"
)

func TestLoggerThresholdsAndFormatting(t *testing.T) {
	logger := New(2)
	if logger.Logf(Debug, "hidden") {
		t.Fatal("debug line was not filtered")
	}
	for _, level := range []Level{Error, Fatal, Warn, Info} {
		if !logger.Logf(level, "%s event", level) {
			t.Fatalf("%s was unexpectedly filtered", level)
		}
	}
	want := []string{"[ERROR] ERROR event", "[FATAL] FATAL event", "[WARN] WARN event", "[INFO] INFO event"}
	if got := logger.Lines(); !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %v, want %v", got, want)
	}
}

func TestLoggerVerbosityBoundaries(t *testing.T) {
	for _, test := range []struct {
		verbosity int
		enabled   []Level
	}{
		{0, []Level{Error, Fatal}},
		{1, []Level{Error, Fatal, Warn}},
		{2, []Level{Error, Fatal, Warn, Info}},
		{3, []Level{Error, Fatal, Warn, Info, Debug}},
		{4, []Level{Error, Fatal, Warn, Info, Debug, Trace}},
	} {
		logger := New(test.verbosity)
		for _, level := range []Level{Trace, Debug, Info, Warn, Error, Fatal} {
			want := false
			for _, candidate := range test.enabled {
				want = want || level == candidate
			}
			if got := logger.Enabled(level); got != want {
				t.Fatalf("verbosity %d: %s enabled = %v, want %v", test.verbosity, level, got, want)
			}
		}
	}
}

func TestLoggerIsConcurrent(t *testing.T) {
	logger := New(4)
	const count = 50
	var group sync.WaitGroup
	for index := 0; index < count; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			logger.Logf(Trace, "event %d", index)
		}(index)
	}
	group.Wait()
	if got := len(logger.Lines()); got != count {
		t.Fatalf("line count = %d, want %d", got, count)
	}
}
