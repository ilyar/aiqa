package suites

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

func DiscoveryTests(p profile.Profile, resp api.Response, models []api.Model, listErr error) []result.TestResult {
	httpTest := testBase(p, "models.list.http", "compat")
	httpTest.DurationMS = resp.Duration.Milliseconds()
	httpTest.HTTP = httpObservation(resp)
	if resp.StatusCode == 0 && listErr != nil {
		httpTest.Status = result.StatusError
		httpTest.Summary = "model discovery failed before an HTTP response"
		httpTest.Error = errorText(listErr)
	} else if !resp.Is2xx() {
		if providerFailureStatusCode(resp.StatusCode) {
			httpTest.Status = result.StatusError
			httpTest.Summary = providerFailureSummary("GET /models", resp.StatusCode)
		} else {
			httpTest.Status = result.StatusFail
			httpTest.Summary = fmt.Sprintf("GET /models returned HTTP %d", resp.StatusCode)
		}
	} else if !strings.Contains(strings.ToLower(resp.ContentType), "json") {
		httpTest.Status = result.StatusFail
		httpTest.Summary = "GET /models returned a non-JSON content type"
	} else {
		httpTest.Status = result.StatusPass
		httpTest.Summary = fmt.Sprintf("GET /models returned HTTP %d JSON", resp.StatusCode)
		if listErr != nil {
			httpTest.Observation = "response arrived, but model-list parsing reported: " + errorText(listErr)
		}
	}

	schemaTest := testBase(p, "models.list.schema", "compat")
	schemaTest.DurationMS = resp.Duration.Milliseconds()
	schemaTest.HTTP = httpObservation(resp)
	if !resp.Is2xx() || resp.JSON == nil {
		schemaTest.Status = result.StatusSkip
		schemaTest.Summary = "schema was not checked because model discovery failed"
	} else {
		var problems []string
		missingCreated := 0
		if object, _ := resp.JSON["object"].(string); object != "list" {
			problems = append(problems, fmt.Sprintf("object=%q, expected list", object))
		}
		data, ok := resp.JSON["data"].([]any)
		if !ok {
			problems = append(problems, "data is not an array")
		} else {
			seen := map[string]bool{}
			for i, raw := range data {
				m, ok := raw.(map[string]any)
				if !ok {
					problems = append(problems, fmt.Sprintf("data[%d] is not an object", i))
					continue
				}
				id, _ := m["id"].(string)
				if id == "" {
					problems = append(problems, fmt.Sprintf("data[%d].id is missing", i))
				} else if seen[id] {
					problems = append(problems, fmt.Sprintf("duplicate model id %q", id))
				}
				seen[id] = true
				if object, _ := m["object"].(string); object != "" && object != "model" {
					problems = append(problems, fmt.Sprintf("data[%d].object=%q", i, object))
				}
				if _, ok := m["created"]; !ok {
					missingCreated++
				}
			}
			if missingCreated > 0 {
				problems = append(problems, fmt.Sprintf("created is missing in %d/%d model entries", missingCreated, len(data)))
				schemaTest.Metrics["missing_created_entries"] = missingCreated
				schemaTest.Metrics["model_entries"] = len(data)
			}
		}
		if len(problems) == 0 {
			schemaTest.Status = result.StatusPass
			schemaTest.Summary = "model list follows the OpenAI list shape"
		} else {
			schemaTest.Status = result.StatusFail
			schemaTest.Summary = "model list schema differs from OpenAI"
			schemaTest.Observation = strings.Join(problems, "; ")
		}
	}
	return []result.TestResult{httpTest, schemaTest}
}

func RunEndpointCompat(ctx context.Context, env Environment, validModel string) []result.TestResult {
	var out []result.TestResult
	out = append(out, probeInvalidModel(ctx, env))
	out = append(out, probeInvalidAuth(ctx, env, validModel))
	out = append(out, probeInvalidJSON(ctx, env))
	out = append(out, probeInvalidTypes(ctx, env, validModel))
	return out
}

func RunModelCompat(ctx context.Context, env Environment, model string) []result.TestResult {
	env.logf("[%s] compat: model metadata", model)
	out := []result.TestResult{probeRetrieveModel(ctx, env, model)}

	env.logf("[%s] compat: basic completion", model)
	basicResp, basicErr := callChat(ctx, env, map[string]any{
		"model": model,
		"messages": []any{
			map[string]any{"role": "user", "content": "Reply with exactly AIQA_OK and nothing else."},
		},
	})
	out = append(out, evaluateBasic(env.Profile, basicResp, basicErr))
	out = append(out, evaluateChatSchema(env.Profile, basicResp, basicErr))
	out = append(out, evaluateUsage(env.Profile, basicResp, basicErr))

	env.logf("[%s] compat: message roles", model)
	out = append(out, probeRole(ctx, env, model, "system"))
	out = append(out, probeRole(ctx, env, model, "developer"))
	out = append(out, probeTextContentParts(ctx, env, model))
	out = append(out, probeMultiTurn(ctx, env, model))

	env.logf("[%s] compat: streaming", model)
	out = append(out, probeStreaming(ctx, env, model, false))
	out = append(out, probeStreaming(ctx, env, model, true))

	env.logf("[%s] compat: tool calling", model)
	out = append(out, probeToolChoice(ctx, env, model, "none"))
	out = append(out, probeToolChoice(ctx, env, model, "auto"))
	out = append(out, probeToolChoice(ctx, env, model, "required"))
	toolResult, assistantMessage := probeForcedTool(ctx, env, model)
	out = append(out, toolResult)
	out = append(out, probeStreamingTool(ctx, env, model))
	out = append(out, probeToolContinuation(ctx, env, model, toolResult, assistantMessage))

	env.logf("[%s] compat: structured output", model)
	out = append(out, probeJSONObject(ctx, env, model))
	out = append(out, probeJSONSchema(ctx, env, model))
	out = append(out, probeLogprobs(ctx, env, model))
	return out
}

