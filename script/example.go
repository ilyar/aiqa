//go:build ignore

// Command example coordinates the successful and intentionally failing AIQA
// report examples. It is a repository maintenance command and is not linked
// into the AIQA binary.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	defaultProviderStorage = "/arch/model"
	defaultProviderListen  = "0.0.0.0:8080"
	defaultWireMockImage   = "wiremock/wiremock:3.13.2"
)

type statusDocument struct {
	Running bool `json:"running"`
	Health  struct {
		OK bool `json:"ok"`
	} `json:"health"`
}

type reportDocument struct {
	Summary struct {
		Verdict string `json:"verdict"`
	} `json:"summary"`
	Targets []struct {
		ID      string `json:"id"`
		Summary struct {
			Error int `json:"error"`
		} `json:"summary"`
	} `json:"targets"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Stdout, os.Stderr))
}

func run(ctx context.Context, stdout, stderr io.Writer) int {
	if err := generate(ctx, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "ERROR: %v\n", err)
		return 1
	}
	return 0
}

func generate(ctx context.Context, stdout, stderr io.Writer) error {
	projectRoot, err := findProjectRoot()
	if err != nil {
		return err
	}
	goodGenerated, err := generateGood(ctx, stdout, stderr, projectRoot)
	if err != nil {
		return err
	}
	if !goodGenerated {
		return nil
	}
	if err := generateBad(ctx, stdout, stderr, projectRoot); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Generated example/good-report.{md,json} and example/bad-report.{md,json}")
	return nil
}

func parseExports(output []byte) (map[string]string, error) {
	exports := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		line, ok := strings.CutPrefix(line, "export ")
		if !ok {
			return nil, fmt.Errorf("line %d is not an export", lineNumber)
		}
		name, rawValue, ok := strings.Cut(line, "=")
		if !ok || !validEnvironmentName(name) {
			return nil, fmt.Errorf("line %d has an invalid variable name", lineNumber)
		}
		if _, duplicate := exports[name]; duplicate {
			return nil, fmt.Errorf("line %d repeats variable %s", lineNumber, name)
		}
		value, err := decodeShellQuoted(rawValue)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		exports[name] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan exports: %w", err)
	}
	if len(exports) == 0 {
		return nil, errors.New("no exports were returned")
	}
	return exports, nil
}

func validEnvironmentName(name string) bool {
	if name == "" || !isEnvironmentNameStart(name[0]) {
		return false
	}
	for index := 1; index < len(name); index++ {
		if !isEnvironmentNameStart(name[index]) && (name[index] < '0' || name[index] > '9') {
			return false
		}
	}
	return true
}

func isEnvironmentNameStart(value byte) bool {
	return value == '_' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func decodeShellQuoted(value string) (string, error) {
	value = strings.TrimSuffix(value, ";")
	if len(value) < 2 || value[0] != '\'' || value[len(value)-1] != '\'' {
		return "", errors.New("value is not single-quoted")
	}
	parts := strings.Split(value[1:len(value)-1], `'"'"'`)
	for _, part := range parts {
		if strings.ContainsRune(part, '\'') {
			return "", errors.New("value contains unsupported shell quoting")
		}
	}
	return strings.Join(parts, "'"), nil
}

func mergeEnvironment(base []string, overrides map[string]string, unset ...string) []string {
	unsets := make(map[string]bool, len(unset))
	for _, name := range unset {
		unsets[name] = true
	}
	removed := make(map[string]bool, len(unset)+len(overrides))
	for name := range unsets {
		removed[name] = true
	}
	for name := range overrides {
		removed[name] = true
	}

	merged := make([]string, 0, len(base)+len(overrides))
	for _, item := range base {
		name, _, ok := strings.Cut(item, "=")
		if ok && !removed[name] {
			merged = append(merged, item)
		}
	}
	keys := make([]string, 0, len(overrides))
	for name := range overrides {
		if unsets[name] {
			continue
		}
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		merged = append(merged, name+"="+overrides[name])
	}
	return merged
}

func validateGoodReport(exampleDir string) error {
	if err := requireNonEmpty(filepath.Join(exampleDir, "good-report.md")); err != nil {
		return err
	}
	report, err := loadReport(filepath.Join(exampleDir, "good-report.json"))
	if err != nil {
		return err
	}
	if strings.TrimSpace(report.Summary.Verdict) == "" {
		return errors.New("good-report verdict is empty")
	}
	want := map[string]bool{"openai": false, "anthropic": false, "google-genai": false}
	for _, target := range report.Targets {
		if _, expected := want[target.ID]; expected {
			want[target.ID] = true
		}
	}
	for target, seen := range want {
		if !seen {
			return fmt.Errorf("good-report target %s is missing", target)
		}
	}
	return nil
}

func validateBadReport(exampleDir string) error {
	if err := requireNonEmpty(filepath.Join(exampleDir, "bad-report.md")); err != nil {
		return err
	}
	report, err := loadReport(filepath.Join(exampleDir, "bad-report.json"))
	if err != nil {
		return err
	}
	if report.Summary.Verdict != "inconclusive" && report.Summary.Verdict != "fail" {
		return fmt.Errorf("bad-report verdict is %q, want fail or inconclusive", report.Summary.Verdict)
	}
	want := map[string]bool{"openai": false, "anthropic": false, "google-genai": false}
	for _, target := range report.Targets {
		if _, expected := want[target.ID]; !expected {
			continue
		}
		if target.Summary.Error == 0 {
			return fmt.Errorf("bad-report target %s has no error results", target.ID)
		}
		want[target.ID] = true
	}
	for target, seen := range want {
		if !seen {
			return fmt.Errorf("bad-report target %s is missing", target)
		}
	}
	return nil
}

func loadReport(path string) (reportDocument, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return reportDocument{}, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	var report reportDocument
	if err := json.Unmarshal(data, &report); err != nil {
		return report, fmt.Errorf("decode %s: %w", filepath.Base(path), err)
	}
	return report, nil
}

func requireNonEmpty(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", filepath.Base(path), err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("%s is empty", filepath.Base(path))
	}
	return nil
}

// removePrivateSidecars makes report regeneration idempotent. These files are
// private diagnostics, not example artifacts; keeping an old O_EXCL sidecar
// must not prevent a fresh public sample report from being generated.
func removePrivateSidecars(exampleDir, name string) error {
	for _, suffix := range []string{".log", ".trace-verdict.json"} {
		path := filepath.Join(exampleDir, name+suffix)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale private sidecar %s: %w", filepath.Base(path), err)
		}
	}
	return nil
}

func findProjectRoot() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	for {
		goMod, readErr := os.ReadFile(filepath.Join(directory, "go.mod"))
		if readErr == nil && bytes.Contains(goMod, []byte("module github.com/ilyar/aiqa")) {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errors.New("AIQA project root was not found")
		}
		directory = parent
	}
}

func waitForHTTP(ctx context.Context, endpoint string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				return nil
			}
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("%s did not become ready within %s", endpoint, timeout)
}

func commandOutput(ctx context.Context, directory string, environment []string, stderr io.Writer, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = directory
	command.Env = environment
	command.Stderr = stderr
	return command.Output()
}

func runCommand(ctx context.Context, directory string, environment []string, stdout, stderr io.Writer, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = directory
	command.Env = environment
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

func runCommandAllowExit(ctx context.Context, directory string, environment []string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	err := runCommand(ctx, directory, environment, stdout, stderr, name, args...)
	if err == nil {
		return 0, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode(), nil
	}
	return -1, err
}

func stopContainer(dockerPath, container string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, dockerPath, "stop", container)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Run()
}

func firstNonEmptyLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
