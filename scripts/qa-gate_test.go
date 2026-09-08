package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestQAGateCheckAndUpdate(t *testing.T) {
	repo := t.TempDir()
	copyFile(t, filepath.Join(repo, "scripts", "qa-gate"), filepath.Join(projectRoot(t), "scripts", "qa-gate"), 0o755)
	writeFile(t, filepath.Join(repo, ".git"), "")
	writeFile(t, filepath.Join(repo, ".qa-gate.json"), "{\n  \"min_coverage_pct\": 80.0,\n  \"min_mutation_score_pct\": 70.0\n}\n")

	bin := filepath.Join(repo, "bin")
	writeFile(t, filepath.Join(bin, "git"), "#!/bin/sh\nprintf '%s\\n' \"$QA_GATE_TEST_ROOT\"\n")
	writeFile(t, filepath.Join(bin, "go"), "#!/bin/sh\ncase \"$1 $2\" in\n  'test -coverprofile='*) : ;;\nesac\nif [ \"$1\" = test ]; then printf 'mode: set\\n' > \"${2#-coverprofile=}\"; exit 0; fi\nif [ \"$1 $2\" = 'tool cover' ]; then printf 'total: (statements) 85.4%%\\n'; exit 0; fi\nexit 9\n")
	writeFile(t, filepath.Join(bin, "gremlins"), "#!/bin/sh\n[ \"$1\" = run ] && [ \"$2\" = --output ] || exit 9\nprintf '{\"test_efficacy\":72.5,\"mutants_killed\":29,\"mutants_lived\":11}\\n' > \"$3\"\n")

	output, err := runGate(repo, bin, "check")
	if err != nil {
		t.Fatalf("check failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Metric") || !strings.Contains(output, "[PASS]") {
		t.Fatalf("unexpected check output: %s", output)
	}
	output, err = runGate(repo, bin, "update")
	if err != nil {
		t.Fatalf("update failed: %v\n%s", err, output)
	}
	baseline, err := os.ReadFile(filepath.Join(repo, ".qa-gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(baseline), "{\n  \"min_coverage_pct\": 85.4,\n  \"min_mutation_score_pct\": 72.5\n}\n"; got != want {
		t.Fatalf("baseline = %q, want %q", got, want)
	}
}

func TestQAGateRejectsUnassessedMutants(t *testing.T) {
	repo := t.TempDir()
	copyFile(t, filepath.Join(repo, "scripts", "qa-gate"), filepath.Join(projectRoot(t), "scripts", "qa-gate"), 0o755)
	writeFile(t, filepath.Join(repo, ".git"), "")
	bin := filepath.Join(repo, "bin")
	writeFile(t, filepath.Join(bin, "git"), "#!/bin/sh\nprintf '%s\\n' \"$QA_GATE_TEST_ROOT\"\n")
	writeFile(t, filepath.Join(bin, "go"), "#!/bin/sh\nif [ \"$1\" = test ]; then printf x > \"${2#-coverprofile=}\"; exit 0; fi\nif [ \"$1 $2\" = 'tool cover' ]; then printf 'total: 80.0%%\\n'; exit 0; fi\n")
	writeFile(t, filepath.Join(bin, "gremlins"), "#!/bin/sh\nprintf '{\"test_efficacy\":0,\"mutants_killed\":0,\"mutants_lived\":0}\\n' > \"$3\"\n")

	output, err := runGate(repo, bin, "update")
	if err == nil || !strings.Contains(output, "no assessed mutants") {
		t.Fatalf("expected assessed-mutants failure, err=%v output=%s", err, output)
	}
}

func TestQAGateReportsMetricRegression(t *testing.T) {
	repo := t.TempDir()
	copyFile(t, filepath.Join(repo, "scripts", "qa-gate"), filepath.Join(projectRoot(t), "scripts", "qa-gate"), 0o755)
	writeFile(t, filepath.Join(repo, ".git"), "")
	writeFile(t, filepath.Join(repo, ".qa-gate.json"), "{\n  \"min_coverage_pct\": 86.0,\n  \"min_mutation_score_pct\": 73.0\n}\n")
	bin := filepath.Join(repo, "bin")
	writeFile(t, filepath.Join(bin, "git"), "#!/bin/sh\nprintf '%s\\n' \"$QA_GATE_TEST_ROOT\"\n")
	writeFile(t, filepath.Join(bin, "go"), "#!/bin/sh\nif [ \"$1\" = test ]; then printf x > \"${2#-coverprofile=}\"; exit 0; fi\nif [ \"$1 $2\" = 'tool cover' ]; then printf 'total: 85.4%%\\n'; exit 0; fi\n")
	writeFile(t, filepath.Join(bin, "gremlins"), "#!/bin/sh\nprintf '{\"test_efficacy\":72.5,\"mutants_killed\":29,\"mutants_lived\":11}\\n' > \"$3\"\n")

	output, err := runGate(repo, bin, "check")
	if err == nil {
		t.Fatalf("expected gate failure, output=%s", output)
	}
	for _, want := range []string{"Coverage", "Mutation", "[FAIL]", "Baseline/current diff:", "baseline 86.00%, current 85.40%", "baseline 73.00%, current 72.50%"} {
		if !strings.Contains(output, want) {
			t.Fatalf("gate output missing %q: %s", want, output)
		}
	}
}

func TestQAGateExplainsHowToInstallGremlins(t *testing.T) {
	repo := t.TempDir()
	copyFile(t, filepath.Join(repo, "scripts", "qa-gate"), filepath.Join(projectRoot(t), "scripts", "qa-gate"), 0o755)
	bin := filepath.Join(repo, "bin")
	writeFile(t, filepath.Join(bin, "git"), "#!/bin/sh\nprintf '%s\\n' \"$QA_GATE_TEST_ROOT\"\n")

	output, err := runGate(repo, bin, "check")
	if err == nil || !strings.Contains(output, "go install github.com/go-gremlins/gremlins/cmd/gremlins@latest") {
		t.Fatalf("expected install instruction, err=%v output=%s", err, output)
	}
}

func projectRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func runGate(repo, bin, mode string) (string, error) {
	command := exec.Command(filepath.Join(repo, "scripts", "qa-gate"), mode)
	command.Dir = repo
	command.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin", "QA_GATE_TEST_ROOT="+repo)
	output, err := command.CombinedOutput()
	return string(output), err
}

func copyFile(t *testing.T, target, source string, mode os.FileMode) {
	t.Helper()
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	writeFileMode(t, target, string(contents), mode)
}

func writeFile(t *testing.T, path, contents string) { writeFileMode(t, path, contents, 0o755) }

func writeFileMode(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}
