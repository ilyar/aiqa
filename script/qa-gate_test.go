package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const report = `{"test_efficacy":75,"mutants_killed":3,"mutants_lived":1,"files":[{"mutations":[{"status":"KILLED"},{"status":"KILLED"},{"status":"KILLED"},{"status":"LIVED"},{"status":"TIMED OUT"}]}]}`

func TestPinnedArgsAndPersistentArtifacts(t *testing.T) {
	r, b := repo(t, `{"version":2,"full":{"min_coverage_pct":80,"min_mutation_score_pct":70,"min_assessed_mutants":4,"max_timeout_pct":25}}`)
	out, e := gate(r, b, "check")
	if e != nil {
		t.Fatal(e, out)
	}
	args, _ := os.ReadFile(filepath.Join(r, "gremlins.args"))
	for _, v := range []string{"run", "--workers=1", "--test-cpu=1", "--timeout-coefficient=5", "--output="} {
		if !strings.Contains(string(args), v) {
			t.Fatalf("missing %s in %s", v, args)
		}
	}
	d, _ := filepath.Glob(filepath.Join(r, "build", "qa-gate", "run-*"))
	if len(d) != 1 {
		t.Fatal("artifacts", d)
	}
	for _, v := range []string{"coverage.out", "mutations.json", "metadata.json", "gremlins.stdout.txt", "gremlins-build-info.txt"} {
		if _, e := os.Stat(filepath.Join(d[0], v)); e != nil {
			t.Error(v, e)
		}
	}
	metadata, err := os.ReadFile(filepath.Join(d[0], "metadata.json"))
	if err != nil || !strings.Contains(string(metadata), `"revision": "headbeef"`) || !strings.Contains(string(metadata), `"dirty_tree": false`) {
		t.Fatalf("state fingerprint = %q, %v", metadata, err)
	}
}
func TestTimeoutBoundaryAndInvalidUpdate(t *testing.T) {
	r, b := repo(t, `{"version":2,"full":{"min_coverage_pct":80,"min_mutation_score_pct":70,"min_assessed_mutants":4,"max_timeout_pct":19}}`)
	out, e := gate(r, b, "check")
	if e == nil || !strings.Contains(out, "Timed out") || !strings.Contains(out, "[INCONCLUSIVE]") || !strings.Contains(out, "Assessed   | 4/5") || strings.Count(out, "[PASS]") < 2 {
		t.Fatal(e, out)
	}
	before, _ := os.ReadFile(filepath.Join(r, ".qa-gate.json"))
	out, e = gate(r, b, "update")
	if e == nil || !strings.Contains(out, "cannot update baseline") {
		t.Fatal("timeout-invalid update unexpectedly succeeded", e, out)
	}
	after, _ := os.ReadFile(filepath.Join(r, ".qa-gate.json"))
	if string(before) != string(after) {
		t.Fatal("timeout-invalid update overwrote baseline")
	}
	write(t, filepath.Join(b, "gremlins"), gremlin(`{"test_efficacy":75,"mutants_killed":9,"mutants_lived":1,"files":[{"mutations":[{"status":"KILLED"}]}]}`))
	_, e = gate(r, b, "update")
	after, _ = os.ReadFile(filepath.Join(r, ".qa-gate.json"))
	if e == nil || string(before) != string(after) {
		t.Fatal("invalid report overwrote baseline")
	}
}
func TestDiffSkipAndUntrackedRefusal(t *testing.T) {
	r, b := repo(t, `{"version":2,"diff":{"min_coverage_pct":80,"min_mutation_score_pct":70,"min_assessed_mutants":4,"max_timeout_pct":25}}`)
	write(t, filepath.Join(b, "git"), git("docs/a.md\n", ""))
	out, e := gate(r, b, "check", "--scope=diff", "--base=main")
	if e != nil || !strings.Contains(out, "Mutation: skipped") {
		t.Fatal(e, out)
	}
	if _, e = os.Stat(filepath.Join(r, "gremlins.args")); !os.IsNotExist(e) {
		t.Fatal("gremlins ran")
	}
	runs, _ := filepath.Glob(filepath.Join(r, "build", "qa-gate", "run-*"))
	metadata, err := os.ReadFile(filepath.Join(runs[0], "metadata.json"))
	if err != nil || !strings.Contains(string(metadata), `"resolved_base": "deadbeef"`) {
		t.Fatalf("diff base fingerprint = %q, %v", metadata, err)
	}
	write(t, filepath.Join(b, "git"), git("internal/a.go\n", "internal/a.go\n"))
	out, e = gate(r, b, "check", "--scope=diff", "--base=main")
	if e == nil || !strings.Contains(out, "untracked production Go") {
		t.Fatal(e, out)
	}
}
func TestDiffGitProbeFailureFailsClosed(t *testing.T) {
	r, b := repo(t, `{"version":2,"diff":{"min_coverage_pct":80,"min_mutation_score_pct":70,"min_assessed_mutants":4,"max_timeout_pct":25}}`)
	write(t, filepath.Join(b, "git"), gitFail("diff"))
	out, e := gate(r, b, "check", "--scope=diff", "--base=main")
	if e == nil || !strings.Contains(out, "cannot inspect production Go diff") {
		t.Fatal("diff failure passed", e, out)
	}
	write(t, filepath.Join(b, "git"), gitFail("ls-files"))
	out, e = gate(r, b, "check", "--scope=diff", "--base=main")
	if e == nil || !strings.Contains(out, "cannot inspect untracked production Go files") {
		t.Fatal("untracked failure passed", e, out)
	}
}
func TestBuildMetadataVersion(t *testing.T) {
	r, b := repo(t, `{}`)
	write(t, filepath.Join(b, "go"), "#!/bin/sh\nif [ \"$1 $2\" = 'version -m' ];then printf '\\tmod\\tgithub.com/go-gremlins/gremlins\\tv0.5.0\\n';exit;fi\n")
	out, e := gate(r, b, "check")
	if e == nil || !strings.Contains(out, "v0.6.0") {
		t.Fatal(e, out)
	}
	runs, _ := filepath.Glob(filepath.Join(r, "build", "qa-gate", "run-*"))
	if len(runs) != 1 {
		t.Fatalf("failure artifacts = %v", runs)
	}
	data, err := os.ReadFile(filepath.Join(runs[0], "metadata.json"))
	if err != nil || !strings.Contains(string(data), "gremlins v0.6.0 is required") {
		t.Fatalf("failure metadata = %q, %v", data, err)
	}
}

