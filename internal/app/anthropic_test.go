package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/schema"
	"github.com/ilyar/aiqa/internal/traceverify"
)

func TestDualTargetCommandWritesIndependentV3Report(t *testing.T) {
	openAI := newMockOpenAI(t)
	defer openAI.Close()
	anthropic := newMockAnthropic(t)
	defer anthropic.Close()
	reportSchemaPath, err := filepath.Abs(filepath.Join("..", "..", "schema", "report-v3.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	traceSchemaPath, err := filepath.Abs(filepath.Join("..", "..", "schema", "trace-verdict-v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	withTempWorkingDirectory(t)
	t.Setenv("OPENAI_BASE_URL", openAI.URL+"/v1")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", anthropic.URL+"/anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-test-key")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "must-not-be-used")
	t.Setenv("ANTHROPIC_BETA", "must-not-be-used")
	cfg, err := config.Parse([]string{"test=compat", "--timeout=5", "--request-interval=1ms", "--verbose=4", "--format=markdown,json", "--name=dual"})
	if err != nil {
		t.Fatal(err)
	}
	var progressOutput bytes.Buffer
	oldWriter, oldTTY := progressWriter, progressTTY
	progressWriter, progressTTY = &progressOutput, func() bool { return false }
	t.Cleanup(func() { progressWriter, progressTTY = oldWriter, oldTTY })
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ExitCode != 0 {
		t.Fatalf("exit=%d summary=%+v", out.ExitCode, out.ReportV3.Summary)
	}
	progressText := progressOutput.String()
	for _, want := range []string{"Discovering anthropic", "Discovering openai", "Planned "} {
		if !strings.Contains(progressText, want) {
			t.Fatalf("progress misses %q:\n%s", want, progressText)
		}
	}
	if strings.Index(progressText, "Discovering openai") > strings.Index(progressText, "Planned ") {
		t.Fatalf("progress froze before every target inventory was prepared:\n%s", progressText)
	}
	trace, err := os.ReadFile("dual.log")
	if err != nil {
		t.Fatalf("read diagnostic trace: %v", err)
	}
	for _, required := range []string{
		`"level":"TRACE"`, `"event":"http.attempt.begin"`, `"event":"http.logical_request.end"`,
		`"event":"probe.slots.wait"`, `"event":"http.sse.event"`,
		`"seq":`, `"target":"openai"`, `"operation_id":"op-`, `"derived_from":"anthropic.messages.basic"`,
	} {
		if !strings.Contains(string(trace), required) {
			t.Fatalf("verbose 4 trace does not contain %q", required)
		}
	}
	for _, forbidden := range []string{"test-key", "anthropic-test-key", "Reply with exactly", "AIQA_OK", "Authorization"} {
		if strings.Contains(string(trace), forbidden) {
			t.Fatalf("verbose 4 trace leaks %q", forbidden)
		}
	}
	assertExecutionTraceGraph(t, trace)
	verdictData, err := os.ReadFile("dual.trace-verdict.json")
	if err != nil {
		t.Fatalf("read trace verdict: %v", err)
	}
	var traceVerdict traceverify.Verdict
	if err := json.Unmarshal(verdictData, &traceVerdict); err != nil {
		t.Fatalf("decode trace verdict: %v", err)
	}
	if traceVerdict.SchemaVersion != "aiqa.trace-verdict.v1" || traceVerdict.Status != traceverify.Pass {
		t.Fatalf("invalid trace verdict: %s", verdictData)
	}
	if offline := traceverify.Verify(bytes.NewReader(trace)); !reflect.DeepEqual(offline, traceVerdict) {
		t.Fatalf("offline verdict differs from run verdict:\noffline=%+v\nrun=%+v", offline, traceVerdict)
	}
	if err := schema.ValidateFile(traceSchemaPath, "dual.trace-verdict.json"); err != nil {
		t.Fatalf("trace verdict schema validation failed: %v", err)
	}
	data, err := os.ReadFile("dual.json")
	if err != nil {
		t.Fatal(err)
	}
	var report result.ReportV3
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != "aiqa.report.v3" || len(report.Targets) != 2 {
		t.Fatalf("unexpected report: schema=%s targets=%d", report.SchemaVersion, len(report.Targets))
	}
	if report.ForensicTrace == nil || report.ForensicTrace.Status != "pass" || report.ForensicTrace.VerdictPath != "dual.trace-verdict.json" {
		t.Fatalf("public forensic trace reference=%+v", report.ForensicTrace)
	}
	if report.Targets[0].Protocol != "anthropic" || report.Targets[1].Protocol != "openai" {
		t.Fatalf("target order/protocols: %+v", report.Targets)
	}
	for _, target := range report.Targets {
		if target.Summary.Assessment == nil || target.Summary.Assessment.Verdict != "pass" {
			var gaps []string
			for _, requirement := range target.Requirements {
				if requirement.Status != result.RequirementPass && requirement.Status != result.RequirementNotApplicable {
					gaps = append(gaps, fmt.Sprintf("%s=%s", requirement.ID, requirement.Status))
				}
			}
			t.Fatalf("target %s did not pass: %+v gaps=%v", target.ID, target.Summary, gaps)
		}
	}
	var anthropicTarget *result.TargetReport
	for index := range report.Targets {
		if report.Targets[index].Protocol == "anthropic" {
			anthropicTarget = &report.Targets[index]
		}
	}
	if anthropicTarget == nil {
		t.Fatal("Anthropic target is missing")
	}
	if anthropicTarget.Summary.Usage.TotalTokens != anthropicTarget.Summary.Usage.InputTokens+anthropicTarget.Summary.Usage.OutputTokens+anthropicTarget.Summary.Usage.CacheCreationInputTokens+anthropicTarget.Summary.Usage.CacheReadInputTokens {
		t.Fatalf("Anthropic total usage is inconsistent: %+v", anthropicTarget.Summary.Usage)
	}
	var basicUsage, usageCheck, toolUsage result.Usage
	for _, probe := range anthropicTarget.Models[0].Compatibility {
		switch probe.ID {
		case "anthropic.messages.basic":
			basicUsage = probe.Usage
		case "anthropic.messages.usage":
			usageCheck = probe.Usage
		case "anthropic.tools.specific":
			toolUsage = probe.Usage
		}
	}
	if basicUsage.TotalTokens != 0 || usageCheck.TotalTokens == 0 || toolUsage.TotalTokens == 0 {
		t.Fatalf("Anthropic usage must be counted once and include tool probes: basic=%+v usage=%+v tool=%+v", basicUsage, usageCheck, toolUsage)
	}
	contract, err := profile.LoadFile("anthropic-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	covered := map[string]bool{}
	for _, requirement := range anthropicTarget.Requirements {
		covered[requirement.ID] = true
		if requirement.Status == result.RequirementSkip {
			t.Fatalf("full mock left requirement %s skipped", requirement.ID)
		}
	}
	requestedModules := map[string]bool{}
	for _, module := range anthropicTarget.Models[0].Modules {
		requestedModules[module.ID] = module.Requested
	}
	for _, probe := range contract.Probes {
		if probe.Executor == "extension" {
			continue
		}
		moduleID := ""
		for _, module := range contract.Modules {
			for _, id := range module.ProbeIDs {
				if id == probe.ID {
					moduleID = module.ID
				}
			}
		}
		if moduleID != "" && !requestedModules[moduleID] {
			continue
		}
		if !covered[probe.ID] {
			t.Fatalf("profile probe %s is absent from canonical requirements", probe.ID)
		}
	}
	if strings.Contains(string(data), "test-key") || strings.Contains(string(data), "anthropic-test-key") {
		t.Fatal("API key leaked into report")
	}
	if err := schema.ValidateFile(reportSchemaPath, "dual.json"); err != nil {
		t.Fatalf("v3 schema validation failed: %v", err)
	}
	markdown, err := os.ReadFile("dual.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Target: OpenAI", "## Target: Anthropic", "Percentages are not combined into a cross-protocol score"} {
		if !strings.Contains(string(markdown), want) {
			t.Fatalf("markdown missing %q", want)
		}
	}
}

// assertExecutionTraceGraph validates the private JSONL contract from a real
// loopback run. It deliberately reconstructs links from IDs instead of
// asserting individual text fragments, so a missing parent or terminal span
// cannot be hidden by a superficially plausible log line.
func assertExecutionTraceGraph(t *testing.T, data []byte) {
	t.Helper()
	type span struct {
		name   string
		parent string
		start  int
		end    int
	}
	spans := map[string]*span{}
	previousSeq := float64(0)
	for lineNo, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("line %d is not JSONL: %v", lineNo+1, err)
		}
		for _, field := range []string{"time", "seq", "level", "event", "trace_id", "span_id", "parent_span_id"} {
			if _, ok := event[field]; !ok {
				t.Fatalf("line %d misses %s: %s", lineNo+1, field, line)
			}
		}
		seq, ok := event["seq"].(float64)
		if !ok || seq <= previousSeq {
			t.Fatalf("line %d has non-monotonic seq %v after %v", lineNo+1, event["seq"], previousSeq)
		}
		previousSeq = seq
		name, _ := event["event"].(string)
		spanID, _ := event["span_id"].(string)
		if spanID == "" || !strings.HasSuffix(name, ".start") {
			continue
		}
		if _, exists := spans[spanID]; exists {
			t.Fatalf("span %s has more than one start", spanID)
		}
		parent, _ := event["parent_span_id"].(string)
		spans[spanID] = &span{name: strings.TrimSuffix(name, ".start"), parent: parent, start: lineNo}
	}
	for lineNo, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event map[string]any
		_ = json.Unmarshal([]byte(line), &event)
		name, _ := event["event"].(string)
		spanID, _ := event["span_id"].(string)
		if !strings.HasSuffix(name, ".end") || spanID == "" {
			continue
		}
		item, exists := spans[spanID]
		if !exists {
			// TRACE phase events such as pacing.wait.end are not lifecycle
			// terminals and intentionally reuse the active span ID.
			continue
		}
		if item.name != strings.TrimSuffix(name, ".end") {
			continue
		}
		if item.end != 0 {
			t.Fatalf("span %s has more than one terminal event", spanID)
		}
		item.end = lineNo + 1
	}
	for id, item := range spans {
		if item.end == 0 {
			t.Fatalf("span %s (%s) has no terminal event", id, item.name)
		}
		if item.parent != "" {
			parent, exists := spans[item.parent]
			if !exists || parent.start >= item.start || parent.end <= item.end {
				t.Fatalf("span %s (%s) is outside parent %s: child=%+v parent=%+v", id, item.name, item.parent, item, parent)
			}
		}
	}
	chain := []string{"target", "model", "suite", "probe", "http.logical_request", "http.attempt"}
	children := map[string][]string{}
	for id, item := range spans {
		children[item.parent] = append(children[item.parent], id)
	}
	var hasChain func(string, int) bool
	hasChain = func(parent string, index int) bool {
		if index == len(chain) {
			return true
		}
		for _, child := range children[parent] {
			if spans[child].name == chain[index] && hasChain(child, index+1) {
				return true
			}
		}
		return false
	}
	for id, item := range spans {
		if item.name == "run" && hasChain(id, 0) {
			return
		}
	}
	t.Fatalf("no complete execution chain run→%s; spans=%d", strings.Join(chain, "→"), len(spans))
}

