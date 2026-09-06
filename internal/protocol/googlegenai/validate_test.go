package googlegenai

import (
	"testing"

	"github.com/ilyar/aiqa/internal/api"
)

func TestValidateGoogleModelsAndUsage(t *testing.T) {
	valid := map[string]any{
		"models": []any{map[string]any{
			"name": "models/gemini-test", "inputTokenLimit": float64(4096), "outputTokenLimit": float64(256),
			"supportedGenerationMethods": []any{"generateContent", "countTokens"},
		}},
		"nextPageToken": "next",
	}
	if problems := ValidateModels(valid); len(problems) != 0 {
		t.Fatalf("valid model envelope rejected: %v", problems)
	}
	invalid := map[string]any{"models": []any{map[string]any{"name": "gemini-test", "inputTokenLimit": -1.0}}}
	if problems := ValidateModels(invalid); len(problems) == 0 {
		t.Fatal("invalid model envelope was accepted")
	}
	usage := ParseUsage(map[string]any{"usageMetadata": map[string]any{
		"promptTokenCount": float64(10), "candidatesTokenCount": float64(4), "thoughtsTokenCount": float64(2), "totalTokenCount": float64(16), "cachedContentTokenCount": float64(3),
	}})
	if usage.InputTokens != 10 || usage.OutputTokens != 4 || usage.ThinkingTokens != 2 || usage.TotalTokens != 16 || usage.CacheReadInputTokens != 3 {
		t.Fatalf("usage was not normalized: %+v", usage)
	}
	interactionsUsage := ParseUsage(map[string]any{"usage": map[string]any{
		"total_input_tokens": float64(11), "total_output_tokens": float64(5), "total_cached_tokens": float64(2), "total_thought_tokens": float64(1), "total_tokens": float64(17), "total_tool_use_tokens": float64(3),
	}})
	if interactionsUsage.InputTokens != 11 || interactionsUsage.OutputTokens != 5 || interactionsUsage.CacheReadInputTokens != 2 || interactionsUsage.ThinkingTokens != 1 || interactionsUsage.TotalTokens != 17 || interactionsUsage.ProviderFields["tool_use_tokens"] != 3 {
		t.Fatalf("Interactions usage was not normalized: %+v", interactionsUsage)
	}
	if problems := ValidateError(map[string]any{"error": map[string]any{"code": float64(400), "message": "bad request", "status": "INVALID_ARGUMENT"}}); len(problems) != 0 {
		t.Fatalf("valid error envelope rejected: %v", problems)
	}
}

func TestValidateGoogleEmbeddingRejectsNonFiniteValues(t *testing.T) {
	valid := map[string]any{"embedding": map[string]any{"values": []any{float64(0), float64(1.5)}}}
	if problems := ValidateEmbedding(valid); len(problems) != 0 {
		t.Fatalf("valid embedding rejected: %v", problems)
	}
	invalid := map[string]any{"embedding": map[string]any{"values": []any{"NaN"}}}
	if problems := ValidateEmbedding(invalid); len(problems) == 0 {
		t.Fatal("non-numeric embedding value was accepted")
	}
}

