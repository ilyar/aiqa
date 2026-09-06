package result

import "testing"

func TestClassifyTestResultTreatsExactMarkerAsBehavioralObservation(t *testing.T) {
	classification := ClassifyTestResult(TestResult{ID: "google.generate-content.basic", Status: StatusFail, Summary: "response did not contain AIQA_OK", HTTP: HTTPObservation{StatusCode: 200}}, "2026-09-06")
	if classification == nil || classification.Kind != ClassificationBehavioralObservation {
		t.Fatalf("classification = %+v, want behavioral observation", classification)
	}
}

func TestClassifyTestResultTreatsBodylessErrorShapeAsInsufficientEvidence(t *testing.T) {
	classification := ClassifyTestResult(TestResult{ID: "google.error.invalid_model", Status: StatusFail, HTTP: HTTPObservation{StatusCode: 404}}, "2026-09-06")
	if classification == nil || classification.Kind != ClassificationInsufficientEvidence {
		t.Fatalf("classification = %+v, want insufficient evidence", classification)
	}
}

func TestClassifyResultTreatsNoHTTPTimeoutAsExecutionGap(t *testing.T) {
	classification := ClassifyResult(StatusError, "official-sdk.openai.chat", "", "2026-09-06", HTTPObservation{})
	if classification == nil || classification.Kind != ClassificationExecutionGap {
		t.Fatalf("classification = %+v, want execution gap", classification)
	}
}
