package googlegenai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	genai "google.golang.org/genai"

	"github.com/ilyar/aiqa/internal/extsuite"
	protocolgoogle "github.com/ilyar/aiqa/internal/protocol/googlegenai"
	"github.com/ilyar/aiqa/internal/result"
)

const (
	extensionID = "official-sdk.google-genai"
	sdkVersion  = "v1.71.0"
)

type Extension struct{}

func New() *Extension { return &Extension{} }

func (e *Extension) Manifest() extsuite.Manifest {
	return extsuite.Manifest{
		SchemaVersion:    extsuite.ProtocolVersion,
		Type:             "manifest",
		Extension:        extsuite.ExtensionInfo{ID: extensionID, Name: "Google Gen AI Go SDK", Version: sdkVersion},
		ReportContracts:  []string{"aiqa.report.v3"},
		ProfileContracts: []string{"aiqa.profile.v3"},
		Protocols:        []string{"google-genai"},
		Suites: []extsuite.SuiteDescriptor{
			{ID: "models.list", Subject: "provider", Cost: "text", InternalConcurrency: 1},
			{ID: "models.get", Subject: "model", Cost: "text", InternalConcurrency: 1},
			{ID: "generate-content", Subject: "model", Capabilities: []string{"google-genai.generate-content.text"}, Cost: "text", InternalConcurrency: 1},
			{ID: "generate-content-stream", Subject: "model", Capabilities: []string{"google-genai.generate-content.text"}, Cost: "text", InternalConcurrency: 1},
			{ID: "count-tokens", Subject: "model", Capabilities: []string{"google-genai.count-tokens"}, Cost: "text", InternalConcurrency: 1},
			{ID: "embeddings", Subject: "model", Capabilities: []string{"google-genai.embed-content"}, Cost: "text", InternalConcurrency: 1},
			{ID: "tools", Subject: "model", Capabilities: []string{"google-genai.generate-content.tools"}, Cost: "text", InternalConcurrency: 1},
			{ID: "structured-output", Subject: "model", Capabilities: []string{"google-genai.generate-content.structured-output"}, Cost: "text", InternalConcurrency: 1},
			{ID: "multimodal", Subject: "model", Capabilities: []string{"google-genai.generate-content.image-input"}, Cost: "text", InternalConcurrency: 1},
		},
	}
}

func (e *Extension) Provenance() result.ExtensionProvenance {
	manifest := e.Manifest()
	return result.ExtensionProvenance{ID: extensionID, Name: manifest.Extension.Name, Version: sdkVersion, Source: "builtin", ProtocolVersion: extsuite.ProtocolVersion}
}

func (e *Extension) Run(ctx context.Context, request extsuite.RunRequest, sink extsuite.EventSink) (extsuite.RunnerTelemetry, error) {
	started := time.Now()
	attempts := int32(1)
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:     request.Credential.Value,
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: request.Runtime.HTTPClient,
		HTTPOptions: genai.HTTPOptions{
			BaseURL:      strings.TrimRight(request.Target.BaseURL, "/") + "/",
			APIVersion:   request.Target.APIVersion,
			RetryOptions: &genai.HTTPRetryOptions{Attempts: &attempts},
		},
	})
	if err != nil {
		return extsuite.RunnerTelemetry{TotalDuration: time.Since(started)}, err
	}
	err = extsuite.ExecuteBuiltinSuites(ctx, request, extensionID, sink, func(suiteCtx context.Context, suiteID string) extsuite.SuiteEvent {
		return runSuite(suiteCtx, client, request, suiteID)
	})
	return extsuite.RunnerTelemetry{TotalDuration: time.Since(started)}, err
}

func runSuite(parent context.Context, client *genai.Client, request extsuite.RunRequest, suiteID string) extsuite.SuiteEvent {
	started := time.Now()
	event := extsuite.SuiteEvent{SchemaVersion: extsuite.ProtocolVersion, Type: "suite_result", RunID: request.RunID, ExtensionID: extensionID, SuiteID: suiteID, Subject: request.Subject}
	ctx := parent
	if request.Limits.RequestTimeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(parent, time.Duration(request.Limits.RequestTimeoutMS)*time.Millisecond)
		defer cancel()
	}
	usage, err := runCase(ctx, client, suiteID, request.Subject.ID)
	duration := time.Since(started)
	event.DurationMS = duration.Milliseconds()
	event.DurationNS = duration.Nanoseconds()
	event.Usage = usage
	if err == nil {
		event.Outcome = result.ExtensionOutcomePass
		event.Summary = "Google Gen AI Go SDK sent and parsed the suite"
		return event
	}
	applyError(&event, err)
	return event
}

