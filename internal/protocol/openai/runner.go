// Package openai implements the native OpenAI-compatible target runner.
package openai

import (
	"context"
	"fmt"
	"os"
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
			BaseURL: target.BaseURL, Tests: append([]string(nil), cfg.Tests...), RequestedModels: append([]string(nil), requestedModels(cfg, target)...),
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

	requested := requestedModels(cfg, target)
	selected, metadata := selectModels(requested, models)
	if len(selected) == 0 {
		if !cfg.HasTest("compat") {
			tmp.Discovery.Tests = append(tmp.Discovery.Tests, suites.DiscoveryTests(p, listResp, models, listErr)...)
		}
		finish(&tmp, p, target)
		out := protocolpkg.RunResult{Target: toTarget(target, tmp), Runtime: runtime}
		if listErr != nil || !listResp.Is2xx() {
			if listErr != nil {
				return out, fmt.Errorf("model discovery failed: %w", listErr)
			}
			return out, fmt.Errorf("model discovery returned HTTP %d", listResp.StatusCode)
		}
		return out, nil
	}
	fmt.Fprintf(os.Stderr, "aiqa: testing %d model(s): %s\n", len(selected), joinModels(selected))
	if cfg.HasTest("model-param") {
		fmt.Fprintf(os.Stderr, "aiqa: context probing is enabled; the per-model prompt budget is %d tokens\n", cfg.ContextBudget)
	}

	classifications := make([]capability.Classification, len(selected))
	modulePlans := make([][]result.ModuleAssessment, len(selected))
	preflights := make([]*api.Response, len(selected))
	preflightErrors := make([]error, len(selected))
	preflightTests := make([]*result.TestResult, len(selected))
	routeBlocked := make([]bool, len(selected))
	for index, model := range selected {
		explicit := len(requested) > 0
		unknownPolicy := target.UnknownModel
		if explicit {
			unknownPolicy = "inventory"
		}
		classifications[index] = capability.Classify(model, metadata[model], capability.Options{BaseURL: target.BaseURL, Overrides: overrides, UnknownPolicy: unknownPolicy})
		if explicit && classifications[index].Unknown {
			test, response, preflightErr := suites.RunChatPreflight(ctx, env, model)
			preflightTests[index], preflights[index], preflightErrors[index] = &test, &response, preflightErr
			switch {
			case response.Is2xx() && preflightErr == nil:
				classifications[index].Unknown = false
				classifications[index].Reason = ""
				classifications[index].Capabilities = append(classifications[index].Capabilities, capability.Evidence{Capability: capability.ChatText, Source: "preflight", Rule: "openai.chat.preflight", Confidence: "high", Reason: "minimal Chat Completions preflight accepted the requested model"})
			case isTransientResponse(response, preflightErr):
				classifications[index].Reason = "model availability is inconclusive after transient preflight failure"
				routeBlocked[index] = true
				markRouteBlockedTest(preflightTests[index], "model availability could not be determined after a transient preflight failure")
			default:
				if isModelRouteRejection(response) {
					classifications[index].Reason = "explicit model route rejection"
					markRouteRejectionTest(preflightTests[index])
				} else {
					classifications[index].Reason = "model availability is inconclusive after validation preflight failure"
					markRouteBlockedTest(preflightTests[index], "model availability could not be determined from this validation response")
				}
				routeBlocked[index] = true
			}
		}
		modulePlans[index] = capability.AssessModules(classifications[index], p.Modules, capability.RoutingOptions{
			Modules:                  target.Modules,
			AllowBillableArtifacts:   cfg.AllowBillableArtifacts,
			AllowHighTokenProbes:     cfg.AllowHighTokenProbes,
			AllowPersistentResources: cfg.AllowPersistentResources,
		})
		if routeBlocked[index] {
			for moduleIndex := range modulePlans[index] {
				if modulePlans[index][moduleIndex].Applicability != result.ApplicabilityNotApplicable {
					modulePlans[index][moduleIndex].BlockedBy = []string{"chat.basic"}
				}
			}
		}
	}
	chatRepresentative := representativeModel(selected, modulePlans, capability.ModuleChat)
	if cfg.HasTest("compat") {
		if chatRepresentative != "" {
			tmp.Discovery.Compatibility = append(tmp.Discovery.Compatibility, suites.RunEndpointCompat(ctx, env, chatRepresentative)...)
		} else {
			// Endpoint-level chat error probes are provider-scoped, but there is
			// no valid text route from which to execute them for an image-only or
			// inventory-only selection. Mark them N/A instead of manufacturing a
			// provider evidence gap that would make an otherwise valid image run
			// inconclusive.
			tmp.Discovery.Compatibility = append(tmp.Discovery.Compatibility, chatEndpointNotApplicable(p)...)
		}
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
				if cfg.HasTest("compat") && preflightTests[index] != nil && routeBlocked[index] {
					mr.Compatibility = append(mr.Compatibility, *preflightTests[index])
				} else if cfg.HasTest("compat") && chatModule != nil && chatModule.Execution == result.ExecutionExecuted {
					mr.Compatibility = suites.RunModelCompatWithPreflight(ctx, env, model, preflights[index], preflightErrors[index])
				}
				if cfg.HasTest("compat") && imageModule != nil && imageModule.Execution == result.ExecutionExecuted {
					mr.Compatibility = append(mr.Compatibility, suites.RunImageCompat(ctx, env, model)...)
				}
				suites.SortTests(mr.Compatibility)
				if cfg.HasTest("model-param") && chatModule != nil && chatModule.Execution == result.ExecutionExecuted {
					mr.Parameters, mr.Context = suites.RunModelParameters(ctx, env, model, metadata[model])
				}
				stats := client.PacingStatsForModel(model)
				mr.RateLimit = protocolpkg.RateLimitStats(stats)
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
	tmp.RateLimit = *protocolpkg.RateLimitStats(client.PacingStats())
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
	return protocolpkg.SelectModels(requested, discovered, protocolpkg.ExactModelID, openAIInventoryTrusted)
}

func requestedModels(cfg config.Config, target config.TargetConfig) []string {
	if len(cfg.Models) > 0 {
		return append([]string(nil), cfg.Models...)
	}
	// Target.Models is retained as a resolved execution-list input for callers
	// that invoke a protocol runner directly. Config.Parse never fills it from
	// a protocol-specific user selector.
	return append([]string(nil), target.Models...)
}

func openAIInventoryTrusted(metadata map[string]any) bool {
	if metadata == nil {
		return false
	}
	id, _ := metadata["id"].(string)
	object, objectOK := metadata["object"].(string)
	_, createdOK := metadata["created"]
	return strings.TrimSpace(id) != "" && objectOK && object == "model" && createdOK
}

func isTransientResponse(resp api.Response, err error) bool {
	return err != nil && resp.StatusCode == 0 || resp.StatusCode == 408 || resp.StatusCode == 429 || resp.StatusCode >= 500
}

func isModelRouteRejection(resp api.Response) bool {
	if resp.StatusCode == 404 || resp.StatusCode == 410 {
		return true
	}
	if resp.StatusCode < 400 || resp.StatusCode >= 500 {
		return false
	}
	text := strings.ToLower(resp.Snippet(2000))
	for _, marker := range []string{"model not found", "model_not_found", "unknown model", "unsupported model", "model is not", "no such model", "route mismatch", "unsupported route", "does not support this model"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func markRouteRejectionTest(test *result.TestResult) {
	if test == nil {
		return
	}
	test.Signals = append(test.Signals, result.EvidenceSignal{Code: "routing.model_route_rejected", RootCause: "model-route-rejection", Parameters: map[string]any{"route_rejection": true}})
	test.Summary = "The provider explicitly rejected the requested model route"
}

func markRouteBlockedTest(test *result.TestResult, summary string) {
	if test == nil {
		return
	}
	test.Status = result.StatusError
	test.Summary = summary
	test.Signals = append(test.Signals, result.EvidenceSignal{Code: "execution.model_route_unknown", RootCause: "model-route-availability-unknown"})
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

func chatEndpointNotApplicable(p profile.Profile) []result.TestResult {
	ids := []string{"chat.error.invalid_model", "chat.error.invalid_auth", "chat.error.invalid_json", "chat.error.invalid_types"}
	tests := make([]result.TestResult, 0, len(ids))
	for _, id := range ids {
		spec := p.Probe(id)
		tests = append(tests, result.TestResult{
			ID: id, RequirementID: id, ProbeID: "aiqa.native." + id,
			Name: spec.Name, Suite: "compat", Category: spec.Category,
			Required: spec.Required, Applicability: result.ApplicabilityNotApplicable,
			Status:  result.StatusSkip,
			Summary: "chat endpoint is not applicable because no selected model has a text capability",
		})
	}
	return tests
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
