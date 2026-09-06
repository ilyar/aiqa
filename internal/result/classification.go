package result

import "strings"

const (
	ClassificationProviderNonconformance = "provider_nonconformance"
	ClassificationCheckerDefect          = "checker_defect"
	ClassificationBehavioralObservation  = "behavioral_observation"
	ClassificationExecutionGap           = "execution_gap"
	ClassificationUnsupportedCapability  = "unsupported_capability"
	ClassificationInsufficientEvidence   = "insufficient_evidence"
)

// ClassifyResult returns evidence metadata for a non-pass observation. The
// canonical Status deliberately remains untouched: assessment owns verdicts.
func ClassifyResult(status Status, id, requirementID, profileRevision string, http HTTPObservation) *Classification {
	return classifyResult(status, id, requirementID, profileRevision, http, "")
}

func ClassifyTestResult(test TestResult, profileRevision string) *Classification {
	return classifyResult(test.Status, test.ID, test.RequirementID, profileRevision, test.HTTP, test.Summary+" "+test.Expected+" "+test.Observation)
}

func classifyResult(status Status, id, requirementID, profileRevision string, http HTTPObservation, semanticEvidence string) *Classification {
	if status == StatusPass || status == StatusSkip {
		return nil
	}
	c := &Classification{RequirementID: requirementID, ProfileRevision: profileRevision}
	if c.RequirementID == "" {
		c.RequirementID = id
	}
	if http.TraceID != "" {
		c.OperationIDs = []string{http.TraceID}
	}
	switch {
	case strings.Contains(id, "checker."):
		c.Kind, c.Reason = ClassificationCheckerDefect, "checker rejected an explicitly marked compatibility fixture"
	case status == StatusUnsupported:
		c.Kind, c.Reason = ClassificationUnsupportedCapability, "optional capability was explicitly rejected"
	case status == StatusError && http.StatusCode == 0:
		c.Kind, c.Reason = ClassificationExecutionGap, "no terminal HTTP response was observed"
	case status == StatusError && (http.StatusCode == 429 || http.StatusCode >= 500):
		c.Kind, c.Reason = ClassificationExecutionGap, "transient provider or rate-limit response is not conformance evidence"
	case isBehavioralAssertion(id) || isBehavioralEvidence(semanticEvidence):
		c.Kind, c.Reason = ClassificationBehavioralObservation, "semantic model assertion is not provider nonconformance by itself"
	case isErrorShape(id) && http.StatusCode >= 400 && http.Snippet == "":
		c.Kind, c.Reason = ClassificationInsufficientEvidence, "HTTP error has no safe response shape evidence"
	default:
		c.Kind, c.Reason = ClassificationProviderNonconformance, "applicable checker requirement did not pass"
	}
	return c
}

func isBehavioralAssertion(id string) bool {
	return strings.Contains(id, "output_text") || strings.Contains(id, "exact_marker") ||
		strings.Contains(id, "tools.") || strings.Contains(id, "tool_") ||
		strings.Contains(id, "json_schema") || strings.Contains(id, "structured")
}

func isErrorShape(id string) bool {
	return strings.Contains(id, ".error.") || strings.Contains(id, "invalid_")
}

func isBehavioralEvidence(value string) bool {
	value = strings.ToLower(value)
	return strings.Contains(value, "aiqa_ok") || strings.Contains(value, "exact marker") ||
		strings.Contains(value, "function call") || strings.Contains(value, "tool call") ||
		strings.Contains(value, "local json schema")
}
