package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/ilyar/aiqa/internal/extsuite"
	"github.com/ilyar/aiqa/internal/result"
)

const (
	extensionID = "official-sdk.openai"
	sdkVersion  = "v3.54.0"
)

type Extension struct{}

func New() *Extension { return &Extension{} }

func (e *Extension) Manifest() extsuite.Manifest {
	return extsuite.Manifest{
		SchemaVersion:    extsuite.ProtocolVersion,
		Type:             "manifest",
		Extension:        extsuite.ExtensionInfo{ID: extensionID, Name: "OpenAI Go SDK", Version: sdkVersion},
		ReportContracts:  []string{"aiqa.report.v3"},
		ProfileContracts: []string{"aiqa.profile.v3"},
		Protocols:        []string{"openai"},
		Suites: []extsuite.SuiteDescriptor{
			{ID: "chat.completions", Subject: "model", Capabilities: []string{"chat.completions.text"}, Cost: "text", InternalConcurrency: 1},
			{ID: "chat.json", Subject: "model", Capabilities: []string{"chat.completions.text"}, Cost: "text", InternalConcurrency: 1},
			{ID: "chat.multi-turn", Subject: "model", Capabilities: []string{"chat.completions.text"}, Cost: "text", InternalConcurrency: 1},
			{ID: "chat.stream", Subject: "model", Capabilities: []string{"chat.completions.text"}, Cost: "text", InternalConcurrency: 1},
			{ID: "chat.stream.usage", Subject: "model", Capabilities: []string{"chat.completions.text"}, Cost: "text", InternalConcurrency: 1},
			{ID: "chat.tools", Subject: "model", Capabilities: []string{"chat.completions.text"}, Cost: "text", InternalConcurrency: 1},
			{ID: "chat.tools.stream", Subject: "model", Capabilities: []string{"chat.completions.text"}, Cost: "text", InternalConcurrency: 1},
			{ID: "error.responses", Subject: "model", Capabilities: []string{"chat.completions.text"}, Cost: "text", InternalConcurrency: 1},
			{ID: "images.generations", Subject: "model", Capabilities: []string{"images.generations"}, Cost: "billable_artifact", InternalConcurrency: 1},
			{ID: "models.get", Subject: "model", Cost: "text", InternalConcurrency: 1},
			{ID: "models.list", Subject: "provider", Cost: "text", InternalConcurrency: 1},
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
	}
	if request.Runtime.HTTPClient != nil {
		clientOptions = append(clientOptions, option.WithHTTPClient(request.Runtime.HTTPClient))
	}
	client := openaisdk.NewClient(clientOptions...)
	err := extsuite.ExecuteBuiltinSuites(ctx, request, extensionID, sink, func(suiteCtx context.Context, suiteID string) extsuite.SuiteEvent {
		return runSuite(suiteCtx, &client, request, suiteID)
	})
	return extsuite.RunnerTelemetry{TotalDuration: time.Since(started)}, err
}

func runSuite(ctx context.Context, client *openaisdk.Client, request extsuite.RunRequest, suiteID string) extsuite.SuiteEvent {
	started := time.Now()
	event := extsuite.SuiteEvent{SchemaVersion: extsuite.ProtocolVersion, Type: "suite_result", RunID: request.RunID, ExtensionID: extensionID, SuiteID: suiteID, Subject: request.Subject}
	if request.Limits.RequestTimeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(request.Limits.RequestTimeoutMS)*time.Millisecond)
		defer cancel()
	}
	model := request.Subject.ID
	var err error
	switch suiteID {
	case "models.list":
		page, listErr := client.Models.List(ctx)
		err = listErr
		if err == nil && (page == nil || len(page.Data) == 0) {
			err = errors.New("SDK returned an empty model page")
		}
	case "models.get":
		var got *openaisdk.Model
		got, err = client.Models.Get(ctx, model)
		if err == nil && (got == nil || got.ID != model) {
			err = fmt.Errorf("SDK returned model %q instead of %q", modelID(got), model)
		}
	case "chat.completions":
		var completion *openaisdk.ChatCompletion
		completion, err = client.Chat.Completions.New(ctx, basicParams(model, "Reply with exactly AIQA_OK."))
		if err == nil {
			err = validateCompletion(completion, true)
			if completion != nil {
				event.Usage = completionUsage(completion.Usage)
			}
		}
	case "chat.stream":
		var accumulated openaisdk.ChatCompletionAccumulator
		accumulated, err = streamCompletion(ctx, client, basicParams(model, "Reply with exactly AIQA_OK."))
		if err == nil {
			err = validateCompletion(&accumulated.ChatCompletion, true)
		}
	case "chat.stream.usage":
		var accumulated openaisdk.ChatCompletionAccumulator
		accumulated, err = streamCompletion(ctx, client, basicParams(model, "Reply with exactly AIQA_OK."), option.WithJSONSet("stream_options", map[string]any{"include_usage": true}))
		if err == nil && accumulated.Usage.TotalTokens <= 0 {
			err = errors.New("SDK parsed the stream without terminal usage")
		}
		if err == nil {
			event.Usage = completionUsage(accumulated.Usage)
		}
	case "chat.json":
		var completion *openaisdk.ChatCompletion
		completion, err = client.Chat.Completions.New(ctx, basicParams(model, "Return only JSON with boolean field ok set to true."), option.WithJSONSet("response_format", map[string]any{"type": "json_object"}))
		if err == nil {
			if validationErr := validateCompletion(completion, false); validationErr != nil {
				err = validationErr
			} else {
				var object map[string]any
				if json.Unmarshal([]byte(completion.Choices[0].Message.Content), &object) != nil || object["ok"] != true {
					err = errors.New("SDK parsed a response that violated the requested JSON object")
				}
			}
		}
	case "chat.tools":
		err = runTools(ctx, client, model)
	case "chat.tools.stream":
		params := basicParams(model, "Call get_status for resource-alpha.")
		var accumulated openaisdk.ChatCompletionAccumulator
		accumulated, err = streamCompletion(ctx, client, params, toolOptions()...)
		if err == nil && !hasToolCall(&accumulated.ChatCompletion) {
			err = errors.New("SDK parsed the stream without a tool call")
		}
	case "chat.multi-turn":
		params := basicParams(model, "Return the marker you were asked to remember.")
		params.Messages = []openaisdk.ChatCompletionMessageParamUnion{openaisdk.SystemMessage("Keep responses concise."), openaisdk.UserMessage("Remember AIQA_OK."), openaisdk.AssistantMessage("I remember it."), openaisdk.UserMessage("Return the marker.")}
		var completion *openaisdk.ChatCompletion
		completion, err = client.Chat.Completions.New(ctx, params)
		if err == nil {
			err = validateCompletion(completion, false)
		}
	case "error.responses":
		_, gotErr := client.Chat.Completions.New(ctx, basicParams("aiqa-missing-model", "Reply with AIQA_OK."))
		if gotErr == nil {
			err = errors.New("invalid model request unexpectedly succeeded")
		} else {
			var apiErr *openaisdk.Error
			if !errors.As(gotErr, &apiErr) {
				err = fmt.Errorf("SDK did not parse an OpenAI API error: %w", gotErr)
			} else if apiErr.StatusCode != 400 && apiErr.StatusCode != 404 && apiErr.StatusCode != 422 {
				err = fmt.Errorf("invalid model returned unexpected HTTP %d", apiErr.StatusCode)
			}
		}
	case "images.generations":
		var images *openaisdk.ImagesResponse
		images, err = client.Images.Generate(ctx, openaisdk.ImageGenerateParams{Prompt: "A plain blue circle on a white background", Model: openaisdk.ImageModel(model)}, option.WithJSONSet("n", 1))
		if err == nil && (images == nil || len(images.Data) == 0 || images.Data[0].B64JSON == "" && images.Data[0].URL == "") {
			err = errors.New("SDK parsed an image response without image data")
		}
	default:
		err = fmt.Errorf("unknown OpenAI SDK suite %q", suiteID)
	}
	duration := time.Since(started)
	event.DurationMS = duration.Milliseconds()
	event.DurationNS = duration.Nanoseconds()
	if err == nil {
		event.Outcome = result.ExtensionOutcomePass
		event.Summary = "OpenAI Go SDK sent and parsed the suite"
		return event
	}
	applyError(&event, err)
	return event
}

