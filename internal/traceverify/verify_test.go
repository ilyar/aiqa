package traceverify

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestVerifyValidNativeGraph(t *testing.T) {
	verdict := Verify(strings.NewReader(trace(
		traceRecord(1, "run.start", "run", "", "run-op"),
		traceRecord(2, "target.start", "target", "run", "target-op"),
		traceRecord(3, "model.start", "model", "target", "model-op"),
		traceRecord(4, "suite.start", "suite", "model", "suite-op"),
		traceRecord(5, "probe.start", "probe", "suite", "probe-op"),
		traceRecord(6, "http.logical_request.start", "logical", "probe", "logical-op"),
		traceRecord(7, "http.attempt.start", "attempt", "logical", "attempt-op"),
		traceRecord(8, "http.attempt.end", "attempt", "logical", "attempt-op"),
		traceRecord(9, "http.logical_request.end", "logical", "probe", "logical-op"),
		traceRecord(10, "probe.validator", "probe", "suite", "probe-op"),
		traceRecord(11, "probe.end", "probe", "suite", "probe-op"),
		traceRecord(12, "suite.end", "suite", "model", "suite-op"),
		traceRecord(13, "model.end", "model", "target", "model-op"),
		traceRecord(14, "target.end", "target", "run", "target-op"),
		traceRecord(15, "run.end", "run", "", "run-op"),
	)))
	if verdict.Status != Pass || len(verdict.Violations) != 0 {
		t.Fatalf("verdict=%+v", verdict)
	}
	if verdict.CheckedHTTPAttempts != 1 || verdict.CheckedSpans != 7 {
		t.Fatalf("unexpected counts: %+v", verdict)
	}
	if len(verdict.Coverage) != 1 || verdict.Coverage[0].Executor != "native" {
		t.Fatalf("coverage=%+v", verdict.Coverage)
	}
}

func TestVerifyRejectsOrphanHTTPAttempt(t *testing.T) {
	verdict := Verify(strings.NewReader(trace(
		traceRecord(1, "run.start", "run", "", "run-op"),
		traceRecord(2, "http.attempt.start", "attempt", "run", "attempt-op"),
		traceRecord(3, "http.attempt.end", "attempt", "run", "attempt-op"),
		traceRecord(4, "run.end", "run", "", "run-op"),
	)))
	wantViolation(t, verdict, "http_attempt_without_probe")
	wantViolation(t, verdict, "http_attempt_without_logical_request")
}

func TestVerifyRejectsIncompleteSubjectChainAndDoubleProbeOwnership(t *testing.T) {
	// A superficially closed request graph is not enough: model and suite are
	// mandatory ownership layers between target and a report probe.
	verdict := Verify(strings.NewReader(trace(
		traceRecord(1, "run.start", "run", "", "run-op"),
		traceRecord(2, "target.start", "target", "run", "target-op"),
		traceRecord(3, "probe.start", "probe", "target", "probe-op"),
		traceRecord(4, "http.logical_request.start", "logical", "probe", "logical-op"),
		traceRecord(5, "http.attempt.start", "attempt", "logical", "attempt-op"),
		traceRecord(6, "http.attempt.end", "attempt", "logical", "attempt-op"),
		traceRecord(7, "http.logical_request.end", "logical", "probe", "logical-op"),
		traceRecord(8, "probe.end", "probe", "target", "probe-op"),
		traceRecord(9, "target.end", "target", "run", "target-op"),
		traceRecord(10, "run.end", "run", "", "run-op"),
	)))
	wantViolation(t, verdict, "execution_hierarchy_invalid")

	verdict = Verify(strings.NewReader(trace(
		traceRecord(1, "run.start", "run", "", "run-op"),
		traceRecord(2, "target.start", "target", "run", "target-op"),
		traceRecord(3, "model.start", "model", "target", "model-op"),
		traceRecord(4, "suite.start", "suite", "model", "suite-op"),
		traceRecord(5, "probe.start", "probe-a", "suite", "probe-a-op"),
		traceRecord(6, "sdk.probe.start", "probe-b", "probe-a", "probe-b-op"),
		traceRecord(7, "sdk.logical_request.start", "logical", "probe-b", "logical-op"),
		traceRecord(8, "sdk.http_attempt.start", "attempt", "logical", "attempt-op"),
		traceRecord(9, "sdk.http_attempt.end", "attempt", "logical", "attempt-op"),
		traceRecord(10, "sdk.logical_request.end", "logical", "probe-b", "logical-op"),
		traceRecord(11, "sdk.probe.end", "probe-b", "probe-a", "probe-b-op"),
		traceRecord(12, "probe.end", "probe-a", "suite", "probe-a-op"),
		traceRecord(13, "suite.end", "suite", "model", "suite-op"),
		traceRecord(14, "model.end", "model", "target", "model-op"),
		traceRecord(15, "target.end", "target", "run", "target-op"),
		traceRecord(16, "run.end", "run", "", "run-op"),
	)))
	wantViolation(t, verdict, "http_attempt_multiple_probe_owners")
}

