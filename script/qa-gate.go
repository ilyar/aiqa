//go:build ignore

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const gremlinsVersion = "v0.6.0"
const installHint = "go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0"

var gremlinsBuildInfo = regexp.MustCompile(`(?m)^\s*mod\s+github\.com/go-gremlins/gremlins\s+v0\.6\.0(?:\s|$)`)

type thresholds struct {
	MinCoverage float64 `json:"min_coverage_pct"`
	MinMutation float64 `json:"min_mutation_score_pct"`
	MinAssessed int     `json:"min_assessed_mutants"`
	MaxTimeout  float64 `json:"max_timeout_pct"`
}
type baselineFile struct {
	Version int         `json:"version"`
	Full    *thresholds `json:"full,omitempty"`
	Diff    *thresholds `json:"diff,omitempty"`
}
type mutation struct {
	Status string `json:"status"`
}
type mutationFile struct {
	Mutations []mutation `json:"mutations"`
}
type mutationReport struct {
	Score  float64        `json:"test_efficacy"`
	Killed int            `json:"mutants_killed"`
	Lived  int            `json:"mutants_lived"`
	Files  []mutationFile `json:"files"`
}
type mutationStats struct{ Killed, Lived, TimedOut, Total int }
type metadata struct {
	StartedAt    string   `json:"started_at"`
	Scope        string   `json:"scope"`
	Base         string   `json:"base,omitempty"`
	ResolvedBase string   `json:"resolved_base,omitempty"`
	Revision     string   `json:"revision"`
	DirtyTree    bool     `json:"dirty_tree"`
	Commands     []string `json:"commands"`
	Error        string   `json:"error,omitempty"`
}

var activeArtifact string
var activeMetadata *metadata

func persistMetadata() {
	if activeArtifact == "" || activeMetadata == nil {
		return
	}
	b, err := json.MarshalIndent(activeMetadata, "", "  ")
	if err == nil {
		_ = os.WriteFile(filepath.Join(activeArtifact, "metadata.json"), append(b, '\n'), 0o644)
	}
}

func fail(f string, a ...any) {
	if activeMetadata != nil {
		activeMetadata.Error = fmt.Sprintf(f, a...)
	}
	persistMetadata()
	fmt.Fprintf(os.Stderr, "qa-gate: "+f+"\n", a...)
	os.Exit(2)
}
func run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

