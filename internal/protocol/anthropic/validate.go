package anthropic

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/result"
)

// ModelSchemaDiagnostic preserves a machine-readable model-list schema fault
// for guidance. ValidateModels remains the compatibility API for callers that
// need the existing human-readable validator output.
type ModelSchemaDiagnostic struct {
	Path     string
	ModelID  string
	Issue    string
	Expected string
	message  string
}

func ModelSchemaDiagnostics(root map[string]any) []ModelSchemaDiagnostic {
	return modelSchemaDiagnostics(root)
}

func ValidateModels(root map[string]any) []string {
	diagnostics := modelSchemaDiagnostics(root)
	problems := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		problems = append(problems, diagnostic.message)
	}
	return problems
}

func modelSchemaDiagnostics(root map[string]any) []ModelSchemaDiagnostic {
	var diagnostics []ModelSchemaDiagnostic
	add := func(path, modelID, issue, expected, message string) {
		diagnostics = append(diagnostics, ModelSchemaDiagnostic{Path: path, ModelID: modelID, Issue: issue, Expected: expected, message: message})
	}
	data, ok := root["data"].([]any)
	if !ok {
		return []ModelSchemaDiagnostic{{Path: "data", Issue: "invalid_type", Expected: "array", message: "data is missing or not an array"}}
	}
	seen := map[string]bool{}
	for i, raw := range data {
		path := fmt.Sprintf("data[%d]", i)
		model, ok := raw.(map[string]any)
		if !ok {
			add(path, "", "invalid_type", "object", path+" is not an object")
			continue
		}
		id := stringValue(model["id"])
		if id == "" {
			add(path+".id", "", "missing", "string", path+".id is missing")
		} else if seen[id] {
			add(path+".id", id, "duplicate", "unique model id", path+".id is duplicated")
		}
		seen[id] = true
		if stringValue(model["type"]) != "model" {
			add(path+".type", id, "invalid_value", "model", path+".type is not model")
		}
		if stringValue(model["display_name"]) == "" {
			add(path+".display_name", id, "missing", "string", path+".display_name is missing")
		}
		created := stringValue(model["created_at"])
		if _, err := time.Parse(time.RFC3339, created); err != nil {
			add(path+".created_at", id, "invalid_value", "RFC3339 timestamp", path+".created_at is not RFC3339")
		}
		if value, valid := positiveInteger(model["max_input_tokens"]); !valid || value <= 0 {
			add(path+".max_input_tokens", id, "invalid_value", "positive integer", path+".max_input_tokens is missing or not positive")
		}
		if value, valid := positiveInteger(model["max_tokens"]); !valid || value <= 0 {
			add(path+".max_tokens", id, "invalid_value", "positive integer", path+".max_tokens is missing or not positive")
		}
		capabilities, ok := model["capabilities"].(map[string]any)
		if !ok {
			add(path+".capabilities", id, "invalid_type", "object", path+".capabilities is missing or not an object")
		} else {
			diagnostics = append(diagnostics, validateModelCapabilities(path+".capabilities", id, capabilities)...)
		}
	}
	if value, exists := root["has_more"]; !exists {
		add("has_more", "", "missing", "boolean", "has_more is missing")
	} else if _, ok := value.(bool); !ok {
		add("has_more", "", "invalid_type", "boolean", "has_more is not boolean")
	}
	for _, key := range []string{"first_id", "last_id"} {
		if value, exists := root[key]; !exists {
			add(key, "", "missing", "string or null", key+" is missing")
		} else if value != nil {
			if _, ok := value.(string); !ok {
				add(key, "", "invalid_type", "string or null", key+" is not string or null")
			}
		}
	}
	return diagnostics
}

