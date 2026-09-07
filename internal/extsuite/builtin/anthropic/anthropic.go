package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/extsuite"
	"github.com/ilyar/aiqa/internal/result"
)

const (
	extensionID = "official-sdk.anthropic"
	sdkVersion  = "v1.68.0"
)

type Extension struct{}

func New() *Extension { return &Extension{} }

func (e *Extension) Manifest() extsuite.Manifest {
	return extsuite.Manifest{
		SchemaVersion:    extsuite.ProtocolVersion,
		Type:             "manifest",
		Extension:        extsuite.ExtensionInfo{ID: extensionID, Name: "Anthropic Go SDK", Version: sdkVersion},
		ReportContracts:  []string{"aiqa.report.v3"},
		ProfileContracts: []string{"aiqa.profile.v3"},
		Protocols:        []string{"anthropic"},
		Suites: []extsuite.SuiteDescriptor{
			{ID: "count-tokens", Subject: "model", Capabilities: []string{"anthropic.messages.text"}, Cost: "text", InternalConcurrency: 1},
			{ID: "error.responses", Subject: "model", Capabilities: []string{"anthropic.messages.text"}, Cost: "text", InternalConcurrency: 1},
			{ID: "messages", Subject: "model", Capabilities: []string{"anthropic.messages.text"}, Cost: "text", InternalConcurrency: 1},
			{ID: "messages.multi-turn", Subject: "model", Capabilities: []string{"anthropic.messages.text"}, Cost: "text", InternalConcurrency: 1},
			{ID: "messages.stream", Subject: "model", Capabilities: []string{"anthropic.messages.text"}, Cost: "text", InternalConcurrency: 1},
			{ID: "messages.system", Subject: "model", Capabilities: []string{"anthropic.messages.text"}, Cost: "text", InternalConcurrency: 1},
			{ID: "models.get", Subject: "model", Cost: "text", InternalConcurrency: 1},
			{ID: "models.list", Subject: "provider", Cost: "text", InternalConcurrency: 1},
			{ID: "structured-output", Subject: "model", Capabilities: []string{"anthropic.messages.text"}, Cost: "text", InternalConcurrency: 1},
			{ID: "tools", Subject: "model", Capabilities: []string{"anthropic.messages.tools"}, Cost: "text", InternalConcurrency: 1},
			{ID: "tools.stream", Subject: "model", Capabilities: []string{"anthropic.messages.tools"}, Cost: "text", InternalConcurrency: 1},
		},
	}
}

func (e *Extension) Provenance() result.ExtensionProvenance {
	manifest := e.Manifest()
	return result.ExtensionProvenance{ID: extensionID, Name: manifest.Extension.Name, Version: sdkVersion, Source: "builtin", ProtocolVersion: extsuite.ProtocolVersion}
}

func (e *Extension) Run(ctx context.Context, request extsuite.RunRequest, sink extsuite.EventSink) (extsuite.RunnerTelemetry, error) {
	started := time.Now()
	clientOptions := []option.RequestOption{
		option.WithBaseURL(strings.TrimRight(request.Target.BaseURL, "/") + "/"),
		option.WithAPIKey(request.Credential.Value),
		option.WithMaxRetries(0),
		option.WithHeader("anthropic-version", request.Target.APIVersion),
		option.WithMiddleware(escapeModelPathSegments),
	}
	if request.Runtime.HTTPClient != nil {
		clientOptions = append(clientOptions, option.WithHTTPClient(request.Runtime.HTTPClient))
	}
	client := anthropicsdk.NewClient(clientOptions...)
	err := extsuite.ExecuteBuiltinSuites(ctx, request, extensionID, sink, func(suiteCtx context.Context, suiteID string) extsuite.SuiteEvent {
		return runSuite(suiteCtx, &client, request, suiteID)
	})
	return extsuite.RunnerTelemetry{TotalDuration: time.Since(started)}, err
}

