package googlegenai

import (
	"errors"
	"testing"

	genai "google.golang.org/genai"
)

func TestBeta2SDKGenerationOutcomesAreTyped(t *testing.T) {
	marker := &generationSemanticWarning{stream: true}
	var decodedMarker *generationSemanticWarning
	if !errors.As(marker, &decodedMarker) || decodedMarker.empty || decodedMarker.Error() == "" {
		t.Fatalf("semantic warning lost type: %#v", decodedMarker)
	}
	empty := &generationSemanticWarning{empty: true}
	if !empty.empty || empty.Error() == marker.Error() {
		t.Fatalf("empty response warning is not distinct: %q", empty.Error())
	}
	truncated := &generationTruncated{stream: true}
	var decodedTruncated *generationTruncated
	if !errors.As(truncated, &decodedTruncated) || !decodedTruncated.stream {
		t.Fatalf("truncation lost type: %#v", decodedTruncated)
	}
	if !generationWasTruncated(&genai.GenerateContentResponse{Candidates: []*genai.Candidate{{FinishReason: genai.FinishReasonMaxTokens}}}) {
		t.Fatal("MAX_TOKENS was not detected")
	}
	if generationWasTruncated(&genai.GenerateContentResponse{Candidates: []*genai.Candidate{{FinishReason: genai.FinishReasonStop}}}) {
		t.Fatal("STOP was treated as truncation")
	}
	if len((&genai.GenerateContentResponse{}).Candidates) != 0 {
		t.Fatal("empty SDK response unexpectedly has candidates")
	}
}
