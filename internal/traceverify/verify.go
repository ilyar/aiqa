// Package traceverify validates AIQA forensic JSONL traces without contacting a
// provider. It intentionally retains only correlation identifiers and counts;
// callers must not use it as a mechanism for recovering diagnostic payloads.
package traceverify

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ilyar/aiqa/internal/logging"
)

const (
	TraceSchemaVersion   = "aiqa.trace.v1"
	VerdictSchemaVersion = "aiqa.trace-verdict.v1"
)

type Status string

const (
	Pass         Status = "pass"
	Fail         Status = "fail"
	Inconclusive Status = "inconclusive"
	NotRequested Status = "not_requested"
)

// Violation has only stable codes and safe local correlation identifiers.
type Violation struct {
	Code        string `json:"code"`
	Severity    string `json:"severity"`
	Seq         uint64 `json:"seq,omitempty"`
	SpanID      string `json:"span_id,omitempty"`
	OperationID string `json:"operation_id,omitempty"`
	Remediation string `json:"remediation"`
}

// Coverage is deliberately aggregate: no URLs, provider request IDs or payload
// data are copied into the verdict.
type Coverage struct {
	Target   string `json:"target"`
	Protocol string `json:"protocol"`
	Executor string `json:"executor"`
	Status   string `json:"status"`
	Count    int    `json:"count"`
}

// Verdict is serializable as aiqa.trace-verdict.v1.
type Verdict struct {
	SchemaVersion       string      `json:"schema_version"`
	TraceSchemaVersion  string      `json:"trace_schema_version"`
	RunID               string      `json:"run_id,omitempty"`
	Status              Status      `json:"status"`
	CheckedRecords      int         `json:"checked_records"`
	CheckedSpans        int         `json:"checked_spans"`
	CheckedHTTPAttempts int         `json:"checked_http_attempts"`
	Coverage            []Coverage  `json:"coverage"`
	Violations          []Violation `json:"violations"`
}

type record struct {
	SchemaVersion string
	Time          string
	Seq           uint64
	Level         string
	Event         string
	TraceID       string
	SpanID        string
	ParentSpanID  string
	OperationID   string
	Fields        map[string]json.RawMessage
}

type span struct {
	id, operation, parent, traceID, operationID, source string
	target, protocol                                    string
	namedProbe                                          bool
	startSeq                                            uint64
	endSeq                                              uint64
}