func runSuite(ctx context.Context, client *anthropicsdk.Client, request extsuite.RunRequest, suiteID string) extsuite.SuiteEvent {
	started := time.Now()
	event := extsuite.SuiteEvent{SchemaVersion: extsuite.ProtocolVersion, Type: "suite_result", RunID: request.RunID, ExtensionID: extensionID, SuiteID: suiteID, Subject: request.Subject}
	if request.Limits.RequestTimeoutMS > 0 {
		var cancel context.CancelFunc
		if request.Runtime.HTTPClient != nil {
			ctx, cancel = api.WithRequestTimeout(ctx, time.Duration(request.Limits.RequestTimeoutMS)*time.Millisecond)
		} else {
			ctx, cancel = context.WithTimeout(ctx, time.Duration(request.Limits.RequestTimeoutMS)*time.Millisecond)
		}
		defer cancel()
	}
	usage, err := runCase(ctx, client, suiteID, request.Subject.ID)
	duration := time.Since(started)
	event.DurationMS = duration.Milliseconds()
	event.DurationNS = duration.Nanoseconds()
	event.Usage = usage
	if err == nil {
		event.Outcome = result.ExtensionOutcomePass
		event.Summary = "Anthropic Go SDK sent and parsed the suite"
		return event
	}
	applyError(&event, err)
	return event
}

func runCase(ctx context.Context, client *anthropicsdk.Client, suiteID, model string) (result.Usage, error) {
	params := messageParams(model, "Reply with exactly AIQA_OK.")
	switch suiteID {
	case "models.list":
		page, err := client.Models.List(ctx, anthropicsdk.ModelListParams{})
		if err != nil {
			return result.Usage{}, err
		}
		if len(page.Data) == 0 {
			return result.Usage{}, errors.New("models list is empty")
		}
		return result.Usage{}, nil
	case "models.get":
		got, err := client.Models.Get(ctx, model, anthropicsdk.ModelGetParams{})
		if err != nil {
			return result.Usage{}, err
		}
		if got.ID != model {
			return result.Usage{}, fmt.Errorf("SDK returned model %q instead of %q", got.ID, model)
		}
		return result.Usage{}, nil
	case "messages":
		message, err := client.Messages.New(ctx, params)
		if err != nil {
			return result.Usage{}, err
		}
		return messageUsage(message), validateSDKMessage(message, true)
	case "messages.stream":
		stream := client.Messages.NewStreaming(ctx, params, option.WithHeader("Accept", "text/event-stream"))
		defer stream.Close()
		message := anthropicsdk.Message{}
		for stream.Next() {
			if err := message.Accumulate(stream.Current()); err != nil {
				return messageUsage(&message), err
			}
		}
		if err := stream.Err(); err != nil {
			return messageUsage(&message), err
		}
		return messageUsage(&message), validateSDKMessage(&message, true)
	case "messages.system":
		message, err := client.Messages.New(ctx, params, option.WithJSONSet("system", "Reply with exactly AIQA_OK."))
		if err != nil {
			return result.Usage{}, err
		}
		return messageUsage(message), validateSDKMessage(message, true)
	case "messages.multi-turn":
		params.Messages = []anthropicsdk.MessageParam{
			anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("Remember the code word AIQA_OK.")),
			anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock("I will remember that the code word is AIQA_OK.")),
			anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("What is the code word from the first user message? Reply with only that code word.")),
		}
		message, err := client.Messages.New(ctx, params)
		if err != nil {
			return result.Usage{}, err
		}
		return messageUsage(message), validateSDKMessage(message, true)
	case "tools":
		return runTools(ctx, client, model, messageParams(model, toolPrompt))
	case "tools.stream":
		streamParams := messageParams(model, toolPrompt)
		stream := client.Messages.NewStreaming(ctx, streamParams, option.WithHeader("Accept", "text/event-stream"), option.WithJSONSet("tools", []map[string]any{toolDefinition()}), option.WithJSONSet("tool_choice", map[string]any{"type": "tool", "name": "get_status"}))
		defer stream.Close()
		message := anthropicsdk.Message{}
		for stream.Next() {
			if err := message.Accumulate(stream.Current()); err != nil {
				return messageUsage(&message), err
			}
		}
		if err := stream.Err(); err != nil {
			return messageUsage(&message), err
		}
		_, err := sdkToolUse(&message)
		return messageUsage(&message), err
	case "count-tokens":
		count, err := client.Messages.CountTokens(ctx, anthropicsdk.MessageCountTokensParams{Model: model, Messages: params.Messages})
		if err != nil {
			return result.Usage{}, err
		}
		if count.InputTokens <= 0 {
			return result.Usage{}, errors.New("input_tokens is not positive")
		}
		return result.Usage{InputTokens: count.InputTokens, TotalTokens: count.InputTokens}, nil
	case "structured-output":
		message, err := client.Messages.New(ctx, params, option.WithJSONSet("output_config.format", map[string]any{"type": "json_schema", "schema": map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}, "value": map[string]any{"type": "string"}}, "required": []string{"ok"}, "additionalProperties": false}}))
		if err != nil {
			return result.Usage{}, err
		}
		var value map[string]any
		for _, block := range message.Content {
			if block.Type == "text" && json.Unmarshal([]byte(block.Text), &value) == nil && value["ok"] == true && onlyKeys(value, "ok", "value") {
				return messageUsage(message), nil
			}
		}
		return messageUsage(message), errors.New("SDK parsed a message that violated the requested JSON schema")
	case "error.responses":
		params.Model = "aiqa-missing-model"
		_, err := client.Messages.New(ctx, params)
		if err == nil {
			return result.Usage{}, errors.New("invalid model request unexpectedly succeeded")
		}
		var apiErr *anthropicsdk.Error
		if !errors.As(err, &apiErr) {
			return result.Usage{}, fmt.Errorf("SDK did not parse an Anthropic API error: %w", err)
		}
		if apiErr.StatusCode != 400 && apiErr.StatusCode != 404 && apiErr.StatusCode != 422 {
			return result.Usage{}, fmt.Errorf("invalid model returned unexpected HTTP %d: %w", apiErr.StatusCode, err)
		}
		if apiErr.Type() == "" {
			return result.Usage{}, fmt.Errorf("SDK parsed an error without error.type: %w", err)
		}
		return result.Usage{}, nil
	default:
		return result.Usage{}, fmt.Errorf("unknown Anthropic SDK suite %q", suiteID)
	}
}

