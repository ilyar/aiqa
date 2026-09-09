package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/ilyar/aiqa/internal/app"
	"github.com/ilyar/aiqa/internal/config"
)

func traceLine(t *testing.T, values map[string]any) string {
	t.Helper()
	record := map[string]any{
		"schema_version": "aiqa.trace.v1", "time": "2026-09-09T12:00:00Z", "seq": 1,
		"level": "INFO", "event": "probe.end", "trace_id": "trace", "span_id": "span",
		"parent_span_id": "parent", "operation_id": "job-1", "target": "openai", "model": "model-a",
		"outcome": "pass", "message": "safe",
	}
	for k, v := range values {
		record[k] = v
	}
	b, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func TestLogFiltersStdinAndJSON(t *testing.T) {
	input := traceLine(t, map[string]any{"level": "DEBUG", "outcome": "assertion_failed"}) + traceLine(t, map[string]any{"seq": 2, "level": "INFO", "target": "other"})
	var out, errOut bytes.Buffer
	if err := runLog([]string{"-", "--failed", "--target=openai", "--format=json"}, strings.NewReader(input), &out, &errOut, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "assertion_failed") || strings.Contains(out.String(), `"seq":2`) || errOut.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", out.String(), errOut.String())
	}
}

func TestLogRejectsFilterBeforeOpeningSource(t *testing.T) {
	var out, errOut bytes.Buffer
	err := runLog([]string{"missing.log", "--level=INFO", "--only-level=INFO"}, strings.NewReader(""), &out, &errOut, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("error=%v", err)
	}
}

func TestLogNameAndSafeHumanOutput(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/saved.log"
	if err := os.WriteFile(path, []byte(traceLine(t, map[string]any{"message": "x\x1b[2J\ny"})), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if err := runLog([]string{"--name=saved", "--format=table"}, strings.NewReader(""), &out, &errOut, dir); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "SEQ   TIME") || strings.Contains(out.String(), "\x1b") || !strings.Contains(out.String(), `\u001b`) {
		t.Fatalf("unsafe table %q", out.String())
	}
}

func TestLogMalformedIsNonZero(t *testing.T) {
	var out, errOut bytes.Buffer
	err := runLog([]string{"-", "--format=compact"}, strings.NewReader("{not json}\n"), &out, &errOut, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "invalid") || !strings.Contains(errOut.String(), "supported") {
		t.Fatalf("error=%v stderr=%q", err, errOut.String())
	}
}

func TestLogHelpDoesNotOpenInput(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := runLog([]string{"--help"}, strings.NewReader(""), &out, &errOut, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "usage: aiqa log") {
		t.Fatalf("help=%q", out.String())
	}
}

func TestRunApplicationFirstInterruptCancelsAndReturnsPartial(t *testing.T) {
	signals := make(chan os.Signal, 1)
	started := make(chan struct{})
	go func() {
		<-started
		signals <- os.Interrupt
	}()
	out, err, code, interrupted := runApplication(context.Background(), config.Config{}, signals, func(ctx context.Context, _ config.Config, _ app.BuildInfo) (app.RunResult, error) {
		close(started)
		<-ctx.Done()
		return app.RunResult{ExitCode: 2, Stdout: []byte("partial\n")}, ctx.Err()
	})
	if !interrupted || code != 130 || err != context.Canceled || string(out.Stdout) != "partial\n" {
		t.Fatalf("out=%+v err=%v code=%d interrupted=%v", out, err, code, interrupted)
	}
}

func TestRunApplicationSecondInterruptStopsWaiting(t *testing.T) {
	signals := make(chan os.Signal, 2)
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	go func() {
		<-started
		signals <- os.Interrupt
		signals <- syscall.SIGTERM
	}()
	_, err, code, interrupted := runApplication(context.Background(), config.Config{}, signals, func(context.Context, config.Config, app.BuildInfo) (app.RunResult, error) {
		close(started)
		<-release
		return app.RunResult{}, nil
	})
	if err != nil || !interrupted || code != 143 {
		t.Fatalf("err=%v code=%d interrupted=%v", err, code, interrupted)
	}
}

func TestRunApplicationNaturalCompletionKeepsVerdictExit(t *testing.T) {
	out, err, code, interrupted := runApplication(context.Background(), config.Config{}, make(chan os.Signal), func(context.Context, config.Config, app.BuildInfo) (app.RunResult, error) {
		return app.RunResult{ExitCode: 1}, nil
	})
	if err != nil || interrupted || code != 0 || out.ExitCode != 1 {
		t.Fatalf("out=%+v err=%v code=%d interrupted=%v", out, err, code, interrupted)
	}
}

func TestRunCLIRuntimeFailureIsExitTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCLI([]string{"--base-url=http://127.0.0.1:8080/v1", "--api-key=test", "--allow-http"}, strings.NewReader(""), &stdout, &stderr, nil, func(context.Context, config.Config, app.BuildInfo) (app.RunResult, error) {
		return app.RunResult{ExitCode: 0}, os.ErrClosed
	})
	if code != 2 || !strings.Contains(stderr.String(), "[FATAL]") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}