func basicParams(model, prompt string) openaisdk.ChatCompletionNewParams {
	return openaisdk.ChatCompletionNewParams{Model: openaisdk.ChatModel(model), Messages: []openaisdk.ChatCompletionMessageParamUnion{openaisdk.UserMessage(prompt)}}
}

func streamCompletion(ctx context.Context, client *openaisdk.Client, params openaisdk.ChatCompletionNewParams, options ...option.RequestOption) (openaisdk.ChatCompletionAccumulator, error) {
	stream := client.Chat.Completions.NewStreaming(ctx, params, options...)
	defer stream.Close()
	var accumulator openaisdk.ChatCompletionAccumulator
	for stream.Next() {
		if !accumulator.AddChunk(stream.Current()) {
			return accumulator, errors.New("SDK stream accumulator rejected a chunk")
		}
	}
	return accumulator, stream.Err()
}

func toolOptions() []option.RequestOption {
	tool := map[string]any{"type": "function", "function": map[string]any{"name": "get_status", "description": "Return resource status.", "parameters": map[string]any{"type": "object", "properties": map[string]any{"resource_id": map[string]any{"type": "string"}}, "required": []string{"resource_id"}}}}
	return []option.RequestOption{option.WithJSONSet("tools", []map[string]any{tool}), option.WithJSONSet("tool_choice", map[string]any{"type": "function", "function": map[string]any{"name": "get_status"}})}
}

