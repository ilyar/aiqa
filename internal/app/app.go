package app

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/assessment"
	"github.com/ilyar/aiqa/internal/capability"
	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/extsuite"
	extanthropic "github.com/ilyar/aiqa/internal/extsuite/builtin/anthropic"
	extgoogle "github.com/ilyar/aiqa/internal/extsuite/builtin/googlegenai"
	extopenai "github.com/ilyar/aiqa/internal/extsuite/builtin/openai"
	"github.com/ilyar/aiqa/internal/guidance"
	"github.com/ilyar/aiqa/internal/probe"
	"github.com/ilyar/aiqa/internal/profile"
	protocolanthropic "github.com/ilyar/aiqa/internal/protocol/anthropic"
	protocolgoogle "github.com/ilyar/aiqa/internal/protocol/googlegenai"
	reportpkg "github.com/ilyar/aiqa/internal/report"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/suites"
)

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

type RunResult struct {
	Report   result.Report
	ReportV3 *result.ReportV3
	Paths    []string
	ExitCode int
	client   *api.Client
	executor *probe.Executor
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

func runOpenAI(ctx context.Context, cfg config.Config, build BuildInfo) (RunResult, error) {
	overrides, err := capability.LoadOverrideFile(cfg.CapabilityFile)
	if err != nil {
		return RunResult{}, err
	}
	if overrides == nil {
		overrides = map[string][]string{}
	}
	for _, raw := range cfg.ModelCapabilities {
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return RunResult{}, fmt.Errorf("invalid --model-capability %q", raw)
		}
		values := strings.Split(parts[1], "+")
		for index := range values {
			values[index] = strings.TrimSpace(values[index])
			value := values[index]
			if !capability.Known[value] {
				return RunResult{}, fmt.Errorf("unknown capability %q in --model-capability", value)
			}
		}
		overrides[strings.TrimSpace(parts[0])] = values
	}
	p, err := profile.LoadFile(cfg.OpenAIProfile)
	if err != nil {
		return RunResult{}, err
	}
	client, err := api.NewClient(cfg.BaseURL, cfg.APIKey, "aiqa/"+build.Version, cfg.RequestInterval)
	if err != nil {
		return RunResult{}, err
	}
	client.SetConcurrency(cfg.Concurrency)
	probeExecutor := probe.NewExecutor(cfg.Concurrency)
	r := result.Report{
		SchemaVersion: "aiqa.report.internal",
		GeneratedAt:   time.Now().UTC(),
		Tool: result.ToolInfo{
			Name:      "aiqa",
			Version:   build.Version,
			Commit:    build.Commit,
			BuildDate: build.Date,
			GoVersion: runtime.Version(),
		},
		Profile: result.ProfileInfo{
			SchemaVersion:     p.SchemaVersion,
			ID:                p.ID,
			Protocol:          p.Protocol,
			Revision:          p.Revision,
			APIVersion:        p.APIVersion,
			BaseURLSemantics:  p.BaseURLSemantics,
			Source:            p.Source,
			SourceCommit:      p.SourceCommit,
			SourceRefRevision: p.SourceRefRevision,
			Scope:             p.Scope,
		},
		Config: result.PublicConfig{
			BaseURL:                cfg.BaseURL,
			Tests:                  append([]string(nil), cfg.Tests...),
			RequestedModels:        append([]string(nil), cfg.Models...),
			Timeout:                cfg.Timeout.String(),
			Formats:                append([]string(nil), cfg.Formats...),
			ExtSuites:              append([]string(nil), cfg.ExtSuites...),
			FailOn:                 cfg.FailOn,
			ContextMaxTokens:       cfg.ContextMaxTokens,
			ContextPrecision:       cfg.ContextPrecision,
			ContextBudget:          cfg.ContextBudget,
			Samples:                cfg.Samples,
			Modules:                append([]string(nil), cfg.Modules...),
			UnknownModel:           cfg.UnknownModel,
			AllowBillableArtifacts: cfg.AllowBillableArtifacts,
			MaxArtifactsPerModel:   cfg.MaxArtifactsPerModel,
			Guidance:               cfg.Guidance,
			GuidancePack:           cfg.GuidancePack,
			GuidanceLocale:         cfg.GuidanceLocale,
		},
	}

	logf := func(format string, args ...any) {
		if cfg.Verbose {
			fmt.Fprintf(os.Stderr, "aiqa: "+format+"\n", args...)
		}
	}
	env := suites.Environment{Client: client, Config: cfg, Profile: p, Logf: logf, Probes: probeExecutor}

	listCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	models, listResp, listErr := client.ListModels(listCtx)
	cancel()
	r.Discovery.ModelCount = len(models)
	for _, model := range models {
		r.Discovery.Models = append(r.Discovery.Models, cloneMap(model.Raw))
	}
	if cfg.HasTest("compat") {
		r.Discovery.Tests = append(r.Discovery.Tests, suites.DiscoveryTests(p, listResp, models, listErr)...)
	}

	selected, metadata := selectModels(cfg.Models, models)
	if len(selected) == 0 {
		// model-param still needs a visible discovery failure even when compat was
		// not selected.
		if !cfg.HasTest("compat") {
			r.Discovery.Tests = append(r.Discovery.Tests, suites.DiscoveryTests(p, listResp, models, listErr)...)
		}
		reportpkg.Finalize(&r)
		guidance.ExtractArtifacts(&r)
		assessment.Apply(&r, p)
		guidance.Apply(&r, p, guidance.Options{Mode: cfg.Guidance, Pack: cfg.GuidancePack, Locale: cfg.GuidanceLocale})
		if listErr != nil {
			return RunResult{Report: r, ExitCode: 1, client: client, executor: probeExecutor}, fmt.Errorf("no models available: %w", listErr)
		}
		return RunResult{Report: r, ExitCode: 1, client: client, executor: probeExecutor}, fmt.Errorf("no models returned by endpoint")
	}
	fmt.Fprintf(os.Stderr, "aiqa: testing %d model(s): %s\n", len(selected), joinModels(selected))
	if cfg.HasTest("model-param") {
		fmt.Fprintf(os.Stderr, "aiqa: context probing is enabled; the per-model prompt budget is %d tokens\n", cfg.ContextBudget)
	}

	classifications := make([]capability.Classification, len(selected))
	modulePlans := make([][]result.ModuleAssessment, len(selected))
	for index, model := range selected {
		classifications[index] = capability.Classify(model, metadata[model], capability.Options{BaseURL: cfg.BaseURL, Overrides: overrides, UnknownPolicy: cfg.UnknownModel})
		modulePlans[index] = capability.AssessModules(classifications[index], p.Modules, capability.RoutingOptions{Modules: cfg.Modules, AllowBillableArtifacts: cfg.AllowBillableArtifacts})
	}
	chatRepresentative := representativeModel(selected, modulePlans, capability.ModuleChat)

	if cfg.HasTest("compat") && chatRepresentative != "" {
		r.Discovery.Compatibility = append(r.Discovery.Compatibility, suites.RunEndpointCompat(ctx, env, chatRepresentative)...)
		suites.SortTests(r.Discovery.Compatibility)
	}

	type item struct {
		index  int
		report result.ModelReport
	}
	jobs := make(chan int)
	results := make(chan item, len(selected))
	workers := cfg.Concurrency
	if workers > len(selected) {
		workers = len(selected)
	}
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				model := selected[index]
				fmt.Fprintf(os.Stderr, "aiqa: [%s] start\n", model)
				mr := result.ModelReport{ID: model, Metadata: cloneMap(metadata[model]), Modules: modulePlans[index]}
				for _, evidence := range classifications[index].Capabilities {
					mr.Capabilities = append(mr.Capabilities, result.CapabilityAssessment{ID: evidence.Capability, Source: evidence.Source, Rule: evidence.Rule, Confidence: evidence.Confidence, Reason: evidence.Reason})
				}
				chatModule := capability.Module(mr.Modules, capability.ModuleChat)
				imageModule := capability.Module(mr.Modules, capability.ModuleImages)
				if cfg.HasTest("compat") && chatModule != nil && chatModule.Execution == result.ExecutionExecuted {
					mr.Compatibility = suites.RunModelCompat(ctx, env, model)
				}
				if cfg.HasTest("compat") && imageModule != nil && imageModule.Execution == result.ExecutionExecuted {
					mr.Compatibility = append(mr.Compatibility, suites.RunImageCompat(ctx, env, model)...)
				}
				suites.SortTests(mr.Compatibility)
				if cfg.HasTest("model-param") && chatModule != nil && chatModule.Execution == result.ExecutionExecuted {
					mr.Parameters, mr.Context = suites.RunModelParameters(ctx, env, model, metadata[model])
				}
				if len(selected) > 1 {
					stats := client.PacingStatsForModel(model)
					mr.RateLimit = &result.RateLimitStats{SuccessfulRequests: stats.SuccessfulRequests, RateLimited: stats.RateLimited, RetriesRecovered: stats.RetriesRecovered, BackoffMS: stats.Backoff.Milliseconds(), SafeRate: stats.SafeRate, SafeConcurrency: stats.SafeConcurrency}
					addModelLatencyStats(mr.RateLimit, mr)
				}
				fmt.Fprintf(os.Stderr, "aiqa: [%s] done\n", model)
				results <- item{index: index, report: mr}
			}
		}()
	}
	go func() {
		for i := range selected {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	ordered := make([]result.ModelReport, len(selected))
	for item := range results {
		ordered[item.index] = item.report
	}
	r.Models = ordered
	stats := client.PacingStats()
	r.RateLimit = result.RateLimitStats{SuccessfulRequests: stats.SuccessfulRequests, RateLimited: stats.RateLimited, RetriesRecovered: stats.RetriesRecovered, BackoffMS: stats.Backoff.Milliseconds(), SafeRate: stats.SafeRate, SafeConcurrency: stats.SafeConcurrency}
	addLatencyStats(&r.RateLimit, r)
	reportpkg.Finalize(&r)
	guidance.ExtractArtifacts(&r)
	assessment.Apply(&r, p)
	guidance.Apply(&r, p, guidance.Options{Mode: cfg.Guidance, Pack: cfg.GuidancePack, Locale: cfg.GuidanceLocale})
	return RunResult{Report: r, ExitCode: exitCode(cfg, r.Summary), client: client, executor: probeExecutor}, nil
}

func runMultiTarget(ctx context.Context, cfg config.Config, build BuildInfo) (RunResult, error) {
	cfg.ExtSuites = config.ResolveExtSuiteSelection(cfg.ExtSuites, cfg.Targets)
	r := result.ReportV3{
		SchemaVersion: "aiqa.report.v3",
		GeneratedAt:   time.Now().UTC(),
		Tool:          result.ToolInfo{Name: "aiqa", Version: build.Version, Commit: build.Commit, BuildDate: build.Date, GoVersion: runtime.Version()},
		Config: result.PublicConfigV3{
			Tests: append([]string(nil), cfg.Tests...), Timeout: cfg.Timeout.String(), Formats: append([]string(nil), cfg.Formats...),
			FailOn: cfg.FailOn, Guidance: cfg.Guidance, ExtSuites: append([]string(nil), cfg.ExtSuites...),
			AllowHighTokenProbes: cfg.AllowHighTokenProbes, AllowPersistentResources: cfg.AllowPersistentResources,
		},
	}
	registry, err := extsuite.NewRegistry(extopenai.New(), extanthropic.New(), extgoogle.New())
	if err != nil {
		return RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("register built-in extension suites: %w", err)
	}
	if cfg.HasTest("ext-suite") {
		if err := extsuite.RegisterExecutables(ctx, registry, cfg, extsuite.HostInfo{Name: "aiqa", Version: build.Version}); err != nil {
			return RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("discover extension suites: %w", err)
		}
		if err := extsuite.ValidateSelection(registry, cfg.ExtSuites); err != nil {
			return RunResult{ReportV3: &r, ExitCode: 2}, err
		}
	}
	logf := func(format string, args ...any) {
		if cfg.Verbose {
			fmt.Fprintf(os.Stderr, "aiqa: "+format+"\n", args...)
		}
	}
	targets := append([]config.TargetConfig(nil), cfg.Targets...)
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	for _, target := range targets {
		r.Config.Targets = append(r.Config.Targets, result.TargetPublicConfig{ID: target.ID, Protocol: string(target.Protocol), BaseURL: target.BaseURL, BaseURLNormalized: target.BaseURLNormalized, RequestedModels: append([]string(nil), target.Models...), APIVersion: target.APIVersion})
		p, err := profile.LoadFile(target.ProfileFile)
		if err != nil {
			return RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("load %s profile: %w", target.ID, err)
		}
		if p.Protocol != string(target.Protocol) {
			return RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("profile %s uses protocol %s, expected %s", target.ProfileFile, p.Protocol, target.Protocol)
		}
		switch target.Protocol {
		case config.ProtocolOpenAI:
			openCfg := openAIConfig(cfg, target)
			openCfg.Formats = nil
			out, runErr := runOpenAI(ctx, openCfg, build)
			if out.Report.SchemaVersion == "" {
				return RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("run OpenAI target: %w", runErr)
			}
			guidance.QualifyArtifacts(&out.Report, target.ID)
			assessment.ApplyTarget(&out.Report, p, target.ID, string(target.Protocol))
			guidance.Apply(&out.Report, p, guidance.Options{Mode: cfg.Guidance, Pack: cfg.GuidancePack, Locale: cfg.GuidanceLocale})
			targetReport := targetFromV1(target, out.Report)
			targetReport, extErr := finishTargetExtensions(ctx, cfg, target, p, targetReport, registry, out.client, out.executor)
			if extErr != nil {
				return RunResult{ReportV3: &r, ExitCode: 2}, extErr
			}
			r.Targets = append(r.Targets, targetReport)
		case config.ProtocolAnthropic:
			runtime := &protocolanthropic.Runtime{}
			targetReport, runErr := protocolanthropic.Run(ctx, protocolanthropic.RunOptions{Target: target, Config: cfg, Profile: p, UserAgent: "aiqa/" + build.Version, Logf: logf, Runtime: runtime})
			if targetReport.ID == "" {
				return RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("run Anthropic target: %w", runErr)
			}
			tmp := reportFromTarget(targetReport, target.BaseURL)
			guidance.Apply(&tmp, p, guidance.Options{Mode: cfg.Guidance, Pack: cfg.GuidancePack, Locale: cfg.GuidanceLocale})
			targetReport.Guidance = tmp.Guidance
			targetReport, extErr := finishTargetExtensions(ctx, cfg, target, p, targetReport, registry, runtime.Client, runtime.Executor)
			if extErr != nil {
				return RunResult{ReportV3: &r, ExitCode: 2}, extErr
			}
			r.Targets = append(r.Targets, targetReport)
		case config.ProtocolGoogleGenAI:
			runtime := &protocolgoogle.Runtime{}
			targetReport, runErr := protocolgoogle.Run(ctx, protocolgoogle.RunOptions{Target: target, Config: cfg, Profile: p, UserAgent: "aiqa/" + build.Version, Logf: logf, Runtime: runtime})
			if targetReport.ID == "" {
				return RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("run Google GenAI target: %w", runErr)
			}
			targetReport.Notes = []string{"Official Google Gen AI Go SDK v1.71.0 does not expose public Interactions API methods; no SDK Interactions suite is claimed."}
			tmp := reportFromTarget(targetReport, target.BaseURL)
			guidance.Apply(&tmp, p, guidance.Options{Mode: cfg.Guidance, Pack: cfg.GuidancePack, Locale: cfg.GuidanceLocale})
			targetReport.Guidance = tmp.Guidance
			targetReport, extErr := finishTargetExtensions(ctx, cfg, target, p, targetReport, registry, runtime.Client, runtime.Executor)
			if extErr != nil {
				return RunResult{ReportV3: &r, ExitCode: 2}, extErr
			}
			r.Targets = append(r.Targets, targetReport)
		default:
			return RunResult{ReportV3: &r, ExitCode: 2}, fmt.Errorf("unsupported target protocol %s", target.Protocol)
		}
	}
	r.Summary = summarizeRun(r.Targets)
	paths, err := reportpkg.WriteV3(r, cfg)
	if err != nil {
		return RunResult{ReportV3: &r, ExitCode: 2}, err
	}
	return RunResult{ReportV3: &r, Paths: paths, ExitCode: multiExitCode(cfg, r.Summary)}, nil
}

func openAIConfig(cfg config.Config, target config.TargetConfig) config.Config {
	cfg.BaseURL = target.BaseURL
	cfg.APIKey = target.APIKey
	cfg.Models = append([]string(nil), target.Models...)
	cfg.Modules = append([]string(nil), target.Modules...)
	cfg.ModelCapabilities = append([]string(nil), target.ModelCapabilities...)
	cfg.CapabilityFile = target.CapabilityFile
	cfg.UnknownModel = target.UnknownModel
	cfg.OpenAIProfile = target.ProfileFile
	return cfg
}

func targetFromV1(target config.TargetConfig, r result.Report) result.TargetReport {
	return result.TargetReport{ID: target.ID, Protocol: string(target.Protocol), Profile: r.Profile, Discovery: r.Discovery, Models: r.Models, Requirements: r.Requirements, Summary: r.Summary, Guidance: r.Guidance, RateLimit: r.RateLimit, ExtensionRateLimit: r.ExtensionRateLimit, Extensions: r.Extensions}
}

func reportFromTarget(target result.TargetReport, baseURL string) result.Report {
	return result.Report{SchemaVersion: "aiqa.report.internal", Profile: target.Profile, Config: result.PublicConfig{BaseURL: baseURL}, Discovery: target.Discovery, Models: target.Models, Requirements: target.Requirements, Summary: target.Summary, Guidance: target.Guidance, RateLimit: target.RateLimit, ExtensionRateLimit: target.ExtensionRateLimit, Extensions: target.Extensions}
}

func finishTargetExtensions(ctx context.Context, cfg config.Config, target config.TargetConfig, p profile.Profile, targetReport result.TargetReport, registry *extsuite.Registry, client *api.Client, executor *probe.Executor) (result.TargetReport, error) {
	if !cfg.HasTest("ext-suite") {
		return targetReport, nil
	}
	if client == nil || executor == nil {
		return targetReport, fmt.Errorf("prepare %s extension runtime: native target runtime is unavailable", target.ID)
	}
	before := client.PacingStats()
	targetReport.Extensions = extsuite.ExecuteTarget(ctx, registry, cfg, target, p, targetReport.Models, client, executor)
	if hasBuiltInExtension(targetReport.Extensions) {
		after := client.PacingStats()
		targetReport.ExtensionRateLimit = &result.RateLimitStats{
			SuccessfulRequests: after.SuccessfulRequests - before.SuccessfulRequests,
			RateLimited:        after.RateLimited - before.RateLimited,
			RetriesRecovered:   after.RetriesRecovered - before.RetriesRecovered,
			BackoffMS:          (after.Backoff - before.Backoff).Milliseconds(),
			SafeRate:           after.SafeRate,
			SafeConcurrency:    after.SafeConcurrency,
		}
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

func representativeModel(models []string, plans [][]result.ModuleAssessment, moduleID string) string {
	for index, model := range models {
		if index >= len(plans) {
			break
		}
		if module := capability.Module(plans[index], moduleID); module != nil && module.Execution == result.ExecutionExecuted {
			return model
		}
	}
	return ""
}

func addModelLatencyStats(out *result.RateLimitStats, m result.ModelReport) {
	var values []int64
	for _, t := range m.Compatibility {
		if t.HTTP.StatusCode >= 200 && t.HTTP.StatusCode < 300 && t.DurationMS > 0 {
			values = append(values, t.DurationMS)
		}
	}
	if len(values) == 0 {
		return
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	var total int64
	for _, v := range values {
		total += v
	}
	out.LatencySamples, out.MeanLatencyMS = len(values), total/int64(len(values))
	out.P50LatencyMS = values[(len(values)-1)/2]
	out.P95LatencyMS = values[(95*len(values)-1)/100]
}

func addLatencyStats(out *result.RateLimitStats, r result.Report) {
	var values []int64
	add := func(t result.TestResult) {
		if t.HTTP.StatusCode >= 200 && t.HTTP.StatusCode < 300 && t.DurationMS > 0 {
			values = append(values, t.DurationMS)
		}
	}
	for _, t := range r.Discovery.Tests {
		add(t)
	}
	for _, t := range r.Discovery.Compatibility {
		add(t)
	}
	for _, m := range r.Models {
		for _, t := range m.Compatibility {
			add(t)
		}
	}
	if len(values) == 0 {
		return
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	var total int64
	for _, v := range values {
		total += v
	}
	out.LatencySamples, out.MeanLatencyMS = len(values), total/int64(len(values))
	out.P50LatencyMS = values[(len(values)-1)/2]
	out.P95LatencyMS = values[(95*len(values)-1)/100]
}

func selectModels(requested []string, discovered []api.Model) ([]string, map[string]map[string]any) {
	metadata := make(map[string]map[string]any, len(discovered))
	for _, model := range discovered {
		metadata[model.ID] = model.Raw
	}
	if len(requested) > 0 {
		selected := append([]string(nil), requested...)
		for _, id := range selected {
			if metadata[id] == nil {
				metadata[id] = map[string]any{"id": id, "object": "model", "source": "--model"}
			}
		}
		return selected, metadata
	}
	selected := make([]string, 0, len(discovered))
	for _, model := range discovered {
		selected = append(selected, model.ID)
	}
	sort.Strings(selected)
	return selected, metadata
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func joinModels(models []string) string {
	const max = 5
	if len(models) <= max {
		return fmt.Sprint(models)
	}
	return fmt.Sprintf("%v and %d more", models[:max], len(models)-max)
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
