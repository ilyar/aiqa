package googlegenai

import (
	"testing"

	"github.com/ilyar/aiqa/internal/extsuite"
	"github.com/ilyar/aiqa/internal/result"
	genai "google.golang.org/genai"
)

func TestSDKModelAccessBlockerValueAndPointer(t *testing.T) {
	apiErr := genai.APIError{Code: 404, Status: "NOT_FOUND", Message: "This model models/gemini-2.5-flash is no longer available to new users. Please update your code."}
	for _, err := range []error{apiErr, &apiErr} {
		event := extsuite.SuiteEvent{SuiteID: "generate-content"}
		applyError(&event, err)
		if event.Outcome != result.ExtensionOutcomeExecutionError || event.HTTP.StatusCode != 404 || event.ErrorKind != "" || event.Error != "" || len(event.Signals) != 1 || event.Signals[0].Code != "execution.model_access_unavailable" {
			t.Fatalf("event=%+v", event)
		}
	}
	apiErr.Message = "Model not found"
	event := extsuite.SuiteEvent{SuiteID: "generate-content"}
	applyError(&event, apiErr)
	if event.Outcome != result.ExtensionOutcomeRequestRejected || event.ErrorKind != "capability_rejection" {
		t.Fatalf("generic404 was hidden: %+v", event)
	}
}

func TestSDKDailyQuotaKeepsExecutionError(t *testing.T) {
	event := extsuite.SuiteEvent{SuiteID: "tools"}
	applyError(&event, genai.APIError{Code: 429, Status: "RESOURCE_EXHAUSTED", Details: []map[string]any{{
		"@type": "type.googleapis.com/google.rpc.QuotaFailure", "violations": []any{map[string]any{"quotaId": "GenerateRequestsPerDayPerProjectPerModel-FreeTier", "quotaMetric": "generativelanguage.googleapis.com/generate_content_free_tier_requests"}},
	}}})
	if event.Outcome != result.ExtensionOutcomeExecutionError || len(event.Signals) != 1 || event.Signals[0].Code != "execution.daily_quota_exhausted" {
		t.Fatalf("event=%+v", event)
	}
}
