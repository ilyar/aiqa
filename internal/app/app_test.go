package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

func TestNewForensicRunIDUsesCanonicalAIQANamespace(t *testing.T) {
	id, err := newForensicRunID()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "aiqa-") || len(id) != len("aiqa-")+32 {
		t.Fatalf("run ID=%q, want aiqa-<32 lowercase hex characters>", id)
	}
}

func TestRunShowsPlainProgressWithoutVerboseLog(t *testing.T) {
	server := newMockOpenAI(t)
	defer server.Close()
	withTempWorkingDirectory(t)
	var output bytes.Buffer
	oldWriter, oldTTY := progressWriter, progressTTY
	progressWriter, progressTTY = &output, func() bool { return false }
	t.Cleanup(func() { progressWriter, progressTTY = oldWriter, oldTTY })
	cfg := testConfig(server.URL+"/v1", []string{"compat"})
	cfg.Name = "progress"
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil || out.ExitCode != 0 {
		t.Fatalf("run: exit=%d err=%v", out.ExitCode, err)
	}
	text := output.String()
	for _, want := range []string{"aiqa: Planning 1 target(s)", "Discovering openai", "Planned ", "logical probe(s)", "completed; reports:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("progress output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"schema_version":"aiqa.trace.v1"`) || strings.Contains(text, "\r") {
		t.Fatalf("non-TTY progress leaked JSONL or terminal control: %q", text)
	}
	if _, err := os.Stat("progress.log"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("verbosity 0 log exists or stat failed: %v", err)
	}
}

func TestRunNoProgressSuppressesOperatorOutput(t *testing.T) {
	server := newMockOpenAI(t)
	defer server.Close()
	withTempWorkingDirectory(t)
	var output bytes.Buffer
	oldWriter, oldTTY := progressWriter, progressTTY
	progressWriter, progressTTY = &output, func() bool { return false }
	t.Cleanup(func() { progressWriter, progressTTY = oldWriter, oldTTY })
	cfg := testConfig(server.URL+"/v1", []string{"compat"})
	cfg.Formats = nil
	cfg.NoProgress = true
	if out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"}); err != nil || out.ExitCode != 0 {
		t.Fatalf("run: exit=%d err=%v", out.ExitCode, err)
	}
	if output.Len() != 0 {
		t.Fatalf("--no-progress wrote stderr: %q", output.String())
	}
}

func TestProgressTTYDisablesRedrawForDumbTerminalAndCI(t *testing.T) {
	for _, environment := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "dumb terminal", key: "TERM", value: "dumb"},
		{name: "CI", key: "CI", value: "1"},
	} {
		t.Run(environment.name, func(t *testing.T) {
			t.Setenv("TERM", "xterm-256color")
			t.Setenv("CI", "")
			t.Setenv(environment.key, environment.value)
			if progressTTY() {
				t.Fatalf("progressTTY() = true with %s=%q", environment.key, environment.value)
			}
		})
	}
}

func TestProgressTTYRequiresInteractiveStderr(t *testing.T) {
	old := stderrIsTerminal
	stderrIsTerminal = func() bool { return false }
	t.Cleanup(func() { stderrIsTerminal = old })
	t.Setenv("CI", "")
	t.Setenv("TERM", "xterm-256color")
	if progressTTY() {
		t.Fatal("progressTTY() accepted a non-terminal stderr device")
	}
}

func TestRunCompatibilityWritesReports(t *testing.T) {
	server := newMockOpenAI(t)
	defer server.Close()
	withTempWorkingDirectory(t)
	cfg := testConfig(server.URL+"/v1", []string{"compat"})
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ExitCode != 0 {
		t.Fatalf("unexpected exit code %d; summary=%+v", out.ExitCode, out.ReportV3.Summary)
	}
	for _, path := range []string{"report.md", "report.json"} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing %s: %v", path, err)
		}
	}
	data, err := os.ReadFile("report.json")
	if err != nil {
		t.Fatal(err)
	}
	var decoded result.ReportV3
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Summary map[string]json.RawMessage `json:"summary"`
		Targets []struct {
			Summary map[string]json.RawMessage `json:"summary"`
			Models  []struct {
				Summary map[string]json.RawMessage `json:"summary"`
			} `json:"models"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"score", "score_method", "score_deprecated"} {
		if _, exists := raw.Summary[field]; exists {
			t.Fatalf("legacy scoring field %q remains in report summary", field)
		}
		for _, target := range raw.Targets {
			if _, exists := target.Summary[field]; exists {
				t.Fatalf("legacy scoring field %q remains in target summary", field)
			}
			for _, model := range target.Models {
				if _, exists := model.Summary[field]; exists {
					t.Fatalf("legacy scoring field %q remains in model summary", field)
				}
			}
		}
	}
	if len(decoded.Targets) != 1 || len(decoded.Targets[0].Models) != 1 || decoded.Targets[0].Models[0].ID != "vendor/model-a" {
		t.Fatalf("unexpected targets/models: %#v", decoded.Targets)
	}
	if decoded.Execution == nil || decoded.Execution.SchemaVersion != "aiqa.execution-summary.v1" || decoded.Execution.PlannedJobs == 0 || decoded.Execution.FinishedJobs != decoded.Execution.PlannedJobs || decoded.Execution.CancelledJobs != 0 {
		t.Fatalf("completed run lacks truthful execution summary: %+v", decoded.Execution)
	}
	target := decoded.Targets[0]
	if target.Summary.RequiredFail != 0 {
		t.Fatalf("core tests failed: %+v", target.Summary)
	}
	var basicUsage, usageCheck result.Usage
	for _, probe := range target.Models[0].Compatibility {
		switch probe.ID {
		case "chat.basic":
			basicUsage = probe.Usage
		case "chat.usage":
			usageCheck = probe.Usage
		}
	}
	if basicUsage.TotalTokens != 0 || usageCheck.TotalTokens == 0 {
		t.Fatalf("basic response usage must be counted once by chat.usage: basic=%+v usage=%+v", basicUsage, usageCheck)
	}
	if target.Summary.Fail != 0 || target.Summary.Error != 0 || target.Summary.Warn != 0 || target.Summary.Unsupported != 0 {
		t.Fatalf("mock endpoint should pass every built-in compatibility probe: %+v", target.Summary)
	}
	if target.Summary.Assessment == nil || target.Summary.Assessment.Verdict != "pass" || len(target.Requirements) == 0 {
		t.Fatalf("canonical assessment is missing or incorrect: summary=%+v requirements=%d", target.Summary, len(target.Requirements))
	}
	if target.Guidance.Status != "complete" || target.Guidance.SchemaVersion != "aiqa.guidance.v1" {
		t.Fatalf("guidance contract is missing: %+v", target.Guidance)
	}
	if strings.Contains(string(data), "test-key") {
		t.Fatal("API key leaked into JSON report")
	}
}

