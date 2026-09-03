package googlegenai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/result"
)

type InteractionStreamValidation struct {
	Text          string
	Usage         result.Usage
	Problems      []string
	UnknownEvents []string
	ProviderError bool
	Complete      bool
	SawDone       bool
	SawDoneEvent  bool
	SawCompleted  bool
	SawUsage      bool
	ToolArguments map[string]string
}

// ValidateInteractionStream is a separate state machine from legacy
// GenerateContent SSE. It tolerates unknown future event names while enforcing
// the required terminal marker and legal lifecycle ordering.
func ValidateInteractionStream(stream api.StreamResponse) InteractionStreamValidation {
	out := InteractionStreamValidation{Problems: append([]string(nil), stream.InvalidEvents...), ToolArguments: map[string]string{}}
	created, completed, started, stopped := false, false, map[string]bool{}, map[string]bool{}
	seenStep := false
	for _, event := range stream.Events {
		if event.Data == "[DONE]" {
			if event.Event != "" && event.Event != "done" {
				out.Problems = append(out.Problems, "[DONE] was emitted with a non-done event name")
			}
			if out.SawDone {
				out.Problems = append(out.Problems, "duplicate [DONE] terminal marker")
			}
			out.SawDone = true
			if event.Event == "done" {
				out.SawDoneEvent = true
			}
			continue
		}
		kind := event.Event
		if kind == "" {
			kind = stringValue(event.JSON["type"])
		}
		if kind == "" {
			out.Problems = append(out.Problems, "interaction SSE event has no type")
			continue
		}
		if event.Event != "" {
			jsonKind := stringValue(event.JSON["event_type"])
			if jsonKind == "" {
				jsonKind = stringValue(event.JSON["type"])
			}
			if jsonKind != "" && knownInteractionEvent(kind) && jsonKind != kind {
				out.Problems = append(out.Problems, fmt.Sprintf("SSE event name %q does not match JSON type %q", kind, jsonKind))
			}
		}
		switch kind {
		case "interaction.created":
			if created {
				out.Problems = append(out.Problems, "duplicate interaction.created")
			}
			created = true
		case "status_update":
			if !created || completed {
				out.Problems = append(out.Problems, "status_update out of sequence")
			}
		case "step.start":
			id := eventID(event.JSON)
			seenStep = true
			if id == "" {
				out.Problems = append(out.Problems, "step.start has no step id")
				continue
			}
			if !created || completed || started[id] {
				out.Problems = append(out.Problems, "step.start out of sequence")
			}
			started[id] = true
		case "step.delta":
			id := eventID(event.JSON)
			if id == "" {
				out.Problems = append(out.Problems, "step.delta has no step id")
				continue
			}
			if !started[id] || stopped[id] {
				out.Problems = append(out.Problems, "step.delta before/after step lifecycle")
			}
			out.Text += deltaText(event.JSON)
			appendToolArguments(out.ToolArguments, event.JSON)
		case "step.stop":
			id := eventID(event.JSON)
			if id == "" {
				out.Problems = append(out.Problems, "step.stop has no step id")
				continue
			}
			if !started[id] || stopped[id] {
				out.Problems = append(out.Problems, "step.stop out of sequence")
			}
			stopped[id] = true
		case "interaction.completed":
			if !created || completed {
				out.Problems = append(out.Problems, "interaction.completed out of sequence")
			}
			for id := range started {
				if !stopped[id] {
					out.Problems = append(out.Problems, "interaction.completed before step.stop: "+id)
				}
			}
			completed = true
			out.SawCompleted = true
			out.Usage = ParseUsage(event.JSON)
			if usage, ok := event.JSON["usage"].(map[string]any); ok {
				out.SawUsage = len(ValidateUsage(usage)) == 0
			}
		case "error", "interaction.failed":
			out.ProviderError = true
		default:
			out.UnknownEvents = append(out.UnknownEvents, kind)
		}
		if usage, ok := event.JSON["usage"].(map[string]any); ok {
			out.Usage = ParseUsage(map[string]any{"usageMetadata": usage})
			out.SawUsage = len(ValidateUsage(usage)) == 0
		}
	}
	for id := range started {
		if !stopped[id] {
			out.Problems = append(out.Problems, "step did not receive step.stop: "+id)
		}
	}
	for id, arguments := range out.ToolArguments {
		if strings.TrimSpace(arguments) != "" && !json.Valid([]byte(arguments)) {
			out.Problems = append(out.Problems, "tool arguments for "+id+" are not valid JSON")
		}
	}
	out.Complete = created && completed && seenStep && out.SawDone && out.SawDoneEvent
	if !out.Complete && !out.ProviderError {
		out.Problems = append(out.Problems, "interaction stream did not reach completed/[DONE]")
	}
	return out
}

type LegacyStreamValidation struct {
	Text            string
	Usage           result.Usage
	Problems        []string
	ProviderError   bool
	Complete        bool
	SawChunk        bool
	SawFinishReason bool
	SawUsage        bool
}

