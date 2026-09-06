package logging

import (
	"bytes"
	"encoding/json"
	"mime"
	"sort"
	"strings"
)

// DiagnosticSchemaVersion identifies the intentionally small, private
// fingerprint contract. It is separate from the enclosing trace schema so a
// reader can evolve shape interpretation without accepting arbitrary fields.
const DiagnosticSchemaVersion = "aiqa.diagnostic.v1"

// SSEShapeEvent is the non-payload portion of one parsed SSE event. Callers
// must not put Data, IDs or any provider text here.
type SSEShapeEvent struct {
	Event string
	JSON  map[string]any
	Done  bool
}

// ResponseShapeFields produces the complete allowlisted payload for a terminal
// transport diagnostic. It inspects JSON only in memory and returns field names
// and value kinds from fixed allowlists; no provider values are retained.
func ResponseShapeFields(method, path, requestContentType string, requestBody []byte, status int, responseContentType, requestID string, responseBody []byte) Fields {
	request, _ := jsonObject(requestBody)
	response, _ := jsonObject(responseBody)
	fields := Fields{
		"diagnostic_schema":          DiagnosticSchemaVersion,
		"method":                     safeMethod(method),
		"path":                       path,
		"request_content_type":       safeContentType(requestContentType),
		"request_size_bucket":        sizeBucket(len(requestBody)),
		"request_shape_keys":         shapeKeys(request, approvedRequestKeys),
		"request_shape_types":        shapeTypes(request, approvedRequestKeys),
		"request_unknown_key_count":  unknownKeyCount(request, approvedRequestKeys),
		"status":                     status,
		"response_content_type":      safeContentType(responseContentType),
		"provider_request_id":        safeRequestID(requestID),
		"response_shape_keys":        shapeKeys(response, approvedResponseKeys),
		"response_shape_types":       shapeTypes(response, approvedResponseKeys),
		"response_unknown_key_count": unknownKeyCount(response, approvedResponseKeys),
	}
	if errorEnvelope, ok := response["error"].(map[string]any); ok {
		fields["error_present"] = true
		fields["error_shape_keys"] = shapeKeys(errorEnvelope, approvedErrorKeys)
		fields["error_shape_types"] = shapeTypes(errorEnvelope, approvedErrorKeys)
		fields["error_code"] = numericCode(errorEnvelope["code"])
		fields["error_status"] = safeErrorStatus(errorEnvelope["status"])
	} else {
		fields["error_present"] = false
	}
	return fields
}

// SSEShapeFields adds a terminal stream summary. Ordered names and event types
// are normalized to reviewed vocabulary; all other provider-supplied names are
// represented by the literal "unknown" rather than being written verbatim.
func SSEShapeFields(events []SSEShapeEvent, invalidCount int, done bool) Fields {
	names := make([]string, 0, len(events))
	types := make([]string, 0, len(events))
	correlations := make([]string, 0, len(events))
	terminal := make([]string, 0, 2)
	for _, event := range events {
		if event.Event != "" {
			names = append(names, safeSSEName(event.Event))
		}
		if event.JSON != nil {
			if raw, ok := event.JSON["event_type"].(string); ok {
				types = append(types, safeSSEName(raw))
			}
			for _, key := range []string{"index", "step_id"} {
				if value, present := event.JSON[key]; present {
					correlations = append(correlations, key+":"+valueKind(value))
					break
				}
			}
			if interaction, ok := event.JSON["interaction"].(map[string]any); ok {
				if raw, ok := interaction["event_type"].(string); ok {
					types = append(types, safeSSEName(raw))
				}
			}
			if step, ok := event.JSON["step"].(map[string]any); ok {
				for _, key := range []string{"index", "step_id"} {
					if value, present := step[key]; present {
						correlations = append(correlations, key+":"+valueKind(value))
						break
					}
				}
			}
		}
		if event.Done {
			terminal = append(terminal, "done")
		}
	}
	if done {
		terminal = append(terminal, "stream_done")
	}
	return Fields{
		"sse_event_names":         strings.Join(names, ","),
		"sse_event_types":         strings.Join(types, ","),
		"sse_correlation_fields":  strings.Join(correlations, ","),
		"sse_terminal_markers":    strings.Join(terminal, ","),
		"sse_invalid_event_count": invalidCount,
	}
}

func jsonObject(raw []byte) (map[string]any, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, false
	}
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&object) != nil {
		return nil, false
	}
	return object, true
}

func shapeKeys(object map[string]any, approved map[string]struct{}) string {
	keys := make([]string, 0, len(object))
	for key := range object {
		if _, ok := approved[key]; ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func shapeTypes(object map[string]any, approved map[string]struct{}) string {
	types := make([]string, 0, len(object))
	for key, value := range object {
		if _, ok := approved[key]; ok {
			types = append(types, key+":"+valueKind(value))
		}
	}
	sort.Strings(types)
	return strings.Join(types, ",")
}

func unknownKeyCount(object map[string]any, approved map[string]struct{}) int {
	count := 0
	for key := range object {
		if _, ok := approved[key]; !ok {
			count++
		}
	}
	return count
}

func valueKind(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case json.Number, float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "number"
	default:
		return "other"
	}
}

func safeMethod(method string) string {
	switch strings.ToUpper(method) {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD":
		return strings.ToUpper(method)
	default:
		return "OTHER"
	}
}

func safeContentType(contentType string) string {
	parsed, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	switch strings.ToLower(parsed) {
	case "application/json", "text/event-stream", "application/problem+json", "text/plain":
		return strings.ToLower(parsed)
	default:
		return "other"
	}
}

func sizeBucket(size int) string {
	switch {
	case size == 0:
		return "0"
	case size <= 1024:
		return "1_1024"
	case size <= 16<<10:
		return "1025_16384"
	case size <= 256<<10:
		return "16385_262144"
	default:
		return "262145_plus"
	}
}

func numericCode(value any) int {
	switch code := value.(type) {
	case json.Number:
		value, _ := code.Int64()
		return int(value)
	case float64:
		return int(code)
	case int:
		return code
	default:
		return 0
	}
}

func safeErrorStatus(value any) string {
	status, _ := value.(string)
	switch status {
	case "INVALID_ARGUMENT", "NOT_FOUND", "RESOURCE_EXHAUSTED", "UNAUTHENTICATED", "PERMISSION_DENIED", "INTERNAL", "UNAVAILABLE", "DEADLINE_EXCEEDED":
		return strings.ToLower(status)
	case "":
		return ""
	default:
		return "other"
	}
}

func safeRequestID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 128 {
		return ""
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return ""
		}
	}
	return value
}

func safeSSEName(value string) string {
	approved := map[string]struct{}{
		"message_start": {}, "message_delta": {}, "message_stop": {}, "content_block_start": {}, "content_block_delta": {}, "content_block_stop": {},
		"response.created": {}, "response.output_text.delta": {}, "response.completed": {}, "response.failed": {},
		"interaction.created": {}, "interaction.completed": {}, "step.start": {}, "step.delta": {}, "step.stop": {},
	}
	if _, ok := approved[value]; ok {
		return value
	}
	return "unknown"
}

var approvedRequestKeys = setOf("model", "messages", "input", "stream", "tools", "tool_choice", "response_format", "max_tokens", "max_output_tokens", "temperature", "n", "size", "quality", "contents", "generationConfig")
var approvedResponseKeys = setOf("id", "object", "created", "model", "choices", "usage", "error", "type", "data", "status", "code", "request_id", "interaction", "step", "delta", "event_type")
var approvedErrorKeys = setOf("code", "status", "type", "param", "message", "details")

func setOf(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}