func TestForensicSidecarsExistWithPrivateModesBeforeFirstHTTPRequest(t *testing.T) {
	withTempWorkingDirectory(t)
	observed := make(chan error, 1)
	server := newMockOpenAIWithMiddleware(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, path := range []string{"before-request.log", "before-request.trace-verdict.json"} {
				info, err := os.Stat(path)
				if err != nil {
					select {
					case observed <- fmt.Errorf("%s absent at first request: %w", path, err):
					default:
					}
					break
				}
				if info.Mode().Perm() != 0o600 {
					select {
					case observed <- fmt.Errorf("%s mode=%#o, want 0600", path, info.Mode().Perm()):
					default:
					}
					break
				}
			}
			select {
			case observed <- nil:
			default:
			}
			next.ServeHTTP(w, r)
		})
	})
	defer server.Close()
	cfg := testConfig(server.URL+"/v1", []string{"compat"})
	cfg.Formats = nil
	cfg.Verbosity, cfg.Name = 4, "before-request"
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil || out.ExitCode != 0 {
		t.Fatalf("forensic run: exit=%d err=%v", out.ExitCode, err)
	}
	select {
	case err := <-observed:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("fixture did not observe an HTTP request")
	}
}

func TestVerbosityBelowFourDoesNotCreateForensicVerdict(t *testing.T) {
	server := newMockOpenAI(t)
	defer server.Close()
	withTempWorkingDirectory(t)
	for _, verbosity := range []int{0, 1, 2, 3} {
		t.Run(fmt.Sprintf("level-%d", verbosity), func(t *testing.T) {
			cfg := testConfig(server.URL+"/v1", []string{"compat"})
			cfg.Formats = nil
			cfg.Verbosity = verbosity
			cfg.Name = fmt.Sprintf("below-forensic-%d", verbosity)
			out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
			if err != nil || out.ExitCode != 0 {
				t.Fatalf("verbosity %d run: exit=%d err=%v", verbosity, out.ExitCode, err)
			}
			if _, err := os.Stat(cfg.Name + ".trace-verdict.json"); !os.IsNotExist(err) {
				t.Fatalf("verbosity %d unexpectedly created forensic verdict: %v", verbosity, err)
			}
		})
	}
}

func TestCompatibilityOnlyDoesNotDiscoverPATHConstructedExtensions(t *testing.T) {
	server := newMockOpenAI(t)
	defer server.Close()
	pathDir := t.TempDir()
	broken := filepath.Join(pathDir, "aiqa-broken-ext-suite")
	if err := os.WriteFile(broken, []byte("not an executable format"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathDir)
	withTempWorkingDirectory(t)
	cfg := testConfig(server.URL+"/v1", []string{"compat"})
	cfg.Formats = nil
	if _, err := Run(context.Background(), cfg, BuildInfo{Version: "test"}); err != nil {
		t.Fatalf("compat-only run touched PATH extension candidate: %v", err)
	}
}

func TestSDKSelectionDoesNotDiscoverPATHConstructedExtensions(t *testing.T) {
	server := newMockOpenAI(t)
	defer server.Close()
	pathDir := t.TempDir()
	broken := filepath.Join(pathDir, "aiqa-broken-ext-suite")
	if err := os.WriteFile(broken, []byte("not an executable format"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathDir)
	cfg := testConfig(server.URL+"/v1", []string{"compat", "sdk"})
	cfg.Formats = nil
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatalf("SDK selection touched PATH extension candidate: %v", err)
	}
	for _, extension := range out.ReportV3.Targets[0].Extensions {
		if extension.Provenance.Source == "executable" {
			t.Fatalf("SDK selection unexpectedly ran executable extension: %+v", extension.Provenance)
		}
	}
}

func TestUnavailableSelectedExtensionIsConfigurationError(t *testing.T) {
	server := newMockOpenAI(t)
	defer server.Close()
	t.Setenv("PATH", t.TempDir())
	cfg := testConfig(server.URL+"/v1", []string{"ext-suite"})
	cfg.ExtSuites = []string{"missing.extension"}
	cfg.Formats = nil
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err == nil || out.ExitCode != 2 || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("unexpected missing-extension result: exit=%d err=%v", out.ExitCode, err)
	}
}

func TestExecutableExtensionProducesVerifiableForensicCoverage(t *testing.T) {
	server := newMockOpenAI(t)
	defer server.Close()
	fixtureDir := t.TempDir()
	binary := filepath.Join(fixtureDir, "aiqa-fixture-ext-suite")
	build := exec.Command("go", "build", "-o", binary, "../../test/fixtures/ext-suite")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build executable extension fixture: %v\n%s", err, output)
	}
	t.Setenv("PATH", fixtureDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	withTempWorkingDirectory(t)
	cfg := testConfig(server.URL+"/v1", []string{"compat", "ext-suite"})
	cfg.ExtSuites = []string{"fixture.contract"}
	cfg.Formats = nil
	cfg.Verbosity = 4
	cfg.Name = "executable-forensic"
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil || out.ExitCode != 0 {
		t.Fatalf("executable extension run failed: exit=%d err=%v", out.ExitCode, err)
	}
	data, readErr := os.ReadFile("executable-forensic.trace-verdict.json")
	if readErr != nil {
		t.Fatalf("read executable forensic verdict: %v", readErr)
	}
	var verdict struct {
		Status   string `json:"status"`
		Coverage []struct {
			Target, Protocol, Executor, Status string
		} `json:"coverage"`
	}
	if err := json.Unmarshal(data, &verdict); err != nil {
		t.Fatalf("decode executable forensic verdict: %v", err)
	}
	if verdict.Status != "pass" {
		t.Fatalf("executable forensic verdict=%s", data)
	}
	coverage := map[string]string{}
	for _, item := range verdict.Coverage {
		if item.Target == "openai" && item.Protocol == "openai" {
			coverage[item.Executor] = item.Status
		}
	}
	if coverage["native"] != "complete" || coverage["extension"] != "complete" || coverage["sdk"] != "not_selected" {
		t.Fatalf("executable forensic coverage=%v", coverage)
	}
}

