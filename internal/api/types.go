package api

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ilyar/aiqa/internal/result"
)

type Model struct {
	ID  string
	Raw map[string]any
}

type Response struct {
	StatusCode  int
	Header      map[string][]string
	ContentType string
	RequestID   string
	Body        []byte
	JSON        map[string]any
	Duration    time.Duration
	TraceID     string
}

func (r Response) Is2xx() bool { return r.StatusCode >= 200 && r.StatusCode < 300 }

func (r Response) Snippet(limit int) string {
	if limit <= 0 || len(r.Body) <= limit {
		return strings.TrimSpace(string(r.Body))
	}
	return strings.TrimSpace(string(r.Body[:limit])) + "…"
}

type StreamResponse struct {
	StatusCode    int
	Header        map[string][]string
	ContentType   string
	RequestID     string
	Duration      time.Duration
	Events        []SSEEvent
	Done          bool
	Content       string
	FinishReasons []string
	Usage         result.Usage
	InvalidEvents []string
	ToolCalls     []map[string]any
	RawSnippet    string
	TraceID       string
}

func (r StreamResponse) Is2xx() bool { return r.StatusCode >= 200 && r.StatusCode < 300 }

type SSEEvent struct {
	Event string
	ID    string
	Data  string
	JSON  map[string]any
}

func ParseUsage(root map[string]any) result.Usage {
	usageMap, _ := asMap(root["usage"])
	if usageMap == nil {
		return result.Usage{}
	}
	u := result.Usage{
		PromptTokens:     int64Value(usageMap["prompt_tokens"]),
		CompletionTokens: int64Value(usageMap["completion_tokens"]),
		TotalTokens:      int64Value(usageMap["total_tokens"]),
	}
	if u.PromptTokens == 0 {
		u.PromptTokens = int64Value(usageMap["input_tokens"])
	}
	if u.CompletionTokens == 0 {
		u.CompletionTokens = int64Value(usageMap["output_tokens"])
	}
	if u.TotalTokens == 0 && (u.PromptTokens > 0 || u.CompletionTokens > 0) {
		u.TotalTokens = u.PromptTokens + u.CompletionTokens
	}
	for _, key := range []string{"completion_tokens_details", "output_tokens_details"} {
		if details, _ := asMap(usageMap[key]); details != nil {
			if n := int64Value(details["reasoning_tokens"]); n > 0 {
				u.ReasoningTokens = n
				break
			}
		}
	}
	return u
}

func FirstChoice(root map[string]any) (map[string]any, bool) {
	choices, ok := root["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil, false
	}
	choice, ok := choices[0].(map[string]any)
	return choice, ok
}

func FirstMessage(root map[string]any) (map[string]any, bool) {
	choice, ok := FirstChoice(root)
	if !ok {
		return nil, false
	}
	message, ok := choice["message"].(map[string]any)
	return message, ok
}

func FirstContent(root map[string]any) string {
	message, ok := FirstMessage(root)
	if !ok {
		return ""
	}
	return contentString(message["content"])
}

func FinishReason(root map[string]any) string {
	choice, ok := FirstChoice(root)
	if !ok {
		return ""
	}
	return stringValue(choice["finish_reason"])
}

func ChoiceCount(root map[string]any) int {
	choices, _ := root["choices"].([]any)
	return len(choices)
}