func runCase(ctx context.Context, client *genai.Client, suiteID, model string) (result.Usage, error) {
	model = strings.TrimPrefix(strings.TrimSpace(model), "models/")
	switch suiteID {
	case "models.list":
		page, err := client.Models.List(ctx, nil)
		if err != nil {
			return result.Usage{}, err
		}
		if len(page.Items) == 0 {
			return result.Usage{}, errors.New("SDK returned an empty model page")
		}
		return result.Usage{}, nil
	case "models.get":
		got, err := client.Models.Get(ctx, model, nil)
		if err != nil {
			return result.Usage{}, err
		}
		if got == nil || strings.TrimPrefix(got.Name, "models/") != model {
			return result.Usage{}, fmt.Errorf("SDK returned model %q instead of %q", modelName(got), model)
		}
		return result.Usage{}, nil
	case "generate-content":
		response, err := client.Models.GenerateContent(ctx, model, genai.Text("Reply with exactly AIQA_OK."), &genai.GenerateContentConfig{MaxOutputTokens: 32})
		if err != nil {
			return result.Usage{}, err
		}
		if response == nil || !strings.Contains(response.Text(), "AIQA_OK") {
			return responseUsage(response), errors.New("SDK response did not contain AIQA_OK")
		}
		return responseUsage(response), nil
	case "generate-content-stream":
		var text strings.Builder
		var usage result.Usage
		for response, err := range client.Models.GenerateContentStream(ctx, model, genai.Text("Reply with exactly AIQA_OK."), &genai.GenerateContentConfig{MaxOutputTokens: 32}) {
			if err != nil {
				return usage, err
			}
			if response != nil {
				text.WriteString(response.Text())
				usage = responseUsage(response)
			}
		}
		if !strings.Contains(text.String(), "AIQA_OK") {
			return usage, errors.New("SDK stream did not reconstruct AIQA_OK")
		}
		return usage, nil
	case "count-tokens":
		count, err := client.Models.CountTokens(ctx, model, genai.Text("Count this text."), nil)
		if err != nil {
			return result.Usage{}, err
		}
		if count == nil || count.TotalTokens <= 0 {
			return result.Usage{}, errors.New("SDK returned no positive token count")
		}
		return result.Usage{InputTokens: int64(count.TotalTokens), TotalTokens: int64(count.TotalTokens)}, nil
	case "embeddings":
		response, err := client.Models.EmbedContent(ctx, model, genai.Text("AIQA embedding probe"), nil)
		if err != nil {
			return result.Usage{}, err
		}
		if response == nil || len(response.Embeddings) == 0 || len(response.Embeddings[0].Values) == 0 {
			return result.Usage{}, errors.New("SDK returned an empty embedding")
		}
		return result.Usage{}, nil
	case "tools":
		tool := &genai.Tool{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "get_status", Description: "Return status.", Parameters: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{"resource_id": {Type: genai.TypeString}}, Required: []string{"resource_id"}}}}}
		response, err := client.Models.GenerateContent(ctx, model, genai.Text("Call get_status for resource-alpha."), &genai.GenerateContentConfig{Tools: []*genai.Tool{tool}, MaxOutputTokens: 32})
		if err != nil {
			return result.Usage{}, err
		}
		if response == nil || len(response.FunctionCalls()) == 0 {
			return responseUsage(response), errors.New("SDK returned no function call")
		}
		return responseUsage(response), nil
	case "structured-output":
		response, err := client.Models.GenerateContent(ctx, model, genai.Text("Return JSON with ok=true and value=AIQA_OK."), &genai.GenerateContentConfig{ResponseMIMEType: "application/json", ResponseSchema: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{"ok": {Type: genai.TypeBoolean}, "value": {Type: genai.TypeString}}, Required: []string{"ok", "value"}}, MaxOutputTokens: 32})
		if err != nil {
			return result.Usage{}, err
		}
		var structured map[string]any
		if response == nil || json.Unmarshal([]byte(response.Text()), &structured) != nil || !protocolgoogle.ValidateStructuredJSON(response.Text()) {
			return responseUsage(response), errors.New("SDK structured output did not conform to the local schema")
		}
		return responseUsage(response), nil
	case "multimodal":
		image, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
		if err != nil {
			return result.Usage{}, err
		}
		response, err := client.Models.GenerateContent(ctx, model, []*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{{InlineData: &genai.Blob{MIMEType: "image/png", Data: image}}, {Text: "Reply with exactly AIQA_OK."}}}}, &genai.GenerateContentConfig{MaxOutputTokens: 32})
		if err != nil {
			return result.Usage{}, err
		}
		return responseUsage(response), nil
	default:
		return result.Usage{}, fmt.Errorf("unknown Google Gen AI SDK suite %q", suiteID)
	}
}

func responseUsage(response *genai.GenerateContentResponse) result.Usage {
	if response == nil || response.UsageMetadata == nil {
		return result.Usage{}
	}
	metadata := response.UsageMetadata
	return result.Usage{InputTokens: int64(metadata.PromptTokenCount), OutputTokens: int64(metadata.CandidatesTokenCount), ThinkingTokens: int64(metadata.ThoughtsTokenCount), TotalTokens: int64(metadata.TotalTokenCount), CacheReadInputTokens: int64(metadata.CachedContentTokenCount), ProviderFields: map[string]int64{"tool_use_prompt_tokens": int64(metadata.ToolUsePromptTokenCount)}}
}

func modelName(model *genai.Model) string {
	if model == nil {
		return ""
	}
	return model.Name
}

func applyError(event *extsuite.SuiteEvent, err error) {
	event.Outcome = result.ExtensionOutcomeAssertionFailed
	event.Summary = err.Error()
	event.Error = err.Error()
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		event.Outcome = result.ExtensionOutcomeRequestRejected
		event.HTTP.StatusCode = apiErr.Code
		if capabilitySuite(event.SuiteID) {
			event.ErrorKind = "capability_rejection"
		}
		return
	}
	var apiErrPtr *genai.APIError
	if errors.As(err, &apiErrPtr) && apiErrPtr != nil {
		event.Outcome = result.ExtensionOutcomeRequestRejected
		event.HTTP.StatusCode = apiErrPtr.Code
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
	case "models.get", "generate-content", "generate-content-stream", "count-tokens", "embeddings", "tools", "structured-output", "multimodal":
		return true
	default:
		return false
	}
}
