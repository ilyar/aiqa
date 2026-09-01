package result

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactSecretsCoversTypedFieldsMetadataAndMapKeys(t *testing.T) {
	report := ReportV2{Targets: []TargetReport{{
		ID: "anthropic",
		Discovery: Discovery{Models: []map[string]any{{
			"echo-secret": "prefix secret suffix",
			"nested":      map[string]any{"value": "secret"},
		}}},
		Guidance: GuidanceReport{Artifacts: map[string]GuidanceArtifact{
			"secret-ref": {Content: "secret output"},
		}},
	}}}
	RedactSecrets(&report, "secret")
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret") {
		t.Fatalf("secret remained in redacted report: %s", data)
	}
	if !strings.Contains(string(data), "[REDACTED]") {
		t.Fatalf("redaction marker is missing: %s", data)
	}
}