func TestBuiltInExtensionRequestsShareGlobalConcurrencyCeiling(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	server := newMockOpenAIWithMiddleware(t, func(next http.Handler) http.Handler {
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
	t.Setenv("PATH", t.TempDir())
	cfg := testConfig(server.URL+"/v1", []string{"sdk"})
	cfg.Concurrency = 3
	cfg.Formats = nil
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil || out.ExitCode != 0 {
		t.Fatalf("extension run failed: exit=%d err=%v", out.ExitCode, err)
	}
	if got := maximum.Load(); got < 1 || got > 3 {
		t.Fatalf("maximum concurrent extension requests=%d, want 1..3", got)
	}
}

func TestRunFreezesParallelPreparationBeforeParallelTargetExecution(t *testing.T) {
	var prepareRequests atomic.Int32
	var executeRequests atomic.Int32
	prepareBarrier := make(chan struct{})
	executeBarrier := make(chan struct{})
	server := newMockOpenAIWithMiddleware(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v1/models":
				if prepareRequests.Add(1) == 2 {
					close(prepareBarrier)
				}
				select {
				case <-prepareBarrier:
				case <-r.Context().Done():
					return
				}
			case "/v1/chat/completions":
				if executeRequests.Add(1) == 2 {
					close(executeBarrier)
				}
				select {
				case <-executeBarrier:
				case <-r.Context().Done():
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	defer server.Close()
	withTempWorkingDirectory(t)
	cfg := testConfig(server.URL+"/v1", []string{"compat"})
	cfg.Formats = nil
	// This test asserts target-workflow overlap. Physical HTTP admission is
	// intentionally global, so grant both discovery requests a real C=2 slot.
	cfg.Concurrency = 2
	cfg.TargetConcurrency = 2
	// This fixture exercises independent target workflows. The same loopback
	// origin is only a test convenience; production targets would be distinct
	// origins, so keep their quota groups independent explicitly.
	cfg.PacingGroups = []string{"alpha=alpha-test", "zeta=zeta-test"}
	cfg.Targets = []config.TargetConfig{
		{ID: "zeta", Protocol: config.ProtocolOpenAI, BaseURL: server.URL + "/v1", APIKey: "test-key", ProfileFile: profile.DefaultFile, Modules: []string{"auto"}, UnknownModel: "inventory", RequestInterval: time.Millisecond},
		{ID: "alpha", Protocol: config.ProtocolOpenAI, BaseURL: server.URL + "/v1", APIKey: "test-key", ProfileFile: profile.DefaultFile, Modules: []string{"auto"}, UnknownModel: "inventory", RequestInterval: time.Millisecond},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := Run(ctx, cfg, BuildInfo{Version: "test"})
	if err != nil || out.ExitCode != 0 {
		t.Fatalf("parallel run: exit=%d err=%v", out.ExitCode, err)
	}
	if prepareRequests.Load() < 2 || executeRequests.Load() < 2 {
		t.Fatalf("barriers did not observe overlapping workflows: prepare=%d execute=%d", prepareRequests.Load(), executeRequests.Load())
	}
	if got := []string{out.ReportV3.Targets[0].ID, out.ReportV3.Targets[1].ID}; !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
		t.Fatalf("report target order=%v, want deterministic ID order", got)
	}
}

func TestCombinedNativeAndExtensionRunProducesOneAssessment(t *testing.T) {
	server := newMockOpenAI(t)
	defer server.Close()
	t.Setenv("PATH", t.TempDir())
	cfg := testConfig(server.URL+"/v1", []string{"compat", "sdk"})
	cfg.Concurrency = 3
	cfg.Formats = nil
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil || out.ReportV3 == nil || len(out.ReportV3.Targets) != 1 {
		t.Fatalf("combined run failed: exit=%d err=%v report=%+v", out.ExitCode, err, out.ReportV3)
	}
	target := out.ReportV3.Targets[0]
	if len(target.Models) != 1 || len(target.Models[0].Compatibility) == 0 || len(target.Extensions) == 0 {
		t.Fatalf("combined evidence is incomplete: models=%d native=%d extensions=%d", len(target.Models), len(target.Models[0].Compatibility), len(target.Extensions))
	}
	if target.Summary.Assessment == nil || target.Summary.Assessment.Verdict != "pass" {
		t.Fatalf("combined assessment is not authoritative: %+v", target.Summary.Assessment)
	}
	if got := strings.Join(out.ReportV3.Config.Tests, ","); got != "compat,sdk" {
		t.Fatalf("report did not preserve SDK selector: %q", got)
	}
	if len(out.ReportV3.Config.ExtSuites) != 0 {
		t.Fatalf("SDK selector leaked a hidden extension selection into the report: %q", strings.Join(out.ReportV3.Config.ExtSuites, ","))
	}
	seen := map[string]bool{}
	for _, requirement := range target.Requirements {
		key := requirement.Subject.Kind + "\x00" + requirement.Subject.ID + "\x00" + requirement.ID
		if seen[key] {
			t.Fatalf("canonical requirement was counted more than once: %s", requirement.ID)
		}
		seen[key] = true
	}
}

func TestVerboseFourProducesPassingBuiltInSDKTraceVerdict(t *testing.T) {
	server := newMockOpenAI(t)
	defer server.Close()
	withTempWorkingDirectory(t)
	cfg := testConfig(server.URL+"/v1", []string{"compat", "sdk"})
	cfg.Formats = nil
	cfg.Name = "sdk-trace"
	cfg.Verbosity = 4
	cfg.Concurrency = 3
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil || out.ExitCode != 0 {
		t.Fatalf("combined SDK trace run failed: exit=%d err=%v", out.ExitCode, err)
	}
	data, err := os.ReadFile("sdk-trace.trace-verdict.json")
	if err != nil {
		t.Fatalf("read SDK trace verdict: %v", err)
	}
	var verdict struct {
		Status              string `json:"status"`
		CheckedHTTPAttempts int    `json:"checked_http_attempts"`
	}
	if err := json.Unmarshal(data, &verdict); err != nil {
		t.Fatalf("decode SDK trace verdict: %v", err)
	}
	if verdict.Status != "pass" || verdict.CheckedHTTPAttempts == 0 {
		t.Fatalf("SDK trace verdict=%s", data)
	}
	if out.ReportV3.ForensicTrace == nil || out.ReportV3.ForensicTrace.Status != "pass" || out.ReportV3.ForensicTrace.VerdictPath != "sdk-trace.trace-verdict.json" {
		t.Fatalf("public forensic trace reference=%+v", out.ReportV3.ForensicTrace)
	}
}

func TestRunCompatibilityAppliesConcurrencyToProbes(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	server := newMockOpenAIWithMiddleware(t, func(next http.Handler) http.Handler {
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
	cfg := testConfig(server.URL+"/v1", []string{"compat"})
	cfg.Concurrency = 4
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ReportV3.Targets[0].Summary.Fail != 0 || out.ReportV3.Targets[0].Summary.Error != 0 {
		t.Fatalf("parallel run changed compatibility results: %+v", out.ReportV3.Targets[0].Summary)
	}
	if got := maximum.Load(); got < 2 || got > 4 {
		t.Fatalf("maximum concurrent HTTP requests=%d, want 2..4", got)
	}
}

func TestGenerateExampleReports(t *testing.T) {
	dir := os.Getenv("AIQA_EXAMPLE_DIR")
	if dir == "" {
		t.Skip("set AIQA_EXAMPLE_DIR to generate example reports")
	}
	server := newMockOpenAI(t)
	defer server.Close()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	cfg := testConfig(server.URL+"/v1", []string{"compat", "model-param"})
	cfg.Name = "mock-report"
	cfg.ContextMaxTokens = 10_000
	cfg.ContextPrecision = 512
	cfg.ContextBudget = 50_000
	cfg.ContextMaxBytes = 16 * 1024
	out, err := Run(context.Background(), cfg, BuildInfo{
		Version: "1.0.0-alpha.4-example",
		Commit:  "example",
		Date:    "2026-08-30T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ExitCode != 0 {
		t.Fatalf("example run failed: %+v", out.ReportV3.Summary)
	}
}

func TestRunModelParametersFindsContextBoundary(t *testing.T) {
	server := newMockOpenAI(t)
	defer server.Close()
	withTempWorkingDirectory(t)
	cfg := testConfig(server.URL+"/v1", []string{"model-param"})
	cfg.Models = []string{"vendor/model-a"}
	cfg.Formats = []string{"json"}
	cfg.Samples = 2
	cfg.ContextMaxTokens = 10_000
	cfg.ContextPrecision = 512
	cfg.ContextBudget = 50_000
	cfg.ContextStartBytes = 1024
	cfg.ContextMaxBytes = 16 * 1024
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.ReportV3.Targets[0].Models) != 1 {
		t.Fatalf("unexpected model count: %d", len(out.ReportV3.Targets[0].Models))
	}
	model := out.ReportV3.Targets[0].Models[0]
	if model.Context == nil || model.Context.LowerBoundTokens <= 0 || model.Context.UpperBoundTokens <= model.Context.LowerBoundTokens {
		t.Fatalf("context boundary not found: %+v", model.Context)
	}
	if len(model.Parameters) < 20 {
		t.Fatalf("too few parameter results: %d", len(model.Parameters))
	}
	if model.Summary.Fail != 0 || model.Summary.Error != 0 || model.Summary.Warn != 0 || model.Summary.Unsupported != 0 {
		t.Fatalf("mock model should pass every parameter probe: %+v", model.Summary)
	}
}

func TestMixedNonChatCatalogDoesNotReceiveChatRequests(t *testing.T) {
	var modelListRequests, chatRequests atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		modelListRequests.Add(1)
		writeJSON(w, 200, map[string]any{"object": "list", "data": []any{
			map[string]any{"id": "gpt-image-2", "object": "model", "created": 1, "is_image": true},
			map[string]any{"id": "opaque-model", "object": "model", "created": 1},
		}})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		chatRequests.Add(1)
		writeError(w, 500, "unexpected", "chat must not be called")
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	withTempWorkingDirectory(t)
	cfg := testConfig(server.URL+"/v1", []string{"compat", "model-param"})
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if got := chatRequests.Load(); got != 0 {
		t.Fatalf("non-chat catalog received %d chat requests", got)
	}
	if got := modelListRequests.Load(); got != 1 {
		t.Fatalf("inventory-only catalog used %d model-list requests, want 1", got)
	}
	if len(out.ReportV3.Targets[0].Models) != 2 {
		t.Fatalf("inventory lost models: %+v", out.ReportV3.Targets[0].Models)
	}
	for _, model := range out.ReportV3.Targets[0].Models {
		wantVerdict := "not-evaluated"
		if model.ID == "opaque-model" {
			wantVerdict = "inconclusive"
		}
		if model.Assessment.Verdict != wantVerdict {
			t.Fatalf("model %s verdict=%q, want %q", model.ID, model.Assessment.Verdict, wantVerdict)
		}
		chat := moduleByID(model.Modules, "chat-completions")
		if model.ID == "gpt-image-2" && (chat == nil || chat.Applicability != result.ApplicabilityNotApplicable) {
			t.Fatalf("image model chat applicability: %+v", chat)
		}
		if model.ID == "opaque-model" && (chat == nil || chat.Applicability != result.ApplicabilityUnknown) {
			t.Fatalf("unknown model chat applicability: %+v", chat)
		}
	}
	assessment := out.ReportV3.Targets[0].Summary.Assessment
	if assessment == nil || assessment.Verdict != "inconclusive" || assessment.Comparison.Rankable || assessment.Coverage.Unresolved != 1 {
		t.Fatalf("unknown inventory coverage was not reflected in target verdict: %+v", assessment)
	}
	if out.ExitCode != 1 {
		t.Fatalf("core policy must fail inconclusive coverage: exit=%d", out.ExitCode)
	}
	markdown, err := os.ReadFile("report.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(markdown)
	if !strings.Contains(text, "#### Model details: `gpt-image-2`") || !strings.Contains(text, "| `chat-completions` | `chat.completions.text` | true | NOT-APPLICABLE | – | classified capabilities do not include chat.completions.text") {
		t.Fatalf("mixed-catalog routing is not visible in Markdown:\n%s", text)
	}
	if !strings.Contains(text, "NOT-EVALUATED") || !strings.Contains(text, "UNKNOWN") {
		t.Fatalf("inventory-only verdicts are not visible in Markdown:\n%s", text)
	}
}

func TestMissingRequestedModelIsSilentAndDoesNotProbe(t *testing.T) {
	var modelCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"object": "list", "data": []any{
			map[string]any{"id": "gpt-5.4-mini", "object": "model", "created": 1, "capabilities": []any{"chat.completions.text"}},
		}})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		modelCalls.Add(1)
		writeError(w, 500, "unexpected", "missing model must not be probed")
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	withTempWorkingDirectory(t)
	cfg := testConfig(server.URL+"/v1", []string{"compat"})
	cfg.Models = []string{"missing-model"}
	cfg.Formats = nil
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil || out.ExitCode != 1 {
		t.Fatalf("global no-match must be inconclusive under core policy: exit=%d err=%v report=%+v", out.ExitCode, err, out.ReportV3)
	}
	if modelCalls.Load() != 0 || len(out.ReportV3.Targets) != 1 || len(out.ReportV3.Targets[0].Models) != 0 {
		t.Fatalf("missing model was probed or materialized: calls=%d targets=%+v", modelCalls.Load(), out.ReportV3.Targets)
	}
	if len(out.ReportV3.Config.RequestedModels) != 1 || out.ReportV3.Config.RequestedModels[0] != "missing-model" {
		t.Fatalf("global request provenance missing: %+v", out.ReportV3.Config)
	}
	foundCoverage := false
	for _, requirement := range out.ReportV3.Targets[0].Requirements {
		if requirement.Subject.Kind == "model" {
			t.Fatalf("missing model created canonical requirement: %+v", requirement)
		}
		if requirement.ID == "selection.requested_models" && requirement.FailureKind == "no_evaluated_requested_scope" {
			foundCoverage = true
		}
	}
	if !foundCoverage || out.ReportV3.Targets[0].Summary.Assessment == nil || out.ReportV3.Targets[0].Summary.Assessment.Comparison.Rankable {
		t.Fatalf("missing requested scope did not become explicit non-rankable coverage: %+v", out.ReportV3.Targets[0].Summary.Assessment)
	}
}

func TestExplicitAmbiguousModelUsesOnePreflightAndStopsOnRouteRejection(t *testing.T) {
	var preflightCalls, retrieveCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"object": "list", "data": []any{
			map[string]any{"id": "ambiguous-model", "object": "model", "created": 1},
		}})
	})
	mux.HandleFunc("/v1/models/", func(w http.ResponseWriter, r *http.Request) {
		retrieveCalls.Add(1)
		writeError(w, 500, "unexpected", "retrieve must be blocked after route rejection")
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		preflightCalls.Add(1)
		writeError(w, 404, "model_not_found", "model not found")
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	withTempWorkingDirectory(t)
	cfg := testConfig(server.URL+"/v1", []string{"compat"})
	cfg.Models = []string{"ambiguous-model"}
	cfg.Formats = nil
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if preflightCalls.Load() != 1 || retrieveCalls.Load() != 0 {
		t.Fatalf("unexpected ambiguous route calls: preflight=%d retrieve=%d", preflightCalls.Load(), retrieveCalls.Load())
	}
	if out.ReportV3 == nil || len(out.ReportV3.Targets) != 1 || len(out.ReportV3.Targets[0].Models) != 1 {
		t.Fatalf("ambiguous model report is missing: %+v", out.ReportV3)
	}
	model := out.ReportV3.Targets[0].Models[0]
	if len(model.Compatibility) != 1 || model.Compatibility[0].ID != "chat.basic" || model.Compatibility[0].Status != result.StatusFail {
		t.Fatalf("route rejection did not stop dependent probes: %+v", model.Compatibility)
	}
	chat := moduleByID(model.Modules, "chat-completions")
	if chat == nil || len(chat.BlockedBy) == 0 {
		t.Fatalf("route rejection is not visible in module planning: %+v", model.Modules)
	}
	if out.ReportV3.Summary.Verdict != "fail" || out.ExitCode != 1 {
		t.Fatalf("route rejection verdict/exit mismatch: verdict=%s exit=%d", out.ReportV3.Summary.Verdict, out.ExitCode)
	}
	if len(out.ReportV3.Targets[0].Guidance.Findings) != 1 || out.ReportV3.Targets[0].Guidance.Findings[0].RootCause != "root:model-route-rejection" {
		t.Fatalf("route rejection root cause was not grouped explicitly: %+v", out.ReportV3.Targets[0].Guidance.Findings)
	}
}

func TestExplicitAmbiguousModelTransientPreflightIsInconclusiveAndRetried(t *testing.T) {
	var preflightCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"object": "list", "data": []any{
			map[string]any{"id": "ambiguous-model", "object": "model", "created": 1},
		}})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		preflightCalls.Add(1)
		writeError(w, 503, "temporarily_unavailable", "retry later")
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	withTempWorkingDirectory(t)
	cfg := testConfig(server.URL+"/v1", []string{"compat"})
	cfg.Models = []string{"ambiguous-model"}
	cfg.Formats = nil
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if preflightCalls.Load() != 3 {
		t.Fatalf("transient preflight retry count=%d, want 3", preflightCalls.Load())
	}
	if out.ReportV3.Summary.Verdict != "inconclusive" || out.ExitCode != 1 {
		t.Fatalf("transient preflight verdict/exit mismatch: verdict=%s exit=%d", out.ReportV3.Summary.Verdict, out.ExitCode)
	}
	model := out.ReportV3.Targets[0].Models[0]
	if len(model.Compatibility) != 1 || model.Compatibility[0].Status != result.StatusError {
		t.Fatalf("transient preflight did not remain a single inconclusive root: %+v", model.Compatibility)
	}
	if len(out.ReportV3.Targets[0].Guidance.Findings) != 1 || out.ReportV3.Targets[0].Guidance.Findings[0].RootCause != "root:model-route-availability-unknown" {
		t.Fatalf("transient route root cause was not preserved: %+v", out.ReportV3.Targets[0].Guidance.Findings)
	}
}

func TestImageModelUsesImageEndpointWithoutChatRequests(t *testing.T) {
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	var chatRequests, imageRequests atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"object": "list", "data": []any{
			map[string]any{"id": "gpt-image-2", "object": "model", "created": 1},
		}})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		chatRequests.Add(1)
		writeError(w, 500, "unexpected", "chat must not be called")
	})
	mux.HandleFunc("/v1/images/generations", func(w http.ResponseWriter, r *http.Request) {
		imageRequests.Add(1)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, 400, "invalid_request_error", "malformed JSON")
			return
		}
		if body["model"] != "gpt-image-2" {
			writeError(w, 404, "model_not_found", "unknown image model")
			return
		}
		writeJSON(w, 200, map[string]any{"created": 1, "data": []any{map[string]any{"b64_json": png}}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	withTempWorkingDirectory(t)
	cfg := testConfig(server.URL+"/v1", []string{"compat"})
	cfg.Modules = []string{"images-generations"}
	cfg.AllowBillableArtifacts = true
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if chatRequests.Load() != 0 || imageRequests.Load() != 3 {
		t.Fatalf("unexpected endpoint routing: chat=%d images=%d", chatRequests.Load(), imageRequests.Load())
	}
	model := out.ReportV3.Targets[0].Models[0]
	imageModule := moduleByID(model.Modules, "images-generations")
	if imageModule == nil || imageModule.Execution != result.ExecutionExecuted || imageModule.Summary == nil || imageModule.Summary.RequiredFail != 0 {
		t.Fatalf("image module was not evaluated successfully: %+v", imageModule)
	}
	data, err := os.ReadFile("report.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), png) || strings.Contains(string(data), "iVBOR") {
		t.Fatal("image payload leaked into JSON report")
	}
}

func moduleByID(modules []result.ModuleAssessment, id string) *result.ModuleAssessment {
	for index := range modules {
		if modules[index].ID == id {
			return &modules[index]
		}
	}
	return nil
}

func testConfig(baseURL string, tests []string) config.Config {
	return config.Config{
		BaseURL:              baseURL,
		APIKey:               "test-key",
		Tests:                tests,
		Timeout:              5 * time.Second,
		RequestInterval:      time.Millisecond,
		Formats:              []string{"markdown", "json"},
		Name:                 "report",
		Concurrency:          1,
		Modules:              []string{"auto"},
		UnknownModel:         "inventory",
		MaxArtifactsPerModel: 1,
		FailOn:               "core",
		Samples:              2,
		ContextMaxTokens:     10_000,
		ContextPrecision:     512,
		ContextBudget:        50_000,
		ContextStartBytes:    1024,
		ContextMaxBytes:      16 * 1024,
	}
}

func withTempWorkingDirectory(t *testing.T) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func newMockOpenAI(t *testing.T) *httptest.Server {
	return newMockOpenAIWithMiddleware(t, nil)
}

func newMockOpenAIWithMiddleware(t *testing.T, middleware func(http.Handler) http.Handler) *httptest.Server {
	t.Helper()
	var sequence atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r) {
			return
		}
		writeJSON(w, 200, map[string]any{
			"object": "list",
			"data": []any{
				map[string]any{"id": "vendor/model-a", "object": "model", "created": 1, "owned_by": "mock", "context_length": 4096, "capabilities": []any{"chat.completions.text"}},
			},
		})
	})
	mux.HandleFunc("/v1/models/", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r) {
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/v1/models/")
		if id != "vendor/model-a" {
			writeError(w, 404, "model_not_found", "model not found")
			return
		}
		writeJSON(w, 200, map[string]any{"id": id, "object": "model", "created": 1, "owned_by": "mock", "context_length": 4096, "capabilities": []any{"chat.completions.text"}})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r) {
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, 400, "invalid_request_error", err.Error())
			return
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			writeError(w, 400, "invalid_request_error", "malformed JSON")
			return
		}
		model, _ := body["model"].(string)
		if model != "vendor/model-a" {
			writeError(w, 404, "model_not_found", "unknown model")
			return
		}
		messages, ok := body["messages"].([]any)
		if !ok || len(messages) == 0 {
			writeError(w, 400, "invalid_request_error", "messages must be an array")
			return
		}
		if len(raw) > 7000 {
			writeError(w, 400, "context_length_exceeded", "maximum context length exceeded")
			return
		}
		if reason := invalidParameter(body); reason != "" {
			writeError(w, 400, "invalid_request_error", reason)
			return
		}

		content := "AIQA_OK"
		finishReason := "stop"
		completionTokens := int64(4)
		var toolCalls []any
		prompt := lastPrompt(messages)
		roles := messageRoles(messages)
		if strings.Contains(roles, "system") {
			content = "SYSTEM_OK"
		}
		if strings.Contains(roles, "developer") {
			content = "DEVELOPER_OK"
		}
		if strings.Contains(prompt, "code word") || strings.Contains(prompt, "code word.") {
			content = "ALPHA-731"
		}
		if strings.Contains(prompt, "STREAM_OK") {
			content = "STREAM_OK"
		}
		if strings.Contains(prompt, "PARTS_OK") {
			content = "PARTS_OK"
		}
		if strings.Contains(prompt, "forty distinct") {
			if limit := integer(body["max_completion_tokens"]); limit > 0 {
				completionTokens = int64(limit)
				finishReason = "length"
				content = "one two three four five six seven eight"
			} else if limit := integer(body["max_tokens"]); limit > 0 {
				completionTokens = int64(limit)
				finishReason = "length"
				content = "one two three four five six seven eight"
			} else {
				completionTokens = 40
				content = strings.Repeat("word ", 40)
			}
		}
		if strings.Contains(prompt, "verbosity-control probe") {
			switch body["verbosity"] {
			case "low":
				content = "Checksums summarize data and change when the data changes."
				completionTokens = 10
			case "high":
				content = strings.Repeat("Checksums compare a compact computed fingerprint with an expected value, revealing likely accidental changes. ", 4)
				completionTokens = 40
			}
		}
		if strings.Contains(prompt, "nonsense word") {
			seed := integer(body["seed"])
			temperature := number(body["temperature"])
			topP := number(body["top_p"])
			switch {
			case seed != 0:
				content = mockWord(seed)
			case temperature >= 1.5 || topP >= 0.9:
				content = mockWord(int(sequence.Add(1)))
			default:
				content = "mimimi"
			}
		}
		if _, ok := body["stop"]; ok {
			content = "ALPHA "
		}
		if format, _ := body["response_format"].(map[string]any); format != nil {
			content = `{"ok":true,"value":"AIQA_OK"}`
		}
		if hasToolRole(messages) {
			content = "resource-alpha is available."
		} else if _, ok := body["tools"]; ok {
			if choice, _ := body["tool_choice"].(string); choice == "none" {
				content = "NONE_OK"
			} else {
				toolCalls = []any{map[string]any{
					"id":       "call_mock_1",
					"type":     "function",
					"function": map[string]any{"name": "get_status", "arguments": `{"resource_id":"resource-alpha"}`},
				}}
				if parallel, _ := body["parallel_tool_calls"].(bool); parallel && strings.Contains(prompt, "resource-beta") {
					toolCalls = append(toolCalls, map[string]any{
						"id":       "call_mock_2",
						"type":     "function",
						"function": map[string]any{"name": "get_status", "arguments": `{"resource_id":"resource-beta"}`},
					})
				}
				content = ""
				finishReason = "tool_calls"
			}
		}

		reasoningContent := ""
		reasoningTokens := int64(0)
		if effort, _ := body["reasoning_effort"].(string); effort != "" && effort != "none" && effort != "minimal" {
			reasoningContent = "mock reasoning for " + effort
			reasoningTokens = int64(len(effort) + 3)
		}
		if thinking, _ := body["thinking"].(map[string]any); thinking != nil && thinking["type"] == "enabled" {
			reasoningContent = "mock enabled reasoning"
			reasoningTokens = 12
		}
		if split, ok := body["reasoning_split"].(bool); ok {
			if split {
				reasoningContent = "mock split reasoning"
				reasoningTokens = 10
			} else {
				content = "<think>mock hidden thought</think> " + content
			}
		}

		promptTokens := int64(len(raw)/3 + 10)
		usage := map[string]any{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
		}
		if reasoningTokens > 0 {
			usage["completion_tokens_details"] = map[string]any{"reasoning_tokens": reasoningTokens}
		}
		choiceCount := integer(body["n"])
		if choiceCount == 0 {
			choiceCount = 1
		}
		choices := make([]any, 0, choiceCount)
		for i := 0; i < choiceCount; i++ {
			message := map[string]any{"role": "assistant", "content": content}
			if len(toolCalls) > 0 {
				message["content"] = nil
				message["tool_calls"] = toolCalls
			}
			if reasoningContent != "" {
				message["reasoning_content"] = reasoningContent
			}
			choice := map[string]any{"index": i, "message": message, "finish_reason": finishReason}
			if body["logprobs"] == true {
				choice["logprobs"] = map[string]any{"content": []any{map[string]any{"token": "A", "logprob": -0.01}}}
			}
			choices = append(choices, choice)
		}
		root := map[string]any{
			"id":      "chatcmpl_mock",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": choices,
			"usage":   usage,
		}
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			delta := map[string]any{"role": "assistant", "content": content}
			if len(toolCalls) > 0 {
				delta["content"] = nil
				delta["tool_calls"] = []any{map[string]any{
					"index": 0, "id": "call_mock_1", "type": "function",
					"function": map[string]any{"name": "get_status", "arguments": `{"resource_id":"resource-alpha"}`},
				}}
			}
			chunk1 := map[string]any{
				"id": "chatcmpl_mock", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model,
				"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}},
			}
			chunk2 := map[string]any{
				"id": "chatcmpl_mock", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model,
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finishReason}},
			}
			writeSSE(w, chunk1)
			writeSSE(w, chunk2)
			if options, _ := body["stream_options"].(map[string]any); options != nil && options["include_usage"] == true {
				writeSSE(w, map[string]any{"id": "chatcmpl_mock", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model, "choices": []any{}, "usage": usage})
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
		writeJSON(w, 200, root)
	})
	var handler http.Handler = mux
	if middleware != nil {
		handler = middleware(handler)
	}
	return httptest.NewServer(handler)
}