func onlyKeys(value map[string]any, allowed ...string) bool {
	known := map[string]bool{}
	for _, key := range allowed {
		known[key] = true
	}
	for key := range value {
		if !known[key] {
			return false
		}
	}
	return true
}

func runTools(ctx context.Context, client *anthropicsdk.Client, model string, params anthropicsdk.MessageNewParams) (result.Usage, error) {
	tool := toolDefinition()
	message, err := client.Messages.New(ctx, params, option.WithJSONSet("tools", []map[string]any{tool}), option.WithJSONSet("tool_choice", map[string]any{"type": "tool", "name": "get_status"}))
	if err != nil {
		return result.Usage{}, err
	}
	usage := messageUsage(message)
	block, err := sdkToolUse(message)
	if err != nil {
		return usage, err
	}
	continuation := messageParams(model, "Call get_status for resource-alpha.")
	messages := []map[string]any{{"role": "user", "content": "Call get_status for resource-alpha."}, {"role": "assistant", "content": message.Content}, {"role": "user", "content": []map[string]any{{"type": "tool_result", "tool_use_id": block.ID, "content": "resource-alpha is available"}}}}
	final, err := client.Messages.New(ctx, continuation, option.WithJSONSet("messages", messages))
	if err != nil {
		return usage, err
	}
	usage = usage.Add(messageUsage(final))
	if !sdkMessageContains(final, "available") {
		return usage, errors.New("tool_result continuation was not reflected in final text")
	}
	return usage, nil
}