// ValidateGenerateContentStream validates the legacy stream independently of
// Interactions event names. Google legacy SSE consists of JSON response chunks;
// EOF is the terminal condition and [DONE] is not required.
func ValidateGenerateContentStream(stream api.StreamResponse) LegacyStreamValidation {
	out := LegacyStreamValidation{Problems: append([]string(nil), stream.InvalidEvents...)}
	for _, event := range stream.Events {
		if event.Data == "[DONE]" {
			continue
		}
		if stringValue(event.JSON["error"]) != "" {
			out.ProviderError = true
			continue
		}
		if _, ok := event.JSON["error"].(map[string]any); ok {
			out.ProviderError = true
			continue
		}
		out.Text += generateText(event.JSON)
		usage := ParseUsage(event.JSON)
		if usage.TotalTokens != 0 || usage.InputTokens != 0 || usage.OutputTokens != 0 {
			out.Usage = usage
			out.SawUsage = true
		}
		if candidates, present := event.JSON["candidates"]; present {
			out.SawChunk = true
			items, ok := candidates.([]any)
			if !ok || len(items) == 0 {
				out.Problems = append(out.Problems, "stream candidates must be a non-empty array")
				continue
			}
			for index, raw := range items {
				candidate, ok := raw.(map[string]any)
				if !ok {
					out.Problems = append(out.Problems, fmt.Sprintf("stream candidates[%d] is not an object", index))
					continue
				}
				if stringValue(candidate["finishReason"]) != "" || stringValue(candidate["finish_reason"]) != "" {
					out.SawFinishReason = true
				}
				if candidateUsage, ok := candidate["usageMetadata"].(map[string]any); ok {
					out.Usage = ParseUsage(map[string]any{"usageMetadata": candidateUsage})
					out.SawUsage = len(ValidateUsage(candidateUsage)) == 0
				}
				// A legacy stream may emit a finish/usage-only chunk.  Validate
				// content when it is present, but do not require it on every chunk.
				if content, present := candidate["content"]; present {
					contentObject, ok := content.(map[string]any)
					if !ok {
						out.Problems = append(out.Problems, fmt.Sprintf("stream candidates[%d].content is not an object", index))
						continue
					}
					if parts, present := contentObject["parts"]; present {
						if list, ok := parts.([]any); !ok || len(list) == 0 {
							out.Problems = append(out.Problems, fmt.Sprintf("stream candidates[%d].content.parts is not a non-empty array", index))
						}
					}
				}
			}
		}
	}
	out.Complete = len(stream.Events) > 0 && !out.ProviderError && len(out.Problems) == 0
	return out
}

func eventID(raw map[string]any) string {
	for _, key := range []string{"step_id", "stepId", "step_index", "stepIndex", "index", "id"} {
		if value := stringValue(raw[key]); value != "" {
			return value
		}
	}
	for _, key := range []string{"step", "data", "delta"} {
		if nested, ok := raw[key].(map[string]any); ok {
			if value := eventID(nested); value != "" {
				return value
			}
		}
	}
	return ""
}

func knownInteractionEvent(kind string) bool {
	switch kind {
	case "interaction.created", "status_update", "step.start", "step.delta", "step.stop", "interaction.completed", "interaction.failed", "error", "done":
		return true
	default:
		return false
	}
}

func deltaText(raw map[string]any) string {
	for _, key := range []string{"text", "text_delta", "output_text"} {
		if value := stringValue(raw[key]); value != "" {
			return value
		}
	}
	if delta, ok := raw["delta"].(map[string]any); ok {
		return deltaText(delta)
	}
	return ""
}

func appendToolArguments(out map[string]string, raw map[string]any) {
	value := stringValue(raw["arguments_delta"])
	if value == "" {
		if delta, ok := raw["delta"].(map[string]any); ok {
			value = stringValue(delta["arguments_delta"])
			if value == "" {
				value = stringValue(delta["arguments"])
			}
		}
	}
	if value == "" {
		return
	}
	id := eventID(raw)
	if id == "" {
		return
	}
	out[id] += value
	var decoded any
	if strings.HasSuffix(strings.TrimSpace(out[id]), "}") && json.Unmarshal([]byte(out[id]), &decoded) != nil {
		// Reported by the caller through a stable problem when needed; keeping
		// accumulation here prevents a malformed partial delta from being lost.
	}
}

func generateText(root map[string]any) string {
	candidates, _ := root["candidates"].([]any)
	var out strings.Builder
	for _, raw := range candidates {
		candidate, _ := raw.(map[string]any)
		content, _ := candidate["content"].(map[string]any)
		parts, _ := content["parts"].([]any)
		for _, partRaw := range parts {
			part, _ := partRaw.(map[string]any)
			if text := stringValue(part["text"]); text != "" {
				out.WriteString(text)
			}
		}
	}
	return out.String()
}

func streamSummary(kind string, validation any) string {
	return fmt.Sprintf("%s stream validated", kind)
}
