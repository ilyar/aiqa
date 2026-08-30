package external

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/result"
)

const (
	binaryName = "openai-compatibility-tester"
	suites     = "models,models_get,chat_completions,chat_completions_stream,chat_completions_stream_usage,chat_completions_json,chat_completions_tools,chat_completions_tools_stream,chat_completions_multi_turn,error_responses"
)

func Run(parent context.Context, cfg config.Config, model string) result.ExternalResult {
	res := result.ExternalResult{
		Tool:  "beranekio/openai-compatibility-tester",
		Mode:  cfg.ExternalMode,
		Model: model,
	}
	mode, path, reason := resolveMode(parent, cfg.ExternalMode, cfg.ExternalImage)
	if mode == "off" {
		res.Status = result.StatusSkip
		res.Summary = reason
		return res
	}
	if mode == "error" {
		res.Status = result.StatusError
		res.Summary = reason
		return res
	}
	res.Mode = mode

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
		res.Status = result.StatusError
		res.Summary = "unknown resolved external checker mode"
		return res
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
	started := time.Now()
	err := cmd.Run()
	res.DurationMS = time.Since(started).Milliseconds()
	res.Output = redactAndTruncate(output.String(), cfg.APIKey, 16*1024)
	if err == nil {
		res.Status = result.StatusPass
		res.Summary = "official Go SDK compatibility smoke suites passed"
		return res
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		res.Status = result.StatusError
		res.Summary = "external compatibility checker timed out"
		res.Error = ctx.Err().Error()
		return res
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		res.Status = result.StatusFail
		res.Summary = "external official-SDK compatibility suites reported failures"
		res.Error = err.Error()
		return res
	}
	res.Status = result.StatusError
	res.Summary = "external compatibility checker could not start"
	res.Error = err.Error()
	return res
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