func authorize(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Authorization") != "Bearer test-key" {
		writeError(w, 401, "invalid_api_key", "invalid API key")
		return false
	}
	return true
}

func invalidParameter(body map[string]any) string {
	if v, ok := body["temperature"]; ok {
		if _, ok := v.(float64); !ok {
			return "temperature must be numeric"
		}
		if number(v) < 0 || number(v) > 2 {
			return "temperature out of range"
		}
	}
	if v, ok := body["top_p"]; ok && (number(v) <= 0 || number(v) > 1) {
		return "top_p out of range"
	}
	for _, key := range []string{"max_tokens", "max_completion_tokens"} {
		if v, ok := body[key]; ok && integer(v) <= 0 {
			return key + " must be positive"
		}
	}
	if v, ok := body["n"]; ok && integer(v) <= 0 {
		return "n must be positive"
	}
	for _, key := range []string{"presence_penalty", "frequency_penalty"} {
		if v, ok := body[key]; ok && (number(v) < -2 || number(v) > 2) {
			return key + " out of range"
		}
	}
	if v, ok := body["top_logprobs"]; ok && integer(v) > 5 {
		return "top_logprobs out of range"
	}
	if v, ok := body["seed"]; ok {
		if _, ok := v.(float64); !ok {
			return "seed must be integer"
		}
	}
	if v, ok := body["stop"]; ok {
		switch v.(type) {
		case string, []any:
		default:
			return "stop must be string or array"
		}
	}
	if v, ok := body["verbosity"]; ok {
		s, _ := v.(string)
		if s != "low" && s != "medium" && s != "high" {
			return "invalid verbosity"
		}
	}
	if v, ok := body["parallel_tool_calls"]; ok {
		if _, ok := v.(bool); !ok {
			return "parallel_tool_calls must be boolean"
		}
	}
	if v, ok := body["reasoning_effort"]; ok {
		s, _ := v.(string)
		allowed := map[string]bool{"none": true, "minimal": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true}
		if !allowed[s] {
			return "invalid reasoning_effort"
		}
	}
	if v, ok := body["thinking"]; ok {
		m, _ := v.(map[string]any)
		if m == nil || (m["type"] != "enabled" && m["type"] != "disabled") {
			return "invalid thinking"
		}
	}
	if v, ok := body["reasoning_split"]; ok {
		if _, ok := v.(bool); !ok {
			return "reasoning_split must be boolean"
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": message, "type": "invalid_request_error", "param": nil, "code": code}})
}

func writeSSE(w http.ResponseWriter, value any) {
	b, _ := json.Marshal(value)
	fmt.Fprintf(w, "data: %s\n\n", b)
}

func mockWord(value int) string {
	if value < 0 {
		value = -value
	}
	const alphabet = "abcdefghijklmnopqrstuvwxyz"
	buf := make([]byte, 6)
	for i := range buf {
		buf[i] = alphabet[value%len(alphabet)]
		value = value/len(alphabet) + i + 1
	}
	return string(buf)
}

func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}

func integer(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := strconv.Atoi(n.String())
		return i
	default:
		return 0
	}
}