func TestGlobalModelRunsSameIDIndependentlyAcrossTargets(t *testing.T) {
	openAI := newMockOpenAI(t)
	defer openAI.Close()
	// The duplicate Anthropic entry is an independent provider defect. It must
	// not prevent the same resolved model ID from being exercised by OpenAI.
	const modelID = "vendor/model-a"
	duplicateAnthropic := newMockAnthropicForModel(t, modelID, true, nil)
	defer duplicateAnthropic.Close()
	withTempWorkingDirectory(t)
	t.Setenv("OPENAI_BASE_URL", openAI.URL+"/v1")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", duplicateAnthropic.URL+"/anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-test-key")
	cfg, err := config.Parse([]string{
		"test=compat", "--timeout=5", "--request-interval=1ms", "--format=json", "--name=same-model",
		"--model=" + modelID,
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil || out.ReportV3 == nil || len(out.ReportV3.Targets) != 2 {
		t.Fatalf("same-model run failed: exit=%d err=%v report=%+v", out.ExitCode, err, out.ReportV3)
	}
	var openAITarget, anthropicTarget *result.TargetReport
	for index := range out.ReportV3.Targets {
		target := &out.ReportV3.Targets[index]
		switch target.Protocol {
		case "openai":
			openAITarget = target
		case "anthropic":
			anthropicTarget = target
		}
		if len(target.Models) != 1 || target.Models[0].ID != modelID {
			t.Fatalf("target %s did not independently resolve %q: %+v", target.ID, modelID, target.Models)
		}
	}
	if openAITarget == nil || openAITarget.Summary.Assessment == nil || openAITarget.Summary.Assessment.Verdict != "pass" {
		t.Fatalf("OpenAI same-model target did not pass: %+v", openAITarget)
	}
	if anthropicTarget == nil || anthropicTarget.Summary.Assessment == nil || anthropicTarget.Summary.Assessment.Verdict != "fail" {
		t.Fatalf("Anthropic provider defect did not remain an independent failure: %+v", anthropicTarget)
	}
	seenEvidence := map[string]bool{}
	for _, target := range out.ReportV3.Targets {
		for _, requirement := range target.Requirements {
			if requirement.Subject.TargetID != target.ID || requirement.Subject.Protocol != target.Protocol {
				t.Fatalf("requirement subject lost target qualification: target=%s/%s subject=%+v", target.ID, target.Protocol, requirement.Subject)
			}
			for _, evidence := range requirement.Evidence {
				if seenEvidence[evidence.ID] {
					t.Fatalf("target-qualified evidence ID collided: %s", evidence.ID)
				}
				seenEvidence[evidence.ID] = true
			}
		}
	}
}

func TestAnthropicThinkingRequiresOptInAndRedactsPrivateContent(t *testing.T) {
	server := newMockAnthropic(t)
	defer server.Close()
	withTempWorkingDirectory(t)
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL+"/anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-test-key")
	cfg, err := config.Parse([]string{
		"test=compat", "--format=json", "--name=thinking", "--timeout=5", "--request-interval=1ms",
		"--module=anthropic.thinking", "--allow-high-token-probes",
		"--model=claude-mock",
		"--anthropic-model-capability=claude-mock=anthropic.messages.text+anthropic.messages.thinking.manual+anthropic.messages.thinking.adaptive",
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ReportV3 == nil || len(out.ReportV3.Targets) != 1 {
		t.Fatalf("unexpected report: %+v", out.ReportV3)
	}
	seen := map[string]result.Status{}
	for _, probe := range out.ReportV3.Targets[0].Models[0].Compatibility {
		if strings.HasPrefix(probe.ID, "anthropic.thinking.") {
			seen[probe.ID] = probe.Status
		}
	}
	for _, id := range []string{"anthropic.thinking.adaptive", "anthropic.thinking.effort", "anthropic.thinking.manual", "anthropic.thinking.manual.validation", "anthropic.thinking.stream"} {
		if seen[id] != result.StatusPass {
			t.Fatalf("thinking probe %s=%s, all=%v model=%+v", id, seen[id], seen, out.ReportV3.Targets[0].Models[0])
		}
	}
	data, err := os.ReadFile("thinking.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private chain") || strings.Contains(string(data), "signature-secret") {
		t.Fatal("thinking text or signature leaked into report")
	}
}

func TestAnthropicConcurrencyAppliesToProbes(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	server := newMockAnthropicWithMiddleware(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			current := active.Add(1)
			for {
				seen := maximum.Load()
				if current <= seen || maximum.CompareAndSwap(seen, current) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			next.ServeHTTP(w, r)
			active.Add(-1)
		})
	})
	defer server.Close()
	withTempWorkingDirectory(t)
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL+"/anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-test-key")
	cfg, err := config.Parse([]string{"test=compat", "--timeout=5", "--request-interval=1ms", "--concurrency=4", "--format=json", "--name=anthropic-parallel"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ReportV3 == nil || out.ExitCode != 0 {
		t.Fatalf("parallel run changed compatibility results: %+v", out.ReportV3)
	}
	if got := maximum.Load(); got < 2 || got > 4 {
		t.Fatalf("maximum concurrent HTTP requests=%d, want 2..4", got)
	}
}

func TestBuiltInOfficialSDKExtensionsRunWithoutDocker(t *testing.T) {
	openAI := newMockOpenAI(t)
	defer openAI.Close()
	anthropic := newMockAnthropic(t)
	defer anthropic.Close()
	reportSchemaPath, err := filepath.Abs(filepath.Join("..", "..", "schema", "report-v3.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	withTempWorkingDirectory(t)
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("OPENAI_BASE_URL", openAI.URL+"/v1")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", anthropic.URL+"/anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-test-key")
	cfg, err := config.Parse([]string{"test=sdk", "--timeout=5", "--request-interval=1ms", "--concurrency=4", "--format=json", "--name=extensions"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ReportV3 == nil || len(out.ReportV3.Targets) != 2 {
		t.Fatalf("unexpected extension report: %+v", out.ReportV3)
	}
	for _, target := range out.ReportV3.Targets {
		if len(target.Extensions) == 0 {
			t.Fatalf("target %s has no extension runs", target.ID)
		}
		for _, run := range target.Extensions {
			if run.Provenance.Source != "builtin" || run.Runner.Status != result.StatusPass || run.Runner.PacingCompliance != "host_enforced" {
				t.Fatalf("target %s extension runner: %+v", target.ID, run)
			}
			for _, suite := range run.Suites {
				if suite.Status != result.StatusPass {
					t.Fatalf("target %s suite %s=%s: %s", target.ID, suite.SuiteID, suite.Status, suite.Summary)
				}
			}
		}
		for _, model := range target.Models {
			if len(model.Compatibility) != 0 {
				t.Fatalf("extension-only run executed native compatibility probes for %s/%s", target.ID, model.ID)
			}
		}
	}
	data, err := os.ReadFile("extensions.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"external"`) || strings.Contains(string(data), "docker") || strings.Contains(string(data), "test-key") || strings.Contains(string(data), "anthropic-test-key") {
		t.Fatalf("v3 report contains legacy transport or credential data: %s", data)
	}
	t.Setenv("PATH", originalPath)
	if err := schema.ValidateFile(reportSchemaPath, "extensions.json"); err != nil {
		t.Fatalf("v3 extension schema validation failed: %v", err)
	}
}

func newMockAnthropic(t *testing.T) *httptest.Server {
	return newMockAnthropicForModel(t, "claude-mock", false, nil)
}

func newMockAnthropicWithMiddleware(t *testing.T, middleware func(http.Handler) http.Handler) *httptest.Server {
	return newMockAnthropicForModel(t, "claude-mock", false, middleware)
}

func newMockAnthropicForModel(t *testing.T, modelID string, duplicateModel bool, middleware func(http.Handler) http.Handler) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	check := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("x-api-key") != "anthropic-test-key" {
			anthropicError(w, 401, "authentication_error", "invalid key")
			return false
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			anthropicError(w, 400, "invalid_request_error", "invalid version")
			return false
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("Anthropic request leaked Authorization header")
		}
		if r.Header.Get("anthropic-beta") != "" {
			t.Errorf("stable Anthropic request inherited a beta header")
		}
		return true
	}
	mux.HandleFunc("/anthropic/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		entries := []any{anthropicModel(modelID)}
		if duplicateModel {
			entries = append(entries, anthropicModel(modelID))
		}
		writeJSON(w, 200, map[string]any{"data": entries, "has_more": false, "first_id": modelID, "last_id": modelID})
	})
	mux.HandleFunc("/anthropic/v1/models/", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/anthropic/v1/models/")
		if id != modelID {
			anthropicError(w, 404, "not_found_error", "model not found")
			return
		}
		writeJSON(w, 200, anthropicModel(modelID))
	})
	mux.HandleFunc("/anthropic/v1/messages/count_tokens", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		count := 12
		if _, ok := body["system"]; ok {
			count += 4
		}
		if _, ok := body["tools"]; ok {
			count += 8
		}
		writeJSON(w, 200, map[string]any{"input_tokens": count})
	})
	mux.HandleFunc("/anthropic/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		if !check(w, r) {
			return
		}
		var body map[string]any
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&body); err != nil {
			anthropicError(w, 400, "invalid_request_error", "invalid JSON")
			return
		}
		if body["model"] != modelID {
			anthropicError(w, 404, "not_found_error", "model not found")
			return
		}
		if _, ok := body["max_tokens"].(float64); !ok {
			anthropicError(w, 400, "invalid_request_error", "max_tokens must be integer")
			return
		}
		thinking, _ := body["thinking"].(map[string]any)
		if thinking != nil {
			if budget, ok := thinking["budget_tokens"].(float64); ok && budget < 1024 {
				anthropicError(w, 400, "invalid_request_error", "thinking budget is too small")
				return
			}
		}
		if body["stream"] == true {
			if r.Header.Get("Accept") != "text/event-stream" {
				t.Errorf("Anthropic stream Accept=%q", r.Header.Get("Accept"))
			}
			if thinking != nil {
				writeAnthropicThinkingStream(w, modelID)
			} else if choice, _ := body["tool_choice"].(map[string]any); choice != nil && choice["type"] == "tool" {
				writeAnthropicToolStream(w, modelID)
			} else {
				writeAnthropicStream(w, modelID)
			}
			return
		}
		content := []any{map[string]any{"type": "text", "text": "AIQA_OK"}}
		if thinking != nil {
			content = []any{map[string]any{"type": "thinking", "thinking": "private chain", "signature": "signature-secret"}, map[string]any{"type": "text", "text": "AIQA_OK"}}
		}
		stop := "end_turn"
		stopSequence := any(nil)
		if sequences, _ := body["stop_sequences"].([]any); len(sequences) > 0 {
			stop = "stop_sequence"
			stopSequence = sequences[0]
		}
		if choice, _ := body["tool_choice"].(map[string]any); choice != nil && choice["type"] != "none" {
			content = []any{map[string]any{"type": "tool_use", "id": "toolu_mock", "name": "get_status", "input": map[string]any{"resource_id": "resource-alpha"}}}
			if tools, _ := body["tools"].([]any); len(tools) > 1 {
				content = append(content, map[string]any{"type": "tool_use", "id": "toolu_owner", "name": "get_owner", "input": map[string]any{"resource_id": "resource-alpha"}})
			}
			stop = "tool_use"
			stopSequence = nil
		} else if messages, _ := body["messages"].([]any); hasAnthropicToolResult(messages) {
			content = []any{map[string]any{"type": "text", "text": "resource-alpha is available"}}
		} else if _, ok := body["output_config"]; ok {
			content = []any{map[string]any{"type": "text", "text": `{"ok":true,"value":"AIQA_OK"}`}}
		}
		writeJSON(w, 200, map[string]any{"id": "msg_mock", "type": "message", "role": "assistant", "model": modelID, "content": content, "stop_reason": stop, "stop_sequence": stopSequence, "usage": map[string]any{"input_tokens": 12, "output_tokens": 3}})
	})
	var handler http.Handler = mux
	if middleware != nil {
		handler = middleware(handler)
	}
	return httptest.NewServer(handler)
}