func main() {
	mode, args := "check", os.Args[1:]
	if len(args) > 0 && (args[0] == "check" || args[0] == "update") {
		mode, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("qa-gate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	scope := fs.String("scope", "full", "")
	base := fs.String("base", "", "")
	if fs.Parse(args) != nil || fs.NArg() != 0 || (mode != "check" && mode != "update") || (*scope != "full" && *scope != "diff") {
		fmt.Fprintln(os.Stderr, "usage: qa-gate [check|update] [--scope=full|diff] [--base=REF]")
		os.Exit(2)
	}
	if *scope == "diff" && *base == "" {
		fail("--base is required with --scope=diff")
	}
	out, err := run("git", "rev-parse", "--show-toplevel")
	root := strings.TrimSpace(string(out))
	if err != nil || root == "" {
		fail("run from inside a Git worktree")
	}
	if err = os.Chdir(root); err != nil {
		fail("change to Git worktree: %v", err)
	}
	artifactRoot := filepath.Join(root, "build", "qa-gate")
	if err := os.MkdirAll(artifactRoot, 0o755); err != nil {
		fail("create persistent artifact directory: %v", err)
	}
	artifact, err := os.MkdirTemp(artifactRoot, "run-")
	if err != nil {
		fail("create persistent artifact directory: %v", err)
	}
	meta := metadata{StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Scope: *scope, Base: *base}
	saved := ""
	activeArtifact, activeMetadata = artifact, &meta
	defer func() {
		meta.Error = saved
		persistMetadata()
	}()
	out, err = run("git", "rev-parse", "HEAD")
	meta.Commands = append(meta.Commands, "git rev-parse HEAD")
	if err != nil || strings.TrimSpace(string(out)) == "" {
		saved = "cannot resolve HEAD"
		fail("resolve current revision")
	}
	meta.Revision = strings.TrimSpace(string(out))
	out, err = run("git", "status", "--porcelain")
	meta.Commands = append(meta.Commands, "git status --porcelain")
	if err != nil {
		saved = "cannot read dirty tree status"
		fail("read Git worktree status")
	}
	meta.DirtyTree = strings.TrimSpace(string(out)) != ""
	path, err := exec.LookPath("gremlins")
	if err != nil {
		saved = "gremlins missing"
		fail("gremlins %s is required; install it with:\n  %s", gremlinsVersion, installHint)
	}
	version, err := run("go", "version", "-m", path)
	meta.Commands = append(meta.Commands, "go version -m "+path)
	_ = os.WriteFile(filepath.Join(artifact, "gremlins-build-info.txt"), version, 0644)
	if err != nil || !gremlinsBuildInfo.Match(version) {
		saved = "wrong gremlins version"
		fail("gremlins %s is required; build metadata was %q", gremlinsVersion, strings.TrimSpace(string(version)))
	}
	if *scope == "diff" {
		out, err = run("git", "rev-parse", "--verify", *base+"^{commit}")
		if err != nil || strings.TrimSpace(string(out)) == "" {
			saved = "invalid diff base"
			fail("invalid --base %q", *base)
		}
		meta.ResolvedBase = strings.TrimSpace(string(out))
		untracked, err := untrackedProductionGo()
		if err != nil {
			saved = err.Error()
			fail("%v", err)
		}
		if untracked {
			saved = "untracked production Go files"
			fail("diff scope refuses untracked production Go files; add them before running the gate")
		}
	}
	coverage, err := measureCoverage(artifact, &meta)
	if err != nil {
		saved = err.Error()
		fmt.Fprintln(os.Stderr, err)
		meta.Error = saved
		persistMetadata()
		os.Exit(1)
	}
	skipped := false
	if *scope == "diff" {
		changed, err := productionGoDiff(*base)
		if err != nil {
			saved = err.Error()
			fail("%v", err)
		}
		skipped = !changed
	}
	var report mutationReport
	var stats mutationStats
	if skipped {
		fmt.Println("Mutation: skipped (no production Go changes in diff)")
	} else {
		report, stats, err = measureMutations(artifact, *scope, *base, &meta)
		if err != nil {
			saved = err.Error()
			fmt.Fprintln(os.Stderr, err)
			meta.Error = saved
			persistMetadata()
			os.Exit(1)
		}
	}
	if mode == "update" {
		if skipped {
			saved = "cannot update skipped diff scope"
			fail("cannot update a diff baseline with no production Go changes")
		}
		minimumAssessed := 100
		if *scope == "diff" {
			minimumAssessed = 1
		}
		if stats.Killed+stats.Lived < minimumAssessed || timeoutPct(stats) > 10 {
			saved = "mutation run does not meet baseline validity policy"
			fail("cannot update baseline: assessed mutants must be >= %d and timed out must be <= 10%%", minimumAssessed)
		}
		b, e := readBaseline(filepath.Join(root, ".qa-gate.json"))
		if e != nil && !os.IsNotExist(e) {
			saved = e.Error()
			fail("invalid baseline: %v", e)
		}
		if b.Version == 0 {
			b.Version = 2
		}
		t := thresholds{coverage, report.Score, minimumAssessed, 10}
		if *scope == "full" {
			b.Full = &t
		} else {
			b.Diff = &t
		}
		writeBaseline(filepath.Join(root, ".qa-gate.json"), b)
		fmt.Printf("Updated %s/.qa-gate.json (%s)\n", root, *scope)
		return
	}
	b, err := readBaseline(filepath.Join(root, ".qa-gate.json"))
	if err != nil {
		saved = err.Error()
		fail("missing or invalid .qa-gate.json: %v", err)
	}
	t := b.Full
	if *scope == "diff" {
		t = b.Diff
	}
	if t == nil {
		saved = "missing scope baseline"
		fail("baseline has no %s thresholds", *scope)
	}
	cp := coverage >= t.MinCoverage
	mp, assessed, timedOut := skipped, skipped, skipped
	if !skipped {
		mp = report.Score >= t.MinMutation
		assessed = stats.Killed+stats.Lived >= t.MinAssessed
		timedOut = timeoutPct(stats) <= t.MaxTimeout
	}
	printResult(coverage, report, stats, *t, cp, mp, assessed, timedOut, skipped)
	if cp && mp && assessed && timedOut {
		return
	}
	saved = "quality gate failed"
	meta.Error = saved
	persistMetadata()
	os.Exit(1)
}

func measureCoverage(dir string, m *metadata) (float64, error) {
	p := filepath.Join(dir, "coverage.out")
	out, e := run("go", "test", "-coverprofile="+p, "./...")
	m.Commands = append(m.Commands, "go test -coverprofile=<artifact>/coverage.out ./...")
	_ = os.WriteFile(filepath.Join(dir, "coverage.stdout.txt"), out, 0644)
	if e != nil {
		return 0, fmt.Errorf("coverage tests failed; see %s", dir)
	}
	out, e = run("go", "tool", "cover", "-func="+p)
	m.Commands = append(m.Commands, "go tool cover -func=<artifact>/coverage.out")
	_ = os.WriteFile(filepath.Join(dir, "coverage-summary.txt"), out, 0644)
	if e != nil {
		return 0, fmt.Errorf("coverage summary failed; see %s", dir)
	}
	x := regexp.MustCompile(`(?m)^total:.*\s([0-9]+(?:\.[0-9]+)?)%\s*$`).FindSubmatch(out)
	if x == nil {
		return 0, fmt.Errorf("could not read total coverage; see %s", dir)
	}
	v, _ := strconv.ParseFloat(string(x[1]), 64)
	return v, nil
}
func measureMutations(dir, scope, base string, m *metadata) (mutationReport, mutationStats, error) {
	p := filepath.Join(dir, "mutations.json")
	a := []string{"run", "--workers=1", "--test-cpu=1", "--timeout-coefficient=5", "--output=" + p}
	if scope == "diff" {
		a = append(a, "--diff="+base)
	}
	out, e := run("gremlins", a...)
	m.Commands = append(m.Commands, "gremlins "+strings.Join(a, " "))
	_ = os.WriteFile(filepath.Join(dir, "gremlins.stdout.txt"), out, 0644)
	if e != nil {
		return mutationReport{}, mutationStats{}, fmt.Errorf("gremlins failed; see %s", dir)
	}
	b, e := os.ReadFile(p)
	if e != nil || len(b) == 0 {
		return mutationReport{}, mutationStats{}, fmt.Errorf("gremlins did not produce a JSON report in %s", dir)
	}
	r, s, e := decodeMutationReport(b)
	if e != nil {
		return r, s, fmt.Errorf("invalid gremlins JSON report: %w", e)
	}
	return r, s, nil
}
func productionGoDiff(base string) (bool, error) {
	out, e := run("git", "diff", "--name-only", base, "--")
	if e != nil {
		return false, fmt.Errorf("cannot inspect production Go diff")
	}
	return containsProductionGo(string(out)), nil
}
func untrackedProductionGo() (bool, error) {
	out, e := run("git", "ls-files", "--others", "--exclude-standard")
	if e != nil {
		return false, fmt.Errorf("cannot inspect untracked production Go files")
	}
	return containsProductionGo(string(out)), nil
}
func containsProductionGo(s string) bool {
	for _, n := range strings.Fields(s) {
		if strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") && !strings.HasPrefix(n, "vendor/") {
			return true
		}
	}
	return false
}
func timeoutPct(s mutationStats) float64 {
	denominator := s.Killed + s.Lived + s.TimedOut
	if denominator == 0 {
		return 100
	}
	return float64(s.TimedOut) * 100 / float64(denominator)
}
func decodeMutationReport(b []byte) (mutationReport, mutationStats, error) {
	var r mutationReport
	if e := json.Unmarshal(b, &r); e != nil {
		return r, mutationStats{}, e
	}
	if !validPct(r.Score) || r.Killed < 0 || r.Lived < 0 {
		return r, mutationStats{}, fmt.Errorf("invalid header metrics")
	}
	s := mutationStats{}
	for _, f := range r.Files {
		for _, m := range f.Mutations {
			s.Total++
			switch strings.ReplaceAll(strings.ToUpper(m.Status), " ", "_") {
			case "KILLED":
				s.Killed++
			case "LIVED":
				s.Lived++
			case "TIMED_OUT":
				s.TimedOut++
			}
		}
	}
	if s.Total == 0 || s.Killed+s.Lived == 0 {
		return r, s, fmt.Errorf("no assessed mutants in files[].mutations")
	}
	if r.Killed != s.Killed || r.Lived != s.Lived {
		return r, s, fmt.Errorf("header counters disagree with files[].mutations status data")
	}
	if math.Abs(r.Score-float64(s.Killed)*100/float64(s.Killed+s.Lived)) > .01 {
		return r, s, fmt.Errorf("test_efficacy disagrees with files[].mutations status data")
	}
	return r, s, nil
}
func validPct(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 100 }
func readBaseline(p string) (baselineFile, error) {
	b, e := os.ReadFile(p)
	if e != nil {
		return baselineFile{}, e
	}
	var v baselineFile
	if e = json.Unmarshal(b, &v); e != nil {
		return v, e
	}
	if v.Version == 0 {
		var old thresholds
		if e = json.Unmarshal(b, &old); e != nil {
			return v, e
		}
		old.MinAssessed = 1
		old.MaxTimeout = 100
		v = baselineFile{Version: 1, Full: &old}
	}
	if v.Version != 1 && v.Version != 2 {
		return v, fmt.Errorf("unsupported version %d", v.Version)
	}
	for _, t := range []*thresholds{v.Full, v.Diff} {
		if t != nil && (!validPct(t.MinCoverage) || !validPct(t.MinMutation) || !validPct(t.MaxTimeout) || t.MinAssessed < 1) {
			return v, fmt.Errorf("invalid thresholds")
		}
	}
	return v, nil
}
func writeBaseline(p string, b baselineFile) {
	d, e := json.MarshalIndent(b, "", "  ")
	if e != nil {
		fail("marshal baseline: %v", e)
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".qa-gate.*.tmp")
	if e != nil {
		fail("create baseline: %v", e)
	}
	n := f.Name()
	if _, e = f.Write(append(d, '\n')); e == nil {
		e = f.Sync()
	}
	_ = f.Close()
	if e == nil {
		e = os.Rename(n, p)
	}
	if e != nil {
		_ = os.Remove(n)
		fail("write baseline: %v", e)
	}
}
func printResult(c float64, r mutationReport, s mutationStats, t thresholds, cp, mp, assessed, timedOut, skip bool) {
	st := func(ok bool) string {
		if ok {
			return "[PASS]"
		}
		return "[FAIL]"
	}
	rows := [][]string{{"Coverage", fmt.Sprintf("%.4g%%", c), fmt.Sprintf(">= %.4g%%", t.MinCoverage), st(cp)}}
	if skip {
		rows = append(rows, []string{"Mutation", "skipped", "n/a", "[PASS]"})
	} else {
		mutationStatus := st(mp)
		if !assessed || !timedOut {
			mutationStatus = "[INCONCLUSIVE]"
		}
		rows = append(rows,
			[]string{"Mutation", fmt.Sprintf("%.4g%%", r.Score), fmt.Sprintf(">= %.4g%%", t.MinMutation), mutationStatus},
			[]string{"Assessed", fmt.Sprintf("%d/%d", s.Killed+s.Lived, s.Killed+s.Lived+s.TimedOut), fmt.Sprintf(">= %d", t.MinAssessed), st(assessed)},
			[]string{"Timed out", fmt.Sprintf("%.4g%%", timeoutPct(s)), fmt.Sprintf("<= %.4g%%", t.MaxTimeout), st(timedOut)},
		)
	}
	printTable([]string{"Metric", "Current", "Required", "Status"}, rows)
}

func printTable(header []string, rows [][]string) {
	widths := make([]int, len(header))
	for i, cell := range header {
		widths[i] = len(cell)
	}
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	printRow := func(row []string) {
		for i, cell := range row {
			if i > 0 {
				fmt.Print(" | ")
			}
			fmt.Printf("%-*s", widths[i], cell)
		}
		fmt.Println()
	}
	printRow(header)
	for i, width := range widths {
		if i > 0 {
			fmt.Print("-+-")
		}
		fmt.Print(strings.Repeat("-", width))
	}
	fmt.Println()
	for _, row := range rows {
		printRow(row)
	}
}
