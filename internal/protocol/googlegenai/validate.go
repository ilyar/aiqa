package googlegenai

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/ilyar/aiqa/internal/result"
)

func ValidateModels(root map[string]any) []string {
	items, ok := root["models"].([]any)
	if !ok {
		return []string{"models is missing or not an array"}
	}
	seen := map[string]bool{}
	var problems []string
	for index, raw := range items {
		model, ok := raw.(map[string]any)
		if !ok {
			problems = append(problems, fmt.Sprintf("models[%d] is not an object", index))
			continue
		}
		name, _ := model["name"].(string)
		if name == "" || !strings.HasPrefix(name, "models/") {
			problems = append(problems, fmt.Sprintf("models[%d].name must use models/ prefix", index))
		}
		id := NormalizeModelID(name)
		if id != "" && seen[id] {
			problems = append(problems, fmt.Sprintf("models[%d].name is duplicated", index))
		}
		if id != "" {
			seen[id] = true
		}
		for _, field := range []string{"inputTokenLimit", "outputTokenLimit"} {
			if value, exists := model[field]; exists {
				if n, ok := nonNegativeInteger(value); !ok || n < 0 {
					problems = append(problems, field+" must be a non-negative integer")
				}
			}
		}
		if methods, exists := model["supportedGenerationMethods"]; exists {
			// The Models contract specifies an array of strings, not a nonempty
			// array. An empty catalog declaration must not invent generation support.
			if !validGenerationMethods(methods) {
				problems = append(problems, fmt.Sprintf("models[%d].supportedGenerationMethods must be an array of strings", index))
			}
		}
	}
	if token, exists := root["nextPageToken"]; exists && token != nil {
		if _, ok := token.(string); !ok {
			problems = append(problems, "nextPageToken must be a string")
		}
	}
	return problems
}

func validGenerationMethods(raw any) bool {
	switch values := raw.(type) {
	case []any:
		for _, value := range values {
			if _, ok := value.(string); !ok {
				return false
			}
		}
		return true
	case []string:
		return true
	default:
		return false
	}
}

func ValidateGenerateContent(root map[string]any) []string {
	var problems []string
	candidates, ok := root["candidates"].([]any)
	if !ok || len(candidates) == 0 {
		problems = append(problems, "candidates is missing or empty")
	} else {
		for index, raw := range candidates {
			candidate, ok := raw.(map[string]any)
			if !ok {
				problems = append(problems, fmt.Sprintf("candidates[%d] is not an object", index))
				continue
			}
			content, ok := candidate["content"].(map[string]any)
			if !ok {
				problems = append(problems, fmt.Sprintf("candidates[%d].content is missing", index))
				continue
			}
			parts, ok := content["parts"].([]any)
			if !ok || len(parts) == 0 {
				problems = append(problems, fmt.Sprintf("candidates[%d].content.parts is missing or empty", index))
			}
		}
	}
	if finish := stringValue(root["finishReason"]); finish == "" && len(candidates) > 0 {
		// finishReason is normally on the candidate, so do not reject a valid
		// response when it is nested there.
	}
	return problems
}

func ValidateInteraction(root map[string]any) []string {
	var problems []string
	for _, field := range []string{"id", "object", "status"} {
		if stringValue(root[field]) == "" {
			problems = append(problems, field+" is missing")
		}
	}
	if object := stringValue(root["object"]); object != "" && object != "interaction" {
		problems = append(problems, "object must be interaction")
	}
	if steps, ok := root["steps"].([]any); !ok || len(steps) == 0 {
		problems = append(problems, "steps is missing or empty")
	} else {
		for index, raw := range steps {
			step, ok := raw.(map[string]any)
			if !ok || stringValue(step["type"]) == "" {
				problems = append(problems, fmt.Sprintf("steps[%d] must be an object with type", index))
			}
		}
	}
	usage, ok := root["usage"].(map[string]any)
	if !ok {
		problems = append(problems, "usage is missing or not an object")
	} else {
		problems = append(problems, ValidateUsage(usage)...)
	}
	return problems
}

func knownInteractionStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "completed", "requires_action", "failed", "cancelled", "incomplete":
		return true
	default:
		return false
	}
}