func validateSDKMessage(message *anthropicsdk.Message, requireMarker bool) error {
	if message == nil || message.ID == "" || message.Type != "message" || string(message.Role) != "assistant" || len(message.Content) == 0 {
		return errors.New("SDK parsed an incomplete Anthropic Message")
	}
	if message.Usage.InputTokens < 0 || message.Usage.OutputTokens < 0 {
		return errors.New("SDK parsed negative message usage")
	}
	if requireMarker && !sdkMessageContains(message, "AIQA_OK") {
		return errors.New("SDK parsed a Message without the requested marker")
	}
	return nil
}

func sdkMessageContains(message *anthropicsdk.Message, marker string) bool {
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

func sdkToolUse(message *anthropicsdk.Message) (anthropicsdk.ToolUseBlock, error) {
	if message == nil {
		return anthropicsdk.ToolUseBlock{}, errors.New("tool response is nil")
	}
	for _, block := range message.Content {
		if block.Type != "tool_use" || block.Name != "get_status" || block.ID == "" {
			continue
		}
		var input map[string]any
		if json.Unmarshal(block.Input, &input) != nil || !onlyKeys(input, "resource_id") || input["resource_id"] != "resource-alpha" {
			return anthropicsdk.ToolUseBlock{}, errors.New("SDK parsed tool_use with invalid input")
		}
		return block.AsToolUse(), nil
	}
	return anthropicsdk.ToolUseBlock{}, errors.New("SDK parsed no get_status tool_use block")
}

const toolPrompt = "Call get_status for resource-alpha."

func toolDefinition() map[string]any {
	return map[string]any{
		"name":        "get_status",
		"description": "Return status.",
		"input_schema": map[string]any{
			"type":                 "object",
			"properties":           map[string]any{"resource_id": map[string]any{"type": "string"}},
			"required":             []string{"resource_id"},
			"additionalProperties": false,
		},
	}
}

func messageParams(model, prompt string) anthropicsdk.MessageNewParams {
	return anthropicsdk.MessageNewParams{Model: model, MaxTokens: 32, Messages: []anthropicsdk.MessageParam{anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(prompt))}}
}

func messageUsage(message *anthropicsdk.Message) result.Usage {
	if message == nil {
		return result.Usage{}
	}
	return result.Usage{InputTokens: message.Usage.InputTokens, OutputTokens: message.Usage.OutputTokens, CacheCreationInputTokens: message.Usage.CacheCreationInputTokens, CacheReadInputTokens: message.Usage.CacheReadInputTokens, TotalTokens: message.Usage.InputTokens + message.Usage.OutputTokens + message.Usage.CacheCreationInputTokens + message.Usage.CacheReadInputTokens}
}

func applyError(event *extsuite.SuiteEvent, err error) {
	event.Outcome = result.ExtensionOutcomeAssertionFailed
	event.Summary = err.Error()
	event.Error = err.Error()
	var apiErr *anthropicsdk.Error
	if errors.As(err, &apiErr) {
		event.Outcome = result.ExtensionOutcomeRequestRejected
		event.HTTP.StatusCode = apiErr.StatusCode
		if capabilitySuite(event.SuiteID) {
			event.ErrorKind = "capability_rejection"
		}
		return
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		event.Outcome = result.ExtensionOutcomeExecutionError
		return
	}
	var urlErr *url.Error
	var netErr net.Error
	if errors.As(err, &urlErr) || errors.As(err, &netErr) {
		event.Outcome = result.ExtensionOutcomeExecutionError
	}
}

func capabilitySuite(suiteID string) bool {
	switch suiteID {
	case "models.get", "messages.system", "messages.multi-turn", "tools", "tools.stream", "count-tokens", "structured-output":
		return true
	default:
		return false
	}
}

func escapeModelPathSegments(request *http.Request, next option.MiddlewareNext) (*http.Response, error) {
	const marker = "/v1/models/"
	if request.Method == http.MethodGet {
		if index := strings.LastIndex(request.URL.Path, marker); index >= 0 {
			prefix := request.URL.Path[:index+len(marker)]
			modelID := request.URL.Path[index+len(marker):]
			if modelID != "" {
				request.URL.RawPath = prefix + url.PathEscape(modelID)
			}
		}
	}
	return next(request)
}
