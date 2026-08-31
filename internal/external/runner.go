package external

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

const (
	binaryName = "openai-compatibility-tester"
	toolName   = "official-SDK"
)

var moduleSuites = map[string]struct {
	ModelEnv string
	Suites   []string
}{
	"discovery":            {Suites: []string{"models"}},
	"chat-completions":     {ModelEnv: "OPENAI_MODEL", Suites: []string{"models_get", "chat_completions", "chat_completions_stream", "chat_completions_stream_usage", "chat_completions_json", "chat_completions_tools", "chat_completions_tools_stream", "chat_completions_multi_turn", "error_responses"}},
	"images-generations":   {ModelEnv: "OPENAI_IMAGE_MODEL", Suites: []string{"images_generations"}},
	"embeddings":           {ModelEnv: "OPENAI_EMBEDDING_MODEL", Suites: []string{"embeddings", "embeddings_batch"}},
	"responses":            {ModelEnv: "OPENAI_RESPONSES_MODEL", Suites: []string{"responses", "responses_stream", "responses_json", "responses_tools"}},
	"audio-speech":         {ModelEnv: "OPENAI_TTS_MODEL", Suites: []string{"audio_speech"}},
	"audio-transcriptions": {ModelEnv: "OPENAI_WHISPER_MODEL", Suites: []string{"audio_transcriptions"}},
}

var suiteRequirements = map[string][]string{
	"models":                        {"models.list.schema"},
	"models_get":                    {"models.retrieve"},
	"chat_completions":              {"official-sdk.chat.completions"},
	"chat_completions_stream":       {"official-sdk.chat.stream"},
	"chat_completions_stream_usage": {"official-sdk.chat.stream.usage"},
	"chat_completions_json":         {"official-sdk.chat.json"},
	"chat_completions_tools":        {"official-sdk.chat.tools"},
	"chat_completions_tools_stream": {"official-sdk.chat.tools.stream"},
	"chat_completions_multi_turn":   {"official-sdk.chat.multi_turn"},
	"error_responses":               {"official-sdk.error.responses"},
	"images_generations":            {"official-sdk.images.generations"},
	"embeddings":                    {"official-sdk.embeddings"},
	"embeddings_batch":              {"official-sdk.embeddings.batch"},
	"responses":                     {"official-sdk.responses"},
	"responses_stream":              {"official-sdk.responses.stream"},
	"responses_json":                {"official-sdk.responses.json"},
	"responses_tools":               {"official-sdk.responses.tools"},
	"audio_speech":                  {"official-sdk.audio.speech"},
	"audio_transcriptions":          {"official-sdk.audio.transcriptions"},
}

var suiteRelatedRequirements = map[string][]string{
	"chat_completions":              {"chat.basic"},
	"chat_completions_stream":       {"chat.stream.sse"},
	"chat_completions_stream_usage": {"chat.stream.usage"},
	"chat_completions_json":         {"chat.structured.json_object"},
	"chat_completions_tools":        {"chat.tools.required", "chat.tools.forced"},
	"chat_completions_tools_stream": {"chat.tools.stream"},
	"chat_completions_multi_turn":   {"chat.multi_turn"},
	"error_responses":               {"chat.error.invalid_model", "chat.error.invalid_json", "chat.error.invalid_types"},
	"images_generations":            {"images.generate.basic"},
}

var (
	suiteStart  = regexp.MustCompile(`^==> running suite "([^"]+)"\s*$`)
	suiteResult = regexp.MustCompile(`^\s*(PASS|FAIL|SKIP|ERROR)\s+\(([^)]+)\):?\s*(.*)$`)
)

func Run(parent context.Context, cfg config.Config, model string, p profile.Profile) []result.ExternalResult {
	return RunModule(parent, cfg, model, p, "chat-completions")
}

