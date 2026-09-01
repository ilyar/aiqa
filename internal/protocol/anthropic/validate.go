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

func ValidateModels(root map[string]any) []string {
	var problems []string
	data, ok := root["data"].([]any)
	if !ok {
		return []string{"data is missing or not an array"}
	}
	seen := map[string]bool{}
	for i, raw := range data {
		model, ok := raw.(map[string]any)
		if !ok {
			problems = append(problems, fmt.Sprintf("data[%d] is not an object", i))
			continue
		}
		id := stringValue(model["id"])
		if id == "" {
			problems = append(problems, fmt.Sprintf("data[%d].id is missing", i))
		} else if seen[id] {
			problems = append(problems, fmt.Sprintf("data[%d].id is duplicated", i))
		}
		seen[id] = true
		if stringValue(model["type"]) != "model" {
			problems = append(problems, fmt.Sprintf("data[%d].type is not model", i))
		}
		if stringValue(model["display_name"]) == "" {
			problems = append(problems, fmt.Sprintf("data[%d].display_name is missing", i))
		}
		created := stringValue(model["created_at"])
		if _, err := time.Parse(time.RFC3339, created); err != nil {
			problems = append(problems, fmt.Sprintf("data[%d].created_at is not RFC3339", i))
		}
	}
	if value, exists := root["has_more"]; !exists {
		problems = append(problems, "has_more is missing")
	} else if _, ok := value.(bool); !ok {
		problems = append(problems, "has_more is not boolean")
	}
	for _, key := range []string{"first_id", "last_id"} {
		if value, exists := root[key]; !exists {
			problems = append(problems, key+" is missing")
		} else if value != nil {
			if _, ok := value.(string); !ok {
				problems = append(problems, key+" is not string or null")
			}
		}
	}
	return problems
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
				out.Usage.OutputTokens = int64Value(usage["output_tokens"])
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