func validateModelCapabilities(path, modelID string, capabilities map[string]any) []ModelSchemaDiagnostic {
	var diagnostics []ModelSchemaDiagnostic
	for _, key := range []string{"batch", "citations", "code_execution", "image_input", "pdf_input", "structured_outputs"} {
		diagnostics = append(diagnostics, validateCapabilitySupport(path+"."+key, modelID, capabilities, key)...)
	}

	contextManagement, ok := capabilities["context_management"].(map[string]any)
	if !ok {
		diagnostics = append(diagnostics, ModelSchemaDiagnostic{Path: path + ".context_management", ModelID: modelID, Issue: "invalid_type", Expected: "object", message: path + ".context_management is missing or not an object"})
	} else {
		for _, key := range []string{"clear_thinking_20251015", "clear_tool_uses_20250919", "compact_20260112"} {
			diagnostics = append(diagnostics, validateCapabilitySupport(path+".context_management."+key, modelID, contextManagement, key)...)
		}
		diagnostics = append(diagnostics, validateRequiredBool(path+".context_management.supported", modelID, contextManagement, "supported")...)
	}

	effort, ok := capabilities["effort"].(map[string]any)
	if !ok {
		diagnostics = append(diagnostics, ModelSchemaDiagnostic{Path: path + ".effort", ModelID: modelID, Issue: "invalid_type", Expected: "object", message: path + ".effort is missing or not an object"})
	} else {
		for _, key := range []string{"high", "low", "max", "medium", "xhigh"} {
			diagnostics = append(diagnostics, validateCapabilitySupport(path+".effort."+key, modelID, effort, key)...)
		}
		diagnostics = append(diagnostics, validateRequiredBool(path+".effort.supported", modelID, effort, "supported")...)
	}

	thinking, ok := capabilities["thinking"].(map[string]any)
	if !ok {
		diagnostics = append(diagnostics, ModelSchemaDiagnostic{Path: path + ".thinking", ModelID: modelID, Issue: "invalid_type", Expected: "object", message: path + ".thinking is missing or not an object"})
	} else {
		diagnostics = append(diagnostics, validateRequiredBool(path+".thinking.supported", modelID, thinking, "supported")...)
		types, ok := thinking["types"].(map[string]any)
		if !ok {
			diagnostics = append(diagnostics, ModelSchemaDiagnostic{Path: path + ".thinking.types", ModelID: modelID, Issue: "invalid_type", Expected: "object with adaptive and enabled objects, each containing a boolean supported field", message: path + ".thinking.types is missing or not an object"})
		} else {
			for _, key := range []string{"adaptive", "enabled"} {
				diagnostics = append(diagnostics, validateCapabilitySupport(path+".thinking.types."+key, modelID, types, key)...)
			}
		}
	}
	return diagnostics
}

func validateCapabilitySupport(path, modelID string, parent map[string]any, key string) []ModelSchemaDiagnostic {
	value, exists := parent[key]
	if !exists {
		return []ModelSchemaDiagnostic{{Path: path, ModelID: modelID, Issue: "missing", Expected: "object containing a boolean supported field", message: path + " is missing"}}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return []ModelSchemaDiagnostic{{Path: path, ModelID: modelID, Issue: "invalid_type", Expected: "object containing a boolean supported field", message: path + " is not an object"}}
	}
	return validateRequiredBool(path+".supported", modelID, object, "supported")
}

func validateRequiredBool(path, modelID string, parent map[string]any, key string) []ModelSchemaDiagnostic {
	value, exists := parent[key]
	if !exists {
		return []ModelSchemaDiagnostic{{Path: path, ModelID: modelID, Issue: "missing", Expected: "boolean", message: path + " is missing"}}
	}
	if _, ok := value.(bool); !ok {
		return []ModelSchemaDiagnostic{{Path: path, ModelID: modelID, Issue: "invalid_type", Expected: "boolean", message: path + " is not boolean"}}
	}
	return nil
}

func positiveInteger(value any) (int64, bool) {
	number, ok := nonNegativeInteger(value)
	return number, ok && number > 0
}

func ValidateMessage(root map[string]any) []string {
	var problems []string
	if stringValue(root["id"]) == "" {
		problems = append(problems, "id is missing")
	}
	if stringValue(root["type"]) != "message" {
		problems = append(problems, "type is not message")
	}
	if stringValue(root["role"]) != "assistant" {
		problems = append(problems, "role is not assistant")
	}
	if stringValue(root["model"]) == "" {
		problems = append(problems, "model is missing")
	}
	if _, ok := root["content"].([]any); !ok {
		problems = append(problems, "content is not an array")
	} else {
		for index, raw := range root["content"].([]any) {
			block, ok := raw.(map[string]any)
			if !ok {
				problems = append(problems, fmt.Sprintf("content[%d] is not an object", index))
				continue
			}
			typ := stringValue(block["type"])
			if typ == "" {
				problems = append(problems, fmt.Sprintf("content[%d].type is missing", index))
			}
			if typ == "text" {
				if _, ok := block["text"].(string); !ok {
					problems = append(problems, fmt.Sprintf("content[%d].text is not a string", index))
				}
			}
		}
	}
	if value, exists := root["stop_reason"]; !exists {
		problems = append(problems, "stop_reason is missing")
	} else if value != nil {
		if _, ok := value.(string); !ok {
			problems = append(problems, "stop_reason is not string or null")
		}
	}
	usage, ok := root["usage"].(map[string]any)
	if !ok {
		problems = append(problems, "usage is missing or not an object")
	} else {
		for _, field := range []string{"input_tokens", "output_tokens"} {
			if value, valid := nonNegativeInteger(usage[field]); !valid || value < 0 {
				problems = append(problems, "usage."+field+" is missing or not a non-negative integer")
			}
		}
	}
	return problems
}

