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
	"github.com/ilyar/aiqa/internal/external"
	"github.com/ilyar/aiqa/internal/guidance"
	"github.com/ilyar/aiqa/internal/profile"
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
	Paths    []string
	ExitCode int
}

func Run(ctx context.Context, cfg config.Config, build BuildInfo) (RunResult, error) {
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
	p, err := profile.Load()
	if err != nil {
		return RunResult{}, err
	}
	client, err := api.NewClient(cfg.BaseURL, cfg.APIKey, "aiqa/"+build.Version, cfg.RequestInterval)
	if err != nil {
		return RunResult{}, err
	}
	r := result.Report{
		SchemaVersion: "aiqa.report.v1",
		GeneratedAt:   time.Now().UTC(),
		Tool: result.ToolInfo{
			Name:      "aiqa",
			Version:   build.Version,
			Commit:    build.Commit,
			BuildDate: build.Date,
			GoVersion: runtime.Version(),
		},
		Profile: result.ProfileInfo{
			ID:                p.ID,
			Revision:          p.Revision,
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
			ExternalMode:           cfg.ExternalMode,
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
	env := suites.Environment{Client: client, Config: cfg, Profile: p, Logf: logf}

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
		paths, writeErr := reportpkg.Write(r, cfg)
		if writeErr != nil {
			return RunResult{Report: r, ExitCode: 2}, writeErr
		}
		if listErr != nil {
			return RunResult{Report: r, Paths: paths, ExitCode: 1}, fmt.Errorf("no models available: %w", listErr)
		}
		return RunResult{Report: r, Paths: paths, ExitCode: 1}, fmt.Errorf("no models returned by endpoint")
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
	if cfg.HasTest("compat") && cfg.ExternalMode != "off" {
		r.Discovery.External = external.RunModule(ctx, cfg, "", p, "discovery")
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
				if cfg.HasTest("compat") && cfg.ExternalMode != "off" && chatModule != nil && chatModule.Execution == result.ExecutionExecuted {
					mr.External = append(mr.External, external.RunModule(ctx, cfg, model, p, capability.ModuleChat)...)
				}
				// The native image probe consumes the first artifact slot. The
				// Official SDK image suite is also billable, so run it only when
				// the user explicitly budgets a second artifact.
				if cfg.HasTest("compat") && cfg.ExternalMode != "off" && cfg.MaxArtifactsPerModel >= 2 && imageModule != nil && imageModule.Execution == result.ExecutionExecuted {
					mr.External = append(mr.External, external.RunModule(ctx, cfg, model, p, capability.ModuleImages)...)
				}
				if len(selected) > 1 {
					stats := client.PacingStatsForModel(model)
					mr.RateLimit = &result.RateLimitStats{SuccessfulRequests: stats.SuccessfulRequests, RateLimited: stats.RateLimited, RetriesRecovered: stats.RetriesRecovered, BackoffMS: stats.Backoff.Milliseconds()}
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
	paths, err := reportpkg.Write(r, cfg)
	if err != nil {
		return RunResult{Report: r, ExitCode: 2}, err
	}
	return RunResult{Report: r, Paths: paths, ExitCode: exitCode(cfg, r.Summary)}, nil
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