func anthropicModel(modelID string) map[string]any {
	support := func() map[string]any { return map[string]any{"supported": true} }
	return map[string]any{
		"id": modelID, "type": "model", "display_name": "Claude Mock",
		"created_at":       time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		"max_input_tokens": 200000, "max_tokens": 8192, "debug_echo": "anthropic-test-key",
		"capabilities": map[string]any{
			"batch": support(), "citations": support(), "code_execution": support(), "image_input": support(), "pdf_input": support(), "structured_outputs": support(), "tools": support(),
			"context_management": map[string]any{"clear_thinking_20251015": support(), "clear_tool_uses_20250919": support(), "compact_20260112": support(), "supported": true},
			"effort":             map[string]any{"high": support(), "low": support(), "max": support(), "medium": support(), "xhigh": support(), "supported": true},
			"thinking":           map[string]any{"supported": true, "types": map[string]any{"adaptive": support(), "enabled": support()}},
		},
	}
}
func anthropicError(w http.ResponseWriter, status int, typ, message string) {
	writeJSON(w, status, map[string]any{"type": "error", "error": map[string]any{"type": typ, "message": message}, "request_id": "req_mock"})
}
func hasAnthropicToolResult(messages []any) bool {
	for _, raw := range messages {
		m, _ := raw.(map[string]any)
		blocks, _ := m["content"].([]any)
		for _, blockRaw := range blocks {
			block, _ := blockRaw.(map[string]any)
			if block["type"] == "tool_result" {
				return true
			}
		}
	}
	return false
}
func writeAnthropicStream(w http.ResponseWriter, modelID string) {
	w.Header().Set("Content-Type", "text/event-stream")
	events := []struct {
		name string
		data map[string]any
	}{
		{"message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_stream", "type": "message", "role": "assistant", "model": modelID, "content": []any{}, "stop_reason": nil, "usage": map[string]any{"input_tokens": 12, "output_tokens": 0}}}},
		{"content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}}},
		{"content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": "AIQA_OK"}}},
		{"content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}},
		{"message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 3}}},
		{"message_stop", map[string]any{"type": "message_stop"}},
	}
	for _, event := range events {
		raw, _ := json.Marshal(event.data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.name, raw)
	}
}