func callChat(parent context.Context, env Environment, body map[string]any) (api.Response, error) {
	ctx, cancel := env.probeContext(parent)
	defer cancel()
	return env.Client.Chat(ctx, body)
}

func probeRetrieveModel(parent context.Context, env Environment, model string) result.TestResult {
	t := testBase(env.Profile, "models.retrieve", "compat")
	ctx, cancel := env.probeContext(parent)
	defer cancel()
	resp, err := env.Client.RetrieveModel(ctx, model)
	t.DurationMS = resp.Duration.Milliseconds()
	t.HTTP = httpObservation(resp)
	t.Status = statusForOptionalHTTP(resp, err)
	switch t.Status {
	case result.StatusPass:
		id, _ := resp.JSON["id"].(string)
		if id != model {
			t.Status = result.StatusFail
			t.Summary = "model retrieval returned a different ID"
			t.Observation = fmt.Sprintf("requested=%q returned=%q", model, id)
		} else {
			t.Summary = "model metadata can be retrieved by ID"
		}
	case result.StatusUnsupported:
		t.Summary = "GET /models/{model} is not supported"
	case result.StatusError:
		t.Summary = "model retrieval could not be evaluated"
		t.Error = errorText(err)
	default:
		t.Summary = "model retrieval is not OpenAI-compatible"
		t.Error = errorText(err)
	}
	return t
}

func evaluateBasic(p profile.Profile, resp api.Response, err error) result.TestResult {
	t := testBase(p, "chat.basic", "compat")
	t.DurationMS = resp.Duration.Milliseconds()
	t.HTTP = httpObservation(resp)
	t.Usage = api.ParseUsage(resp.JSON)
	if err != nil && resp.StatusCode == 0 {
		t.Status = result.StatusError
		t.Summary = "chat request failed before an HTTP response"
		t.Error = errorText(err)
		return t
	}
	if !resp.Is2xx() {
		if providerFailureStatusCode(resp.StatusCode) {
			t.Status = result.StatusError
			t.Summary = providerFailureSummary("minimal chat completion", resp.StatusCode)
		} else {
			t.Status = result.StatusFail
			t.Summary = fmt.Sprintf("minimal chat completion returned HTTP %d", resp.StatusCode)
		}
		t.Error = errorText(err)
		return t
	}
	if resp.JSON == nil {
		t.Status = result.StatusFail
		t.Summary = "minimal chat completion did not return a JSON object"
		t.Error = errorText(err)
		return t
	}
	content := strings.TrimSpace(api.FirstContent(resp.JSON))
	if content == "" {
		t.Status = result.StatusFail
		t.Summary = "chat completion succeeded but had no assistant content"
		return t
	}
	t.Status = result.StatusPass
	t.Summary = "minimal chat completion succeeded"
	t.Observation = "assistant content: " + truncate(content, 160)
	return t
}

func evaluateChatSchema(p profile.Profile, resp api.Response, err error) result.TestResult {
	t := testBase(p, "chat.schema", "compat")
	t.DurationMS = resp.Duration.Milliseconds()
	t.HTTP = httpObservation(resp)
	if err != nil || !resp.Is2xx() || resp.JSON == nil {
		t.Status = result.StatusSkip
		t.Summary = "response schema was not checked because basic chat failed"
		return t
	}
	problems := api.ValidateChatShape(resp.JSON)
	if len(problems) > 0 {
		t.Status = result.StatusFail
		t.Summary = "chat completion response has schema mismatches"
		t.Observation = strings.Join(problems, "; ")
		return t
	}
	t.Status = result.StatusPass
	t.Summary = "chat completion response has the expected OpenAI shape"
	return t
}

func evaluateUsage(p profile.Profile, resp api.Response, err error) result.TestResult {
	t := testBase(p, "chat.usage", "compat")
	t.DurationMS = resp.Duration.Milliseconds()
	t.HTTP = httpObservation(resp)
	if err != nil || !resp.Is2xx() || resp.JSON == nil {
		t.Status = result.StatusSkip
		t.Summary = "usage was not checked because basic chat failed"
		return t
	}
	u := api.ParseUsage(resp.JSON)
	t.Usage = u
	problems := api.ValidateUsage(u)
	if len(problems) > 0 {
		t.Status = result.StatusFail
		t.Summary = "non-stream token usage is absent or inconsistent"
		t.Observation = strings.Join(problems, "; ")
		return t
	}
	t.Status = result.StatusPass
	t.Summary = fmt.Sprintf("usage reported %d prompt + %d completion tokens", u.PromptTokens, u.CompletionTokens)
	return t
}

