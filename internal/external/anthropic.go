package external

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/result"
)

const anthropicBinaryName = "anthropic-sdk-checker"

var anthropicRequirements = map[string][]string{
	"models_list": {"official-sdk.anthropic.models.list"}, "models_get": {"official-sdk.anthropic.models.get"},
	"messages": {"official-sdk.anthropic.messages"}, "messages_stream": {"official-sdk.anthropic.messages.stream"},
	"system": {"official-sdk.anthropic.system"}, "multi_turn": {"official-sdk.anthropic.multi_turn"},
	"tools": {"official-sdk.anthropic.tools"}, "tools_stream": {"official-sdk.anthropic.tools.stream"},
	"count_tokens": {"official-sdk.anthropic.count_tokens"}, "structured_output": {"official-sdk.anthropic.structured_output"},
	"error_responses": {"official-sdk.anthropic.error_responses"},
}

var anthropicRelated = map[string][]string{
	"models_list": {"anthropic.models.list.schema"}, "models_get": {"anthropic.models.retrieve"},
	"messages": {"anthropic.messages.basic", "anthropic.messages.schema"}, "messages_stream": {"anthropic.messages.stream.sequence"},
	"system": {"anthropic.messages.system.top_level"}, "multi_turn": {"anthropic.messages.multi_turn"},
	"tools": {"anthropic.tools.specific", "anthropic.tools.continuation"}, "tools_stream": {"anthropic.tools.stream"},
	"count_tokens": {"anthropic.tokens.count.basic"}, "structured_output": {"anthropic.structured.json_schema"},
	"error_responses": {"anthropic.error.invalid_model", "anthropic.error.shape"},
}

type anthropicEntry struct {
	SchemaVersion string                  `json:"schema_version"`
	Tool          string                  `json:"tool"`
	ToolVersion   string                  `json:"tool_version"`
	Suite         string                  `json:"suite"`
	Status        result.Status           `json:"status"`
	DurationMS    int64                   `json:"duration_ms"`
	Summary       string                  `json:"summary"`
	Signals       []result.EvidenceSignal `json:"signals,omitempty"`
}

func RunAnthropic(parent context.Context, target config.TargetConfig, timeout time.Duration, model string, suites []string) []result.ExternalResult {
	if target.ExternalMode == "off" {
		return []result.ExternalResult{{Tool: "anthropic-official-sdk", Model: model, Status: result.StatusSkip, Summary: "not setup"}}
	}
	mode, path, reason := resolveAnthropicMode(parent, target.ExternalMode, target.ExternalImage)
	if mode == "off" {
		return []result.ExternalResult{{Tool: "anthropic-official-sdk", Model: model, Status: result.StatusSkip, Summary: reason}}
	}
	if mode == "error" {
		return []result.ExternalResult{{Tool: "anthropic-official-sdk", Model: model, Status: result.StatusError, Summary: reason}}
	}
	ctx, cancel := context.WithTimeout(parent, timeout*time.Duration(maxInt(1, len(suites))))
	defer cancel()
	var cmd *exec.Cmd
	suiteList := strings.Join(suites, ",")
	if mode == "binary" {
		cmd = exec.CommandContext(ctx, path)
	} else {
		cmd = exec.CommandContext(ctx, path, anthropicDockerArgs(target)...)
	}
	cmd.Env = cleanAnthropicEnvironment(target, suiteList, model)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	parsed, parseErr := parseAnthropicJSONL(stdout.String(), model, target.APIKey)
	if parseErr != nil {
		parsed = append(parsed, result.ExternalResult{Tool: "anthropic-official-sdk", Model: model, Status: result.StatusError, Summary: "checker emitted malformed JSONL", Error: parseErr.Error()})
	}
	if len(parsed) > 0 {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			parsed = append(parsed, result.ExternalResult{Tool: "anthropic-official-sdk", Model: model, Status: result.StatusError, Summary: "checker timed out"})
		}
		return parsed
	}
	clean := redactAndTruncate(stderr.String(), target.APIKey, 16*1024)
	if err != nil {
		return []result.ExternalResult{{Tool: "anthropic-official-sdk", Model: model, Status: result.StatusError, Summary: "checker failed before reporting a suite", Error: err.Error(), Output: clean}}
	}
	return []result.ExternalResult{{Tool: "anthropic-official-sdk", Model: model, Status: result.StatusError, Summary: "checker produced no suite results", Output: clean}}
}