func lastPrompt(messages []any) string {
	for i := len(messages) - 1; i >= 0; i-- {
		m, _ := messages[i].(map[string]any)
		if m == nil {
			continue
		}
		switch content := m["content"].(type) {
		case string:
			return content
		case []any:
			var parts []string
			for _, raw := range content {
				part, _ := raw.(map[string]any)
				if text, _ := part["text"].(string); text != "" {
					parts = append(parts, text)
				}
			}
			return strings.Join(parts, "")
		}
	}
	return ""
}

func messageRoles(messages []any) string {
	var roles []string
	for _, raw := range messages {
		m, _ := raw.(map[string]any)
		if role, _ := m["role"].(string); role != "" {
			roles = append(roles, role)
		}
	}
	return strings.Join(roles, ",")
}

func hasToolRole(messages []any) bool {
	return strings.Contains(messageRoles(messages), "tool")
}

func TestMain(m *testing.M) {
	// Guard against a future test accidentally writing reports into the source tree.
	root, _ := os.Getwd()
	code := m.Run()
	matches, _ := filepath.Glob(filepath.Join(root, "report.*"))
	for _, match := range matches {
		_ = os.Remove(match)
	}
	os.Exit(code)
}

func TestVerboseLoggerFiltersProgressByLevel(t *testing.T) {
	logger := logging.New(2)
	logger.Logf(logging.Info, "[%s] target started (%s)", "openai", "openai")
	logger.Logf(logging.Debug, "[%s/%s] done", "openai", "gpt-test")
	got := logger.Lines()
	if len(got) != 1 || !strings.Contains(got[0], `"level":"INFO"`) || !strings.Contains(got[0], `"event":"message"`) || !strings.Contains(got[0], `"message":"[openai] target started (openai)"`) {
		t.Fatalf("lines = %#v, want one structured INFO message", got)
	}
}