func nonNegativeInteger(value any) (int64, bool) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil && parsed >= 0
	case int:
		return int64(typed), typed >= 0
	case int64:
		return typed, typed >= 0
	case float64:
		integer := int64(typed)
		return integer, typed == float64(integer) && integer >= 0
	default:
		return 0, false
	}
}

func ParseUsage(root map[string]any) result.Usage {
	usage, _ := root["usage"].(map[string]any)
	if usage == nil {
		return result.Usage{}
	}
	u := result.Usage{
		InputTokens:              int64Value(usage["input_tokens"]),
		OutputTokens:             int64Value(usage["output_tokens"]),
		CacheCreationInputTokens: int64Value(usage["cache_creation_input_tokens"]),
		CacheReadInputTokens:     int64Value(usage["cache_read_input_tokens"]),
	}
	u.TotalTokens = u.InputTokens + u.OutputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
	if details, ok := usage["output_tokens_details"].(map[string]any); ok {
		u.ThinkingTokens = int64Value(details["thinking_tokens"])
	}
	if details, ok := usage["cache_creation"].(map[string]any); ok {
		u.ProviderFields = map[string]int64{}
		for key, value := range details {
			u.ProviderFields["cache_creation."+key] = int64Value(value)
		}
	}
	return u
}

func ValidateUsage(u result.Usage) []string {
	var problems []string
	if u.InputTokens < 0 {
		problems = append(problems, "input_tokens is negative")
	}
	if u.OutputTokens < 0 {
		problems = append(problems, "output_tokens is negative")
	}
	if u.InputTokens == 0 {
		problems = append(problems, "input_tokens is missing or zero")
	}
	return problems
}

func Text(root map[string]any) string {
	blocks, _ := root["content"].([]any)
	var parts []string
	for _, raw := range blocks {
		block, _ := raw.(map[string]any)
		if stringValue(block["type"]) == "text" {
			parts = append(parts, stringValue(block["text"]))
		}
	}
	return strings.Join(parts, "")
}

func ToolUses(root map[string]any) []map[string]any {
	blocks, _ := root["content"].([]any)
	var out []map[string]any
	for _, raw := range blocks {
		if block, ok := raw.(map[string]any); ok && stringValue(block["type"]) == "tool_use" {
			out = append(out, block)
		}
	}
	return out
}

func ValidateError(root map[string]any) []string {
	var problems []string
	if stringValue(root["type"]) != "error" {
		problems = append(problems, "type is not error")
	}
	errMap, _ := root["error"].(map[string]any)
	if errMap == nil {
		return append(problems, "error is missing or not an object")
	}
	if stringValue(errMap["type"]) == "" {
		problems = append(problems, "error.type is missing")
	}
	if stringValue(errMap["message"]) == "" {
		problems = append(problems, "error.message is missing")
	}
	return problems
}

type StreamValidation struct {
	Text            string
	Usage           result.Usage
	ToolInputs      map[int]string
	Problems        []string
	Unknown         []string
	ProviderError   bool
	Incomplete      bool
	SawMessageStop  bool
	SawDoneSentinel bool
}