func runTools(ctx context.Context, client *openaisdk.Client, model string) error {
	completion, err := client.Chat.Completions.New(ctx, basicParams(model, "Call get_status for resource-alpha."), toolOptions()...)
	if err != nil {
		return err
	}
	if !hasToolCall(completion) {
		return errors.New("SDK parsed a response without a tool call")
	}
	call := completion.Choices[0].Message.ToolCalls[0].AsFunction()
	if call.ID == "" || call.Function.Name != "get_status" || !json.Valid([]byte(call.Function.Arguments)) {
		return errors.New("SDK parsed an invalid forced tool call")
	}
	params := basicParams(model, "")
	params.Messages = []openaisdk.ChatCompletionMessageParamUnion{openaisdk.UserMessage("Call get_status for resource-alpha."), completion.Choices[0].Message.ToParam(), openaisdk.ToolMessage("resource-alpha is available", call.ID)}
	final, err := client.Chat.Completions.New(ctx, params)
	if err != nil {
		return err
	}
	if err := validateCompletion(final, false); err != nil {
		return err
	}
	if !strings.Contains(strings.ToLower(final.Choices[0].Message.Content), "available") {
		return errors.New("tool continuation was not reflected in final text")
	}
	return nil
}

func validateCompletion(completion *openaisdk.ChatCompletion, requireMarker bool) error {
	if completion == nil || completion.ID == "" || len(completion.Choices) == 0 || string(completion.Choices[0].Message.Role) != "assistant" {
		return errors.New("SDK parsed an incomplete Chat Completion")
	}
	if requireMarker && !strings.Contains(completion.Choices[0].Message.Content, "AIQA_OK") {
		return errors.New("SDK parsed a completion without the requested marker")
	}
	return nil
}

func hasToolCall(completion *openaisdk.ChatCompletion) bool {
	return completion != nil && len(completion.Choices) > 0 && len(completion.Choices[0].Message.ToolCalls) > 0
}

func modelID(model *openaisdk.Model) string {
	if model == nil {
		return ""
	}
	return model.ID
}

func completionUsage(usage openaisdk.CompletionUsage) result.Usage {
	return result.Usage{PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens, TotalTokens: usage.TotalTokens, ReasoningTokens: usage.CompletionTokensDetails.ReasoningTokens}
}

func applyError(event *extsuite.SuiteEvent, err error) {
	event.Outcome = result.ExtensionOutcomeAssertionFailed
	event.Summary = err.Error()
	event.Error = err.Error()
	var apiErr *openaisdk.Error
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
	case "models.get", "chat.json", "chat.tools", "chat.tools.stream", "images.generations":
		return true
	default:
		return false
	}
}
