// Package openai implements the native OpenAI-compatible target runner.
package openai

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/assessment"
	"github.com/ilyar/aiqa/internal/capability"
	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/guidance"
	"github.com/ilyar/aiqa/internal/probe"
	"github.com/ilyar/aiqa/internal/profile"
	protocolpkg "github.com/ilyar/aiqa/internal/protocol"
	reportpkg "github.com/ilyar/aiqa/internal/report"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/suites"
)

// RunTarget executes one OpenAI-compatible target through the common protocol
// registry contract.
func RunTarget(ctx context.Context, options protocolpkg.RunOptions) (protocolpkg.RunResult, error) {
	target, cfg, p := options.Target, options.Config, options.Profile
	overrides, err := capability.LoadOverrideFile(target.CapabilityFile)
	if err != nil {
		return protocolpkg.RunResult{}, err
	}
	if overrides == nil {
		overrides = map[string][]string{}
	}
	for _, raw := range target.ModelCapabilities {
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return protocolpkg.RunResult{}, fmt.Errorf("invalid --model-capability %q", raw)
		}
		values := strings.Split(parts[1], "+")
		for index := range values {
			values[index] = strings.TrimSpace(values[index])
			if !capability.Known[values[index]] {
				return protocolpkg.RunResult{}, fmt.Errorf("unknown capability %q in --model-capability", values[index])
			}
		}
		overrides[strings.TrimSpace(parts[0])] = values
	}

	client, err := api.NewClient(target.BaseURL, target.APIKey, options.UserAgent, target.RequestInterval)
	if err != nil {
		return protocolpkg.RunResult{}, err
	}
	client.SetConcurrency(cfg.Concurrency)
	executor := probe.NewExecutor(cfg.Concurrency)
	runtime := protocolpkg.Runtime{Client: client, Executor: executor}
	tmp := result.Report{
		SchemaVersion: "aiqa.report.internal",
		Profile:       profileInfo(p),
		Config: result.PublicConfig{
			BaseURL: target.BaseURL, Tests: append([]string(nil), cfg.Tests...), RequestedModels: append([]string(nil), target.Models...),
			Timeout: cfg.Timeout.String(), Formats: append([]string(nil), cfg.Formats...), ExtSuites: append([]string(nil), cfg.ExtSuites...),
			FailOn: cfg.FailOn, ContextMaxTokens: cfg.ContextMaxTokens, ContextPrecision: cfg.ContextPrecision,
			ContextBudget: cfg.ContextBudget, Samples: cfg.Samples, Modules: append([]string(nil), target.Modules...),
			UnknownModel: target.UnknownModel, AllowBillableArtifacts: cfg.AllowBillableArtifacts,
			MaxArtifactsPerModel: cfg.MaxArtifactsPerModel, Guidance: cfg.Guidance,
			GuidancePack: cfg.GuidancePack, GuidanceLocale: cfg.GuidanceLocale,
		},
	}
	logf := options.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	env := suites.Environment{Client: client, Config: cfg, Profile: p, Logf: logf, Probes: executor}

	listCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	models, listResp, listErr := client.ListModels(listCtx)
	cancel()
	tmp.Discovery.ModelCount = len(models)
	for _, model := range models {
		tmp.Discovery.Models = append(tmp.Discovery.Models, cloneMap(model.Raw))
	}
	if cfg.HasTest("compat") {
		tmp.Discovery.Tests = append(tmp.Discovery.Tests, suites.DiscoveryTests(p, listResp, models, listErr)...)
	}

	selected, metadata := selectModels(target.Models, models)
	if len(selected) == 0 {
		if !cfg.HasTest("compat") {
			tmp.Discovery.Tests = append(tmp.Discovery.Tests, suites.DiscoveryTests(p, listResp, models, listErr)...)
		}
		finish(&tmp, p, target)
		out := protocolpkg.RunResult{Target: toTarget(target, tmp), Runtime: runtime}
		if listErr != nil {
			return out, fmt.Errorf("no models available: %w", listErr)
		}
		return out, fmt.Errorf("no models returned by endpoint")
	}
	fmt.Fprintf(os.Stderr, "aiqa: testing %d model(s): %s\n", len(selected), joinModels(selected))
	if cfg.HasTest("model-param") {
		fmt.Fprintf(os.Stderr, "aiqa: context probing is enabled; the per-model prompt budget is %d tokens\n", cfg.ContextBudget)
	}

	classifications := make([]capability.Classification, len(selected))
	modulePlans := make([][]result.ModuleAssessment, len(selected))
	for index, model := range selected {
		classifications[index] = capability.Classify(model, metadata[model], capability.Options{BaseURL: target.BaseURL, Overrides: overrides, UnknownPolicy: target.UnknownModel})
		modulePlans[index] = capability.AssessModules(classifications[index], p.Modules, capability.RoutingOptions{Modules: target.Modules, AllowBillableArtifacts: cfg.AllowBillableArtifacts})
	}
	chatRepresentative := representativeModel(selected, modulePlans, capability.ModuleChat)
	if cfg.HasTest("compat") && chatRepresentative != "" {
		tmp.Discovery.Compatibility = append(tmp.Discovery.Compatibility, suites.RunEndpointCompat(ctx, env, chatRepresentative)...)
		suites.SortTests(tmp.Discovery.Compatibility)
	}

	type modelItem struct {
		index  int
		report result.ModelReport
	}
	jobs := make(chan int)
	modelResults := make(chan modelItem, len(selected))
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
					mr.RateLimit = rateLimitStats(stats)
					addModelLatency(mr.RateLimit, mr)
				}
				fmt.Fprintf(os.Stderr, "aiqa: [%s] done\n", model)
				modelResults <- modelItem{index: index, report: mr}
			}
		}()
	}
	go func() {
		for index := range selected {
			jobs <- index
		}
		close(jobs)
		wg.Wait()
		close(modelResults)
	}()
	tmp.Models = make([]result.ModelReport, len(selected))
	for item := range modelResults {
		tmp.Models[item.index] = item.report
	}
	tmp.RateLimit = *rateLimitStats(client.PacingStats())
	addLatency(&tmp.RateLimit, tmp)
	finish(&tmp, p, target)
	return protocolpkg.RunResult{Target: toTarget(target, tmp), Runtime: runtime}, nil
}

