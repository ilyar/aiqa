// Package terminal renders safe, deterministic operator-progress frames.
//
// It deliberately has no dependency on the runner state.  The execution
// reducer translates its snapshot to Snapshot before calling Render, so a
// terminal refresh can never affect scheduling or accounting.
package terminal

import (
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
	"golang.org/x/term"
)

// Mode is the selected progress transport. Plain is deliberately free of all
// terminal control bytes and is therefore safe for CI and redirected stderr.
type Mode string

const (
	ModeOff   Mode = "off"
	ModeTTY   Mode = "tty"
	ModePlain Mode = "plain"
	ModeAuto  Mode = "auto"
)

// Options changes presentation only. It must not be used for progress state.
type Options struct {
	ASCII bool
}

// Snapshot is the small view model consumed by Render. IDs in it are display
// values only; callers must retain raw IDs for all lookup and accounting.
type Snapshot struct {
	RunID        string
	Phase        string
	Elapsed      string
	ETA          string
	JobsDone     int
	JobsTotal    int
	Inflight     int
	Concurrency  int
	HTTPDone     int
	HTTPTotal    int
	TargetsDone  int
	TargetsTotal int
	Verdict      string
	Detail       string
	Rows         []Target
}

// Target is one stable-order target row. State is runtime state; Verdict is
// the compatibility verdict and must therefore not be conflated with State.
type Target struct {
	ID        string
	State     string
	JobsDone  int
	JobsTotal int
	Verdict   string
	Detail    string
}

// Frame is a pure render result. Lines never contain a trailing newline.
type Frame struct{ Lines []string }

// String returns a newline-delimited frame suitable for a single write.
func (f Frame) String() string { return strings.Join(f.Lines, "\n") }

// Render builds a bounded frame. width is terminal columns; every line is at
// most width-1 cells to avoid terminal autowrap. height <= 0 means unbounded.
func Render(s Snapshot, width, height int, options Options) Frame {
	limit := width - 1
	if limit < 1 {
		return Frame{}
	}
	phase := safeUpper(s.Phase)
	if phase == "" {
		phase = "RUNNING"
	}
	lines := []string{}
	// Before the plan freezes the denominator is intentionally unknown. Do not
	// turn that into a misleading 0/0 or 100% progress indicator.
	if phase == "PREPARING" && s.JobsTotal == 0 {
		lines = append(lines, fit(fmt.Sprintf("AIQA PREPARING | Elapsed %s | ETA estimating", value(s.Elapsed, "--:--")), limit, options.ASCII))
		lines = append(lines, fit("Planning executable work; total is not known yet.", limit, options.ASCII))
		for _, r := range rows(s.Rows) {
			line := fmt.Sprintf("%s %s", middle(clean(r.ID), 20), safeUpper(r.State))
			if r.Detail != "" {
				line += " | " + clean(r.Detail)
			}
			lines = append(lines, fit(line, limit, options.ASCII))
		}
		if height > 0 && len(lines) > height {
			lines = lines[:height]
		}
		return Frame{Lines: lines}
	}
	if width >= 110 {
		lines = append(lines, fit(fmt.Sprintf("AIQA Run %s", clean(s.RunID)), limit, options.ASCII))
		lines = append(lines, fit(fmt.Sprintf("%s Elapsed %s Jobs %d/%d (%d%%)", phase, value(s.Elapsed, "--:--"), s.JobsDone, s.JobsTotal, percent(s.JobsDone, s.JobsTotal)), limit, options.ASCII))
		lines = append(lines, fit(fmt.Sprintf("ETA %s HTTP %d/%d In-flight %d/%d", value(s.ETA, "unknown"), s.HTTPDone, s.HTTPTotal, s.Inflight, s.Concurrency), limit, options.ASCII))
		lines = append(lines, "")
		lines = append(lines, fit("TARGET       STATE      JOBS  VERDICT", limit, options.ASCII))
		for _, r := range rows(s.Rows) {
			lines = append(lines, fit(fmt.Sprintf("%-12s %-10s %-5s %s", middle(clean(r.ID), 12), safeUpper(r.State), fmt.Sprintf("%d/%d", r.JobsDone, r.JobsTotal), targetVerdict(r)), limit, options.ASCII))
			if r.Detail != "" {
				lines = append(lines, fit("  "+clean(r.Detail), limit, options.ASCII))
			}
		}
	} else if width >= 60 {
		lines = append(lines, fit(fmt.Sprintf("AIQA %s  %d/%d jobs (%d%%)", phase, s.JobsDone, s.JobsTotal, percent(s.JobsDone, s.JobsTotal)), limit, options.ASCII))
		lines = append(lines, fit(fmt.Sprintf("Elapsed %s | ETA %s | HTTP %d/%d", value(s.Elapsed, "--:--"), value(s.ETA, "unknown"), s.HTTPDone, s.HTTPTotal), limit, options.ASCII))
		for _, r := range rows(s.Rows) {
			lines = append(lines, fit(fmt.Sprintf("%-14s %-10s %d/%d  %s", middle(clean(r.ID), 14), safeUpper(r.State), r.JobsDone, r.JobsTotal, targetVerdict(r)), limit, options.ASCII))
			if r.Detail != "" {
				lines = append(lines, fit("  "+clean(r.Detail), limit, options.ASCII))
			}
		}
	} else {
		lines = append(lines, fit(fmt.Sprintf("AIQA %d/%d jobs | %d%%", s.JobsDone, s.JobsTotal, percent(s.JobsDone, s.JobsTotal)), limit, options.ASCII))
		lines = append(lines, fit(fmt.Sprintf("Elapsed %s | ETA %s", value(s.Elapsed, "--:--"), value(s.ETA, "unknown")), limit, options.ASCII))
		lines = append(lines, fit(fmt.Sprintf("HTTP %d/%d | targets %d/%d done", s.HTTPDone, s.HTTPTotal, s.TargetsDone, s.TargetsTotal), limit, options.ASCII))
		for _, r := range rows(s.Rows) {
			if r.State == "THROTTLED" || r.State == "FAILED" || r.State == "INTERRUPTED" || r.Detail != "" {
				lines = append(lines, fit(middle(clean(r.ID), 20)+" "+safeUpper(r.State), limit, options.ASCII))
				if r.Detail != "" {
					lines = append(lines, fit(clean(r.Detail), limit, options.ASCII))
				}
			}
		}
	}
	if s.Detail != "" {
		lines = append(lines, fit(clean(s.Detail), limit, options.ASCII))
	}
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	return Frame{Lines: lines}
}