func probeRole(parent context.Context, env Environment, model, role string) result.TestResult {
	id := "chat.role." + role
	t := testBase(env.Profile, id, "compat")
	marker := strings.ToUpper(role) + "_OK"
	resp, err := callChat(parent, env, map[string]any{
		"model": model,
		"messages": []any{
			map[string]any{"role": role, "content": "Reply with exactly " + marker + "."},
			map[string]any{"role": "user", "content": "Follow the instruction."},
		},
	})
	t.DurationMS = resp.Duration.Milliseconds()
	t.HTTP = httpObservation(resp)
	t.Usage = api.ParseUsage(resp.JSON)
	t.Status = statusForOptionalHTTP(resp, err)
	if t.Status == result.StatusPass {
		problems := api.ValidateChatShape(resp.JSON)
		if len(problems) > 0 {
			t.Status = result.StatusFail
			t.Summary = role + " role returned malformed chat output"
			t.Observation = strings.Join(problems, "; ")
			return t
		}
		content := api.FirstContent(resp.JSON)
		if strings.Contains(strings.ToUpper(content), marker) {
			t.Summary = role + " role is accepted and affects the response"
		} else {
			t.Status = result.StatusWarn
			t.Summary = role + " role is accepted, but its semantic effect was not demonstrated"
			t.Observation = "assistant content: " + truncate(content, 160)
		}
	} else if t.Status == result.StatusUnsupported {
		t.Summary = role + " role is rejected or unsupported"
	} else {
		t.Summary = role + " role could not be evaluated"
		t.Error = errorText(err)
	}
	return t
}

func probeTextContentParts(parent context.Context, env Environment, model string) result.TestResult {
	t := testBase(env.Profile, "chat.content_parts.text", "compat")
	resp, err := callChat(parent, env, map[string]any{
		"model": model,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "Reply with exactly PARTS_OK."},
				},
			},
		},
	})
	t.DurationMS = resp.Duration.Milliseconds()
	t.HTTP = httpObservation(resp)
	t.Usage = api.ParseUsage(resp.JSON)
	t.Status = statusForOptionalHTTP(resp, err)
	if t.Status == result.StatusPass {
		content := strings.TrimSpace(api.FirstContent(resp.JSON))
		if strings.Contains(strings.ToUpper(content), "PARTS_OK") {
			t.Summary = "array-form text content parts are accepted and interpreted"
		} else {
			t.Status = result.StatusWarn
			t.Summary = "array-form text content was accepted, but semantic use was not demonstrated"
			t.Observation = "assistant content: " + truncate(content, 160)
		}
	} else if t.Status == result.StatusUnsupported {
		t.Summary = "array-form text content parts are unsupported"
	} else {
		t.Summary = "text content-parts support could not be evaluated"
		t.Error = errorText(err)
	}
	return t
}

func probeMultiTurn(parent context.Context, env Environment, model string) result.TestResult {
	t := testBase(env.Profile, "chat.multi_turn", "compat")
	resp, err := callChat(parent, env, map[string]any{
		"model": model,
		"messages": []any{
			map[string]any{"role": "user", "content": "The code word is ALPHA-731."},
			map[string]any{"role": "assistant", "content": "Understood: ALPHA-731."},
			map[string]any{"role": "user", "content": "Reply with only the code word."},
		},
	})
	t.DurationMS = resp.Duration.Milliseconds()
	t.HTTP = httpObservation(resp)
	t.Usage = api.ParseUsage(resp.JSON)
	t.Status = statusForOptionalHTTP(resp, err)
	if t.Status == result.StatusPass {
		content := api.FirstContent(resp.JSON)
		if strings.Contains(strings.ToUpper(content), "ALPHA-731") {
			t.Summary = "assistant history is accepted and used"
		} else {
			t.Status = result.StatusWarn
			t.Summary = "assistant history is accepted, but recall was not demonstrated"
			t.Observation = "assistant content: " + truncate(content, 160)
		}
	} else if t.Status == result.StatusUnsupported {
		t.Summary = "multi-turn assistant history is rejected"
	} else {
		t.Summary = "multi-turn messages could not be evaluated"
		t.Error = errorText(err)
	}
	return t
}

