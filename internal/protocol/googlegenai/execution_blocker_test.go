package googlegenai

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/assessment"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

func TestModelAccessBlockerDoesNotHideGenericFailures(t *testing.T) {
	for _, tc := range []struct {
		name, message, statusName string
		http                      int
		blocked                   bool
	}{
		{"access", "This model models/gemini-2.5-flash is no longer available to new users. Please update your code.", "NOT_FOUND", 404, true},
		{"generic404", "Model not found", "NOT_FOUND", 404, false},
		{"wrong status", "This model models/gemini-2.5-flash is no longer available to new users.", "INVALID_ARGUMENT", 404, false},
		{"wrong http", "This model models/gemini-2.5-flash is no longer available to new users.", "NOT_FOUND", 400, false},
		{"missing model", "This model models/ is no longer available to new users.", "NOT_FOUND", 404, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := api.Response{StatusCode: tc.http, JSON: map[string]any{"error": map[string]any{"status": tc.statusName, "message": tc.message}}}
			for _, optional := range []bool{false, true} {
				test := result.TestResult{HTTP: result.HTTPObservation{StatusCode: tc.http}}
				setHTTPStatus(&test, resp, nil, optional)
				if (test.Status == result.StatusError) != tc.blocked {
					t.Fatalf("optional=%v result=%+v", optional, test)
				}
				if tc.blocked && (len(test.Signals) != 1 || test.Signals[0].Code != "execution.model_access_unavailable") {
					t.Fatalf("missing typed access evidence: %+v", test)
				}
			}
			body, _ := json.Marshal(resp.JSON)
			streamTest := result.TestResult{}
			setStreamError(&streamTest, errors.New("HTTP error"), tc.http, false)
			applyStreamExecutionBlocker(&streamTest, api.StreamResponse{StatusCode: tc.http, RawSnippet: string(body)})
			if (streamTest.Status == result.StatusError) != tc.blocked {
				t.Fatalf("stream=%+v", streamTest)
			}
			negative := result.TestResult{}
			setExpectedError(&negative, resp, nil)
			if tc.blocked && negative.Status != result.StatusError {
				t.Fatalf("unavailable model cannot satisfy negative probe: %+v", negative)
			}
		})
	}
}

func TestDailyQuotaAddsSafeTypedEvidence(t *testing.T) {
	var body map[string]any
	if err := json.Unmarshal([]byte(`{"error":{"status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.QuotaFailure","violations":[{"quotaId":"GenerateRequestsPerDayPerProjectPerModel-FreeTier","quotaMetric":"generativelanguage.googleapis.com/generate_content_free_tier_requests"}]}]}}`), &body); err != nil {
		t.Fatal(err)
	}
	test := result.TestResult{}
	setHTTPStatus(&test, api.Response{StatusCode: 429, JSON: body}, nil, false)
	if test.Status != result.StatusError || len(test.Signals) != 1 || test.Signals[0].Code != "execution.daily_quota_exhausted" || test.Error != "" {
		t.Fatalf("quota=%+v", test)
	}
	applyStreamExecutionBlocker(&test, api.StreamResponse{StatusCode: 200, RawSnippet: `{}`})
	applyStreamExecutionBlocker(&test, api.StreamResponse{StatusCode: 404, RawSnippet: `{`})
	if len(test.Signals) != 1 {
		t.Fatal("malformed/success stream must not add blocker evidence")
	}
}

func TestModelAccessBlockerCanonicalVerdictRemainsInconclusive(t *testing.T) {
	test := result.TestResult{ID: "google.generate-content.basic", Required: true, HTTP: result.HTTPObservation{StatusCode: 404}}
	setHTTPStatus(&test, api.Response{StatusCode: 404, JSON: map[string]any{"error": map[string]any{"status": "NOT_FOUND", "message": "This model models/gemini-2.5-flash is no longer available to new users."}}}, nil, false)
	r := result.Report{Models: []result.ModelReport{{ID: "gemini-2.5-flash", Compatibility: []result.TestResult{test}}}}
	assessment.ApplyTarget(&r, profile.Profile{}, "google-genai", "google-genai")
	if r.Summary.Assessment.Verdict != "inconclusive" || r.Summary.Assessment.Core.Error != 1 || r.Summary.Assessment.Core.Fail != 0 {
		t.Fatalf("access gap was scored as conformance: %+v", r.Summary.Assessment)
	}
}