// Verify reads a trace once and returns a deterministic, offline verdict.
// Empty input denotes a forensic trace that was not requested. Syntax errors
// are inconclusive because no complete trace could be examined.
func Verify(input io.Reader) Verdict {
	v := Verdict{SchemaVersion: VerdictSchemaVersion, TraceSchemaVersion: TraceSchemaVersion, Status: Pass, Coverage: []Coverage{}, Violations: []Violation{}}
	spans := map[string]*span{}
	closed := map[string]uint64{}
	lastSeq := uint64(0)
	seenAny := false
	runID := ""
	runSpans := []string{}
	executors := map[coverageKey]int{}
	planned := map[coverageKey]bool{}
	executableInvocations := []string{}

	add := func(code, severity string, r record, remediation string) {
		v.Violations = append(v.Violations, Violation{Code: code, Severity: severity, Seq: r.Seq, SpanID: r.SpanID, OperationID: r.OperationID, Remediation: remediation})
	}

	scanner := bufio.NewScanner(input)
	// Structured records may include many safe fields. The scanner limit only
	// prevents an attacker-controlled line from causing an unbounded allocation.
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for line := 1; scanner.Scan(); line++ {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		seenAny = true
		r, err := parseRecord([]byte(scanner.Text()))
		if err != nil {
			v.Violations = append(v.Violations, Violation{Code: "malformed_json", Severity: "error", Remediation: "write complete JSONL records before generating a verdict"})
			v.Status = Inconclusive
			continue
		}
		v.CheckedRecords++
		if r.SchemaVersion != TraceSchemaVersion {
			add("trace_schema_version_invalid", "error", r, "emit aiqa.trace.v1 records or select a compatible verifier")
		}
		operation, phase := eventOperation(r.Event)
		if r.Time == "" || r.Level == "" || r.Event == "" || !hasField(r.Fields, "operation_id") {
			add("mandatory_field_missing", "error", r, "emit every required forensic correlation field")
		}
		if phase != "" && r.OperationID == "" {
			add("mandatory_field_missing", "error", r, "emit an operation_id for every lifecycle record")
		}
		if r.Seq == 0 || r.Seq <= lastSeq {
			add("seq_not_monotonic", "error", r, "assign strictly increasing non-zero sequence numbers")
		} else {
			lastSeq = r.Seq
		}
		if !hasOnlyAllowedFields(r.Fields) || containsSensitiveData(r.Fields) {
			add("sensitive_field_detected", "error", r, "remove sensitive diagnostic fields before writing the trace")
		}
		if derived := stringField(r.Fields, "derived_from"); derived != "" && !safeIdentifier(derived) {
			add("derived_reference_invalid", "error", r, "use a stable local requirement identifier for derived_from")
		}
		if r.Event == "run.start" {
			if value := stringField(r.Fields, "run_id"); value != "" {
				runID = value
			}
		}
		if r.Event == "target.plan" {
			target, protocol := stringField(r.Fields, "target"), stringField(r.Fields, "protocol")
			for executor, selected := range map[string]bool{"native": boolField(r.Fields, "native_selected"), "sdk": boolField(r.Fields, "sdk_selected"), "extension": boolField(r.Fields, "extension_selected")} {
				planned[coverageKey{target: target, protocol: protocol, executor: executor}] = selected
			}
		}

		switch phase {
		case "start":
			if r.SpanID == "" {
				add("span_id_missing", "error", r, "attach a span_id to every lifecycle record")
				break
			}
			if _, exists := spans[r.SpanID]; exists {
				add("duplicate_span_start", "error", r, "start each span exactly once")
				break
			}
			if _, exists := closed[r.SpanID]; exists {
				add("duplicate_span_start", "error", r, "do not reuse a closed span identifier")
				break
			}
			if operation == "run" && r.ParentSpanID == "" {
				runSpans = append(runSpans, r.SpanID)
			}
			if r.ParentSpanID != "" {
				parent, exists := spans[r.ParentSpanID]
				if !exists {
					add("parent_missing", "error", r, "start the parent span before its child")
				} else if parent.endSeq != 0 {
					add("child_outlives_parent", "error", r, "do not start children after parent end")
				} else if r.TraceID != "" && parent.traceID != "" && r.TraceID != parent.traceID {
					add("trace_mismatch", "error", r, "keep child and parent in the same trace")
				}
				if exists && !validLifecycleParent(operation, parent.operation) {
					add("execution_hierarchy_invalid", "error", r, "preserve run to target to subject to suite to probe to request to attempt ownership")
				}
				if exists && parent.operation != "run" && parent.target != "" {
					// A span may inherit an empty target field from a deliberately
					// unscoped record, but an explicitly scoped child must never
					// cross from one target execution tree into another.
					if target := stringField(r.Fields, "target"); target != "" && target != parent.target {
						add("target_scope_mismatch", "error", r, "keep every child lifecycle span within its target execution tree")
					}
				}
			} else if operation != "run" && operation != "probe.slots" {
				add("parent_missing", "error", r, "execution lifecycle operations require an active parent")
			}
			// Logging context is deliberately inherited by descendants.  Therefore a
			// descendant request also carries its enclosing `probe` identifier; it
			// must not be mistaken for a second lifecycle owner.  Ownership is the
			// explicit lifecycle operation, not a copied diagnostic field.
			spans[r.SpanID] = &span{id: r.SpanID, operation: operation, parent: r.ParentSpanID, traceID: r.TraceID, operationID: r.OperationID, source: stringField(r.Fields, "source"), target: stringField(r.Fields, "target"), protocol: stringField(r.Fields, "protocol"), namedProbe: operation == "probe" || operation == "sdk.probe", startSeq: r.Seq}
			v.CheckedSpans++
			if isHTTPAttempt(operation) {
				v.CheckedHTTPAttempts++
				if !hasAncestor(spans, r.SpanID, isLogicalRequest) {
					add("http_attempt_without_logical_request", "error", r, "make every HTTP attempt a child of a logical request")
				}
				if owners := namedProbeAncestors(spans, r.SpanID); owners != 1 {
					code := "http_attempt_without_probe"
					if owners > 1 {
						code = "http_attempt_multiple_probe_owners"
					}
					add(code, "error", r, "make every HTTP attempt a descendant of exactly one named report probe")
				}
			}
			if executor := executorFor(operation, stringField(r.Fields, "source")); executor != "" {
				executors[coverageKey{target: stringField(r.Fields, "target"), protocol: stringField(r.Fields, "protocol"), executor: executor}]++
			}
			if operation == "extension.invocation" && stringField(r.Fields, "source") == "executable" {
				executableInvocations = append(executableInvocations, r.SpanID)
			}
		case "end":
			if r.SpanID == "" {
				add("span_id_missing", "error", r, "attach a span_id to every lifecycle record")
				break
			}
			s, exists := spans[r.SpanID]
			if !exists {
				if _, alreadyClosed := closed[r.SpanID]; alreadyClosed {
					add("duplicate_span_end", "error", r, "end each span exactly once")
				} else {
					add("span_end_without_start", "error", r, "start a span before ending it")
				}
				break
			}
			if s.endSeq != 0 {
				add("duplicate_span_end", "error", r, "end each span exactly once")
				break
			}
			if operation != s.operation || r.OperationID != s.operationID {
				add("span_operation_mismatch", "error", r, "use the same operation identity for start and end")
			}
			if s.parent != "" {
				if parent, ok := spans[s.parent]; !ok {
					add("parent_missing", "error", r, "retain the parent lifecycle record")
				} else if parent.endSeq != 0 {
					add("parent_order_invalid", "error", r, "end children before their parent")
				}
			}
			s.endSeq = r.Seq
			closed[r.SpanID] = r.Seq
		case "validator":
			if r.SpanID == "" {
				add("validator_without_probe", "error", r, "emit validators from an active named probe")
				break
			}
			s, ok := spans[r.SpanID]
			if !ok || !isNamedProbe(s) {
				add("validator_without_probe", "error", r, "emit validators from an active named probe")
			} else if s.endSeq != 0 {
				add("validator_after_probe_end", "error", r, "emit validator evidence before probe.end")
			}
		}
	}
	if err := scanner.Err(); err != nil {
		v.Violations = append(v.Violations, Violation{Code: "trace_read_failed", Severity: "error", Remediation: "make the complete trace readable before verification"})
		v.Status = Inconclusive
	}
	if !seenAny {
		v.Status = NotRequested
		return v
	}
	if len(runSpans) != 1 {
		v.Violations = append(v.Violations, Violation{Code: "run_root_invalid", Severity: "error", Remediation: "emit exactly one root run span for each forensic trace"})
	}
	for _, s := range spans {
		if s.endSeq == 0 {
			v.Violations = append(v.Violations, Violation{Code: "missing_span_end", Severity: "error", Seq: s.startSeq, SpanID: s.id, OperationID: s.operationID, Remediation: "close every started span on every terminal path"})
		}
	}
	v.RunID = runID
	v.Coverage = coverage(executors, planned)
	for key, selected := range planned {
		if selected && executors[key] == 0 {
			v.Violations = append(v.Violations, Violation{Code: "selected_executor_unobserved", Severity: "error", Remediation: "emit complete lifecycle evidence for every selected executor or mark it not_selected in the execution plan"})
		}
	}
	for _, invocationID := range executableInvocations {
		invocation := spans[invocationID]
		if invocation != nil && hasExtensionHTTPTrace(spans, invocationID) {
			continue
		}
		// A non-cooperative executable returns only a terminal SuiteEvent after the child process
		// exits. It has no host-verifiable HTTP lifecycle, so treating its
		// invocation span as proof of complete forensic coverage would be false.
		if invocation == nil {
			continue
		}
		v.Violations = append(v.Violations, Violation{Code: "extension_trace_unavailable", Severity: "warning", Seq: invocation.startSeq, SpanID: invocation.id, OperationID: invocation.operationID, Remediation: "use an extension trace-capable protocol before claiming executable HTTP coverage"})
		v.Status = Inconclusive
	}
	if v.Status != Inconclusive && len(v.Violations) > 0 {
		v.Status = Fail
	}
	return v
}