func writeAnthropicToolStream(w http.ResponseWriter, modelID string) {
	w.Header().Set("Content-Type", "text/event-stream")
	events := []struct {
		name string
		data map[string]any
	}{
		{"message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_tool", "type": "message", "role": "assistant", "model": modelID, "content": []any{}, "stop_reason": nil, "usage": map[string]any{"output_tokens": 0, "input_tokens": 12}}}},
		{"content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": "toolu_mock", "name": "get_status", "input": map[string]any{}}}},
		{"content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": "{\"resource_id\":\"resource-alpha\"}"}}},
		{"content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}},
		{"message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use"}, "usage": map[string]any{"output_tokens": 4}}},
		{"message_stop", map[string]any{"type": "message_stop"}},
	}
	for _, event := range events {
		raw, _ := json.Marshal(event.data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.name, raw)
	}
}

func writeAnthropicThinkingStream(w http.ResponseWriter, modelID string) {
	w.Header().Set("Content-Type", "text/event-stream")
	events := []struct {
		name string
		data map[string]any
	}{
		{"message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_thinking", "type": "message", "role": "assistant", "model": modelID, "content": []any{}, "stop_reason": nil, "usage": map[string]any{"input_tokens": 12, "output_tokens": 0}}}},
		{"content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "thinking", "thinking": ""}}},
		{"content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "thinking_delta", "thinking": "private chain"}}},
		{"content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "signature_delta", "signature": "signature-secret"}}},
		{"content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}},
		{"content_block_start", map[string]any{"type": "content_block_start", "index": 1, "content_block": map[string]any{"type": "text", "text": ""}}},
		{"content_block_delta", map[string]any{"type": "content_block_delta", "index": 1, "delta": map[string]any{"type": "text_delta", "text": "AIQA_OK"}}},
		{"content_block_stop", map[string]any{"type": "content_block_stop", "index": 1}},
		{"message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 5}}},
		{"message_stop", map[string]any{"type": "message_stop"}},
	}
	for _, event := range events {
		raw, _ := json.Marshal(event.data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.name, raw)
	}
}
