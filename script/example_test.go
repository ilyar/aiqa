//go:build ignore

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateWithoutModelProviderWarnsAndStops(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var stdout, stderr bytes.Buffer
	if err := generate(context.Background(), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "WARNING: model-provider was not found") {
		t.Fatalf("missing warning: %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestParseExports(t *testing.T) {
	exports, err := parseExports([]byte("export OPENAI_BASE_URL='http://127.0.0.1:8080/v1';\nexport API_KEY='value-with-'\"'\"'quote';\n"))
	if err != nil {
		t.Fatal(err)
	}
	if exports["OPENAI_BASE_URL"] != "http://127.0.0.1:8080/v1" || exports["API_KEY"] != "value-with-'quote" {
		t.Fatalf("unexpected exports: %#v", exports)
	}
}

func TestParseExportsRejectsShellSyntax(t *testing.T) {
	for _, input := range []string{
		"OPENAI_API_KEY='secret'\n",
		"export 1INVALID='secret'\n",
		"export OPENAI_API_KEY=$(command)\n",
		"export OPENAI_API_KEY='secret'; command\n",
		"export OPENAI_API_KEY='first'\nexport OPENAI_API_KEY='second'\n",
	} {
		if _, err := parseExports([]byte(input)); err == nil {
			t.Fatalf("accepted unsafe input %q", input)
		}
	}
}

func TestMergeEnvironmentOverridesAndUnsets(t *testing.T) {
	got := mergeEnvironment(
		[]string{"KEEP=one", "REPLACE=old", "REMOVE=value"},
		map[string]string{"REPLACE": "new", "ADD": "two"},
		"REMOVE",
	)
	joined := strings.Join(got, "\n")
	for _, want := range []string{"KEEP=one", "REPLACE=new", "ADD=two"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("environment %q does not contain %q", joined, want)
		}
	}
	if strings.Contains(joined, "REPLACE=old") || strings.Contains(joined, "REMOVE=") {
		t.Fatalf("environment retained removed values: %q", joined)
	}
}

func TestReportValidation(t *testing.T) {
	directory := t.TempDir()
	writeFixture(t, filepath.Join(directory, "good-report.md"), "# good\n")
	writeFixture(t, filepath.Join(directory, "good-report.json"), `{"summary":{"verdict":"pass"}}`)
	if err := validateGoodReport(directory); err != nil {
		t.Fatal(err)
	}

	writeFixture(t, filepath.Join(directory, "bad-report.md"), "# bad\n")
	writeFixture(t, filepath.Join(directory, "bad-report.json"), `{
		"summary":{"verdict":"inconclusive"},
		"targets":[
			{"id":"openai","summary":{"error":1}},
			{"id":"anthropic","summary":{"error":2}},
			{"id":"google-genai","summary":{"error":3}}
		]
	}`)
	if err := validateBadReport(directory); err != nil {
		t.Fatal(err)
	}
}

func writeFixture(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}
