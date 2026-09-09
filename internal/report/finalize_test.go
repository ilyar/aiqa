package report

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/result"
)

func TestFinalizeV3UsesOnePayloadForFilesAndStdout(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		Name:    filepath.Join(dir, "report"),
		APIKey:  "must-not-leak",
		Formats: []string{"json", "markdown"},
	}
	report := finalizableReport()
	final, err := FinalizeV3(report, cfg)
	if err != nil {
		t.Fatal(err)
	}

	jsonPayload, err := final.Bytes("json")
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Execution *result.ExecutionSummary `json:"execution"`
	}
	if err := json.Unmarshal(jsonPayload, &decoded); err != nil {
		t.Fatalf("stdout JSON is not parseable: %v\n%s", err, jsonPayload)
	}
	if decoded.Execution == nil || decoded.Execution.Phase != "completed" || decoded.Execution.FinishedJobs != 2 {
		t.Fatalf("execution block changed during finalization: %+v", decoded.Execution)
	}
	if strings.Contains(string(jsonPayload), "must-not-leak") {
		t.Fatalf("stdout payload leaked a credential: %s", jsonPayload)
	}
	markdownPayload, err := final.Bytes("markdown")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Execution", "**Phase:** `completed`", "**Jobs:** 2 planned; 2 finished; 0 cancelled"} {
		if !strings.Contains(string(markdownPayload), want) {
			t.Fatalf("stdout Markdown missing %q:\n%s", want, markdownPayload)
		}
	}

	paths, err := final.Commit(cfg.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("paths = %v, want two outputs", paths)
	}
	for _, test := range []struct {
		format string
		path   string
	}{
		{format: "json", path: cfg.Name + ".json"},
		{format: "markdown", path: cfg.Name + ".md"},
	} {
		want, err := final.Bytes(test.format)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(test.path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("%s file differs from stdout payload\nfile: %q\nstdout: %q", test.format, got, want)
		}
	}
}

func TestFinalizeV3RejectsInvalidExecutionSummaryBeforeAnyOutput(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{Name: filepath.Join(dir, "report"), Formats: []string{"json", "markdown"}}
	report := finalizableReport()
	report.Execution.ObservedPeakInflight = 3
	report.Execution.GlobalRequestCapacity = 2
	if _, err := WriteV3(report, cfg); err == nil || !strings.Contains(err.Error(), "request capacity") {
		t.Fatalf("WriteV3 error = %v, want execution consistency failure", err)
	}
	for _, path := range []string{cfg.Name + ".json", cfg.Name + ".md"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("invalid finalization created output %s: %v", path, err)
		}
	}
}

func TestFinalizeV3RejectsIncompleteCompletedExecution(t *testing.T) {
	report := finalizableReport()
	report.Execution.FinishedJobs = 1
	if _, err := FinalizeV3(report, config.Config{Formats: []string{"json"}}); err == nil || !strings.Contains(err.Error(), "account for every planned job") {
		t.Fatalf("FinalizeV3 error = %v, want completed execution consistency failure", err)
	}
}

func TestFinalV3CommitRollsBackOnSecondOutputFailureAndClaimsNoPaths(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "report")
	if err := os.WriteFile(name+".json", []byte("old-json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name+".md", []byte("old-markdown\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	final, err := FinalizeV3(finalizableReport(), config.Config{Formats: []string{"json", "markdown"}})
	if err != nil {
		t.Fatal(err)
	}
	paths, err := final.commit(name, func(old, new string) error {
		if new == name+".md" && strings.Contains(filepath.Base(old), ".aiqa-report-stage-") {
			return errors.New("injected second output failure")
		}
		return os.Rename(old, new)
	})
	if err == nil || !strings.Contains(err.Error(), "injected second output failure") {
		t.Fatalf("commit error = %v, want injected failure", err)
	}
	if paths != nil {
		t.Fatalf("failed commit claimed written paths: %v", paths)
	}
	for path, want := range map[string]string{
		name + ".json": "old-json\n",
		name + ".md":   "old-markdown\n",
	} {
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(got) != want {
			t.Fatalf("partial output survived %s: got %q, want %q", path, got, want)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".aiqa-report-") {
			t.Fatalf("transaction temporary was retained: %s", entry.Name())
		}
	}
}

func TestFinalV3BytesIsDefensiveAndRejectsUnconfiguredFormat(t *testing.T) {
	final, err := FinalizeV3(finalizableReport(), config.Config{Formats: []string{"json"}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := final.Bytes("json")
	if err != nil {
		t.Fatal(err)
	}
	first[0] = '!'
	second, err := final.Bytes("json")
	if err != nil {
		t.Fatal(err)
	}
	if second[0] == '!' {
		t.Fatal("Bytes exposed mutable finalized payload")
	}
	if _, err := final.Bytes("markdown"); err == nil {
		t.Fatal("Bytes accepted an unconfigured stdout format")
	}
}

func finalizableReport() result.ReportV3 {
	return result.ReportV3{
		SchemaVersion: "aiqa.report.v3",
		Tool:          result.ToolInfo{Name: "aiqa", Version: "1.0.0-alpha.10", GoVersion: "go1.27"},
		Config: result.PublicConfigV3{
			Tests: []string{"compat"}, Timeout: "1s", Formats: []string{"json", "markdown"}, FailOn: "core",
		},
		Targets: []result.TargetReport{{ID: "fixture", Protocol: "openai", Models: []result.ModelReport{}}},
		Summary: result.RunSummary{Verdict: "pass", TargetCounts: map[string]int{"pass": 1}},
		Execution: &result.ExecutionSummary{
			SchemaVersion: "aiqa.execution-summary.v1", Phase: "completed", ElapsedMS: 42,
			GlobalRequestCapacity: 2, ObservedPeakInflight: 2, PlannedJobs: 2, FinishedJobs: 2,
			CancelledJobs: 0, PacingGroupCount: 1, SchedulerPolicy: "fair-target-admission/v1",
			EstimateMethod: "remaining-work-simulation/v1",
		},
	}
}
