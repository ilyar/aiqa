package guidance

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

func TestActionableFixtureMatchesDraft202012Schema(t *testing.T) {
	report := vibecodeActionableFixture()
	runActionableFixture(t, &report)
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	report.SchemaVersion = "aiqa.report.v1"
	report.GeneratedAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	report.Tool = result.ToolInfo{Name: "aiqa", Version: "test", GoVersion: "go1.27"}
	report.Profile = result.ProfileInfo{ID: p.ID, Revision: p.Revision, Source: p.Source, SourceCommit: p.SourceCommit, Scope: p.Scope}
	report.Config.Tests = []string{"compat"}
	report.Config.Timeout = "30s"
	report.Config.Formats = []string{"json", "markdown"}
	report.Config.ExternalMode = "binary"
	report.Config.FailOn = "core"
	report.Summary.Status = "fail"
	report.Discovery.ModelCount = len(report.Models)
	for i := range report.Discovery.Tests {
		if report.Discovery.Tests[i].Suite == "" {
			report.Discovery.Tests[i].Suite = "compat"
		}
		if report.Discovery.Tests[i].Category == "" {
			report.Discovery.Tests[i].Category = "schema"
		}
	}
	for i := range report.Models {
		report.Models[i].Assessment = result.ModelAssessment{Verdict: "partial"}
		report.Models[i].Summary.Status = "partial"
		for j := range report.Models[i].Compatibility {
			if report.Models[i].Compatibility[j].Suite == "" {
				report.Models[i].Compatibility[j].Suite = "compat"
			}
			if report.Models[i].Compatibility[j].Category == "" {
				report.Models[i].Compatibility[j].Category = "fixture"
			}
		}
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(reportPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	schemaPath, err := filepath.Abs(filepath.Join("..", "..", "schema", "report.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	program := `import copy,json,sys
from jsonschema import Draft202012Validator, FormatChecker
schema=json.load(open(sys.argv[1]))
Draft202012Validator.check_schema(schema)
report=json.load(open(sys.argv[2]))
validator=Draft202012Validator(schema, format_checker=FormatChecker())
validator.validate(report)
legacy=copy.deepcopy(report)
legacy["summary"]["score"]=100
if not list(validator.iter_errors(legacy)):
    raise AssertionError("legacy summary.score must be rejected")`
	command := exec.Command("python3", "-c", program, schemaPath, reportPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Draft 2020-12 validation failed: %v\n%s", err, output)
	}
}
