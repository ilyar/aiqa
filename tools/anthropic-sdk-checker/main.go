package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type entry struct {
	SchemaVersion string   `json:"schema_version"`
	Tool          string   `json:"tool"`
	ToolVersion   string   `json:"tool_version"`
	Suite         string   `json:"suite"`
	Status        string   `json:"status"`
	DurationMS    int64    `json:"duration_ms"`
	Summary       string   `json:"summary"`
	Signals       []signal `json:"signals,omitempty"`
}

type signal struct {
	Code string `json:"code"`
}

func main() {
	baseURL, key, model := os.Getenv("ANTHROPIC_BASE_URL"), os.Getenv("ANTHROPIC_API_KEY"), os.Getenv("ANTHROPIC_MODEL")
	suites := split(os.Getenv("AIQA_SUITES"))
	if len(suites) == 0 {
		suites = []string{"models_list", "models_get", "messages", "messages_stream", "system", "multi_turn", "tools", "tools_stream", "count_tokens", "structured_output", "error_responses"}
	}
	// From this point forward, the SDK receives only explicit target-bound
	// options. Ambient SDK credentials, auth tokens and beta headers cannot alter
	// the checker target.
	os.Clearenv()
	client := anthropic.NewClient(option.WithBaseURL(strings.TrimRight(baseURL, "/")+"/"), option.WithAPIKey(key), option.WithMaxRetries(0), option.WithHeader("anthropic-version", "2023-06-01"), option.WithMiddleware(escapeModelPathSegments))
	ctx := context.Background()
	for _, suite := range suites {
		started := time.Now()
		err := run(ctx, &client, suite, model)
		item := entry{SchemaVersion: "aiqa.external.v1", Tool: "anthropic-official-sdk", ToolVersion: "v1.68.0", Suite: suite, Status: "pass", Summary: "Official Anthropic Go SDK executed and parsed the suite"}
		if err != nil {
			item.Status = classifySuiteError(suite, err)
			item.Summary = sanitize(err.Error(), key)
			item.Signals = []signal{{Code: "anthropic.sdk." + strings.ReplaceAll(suite, "_", ".") + ".failed"}}
		}
		item.DurationMS = time.Since(started).Milliseconds()
		_ = json.NewEncoder(os.Stdout).Encode(item)
	}
}

// The generated v1.68.0 SDK interpolates model_id directly into URL.Path. Go
// then treats a slash inside an opaque provider model ID as a path separator.
// Preserve the SDK request and decoder while correcting only that path
// parameter at the transport boundary.
func escapeModelPathSegments(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
	const marker = "/v1/models/"
	if req.Method == http.MethodGet {
		if index := strings.LastIndex(req.URL.Path, marker); index >= 0 {
			prefix := req.URL.Path[:index+len(marker)]
			modelID := req.URL.Path[index+len(marker):]
			if modelID != "" {
				req.URL.RawPath = prefix + url.PathEscape(modelID)
			}
		}
	}
	return next(req)
}

func classifySuiteError(suite string, err error) string {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		return "error"
	}
	if apiErr.StatusCode == 408 || apiErr.StatusCode == 429 || apiErr.StatusCode >= 500 {
		return "error"
	}
	optional := map[string]bool{
		"models_get": true, "system": true, "multi_turn": true,
		"tools": true, "tools_stream": true, "count_tokens": true,
		"structured_output": true,
	}
	if optional[suite] && (apiErr.StatusCode == 400 || apiErr.StatusCode == 404 || apiErr.StatusCode == 422) {
		return "unsupported"
	}
	return "fail"
}