func TestVerifyRejectsDuplicateAndMissingEnds(t *testing.T) {
	verdict := Verify(strings.NewReader(trace(
		traceRecord(1, "run.start", "run", "", "run-op"),
		traceRecord(2, "run.end", "run", "", "run-op"),
		traceRecord(3, "run.end", "run", "", "run-op"),
		traceRecord(4, "target.start", "target", "", "target-op"),
	)))
	wantViolation(t, verdict, "duplicate_span_end")
	wantViolation(t, verdict, "missing_span_end")
}

func TestVerifyRejectsParentOrderAndLateValidator(t *testing.T) {
	verdict := Verify(strings.NewReader(trace(
		traceRecord(1, "run.start", "run", "", "run-op"),
		traceRecord(2, "probe.start", "probe", "run", "probe-op"),
		traceRecord(3, "probe.end", "probe", "run", "probe-op"),
		traceRecord(4, "probe.validator", "probe", "run", "probe-op"),
		traceRecord(5, "suite.start", "suite", "run", "suite-op"),
		traceRecord(6, "run.end", "run", "", "run-op"),
		traceRecord(7, "suite.end", "suite", "run", "suite-op"),
	)))
	wantViolation(t, verdict, "validator_after_probe_end")
	wantViolation(t, verdict, "parent_order_invalid")
}

func TestVerifyPreservesIndependentConcurrentTargetTrees(t *testing.T) {
	// Starts and ends are deliberately interleaved as they are with
	// --concurrency > 1. Each branch must retain its own target owner.
	records := []map[string]any{
		traceRecord(1, "run.start", "run", "", "run-op"),
		traceRecord(2, "target.start", "target-a", "run", "target-a-op"),
		traceRecord(3, "target.start", "target-b", "run", "target-b-op"),
		traceRecord(4, "model.start", "model-a", "target-a", "model-a-op"),
		traceRecord(5, "model.start", "model-b", "target-b", "model-b-op"),
		traceRecord(6, "suite.start", "suite-a", "model-a", "suite-a-op"),
		traceRecord(7, "suite.start", "suite-b", "model-b", "suite-b-op"),
		traceRecord(8, "probe.start", "probe-a", "suite-a", "probe-a-op"),
		traceRecord(9, "probe.start", "probe-b", "suite-b", "probe-b-op"),
		traceRecord(10, "http.logical_request.start", "logical-a", "probe-a", "logical-a-op"),
		traceRecord(11, "http.logical_request.start", "logical-b", "probe-b", "logical-b-op"),
		traceRecord(12, "http.attempt.start", "attempt-a", "logical-a", "attempt-a-op"),
		traceRecord(13, "http.attempt.start", "attempt-b", "logical-b", "attempt-b-op"),
		traceRecord(14, "http.attempt.end", "attempt-b", "logical-b", "attempt-b-op"),
		traceRecord(15, "http.attempt.end", "attempt-a", "logical-a", "attempt-a-op"),
		traceRecord(16, "http.logical_request.end", "logical-a", "probe-a", "logical-a-op"),
		traceRecord(17, "http.logical_request.end", "logical-b", "probe-b", "logical-b-op"),
		traceRecord(18, "probe.validator", "probe-a", "suite-a", "probe-a-op"),
		traceRecord(19, "probe.validator", "probe-b", "suite-b", "probe-b-op"),
		traceRecord(20, "probe.end", "probe-a", "suite-a", "probe-a-op"),
		traceRecord(21, "probe.end", "probe-b", "suite-b", "probe-b-op"),
		traceRecord(22, "suite.end", "suite-a", "model-a", "suite-a-op"),
		traceRecord(23, "suite.end", "suite-b", "model-b", "suite-b-op"),
		traceRecord(24, "model.end", "model-a", "target-a", "model-a-op"),
		traceRecord(25, "model.end", "model-b", "target-b", "model-b-op"),
		traceRecord(26, "target.end", "target-a", "run", "target-a-op"),
		traceRecord(27, "target.end", "target-b", "run", "target-b-op"),
		traceRecord(28, "run.end", "run", "", "run-op"),
	}
	for _, record := range records {
		span, _ := record["span_id"].(string)
		if strings.HasSuffix(span, "-a") || span == "target-a" {
			record["target"], record["protocol"] = "target-a", "openai"
		} else if strings.HasSuffix(span, "-b") || span == "target-b" {
			record["target"], record["protocol"] = "target-b", "anthropic"
		}
	}
	verdict := Verify(strings.NewReader(trace(records...)))
	if verdict.Status != Pass || verdict.CheckedHTTPAttempts != 2 {
		t.Fatalf("concurrent verdict=%+v", verdict)
	}
	seen := map[string]bool{}
	for _, item := range verdict.Coverage {
		if item.Executor == "native" && item.Status == "complete" {
			seen[item.Target] = true
		}
	}
	if !seen["target-a"] || !seen["target-b"] {
		t.Fatalf("concurrent coverage=%+v", verdict.Coverage)
	}
}