func RunModule(parent context.Context, cfg config.Config, model string, p profile.Profile, module string) []result.ExternalResult {
	definition, ok := moduleSuites[module]
	if !ok {
		return runnerResult(model, result.StatusError, "unknown official-SDK module "+module, "", "")
	}
	suites := strings.Join(definition.Suites, ",")
	if cfg.ExternalMode == "off" {
		return runSuite(parent, cfg, model, p, suites, definition.ModelEnv)
	}
	var out []result.ExternalResult
	for _, suite := range definition.Suites {
		for attempt := 0; attempt < 3; attempt++ {
			got := runSuite(parent, cfg, model, p, suite, definition.ModelEnv)
			if len(got) == 1 && got[0].Suite == "" {
				return append(out, got...)
			}
			if !hasRateLimit(got) || attempt == 2 {
				out = append(out, got...)
				break
			}
			delay := time.Duration(1<<attempt)*time.Second + time.Duration(attempt)*100*time.Millisecond
			select {
			case <-parent.Done():
				return append(out, got...)
			case <-time.After(delay):
			}
		}
	}
	return out
}

func runSuite(parent context.Context, cfg config.Config, model string, p profile.Profile, suite, modelEnv string) []result.ExternalResult {
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
		args, baseURL := dockerRunArgs(cfg)
		cmd = exec.CommandContext(ctx, path, args...)
		cfg.BaseURL = baseURL
	default:
		return runnerResult(model, result.StatusError, "unknown official-SDK checker mode", "", "")
	}
	environment := []string{
		"OPENAI_BASE_URL=" + cfg.BaseURL,
		"OPENAI_API_KEY=" + cfg.APIKey,
		"TEST_SUITES=" + suite,
		"REQUEST_TIMEOUT=" + cfg.Timeout.String(),
	}
	if modelEnv != "" {
		environment = append(environment, modelEnv+"="+model)
	}
	cmd.Env = append(os.Environ(), environment...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	cleanOutput := redactAndTruncate(output.String(), cfg.APIKey, 16*1024)
	parsed := parseSuiteResults(output.String(), model)
	redactSuiteResults(parsed, cfg.APIKey)
	attachDiagnostics(parsed, cleanOutput)
	if len(parsed) > 0 {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return normalizeAgainstProfile(append(parsed, runnerResult(model, result.StatusError, "official-SDK checker timed out", ctx.Err().Error(), cleanOutput)...), p)
		}
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			return normalizeAgainstProfile(append(parsed, runnerResult(model, result.StatusError, "official-SDK checker terminated unexpectedly", err.Error(), cleanOutput)...), p)
		}
		return normalizeAgainstProfile(parsed, p)
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

func hasRateLimit(results []result.ExternalResult) bool {
	for _, r := range results {
		if r.Status == result.StatusError && strings.Contains(strings.ToLower(r.Summary), "rate limited") {
			return true
		}
	}
	return false
}

// normalizeAgainstProfile makes an external checker's stricter suite policy
// consistent with the selected AIQA compatibility profile. The official SDK
// checker considers Models.Get mandatory, while this profile marks the
// corresponding endpoint as optional.
func normalizeAgainstProfile(results []result.ExternalResult, p profile.Profile) []result.ExternalResult {
	for index := range results {
		r := &results[index]
		if r.Status == result.StatusFail && providerFailureStatus(r.Summary) {
			r.Status = result.StatusError
			if containsHTTPStatus(r.Summary, http.StatusTooManyRequests) {
				r.Summary = "rate limited; compatibility outcome is inconclusive: " + r.Summary
			} else {
				r.Summary = "provider or transport failure; compatibility outcome is inconclusive: " + r.Summary
			}
			continue
		}
		if r.Status != result.StatusFail || !isCapabilityStatus(r.Summary) {
			continue
		}
		probeIDs, reason := optionalSuiteProbes(*r)
		if len(probeIDs) == 0 || !allOptional(p, probeIDs) {
			continue
		}
		r.Status = result.StatusUnsupported
		r.Summary = "optional " + reason + " is not supported: " + r.Summary
	}
	return results
}

var providerFailure = regexp.MustCompile(`\b(?:408|429|5\d\d)\b`)

func providerFailureStatus(summary string) bool {
	return providerFailure.MatchString(summary)
}

func optionalSuiteProbes(r result.ExternalResult) ([]string, string) {
	switch r.Suite {
	case "models_get":
		return []string{"models.retrieve"}, "GET /models/{model}"
	case "chat_completions_tools":
		return []string{"chat.tools.forced", "chat.tools.required"}, "forced/required tool choice"
	case "chat_completions_tools_stream":
		return []string{"chat.tools.stream"}, "streaming tool calls"
	case "chat_completions_multi_turn":
		if strings.Contains(strings.ToLower(r.Summary), "developer") {
			return []string{"chat.role.developer"}, "developer role"
		}
	}
	return nil, ""
}