func TestRunWritesFatalToVerboseLogForEarlyProfileFailure(t *testing.T) {
	withTempWorkingDirectory(t)
	cfg := testConfig("http://127.0.0.1:1/v1", []string{"compat"})
	cfg.Verbosity = 1
	cfg.Name = "early-failure"
	cfg.OpenAIProfile = "missing-profile.json"

	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err == nil || out.ExitCode != 2 {
		t.Fatalf("unexpected early failure: exit=%d err=%v", out.ExitCode, err)
	}
	data, readErr := os.ReadFile("early-failure.log")
	if readErr != nil {
		t.Fatalf("read verbose log: %v", readErr)
	}
	if got := string(data); !strings.Contains(got, `"level":"FATAL"`) || !strings.Contains(got, `"event":"message"`) || !strings.Contains(got, "run failed; inspect public report or safe error classification") || strings.Contains(got, "missing-profile.json") {
		t.Fatalf("verbose log did not retain fatal profile failure: %q", got)
	}
	info, statErr := os.Stat("early-failure.log")
	if statErr != nil {
		t.Fatalf("stat verbose log: %v", statErr)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("verbose log mode=%#o, want 0600", got)
	}
	if !reflect.DeepEqual(out.Paths, []string{"early-failure.log"}) {
		t.Fatalf("paths = %#v, want only verbose log", out.Paths)
	}
}