func TestVerifyRejectsCrossTargetParentage(t *testing.T) {
	parent := traceRecord(2, "target.start", "target-a", "run", "target-a-op")
	parent["target"] = "target-a"
	child := traceRecord(3, "model.start", "model-b", "target-a", "model-b-op")
	child["target"] = "target-b"
	verdict := Verify(strings.NewReader(trace(
		traceRecord(1, "run.start", "run", "", "run-op"),
		parent, child,
	)))
	wantViolation(t, verdict, "target_scope_mismatch")
}

func TestVerifyMalformedJSONIsInconclusive(t *testing.T) {
	verdict := Verify(strings.NewReader("{not-json}\n"))
	if verdict.Status != Inconclusive {
		t.Fatalf("status=%q, want inconclusive", verdict.Status)
	}
	wantViolation(t, verdict, "malformed_json")
}

func TestVerifyRejectsMissingMandatoryField(t *testing.T) {
	bad := traceRecord(1, "run.start", "run", "", "run-op")
	delete(bad, "operation_id")
	verdict := Verify(strings.NewReader(trace(bad)))
	if verdict.Status != Fail {
		t.Fatalf("status=%q, want fail", verdict.Status)
	}
	wantViolation(t, verdict, "mandatory_field_missing")
}

func TestVerifyRejectsSensitiveTraceFields(t *testing.T) {
	record := traceRecord(1, "run.start", "run", "", "run-op")
	record["api_key"] = "do-not-retain"
	verdict := Verify(strings.NewReader(trace(record)))
	wantViolation(t, verdict, "sensitive_field_detected")
}

func TestVerifyRejectsNestedSensitiveOrUnknownTraceField(t *testing.T) {
	record := traceRecord(1, "run.start", "run", "", "run-op")
	record["message"] = map[string]any{"data": map[string]any{"prompt": "secret"}}
	verdict := Verify(strings.NewReader(trace(record)))
	wantViolation(t, verdict, "sensitive_field_detected")

	record = traceRecord(1, "run.start", "run", "", "run-op")
	record["unreviewed_field"] = "value"
	verdict = Verify(strings.NewReader(trace(record)))
	wantViolation(t, verdict, "sensitive_field_detected")
}

func TestVerifyCoverageDistinguishesNotSelectedAndSelectedWithoutEvidence(t *testing.T) {
	plan := traceRecord(2, "target.plan", "target", "run", "target-op")
	plan["target"], plan["protocol"] = "fixture", "openai"
	plan["native_selected"], plan["sdk_selected"], plan["extension_selected"] = true, false, false
	verdict := Verify(strings.NewReader(trace(
		traceRecord(1, "run.start", "run", "", "run-op"), plan,
		traceRecord(3, "target.end", "target", "run", "target-op"), traceRecord(4, "run.end", "run", "", "run-op"),
	)))
	wantViolation(t, verdict, "selected_executor_unobserved")
	if len(verdict.Coverage) != 3 {
		t.Fatalf("coverage=%+v", verdict.Coverage)
	}
	for _, item := range verdict.Coverage {
		if item.Executor == "native" && item.Status != "missing" {
			t.Fatalf("selected but unobserved coverage=%+v", verdict.Coverage)
		}
		if item.Executor != "native" && item.Status != "not_selected" {
			t.Fatalf("unselected coverage=%+v", verdict.Coverage)
		}
	}
}

func TestVerifyRejectsUnsafeDerivedReference(t *testing.T) {
	record := traceRecord(1, "run.start", "run", "", "run-op")
	record["derived_from"] = "https://provider.example/secret"
	verdict := Verify(strings.NewReader(trace(record)))
	wantViolation(t, verdict, "derived_reference_invalid")
}

func TestVerifyEmptyTraceIsNotRequested(t *testing.T) {
	if verdict := Verify(strings.NewReader("\n")); verdict.Status != NotRequested {
		t.Fatalf("verdict=%+v", verdict)
	}
}