func probeStreaming(parent context.Context, env Environment, model string, includeUsage bool) result.TestResult {
	id := "chat.stream.sse"
	if includeUsage {
		id = "chat.stream.usage"
	}
	t := testBase(env.Profile, id, "compat")
	body := map[string]any{
		"model":    model,
		"messages": []any{map[string]any{"role": "user", "content": "Reply with exactly STREAM_OK."}},
	}
	if includeUsage {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	ctx, cancel := env.probeContext(parent)
	defer cancel()
	resp, err := env.Client.ChatStream(ctx, body)
	t.DurationMS = resp.Duration.Milliseconds()
	t.HTTP = streamObservation(resp)
	t.Usage = resp.Usage
	if err != nil && resp.StatusCode == 0 {
		t.Status = result.StatusError
		t.Summary = "stream failed before an HTTP response"
		t.Error = errorText(err)
		return t
	}
	if !resp.Is2xx() {
		if providerFailureStatusCode(resp.StatusCode) {
			t.Status = result.StatusError
			t.Summary = providerFailureSummary("stream", resp.StatusCode)
		} else if includeUsage && (resp.StatusCode == 400 || resp.StatusCode == 422) {
			t.Status = result.StatusUnsupported
			t.Summary = "stream_options.include_usage is unsupported"
		} else {
			t.Status = result.StatusFail
			t.Summary = fmt.Sprintf("stream returned HTTP %d", resp.StatusCode)
		}
		t.Error = errorText(err)
		return t
	}
	chunkProblems := validateStreamChunkShapes(resp.Events)
	if includeUsage {
		if len(resp.InvalidEvents) > 0 || len(chunkProblems) > 0 {
			t.Status = result.StatusFail
			t.Summary = "stream usage request returned malformed SSE chunks"
			t.Observation = strings.Join(append(resp.InvalidEvents, chunkProblems...), "; ")
			return t
		}
		if resp.Usage.TotalTokens <= 0 {
			t.Status = result.StatusFail
			t.Summary = "stream succeeded but did not include usage"
			return t
		}
		t.Status = result.StatusPass
		t.Summary = fmt.Sprintf("stream reported %d total tokens", resp.Usage.TotalTokens)
		return t
	}
	var problems []string
	problems = append(problems, chunkProblems...)
	if !strings.Contains(strings.ToLower(resp.ContentType), "text/event-stream") {
		problems = append(problems, "Content-Type is not text/event-stream")
	}
	if len(resp.Events) == 0 {
		problems = append(problems, "no SSE data events were received")
	}
	if len(resp.InvalidEvents) > 0 {
		problems = append(problems, fmt.Sprintf("%d invalid SSE event(s)", len(resp.InvalidEvents)))
	}
	if !resp.Done {
		problems = append(problems, "[DONE] terminator is missing")
	}
	if strings.TrimSpace(resp.Content) == "" {
		problems = append(problems, "stream reconstructed no assistant content")
	}
	if len(problems) > 0 {
		t.Status = result.StatusFail
		t.Summary = "streaming protocol differs from OpenAI SSE"
		t.Observation = strings.Join(problems, "; ")
		return t
	}
	t.Status = result.StatusPass
	t.Summary = fmt.Sprintf("valid SSE stream with %d event(s) and [DONE]", len(resp.Events))
	t.Metrics["events"] = len(resp.Events)
	t.Metrics["finish_reasons"] = resp.FinishReasons
	return t
}

func validateStreamChunkShapes(events []api.SSEEvent) []string {
	var problems []string
	for i, event := range events {
		if event.Data == "" || event.Data == "[DONE]" || event.JSON == nil {
			continue
		}
		root := event.JSON
		if object, _ := root["object"].(string); object != "chat.completion.chunk" {
			problems = append(problems, fmt.Sprintf("event %d object=%q", i, object))
		}
		if id, _ := root["id"].(string); id == "" {
			problems = append(problems, fmt.Sprintf("event %d is missing id", i))
		}
		if model, _ := root["model"].(string); model == "" {
			problems = append(problems, fmt.Sprintf("event %d is missing model", i))
		}
		if _, ok := root["created"]; !ok {
			problems = append(problems, fmt.Sprintf("event %d is missing created", i))
		}
		choices, ok := root["choices"].([]any)
		if !ok {
			problems = append(problems, fmt.Sprintf("event %d choices is not an array", i))
			continue
		}
		for j, raw := range choices {
			choice, ok := raw.(map[string]any)
			if !ok {
				problems = append(problems, fmt.Sprintf("event %d choice %d is not an object", i, j))
				continue
			}
			if _, ok := choice["index"]; !ok {
				problems = append(problems, fmt.Sprintf("event %d choice %d is missing index", i, j))
			}
			if _, ok := choice["delta"].(map[string]any); !ok {
				problems = append(problems, fmt.Sprintf("event %d choice %d delta is not an object", i, j))
			}
			if _, ok := choice["finish_reason"]; !ok {
				problems = append(problems, fmt.Sprintf("event %d choice %d is missing finish_reason", i, j))
			}
		}
	}
	if len(problems) > 12 {
		problems = append(problems[:12], fmt.Sprintf("and %d more chunk-shape problem(s)", len(problems)-12))
	}
	return problems
}

func toolDefinition() map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        "get_status",
			"description": "Get the status of an opaque test resource.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"resource_id": map[string]any{"type": "string"},
				},
				"required":             []any{"resource_id"},
				"additionalProperties": false,
			},
		},
	}
}

