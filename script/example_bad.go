//go:build ignore

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func generateBad(ctx context.Context, stdout, stderr io.Writer, projectRoot string) error {
	exampleDir := filepath.Join(projectRoot, "example")
	if err := removePrivateSidecars(exampleDir, "bad-report"); err != nil {
		return err
	}
	aiqaPath, err := exec.LookPath("aiqa")
	if err != nil {
		return fmt.Errorf("find aiqa: %w", err)
	}
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("Docker is required to generate the WireMock failure report: %w", err)
	}
	wireMockImage := envOrDefault("AIQA_WIREMOCK_IMAGE", defaultWireMockImage)
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
		"--concurrency", "16", "--timeout=60",
		"test=compat,sdk", "--target=openai,anthropic,google-genai",
		"--model=wiremock-broken-model",
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
	return nil
}