func ValidateStream(stream api.StreamResponse) StreamValidation {
	out := StreamValidation{ToolInputs: map[int]string{}, Problems: append([]string(nil), stream.InvalidEvents...)}
	started, stopped := false, false
	openBlocks := map[int]bool{}
	closedBlocks := map[int]bool{}
	messageDeltaSeen := false
	for _, event := range stream.Events {
		if event.Data == "[DONE]" {
			out.SawDoneSentinel = true
			continue
		}
		typ := stringValue(event.JSON["type"])
		if event.Event == "" {
			out.Problems = append(out.Problems, "SSE event name is missing")
			continue
		}
		known := event.Event == "message_start" || event.Event == "content_block_start" || event.Event == "content_block_delta" || event.Event == "content_block_stop" || event.Event == "message_delta" || event.Event == "message_stop" || event.Event == "ping" || event.Event == "error"
		if known && event.Event != typ {
			out.Problems = append(out.Problems, fmt.Sprintf("event %q has JSON type %q", event.Event, typ))
		}
		switch event.Event {
		case "message_start":
			if started || stopped {
				out.Problems = append(out.Problems, "duplicate or late message_start")
			}
			started = true
			if message, ok := event.JSON["message"].(map[string]any); ok {
				out.Usage = ParseUsage(message)
			}
		case "content_block_start":
			index, valid := nonNegativeIndex(event.JSON["index"])
			if !valid {
				out.Problems = append(out.Problems, "content block index is missing or negative")
				continue
			}
			if !started || stopped || openBlocks[index] || closedBlocks[index] {
				out.Problems = append(out.Problems, "content block started out of sequence")
			}
			openBlocks[index] = true
		case "content_block_delta":
			index, valid := nonNegativeIndex(event.JSON["index"])
			if !valid {
				out.Problems = append(out.Problems, "content delta index is missing or negative")
				continue
			}
			if !openBlocks[index] {
				out.Problems = append(out.Problems, "content delta before block start")
			}
			delta, _ := event.JSON["delta"].(map[string]any)
			switch stringValue(delta["type"]) {
			case "text_delta":
				out.Text += stringValue(delta["text"])
			case "input_json_delta":
				out.ToolInputs[index] += stringValue(delta["partial_json"])
			}
		case "content_block_stop":
			index, valid := nonNegativeIndex(event.JSON["index"])
			if !valid {
				out.Problems = append(out.Problems, "content stop index is missing or negative")
				continue
			}
			if !openBlocks[index] {
				out.Problems = append(out.Problems, "content block stopped before start")
			}
			delete(openBlocks, index)
			closedBlocks[index] = true
			if raw := out.ToolInputs[index]; raw != "" && !json.Valid([]byte(raw)) {
				out.Problems = append(out.Problems, "streamed tool input is invalid JSON")
			}
		case "message_delta":
			if !started || stopped || len(openBlocks) > 0 {
				out.Problems = append(out.Problems, "message_delta before message_start")
			}
			messageDeltaSeen = true
			if usage, ok := event.JSON["usage"].(map[string]any); ok {
				if _, present := usage["input_tokens"]; present {
					out.Usage.InputTokens = int64Value(usage["input_tokens"])
				}
				if _, present := usage["output_tokens"]; present {
					out.Usage.OutputTokens = int64Value(usage["output_tokens"])
				}
				if _, present := usage["cache_creation_input_tokens"]; present {
					out.Usage.CacheCreationInputTokens = int64Value(usage["cache_creation_input_tokens"])
				}
				if _, present := usage["cache_read_input_tokens"]; present {
					out.Usage.CacheReadInputTokens = int64Value(usage["cache_read_input_tokens"])
				}
				if details, ok := usage["output_tokens_details"].(map[string]any); ok {
					out.Usage.ThinkingTokens = int64Value(details["thinking_tokens"])
				}
			}
		case "message_stop":
			if !started || len(openBlocks) > 0 || stopped || !messageDeltaSeen {
				out.Problems = append(out.Problems, "message_stop is out of sequence")
			}
			stopped = true
			out.SawMessageStop = true
		case "error":
			out.ProviderError = true
		case "ping":
		default:
			out.Unknown = append(out.Unknown, event.Event)
		}
	}
	if !out.ProviderError && !out.SawMessageStop {
		out.Incomplete = true
	}
	out.Usage.TotalTokens = out.Usage.InputTokens + out.Usage.OutputTokens + out.Usage.CacheCreationInputTokens + out.Usage.CacheReadInputTokens
	return out
}

func nonNegativeIndex(value any) (int, bool) {
	switch value.(type) {
	case json.Number, float64, int, int64:
		index := int(int64Value(value))
		return index, index >= 0
	default:
		return 0, false
	}
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if n, ok := v.(json.Number); ok {
		return n.String()
	}
	return ""
}
func int64Value(v any) int64 {
	switch n := v.(type) {
	case json.Number:
		i, _ := n.Int64()
		return i
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	}
	return 0
}
