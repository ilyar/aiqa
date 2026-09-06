package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/assessment"
	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/extsuite"
	extanthropic "github.com/ilyar/aiqa/internal/extsuite/builtin/anthropic"
	extgoogle "github.com/ilyar/aiqa/internal/extsuite/builtin/googlegenai"
	extopenai "github.com/ilyar/aiqa/internal/extsuite/builtin/openai"
	"github.com/ilyar/aiqa/internal/guidance"
	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/probe"
	"github.com/ilyar/aiqa/internal/profile"
	protocolpkg "github.com/ilyar/aiqa/internal/protocol"
	protocolanthropic "github.com/ilyar/aiqa/internal/protocol/anthropic"
	protocolgoogle "github.com/ilyar/aiqa/internal/protocol/googlegenai"
	protocolopenai "github.com/ilyar/aiqa/internal/protocol/openai"
	reportpkg "github.com/ilyar/aiqa/internal/report"
	"github.com/ilyar/aiqa/internal/result"
)

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

type RunResult struct {
	ReportV3 *result.ReportV3
	Paths    []string
	ExitCode int
}

func Run(ctx context.Context, cfg config.Config, build BuildInfo) (RunResult, error) {
	if len(cfg.Targets) == 0 && cfg.BaseURL != "" {
		profileFile := cfg.OpenAIProfile
		if profileFile == "" {
			profileFile = profile.DefaultFile
		}
		cfg.Targets = []config.TargetConfig{{
			ID: "openai", Protocol: config.ProtocolOpenAI, BaseURL: cfg.BaseURL, APIKey: cfg.APIKey,
			Models: append([]string(nil), cfg.Models...), ProfileFile: profileFile, Modules: append([]string(nil), cfg.Modules...),
			ModelCapabilities: append([]string(nil), cfg.ModelCapabilities...), CapabilityFile: cfg.CapabilityFile, UnknownModel: cfg.UnknownModel,
			RequestInterval: cfg.RequestInterval,
		}}
	}
	return runMultiTarget(ctx, cfg, build)
}

