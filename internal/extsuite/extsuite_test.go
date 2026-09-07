package extsuite

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/probe"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/telemetry"
)

type capturedDiagnostic struct {
	event  string
	fields logging.Fields
}
type captureDiagnosticLogger struct{ entries []capturedDiagnostic }

func (l *captureDiagnosticLogger) Log(_ logging.Level, event string, fields logging.Fields) bool {
	copy := logging.Fields{}
	for key, value := range fields {
		copy[key] = value
	}
	l.entries = append(l.entries, capturedDiagnostic{event, copy})
	return true
}

type progressCapture struct {
	mu        sync.Mutex
	added     []int
	lifecycle []telemetry.LifecycleEvent
}

func (p *progressCapture) ObserveLifecycle(event telemetry.LifecycleEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lifecycle = append(p.lifecycle, event)
}
func (*progressCapture) RunStarted(int)          {}
func (*progressCapture) DiscoveryStarted(string) {}
func (*progressCapture) Plan(string, int)        {}
func (*progressCapture) RevisePlan(string, int)  {}
func (p *progressCapture) AddPlan(_ string, delta int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.added = append(p.added, delta)
}
func (*progressCapture) Pacing(string, bool, time.Duration) {}
func (*progressCapture) TargetFinished(string, string)      {}
func (*progressCapture) RunFinished()                       {}
func (*progressCapture) Message(string)                     {}
func (*progressCapture) Close()                             {}

