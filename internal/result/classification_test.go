package result

import "testing"

func TestExecutionBlockerClassificationPreservesVerdicts(t *testing.T) {
	for _, code := range []string{"execution.model_access_unavailable", "execution.daily_quota_exhausted"} {
		test := TestResult{ID: "google.generate-content.basic", Status: StatusError, HTTP: HTTPObservation{StatusCode: 404}, Signals: []EvidenceSignal{{Code: code}}}
		if c := ClassifyTestResult(test, "revision"); c == nil || c.Kind != ClassificationExecutionGap {
			t.Fatalf("native=%+v", c)
		}
		suite := ExtensionSuiteResult{SuiteID: "tools", Status: StatusError, HTTP: test.HTTP, Signals: test.Signals}
		if c := ClassifyExtensionResult(suite, "revision"); c == nil || c.Kind != ClassificationExecutionGap {
			t.Fatalf("sdk=%+v", c)
		}
		test.Status = StatusFail
		if c := ClassifyTestResult(test, "revision"); c == nil || c.Kind == ClassificationExecutionGap {
			t.Fatalf("signal hid a genuine failure: %+v", c)
		}
	}
}

func TestTruncatedToolOutputIsExecutionGap(t *testing.T) {
	test := TestResult{ID: "anthropic.tools.strict_schema", Status: StatusError, HTTP: HTTPObservation{StatusCode: 200}, Signals: []EvidenceSignal{{Code: "execution.output_truncated"}}}
	if c := ClassifyTestResult(test, "revision"); c == nil || c.Kind != ClassificationExecutionGap {
		t.Fatalf("truncation classification=%+v", c)
	}
	test.Status = StatusFail
	if c := ClassifyTestResult(test, "revision"); c == nil || c.Kind != ClassificationBehavioralObservation {
		t.Fatalf("complete invalid schema classification=%+v", c)
	}
}

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

func TestConfiguredCredentialRejectionIsExecutionGap(t *testing.T) {
	test := TestResult{ID: "models.list.http", Status: StatusError, HTTP: HTTPObservation{StatusCode: 401}, Signals: []EvidenceSignal{{Code: "execution.credentials_rejected"}}}
	if c := ClassifyTestResult(test, "revision"); c == nil || c.Kind != ClassificationExecutionGap {
		t.Fatalf("classification=%+v, want execution gap", c)
	}
}

func TestTypedSemanticWarningsAreBehavioralObservations(t *testing.T) {
	for _, code := range []string{"behavior.marker_missing", "behavior.marker_mismatch", "behavior.tool_not_called"} {
		t.Run(code, func(t *testing.T) {
			test := TestResult{ID: "google.generate-content.basic", Status: StatusWarn, Signals: []EvidenceSignal{{Code: code}}}
			if c := ClassifyTestResult(test, "revision"); c == nil || c.Kind != ClassificationBehavioralObservation {
				t.Fatalf("native classification=%+v", c)
			}
			suite := ExtensionSuiteResult{ExtensionID: "official-sdk.google-genai", SuiteID: "generate-content", Status: StatusWarn, Signals: test.Signals}
			if c := ClassifyExtensionResult(suite, "revision"); c == nil || c.Kind != ClassificationBehavioralObservation {
				t.Fatalf("extension classification=%+v", c)
			}
		})
	}
}