func runMultiTarget(ctx context.Context, cfg config.Config, build BuildInfo) (RunResult, error) {
	logger := logging.New(cfg.Verbosity)
	logf := func(level logging.Level, format string, args ...any) {
		if logger.Logf(level, format, args...) {
			// Application messages use fixed text and identifiers, never request
			// payloads or credentials. The sidecar sanitizes again before writing.
			line := "[" + string(level) + "] " + strings.TrimRight(fmt.Sprintf(format, args...), "\n")
			fmt.Fprintf(os.Stderr, "aiqa: %s\n", line)
		}
	}
	// SDK is a first-class test selector. Keep ExtSuites limited to explicitly
	// requested executable extensions; neither selector is rewritten into the
	// other for execution or in the public report.
	extensionCfg := cfg
	r := result.ReportV3{
		SchemaVersion: "aiqa.report.v3",
		GeneratedAt:   time.Now().UTC(),
		Tool:          result.ToolInfo{Name: "aiqa", Version: build.Version, Commit: build.Commit, BuildDate: build.Date, GoVersion: runtime.Version()},
		Config: result.PublicConfigV3{
			Tests: append([]string(nil), cfg.Tests...), Timeout: cfg.Timeout.String(), Formats: append([]string(nil), cfg.Formats...),
			FailOn: cfg.FailOn, Guidance: cfg.Guidance, ExtSuites: append([]string(nil), cfg.ExtSuites...),
			RequestedModels:      append([]string(nil), cfg.Models...),
			AllowHighTokenProbes: cfg.AllowHighTokenProbes, AllowPersistentResources: cfg.AllowPersistentResources,
		},
	}
	writeVerboseLog := func(out *RunResult) error {
		logPaths, err := reportpkg.WriteVerboseLog(cfg, logger.Lines())
		if err != nil {
			return fmt.Errorf("write verbose log: %w", err)
		}
		out.Paths = append(out.Paths, logPaths...)
		return nil
	}
	fatal := func(out RunResult, cause error) (RunResult, error) {
		// Runtime failures are emitted through the same filtered logger as every
		// other application message. This keeps stderr and the verbose sidecar in
		// sync, and intentionally leaves parse/configuration failures to main.
		logf(logging.Fatal, "%v", cause)
		if logErr := writeVerboseLog(&out); logErr != nil {
			return out, errors.Join(cause, logErr)
		}
		return out, cause
	}
	extRegistry, err := extsuite.NewRegistry(extopenai.New(), extanthropic.New(), extgoogle.New())
	if err != nil {
		return fatal(RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("register built-in extension suites: %w", err))
	}
	nativeRegistry, err := newProtocolRegistry()
	if err != nil {
		return fatal(RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("register native protocol runners: %w", err))
	}
	if cfg.HasTest("ext-suite") {
		if err := extsuite.RegisterExecutables(ctx, extRegistry, extensionCfg, extsuite.HostInfo{Name: "aiqa", Version: build.Version}); err != nil {
			return fatal(RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("discover extension suites: %w", err))
		}
		if err := extsuite.ValidateExecutableSelection(extRegistry, extensionCfg.ExtSuites); err != nil {
			return fatal(RunResult{ReportV3: &r, ExitCode: 2}, err)
		}
	}
	var runErrors []error
	logf(logging.Info, "run started: %d target(s), tests=%s", len(cfg.Targets), strings.Join(cfg.Tests, ","))
	runnerLogf := func(level logging.Level, format string, args ...any) { logf(level, format, args...) }
	targets := append([]config.TargetConfig(nil), cfg.Targets...)
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	for _, target := range targets {
		logf(logging.Info, "[%s] target started (%s)", target.ID, target.Protocol)
		r.Config.Targets = append(r.Config.Targets, result.TargetPublicConfig{ID: target.ID, Protocol: string(target.Protocol), BaseURL: target.BaseURL, BaseURLNormalized: target.BaseURLNormalized, APIVersion: target.APIVersion})
		p, err := profile.LoadFile(target.ProfileFile)
		if err != nil {
			return fatal(RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("load %s profile: %w", target.ID, err))
		}
		if p.Protocol != string(target.Protocol) {
			return fatal(RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("profile %s uses protocol %s, expected %s", target.ProfileFile, p.Protocol, target.Protocol))
		}
		runner, ok := nativeRegistry.Runner(target.Protocol)
		if !ok {
			return fatal(RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("unsupported target protocol %s", target.Protocol))
		}
		logf(logging.Trace, "[%s] native compatibility plan: profile=%s, modules=%s", target.ID, p.ID, strings.Join(target.Modules, ","))
		native, runErr := runner(ctx, protocolpkg.RunOptions{Target: target, Config: cfg, Profile: p, UserAgent: "aiqa/" + build.Version, Logf: runnerLogf})
		if native.Target.ID == "" {
			if runErr == nil {
				runErr = errors.New("native runner returned no target report")
			}
			return fatal(RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("run %s target: %w", target.Protocol, runErr))
		}
		targetReport := native.Target
		tmp := reportFromTarget(targetReport, target.BaseURL)
		guidance.Apply(&tmp, p, guidance.Options{Mode: cfg.Guidance, Pack: cfg.GuidancePack, Locale: cfg.GuidanceLocale})
		targetReport.Guidance = tmp.Guidance
		if runErr != nil {
			logf(logging.Error, "[%s] target runner returned an error", target.ID)
			runErrors = append(runErrors, fmt.Errorf("run %s target %s: %w", target.Protocol, target.ID, runErr))
		} else {
			var extErr error
			targetReport, extErr = finishTargetExtensions(ctx, extensionCfg, target, p, targetReport, extRegistry, native.Runtime.Client, native.Runtime.Executor)
			if extErr != nil {
				logf(logging.Error, "[%s] extension suites returned an error", target.ID)
				runErrors = append(runErrors, fmt.Errorf("run extension suites for target %s: %w", target.ID, extErr))
			}
		}
		r.Targets = append(r.Targets, targetReport)
		logf(logging.Info, "[%s] target completed", target.ID)
	}
	r.Summary = summarizeRun(r.Targets)
	paths, err := reportpkg.WriteV3(r, cfg)
	if err != nil {
		return fatal(RunResult{ReportV3: &r, Paths: paths, ExitCode: 2}, err)
	}
	logf(logging.Info, "report serialization completed")
	out := RunResult{ReportV3: &r, Paths: paths, ExitCode: multiExitCode(cfg, r.Summary)}
	if len(runErrors) > 0 {
		out.ExitCode = 2
		return fatal(out, errors.Join(runErrors...))
	}
	if err := writeVerboseLog(&out); err != nil {
		logf(logging.Fatal, "%v", err)
		out.ExitCode = 2
		return out, err
	}
	return out, nil
}