func TestExtensionProgressUsesDeclaredSuitesWithoutHTTPInference(t *testing.T) {
	manifest := testManifest("fixture.progress", "openai",
		SuiteDescriptor{ID: "first", Subject: "provider", Cost: "text", InternalConcurrency: 1},
		SuiteDescriptor{ID: "second", Subject: "provider", Cost: "text", InternalConcurrency: 1})
	extension := &testExtension{source: "executable", manifest: manifest}
	extension.run = func(_ context.Context, request RunRequest, sink EventSink) (RunnerTelemetry, error) {
		for _, suiteID := range request.SuiteIDs {
			if err := sink.Emit(SuiteEvent{SchemaVersion: ProtocolVersion, Type: "suite_result", RunID: request.RunID, ExtensionID: manifest.Extension.ID, SuiteID: suiteID, Subject: request.Subject, Outcome: result.ExtensionOutcomePass, DurationMS: 1, Summary: "ok"}); err != nil {
				return RunnerTelemetry{}, err
			}
		}
		return RunnerTelemetry{}, nil
	}
	registry, err := NewRegistry(extension)
	if err != nil {
		t.Fatal(err)
	}
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	client, err := api.NewClient("https://example.test/v1", "secret", "test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	sink := &progressCapture{}
	runs := ExecuteTargetWithTelemetryAndProgress(context.Background(), registry, config.Config{Tests: []string{"ext-suite"}, Timeout: time.Second, Concurrency: 1}, config.TargetConfig{ID: "fixture", Protocol: config.ProtocolOpenAI, BaseURL: "https://example.test/v1", APIKey: "secret", RequestInterval: time.Millisecond}, p, nil, client, probe.NewExecutor(1), nil, nil, sink)
	if len(runs) != 1 || len(runs[0].Suites) != 2 {
		t.Fatalf("runs=%+v", runs)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.added) != 1 || sink.added[0] != 2 {
		t.Fatalf("plan revisions=%v, want one declared two-suite addition", sink.added)
	}
	if len(sink.lifecycle) != 2 {
		t.Fatalf("terminal lifecycle=%+v, want one terminal per declared suite", sink.lifecycle)
	}
	for _, event := range sink.lifecycle {
		if event.Name != "extension.suite" || event.Phase != "end" || event.Target != "fixture" || event.OperationID == "" || event.Outcome != string(result.StatusPass) {
			t.Fatalf("unexpected declared-suite lifecycle=%+v", event)
		}
	}
}

func TestCollectorDerivesProfileStatusesAndPreservesPartialResults(t *testing.T) {
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{SchemaVersion: ProtocolVersion, Type: "manifest", Extension: ExtensionInfo{ID: "official-sdk.openai", Name: "Official SDK", Version: "test"}, ReportContracts: []string{"aiqa.report.v3"}, ProfileContracts: []string{"aiqa.profile.v3"}, Protocols: []string{"openai"}, Suites: []SuiteDescriptor{
		{ID: "chat.json", Subject: "model", Cost: "text", InternalConcurrency: 1},
		{ID: "models.get", Subject: "model", Cost: "text", InternalConcurrency: 1},
	}}
	subject := result.SubjectRef{TargetID: "openai", Protocol: "openai", Kind: "model", ID: "model-a"}
	request := RunRequest{SchemaVersion: ProtocolVersion, Type: "run_request", RunID: "run", Target: Target{ID: "openai", Protocol: "openai"}, Subject: subject, SuiteIDs: []string{"chat.json", "models.get"}, Credential: Credential{Type: "api_key", Value: "secret-token"}}
	collector, err := NewCollector(manifest, request, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := collector.Emit(SuiteEvent{SchemaVersion: ProtocolVersion, Type: "suite_result", RunID: "run", ExtensionID: "official-sdk.openai", SuiteID: "models.get", Subject: subject, Outcome: result.ExtensionOutcomeRequestRejected, DurationMS: 2, Summary: "unsupported secret-token", ErrorKind: "capability_rejection", HTTP: result.HTTPObservation{StatusCode: 404}}); err != nil {
		t.Fatal(err)
	}
	run := collector.Finish(result.ExtensionProvenance{ID: "official-sdk.openai", Source: "builtin"}, RunnerTelemetry{}, context.DeadlineExceeded, time.Now())
	if run.Runner.Status != result.StatusError || len(run.Suites) != 2 {
		t.Fatalf("partial run was not preserved: %+v", run)
	}
	if run.Suites[0].ErrorKind != "deadline_exceeded" {
		t.Fatalf("missing suite error kind=%q, want deadline_exceeded", run.Suites[0].ErrorKind)
	}
	if run.Suites[0].SuiteID != "chat.json" || run.Suites[0].Status != result.StatusError || run.Suites[1].Status != result.StatusUnsupported {
		t.Fatalf("unexpected derived statuses: %+v", run.Suites)
	}
	encoded, _ := json.Marshal(run)
	if strings.Contains(string(encoded), "secret-token") {
		t.Fatalf("credential leaked: %s", encoded)
	}
}

func TestCollectorTerminalDiagnosticFingerprintsStderrWithoutContent(t *testing.T) {
	subject := result.SubjectRef{TargetID: "target", Protocol: "openai", Kind: "model", ID: "model"}
	manifest := testManifest("fixture.terminal", "openai", SuiteDescriptor{ID: "echo", Subject: "model", Cost: "text", InternalConcurrency: 1})
	logger := &captureDiagnosticLogger{}
	collector, err := NewCollectorWithDiagnostics(manifest, RunRequest{SchemaVersion: ProtocolVersion, Type: "run_request", RunID: "run", Subject: subject, SuiteIDs: []string{"echo"}}, profile.Profile{}, logger)
	if err != nil {
		t.Fatal(err)
	}
	collector.Finish(result.ExtensionProvenance{ID: "fixture.terminal", Source: "executable"}, RunnerTelemetry{Stderr: "secret extension stderr"}, context.DeadlineExceeded, time.Now())
	var found bool
	for _, entry := range logger.entries {
		if entry.event != "extsuite.invocation.completed" {
			continue
		}
		found = true
		if entry.fields["error_kind"] != "deadline_exceeded" || entry.fields["stderr_bytes"] != len("secret extension stderr") || entry.fields["stderr_sha256"] == "" {
			t.Fatalf("unsafe terminal diagnostic: %+v", entry.fields)
		}
		if strings.Contains(fmt.Sprint(entry.fields), "secret extension stderr") {
			t.Fatalf("terminal event leaked stream: %+v", entry.fields)
		}
	}
	if !found {
		t.Fatal("missing terminal extension diagnostic")
	}
}

func TestCollectorTreatsRateLimitAsError(t *testing.T) {
	p, _ := profile.Load()
	manifest := Manifest{SchemaVersion: ProtocolVersion, Type: "manifest", Extension: ExtensionInfo{ID: "official-sdk.openai", Name: "Official SDK", Version: "test"}, ReportContracts: []string{"aiqa.report.v3"}, ProfileContracts: []string{"aiqa.profile.v3"}, Protocols: []string{"openai"}, Suites: []SuiteDescriptor{{ID: "models.get", Subject: "model", Cost: "text", InternalConcurrency: 1}}}
	subject := result.SubjectRef{TargetID: "openai", Protocol: "openai", Kind: "model", ID: "model-a"}
	request := RunRequest{SchemaVersion: ProtocolVersion, Type: "run_request", RunID: "run", Subject: subject, SuiteIDs: []string{"models.get"}}
	collector, _ := NewCollector(manifest, request, p)
	_ = collector.Emit(SuiteEvent{SchemaVersion: ProtocolVersion, Type: "suite_result", RunID: "run", ExtensionID: "official-sdk.openai", SuiteID: "models.get", Subject: subject, Outcome: result.ExtensionOutcomeRequestRejected, DurationMS: 1, Summary: "limited", ErrorKind: "capability_rejection", HTTP: result.HTTPObservation{StatusCode: 429}})
	run := collector.Finish(result.ExtensionProvenance{}, RunnerTelemetry{}, nil, time.Now())
	if run.Suites[0].Status != result.StatusError {
		t.Fatalf("429 status=%s, want error", run.Suites[0].Status)
	}
}

func TestCollectorDiagnosticTimelineIsStructuredAndSecretFree(t *testing.T) {
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	manifest := testManifest("fixture.diagnostics", "openai", SuiteDescriptor{ID: "echo", Subject: "model", Cost: "text", InternalConcurrency: 1})
	subject := result.SubjectRef{TargetID: "openai", Protocol: "openai", Kind: "model", ID: "model-secret-token"}
	logger := logging.New(4)
	collector, err := NewCollectorWithDiagnostics(manifest, RunRequest{SchemaVersion: ProtocolVersion, Type: "run_request", RunID: "run", Subject: subject, SuiteIDs: []string{"echo"}, Credential: Credential{Type: "api_key", Value: "secret-token"}}, p, logger)
	if err != nil {
		t.Fatal(err)
	}
	event := SuiteEvent{SchemaVersion: ProtocolVersion, Type: "suite_result", RunID: "run", ExtensionID: "fixture.diagnostics", SuiteID: "echo", Subject: subject, Outcome: result.ExtensionOutcomeRequestRejected, DurationMS: 7, Summary: "response body secret-token", Error: "Authorization: Bearer secret-token", ErrorKind: "capability_rejection", HTTP: result.HTTPObservation{StatusCode: 429, Snippet: "secret-token"}, Signals: []result.EvidenceSignal{{Code: "transport.rate-limited"}}}
	if err := collector.Emit(event); err != nil {
		t.Fatal(err)
	}
	_ = collector.Finish(result.ExtensionProvenance{ID: "fixture.diagnostics", Source: "builtin"}, RunnerTelemetry{TotalDuration: 9 * time.Millisecond}, nil, time.Now())
	lines := strings.Join(logger.Lines(), "\n")
	for _, forbidden := range []string{"secret-token", "response body", "Authorization", "model-secret-token"} {
		if strings.Contains(lines, forbidden) {
			t.Fatalf("diagnostic leaked %q: %s", forbidden, lines)
		}
	}
	for _, required := range []string{`"level":"TRACE"`, `"event":"extsuite.suite.result_parsed"`, "http_status", `"level":"DEBUG"`, `"event":"extsuite.suite.completed"`, `"status":"error"`, `"event":"extsuite.runner.telemetry"`} {
		if !strings.Contains(lines, required) {
			t.Fatalf("diagnostic omitted %q: %s", required, lines)
		}
	}
}

func TestExecuteBuiltinSuitesCreatesNamedSuiteProbeBranches(t *testing.T) {
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	manifest := testManifest("fixture.trace", "openai",
		SuiteDescriptor{ID: "first", Subject: "model", Cost: "text", InternalConcurrency: 1},
		SuiteDescriptor{ID: "second", Subject: "model", Cost: "text", InternalConcurrency: 1},
	)
	subject := result.SubjectRef{TargetID: "target", Protocol: "openai", Kind: "model", ID: "model"}
	logger := logging.New(4)
	tracing := telemetry.New(logger)
	defer tracing.Shutdown(context.Background())
	rootCtx, root := tracing.Start(context.Background(), "run", nil)
	defer root.End("completed", nil)
	request := RunRequest{SchemaVersion: ProtocolVersion, Type: "run_request", RunID: "run", Subject: subject, SuiteIDs: []string{"first", "second"}, Runtime: RuntimeServices{Executor: probe.NewExecutor(2), Telemetry: tracing, DiagnosticLogger: logger}}
	collector, err := NewCollectorWithDiagnostics(manifest, request, p, logger)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	var seenMu sync.Mutex
	err = ExecuteBuiltinSuites(rootCtx, request, "fixture.trace", collector, func(ctx context.Context, suiteID string) SuiteEvent {
		fields := logging.FieldsFromContext(ctx)
		probeID, _ := fields["probe"].(string)
		seenMu.Lock()
		seen[suiteID] = probeID
		seenMu.Unlock()
		logger.LogContext(ctx, logging.Trace, "fixture.http_called", logging.Fields{"suite": suiteID})
		return SuiteEvent{SchemaVersion: ProtocolVersion, Type: "suite_result", RunID: "run", ExtensionID: "fixture.trace", SuiteID: suiteID, Subject: subject, Outcome: result.ExtensionOutcomePass, DurationMS: 1, Summary: "ok"}
	})
	if err != nil {
		t.Fatal(err)
	}
	for suiteID, probeID := range seen {
		if probeID != "fixture.trace."+suiteID {
			t.Fatalf("suite %q received probe %q", suiteID, probeID)
		}
	}
	var records []map[string]any
	for _, line := range logger.Lines() {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	for _, suiteID := range request.SuiteIDs {
		probeID := "fixture.trace." + suiteID
		var suiteSpan, probeSpan string
		for _, record := range records {
			if record["event"] != "suite.start" || record["suite"] != suiteID {
				continue
			}
			suiteSpan, _ = record["span_id"].(string)
		}
		for _, record := range records {
			if record["event"] != "probe.start" || record["probe"] != probeID {
				continue
			}
			probeSpan, _ = record["span_id"].(string)
			if record["parent_span_id"] != suiteSpan {
				t.Fatalf("probe %q parent=%q, want suite span %q", probeID, record["parent_span_id"], suiteSpan)
			}
		}
		if suiteSpan == "" || probeSpan == "" {
			t.Fatalf("missing suite/probe starts for %q: %s", suiteID, strings.Join(logger.Lines(), "\n"))
		}
		for _, event := range []string{"fixture.http_called", "extsuite.suite.result_parsed", "extsuite.suite.completed", "probe.validator"} {
			matched := false
			for _, record := range records {
				if record["event"] == event && record["suite"] == suiteID && record["span_id"] == probeSpan {
					matched = true
				}
			}
			if !matched {
				t.Fatalf("missing correlated %s for suite %q", event, suiteID)
			}
		}
	}
}

func TestBuiltInAndExecutableEventsUseIdenticalValidation(t *testing.T) {
	p, _ := profile.Load()
	manifest := testManifest("fixture.shared", "openai", SuiteDescriptor{ID: "echo", Subject: "model", Cost: "text", InternalConcurrency: 1})
	subject := result.SubjectRef{TargetID: "openai", Protocol: "openai", Kind: "model", ID: "model-a"}
	request := RunRequest{SchemaVersion: ProtocolVersion, Type: "run_request", RunID: "same", Subject: subject, SuiteIDs: []string{"echo"}, Credential: Credential{Type: "api_key", Value: "secret"}}
	event := SuiteEvent{SchemaVersion: ProtocolVersion, Type: "suite_result", RunID: "same", ExtensionID: "fixture.shared", SuiteID: "echo", Subject: subject, Outcome: result.ExtensionOutcomeAssertionFailed, DurationMS: 7, Summary: "observable mismatch"}
	collect := func(source string) result.ExtensionRun {
		collector, err := NewCollector(manifest, request, p)
		if err != nil {
			t.Fatal(err)
		}
		if err := collector.Emit(event); err != nil {
			t.Fatal(err)
		}
		return collector.Finish(result.ExtensionProvenance{ID: "fixture.shared", Source: source}, RunnerTelemetry{}, nil, time.Now())
	}
	builtin, executable := collect("builtin"), collect("executable")
	if !reflect.DeepEqual(builtin.Suites, executable.Suites) {
		t.Fatalf("source kind changed validated suite evidence:\n builtin=%+v\n executable=%+v", builtin.Suites, executable.Suites)
	}
	if builtin.Suites[0].Status != result.StatusFail || builtin.Runner.PacingCompliance != "host_enforced" || executable.Runner.PacingCompliance != "extension_required_unverified" {
		t.Fatalf("unexpected shared-validator result: builtin=%+v executable=%+v", builtin, executable)
	}
}

func TestExecutableCandidatesRespectExplicitAndPATHPrecedence(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	for _, path := range []string{filepath.Join(first, "aiqa-demo-ext-suite"), filepath.Join(second, "aiqa-demo-ext-suite"), filepath.Join(second, "aiqa-other-ext-suite")} {
		if err := os.WriteFile(path, []byte("fixture"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	explicit := filepath.Join(first, "aiqa-explicit-ext-suite")
	if err := os.WriteFile(explicit, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	candidates, err := executableCandidates([]string{explicit}, strings.Join([]string{first, second}, string(os.PathListSeparator)))
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 3 || candidates[0] != explicit || candidates[1] != filepath.Join(first, "aiqa-demo-ext-suite") || candidates[2] != filepath.Join(second, "aiqa-other-ext-suite") {
		t.Fatalf("unexpected discovery order: %v", candidates)
	}
}

func TestRegistryRejectsDuplicateIDsAndLoaderRejectsNonExecutable(t *testing.T) {
	first := &testExtension{source: "builtin", manifest: testManifest("fixture.duplicate", "openai", SuiteDescriptor{ID: "one", Subject: "provider", Cost: "text", InternalConcurrency: 1})}
	second := &testExtension{source: "executable", manifest: testManifest("fixture.duplicate", "openai", SuiteDescriptor{ID: "two", Subject: "provider", Cost: "text", InternalConcurrency: 1})}
	if _, err := NewRegistry(first, second); err == nil || !strings.Contains(err.Error(), "duplicate extension id") {
		t.Fatalf("duplicate or shadowing extension ID was accepted: %v", err)
	}
	path := filepath.Join(t.TempDir(), "aiqa-invalid-ext-suite")
	if err := os.WriteFile(path, []byte("not executable"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadExecutable(context.Background(), path, HostInfo{Name: "aiqa", Version: "test"}); err == nil || !strings.Contains(err.Error(), "not an executable regular file") {
		t.Fatalf("non-executable candidate was accepted: %v", err)
	}
}

func TestCleanEnvironmentAndDiagnosticSanitizer(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "secret-token")
	t.Setenv("ANTHROPIC_API_KEY", "other-secret")
	for _, entry := range cleanEnvironment() {
		if strings.HasPrefix(entry, "OPENAI_API_KEY=") || strings.HasPrefix(entry, "ANTHROPIC_API_KEY=") {
			t.Fatalf("credential environment leaked: %q", entry)
		}
	}
	got := sanitizeDiagnostic([]byte(`Authorization: Bearer secret-token {"api_key":"other-secret"} https://example.test/a?sig=value`), "secret-token")
	if strings.Contains(got, "secret-token") || strings.Contains(got, "other-secret") || strings.Contains(got, "sig=value") {
		t.Fatalf("diagnostic was not sanitized: %s", got)
	}
}

func TestProtocolRejectsDuplicateAndOversizedRecords(t *testing.T) {
	p, _ := profile.Load()
	manifest := Manifest{SchemaVersion: ProtocolVersion, Type: "manifest", Extension: ExtensionInfo{ID: "official-sdk.openai", Name: "Official SDK", Version: "test"}, ReportContracts: []string{"aiqa.report.v3"}, ProfileContracts: []string{"aiqa.profile.v3"}, Protocols: []string{"openai"}, Suites: []SuiteDescriptor{{ID: "models.get", Subject: "model", Cost: "text", InternalConcurrency: 1}}}
	subject := result.SubjectRef{TargetID: "openai", Protocol: "openai", Kind: "model", ID: "model-a"}
	collector, _ := NewCollector(manifest, RunRequest{SchemaVersion: ProtocolVersion, Type: "run_request", RunID: "run", Subject: subject, SuiteIDs: []string{"models.get"}}, p)
	event := SuiteEvent{SchemaVersion: ProtocolVersion, Type: "suite_result", RunID: "run", ExtensionID: "official-sdk.openai", SuiteID: "models.get", Subject: subject, Outcome: result.ExtensionOutcomePass, DurationMS: 1, Summary: "ok"}
	if err := collector.Emit(event); err != nil {
		t.Fatal(err)
	}
	if err := collector.Emit(event); err == nil {
		t.Fatal("duplicate terminal result was accepted")
	}
	buffer := &boundedBuffer{limit: 3}
	_, _ = buffer.Write([]byte("oversized"))
	if !buffer.overflow || string(buffer.Bytes()) != "ove" {
		t.Fatalf("bounded output failed: overflow=%v data=%q", buffer.overflow, buffer.Bytes())
	}
	if _, err := decodeSuiteEvent([]byte{0xff, 0xfe}); err == nil {
		t.Fatal("invalid UTF-8 was accepted")
	}
}

func TestManifestRejectsProviderScopedBillableArtifact(t *testing.T) {
	manifest := testManifest("fixture.invalid", "openai", SuiteDescriptor{ID: "artifact", Subject: "provider", Cost: "billable_artifact", InternalConcurrency: 1})
	if err := ValidateManifest(&manifest); err == nil || !strings.Contains(err.Error(), "without a model subject") {
		t.Fatalf("provider-scoped billable suite was accepted: %v", err)
	}
}

func TestPublishedExecutableFixture(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "aiqa-fixture-ext-suite")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	command := exec.Command("go", "build", "-o", binary, "../../test/fixtures/ext-suite")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v\n%s", err, output)
	}
	extension, err := LoadExecutable(context.Background(), binary, HostInfo{Name: "aiqa", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	provenance := extension.Provenance()
	if !filepath.IsAbs(provenance.Path) || len(provenance.SHA256) != 64 {
		t.Fatalf("executable provenance is incomplete: %+v", provenance)
	}
	p, _ := profile.Load()
	subject := result.SubjectRef{TargetID: "openai", Protocol: "openai", Kind: "model", ID: "model-a"}
	request := RunRequest{SchemaVersion: ProtocolVersion, Type: "run_request", RunID: "fixture-run", Target: Target{ID: "openai", Protocol: "openai", BaseURL: "https://example.test/v1"}, Subject: subject, SuiteIDs: []string{"echo"}, Profile: ProfileRef{ID: p.ID, Revision: p.Revision}, Limits: Limits{RequestTimeoutMS: 1000, DeadlineMS: 1000, MaxConcurrency: 1, MaxOutputBytes: maxStdoutBytes}, Credential: Credential{Type: "api_key", Value: "fixture-secret"}}
	collector, err := NewCollector(extension.Manifest(), request, p)
	if err != nil {
		t.Fatal(err)
	}
	telemetry, runErr := extension.Run(context.Background(), request, collector)
	run := collector.Finish(extension.Provenance(), telemetry, runErr, time.Now())
	if run.Runner.Status != result.StatusPass || run.Runner.PacingCompliance != "extension_required_unverified" || len(run.Suites) != 1 || run.Suites[0].Status != result.StatusPass || run.Suites[0].Bound {
		t.Fatalf("fixture result: %+v", run)
	}
}

func TestExecutableFixturePreservesValidResultAroundMalformedOutput(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "aiqa-fixture-ext-suite")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if output, err := exec.Command("go", "build", "-o", binary, "../../test/fixtures/ext-suite").CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v\n%s", err, output)
	}
	extension, err := LoadExecutable(context.Background(), binary, HostInfo{Name: "aiqa", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := profile.Load()
	subject := result.SubjectRef{TargetID: "openai", Protocol: "openai", Kind: "model", ID: "model-a"}
	request := RunRequest{SchemaVersion: ProtocolVersion, Type: "run_request", RunID: "partial-run", Target: Target{ID: "openai", Protocol: "openai", BaseURL: "https://example.test/v1"}, Subject: subject, SuiteIDs: []string{"echo", "partial", "after"}, Profile: ProfileRef{ID: p.ID, Revision: p.Revision}, Limits: Limits{RequestTimeoutMS: 1000, DeadlineMS: 1000, MaxConcurrency: 1, MaxOutputBytes: maxStdoutBytes}, Credential: Credential{Type: "api_key", Value: "fixture-secret"}}
	collector, _ := NewCollector(extension.Manifest(), request, p)
	telemetry, runErr := extension.Run(context.Background(), request, collector)
	run := collector.Finish(extension.Provenance(), telemetry, runErr, time.Now())
	if run.Runner.Status != result.StatusError || len(run.Suites) != 3 || run.Suites[0].Status != result.StatusPass || run.Suites[1].Status != result.StatusError || run.Suites[2].Status != result.StatusPass {
		t.Fatalf("partial protocol result was lost: %+v", run)
	}
}

func TestExecutableFixturePreservesValidResultBeforeTimeout(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "aiqa-fixture-ext-suite")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if output, err := exec.Command("go", "build", "-o", binary, "../../test/fixtures/ext-suite").CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v\n%s", err, output)
	}
	extension, err := LoadExecutable(context.Background(), binary, HostInfo{Name: "aiqa", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := profile.Load()
	subject := result.SubjectRef{TargetID: "openai", Protocol: "openai", Kind: "model", ID: "model-a"}
	request := RunRequest{SchemaVersion: ProtocolVersion, Type: "run_request", RunID: "timeout-run", Target: Target{ID: "openai", Protocol: "openai", BaseURL: "https://example.test/v1"}, Subject: subject, SuiteIDs: []string{"echo", "timeout"}, Profile: ProfileRef{ID: p.ID, Revision: p.Revision}, Limits: Limits{RequestTimeoutMS: 25, DeadlineMS: 50, MaxConcurrency: 1, MaxOutputBytes: maxStdoutBytes}, Credential: Credential{Type: "api_key", Value: "fixture-secret"}}
	collector, _ := NewCollector(extension.Manifest(), request, p)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	telemetry, runErr := extension.Run(ctx, request, collector)
	run := collector.Finish(extension.Provenance(), telemetry, runErr, time.Now())
	if run.Runner.Status != result.StatusError || len(run.Suites) != 2 || run.Suites[0].Status != result.StatusPass || run.Suites[1].Status != result.StatusError {
		t.Fatalf("partial timeout result was lost: %+v", run)
	}
}

func FuzzDecodeSuiteEvent(f *testing.F) {
	f.Add([]byte(`{"schema_version":"aiqa.ext.v2","type":"suite_result","run_id":"r","extension_id":"fixture.contract","suite_id":"echo","subject":{"kind":"model","id":"m"},"outcome":"pass","duration_ms":1,"summary":"ok"}`))
	f.Add([]byte("not-json"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = decodeSuiteEvent(data)
	})
}

func TestCollectorMapsCooperativeExecutableTraceUnderHostInvocation(t *testing.T) {
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	logger := logging.New(4)
	provider := telemetry.New(logger)
	ctx, invocation := provider.Start(context.Background(), "extension.invocation", logging.Fields{"extension": "fixture.trace", "source": "executable"})
	defer invocation.End("pass", nil)
	subject := result.SubjectRef{TargetID: "openai", Protocol: "openai", Kind: "model", ID: "model-a"}
	request := RunRequest{SchemaVersion: ProtocolVersion, Type: "run_request", RunID: "trace-run", Subject: subject, SuiteIDs: []string{"echo"}, Runtime: RuntimeServices{Telemetry: provider, InvocationContext: ctx}}
	collector, err := NewCollector(testManifest("fixture.trace", "openai", SuiteDescriptor{ID: "echo", Subject: "model", Cost: "text", InternalConcurrency: 1}), request, p)
	if err != nil {
		t.Fatal(err)
	}
	events := []TraceEvent{
		{SchemaVersion: ProtocolVersion, Type: "trace_event", RunID: "trace-run", ExtensionID: "fixture.trace", SuiteID: "echo", EventID: "suite-1", Operation: "suite", Phase: "start"},
		{SchemaVersion: ProtocolVersion, Type: "trace_event", RunID: "trace-run", ExtensionID: "fixture.trace", SuiteID: "echo", EventID: "probe-1", ParentEventID: "suite-1", Operation: "probe", Phase: "start"},
		{SchemaVersion: ProtocolVersion, Type: "trace_event", RunID: "trace-run", ExtensionID: "fixture.trace", SuiteID: "echo", EventID: "request-1", ParentEventID: "probe-1", Operation: "logical_request", Phase: "start", Method: "POST"},
		{SchemaVersion: ProtocolVersion, Type: "trace_event", RunID: "trace-run", ExtensionID: "fixture.trace", SuiteID: "echo", EventID: "attempt-1", ParentEventID: "request-1", Operation: "attempt", Phase: "start", Method: "POST", Attempt: 1},
		{SchemaVersion: ProtocolVersion, Type: "trace_event", RunID: "trace-run", ExtensionID: "fixture.trace", SuiteID: "echo", EventID: "attempt-1", Operation: "attempt", Phase: "end", Status: 200, DurationMS: 1, Outcome: "pass"},
		{SchemaVersion: ProtocolVersion, Type: "trace_event", RunID: "trace-run", ExtensionID: "fixture.trace", SuiteID: "echo", EventID: "request-1", Operation: "logical_request", Phase: "end", DurationMS: 1, Outcome: "pass"},
		{SchemaVersion: ProtocolVersion, Type: "trace_event", RunID: "trace-run", ExtensionID: "fixture.trace", SuiteID: "echo", EventID: "validator-1", ParentEventID: "probe-1", Operation: "probe", Phase: "validator", Outcome: "pass"},
		{SchemaVersion: ProtocolVersion, Type: "trace_event", RunID: "trace-run", ExtensionID: "fixture.trace", SuiteID: "echo", EventID: "probe-1", Operation: "probe", Phase: "end", Outcome: "pass"},
		{SchemaVersion: ProtocolVersion, Type: "trace_event", RunID: "trace-run", ExtensionID: "fixture.trace", SuiteID: "echo", EventID: "suite-1", Operation: "suite", Phase: "end", Outcome: "pass"},
	}
	for _, event := range events {
		if err := collector.EmitTrace(event); err != nil {
			t.Fatalf("EmitTrace(%s/%s): %v", event.Operation, event.Phase, err)
		}
	}
	if err := collector.Emit(SuiteEvent{SchemaVersion: ProtocolVersion, Type: "suite_result", RunID: "trace-run", ExtensionID: "fixture.trace", SuiteID: "echo", Subject: subject, Outcome: result.ExtensionOutcomePass, DurationMS: 1, Summary: "ok"}); err != nil {
		t.Fatal(err)
	}
	run := collector.Finish(result.ExtensionProvenance{Source: "executable"}, RunnerTelemetry{}, nil, time.Now())
	if run.Runner.Status != result.StatusPass {
		t.Fatalf("runner=%+v", run.Runner)
	}
	joined := strings.Join(logger.Lines(), "\n")
	for _, want := range []string{"suite.start", "probe.start", "sdk.logical_request.start", "sdk.http_attempt.start", "probe.validator"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing host-mapped lifecycle %q in %s", want, joined)
		}
	}
}

func TestCollectorRejectsUnsafeAndUnfinishedCooperativeTrace(t *testing.T) {
	p, _ := profile.Load()
	subject := result.SubjectRef{Kind: "model", ID: "model-a"}
	request := RunRequest{SchemaVersion: ProtocolVersion, Type: "run_request", RunID: "run", Subject: subject, SuiteIDs: []string{"echo"}}
	collector, err := NewCollector(testManifest("fixture.trace", "openai", SuiteDescriptor{ID: "echo", Subject: "model", Cost: "text", InternalConcurrency: 1}), request, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeTraceEvent([]byte(`{"schema_version":"aiqa.ext.v2","type":"trace_event","run_id":"run","extension_id":"fixture.trace","suite_id":"echo","event_id":"x","operation":"suite","phase":"start","url":"https://secret.example"}`)); err == nil {
		t.Fatal("trace decoder accepted unsafe unknown field")
	}
	start := TraceEvent{SchemaVersion: ProtocolVersion, Type: "trace_event", RunID: "run", ExtensionID: "fixture.trace", SuiteID: "echo", EventID: "suite-1", Operation: "suite", Phase: "start"}
	if err := collector.EmitTrace(start); err != nil {
		t.Fatal(err)
	}
	if err := collector.Emit(SuiteEvent{SchemaVersion: ProtocolVersion, Type: "suite_result", RunID: "run", ExtensionID: "fixture.trace", SuiteID: "echo", Subject: subject, Outcome: result.ExtensionOutcomePass, DurationMS: 1, Summary: "ok"}); err == nil {
		t.Fatal("terminal result accepted unfinished extension trace")
	}
	run := collector.Finish(result.ExtensionProvenance{Source: "executable"}, RunnerTelemetry{}, nil, time.Now())
	if run.Runner.Status != result.StatusError || !strings.Contains(run.Runner.Error, "extension trace") {
		t.Fatalf("unfinished trace did not become runner evidence: %+v", run.Runner)
	}
}

type testExtension struct {
	manifest Manifest
	source   string
	run      func(context.Context, RunRequest, EventSink) (RunnerTelemetry, error)
}

func (e *testExtension) Manifest() Manifest { return e.manifest }
func (e *testExtension) Provenance() result.ExtensionProvenance {
	return result.ExtensionProvenance{ID: e.manifest.Extension.ID, Name: e.manifest.Extension.Name, Version: e.manifest.Extension.Version, Source: e.source, ProtocolVersion: ProtocolVersion}
}
func (e *testExtension) Run(ctx context.Context, request RunRequest, sink EventSink) (RunnerTelemetry, error) {
	return e.run(ctx, request, sink)
}

func testManifest(id, protocol string, suites ...SuiteDescriptor) Manifest {
	return Manifest{SchemaVersion: ProtocolVersion, Type: "manifest", Extension: ExtensionInfo{ID: id, Name: id, Version: "test"}, ReportContracts: []string{"aiqa.report.v3"}, ProfileContracts: []string{"aiqa.profile.v3"}, Protocols: []string{protocol}, Suites: suites}
}

func TestPlanRoutesCapabilitiesAndProtocolsWithoutUnrelatedSuites(t *testing.T) {
	openAI := &testExtension{source: "executable", manifest: testManifest("fixture.openai", "openai",
		SuiteDescriptor{ID: "inventory", Subject: "provider", Cost: "text", InternalConcurrency: 1},
		SuiteDescriptor{ID: "chat", Subject: "model", Capabilities: []string{"chat.completions.text"}, Cost: "text", InternalConcurrency: 1},
		SuiteDescriptor{ID: "image", Subject: "model", Capabilities: []string{"images.generations"}, Cost: "text", InternalConcurrency: 1},
	)}
	otherProtocol := &testExtension{source: "executable", manifest: testManifest("fixture.anthropic", "anthropic", SuiteDescriptor{ID: "messages", Subject: "model", Cost: "text", InternalConcurrency: 1})}
	registry, err := NewRegistry(openAI, otherProtocol)
	if err != nil {
		t.Fatal(err)
	}
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	models := []result.ModelReport{
		{ID: "unknown-text-default", Capabilities: []result.CapabilityAssessment{{ID: "chat.completions.text"}}},
		{ID: "image-only", Capabilities: []result.CapabilityAssessment{{ID: "images.generations"}}},
		{ID: "embedding-only", Capabilities: []result.CapabilityAssessment{{ID: "embeddings"}}},
	}
	plan := planTarget(registry, config.Config{Tests: []string{"ext-suite"}}, config.TargetConfig{ID: "openai", Protocol: config.ProtocolOpenAI, BaseURL: "https://example.test/v1"}, p, models)
	got := make([]string, 0, len(plan))
	for _, item := range plan {
		got = append(got, item.manifest.Extension.ID+":"+item.subject.ID+":"+strings.Join(item.suiteIDs, ","))
	}
	want := []string{
		"fixture.openai:image-only:image",
		"fixture.openai:unknown-text-default:chat",
		"fixture.openai:https://example.test/v1:inventory",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected extension plan:\n got %v\nwant %v", got, want)
	}
}

func TestPlanSDKSelectsBuiltinsWithoutExtSuitesAndDeduplicatesExplicitSelection(t *testing.T) {
	builtin := &testExtension{source: "builtin", manifest: testManifest("official-sdk.openai", "openai", SuiteDescriptor{ID: "chat", Subject: "model", Capabilities: []string{"chat.completions.text"}, Cost: "text", InternalConcurrency: 1})}
	executable := &testExtension{source: "executable", manifest: testManifest("fixture.external", "openai", SuiteDescriptor{ID: "external", Subject: "model", Capabilities: []string{"chat.completions.text"}, Cost: "text", InternalConcurrency: 1})}
	registry, err := NewRegistry(builtin, executable)
	if err != nil {
		t.Fatal(err)
	}
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	target := config.TargetConfig{ID: "openai", Protocol: config.ProtocolOpenAI, BaseURL: "https://example.test/v1"}
	models := []result.ModelReport{{ID: "text", Capabilities: []result.CapabilityAssessment{{ID: "chat.completions.text"}}}}

	sdkOnly := planTarget(registry, config.Config{Tests: []string{"sdk"}}, target, p, models)
	if len(sdkOnly) != 1 || sdkOnly[0].manifest.Extension.ID != "official-sdk.openai" {
		t.Fatalf("sdk plan=%+v, want only the built-in SDK", sdkOnly)
	}
	combined := planTarget(registry, config.Config{Tests: []string{"sdk", "ext-suite"}, ExtSuites: []string{"fixture.external"}}, target, p, models)
	if len(combined) != 2 {
		t.Fatalf("combined plan=%+v, want one built-in and one explicit extension", combined)
	}
	seen := map[string]bool{}
	for _, item := range combined {
		if seen[item.manifest.Extension.ID] {
			t.Fatalf("extension %q was planned more than once", item.manifest.Extension.ID)
		}
		seen[item.manifest.Extension.ID] = true
	}
	extOnly := planTarget(registry, config.Config{Tests: []string{"ext-suite"}}, target, p, models)
	if len(extOnly) != 1 || extOnly[0].manifest.Extension.ID != "fixture.external" {
		t.Fatalf("external-only plan=%+v, want only the executable extension", extOnly)
	}
}

func TestValidateExecutableSelectionRejectsBuiltin(t *testing.T) {
	builtin := &testExtension{source: "builtin", manifest: testManifest("official-sdk.openai", "openai", SuiteDescriptor{ID: "chat", Subject: "model", Cost: "text", InternalConcurrency: 1})}
	registry, err := NewRegistry(builtin)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateExecutableSelection(registry, []string{"official-sdk.openai"}); err == nil {
		t.Fatal("built-in SDK was accepted as an external executable")
	}
}

func TestPlanConsumesPerModelArtifactBudgetAcrossSuites(t *testing.T) {
	extension := &testExtension{source: "executable", manifest: testManifest("fixture.artifacts", "openai",
		SuiteDescriptor{ID: "image-a", Subject: "model", Capabilities: []string{"images.generations"}, Cost: "billable_artifact", InternalConcurrency: 1},
		SuiteDescriptor{ID: "image-b", Subject: "model", Capabilities: []string{"images.generations"}, Cost: "billable_artifact", InternalConcurrency: 1},
	)}
	registry, err := NewRegistry(extension)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := profile.Load()
	model := result.ModelReport{ID: "image-model", Capabilities: []result.CapabilityAssessment{{ID: "images.generations"}}, Modules: []result.ModuleAssessment{{ID: "images-generations", Execution: result.ExecutionExecuted}}}
	target := config.TargetConfig{ID: "openai", Protocol: config.ProtocolOpenAI, BaseURL: "https://example.test/v1"}
	extOnly := planTarget(registry, config.Config{Tests: []string{"ext-suite"}, AllowBillableArtifacts: true, MaxArtifactsPerModel: 1}, target, p, []result.ModelReport{model})
	if len(extOnly) != 1 || len(extOnly[0].suiteIDs) != 1 || extOnly[0].suiteIDs[0] != "image-a" {
		t.Fatalf("extension budget was not consumed deterministically: %+v", extOnly)
	}
	combined := planTarget(registry, config.Config{Tests: []string{"compat", "ext-suite"}, AllowBillableArtifacts: true, MaxArtifactsPerModel: 1}, target, p, []result.ModelReport{model})
	if len(combined) != 0 {
		t.Fatalf("combined run exceeded the artifact budget: %+v", combined)
	}
}

func TestExecutableReservationsShareGlobalExecutorAndReceiveCurrentLimits(t *testing.T) {
	var active, maximum atomic.Int32
	updateMaximum := func(current int32) {
		for {
			seen := maximum.Load()
			if current <= seen || maximum.CompareAndSwap(seen, current) {
				return
			}
		}
	}
	var received Limits
	externalManifest := testManifest("fixture.external", "openai", SuiteDescriptor{ID: "external", Subject: "provider", Cost: "text", InternalConcurrency: 2})
	external := &testExtension{source: "executable", manifest: externalManifest}
	external.run = func(_ context.Context, request RunRequest, sink EventSink) (RunnerTelemetry, error) {
		received = request.Limits
		current := active.Add(2)
		updateMaximum(current)
		time.Sleep(20 * time.Millisecond)
		active.Add(-2)
		event := SuiteEvent{SchemaVersion: ProtocolVersion, Type: "suite_result", RunID: request.RunID, ExtensionID: externalManifest.Extension.ID, SuiteID: "external", Subject: request.Subject, Outcome: result.ExtensionOutcomePass, DurationMS: 1, Summary: "ok"}
		return RunnerTelemetry{}, sink.Emit(event)
	}
	builtinManifest := testManifest("fixture.builtin", "openai", SuiteDescriptor{ID: "builtin", Subject: "provider", Cost: "text", InternalConcurrency: 1})
	builtin := &testExtension{source: "builtin", manifest: builtinManifest}
	builtin.run = func(_ context.Context, request RunRequest, sink EventSink) (RunnerTelemetry, error) {
		var emitErr error
		request.Runtime.Executor.Do(func() {
			current := active.Add(1)
			updateMaximum(current)
			time.Sleep(20 * time.Millisecond)
			active.Add(-1)
			event := SuiteEvent{SchemaVersion: ProtocolVersion, Type: "suite_result", RunID: request.RunID, ExtensionID: builtinManifest.Extension.ID, SuiteID: "builtin", Subject: request.Subject, Outcome: result.ExtensionOutcomePass, DurationMS: 1, Summary: "ok"}
			emitErr = sink.Emit(event)
		})
		return RunnerTelemetry{}, emitErr
	}
	registry, err := NewRegistry(external, builtin)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := profile.Load()
	client, err := api.NewClient("https://example.test/v1", "secret", "test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	client.SetConcurrency(2)
	logger := logging.New(4)
	runs := ExecuteTargetWithDiagnostics(context.Background(), registry, config.Config{Tests: []string{"sdk", "ext-suite"}, Timeout: time.Second, Concurrency: 2}, config.TargetConfig{ID: "openai", Protocol: config.ProtocolOpenAI, BaseURL: "https://example.test/v1", APIKey: "secret", RequestInterval: time.Millisecond}, p, nil, client, probe.NewExecutor(2), logger)
	if len(runs) != 2 || maximum.Load() > 2 {
		t.Fatalf("runs=%d maximum concurrency=%d, want two runs and ceiling 2", len(runs), maximum.Load())
	}
	if received.MaxConcurrency != 2 || received.RetryLimit != 2 || received.ObservedSafeRequestsPerSecond <= 0 || received.ObservedSafeConcurrency < 1 || received.ObservedSafeConcurrency > received.MaxConcurrency {
		t.Fatalf("external limits do not describe current host policy: %+v", received)
	}
	lines := strings.Join(logger.Lines(), "\n")
	for _, event := range []string{"extsuite.invocation.started", "extsuite.invocation.plan", "extsuite.suite.scheduled", "extsuite.suite.result_parsed", "extsuite.suite.completed", "extsuite.runner.telemetry", "extsuite.invocation.completed"} {
		if !strings.Contains(lines, event) {
			t.Fatalf("missing diagnostic event %q: %s", event, lines)
		}
	}
	if strings.Contains(lines, "https://example.test") || strings.Contains(lines, "secret") {
		t.Fatalf("timeline leaked sensitive runtime data: %s", lines)
	}
}