func probeToolChoice(parent context.Context, env Environment, model, mode string) result.TestResult {
	id := "chat.tools." + mode
	t := testBase(env.Profile, id, "compat")
	prompt := "Use get_status for resource-alpha."
	if mode == "none" {
		prompt = "Do not call any tool. Reply exactly NONE_OK."
	}
	resp, err := callChat(parent, env, map[string]any{
		"model":       model,
		"messages":    []any{map[string]any{"role": "user", "content": prompt}},
		"tools":       []any{toolDefinition()},
		"tool_choice": mode,
	})
	t.DurationMS = resp.Duration.Milliseconds()
	t.HTTP = httpObservation(resp)
	t.Usage = api.ParseUsage(resp.JSON)
	t.Status = statusForOptionalHTTP(resp, err)
	if t.Status != result.StatusPass {
		if t.Status == result.StatusUnsupported {
			t.Summary = "tool_choice=" + mode + " is unsupported"
		} else {
			t.Summary = "tool_choice=" + mode + " could not be evaluated"
			t.Error = errorText(err)
		}
		return t
	}
	calls := api.FirstToolCalls(resp.JSON)
	content := strings.TrimSpace(api.FirstContent(resp.JSON))
	switch mode {
	case "none":
		if len(calls) > 0 {
			t.Status = result.StatusFail
			t.Summary = "tool_choice=none was accepted but a tool call was emitted"
			return t
		}
		if content == "" {
			t.Status = result.StatusFail
			t.Summary = "tool_choice=none returned neither content nor a tool call"
			return t
		}
		t.Status = result.StatusPass
		t.Summary = "tool_choice=none suppressed tool calls"
	case "required":
		if len(calls) == 0 {
			t.Status = result.StatusFail
			t.Summary = "tool_choice=required was accepted but no tool call was emitted"
			return t
		}
		t.Status = result.StatusPass
		t.Summary = "tool_choice=required emitted a tool call"
		t.Metrics["tool_calls"] = len(calls)
	case "auto":
		if len(calls) == 0 {
			t.Status = result.StatusWarn
			t.Summary = "tool_choice=auto was accepted, but automatic selection was not demonstrated"
			t.Observation = "assistant content: " + truncate(content, 160)
			return t
		}
		t.Status = result.StatusPass
		t.Summary = "tool_choice=auto emitted a tool call when requested"
		t.Metrics["tool_calls"] = len(calls)
	}
	return t
}

func probeForcedTool(parent context.Context, env Environment, model string) (result.TestResult, map[string]any) {
	t := testBase(env.Profile, "chat.tools.forced", "compat")
	resp, err := callChat(parent, env, map[string]any{
		"model":    model,
		"messages": []any{map[string]any{"role": "user", "content": "Use get_status for resource-alpha."}},
		"tools":    []any{toolDefinition()},
		"tool_choice": map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "get_status"},
		},
	})
	t.DurationMS = resp.Duration.Milliseconds()
	t.HTTP = httpObservation(resp)
	t.Usage = api.ParseUsage(resp.JSON)
	t.Status = statusForOptionalHTTP(resp, err)
	if t.Status != result.StatusPass {
		if t.Status == result.StatusUnsupported {
			t.Summary = "forced function tool_choice is unsupported"
		} else {
			t.Summary = "forced tool call could not be evaluated"
			t.Error = errorText(err)
		}
		return t, nil
	}
	calls := api.FirstToolCalls(resp.JSON)
	if len(calls) == 0 {
		t.Status = result.StatusFail
		t.Summary = "request succeeded but no tool_calls were returned"
		return t, nil
	}
	call := calls[0]
	callID, _ := call["id"].(string)
	fn, _ := call["function"].(map[string]any)
	name, _ := fn["name"].(string)
	args, _ := fn["arguments"].(string)
	var decoded map[string]any
	argErr := json.Unmarshal([]byte(args), &decoded)
	resourceID, _ := decoded["resource_id"].(string)
	var problems []string
	if callID == "" {
		problems = append(problems, "tool call id is missing")
	}
	if name != "get_status" {
		problems = append(problems, fmt.Sprintf("function name=%q", name))
	}
	if argErr != nil {
		problems = append(problems, "function arguments are not valid JSON")
	}
	if resourceID == "" {
		problems = append(problems, "resource_id argument is missing")
	}
	if len(problems) > 0 {
		t.Status = result.StatusFail
		t.Summary = "tool call has a non-OpenAI or invalid shape"
		t.Observation = strings.Join(problems, "; ")
		return t, nil
	}
	t.Status = result.StatusPass
	t.Summary = "forced function call returned valid JSON arguments"
	t.Metrics["tool_call_id"] = callID
	t.Metrics["function"] = name
	t.Metrics["arguments"] = decoded
	message, _ := api.FirstMessage(resp.JSON)
	return t, message
}