func allOptional(p profile.Profile, ids []string) bool {
	for _, id := range ids {
		if p.Probe(id).Required {
			return false
		}
	}
	return true
}

func isCapabilityStatus(summary string) bool {
	return containsHTTPStatus(summary, 400) || containsHTTPStatus(summary, 404) || containsHTTPStatus(summary, 422)
}

func attachDiagnostics(results []result.ExternalResult, output string) {
	for i := range results {
		if results[i].Status == result.StatusFail || results[i].Status == result.StatusError {
			results[i].Output = output
		}
	}
}

func containsHTTPStatus(summary string, status int) bool {
	return regexp.MustCompile(`\b` + strconv.Itoa(status) + `\b`).MatchString(summary)
}

func dockerRunArgs(cfg config.Config) ([]string, string) {
	args := []string{"run", "--rm"}
	baseURL := cfg.BaseURL
	if isLoopbackURL(baseURL) && runtime.GOOS == "linux" {
		// A Docker container has its own loopback network namespace. Host network
		// mode lets the checker reach an endpoint bound to 127.0.0.1 on Linux.
		args = append(args, "--network", "host")
	}
	args = append(args,
		"-e", "OPENAI_BASE_URL",
		"-e", "OPENAI_API_KEY",
		"-e", "OPENAI_MODEL",
		"-e", "OPENAI_IMAGE_MODEL",
		"-e", "OPENAI_EMBEDDING_MODEL",
		"-e", "OPENAI_RESPONSES_MODEL",
		"-e", "OPENAI_TTS_MODEL",
		"-e", "OPENAI_WHISPER_MODEL",
		"-e", "TEST_SUITES",
		"-e", "REQUEST_TIMEOUT",
		cfg.ExternalImage,
	)
	return args, baseURL
}

func isLoopbackURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.TrimSpace(u.Hostname())
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
			Tool:                  toolName,
			Suite:                 current,
			Model:                 model,
			Status:                status,
			DurationMS:            duration.Milliseconds(),
			Summary:               summary,
			RequirementIDs:        append([]string(nil), suiteRequirements[current]...),
			RelatedRequirementIDs: append([]string(nil), suiteRelatedRequirements[current]...),
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
	s = redactSensitiveText(s, secret)
	s = strings.TrimSpace(s)
	if len(s) > max {
		return s[:max] + fmt.Sprintf("\n… truncated %d bytes", len(s)-max)
	}
	return s
}

func redactSuiteResults(results []result.ExternalResult, secret string) {
	for i := range results {
		results[i].Summary = redactAndTruncate(results[i].Summary, secret, 4*1024)
		results[i].Error = redactAndTruncate(results[i].Error, secret, 4*1024)
	}
}

var externalCookieSecret = regexp.MustCompile(`(?i)((?:cookie|set-cookie)\s*:\s*)[^\r\n]+`)
var externalBearerSecret = regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)[^\s]+`)
var externalJSONSecret = regexp.MustCompile(`(?i)("(?:api[_-]?key|authorization|access[_-]?token|cookie|tool[_-]?arguments|arguments)"\s*:\s*")[^"]+(")`)
var externalSignedURL = regexp.MustCompile(`(https?://[^\s"'<>?]+)\?[^\s"'<>)]*`)
var externalLongBase64 = regexp.MustCompile(`[A-Za-z0-9+/]{256,}={0,2}`)

func redactSensitiveText(s, secret string) string {
	if secret != "" {
		s = strings.ReplaceAll(s, secret, "[REDACTED]")
	}
	// Avoid accidentally exposing bearer-like values echoed by third-party tools.
	s = externalBearerSecret.ReplaceAllString(s, `${1}[REDACTED]`)
	s = externalCookieSecret.ReplaceAllString(s, `${1}[REDACTED]`)
	s = externalJSONSecret.ReplaceAllString(s, `${1}[REDACTED]${2}`)
	s = externalSignedURL.ReplaceAllString(s, `${1}?[REDACTED]`)
	s = externalLongBase64.ReplaceAllString(s, `[REDACTED BASE64]`)
	return s
}
