package result

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReportV3ExecutionSummaryIsOptionalAndDoesNotExposeSensitiveData(t *testing.T) {
	report := ReportV3{Execution: &ExecutionSummary{
		SchemaVersion: "aiqa.execution-summary.v1", Phase: "completed", ElapsedMS: 1,
		GlobalRequestCapacity: 2, ObservedPeakInflight: 1, PlannedJobs: 2, FinishedJobs: 2,
		PacingGroupCount: 1, SchedulerPolicy: "fair-target-admission/v1", EstimateMethod: "remaining-work-simulation/v1",
	}}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"api_key", "prompt", "response", "base_url"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("execution aggregate unexpectedly exposes %q: %s", forbidden, data)
		}
	}
	var decoded struct {
		Execution *ExecutionSummary `json:"execution"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Execution == nil || decoded.Execution.Phase != "completed" || decoded.Execution.GlobalRequestCapacity != 2 {
		t.Fatalf("execution summary did not round trip: %+v", decoded.Execution)
	}
}
