package openai

import (
	"testing"

	"github.com/ilyar/aiqa/internal/result"
)

func TestRepresentativeModelSkipsEarlierInapplicableModels(t *testing.T) {
	models := []string{"aaa-image", "bbb-unknown", "ccc-chat"}
	plans := [][]result.ModuleAssessment{
		{{ID: "chat-completions", Applicability: result.ApplicabilityNotApplicable}},
		{{ID: "chat-completions", Applicability: result.ApplicabilityUnknown}},
		{{ID: "chat-completions", Applicability: result.ApplicabilityApplicable, Execution: result.ExecutionExecuted}},
	}
	if got := representativeModel(models, plans, "chat-completions"); got != "ccc-chat" {
		t.Fatalf("representative model=%q, want ccc-chat", got)
	}
}