func newProtocolRegistry() (*protocolpkg.Registry, error) {
	registry := protocolpkg.NewRegistry()
	for _, item := range []struct {
		protocol config.Protocol
		runner   protocolpkg.Runner
	}{
		{config.ProtocolOpenAI, protocolopenai.RunTarget},
		{config.ProtocolAnthropic, protocolanthropic.RunTarget},
		{config.ProtocolGoogleGenAI, protocolgoogle.RunTarget},
	} {
		if err := registry.Register(item.protocol, item.runner); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func reportFromTarget(target result.TargetReport, baseURL string) result.Report {
	return result.Report{SchemaVersion: "aiqa.report.internal", Profile: target.Profile, Config: result.PublicConfig{BaseURL: baseURL}, Discovery: target.Discovery, Models: target.Models, Requirements: target.Requirements, Summary: target.Summary, Guidance: target.Guidance, RateLimit: target.RateLimit, ExtensionRateLimit: target.ExtensionRateLimit, Extensions: target.Extensions}
}

func finishTargetExtensions(ctx context.Context, cfg config.Config, target config.TargetConfig, p profile.Profile, targetReport result.TargetReport, registry *extsuite.Registry, client *api.Client, executor *probe.Executor) (result.TargetReport, error) {
	if !cfg.HasExtensionTests() {
		return targetReport, nil
	}
	if client == nil || executor == nil {
		return targetReport, fmt.Errorf("prepare %s extension runtime: native target runtime is unavailable", target.ID)
	}
	before := client.PacingStats()
	targetReport.Extensions = extsuite.ExecuteTarget(ctx, registry, cfg, target, p, targetReport.Models, client, executor)
	if hasBuiltInExtension(targetReport.Extensions) {
		after := client.PacingStats()
		delta := api.DeltaPacingStats(before, after)
		targetReport.ExtensionRateLimit = protocolpkg.RateLimitStats(delta)
	}
	tmp := reportFromTarget(targetReport, target.BaseURL)
	tmp.Config.Tests = append([]string(nil), cfg.Tests...)
	reportpkg.Finalize(&tmp)
	guidance.ExtractArtifacts(&tmp)
	guidance.QualifyArtifacts(&tmp, target.ID)
	assessment.ApplyTarget(&tmp, p, target.ID, string(target.Protocol))
	guidance.Apply(&tmp, p, guidance.Options{Mode: cfg.Guidance, Pack: cfg.GuidancePack, Locale: cfg.GuidanceLocale})
	targetReport.Models = tmp.Models
	targetReport.Requirements = tmp.Requirements
	targetReport.Summary = tmp.Summary
	targetReport.Guidance = tmp.Guidance
	targetReport.ExtensionRateLimit = tmp.ExtensionRateLimit
	return targetReport, nil
}

func hasBuiltInExtension(runs []result.ExtensionRun) bool {
	for _, run := range runs {
		if run.Provenance.Source == "builtin" {
			return true
		}
	}
	return false
}

func summarizeRun(targets []result.TargetReport) result.RunSummary {
	out := result.RunSummary{Verdict: "pass", TargetCounts: map[string]int{"pass": 0, "pass-with-gaps": 0, "partial": 0, "fail": 0, "inconclusive": 0}}
	rank := map[string]int{"pass": 0, "pass-with-gaps": 1, "partial": 2, "inconclusive": 3, "fail": 4}
	best := 0
	for _, target := range targets {
		verdict := target.Summary.Status
		if target.Summary.Assessment != nil {
			verdict = target.Summary.Assessment.Verdict
		}
		if verdict == "" {
			verdict = "inconclusive"
		}
		if _, known := rank[verdict]; !known {
			verdict = "inconclusive"
		}
		out.TargetCounts[verdict]++
		if rank[verdict] > best {
			best, out.Verdict = rank[verdict], verdict
		}
		if verdict == "fail" || verdict == "inconclusive" {
			out.BlockingTargets = append(out.BlockingTargets, target.ID)
		}
	}
	switch out.Verdict {
	case "fail":
		out.VerdictReason = "one_or_more_targets_failed_core_conformance"
	case "inconclusive":
		out.VerdictReason = "one_or_more_targets_have_inconclusive_core_evidence"
	case "partial":
		out.VerdictReason = "one_or_more_targets_have_partial_conformance"
	case "pass-with-gaps":
		out.VerdictReason = "one_or_more_targets_have_optional_gaps"
	default:
		out.VerdictReason = "all_targets_passed"
	}
	return out
}

func multiExitCode(cfg config.Config, summary result.RunSummary) int {
	if cfg.FailOn == "never" {
		return 0
	}
	if cfg.FailOn == "any" && summary.Verdict != "pass" {
		return 1
	}
	if cfg.FailOn == "core" && (summary.Verdict == "fail" || summary.Verdict == "inconclusive") {
		return 1
	}
	return 0
}

func exitCode(cfg config.Config, s result.Summary) int {
	if s.Assessment == nil {
		switch cfg.FailOn {
		case "never":
			return 0
		case "any":
			if s.Fail > 0 || s.Error > 0 || s.Warn > 0 || s.Unsupported > 0 {
				return 1
			}
			return 0
		case "core":
			if s.RequiredFail > 0 || s.Fail > 0 || s.Error > 0 {
				return 1
			}
			return 0
		}
	}
	verdict := s.Assessment.Verdict
	switch cfg.FailOn {
	case "never":
		return 0
	case "any":
		if verdict != "pass" {
			return 1
		}
		return 0
	case "core":
		if verdict == "fail" || verdict == "inconclusive" {
			return 1
		}
		return 0
	default:
		return 2
	}
}
