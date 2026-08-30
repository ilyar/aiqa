package external

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/result"
)

const (
	binaryName = "openai-compatibility-tester"
	toolName   = "official-SDK"
	suites     = "models,models_get,chat_completions,chat_completions_stream,chat_completions_stream_usage,chat_completions_json,chat_completions_tools,chat_completions_tools_stream,chat_completions_multi_turn,error_responses"
)

var (
	suiteStart  = regexp.MustCompile(`^==> running suite "([^"]+)"\s*$`)
	suiteResult = regexp.MustCompile(`^\s*(PASS|FAIL|SKIP|ERROR)\s+\(([^)]+)\):?\s*(.*)$`)
)

func Run(parent context.Context, cfg config.Config, model string) []result.ExternalResult {
	mode, path, reason := resolveMode(parent, cfg.ExternalMode, cfg.ExternalImage)
	if mode == "off" {
		return runnerResult(model, result.StatusSkip, reason, "", "")
	}
	if mode == "error" {
		return runnerResult(model, result.StatusError, reason, "", "")
	}

	// The external checker executes several independent suites. Give the process
	// enough wall time while preserving the user's timeout for each suite.
	ctx, cancel := context.WithTimeout(parent, cfg.Timeout*12)
	defer cancel()

	var cmd *exec.Cmd
	switch mode {
	case "binary":
		cmd = exec.CommandContext(ctx, path)
	case "docker":
		cmd = exec.CommandContext(ctx, path, "run", "--rm",
			"-e", "OPENAI_BASE_URL",
			"-e", "OPENAI_API_KEY",
			"-e", "OPENAI_MODEL",
			"-e", "TEST_SUITES",
			"-e", "REQUEST_TIMEOUT",
			cfg.ExternalImage,
		)
	default:
		return runnerResult(model, result.StatusError, "unknown official-SDK checker mode", "", "")
	}
	cmd.Env = append(os.Environ(),
		"OPENAI_BASE_URL="+cfg.BaseURL,
		"OPENAI_API_KEY="+cfg.APIKey,
		"OPENAI_MODEL="+model,
		"TEST_SUITES="+suites,
		"REQUEST_TIMEOUT="+cfg.Timeout.String(),
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	cleanOutput := redactAndTruncate(output.String(), cfg.APIKey, 16*1024)
	parsed := parseSuiteResults(output.String(), model)
	if len(parsed) > 0 {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return append(parsed, runnerResult(model, result.StatusError, "official-SDK checker timed out", ctx.Err().Error(), cleanOutput)...)
		}
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			return append(parsed, runnerResult(model, result.StatusError, "official-SDK checker terminated unexpectedly", err.Error(), cleanOutput)...)
		}
		return parsed
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return runnerResult(model, result.StatusError, "official-SDK checker timed out before reporting a suite", ctx.Err().Error(), cleanOutput)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return runnerResult(model, result.StatusError, "official-SDK checker exited before reporting a suite", err.Error(), cleanOutput)
	}
	if err == nil {
		return runnerResult(model, result.StatusError, "official-SDK checker produced no suite results", "", cleanOutput)
	}
	return runnerResult(model, result.StatusError, "official-SDK checker could not start", err.Error(), cleanOutput)
}

func runnerResult(model string, status result.Status, summary, errText, output string) []result.ExternalResult {
	return []result.ExternalResult{{
		Tool:    toolName,
		Model:   model,
		Status:  status,
		Summary: summary,
		Error:   errText,
		Output:  output,
	}}
}

func parseSuiteResults(output, model string) []result.ExternalResult {
	var out []result.ExternalResult
	current := ""
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if match := suiteStart.FindStringSubmatch(line); match != nil {
			current = match[1]
			continue
		}
		match := suiteResult.FindStringSubmatch(line)
		if current == "" || match == nil {
			continue
		}
		duration, err := time.ParseDuration(match[2])
		if err != nil {
			continue
		}
		status := result.StatusFail
		switch match[1] {
		case "PASS":
			status = result.StatusPass
		case "SKIP":
			status = result.StatusSkip
		case "ERROR":
			status = result.StatusError
		}
		summary := strings.TrimSpace(match[3])
		if summary == "" {
			summary = strings.ToLower(match[1])
		}
		out = append(out, result.ExternalResult{
			Tool:       toolName,
			Suite:      current,
			Model:      model,
			Status:     status,
			DurationMS: duration.Milliseconds(),
			Summary:    summary,
		})
		current = ""
	}
	return out
}

func resolveMode(parent context.Context, requested, image string) (mode, path, reason string) {
	switch requested {
	case "off":
		return "off", "", "external checker disabled"
	case "binary":
		p, err := exec.LookPath(binaryName)
		if err != nil {
			return "error", "", binaryName + " was explicitly requested but is not installed"
		}
		return "binary", p, ""
	case "docker":
		p, err := exec.LookPath("docker")
		if err != nil {
			return "error", "", "docker was explicitly requested but is not installed"
		}
		return "docker", p, ""
	case "auto":
		if p, err := exec.LookPath(binaryName); err == nil {
			return "binary", p, ""
		}
		docker, err := exec.LookPath("docker")
		if err != nil {
			return "off", "", "no local external compatibility checker found"
		}
		ctx, cancel := context.WithTimeout(parent, 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, docker, "image", "inspect", image, "--format", "{{.Id}}")
		if err := cmd.Run(); err != nil {
			return "off", "", "external checker image is not present locally; built-in probes still ran"
		}
		return "docker", docker, ""
	default:
		return "off", "", "invalid external mode " + strconv.Quote(requested)
	}
}

func redactAndTruncate(s, secret string, max int) string {
	if secret != "" {
		s = strings.ReplaceAll(s, secret, "[REDACTED]")
	}
	// Avoid accidentally exposing bearer-like values echoed by third-party tools.
	for _, prefix := range []string{"Authorization: Bearer ", "authorization: bearer "} {
		for {
			idx := strings.Index(s, prefix)
			if idx < 0 {
				break
			}
			start := idx + len(prefix)
			end := start
			for end < len(s) && s[end] != '\n' && s[end] != '\r' && s[end] != ' ' {
				end++
			}
			s = s[:start] + "[REDACTED]" + s[end:]
		}
	}
	s = strings.TrimSpace(s)
	if len(s) > max {
		return s[:max] + fmt.Sprintf("\n… truncated %d bytes", len(s)-max)
	}
	return s
}