func TestVerifyExecutableExtensionIsInconclusiveWithoutChildTrace(t *testing.T) {
	invocation := traceRecord(2, "extension.invocation.start", "invocation", "run", "ext-1")
	invocation["source"] = "executable"
	verdict := Verify(strings.NewReader(trace(
		traceRecord(1, "run.start", "run", "", "run-1"),
		invocation,
		traceRecord(3, "extension.invocation.end", "invocation", "run", "ext-1"),
		traceRecord(4, "run.end", "run", "", "run-1"),
	)))
	if verdict.Status != Inconclusive {
		t.Fatalf("verdict = %+v, want inconclusive extension coverage", verdict)
	}
	wantViolation(t, verdict, "extension_trace_unavailable")
}

func TestVerifyExtensionTracePassesWithHostMappedHTTPGraph(t *testing.T) {
	records := []map[string]any{
		traceRecord(1, "run.start", "run", "", "run-op"),
		traceRecord(2, "target.start", "target", "run", "target-op"),
		traceRecord(3, "model.start", "model", "target", "model-op"),
		traceRecord(4, "extension.invocation.start", "invocation", "model", "invocation-op"),
		traceRecord(5, "suite.start", "suite", "invocation", "suite-op"),
		traceRecord(6, "probe.start", "probe", "suite", "probe-op"),
		traceRecord(7, "sdk.logical_request.start", "logical", "probe", "logical-op"),
		traceRecord(8, "sdk.http_attempt.start", "attempt", "logical", "attempt-op"),
		traceRecord(9, "sdk.http_attempt.end", "attempt", "logical", "attempt-op"),
		traceRecord(10, "sdk.logical_request.end", "logical", "probe", "logical-op"),
		traceRecord(11, "probe.validator", "probe", "suite", "probe-op"),
		traceRecord(12, "probe.end", "probe", "suite", "probe-op"),
		traceRecord(13, "suite.end", "suite", "invocation", "suite-op"),
		traceRecord(14, "extension.invocation.end", "invocation", "model", "invocation-op"),
		traceRecord(15, "model.end", "model", "target", "model-op"),
		traceRecord(16, "target.end", "target", "run", "target-op"),
		traceRecord(17, "run.end", "run", "", "run-op"),
	}
	for _, index := range []int{3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13} {
		records[index]["source"] = "executable"
	}
	verdict := Verify(strings.NewReader(trace(records...)))
	if verdict.Status != Pass {
		t.Fatalf("verdict = %+v, want executable trace pass", verdict)
	}
	if len(verdict.Coverage) != 1 || verdict.Coverage[0].Executor != "extension" {
		t.Fatalf("coverage = %+v, want executable coverage", verdict.Coverage)
	}
}

func TestVerdictSerializesWithoutPayload(t *testing.T) {
	verdict := Verify(strings.NewReader(trace(traceRecord(1, "run.start", "run", "", "run-op"))))
	encoded, err := json.Marshal(verdict)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("payload")) {
		t.Fatalf("verdict exposed payload: %s", encoded)
	}
}

func TestVerdictNeverCopiesAbsolutePathOrCredentialLikeTraceValue(t *testing.T) {
	record := traceRecord(1, "run.start", "run", "", "run-op")
	record["path"] = "/private/operator/trace.log"
	record["message"] = "Bearer fixture-secret"
	verdict := Verify(strings.NewReader(trace(record)))
	encoded, err := json.Marshal(verdict)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"/private/operator/trace.log", "fixture-secret", "Bearer"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("verdict leaked %q: %s", forbidden, encoded)
		}
	}
	wantViolation(t, verdict, "sensitive_field_detected")
}

func wantViolation(t *testing.T, verdict Verdict, code string) {
	t.Helper()
	if verdict.Status != Fail && verdict.Status != Inconclusive {
		t.Fatalf("status=%q, want failure; verdict=%+v", verdict.Status, verdict)
	}
	for _, violation := range verdict.Violations {
		if violation.Code == code {
			return
		}
	}
	t.Fatalf("missing violation %q in %+v", code, verdict.Violations)
}

func trace(records ...map[string]any) string {
	var builder strings.Builder
	for _, record := range records {
		encoded, _ := json.Marshal(record)
		builder.Write(encoded)
		builder.WriteByte('\n')
	}
	return builder.String()
}

func traceRecord(seq int, event, spanID, parentID, operationID string) map[string]any {
	record := map[string]any{
		"schema_version": TraceSchemaVersion,
		"time":           "2026-09-06T12:00:00Z",
		"seq":            seq,
		"level":          "TRACE",
		"event":          event,
		"trace_id":       "11111111111111111111111111111111",
		"span_id":        spanID,
		"parent_span_id": parentID,
		"operation_id":   operationID,
	}
	if event == "probe.start" {
		record["probe"] = "fixture.probe"
	}
	return record
}