func finish(report *result.Report, p profile.Profile, target config.TargetConfig) {
	reportpkg.Finalize(report)
	guidance.ExtractArtifacts(report)
	guidance.QualifyArtifacts(report, target.ID)
	assessment.ApplyTarget(report, p, target.ID, string(target.Protocol))
}

func profileInfo(p profile.Profile) result.ProfileInfo {
	return result.ProfileInfo{SchemaVersion: p.SchemaVersion, ID: p.ID, Protocol: p.Protocol, Revision: p.Revision, APIVersion: p.APIVersion, BaseURLSemantics: p.BaseURLSemantics, Source: p.Source, SourceCommit: p.SourceCommit, SourceRefRevision: p.SourceRefRevision, Scope: p.Scope}
}

func toTarget(target config.TargetConfig, report result.Report) result.TargetReport {
	return result.TargetReport{ID: target.ID, Protocol: string(target.Protocol), Profile: report.Profile, Discovery: report.Discovery, Models: report.Models, Requirements: report.Requirements, Summary: report.Summary, Guidance: report.Guidance, RateLimit: report.RateLimit}
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

func rateLimitStats(stats api.PacingStats) *result.RateLimitStats {
	return &result.RateLimitStats{SuccessfulRequests: stats.SuccessfulRequests, RateLimited: stats.RateLimited, RetriesRecovered: stats.RetriesRecovered, BackoffMS: stats.Backoff.Milliseconds(), SafeRate: stats.SafeRate, SafeConcurrency: stats.SafeConcurrency}
}

func addModelLatency(out *result.RateLimitStats, model result.ModelReport) {
	var values []int64
	for _, test := range model.Compatibility {
		if test.HTTP.StatusCode >= 200 && test.HTTP.StatusCode < 300 && test.DurationMS > 0 {
			values = append(values, test.DurationMS)
		}
	}
	setLatency(out, values)
}

func addLatency(out *result.RateLimitStats, report result.Report) {
	var values []int64
	add := func(test result.TestResult) {
		if test.HTTP.StatusCode >= 200 && test.HTTP.StatusCode < 300 && test.DurationMS > 0 {
			values = append(values, test.DurationMS)
		}
	}
	for _, test := range report.Discovery.Tests {
		add(test)
	}
	for _, test := range report.Discovery.Compatibility {
		add(test)
	}
	for _, model := range report.Models {
		for _, test := range model.Compatibility {
			add(test)
		}
	}
	setLatency(out, values)
}

func setLatency(out *result.RateLimitStats, values []int64) {
	if len(values) == 0 {
		return
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	var total int64
	for _, value := range values {
		total += value
	}
	out.LatencySamples = len(values)
	out.MeanLatencyMS = total / int64(len(values))
	out.P50LatencyMS = values[(len(values)-1)/2]
	out.P95LatencyMS = values[(95*len(values)-1)/100]
}