func TestValidateGoogleStreamsUseIndependentStateMachines(t *testing.T) {
	interaction := api.StreamResponse{Events: []api.SSEEvent{
		{Event: "interaction.created", JSON: map[string]any{"event_type": "interaction.created", "interaction": map[string]any{"id": "i1"}}},
		{Event: "step.start", JSON: map[string]any{"event_type": "step.start", "interaction": map[string]any{"id": "i1"}, "step": map[string]any{"index": float64(0)}}},
		{Event: "step.delta", JSON: map[string]any{"event_type": "step.delta", "interaction": map[string]any{"id": "i1"}, "step": map[string]any{"index": float64(0)}, "delta": map[string]any{"type": "output_text_delta", "text": "AIQA_OK"}}},
		{Event: "step.stop", JSON: map[string]any{"event_type": "step.stop", "interaction": map[string]any{"id": "i1"}, "step": map[string]any{"index": float64(0)}}},
		{Event: "interaction.completed", JSON: map[string]any{"event_type": "interaction.completed", "interaction": map[string]any{"id": "i1", "usage": map[string]any{"input_tokens": float64(1), "output_tokens": float64(1), "total_tokens": float64(2)}}}},
		{Event: "done", Data: "[DONE]"},
	}}
	result := ValidateInteractionStream(interaction)
	if !result.Complete || result.Text != "AIQA_OK" || len(result.Problems) != 0 {
		t.Fatalf("valid Interaction stream rejected: %+v", result)
	}
	legacy := api.StreamResponse{Events: []api.SSEEvent{
		{JSON: map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": "AIQA_"}}}}}}},
		{JSON: map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": "OK"}}}}}}},
	}}
	legacyResult := ValidateGenerateContentStream(legacy)
	if !legacyResult.Complete || legacyResult.Text != "AIQA_OK" {
		t.Fatalf("valid legacy stream rejected: %+v", legacyResult)
	}
}

func TestValidateInteractionStreamRejectsIllegalIndexLifecycle(t *testing.T) {
	tests := []struct {
		name   string
		events []api.SSEEvent
		want   string
	}{
		{
			name:   "delta before start",
			events: []api.SSEEvent{{Event: "interaction.created"}, {Event: "step.delta", JSON: map[string]any{"step": map[string]any{"index": 0}}}},
			want:   "step.delta before/after step lifecycle",
		},
		{
			name:   "duplicate index start",
			events: []api.SSEEvent{{Event: "interaction.created"}, {Event: "step.start", JSON: map[string]any{"step": map[string]any{"index": 0}}}, {Event: "step.start", JSON: map[string]any{"step": map[string]any{"index": 0}}}},
			want:   "step.start out of sequence",
		},
		{
			name:   "completed before stop",
			events: []api.SSEEvent{{Event: "interaction.created"}, {Event: "step.start", JSON: map[string]any{"step": map[string]any{"index": 0}}}, {Event: "interaction.completed", JSON: map[string]any{"interaction": map[string]any{"usage": map[string]any{"input_tokens": 1}}}}},
			want:   "interaction.completed before step.stop: index:0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validation := ValidateInteractionStream(api.StreamResponse{Events: tt.events})
			if !hasProblem(validation.Problems, tt.want) {
				t.Fatalf("problems=%v, want %q", validation.Problems, tt.want)
			}
		})
	}
}

func TestValidateInteractionStreamAcceptsLegacyStepID(t *testing.T) {
	stream := api.StreamResponse{Events: []api.SSEEvent{
		{Event: "interaction.created"},
		{Event: "step.start", JSON: map[string]any{"step_id": "legacy-1"}},
		{Event: "step.delta", JSON: map[string]any{"step_id": "legacy-1", "text": "AIQA_OK"}},
		{Event: "step.stop", JSON: map[string]any{"step_id": "legacy-1"}},
		{Event: "interaction.completed"},
		{Event: "done", Data: "[DONE]"},
	}}
	validation := ValidateInteractionStream(stream)
	if !validation.Complete || len(validation.Problems) != 0 {
		t.Fatalf("legacy step_id lifecycle rejected: %+v", validation)
	}
}

func TestInteractionValidationCodesAreBounded(t *testing.T) {
	validation := InteractionStreamValidation{Problems: []string{
		"step.start has no step id",
		"event name does not match event_type",
		"provider supplied arbitrary secret text",
	}}
	if got, want := interactionValidationCodes(validation), "event_type_mismatch,sequence_invalid,step_correlation_missing"; got != want {
		t.Fatalf("codes = %q, want %q", got, want)
	}
	if got := interactionValidationCodes(InteractionStreamValidation{}); got != "-" {
		t.Fatalf("valid stream codes = %q, want -", got)
	}
}

func TestLegacyStreamAcceptsFinishOnlyChunk(t *testing.T) {
	stream := api.StreamResponse{StatusCode: 200, Events: []api.SSEEvent{
		{JSON: map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": "AIQA_OK"}}}}}}},
		{JSON: map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP", "usageMetadata": map[string]any{"promptTokenCount": 2, "candidatesTokenCount": 1, "totalTokenCount": 3}}}}},
	}}
	validation := ValidateGenerateContentStream(stream)
	if len(validation.Problems) != 0 || !validation.Complete || !validation.SawFinishReason || !validation.SawUsage {
		t.Fatalf("finish-only legacy chunk was rejected: %+v", validation)
	}
}
