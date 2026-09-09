package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rivo/uniseg"
)

func sample() Snapshot {
	return Snapshot{
		RunID: "run-7f1a", Phase: "running", Elapsed: "09:03", ETA: "~01:18",
		JobsDone: 27, JobsTotal: 30, Inflight: 1, Concurrency: 16, RPS: 0.1,
		HTTPDone: 0, HTTPTotal: 16, TargetsDone: 2, TargetsTotal: 3,
		Rows: []Target{
			{ID: "anthropic", State: "finished", JobsDone: 10, JobsTotal: 10, Verdict: "pass"},
			{ID: "google-genai", State: "finished", JobsDone: 8, JobsTotal: 8, Verdict: "pass-with-gaps"},
			{ID: "openai", State: "throttled", JobsDone: 9, JobsTotal: 12, Detail: "Retry-After: resumes in 01:01; waited 00:08"},
		},
	}
}

func TestGoldenFrames(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		ascii         bool
	}{
		{"120", 120, 24, false}, {"110", 110, 24, false}, {"80", 80, 24, false},
		{"60", 60, 12, false}, {"48", 48, 12, false}, {"ascii", 80, 24, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Render(sample(), tc.width, tc.height, Options{ASCII: tc.ascii}).String() + "\n"
			path := filepath.Join("testdata", tc.name+".golden")
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("golden mismatch (-want +got):\nwant:\n%s\ngot:\n%s", want, got)
			}
			for _, line := range Render(sample(), tc.width, tc.height, Options{ASCII: tc.ascii}).Lines {
				if uniseg.StringWidth(line) > tc.width-1 {
					t.Fatalf("line width %d > %d: %q", uniseg.StringWidth(line), tc.width-1, line)
				}
			}
		})
	}
}

func TestSanitizesControlsAndUsesCellWidths(t *testing.T) {
	s := sample()
	s.Rows[0].ID = "very-long-界\u0301\x1b[31m\u202eevil-openai-model"
	f := Render(s, 48, 12, Options{})
	for _, line := range f.Lines {
		if strings.ContainsAny(line, "\x1b\u202e\r\n") {
			t.Fatalf("unsafe frame line %q", line)
		}
		if uniseg.StringWidth(line) > 47 {
			t.Fatalf("autowrap-prone line %q", line)
		}
	}
}

func TestTerminalOutcomesStayDistinct(t *testing.T) {
	s := sample()
	s.Rows = []Target{{ID: "provider", State: "finished", JobsDone: 1, JobsTotal: 1, Verdict: "fail"}, {ID: "runtime", State: "failed", JobsDone: 1, JobsTotal: 1}, {ID: "user", State: "interrupted", JobsDone: 0, JobsTotal: 1}}
	got := Render(s, 120, 24, Options{}).String()
	for _, want := range []string{"FINISHED", "FAIL", "FAILED", "INTERRUPTED"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "COMPLETED") {
		t.Fatalf("ambiguous completed state in %q", got)
	}
}

func TestRunLabelNamespacesOpaqueRunID(t *testing.T) {
	for input, want := range map[string]string{
		"run-fd37a7f229c990d5f80035ed0b9dbf15": "aiqa-run-fd37a7f229c990d5f80035ed0b9dbf15",
		"aiqa-existing":                        "aiqa-existing",
		"":                                     "aiqa",
	} {
		if got := runLabel(input); got != want {
			t.Fatalf("runLabel(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestModeSelection(t *testing.T) {
	cases := []struct {
		name      string
		requested Mode
		tty       bool
		env       map[string]string
		want      Mode
		fallback  bool
	}{
		{"tty", ModeAuto, true, nil, ModeTTY, false},
		{"redirected stderr", ModeAuto, false, nil, ModePlain, false},
		{"forced tty redirected", ModeTTY, false, nil, ModePlain, true},
		{"term dumb", ModeAuto, true, map[string]string{"TERM": "dumb"}, ModePlain, false},
		{"ci", ModeAuto, true, map[string]string{"CI": "1"}, ModePlain, false},
		{"plain", ModePlain, true, nil, ModePlain, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, fallback := SelectMode(tc.requested, tc.tty, tc.env)
			if got != tc.want || fallback != tc.fallback {
				t.Fatalf("got %s/%t want %s/%t", got, fallback, tc.want, tc.fallback)
			}
		})
	}
}

func TestHeightRetainsHeaderAndCriticalFirst(t *testing.T) {
	s := sample()
	s.Rows[0].Detail = "critical error"
	f := Render(s, 60, 2, Options{})
	if len(f.Lines) != 2 || !strings.HasPrefix(f.Lines[0], "AIQA") {
		t.Fatalf("unexpected short frame %#v", f.Lines)
	}
}

func TestPreparingDoesNotInventProgressDenominator(t *testing.T) {
	s := sample()
	s.Phase, s.JobsDone, s.JobsTotal = "preparing", 0, 0
	got := Render(s, 80, 12, Options{}).String()
	if strings.Contains(got, "0/0") || strings.Contains(got, "100%") {
		t.Fatalf("planning frame invented progress: %q", got)
	}
	if !strings.Contains(got, "total is not known yet") {
		t.Fatalf("missing planning caveat: %q", got)
	}
}