type failingForensicWriter struct{}

func (failingForensicWriter) Write([]byte) (int, error) {
	return 0, errors.New("fixture sidecar write failure")
}

func TestRunMakesSidecarWriteFailureInconclusiveAndFailsRun(t *testing.T) {
	server := newMockOpenAI(t)
	defer server.Close()
	withTempWorkingDirectory(t)
	previous := attachForensicSink
	attachForensicSink = func(logger *logging.Logger, _ *os.File) (*logging.Sink, error) {
		return logger.AddSink(failingForensicWriter{}, logging.SinkOptions{MaxBytes: -1})
	}
	t.Cleanup(func() { attachForensicSink = previous })
	cfg := testConfig(server.URL+"/v1", []string{"compat"})
	cfg.Verbosity = 4
	cfg.Name = "failed-sidecar"
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err == nil || out.ExitCode != 2 {
		t.Fatalf("sidecar failure did not fail run: exit=%d err=%v", out.ExitCode, err)
	}
	data, readErr := os.ReadFile("failed-sidecar.trace-verdict.json")
	if readErr != nil {
		t.Fatalf("read inconclusive verdict: %v", readErr)
	}
	var verdict struct {
		Status     string `json:"status"`
		Violations []struct {
			Code string `json:"code"`
		} `json:"violations"`
	}
	if err := json.Unmarshal(data, &verdict); err != nil {
		t.Fatalf("decode verdict: %v", err)
	}
	if verdict.Status != "inconclusive" || len(verdict.Violations) != 1 || verdict.Violations[0].Code != "trace_write_failed" {
		t.Fatalf("unexpected write-failure verdict: %s", data)
	}
	if out.ReportV3.ForensicTrace == nil || out.ReportV3.ForensicTrace.Status != "inconclusive" {
		t.Fatalf("public forensic status=%+v", out.ReportV3.ForensicTrace)
	}
}

