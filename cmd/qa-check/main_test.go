package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunRejectsInvalidUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"schemas", "extra"}, {"sbom"}, {"unknown"}} {
		if err := run(args); err == nil {
			t.Fatalf("run(%q) succeeded", args)
		}
	}
}

func TestRunSchemasAndSBOM(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	if err := run([]string{"schemas"}); err != nil {
		t.Fatalf("schemas: %v", err)
	}
	sbom := filepath.Join(t.TempDir(), "bom.json")
	contents := `{"components":[{"name":"github.com/openai/openai-go/v3","version":"v3.54.0"},{"name":"github.com/anthropics/anthropic-sdk-go","version":"v1.68.0"},{"name":"google.golang.org/genai","version":"v1.71.0"}]}`
	if err := os.WriteFile(sbom, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"sbom", sbom}); err != nil {
		t.Fatalf("sbom: %v", err)
	}
}
