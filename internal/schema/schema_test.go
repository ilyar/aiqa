package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFileRejectsTrailingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{} {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFile(path); err == nil {
		t.Fatal("ParseFile accepted trailing JSON")
	}
}

func TestParseFileRejectsMissingAndInvalidJSON(t *testing.T) {
	if _, err := ParseFile(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("ParseFile accepted a missing file")
	}
	path := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(path, []byte(`{"unterminated":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFile(path); err == nil {
		t.Fatal("ParseFile accepted malformed JSON")
	}
}

func TestValidateFileAssertsDateAndURIFormats(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"date":{"type":"string","format":"date"},"uri":{"type":"string","format":"uri"}},"required":["date","uri"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	valid := filepath.Join(dir, "valid.json")
	if err := os.WriteFile(valid, []byte(`{"date":"2026-09-09","uri":"https://example.test/path"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFile(schemaPath, valid); err != nil {
		t.Fatalf("valid instance: %v", err)
	}
	for name, body := range map[string]string{
		"bad-date.json": `{"date":"2026-99-99","uri":"https://example.test/path"}`,
		"bad-uri.json":  `{"date":"2026-09-09","uri":"not a URI"}`,
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := ValidateFile(schemaPath, path); err == nil {
			t.Fatalf("ValidateFile accepted %s", name)
		}
	}
}

func TestValidateSchemaAndRepositoryInventory(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckSchemas(root); err != nil {
		t.Fatalf("repository schema inventory: %v", err)
	}
	invalid := filepath.Join(t.TempDir(), "invalid-schema.json")
	if err := os.WriteFile(invalid, []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":42}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSchema(invalid); err == nil {
		t.Fatal("ValidateSchema accepted an invalid schema")
	}
}

func TestCheckSBOM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bom.json")
	valid := `{"components":[{"name":"github.com/openai/openai-go/v3","version":"v3.54.0"},{"name":"github.com/anthropics/anthropic-sdk-go","version":"v1.68.0"},{"name":"google.golang.org/genai","version":"v1.71.0"}]}`
	if err := os.WriteFile(path, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckSBOM(path); err != nil {
		t.Fatalf("valid SBOM: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"components":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckSBOM(path); err == nil {
		t.Fatal("CheckSBOM accepted missing SDK components")
	}
	for name, body := range map[string]string{
		"top-level-array":   `[]`,
		"components-object": `{ "components": {} }`,
		"component-array":   `{ "components": [[]] }`,
		"wrong-version":     `{"components":[{"name":"github.com/openai/openai-go/v3","version":"v0.0.0"},{"name":"github.com/anthropics/anthropic-sdk-go","version":"v1.68.0"},{"name":"google.golang.org/genai","version":"v1.71.0"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := CheckSBOM(path); err == nil {
				t.Fatal("CheckSBOM accepted invalid SBOM")
			}
		})
	}
}