func probeStreamingTool(parent context.Context, env Environment, model string) result.TestResult {
	t := testBase(env.Profile, "chat.tools.stream", "compat")
	body := map[string]any{
		"model":    model,
		"messages": []any{map[string]any{"role": "user", "content": "Use get_status for resource-alpha."}},
		"tools":    []any{toolDefinition()},
		"tool_choice": map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "get_status"},
		},
	}
	ctx, cancel := env.probeContext(parent)
	defer cancel()
	resp, err := env.Client.ChatStream(ctx, body)
	t.DurationMS = resp.Duration.Milliseconds()
	t.HTTP = streamObservation(resp)
	t.Usage = resp.Usage
	if err != nil && resp.StatusCode == 0 {
		t.Status = result.StatusError
		t.Summary = "streaming tool call failed before an HTTP response"
		t.Error = errorText(err)
		return t
	}
	if !resp.Is2xx() {
		if providerFailureStatusCode(resp.StatusCode) {
			t.Status = result.StatusError
			t.Summary = providerFailureSummary("streaming tool call", resp.StatusCode)
		} else if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			t.Status = result.StatusUnsupported
			t.Summary = "streaming tool calls are unsupported"
		} else {
			t.Status = result.StatusError
			t.Summary = fmt.Sprintf("streaming tool call returned HTTP %d", resp.StatusCode)
		}
		t.Error = errorText(err)
		return t
	}
	if len(resp.InvalidEvents) > 0 {
		t.Status = result.StatusFail
		t.Summary = "streaming tool call contained invalid SSE events"
		t.Observation = strings.Join(resp.InvalidEvents, "; ")
		return t
	}
	if !resp.Done {
		t.Status = result.StatusFail
		t.Summary = "streaming tool call is missing [DONE]"
		return t
	}
	if len(resp.ToolCalls) == 0 {
		t.Status = result.StatusFail
		t.Summary = "tool-call request streamed successfully but no tool_calls were reconstructed"
		return t
	}
	call := resp.ToolCalls[0]
	callID, _ := call["id"].(string)
	function, _ := call["function"].(map[string]any)
	name, _ := function["name"].(string)
	args, _ := function["arguments"].(string)
	var decoded map[string]any
	argErr := json.Unmarshal([]byte(args), &decoded)
	if callID == "" || name != "get_status" || argErr != nil {
		t.Status = result.StatusFail
		t.Summary = "streamed tool-call deltas did not reconstruct into a valid OpenAI tool call"
		t.Observation = fmt.Sprintf("id=%q name=%q arguments=%q", callID, name, truncate(args, 240))
		return t
	}
	t.Status = result.StatusPass
	t.Summary = "streamed tool-call deltas reconstructed into valid JSON arguments"
	t.Metrics["tool_call_id"] = callID
	t.Metrics["function"] = name
	t.Metrics["arguments"] = decoded
	return t
}

func probeToolContinuation(parent context.Context, env Environment, model string, prerequisite result.TestResult, assistantMessage map[string]any) result.TestResult {
	t := testBase(env.Profile, "chat.tools.continuation", "compat")
	if prerequisite.Status != result.StatusPass || assistantMessage == nil {
		t.Status = result.StatusSkip
		t.Summary = "skipped because forced tool calling did not pass"
		return t
	}
	calls, _ := assistantMessage["tool_calls"].([]any)
	if len(calls) == 0 {
		t.Status = result.StatusSkip
		t.Summary = "skipped because no tool call was available"
		return t
	}
	call, _ := calls[0].(map[string]any)
	callID, _ := call["id"].(string)
	assistantForRequest := map[string]any{
		"role":       "assistant",
		"content":    assistantMessage["content"],
		"tool_calls": assistantMessage["tool_calls"],
	}
	resp, err := callChat(parent, env, map[string]any{
		"model": model,
		"messages": []any{
			map[string]any{"role": "user", "content": "Use get_status for resource-alpha."},
			assistantForRequest,
			map[string]any{"role": "tool", "tool_call_id": callID, "content": `{"resource_id":"resource-alpha","status":"available"}`},
		},
		"tools": []any{toolDefinition()},
	})
	t.DurationMS = resp.Duration.Milliseconds()
	t.HTTP = httpObservation(resp)
	t.Usage = api.ParseUsage(resp.JSON)
	t.Status = statusForOptionalHTTP(resp, err)
	if t.Status == result.StatusPass {
		content := strings.TrimSpace(api.FirstContent(resp.JSON))
		if content == "" {
			t.Status = result.StatusFail
			t.Summary = "tool continuation returned no final assistant content"
		} else {
			t.Summary = "assistant tool_call and tool result can be continued"
			t.Observation = "assistant content: " + truncate(content, 180)
		}
	} else if t.Status == result.StatusUnsupported {
		t.Summary = "tool-result continuation is unsupported"
	} else {
		t.Summary = "tool-result continuation could not be evaluated"
		t.Error = errorText(err)
	}
	return t
}

