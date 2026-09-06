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

func TestGenerateWhenModelProviderCannotStartWarnsAndStops(t *testing.T) {
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "model-provider"), `#!/bin/sh
case "$*" in
  *"--json status"*) printf '{"running":false}\n' ;;
  *) printf 'ERROR: provider unavailable\n' >&2; exit 1 ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "aiqa"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir)

	var stdout, stderr bytes.Buffer
	if err := generate(context.Background(), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "model-provider could not be started") {
		t.Fatalf("missing startup warning: %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "ERROR:") {
		t.Fatalf("provider error leaked into maintenance output: %q", stderr.String())
	}
	if stdout.Len() == 0 {
		t.Fatal("expected startup status on stdout")
	}
}

func TestProviderCommandArgsPutConfigBeforeCommand(t *testing.T) {
	got := providerCommandArgs("/tmp/provider.json", "--json", "status")
	want := []string{"--config", "/tmp/provider.json", "--json", "status"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("provider args = %#v, want %#v", got, want)
	}
}

func TestEnvOrDefault(t *testing.T) {
	t.Setenv("AIQA_TEST_DEFAULT", " value ")
	if got := envOrDefault("AIQA_TEST_DEFAULT", "fallback"); got != "value" {
		t.Fatalf("envOrDefault value=%q, want value", got)
	}
	t.Setenv("AIQA_TEST_DEFAULT", "")
	if got := envOrDefault("AIQA_TEST_DEFAULT", "fallback"); got != "fallback" {
		t.Fatalf("envOrDefault value=%q, want fallback", got)
	}
}

func TestGenerateGoodTargetsAllProviderProtocolsExplicitly(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "example"), 0o700); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "aiqa-args")
	writeExecutable(t, filepath.Join(binDir, "model-provider"), `#!/bin/sh
case "$1" in
  --json) printf '{"running":true}\n' ;;
  env) printf "export OPENAI_BASE_URL='http://127.0.0.1:8080/openai/v1'\nexport OPENAI_API_KEY='test-key'\nexport ANTHROPIC_BASE_URL='http://127.0.0.1:8080/anthropic'\nexport ANTHROPIC_API_KEY='test-key'\nexport GOOGLE_GENAI_BASE_URL='http://127.0.0.1:8080/genai'\nexport GOOGLE_GENAI_API_KEY='test-key'\n" ;;
  *) exit 1 ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "aiqa"), `#!/bin/sh
printf '%s\n' "$@" > "$AIQA_ARGS_FILE"
printf '# good\n' > good-report.md
printf '{"summary":{"verdict":"fail"},"targets":[{"id":"openai"},{"id":"anthropic"},{"id":"google-genai"}]}\n' > good-report.json
`)
	t.Setenv("PATH", binDir)
	t.Setenv("AIQA_ARGS_FILE", argsFile)

	var stdout, stderr bytes.Buffer
	generated, err := generateGood(context.Background(), &stdout, &stderr, root)
	if err != nil {
		t.Fatal(err)
	}
	if !generated {
		t.Fatal("generateGood reported that no report was generated")
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	argText := string(args)
	if !strings.Contains(argText, "--target=openai,anthropic,google-genai\n") {
		t.Fatalf("provider example did not select all provider protocols explicitly: %q", args)
	}
	if !strings.Contains(argText, "--fail-on=never\n") {
		t.Fatalf("provider example did not preserve reports for non-pass target verdicts: %q", args)
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
		map[string]string{"REPLACE": "new", "REMOVE": "override", "ADD": "two"},
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
	writeFixture(t, filepath.Join(directory, "good-report.json"), `{"summary":{"verdict":"fail"},"targets":[{"id":"openai"},{"id":"anthropic"},{"id":"google-genai"}]}`)
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
	writeFixture(t, filepath.Join(directory, "bad-report.json"), `{
		"summary":{"verdict":"fail"},
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

func writeExecutable(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o700); err != nil {
		t.Fatal(err)
	}
}
