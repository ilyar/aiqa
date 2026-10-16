package googlegenai

import (
	"testing"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/result"
)

func TestBeta2GenerationBudgetAndMarkerClassification(t *testing.T) {
	body := generationBody("fixture", "prompt")
	config, ok := body["generationConfig"].(map[string]any)
	if !ok || config["maxOutputTokens"] != GenerationMaxOutputTokens || GenerationMaxOutputTokens != 256 {
		t.Fatalf("generation budget = %#v, want fixed %d", config, GenerationMaxOutputTokens)
	}
	for _, tc := range []struct {
		name, text string
		reasons    []string
		want       result.Status
		signal     string
	}{
		{"marker", "AIQA_OK", nil, result.StatusPass, ""},
		{"mismatch", "different answer", nil, result.StatusWarn, "behavior.marker_mismatch"},
		{"empty", "", nil, result.StatusWarn, "behavior.marker_missing"},
		{"truncated", "", []string{"MAX_TOKENS"}, result.StatusError, "execution.output_truncated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := result.TestResult{Status: result.StatusPass}
			applyMarkerObservation(&got, tc.text, tc.reasons)
			if got.Status != tc.want {
				t.Fatalf("status=%s, want %s: %+v", got.Status, tc.want, got)
			}
			if tc.signal == "" && len(got.Signals) != 0 {
				t.Fatalf("unexpected signals: %+v", got.Signals)
			}
			if tc.signal != "" && (len(got.Signals) != 1 || got.Signals[0].Code != tc.signal) {
				t.Fatalf("signals=%+v, want %s", got.Signals, tc.signal)
			}
		})
	}
}

func TestBeta2LegacyStreamRejectsMalformedShapeAndPreservesFinishReason(t *testing.T) {
	valid := ValidateGenerateContentStream(api.StreamResponse{Events: []api.SSEEvent{{JSON: map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": "AIQA_OK"}}}, "finishReason": "MAX_TOKENS"}}}}}})
	if len(valid.Problems) != 0 || !valid.Complete || len(valid.FinishReasons) != 1 || valid.FinishReasons[0] != "MAX_TOKENS" {
		t.Fatalf("valid stream=%+v", valid)
	}
	malformed := ValidateGenerateContentStream(api.StreamResponse{Events: []api.SSEEvent{{JSON: map[string]any{"candidates": []any{map[string]any{"content": map[string]any{}}}}}}})
	if len(malformed.Problems) == 0 || malformed.Complete {
		t.Fatalf("malformed stream accepted: %+v", malformed)
	}
	if got := allowlistedFinishReasons([]string{"MAX_TOKENS", "untrusted", "MAX_TOKENS"}); len(got) != 1 || got[0] != "MAX_TOKENS" {
		t.Fatalf("finish allowlist=%v", got)
	}
}

func TestBeta2ToolResponsesSeparateSchemaFromModelChoice(t *testing.T) {
	malformed := map[string]any{}
	if len(ValidateGenerateContent(malformed)) == 0 {
		t.Fatal("malformed tool response did not fail GenerateContent schema validation")
	}
	validNoCall := map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": "I decline"}}}, "finishReason": "STOP"}}}
	if problems := ValidateGenerateContent(validNoCall); len(problems) != 0 {
		t.Fatalf("valid no-call response rejected: %v", problems)
	}
	if _, _, found := generateToolCall(validNoCall); found {
		t.Fatal("ordinary text was misidentified as a function call")
	}
	for _, response := range []map[string]any{
		{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": "AIQA_OK"}}}, "finishReason": "MAX_TOKENS"}}},
		{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": `{"ok":true,"value":"AIQA_OK"}`}}}, "finishReason": "MAX_TOKENS"}}},
	} {
		got := result.TestResult{Status: result.StatusPass}
		if !applyGenerateTruncation(&got, generateFinishReasons(response)) || got.Status != result.StatusError || len(got.Signals) != 1 || got.Signals[0].Code != "execution.output_truncated" {
			t.Fatalf("truncation not retained: %+v", got)
		}
	}
	validTruncated := map[string]any{"candidates": []any{map[string]any{"finishReason": "MAX_TOKENS"}}}
	if problems := ValidateGenerateContent(validTruncated); len(problems) != 0 {
		t.Fatalf("terminal truncated response rejected: %v", problems)
	}
	malformedTruncated := map[string]any{"candidates": []any{map[string]any{"finishReason": "MAX_TOKENS", "content": "not-an-object"}}}
	if len(ValidateGenerateContent(malformedTruncated)) == 0 {
		t.Fatal("wrong content type must remain a schema failure")
	}
}