func run(ctx context.Context, client *anthropic.Client, suite, model string) error {
	params := messageParams(model, "Reply with exactly AIQA_OK.")
	switch suite {
	case "models_list":
		page, err := client.Models.List(ctx, anthropic.ModelListParams{})
		if err != nil {
			return err
		}
		if len(page.Data) == 0 {
			return errors.New("models list is empty")
		}
		return nil
	case "models_get":
		if model == "" {
			return errors.New("ANTHROPIC_MODEL is required")
		}
		got, err := client.Models.Get(ctx, model, anthropic.ModelGetParams{})
		if err != nil {
			return err
		}
		if got.ID != model {
			return fmt.Errorf("returned model %q", got.ID)
		}
		return nil
	case "messages":
		message, err := client.Messages.New(ctx, params)
		if err != nil {
			return err
		}
		return validateSDKMessage(message, true)
	case "messages_stream":
		stream := client.Messages.NewStreaming(ctx, params)
		message := anthropic.Message{}
		for stream.Next() {
			if err := message.Accumulate(stream.Current()); err != nil {
				return err
			}
		}
		if err := stream.Err(); err != nil {
			return err
		}
		return validateSDKMessage(&message, true)
	case "system":
		message, err := client.Messages.New(ctx, params, option.WithJSONSet("system", "Reply with exactly AIQA_OK."))
		if err != nil {
			return err
		}
		return validateSDKMessage(message, true)
	case "multi_turn":
		params.Messages = []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("Remember AIQA_OK.")), anthropic.NewAssistantMessage(anthropic.NewTextBlock("I remember it.")), anthropic.NewUserMessage(anthropic.NewTextBlock("Return the marker."))}
		message, err := client.Messages.New(ctx, params)
		if err != nil {
			return err
		}
		return validateSDKMessage(message, true)
	case "tools":
		tool := map[string]any{"name": "get_status", "description": "Return status.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"resource_id": map[string]any{"type": "string"}}, "required": []string{"resource_id"}}}
		message, err := client.Messages.New(ctx, params, option.WithJSONSet("tools", []map[string]any{tool}), option.WithJSONSet("tool_choice", map[string]any{"type": "tool", "name": "get_status"}))
		if err != nil {
			return err
		}
		block, err := sdkToolUse(message)
		if err != nil {
			return err
		}
		continuation := messageParams(model, "Call get_status for resource-alpha.")
		messages := []map[string]any{
			{"role": "user", "content": "Call get_status for resource-alpha."},
			{"role": "assistant", "content": message.Content},
			{"role": "user", "content": []map[string]any{{"type": "tool_result", "tool_use_id": block.ID, "content": "resource-alpha is available"}}},
		}
		final, err := client.Messages.New(ctx, continuation, option.WithJSONSet("messages", messages))
		if err != nil {
			return err
		}
		if !sdkMessageContains(final, "available") {
			return errors.New("tool_result continuation was not reflected in final text")
		}
		return nil
	case "tools_stream":
		stream := client.Messages.NewStreaming(ctx, params, option.WithJSONSet("tools", []map[string]any{{"name": "get_status", "description": "Return status.", "input_schema": map[string]any{"type": "object"}}}), option.WithJSONSet("tool_choice", map[string]any{"type": "tool", "name": "get_status"}))
		message := anthropic.Message{}
		for stream.Next() {
			if err := message.Accumulate(stream.Current()); err != nil {
				return err
			}
		}
		if err := stream.Err(); err != nil {
			return err
		}
		_, err := sdkToolUse(&message)
		return err
	case "count_tokens":
		count, err := client.Messages.CountTokens(ctx, anthropic.MessageCountTokensParams{Model: model, Messages: params.Messages})
		if err != nil {
			return err
		}
		if count.InputTokens <= 0 {
			return errors.New("input_tokens is not positive")
		}
		return nil
	case "structured_output":
		message, err := client.Messages.New(ctx, params, option.WithJSONSet("output_config.format", map[string]any{"type": "json_schema", "schema": map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}, "required": []string{"ok"}, "additionalProperties": false}}))
		if err != nil {
			return err
		}
		var value map[string]any
		for _, block := range message.Content {
			if block.Type == "text" && json.Unmarshal([]byte(block.Text), &value) == nil && len(value) == 1 && value["ok"] == true {
				return nil
			}
		}
		return errors.New("SDK parsed message but structured output violated the requested schema")
	case "error_responses":
		params.Model = "aiqa-missing-model"
		_, err := client.Messages.New(ctx, params)
		if err == nil {
			return errors.New("invalid model request unexpectedly succeeded")
		}
		var apiErr *anthropic.Error
		if !errors.As(err, &apiErr) {
			return fmt.Errorf("SDK did not parse Anthropic error: %w", err)
		}
		if apiErr.StatusCode != 400 && apiErr.StatusCode != 404 && apiErr.StatusCode != 422 {
			return fmt.Errorf("invalid model returned unexpected HTTP %d: %w", apiErr.StatusCode, err)
		}
		if apiErr.Type() == "" {
			return fmt.Errorf("SDK parsed an error without Anthropic error.type: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown suite %q", suite)
	}
}

func validateSDKMessage(message *anthropic.Message, requireMarker bool) error {
	if message == nil || message.ID == "" || message.Type != "message" || string(message.Role) != "assistant" || len(message.Content) == 0 {
		return errors.New("SDK parsed an incomplete Anthropic Message")
	}
	if message.Usage.InputTokens < 0 || message.Usage.OutputTokens < 0 {
		return errors.New("SDK parsed negative message usage")
	}
	if requireMarker && !sdkMessageContains(message, "AIQA_OK") {
		return errors.New("SDK parsed Message without the requested marker")
	}
	return nil
}

func sdkMessageContains(message *anthropic.Message, marker string) bool {
	if message == nil {
		return false
	}
	for _, block := range message.Content {
		if block.Type == "text" && strings.Contains(block.Text, marker) {
			return true
		}
	}
	return false
}

func sdkToolUse(message *anthropic.Message) (anthropic.ToolUseBlock, error) {
	if message == nil {
		return anthropic.ToolUseBlock{}, errors.New("tool response is nil")
	}
	for _, block := range message.Content {
		if block.Type != "tool_use" || block.Name != "get_status" || block.ID == "" {
			continue
		}
		var input map[string]any
		if json.Unmarshal(block.Input, &input) != nil || input["resource_id"] != "resource-alpha" {
			return anthropic.ToolUseBlock{}, errors.New("SDK parsed tool_use with invalid input")
		}
		return block.AsToolUse(), nil
	}
	return anthropic.ToolUseBlock{}, errors.New("SDK parsed no get_status tool_use block")
}

func messageParams(model, prompt string) anthropic.MessageNewParams {
	return anthropic.MessageNewParams{Model: model, MaxTokens: 32, Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(prompt))}}
}
func split(raw string) []string {
	var out []string
	for _, s := range strings.Split(raw, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
func sanitize(value, key string) string {
	value = strings.ReplaceAll(value, key, "[REDACTED]")
	if len(value) > 1000 {
		return value[:1000] + "…"
	}
	return value
}
