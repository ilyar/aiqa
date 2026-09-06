//go:build ignore

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratePrivateEnvResolvesMultipleEnvironments(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "http-client.env.json")
	outputPath := filepath.Join(directory, "http-client.private.env.json")
	writePrivateEnvFixture(t, inputPath, `{
  "openai": {"base":"https://example.test/v1", "key":"$env.OPENAI_KEY"},
  "anthropic": {"key":"$env.ANTHROPIC_KEY", "test_model":"test"}
}`)
	t.Setenv("OPENAI_KEY", "openai-secret")
	t.Setenv("ANTHROPIC_KEY", "anthropic-secret")

	if err := generatePrivateEnv(inputPath, outputPath, os.LookupEnv); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"anthropic\": {\n    \"key\": \"anthropic-secret\"\n  },\n  \"openai\": {\n    \"key\": \"openai-secret\"\n  }\n}\n"
	if string(got) != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("output permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestGeneratePrivateEnvAcceptsEmptyValue(t *testing.T) {
	directory := t.TempDir()
	inputPath, outputPath := privateEnvPaths(directory)
	writePrivateEnvFixture(t, inputPath, `{"empty":{"key":"$env.EMPTY_KEY"}}`)
	t.Setenv("EMPTY_KEY", "")

	if err := generatePrivateEnv(inputPath, outputPath, os.LookupEnv); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"key": ""`) {
		t.Fatalf("empty value not written: %q", got)
	}
}

func TestGeneratePrivateEnvSkipsUnsetVariables(t *testing.T) {
	directory := t.TempDir()
	inputPath, outputPath := privateEnvPaths(directory)
	writePrivateEnvFixture(t, inputPath, `{
  "available":{"key":"$env.AVAILABLE_KEY"},
  "missing":{"key":"$env.MISSING_KEY"}
}`)
	t.Setenv("AVAILABLE_KEY", "available-secret")
	os.Unsetenv("MISSING_KEY")

	if err := generatePrivateEnv(inputPath, outputPath, os.LookupEnv); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"available\": {\n    \"key\": \"available-secret\"\n  }\n}\n"
	if string(got) != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestGeneratePrivateEnvWritesEmptyObjectWhenAllVariablesAreUnset(t *testing.T) {
	directory := t.TempDir()
	inputPath, outputPath := privateEnvPaths(directory)
	writePrivateEnvFixture(t, inputPath, `{"missing":{"key":"$env.MISSING_KEY"}}`)
	os.Unsetenv("MISSING_KEY")

	if err := generatePrivateEnv(inputPath, outputPath, os.LookupEnv); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "{}\n" {
		t.Fatalf("output = %q, want empty object", got)
	}
}

func TestGeneratePrivateEnvRejectsInvalidInputAndPreservesOutput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		setup func(*testing.T)
	}{
		{"malformed JSON", `{"openai":`, func(*testing.T) {}},
		{"trailing JSON", `{} {}`, func(*testing.T) {}},
		{"missing key", `{"openai":{}}`, func(*testing.T) {}},
		{"nonmatching key", `{"openai":{"key":"literal"}}`, func(*testing.T) {}},
		{"invalid placeholder", `{"openai":{"key":"$env.NOT-VALID"}}`, func(*testing.T) {}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			inputPath, outputPath := privateEnvPaths(directory)
			writePrivateEnvFixture(t, inputPath, test.input)
			writePrivateEnvFixture(t, outputPath, "existing private output\n")
			test.setup(t)

			err := generatePrivateEnv(inputPath, outputPath, os.LookupEnv)
			if err == nil {
				t.Fatal("generatePrivateEnv succeeded")
			}
			if strings.Contains(err.Error(), "existing private output") {
				t.Fatalf("diagnostic leaked private output: %v", err)
			}
			got, readErr := os.ReadFile(outputPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != "existing private output\n" {
				t.Fatalf("output changed after failure: %q", got)
			}
		})
	}
}

func privateEnvPaths(directory string) (string, string) {
	return filepath.Join(directory, "http-client.env.json"), filepath.Join(directory, "http-client.private.env.json")
}

func writePrivateEnvFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