func TestExitPolicies(t *testing.T) {
	cases := []struct {
		policy  string
		summary result.Summary
		want    int
	}{
		{"core", result.Summary{Warn: 1, Unsupported: 1}, 0},
		{"core", result.Summary{Fail: 1}, 1},
		{"core", result.Summary{Error: 1}, 1},
		{"core", result.Summary{RequiredFail: 1}, 1},
		{"any", result.Summary{Warn: 1}, 1},
		{"any", result.Summary{Unsupported: 1}, 1},
		{"never", result.Summary{Fail: 10, Error: 10}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.policy, func(t *testing.T) {
			got := exitCode(config.Config{FailOn: tc.policy}, tc.summary)
			if got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestExitPoliciesUseCanonicalVerdict(t *testing.T) {
	cases := []struct {
		policy  string
		verdict string
		want    int
	}{
		{"core", "pass", 0},
		{"core", "pass-with-gaps", 0},
		{"core", "partial", 0},
		{"core", "fail", 1},
		{"core", "inconclusive", 1},
		{"any", "pass", 0},
		{"any", "pass-with-gaps", 1},
		{"never", "fail", 0},
	}
	for _, tc := range cases {
		t.Run(tc.policy+"/"+tc.verdict, func(t *testing.T) {
			summary := result.Summary{Assessment: &result.ConformanceAssessment{Verdict: tc.verdict}}
			if got := exitCode(config.Config{FailOn: tc.policy}, summary); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}
