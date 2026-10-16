package anthropic

import (
	"testing"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/assessment"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

func TestPrefillContractPreservesUnexpectedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, model, message, errorType string
		status                          result.Status
		http                            int
		required, excluded              bool
	}{
		{"documented rejection", "claude-sonnet-5", "This model does not support assistant message prefill. The conversation must end with a user message.", "invalid_request_error", result.StatusUnsupported, 400, false, true},
		{"unknown model", "other", "This model does not support assistant message prefill. The conversation must end with a user message.", "invalid_request_error", result.StatusUnsupported, 400, false, false},
		{"required profile", "claude-sonnet-5", "This model does not support assistant message prefill. The conversation must end with a user message.", "invalid_request_error", result.StatusUnsupported, 400, true, false},
		{"generic400", "claude-sonnet-5", "Bad request", "invalid_request_error", result.StatusUnsupported, 400, false, false},
		{"wrong envelope", "claude-sonnet-5", "This model does not support assistant message prefill. The conversation must end with a user message.", "authentication_error", result.StatusUnsupported, 400, false, false},
		{"quota", "claude-sonnet-5", "Too many requests", "rate_limit_error", result.StatusError, 429, false, false},
		{"valid continuation", "claude-sonnet-5", "", "", result.StatusPass, 200, false, false},
		{"malformed success", "claude-sonnet-5", "", "", result.StatusFail, 200, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			test := result.TestResult{Status: tc.status, Required: tc.required}
			resp := api.Response{StatusCode: tc.http, JSON: map[string]any{"type": "error", "error": map[string]any{"type": tc.errorType, "message": tc.message}}}
			applyPrefillContract(&test, tc.model, resp)
			if got := test.Applicability == result.ApplicabilityNotApplicable; got != tc.excluded || test.Status != tc.status {
				t.Fatalf("contract changed unexpected evidence: %+v", test)
			}
			if tc.excluded && (len(test.Signals) != 1 || test.Signals[0].Parameters["source"] != sonnet5PrefillContract) {
				t.Fatalf("missing contract provenance: %+v", test)
			}
		})
	}
}

func TestPrefillContractCanonicalExclusionRetainsRawEvidence(t *testing.T) {
	test := result.TestResult{ID: "anthropic.messages.assistant_prefill", Status: result.StatusUnsupported, HTTP: result.HTTPObservation{StatusCode: 400}}
	applyPrefillContract(&test, "claude-sonnet-5", api.Response{StatusCode: 400, JSON: map[string]any{"type": "error", "error": map[string]any{"type": "invalid_request_error", "message": "This model does not support assistant message prefill. The conversation must end with a user message."}}})
	r := result.Report{Models: []result.ModelReport{{ID: "claude-sonnet-5", Compatibility: []result.TestResult{{ID: "anthropic.messages.basic", Required: true, Status: result.StatusPass}, test}}}}
	assessment.ApplyTarget(&r, profile.Profile{}, "anthropic", "anthropic")
	if r.Summary.Assessment.Verdict != "pass" {
		t.Fatalf("canonical verdict=%s", r.Summary.Assessment.Verdict)
	}
	found := false
	for _, requirement := range r.Requirements {
		if requirement.ID == test.ID {
			found = true
			if requirement.Applicability != result.ApplicabilityNotApplicable || len(requirement.Evidence) != 1 || requirement.Evidence[0].Status != result.StatusUnsupported || requirement.Evidence[0].HTTPStatus != 400 {
				t.Fatalf("lost raw rejection: %+v", requirement)
			}
		}
	}
	if !found {
		t.Fatal("excluded requirement disappeared")
	}
}