func probeJSONObject(parent context.Context, env Environment, model string) result.TestResult {
	t := testBase(env.Profile, "chat.structured.json_object", "compat")
	attempts, valid := 3, 0
	var invalid string
	for i := 0; i < attempts; i++ {
		resp, err := callChat(parent, env, map[string]any{
			"model": model, "messages": []any{map[string]any{"role": "user", "content": `Return only the raw JSON object {"ok":true,"value":"AIQA_OK"}. Do not use Markdown, code fences, or any other text.`}},
			"response_format": map[string]any{"type": "json_object"},
		})
		t.DurationMS += resp.Duration.Milliseconds()
		t.Usage = t.Usage.Add(api.ParseUsage(resp.JSON))
		t.HTTP = httpObservation(resp)
		status := statusForOptionalHTTP(resp, err)
		if status == result.StatusUnsupported {
			t.Status = status
			t.Summary = "response_format=json_object is unsupported"
			return t
		}
		if status != result.StatusPass {
			t.Status = status
			t.Summary = "JSON object mode could not be evaluated"
			t.Error = errorText(err)
			return t
		}
		content := api.FirstContent(resp.JSON)
		var object map[string]any
		if json.Unmarshal([]byte(content), &object) == nil {
			valid++
		} else if invalid == "" {
			invalid = truncate(content, 300)
		}
	}
	t.Metrics["attempts"] = attempts
	t.Metrics["valid_json_responses"] = valid
	if valid*2 > attempts {
		t.Status = result.StatusPass
		t.Summary = "JSON object mode returned raw JSON in the majority of attempts"
	} else if valid > 0 {
		t.Status = result.StatusWarn
		t.Summary = "JSON object mode returned raw JSON inconsistently"
	} else {
		t.Status = result.StatusFail
		t.Summary = "JSON object mode never returned raw JSON"
		t.Observation = invalid
	}
	return t
}

func probeJSONSchema(parent context.Context, env Environment, model string) result.TestResult {
	t := testBase(env.Profile, "chat.structured.json_schema", "compat")
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ok":    map[string]any{"type": "boolean", "const": true},
			"value": map[string]any{"type": "string", "const": "AIQA_OK"},
		},
		"required":             []any{"ok", "value"},
		"additionalProperties": false,
	}
	resp, err := callChat(parent, env, map[string]any{
		"model":    model,
		"messages": []any{map[string]any{"role": "user", "content": "Return the requested structured result."}},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "aiqa_result",
				"strict": true,
				"schema": schema,
			},
		},
	})
	t.DurationMS = resp.Duration.Milliseconds()
	t.HTTP = httpObservation(resp)
	t.Usage = api.ParseUsage(resp.JSON)
	t.Status = statusForOptionalHTTP(resp, err)
	if t.Status == result.StatusPass {
		content := api.FirstContent(resp.JSON)
		var object map[string]any
		if err := json.Unmarshal([]byte(content), &object); err != nil {
			t.Status = result.StatusFail
			t.Summary = "strict JSON schema returned invalid JSON"
			t.Observation = truncate(content, 300)
			return t
		}
		ok, okType := object["ok"].(bool)
		value, valueType := object["value"].(string)
		if !okType || !ok || !valueType || value != "AIQA_OK" || len(object) != 2 {
			t.Status = result.StatusFail
			t.Summary = "output does not conform to the requested strict schema"
			b, _ := json.Marshal(object)
			t.Observation = string(b)
		} else {
			t.Summary = "strict JSON schema output conforms to the requested schema"
		}
	} else if t.Status == result.StatusUnsupported {
		t.Summary = "response_format=json_schema is unsupported"
	} else {
		t.Summary = "strict JSON schema could not be evaluated"
		t.Error = errorText(err)
	}
	return t
}

func probeLogprobs(parent context.Context, env Environment, model string) result.TestResult {
	t := testBase(env.Profile, "chat.logprobs", "compat")
	resp, err := callChat(parent, env, map[string]any{
		"model":        model,
		"messages":     []any{map[string]any{"role": "user", "content": "Reply with only A."}},
		"logprobs":     true,
		"top_logprobs": 2,
	})
	t.DurationMS = resp.Duration.Milliseconds()
	t.HTTP = httpObservation(resp)
	t.Usage = api.ParseUsage(resp.JSON)
	t.Status = statusForOptionalHTTP(resp, err)
	if t.Status == result.StatusPass {
		choice, _ := api.FirstChoice(resp.JSON)
		logprobs, exists := choice["logprobs"]
		if !exists || logprobs == nil {
			t.Status = result.StatusFail
			t.Summary = "logprobs request was accepted but no logprobs were returned"
		} else {
			t.Summary = "logprobs and top_logprobs are returned"
		}
	} else if t.Status == result.StatusUnsupported {
		t.Summary = "logprobs are unsupported"
	} else {
		t.Summary = "logprobs could not be evaluated"
		t.Error = errorText(err)
	}
	return t
}

func probeInvalidModel(parent context.Context, env Environment) result.TestResult {
	t := testBase(env.Profile, "chat.error.invalid_model", "compat")
	fake := fmt.Sprintf("__aiqa_missing_model_%d__", time.Now().UnixNano())
	resp, err := callChat(parent, env, map[string]any{
		"model":    fake,
		"messages": []any{map[string]any{"role": "user", "content": "test"}},
	})
	t.DurationMS = resp.Duration.Milliseconds()
	t.HTTP = httpObservation(resp)
	return evaluateErrorProbe(t, resp, err, []int{400, 404, 422}, "invalid model")
}