func FirstToolCalls(root map[string]any) []map[string]any {
	message, ok := FirstMessage(root)
	if !ok {
		return nil
	}
	arr, _ := message["tool_calls"].([]any)
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func ErrorShape(root map[string]any) (message, typ, code string, ok bool) {
	errMap, _ := asMap(root["error"])
	if errMap == nil {
		return "", "", "", false
	}
	message = stringValue(errMap["message"])
	typ = stringValue(errMap["type"])
	code = stringValue(errMap["code"])
	return message, typ, code, message != ""
}

func ReasoningEvidence(root map[string]any) (text string, tokens int64, keys []string) {
	message, _ := FirstMessage(root)
	if message != nil {
		for _, key := range []string{"reasoning_content", "reasoning", "analysis"} {
			if value, exists := message[key]; exists {
				keys = append(keys, "choices[0].message."+key)
				if text == "" {
					text = contentString(value)
				}
			}
		}
		if value, exists := message["reasoning_details"]; exists {
			keys = append(keys, "choices[0].message.reasoning_details")
			if text == "" {
				b, _ := json.Marshal(value)
				text = string(b)
			}
		}
	}
	u := ParseUsage(root)
	tokens = u.ReasoningTokens
	if tokens > 0 {
		keys = append(keys, "usage.*_tokens_details.reasoning_tokens")
	}
	return text, tokens, keys
}

func ValidateChatShape(root map[string]any) []string {
	var problems []string
	if stringValue(root["id"]) == "" {
		problems = append(problems, "id is missing or not a string")
	}
	if object := stringValue(root["object"]); object == "" {
		problems = append(problems, "object is missing or not a string")
	} else if object != "chat.completion" {
		problems = append(problems, fmt.Sprintf("object=%q, expected chat.completion", object))
	}
	if _, ok := numberValue(root["created"]); !ok {
		problems = append(problems, "created is missing or not numeric")
	}
	if stringValue(root["model"]) == "" {
		problems = append(problems, "model is missing or not a string")
	}
	choices, ok := root["choices"].([]any)
	if !ok || len(choices) == 0 {
		problems = append(problems, "choices is missing, not an array, or empty")
		return problems
	}
	for i, raw := range choices {
		choice, ok := raw.(map[string]any)
		if !ok {
			problems = append(problems, fmt.Sprintf("choices[%d] is not an object", i))
			continue
		}
		if _, ok := numberValue(choice["index"]); !ok {
			problems = append(problems, fmt.Sprintf("choices[%d].index is missing or not numeric", i))
		}
		message, ok := choice["message"].(map[string]any)
		if !ok {
			problems = append(problems, fmt.Sprintf("choices[%d].message is missing or not an object", i))
			continue
		}
		if role := stringValue(message["role"]); role != "assistant" {
			problems = append(problems, fmt.Sprintf("choices[%d].message.role=%q, expected assistant", i, role))
		}
		_, hasContent := message["content"]
		_, hasTools := message["tool_calls"]
		_, hasFunction := message["function_call"]
		if !hasContent && !hasTools && !hasFunction {
			problems = append(problems, fmt.Sprintf("choices[%d].message has neither content nor tool_calls", i))
		}
		if _, exists := choice["finish_reason"]; !exists {
			problems = append(problems, fmt.Sprintf("choices[%d].finish_reason is missing", i))
		}
	}
	return problems
}

func ValidateUsage(u result.Usage) []string {
	var problems []string
	if u.PromptTokens <= 0 {
		problems = append(problems, "prompt_tokens/input_tokens is missing or zero")
	}
	if u.CompletionTokens < 0 {
		problems = append(problems, "completion_tokens/output_tokens is negative")
	}
	if u.TotalTokens <= 0 {
		problems = append(problems, "total_tokens is missing or zero")
	}
	if u.TotalTokens > 0 && u.PromptTokens+u.CompletionTokens > 0 && u.TotalTokens < u.PromptTokens+u.CompletionTokens {
		problems = append(problems, "total_tokens is less than prompt_tokens + completion_tokens")
	}
	return problems
}

func int64Value(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	case int32:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	default:
		return 0
	}
}

func numberValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func stringValue(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case json.Number:
		return s.String()
	case float64:
		return strconv.FormatFloat(s, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprint(s)
	}
}

func contentString(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case nil:
		return ""
	case []any:
		var b strings.Builder
		for _, item := range c {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text := stringValue(m["text"]); text != "" {
				b.WriteString(text)
			}
			if textMap, _ := asMap(m["text"]); textMap != nil {
				b.WriteString(stringValue(textMap["value"]))
			}
		}
		return b.String()
	default:
		return fmt.Sprint(c)
	}
}

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}
