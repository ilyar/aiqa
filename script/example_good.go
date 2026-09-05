//go:build ignore

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func generateGood(ctx context.Context, stdout, stderr io.Writer, projectRoot string) (bool, error) {
	exampleDir := filepath.Join(projectRoot, "example")

	providerPath, err := exec.LookPath("model-provider")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			fmt.Fprintln(stderr, "WARNING: model-provider was not found in PATH; example reports were not generated")
			return false, nil
		}
		return false, fmt.Errorf("find model-provider: %w", err)
	}
	aiqaPath, err := exec.LookPath("aiqa")
	if err != nil {
		return false, fmt.Errorf("find aiqa: %w", err)
	}
	providerConfig := strings.TrimSpace(os.Getenv("AIQA_EXAMPLE_PROVIDER_CONFIG"))

	running, err := providerRunning(ctx, providerPath, providerConfig)
	if err != nil {
		fmt.Fprintf(stderr, "WARNING: model-provider status could not be read: %v\n", err)
	}
	if !running {
		fmt.Fprintln(stdout, "model-provider is not running; starting it")
		if err := startProvider(ctx, providerPath, providerConfig, stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "WARNING: model-provider could not be started; example reports were not generated: %v\n", err)
			return false, nil
		}
	} else {
		fmt.Fprintln(stdout, "model-provider is running")
	}

	providerOutput, err := commandOutput(ctx, "", nil, io.Discard, providerPath, providerCommandArgs(providerConfig, "env")...)
	if err != nil {
		fmt.Fprintf(stderr, "WARNING: model-provider environment is unavailable; example reports were not generated: %v\n", err)
		return false, nil
	}
	providerEnv, err := parseExports(providerOutput)
	if err != nil {
		fmt.Fprintf(stderr, "WARNING: model-provider environment is invalid; example reports were not generated: %v\n", err)
		return false, nil
	}
	goodEnv := mergeEnvironment(os.Environ(), providerEnv,
		"GOOGLE_GENAI_BASE_URL", "GOOGLE_GENAI_API_KEY", "GEMINI_API_KEY", "GOOGLE_GENAI_MODEL")

	fmt.Fprintln(stdout, "Generating provider example reports")
	if err := runCommand(ctx, exampleDir, goodEnv, stdout, stderr, aiqaPath,
		"--verbose", "--concurrency", "16", "--timeout=60", "--fail-on=never", "test=compat,sdk",
		"--target=openai,anthropic,google-genai",
		"--format=markdown,json", "--name=good-report"); err != nil {
		return false, fmt.Errorf("generate provider reports: %w", err)
	}
	if err := validateGoodReport(exampleDir); err != nil {
		return false, err
	}
	return true, nil
}

func providerRunning(ctx context.Context, providerPath, providerConfig string) (bool, error) {
	output, err := commandOutput(ctx, "", nil, io.Discard, providerPath, providerCommandArgs(providerConfig, "--json", "status")...)
	if err != nil {
		return false, err
	}
	var status statusDocument
	if err := json.Unmarshal(output, &status); err != nil {
		return false, fmt.Errorf("decode status JSON: %w", err)
	}
	return status.Running, nil
}

func startProvider(ctx context.Context, providerPath, providerConfig string, stdout, stderr io.Writer) error {
	var providerStdout, providerStderr bytes.Buffer
	err := runCommand(ctx, "", nil, &providerStdout, &providerStderr, providerPath, providerCommandArgs(providerConfig,
		"up", "--force", "--storage", envOrDefault("AIQA_EXAMPLE_PROVIDER_STORAGE", defaultProviderStorage),
		"--listen", envOrDefault("AIQA_EXAMPLE_PROVIDER_LISTEN", defaultProviderListen))...)
	if err != nil {
		return normalizeProviderCommandError(err)
	}
	_, _ = io.Copy(stdout, &providerStdout)
	_, _ = io.Copy(stderr, &providerStderr)
	return nil
}

func normalizeProviderCommandError(err error) error {
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return fmt.Errorf("model-provider exited with status %d", exitError.ExitCode())
	}
	return err
}

func providerCommandArgs(providerConfig string, args ...string) []string {
	if providerConfig == "" {
		return args
	}
	commandArgs := make([]string, 0, len(args)+2)
	commandArgs = append(commandArgs, "--config", providerConfig)
	return append(commandArgs, args...)
}