func ValidateUsage(root map[string]any) []string {
	var problems []string
	if root == nil {
		return []string{"usage is missing"}
	}
	knownCounter := false
	for _, field := range []string{"promptTokenCount", "candidatesTokenCount", "thoughtsTokenCount", "totalTokenCount", "cachedContentTokenCount", "toolUsePromptTokenCount", "input_tokens", "output_tokens", "total_tokens", "total_input_tokens", "total_output_tokens", "total_cached_tokens", "total_thought_tokens", "total_tool_use_tokens"} {
		if raw, exists := root[field]; exists {
			knownCounter = true
			if n, ok := nonNegativeInteger(raw); !ok || n < 0 {
				problems = append(problems, "usage."+field+" is not a non-negative integer")
			}
		}
	}
	if !knownCounter {
		problems = append(problems, "usage has no recognized token counters")
	}
	return problems
}

func ParseUsage(root map[string]any) result.Usage {
	usage := result.Usage{}
	if raw, ok := root["usageMetadata"].(map[string]any); ok {
		usage.InputTokens = numberValue(raw["promptTokenCount"])
		usage.OutputTokens = numberValue(raw["candidatesTokenCount"])
		usage.ThinkingTokens = numberValue(raw["thoughtsTokenCount"])
		usage.TotalTokens = numberValue(raw["totalTokenCount"])
		usage.CacheReadInputTokens = numberValue(raw["cachedContentTokenCount"])
		usage.ProviderFields = googleProviderFields(raw)
		return usage
	}
	if raw, ok := root["usage"].(map[string]any); ok {
		usage.InputTokens = firstNumber(raw, "input_tokens", "total_input_tokens", "promptTokenCount")
		usage.OutputTokens = firstNumber(raw, "output_tokens", "total_output_tokens", "candidatesTokenCount")
		usage.TotalTokens = firstNumber(raw, "total_tokens", "totalTokenCount")
		usage.CacheReadInputTokens = firstNumber(raw, "cache_read_input_tokens", "total_cached_tokens", "cachedContentTokenCount")
		usage.ThinkingTokens = firstNumber(raw, "thinking_tokens", "total_thought_tokens", "thoughtsTokenCount")
		usage.ProviderFields = googleProviderFields(raw)
	}
	return usage
}

func firstNumber(root map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if value, exists := root[key]; exists {
			return numberValue(value)
		}
	}
	return 0
}

func googleProviderFields(root map[string]any) map[string]int64 {
	fields := map[string]int64{}
	for _, item := range []struct {
		key  string
		name string
	}{
		{"toolUsePromptTokenCount", "tool_use_prompt_tokens"},
		{"total_tool_use_tokens", "tool_use_tokens"},
		{"thoughtsTokenCount", "thoughts_token_count"},
	} {
		if value := numberValue(root[item.key]); value != 0 {
			fields[item.name] = value
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func ValidateError(root map[string]any) []string {
	errorObject, ok := root["error"].(map[string]any)
	if !ok {
		return []string{"error envelope is missing"}
	}
	var problems []string
	if stringValue(errorObject["message"]) == "" {
		problems = append(problems, "error.message is missing")
	}
	if code, exists := errorObject["code"]; exists {
		if n, ok := nonNegativeInteger(code); !ok || n < 400 {
			problems = append(problems, "error.code is not a valid integer")
		}
	}
	if stringValue(errorObject["status"]) == "" && stringValue(errorObject["type"]) == "" {
		problems = append(problems, "error.status or error.type is missing")
	}
	return problems
}

func ValidateEmbedding(root map[string]any) []string {
	values := []any{}
	if embeddings, ok := root["embeddings"].([]any); ok {
		for _, item := range embeddings {
			if object, ok := item.(map[string]any); ok {
				if vector, ok := object["values"].([]any); ok {
					values = append(values, vector...)
				}
			}
		}
	} else if embedding, ok := root["embedding"].(map[string]any); ok {
		if vector, ok := embedding["values"].([]any); ok {
			values = vector
		}
	}
	if len(values) == 0 {
		return []string{"embedding vector is missing or empty"}
	}
	for index, value := range values {
		n, ok := numberFloat(value)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return []string{fmt.Sprintf("embedding vector element %d is not finite", index)}
		}
	}
	return nil
}

func nonNegativeInteger(value any) (int64, bool) {
	n := numberValue(value)
	if n < 0 {
		return n, false
	}
	switch value.(type) {
	case float64, float32:
		f, _ := numberFloat(value)
		return int64(f), f == math.Trunc(f)
	default:
		return n, true
	}
}

func numberValue(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case json.Number:
		n, _ := typed.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(typed, 10, 64)
		return n
	default:
		return 0
	}
}

func numberFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		n, err := typed.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}

func stringValue(value any) string {
	s, _ := value.(string)
	return strings.TrimSpace(s)
}

func stringValues(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return typed
	default:
		return nil
	}
}