func repo(t *testing.T, base string) (string, string) {
	t.Helper()
	r := t.TempDir()
	copy(t, filepath.Join(r, "script", "qa-gate.go"), filepath.Join(root(t), "script", "qa-gate.go"))
	write(t, filepath.Join(r, ".git"), "")
	write(t, filepath.Join(r, ".qa-gate.json"), base)
	b := filepath.Join(r, "bin")
	write(t, filepath.Join(b, "git"), git("internal/a.go\n", ""))
	write(t, filepath.Join(b, "go"), "#!/bin/sh\nif [ \"$1 $2\" = 'version -m' ];then printf '\\tmod\\tgithub.com/go-gremlins/gremlins\\tv0.6.0\\n';exit;fi\nif [ \"$1\" = test ];then echo x > \"${2#-coverprofile=}\";exit;fi\nif [ \"$1 $2\" = 'tool cover' ];then echo 'total: 85.4%';exit;fi\nexit 9\n")
	write(t, filepath.Join(b, "gremlins"), gremlin(report))
	return r, b
}
func git(diff, untracked string) string {
	return "#!/bin/sh\ncase \"$1 $2\" in\n'rev-parse --show-toplevel')echo \"$QA_GATE_TEST_ROOT\";;\n'rev-parse HEAD')echo headbeef;;\n'rev-parse --verify')echo deadbeef;;\n'status --porcelain'):;;\n'diff --name-only')printf '" + diff + "';;\n'ls-files --others')printf '" + untracked + "';;\nesac\n"
}
func gitFail(command string) string {
	return "#!/bin/sh\ncase \"$1\" in\n" + command + ") exit 1;;\n" +
		"rev-parse) case \"$2\" in --show-toplevel) echo \"$QA_GATE_TEST_ROOT\";; HEAD) echo headbeef;; --verify) echo deadbeef;; esac;;\n" +
		"status) :;;\nesac\n"
}
func gremlin(rep string) string {
	return "#!/bin/sh\nprintf '%s\\n' \"$@\" > gremlins.args\nfor x in \"$@\";do case \"$x\" in --output=*)p=${x#--output=};;esac;done\nprintf '" + rep + "' > \"$p\"\n"
}
func root(t *testing.T) string {
	t.Helper()
	r, e := filepath.Abs("..")
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func gate(r, b string, a ...string) (string, error) {
	x := filepath.Join(r, "q")
	c := exec.Command("go", "build", "-o", x, filepath.Join(r, "script", "qa-gate.go"))
	if o, e := c.CombinedOutput(); e != nil {
		return string(o), e
	}
	c = exec.Command(x, a...)
	c.Dir = r
	c.Env = append(os.Environ(), "PATH="+b+":/usr/bin:/bin", "QA_GATE_TEST_ROOT="+r)
	o, e := c.CombinedOutput()
	return string(o), e
}
func copy(t *testing.T, to, from string) {
	t.Helper()
	x, e := os.ReadFile(from)
	if e != nil {
		t.Fatal(e)
	}
	write(t, to, string(x))
}
func write(t *testing.T, p, s string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(s), 0755); e != nil {
		t.Fatal(e)
	}
}