// SelectMode implements the documented stderr policy. interactive describes
// stderr, not stdout; it exists to make selection deterministic in tests.
func SelectMode(requested Mode, interactive bool, env map[string]string) (mode Mode, fallback bool) {
	if requested == ModeOff || requested == ModePlain {
		return requested, false
	}
	if requested != ModeAuto && requested != ModeTTY {
		requested = ModeAuto
	}
	ci := env["CI"] == "true" || env["CI"] == "1"
	if interactive && !ci && env["TERM"] != "dumb" {
		return ModeTTY, false
	}
	return ModePlain, requested == ModeTTY
}

// StderrMode is the production helper. It intentionally checks stderr only.
func StderrMode(requested Mode, env map[string]string) (Mode, bool) {
	return SelectMode(requested, term.IsTerminal(int(os.Stderr.Fd())), env)
}

// rows returns a copy while preserving the configured order supplied by the
// reducer. Terminal state must not cause fast targets to jump around.
func rows(in []Target) []Target { return append([]Target(nil), in...) }
func value(got, fallback string) string {
	if got == "" {
		return fallback
	}
	return clean(got)
}
func percent(done, total int) int {
	if total <= 0 {
		return 0
	}
	return done * 100 / total
}
func targetVerdict(r Target) string {
	if r.Verdict != "" {
		return safeUpper(r.Verdict)
	}
	if r.State == "FINISHED" {
		return "DONE"
	}
	return "--"
}
func safeUpper(s string) string { return strings.ToUpper(clean(s)) }

// clean removes terminal controls, bidi overrides and invalid UTF-8 before
// data can reach stderr. It never attempts to interpret escape sequences.
func clean(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == utf8.RuneError || unicode.IsControl(r) || r == '\u001b' || r == '\u200e' || r == '\u200f' || (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069') {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func fit(s string, cells int, ascii bool) string {
	if ascii {
		s = asciiOnly(s)
	}
	return truncate(s, cells)
}
func asciiOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 32 && r <= 126 {
			b.WriteRune(r)
		} else {
			b.WriteByte('?')
		}
	}
	return b.String()
}

func middle(s string, cells int) string {
	if uniseg.StringWidth(s) <= cells {
		return s
	}
	if cells <= 3 {
		return truncate(s, cells)
	}
	left := truncate(s, (cells-3)/2)
	rightBudget := cells - 3 - uniseg.StringWidth(left)
	var clusters []string
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		clusters = append(clusters, g.Str())
	}
	var right string
	for i := len(clusters) - 1; i >= 0; i-- {
		if uniseg.StringWidth(clusters[i]+right) > rightBudget {
			break
		}
		right = clusters[i] + right
	}
	return left + "..." + right
}

func truncate(s string, cells int) string {
	if cells <= 0 {
		return ""
	}
	var b strings.Builder
	used := 0
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		c := g.Str()
		n := uniseg.StringWidth(c)
		if used+n > cells {
			break
		}
		b.WriteString(c)
		used += n
	}
	return b.String()
}