func hasExtensionHTTPTrace(spans map[string]*span, invocationID string) bool {
	hasSuite, hasAttempt := false, false
	for id, candidate := range spans {
		if candidate.source != "executable" || !isDescendantOf(spans, id, invocationID) {
			continue
		}
		if candidate.operation == "suite" {
			hasSuite = true
		}
		if candidate.operation == "sdk.http_attempt" {
			hasAttempt = true
		}
	}
	return hasSuite && hasAttempt
}

func isDescendantOf(spans map[string]*span, id, ancestorID string) bool {
	for current := spans[id]; current != nil && current.parent != ""; current = spans[current.parent] {
		if current.parent == ancestorID {
			return true
		}
	}
	return false
}

func parseRecord(line []byte) (record, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(line, &fields); err != nil {
		return record{}, err
	}
	var r record
	r.Fields = fields
	// A syntactically valid record with a missing or ill-typed mandatory field is
	// a trace contract violation, not malformed JSON. Keep parsing it so callers
	// receive a useful stable violation code and can inspect remaining records.
	_ = decode(fields, "schema_version", &r.SchemaVersion)
	_ = decode(fields, "time", &r.Time)
	_ = decode(fields, "seq", &r.Seq)
	_ = decode(fields, "level", &r.Level)
	_ = decode(fields, "event", &r.Event)
	_ = decode(fields, "trace_id", &r.TraceID)
	_ = decode(fields, "span_id", &r.SpanID)
	_ = decode(fields, "parent_span_id", &r.ParentSpanID)
	_ = decode(fields, "operation_id", &r.OperationID)
	return r, nil
}

