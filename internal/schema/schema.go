// Package schema validates AIQA JSON documents and JSON Schema Draft 2020-12
// contracts without requiring an external Python runtime.
package schema

import (
	"fmt"
	"os"
	"path/filepath"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

var schemaFiles = []string{
	"schema/report-v3.schema.json",
	"schema/profile-v3.schema.json",
	"schema/ext-v2.schema.json",
	"schema/trace-v1.schema.json",
	"schema/trace-verdict-v1.schema.json",
}

var profileFiles = []string{
	"profile/openai-compat-2026-09.json",
	"profile/anthropic-compat-2026-09.json",
	"profile/google-genai-compat-2026-09.json",
	"profile/google-genai-generate-content-compat-2026-09.json",
}

var wireMockFiles = []string{
	"test/wiremock/mappings/openai-models.json",
	"test/wiremock/mappings/anthropic-models.json",
	"test/wiremock/mappings/google-genai-models.json",
	"test/wiremock/mappings/unavailable-api.json",
}

// ParseFile verifies that path contains one complete JSON value. In particular,
// it rejects trailing non-whitespace data, matching the previous JSON syntax gate.
func ParseFile(path string) (any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open JSON %q: %w", path, err)
	}
	defer f.Close()
	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		return nil, fmt.Errorf("parse JSON %q: %w", path, err)
	}
	return doc, nil
}

// ValidateFile compiles schemaPath with Draft 2020-12 format assertions and
// validates instancePath. Compilation also validates the schema itself.
func ValidateFile(schemaPath, instancePath string) error {
	schemaPath, err := filepath.Abs(schemaPath)
	if err != nil {
		return fmt.Errorf("resolve schema path: %w", err)
	}
	schemaDoc, err := ParseFile(schemaPath)
	if err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource(schemaPath, schemaDoc); err != nil {
		return fmt.Errorf("add schema %q: %w", schemaPath, err)
	}
	compiled, err := compiler.Compile(schemaPath)
	if err != nil {
		return fmt.Errorf("compile schema %q: %w", schemaPath, err)
	}
	instance, err := ParseFile(instancePath)
	if err != nil {
		return err
	}
	if err := compiled.Validate(instance); err != nil {
		return fmt.Errorf("validate %q against %q: %w", instancePath, schemaPath, err)
	}
	return nil
}

// CheckSchemas performs the repository inventory previously checked by the
// Python json.tool/jsonschema commands.
func CheckSchemas(root string) error {
	for _, relative := range schemaFiles {
		path := filepath.Join(root, relative)
		if _, err := ParseFile(path); err != nil {
			return err
		}
		if err := ValidateSchema(path); err != nil {
			return err
		}
	}
	profileSchema := filepath.Join(root, "schema/profile-v3.schema.json")
	for _, relative := range profileFiles {
		if err := ValidateFile(profileSchema, filepath.Join(root, relative)); err != nil {
			return err
		}
	}
	reportSchema := filepath.Join(root, "schema/report-v3.schema.json")
	examples, err := filepath.Glob(filepath.Join(root, "example", "*.json"))
	if err != nil {
		return fmt.Errorf("find example JSON: %w", err)
	}
	if len(examples) == 0 {
		return fmt.Errorf("no example JSON reports found")
	}
	for _, path := range examples {
		if err := ValidateFile(reportSchema, path); err != nil {
			return err
		}
	}
	for _, relative := range wireMockFiles {
		if _, err := ParseFile(filepath.Join(root, relative)); err != nil {
			return err
		}
	}
	return nil
}

// ValidateSchema compiles path to validate its JSON Schema syntax.
func ValidateSchema(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve schema path: %w", err)
	}
	doc, err := ParseFile(path)
	if err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource(path, doc); err != nil {
		return fmt.Errorf("add schema %q: %w", path, err)
	}
	if _, err := compiler.Compile(path); err != nil {
		return fmt.Errorf("compile schema %q: %w", path, err)
	}
	return nil
}

// CheckSBOM asserts the direct SDK versions required by dependency-check.
func CheckSBOM(path string) error {
	doc, err := ParseFile(path)
	if err != nil {
		return err
	}
	root, ok := doc.(map[string]any)
	if !ok {
		return fmt.Errorf("SBOM %q: top-level value is not an object", path)
	}
	components, ok := root["components"].([]any)
	if !ok {
		return fmt.Errorf("SBOM %q: components is not an array", path)
	}
	versions := make(map[string]string, len(components))
	for index, component := range components {
		entry, ok := component.(map[string]any)
		if !ok {
			return fmt.Errorf("SBOM %q: component %d is not an object", path, index)
		}
		name, nameOK := entry["name"].(string)
		version, versionOK := entry["version"].(string)
		if nameOK && versionOK {
			versions[name] = version
		}
	}
	for name, want := range map[string]string{
		"github.com/openai/openai-go/v3":         "v3.54.0",
		"github.com/anthropics/anthropic-sdk-go": "v1.68.0",
		"google.golang.org/genai":                "v1.71.0",
	} {
		if got := versions[name]; got != want {
			return fmt.Errorf("SBOM %q: component %q version=%q, want %q", path, name, got, want)
		}
	}
	return nil
}
