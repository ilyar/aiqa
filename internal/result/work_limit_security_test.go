package result

import "testing"

func TestWorkLimitIsExecutionGapAfterSuccessfulHTTP(t *testing.T) {
	for _, status := range []int{0, 200, 429} {
		test := TestResult{ID: "chat.stream", Status: StatusError, HTTP: HTTPObservation{StatusCode: status}, Signals: []EvidenceSignal{{Code: "execution.work_limit"}}}
		if c := ClassifyTestResult(test, "revision"); c == nil || c.Kind != ClassificationExecutionGap {
			t.Fatalf("HTTP %d native classification = %+v", status, c)
		}
		suite := ExtensionSuiteResult{SuiteID: "chat.stream", Status: StatusError, HTTP: test.HTTP, Signals: test.Signals}
		if c := ClassifyExtensionResult(suite, "revision"); c == nil || c.Kind != ClassificationExecutionGap {
			t.Fatalf("HTTP %d extension classification = %+v", status, c)
		}
		test.Status = StatusFail
		if c := ClassifyTestResult(test, "revision"); c == nil || c.Kind == ClassificationExecutionGap {
			t.Fatalf("work-limit signal hid an actual assertion failure: %+v", c)
		}
	}
}