func decode(fields map[string]json.RawMessage, key string, into any) error {
	raw, ok := fields[key]
	if !ok {
		return fmt.Errorf("missing %s", key)
	}
	return json.Unmarshal(raw, into)
}

func stringField(fields map[string]json.RawMessage, key string) string {
	var value string
	if raw, ok := fields[key]; ok {
		_ = json.Unmarshal(raw, &value)
	}
	return value
}

func boolField(fields map[string]json.RawMessage, key string) bool {
	var value bool
	if raw, ok := fields[key]; ok {
		_ = json.Unmarshal(raw, &value)
	}
	return value
}

func hasField(fields map[string]json.RawMessage, key string) bool {
	_, ok := fields[key]
	return ok
}

func safeIdentifier(value string) bool {
	if value == "" || len(value) > 160 {
		return false
	}
	for _, runeValue := range value {
		if !(runeValue >= 'a' && runeValue <= 'z' || runeValue >= '0' && runeValue <= '9' || runeValue == '.' || runeValue == '_' || runeValue == '-') {
			return false
		}
	}
	return true
}

func containsSensitiveData(fields map[string]json.RawMessage) bool {
	for key, raw := range fields {
		lowerKey := strings.ToLower(key)
		for _, marker := range []string{"authorization", "api_key", "apikey", "credential", "password", "secret", "token", "header", "query", "body", "prompt", "tool_argument", "media"} {
			if strings.Contains(lowerKey, marker) {
				return true
			}
		}
		if sensitiveJSON(raw) {
			return true
		}
	}
	return false
}

func hasOnlyAllowedFields(fields map[string]json.RawMessage) bool {
	for key := range fields {
		switch key {
		case "schema_version", "time", "level", "event", "seq", "trace_id", "span_id", "parent_span_id":
			continue
		}
		if !logging.IsAllowedField(key) {
			return false
		}
	}
	return true
}

func sensitiveJSON(raw json.RawMessage) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return true
	}
	return sensitiveValue(value)
}

func sensitiveValue(value any) bool {
	switch typed := value.(type) {
	case string:
		lower := strings.ToLower(typed)
		return strings.Contains(lower, "http://") || strings.Contains(lower, "https://") || strings.Contains(lower, "bearer ") || strings.Contains(lower, "api_key") || strings.Contains(lower, "authorization:")
	case []any:
		for _, item := range typed {
			if sensitiveValue(item) {
				return true
			}
		}
	case map[string]any:
		for key, item := range typed {
			lowerKey := strings.ToLower(key)
			if strings.Contains(lowerKey, "authorization") || strings.Contains(lowerKey, "api_key") || strings.Contains(lowerKey, "secret") || strings.Contains(lowerKey, "token") || strings.Contains(lowerKey, "prompt") || strings.Contains(lowerKey, "body") {
				return true
			}
			if sensitiveValue(item) {
				return true
			}
		}
	}
	return false
}

func eventOperation(event string) (string, string) {
	if strings.HasSuffix(event, ".start") {
		operation := strings.TrimSuffix(event, ".start")
		if isLifecycleOperation(operation) {
			return operation, "start"
		}
	}
	if strings.HasSuffix(event, ".end") {
		operation := strings.TrimSuffix(event, ".end")
		if isLifecycleOperation(operation) {
			return operation, "end"
		}
	}
	if strings.HasSuffix(event, ".validator") {
		operation := strings.TrimSuffix(event, ".validator")
		if isLifecycleOperation(operation) {
			return operation, "validator"
		}
	}
	return "", ""
}