func anthropicDockerArgs(target config.TargetConfig) []string {
	args := []string{"run", "--rm"}
	if runtime.GOOS == "linux" && isLoopbackURL(target.BaseURL) {
		args = append(args, "--network", "host")
	}
	return append(args, "-e", "ANTHROPIC_BASE_URL", "-e", "ANTHROPIC_API_KEY", "-e", "ANTHROPIC_MODEL", "-e", "AIQA_SUITES", target.ExternalImage)
}

func resolveAnthropicMode(parent context.Context, mode, image string) (string, string, string) {
	if mode == "off" {
		return "off", "", "not setup"
	}
	if mode == "auto" || mode == "binary" {
		path, err := exec.LookPath(anthropicBinaryName)
		if err == nil {
			return "binary", path, ""
		}
		if mode == "binary" {
			return "error", "", anthropicBinaryName + " is not installed"
		}
		docker, dockerErr := exec.LookPath("docker")
		if dockerErr != nil {
			return "off", "", "not setup"
		}
		ctx, cancel := context.WithTimeout(parent, 5*time.Second)
		defer cancel()
		if exec.CommandContext(ctx, docker, "image", "inspect", image, "--format", "{{.Id}}").Run() != nil {
			return "off", "", "not setup"
		}
		return "docker", docker, ""
	}
	if mode == "docker" {
		path, err := exec.LookPath("docker")
		if err != nil {
			return "error", "", "docker is not installed"
		}
		return "docker", path, ""
	}
	return "error", "", fmt.Sprintf("unknown Anthropic external mode %q", mode)
}

func cleanAnthropicEnvironment(target config.TargetConfig, suites, model string) []string {
	var out []string
	for _, item := range os.Environ() {
		name, _, _ := strings.Cut(item, "=")
		upper := strings.ToUpper(name)
		if strings.HasPrefix(upper, "ANTHROPIC_") || upper == "AIQA_SUITES" {
			continue
		}
		out = append(out, item)
	}
	return append(out, "ANTHROPIC_BASE_URL="+target.BaseURL, "ANTHROPIC_API_KEY="+target.APIKey, "ANTHROPIC_MODEL="+model, "AIQA_SUITES="+suites)
}

func parseAnthropicJSONL(raw, model, key string) ([]result.ExternalResult, error) {
	var out []result.ExternalResult
	var problems []string
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		var entry anthropicEntry
		if err := json.Unmarshal([]byte(text), &entry); err != nil {
			problems = append(problems, fmt.Sprintf("line %d: %v", line, err))
			continue
		}
		_, knownSuite := anthropicRequirements[entry.Suite]
		validStatus := entry.Status == result.StatusPass || entry.Status == result.StatusFail || entry.Status == result.StatusUnsupported || entry.Status == result.StatusError
		if entry.SchemaVersion != "aiqa.external.v1" || entry.Tool != "anthropic-official-sdk" || !knownSuite || !validStatus || entry.DurationMS < 0 || strings.TrimSpace(entry.Summary) == "" {
			problems = append(problems, fmt.Sprintf("line %d: invalid envelope", line))
			continue
		}
		summary := strings.ReplaceAll(entry.Summary, key, "[REDACTED]")
		out = append(out, result.ExternalResult{Tool: entry.Tool + " " + entry.ToolVersion, Suite: entry.Suite, Model: model, Status: entry.Status, DurationMS: entry.DurationMS, Summary: summary, RequirementIDs: append([]string(nil), anthropicRequirements[entry.Suite]...), RelatedRequirementIDs: append([]string(nil), anthropicRelated[entry.Suite]...), Signals: entry.Signals})
	}
	if err := scanner.Err(); err != nil {
		problems = append(problems, err.Error())
	}
	if len(problems) > 0 {
		return out, errors.New(strings.Join(problems, "; "))
	}
	return out, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
