//go:build ignore

// Command example generates successful and intentionally failing AIQA reports.
// It is a repository maintenance command and is not linked into the AIQA binary.
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
	exampleDir := filepath.Join(projectRoot, "example")

	providerPath, err := exec.LookPath("model-provider")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			fmt.Fprintln(stderr, "WARNING: model-provider was not found in PATH; example reports were not generated")
			return nil
		}
		return fmt.Errorf("find model-provider: %w", err)
	}
	aiqaPath, err := exec.LookPath("aiqa")
	if err != nil {
		return fmt.Errorf("find aiqa: %w", err)
	}

	running, err := providerRunning(ctx, providerPath)
	if err != nil {
		fmt.Fprintf(stderr, "WARNING: model-provider status could not be read: %v\n", err)
	}
	if !running {
		fmt.Fprintln(stdout, "model-provider is not running; starting it")
		if err := runCommand(ctx, "", nil, stdout, stderr, providerPath,
			"up", "--force", "--storage", envOrDefault("AIQA_EXAMPLE_PROVIDER_STORAGE", defaultProviderStorage),
			"--listen", envOrDefault("AIQA_EXAMPLE_PROVIDER_LISTEN", defaultProviderListen)); err != nil {
			return fmt.Errorf("start model-provider: %w", err)
		}
	} else {
		fmt.Fprintln(stdout, "model-provider is running")
	}

	providerOutput, err := commandOutput(ctx, "", nil, stderr, providerPath, "env")
	if err != nil {
		return fmt.Errorf("read model-provider environment: %w", err)
	}
	providerEnv, err := parseExports(providerOutput)
	if err != nil {
		return fmt.Errorf("parse model-provider environment: %w", err)
	}
	goodEnv := mergeEnvironment(os.Environ(), providerEnv,
		"GOOGLE_GENAI_BASE_URL", "GOOGLE_GENAI_API_KEY", "GEMINI_API_KEY")

	fmt.Fprintln(stdout, "Generating successful example reports")
	if err := runCommand(ctx, exampleDir, goodEnv, stdout, stderr, aiqaPath,
		"--verbose", "--concurrency", "16", "--timeout=60",
		"test=compat,ext-suite", "--ext-suite=official-sdk",
		"--format=markdown,json", "--name=good-report"); err != nil {
		return fmt.Errorf("generate successful reports: %w", err)
	}
	if err := validateGoodReport(exampleDir); err != nil {
		return err
	}

	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("Docker is required to generate the WireMock failure report: %w", err)
	}
	wireMockImage := envOrDefault("AIQA_EXAMPLE_WIREMOCK_IMAGE", defaultWireMockImage)
	wireMockContainer := fmt.Sprintf("aiqa-example-wiremock-%d", os.Getpid())
	wireMockRoot := filepath.Join(projectRoot, "test", "wiremock")

	fmt.Fprintf(stdout, "Starting WireMock %s\n", wireMockImage)
	if _, err := commandOutput(ctx, "", nil, stderr, dockerPath,
		"run", "--rm", "--detach",
		"--name", wireMockContainer,
		"--publish", "127.0.0.1::8080/tcp",
		"--volume", wireMockRoot+":/home/wiremock:ro",
		wireMockImage); err != nil {
		return fmt.Errorf("start WireMock: %w", err)
	}
	wireMockStarted := true
	defer func() {
		if wireMockStarted {
			if err := stopContainer(dockerPath, wireMockContainer); err != nil {
				fmt.Fprintf(stderr, "WARNING: stop WireMock: %v\n", err)
			}
		}
	}()

	portOutput, err := commandOutput(ctx, "", nil, stderr, dockerPath, "port", wireMockContainer, "8080/tcp")
	if err != nil {
		return fmt.Errorf("resolve WireMock port: %w", err)
	}
	wireMockAddress := firstNonEmptyLine(string(portOutput))
	if wireMockAddress == "" {
		return errors.New("Docker did not publish the WireMock HTTP port")
	}
	wireMockURL := "http://" + wireMockAddress
	if err := waitForHTTP(ctx, wireMockURL+"/__admin/health", 10*time.Second); err != nil {
		return fmt.Errorf("wait for WireMock: %w", err)
	}

	badEnv := mergeEnvironment(os.Environ(), map[string]string{
		"OPENAI_BASE_URL":       wireMockURL + "/openai/v1",
		"OPENAI_API_KEY":        "wiremock-example-key",
		"ANTHROPIC_BASE_URL":    wireMockURL + "/anthropic",
		"ANTHROPIC_API_KEY":     "wiremock-example-key",
		"GOOGLE_GENAI_BASE_URL": wireMockURL + "/google",
		"GOOGLE_GENAI_API_KEY":  "wiremock-example-key",
	})
	fmt.Fprintln(stdout, "Generating intentionally failing example reports")
	badExit, err := runCommandAllowExit(ctx, exampleDir, badEnv, stdout, stderr, aiqaPath,
		"--verbose", "--concurrency", "16", "--timeout=60",
		"test=compat,ext-suite", "--target=openai,anthropic,google-genai",
		"--google-genai-model=wiremock-broken-model", "--ext-suite=official-sdk",
		"--format=markdown,json", "--name=bad-report")
	if err != nil {
		return fmt.Errorf("generate failing reports: %w", err)
	}
	if badExit == 0 {
		return errors.New("intentionally failing AIQA run unexpectedly exited successfully")
	}
	if err := validateBadReport(exampleDir); err != nil {
		return err
	}

	if err := stopContainer(dockerPath, wireMockContainer); err != nil {
		return fmt.Errorf("stop WireMock: %w", err)
	}
	wireMockStarted = false
	fmt.Fprintln(stdout, "Generated example/good-report.{md,json} and example/bad-report.{md,json}")
	return nil
}

func providerRunning(ctx context.Context, providerPath string) (bool, error) {
	output, err := commandOutput(ctx, "", nil, io.Discard, providerPath, "--json", "status")
	if err != nil {
		return false, err
	}
	var status statusDocument
	if err := json.Unmarshal(output, &status); err != nil {
		return false, fmt.Errorf("decode status JSON: %w", err)
	}
	return status.Running, nil
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
	removed := make(map[string]bool, len(unset)+len(overrides))
	for _, name := range unset {
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
	if report.Summary.Verdict != "pass" {
		return fmt.Errorf("good-report verdict is %q, want pass", report.Summary.Verdict)
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
	if report.Summary.Verdict != "inconclusive" {
		return fmt.Errorf("bad-report verdict is %q, want inconclusive", report.Summary.Verdict)
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