func isLifecycleOperation(operation string) bool {
	switch operation {
	case "run", "target", "provider", "model", "suite", "probe", "result",
		"http.logical_request", "http.attempt", "http.stream_logical_request", "http.stream_attempt",
		"sdk.probe", "sdk.logical_request", "sdk.http_attempt",
		"artifact.logical_request", "artifact.attempt", "extension.invocation", "probe.slots":
		return true
	default:
		return false
	}
}

func isHTTPAttempt(operation string) bool {
	return operation == "http.attempt" || operation == "http.stream_attempt" || operation == "sdk.http_attempt" || operation == "artifact.attempt"
}
func isLogicalRequest(operation string) bool {
	return operation == "http.logical_request" || operation == "http.stream_logical_request" || operation == "sdk.logical_request" || operation == "artifact.logical_request"
}
func isNamedProbe(s *span) bool { return s != nil && s.namedProbe }

func hasAncestor(spans map[string]*span, id string, predicate func(string) bool) bool {
	for current := spans[id]; current != nil && current.parent != ""; current = spans[current.parent] {
		parent := spans[current.parent]
		if parent == nil {
			return false
		}
		if predicate(parent.operation) {
			return true
		}
	}
	return false
}

func namedProbeAncestors(spans map[string]*span, id string) int {
	owners := 0
	for current := spans[id]; current != nil && current.parent != ""; current = spans[current.parent] {
		parent := spans[current.parent]
		if parent == nil {
			return owners
		}
		if isNamedProbe(parent) {
			owners++
		}
	}
	return owners
}

func validLifecycleParent(operation, parent string) bool {
	switch operation {
	case "run":
		return false
	case "target":
		return parent == "run"
	case "provider", "model":
		return parent == "target"
	case "extension.invocation":
		return parent == "provider" || parent == "model"
	case "suite":
		return parent == "provider" || parent == "model" || parent == "extension.invocation"
	case "probe", "sdk.probe":
		return parent == "suite"
	case "http.logical_request", "http.stream_logical_request", "sdk.logical_request", "artifact.logical_request":
		return parent == "probe" || parent == "sdk.probe"
	case "http.attempt", "http.stream_attempt", "sdk.http_attempt", "artifact.attempt":
		return isLogicalRequest(parent)
	default:
		return true
	}
}

func executorFor(operation, source string) string {
	if source == "executable" {
		return "extension"
	}
	if source == "builtin" {
		// Built-in extension suites are the official SDK executor branch. The
		// extension protocol is only an implementation boundary; reporting it
		// as an executable extension would make an unselected ext-suite appear
		// observed and hide a missing SDK branch.
		return "sdk"
	}
	switch {
	case strings.HasPrefix(operation, "sdk."):
		return "sdk"
	case strings.HasPrefix(operation, "extension."):
		return "extension"
	case operation == "http.attempt" || operation == "http.stream_attempt" || operation == "http.logical_request" || operation == "http.stream_logical_request" || operation == "probe":
		return "native"
	default:
		return ""
	}
}

type coverageKey struct{ target, protocol, executor string }

func coverage(executors map[coverageKey]int, planned map[coverageKey]bool) []Coverage {
	keys := make([]coverageKey, 0, len(executors)+len(planned))
	seen := map[coverageKey]struct{}{}
	for key := range executors {
		keys = append(keys, key)
		seen[key] = struct{}{}
	}
	for key := range planned {
		if _, exists := seen[key]; !exists {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].target != keys[j].target {
			return keys[i].target < keys[j].target
		}
		if keys[i].protocol != keys[j].protocol {
			return keys[i].protocol < keys[j].protocol
		}
		return keys[i].executor < keys[j].executor
	})
	result := make([]Coverage, 0, len(keys))
	for _, executor := range keys {
		status := "not_selected"
		if executors[executor] > 0 {
			status = "complete"
		} else if planned[executor] {
			status = "missing"
		}
		result = append(result, Coverage{Target: executor.target, Protocol: executor.protocol, Executor: executor.executor, Status: status, Count: executors[executor]})
	}
	return result
}
