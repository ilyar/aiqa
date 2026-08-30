package report

import (
	"strings"
	"testing"

	"github.com/ilyar/aiqa/internal/result"
)

func TestMarkdownRendersOfficialSDKWithoutToolColumn(t *testing.T) {
	report := result.Report{Models: []result.ModelReport{{
		ID: "test-model",
		External: []result.ExternalResult{{
			Tool:       "official-SDK",
			Suite:      "models",
			Status:     result.StatusPass,
			DurationMS: 12,
			Summary:    "pass",
		}},
	}}}
	markdown := Markdown(report)
	for _, want := range []string{
		"- **External checks:** Official SDK",
		"### Official SDK",
		"| Suite | Status | Summary | Duration |",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown is missing %q:\n%s", want, markdown)
		}
	}
	if strings.Contains(markdown, "| Tool |") {
		t.Fatalf("external table must not render a Tool column:\n%s", markdown)
	}
}

func TestMarkdownMarksUnavailableExternalChecksAsNotSetup(t *testing.T) {
	markdown := Markdown(result.Report{Models: []result.ModelReport{{
		External: []result.ExternalResult{{Tool: "official-SDK", Status: result.StatusSkip}},
	}}})
	if !strings.Contains(markdown, "- **External checks:** not setup") {
		t.Fatalf("unexpected external check summary:\n%s", markdown)
	}
}