func probeInvalidAuth(parent context.Context, env Environment, model string) result.TestResult {
	t := testBase(env.Profile, "chat.error.invalid_auth", "compat")
	if model == "" {
		t.Status = result.StatusSkip
		t.Summary = "skipped because no model ID is available"
		return t
	}
	client := env.Client.CloneWithKey("aiqa-intentionally-invalid-key")
	ctx, cancel := env.probeContext(parent)
	defer cancel()
	resp, err := client.Chat(ctx, map[string]any{
		"model":    model,
		"messages": []any{map[string]any{"role": "user", "content": "test"}},
	})
	t.DurationMS = resp.Duration.Milliseconds()
	t.HTTP = httpObservation(resp)
	return evaluateErrorProbe(t, resp, err, []int{401, 403}, "invalid authentication")
}

func probeInvalidJSON(parent context.Context, env Environment) result.TestResult {
	t := testBase(env.Profile, "chat.error.invalid_json", "compat")
	variants := []struct{ name, body string }{
		{"truncated_value", `{"model":`},
		{"trailing_comma", `{"model":"test",}`},
		{"unterminated_string", `{"model":"test}`},
	}
	statuses := make(map[string]any, len(variants))
	passed := 0
	inconclusive := false
	for _, variant := range variants {
		ctx, cancel := env.probeContext(parent)
		resp, err := env.Client.ChatRaw(ctx, []byte(variant.body), "application/json")
		cancel()
		base := testBase(env.Profile, "chat.error.invalid_json", "compat")
		base.DurationMS = resp.Duration.Milliseconds()
		base.HTTP = httpObservation(resp)
		attempt := evaluateErrorProbe(base, resp, err, []int{400, 422}, "malformed JSON")
		t.DurationMS += attempt.DurationMS
		statuses[variant.name] = map[string]any{"status": attempt.Status, "http": resp.StatusCode, "summary": attempt.Summary}
		if attempt.Status == result.StatusPass {
			passed++
		}
		if attempt.Status == result.StatusError {
			inconclusive = true
		}
		if t.HTTP.StatusCode == 0 {
			t.HTTP = attempt.HTTP
		}
	}
	t.Metrics["variants"] = statuses
	t.Metrics["passed_variants"] = passed
	t.Metrics["total_variants"] = len(variants)
	if passed == len(variants) {
		t.Status = result.StatusPass
		t.Summary = "all malformed JSON variants returned client errors with OpenAI-shaped bodies"
	} else if inconclusive {
		t.Status = result.StatusError
		t.Summary = "malformed JSON variants could not all be evaluated because the endpoint remained rate limited"
	} else {
		t.Status = result.StatusFail
		t.Summary = fmt.Sprintf("%d/%d malformed JSON variants returned the expected client error", passed, len(variants))
	}
	return t
}

func probeInvalidTypes(parent context.Context, env Environment, model string) result.TestResult {
	t := testBase(env.Profile, "chat.error.invalid_types", "compat")
	if model == "" {
		t.Status = result.StatusSkip
		t.Summary = "skipped because no model ID is available"
		return t
	}
	resp, err := callChat(parent, env, map[string]any{
		"model":       model,
		"messages":    "this must be an array",
		"temperature": "hot",
	})
	t.DurationMS = resp.Duration.Milliseconds()
	t.HTTP = httpObservation(resp)
	return evaluateErrorProbe(t, resp, err, []int{400, 422}, "invalid request field types")
}

func evaluateErrorProbe(t result.TestResult, resp api.Response, err error, expectedStatus []int, label string) result.TestResult {
	if err != nil && resp.StatusCode == 0 {
		t.Status = result.StatusError
		t.Summary = label + " probe failed before an HTTP response"
		t.Error = errorText(err)
		return t
	}
	if resp.Is2xx() {
		t.Status = result.StatusFail
		t.Summary = label + " was incorrectly accepted"
		return t
	}
	if providerFailureStatusCode(resp.StatusCode) {
		t.Status = result.StatusError
		if resp.StatusCode == http.StatusTooManyRequests {
			t.Summary = label + " remained rate limited after adaptive retries; compatibility outcome is inconclusive"
		} else {
			t.Summary = fmt.Sprintf("%s returned provider HTTP %d; compatibility outcome is inconclusive", label, resp.StatusCode)
		}
		t.Error = errorText(err)
		return t
	}
	statusOK := false
	for _, code := range expectedStatus {
		if resp.StatusCode == code {
			statusOK = true
			break
		}
	}
	message, typ, code, shapeOK := api.ErrorShape(resp.JSON)
	if !statusOK {
		t.Status = result.StatusFail
		t.Summary = fmt.Sprintf("%s returned unexpected HTTP %d", label, resp.StatusCode)
	} else if !shapeOK {
		t.Status = result.StatusFail
		t.Summary = label + " returned a non-OpenAI error shape"
	} else {
		t.Status = result.StatusPass
		t.Summary = fmt.Sprintf("%s returned HTTP %d with an OpenAI-shaped error", label, resp.StatusCode)
		t.Metrics["error_type"] = typ
		t.Metrics["error_code"] = code
		t.Observation = truncate(message, 300)
	}
	if err != nil {
		t.Error = errorText(err)
	}
	return t
}

func SortTests(in []result.TestResult) {
	sort.SliceStable(in, func(i, j int) bool {
		return in[i].ID < in[j].ID
	})
}

var _ = http.MethodGet
