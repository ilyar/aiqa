// Package openai implements the native OpenAI-compatible target runner.
package openai

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/assessment"
	"github.com/ilyar/aiqa/internal/capability"
	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/guidance"
	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/probe"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/progress"
	protocolpkg "github.com/ilyar/aiqa/internal/protocol"
	reportpkg "github.com/ilyar/aiqa/internal/report"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/suites"
	"github.com/ilyar/aiqa/internal/telemetry"
)

// frozenExecution is the protocol-private counterpart to PreparedTarget.Binding.
// Binding provides the audit-visible summary; this retains the structured
// selection evidence that native execution needs without consulting mutable
// capability or inventory inputs after the application's freeze barrier.
type frozenExecution struct {
	selected        []string
	metadata        map[string]map[string]any
	classifications []capability.Classification
	modulePlans     [][]result.ModuleAssessment
	preflights      []*api.Response
	preflightErrs   []error
	preflightTests  []*result.TestResult
	routeBlocked    []bool
}

// RunTarget executes one OpenAI-compatible target through the common protocol
// registry contract.
func PrepareTarget(ctx context.Context, options protocolpkg.RunOptions) (protocolpkg.PreparedTarget, error) {
	client, err := api.NewClient(options.Target.BaseURL, options.Target.APIKey, options.UserAgent, options.Target.RequestInterval)
	if err != nil {
		return protocolpkg.PreparedTarget{}, err
	}
	client.SetConcurrency(options.Config.Concurrency)
	if err := protocolpkg.ConfigureClientObservability(client, options); err != nil {
		return protocolpkg.PreparedTarget{}, err
	}
	discoveryCtx := ctx
	var discoveryProvider, discoverySuite, discoveryProbe interface{ End(string, logging.Fields) }
	if options.Telemetry != nil {
		var providerCtx, suiteCtx context.Context
		providerCtx, discoveryProvider = options.Telemetry.Start(logging.Scope(ctx, options.Target.ID, "", "", "", ""), "provider", logging.Fields{"target": options.Target.ID})
		suiteCtx, discoverySuite = options.Telemetry.Start(logging.Scope(providerCtx, options.Target.ID, "", "discovery", "", ""), "suite", logging.Fields{"suite": "discovery"})
		discoveryCtx, discoveryProbe = options.Telemetry.Start(logging.Scope(suiteCtx, options.Target.ID, "", "discovery", "models.list.http", ""), "probe", logging.Fields{"probe": "aiqa.native.models.list.http"})
	}
	listCtx, cancel := api.WithRequestTimeout(discoveryCtx, options.Config.Timeout)
	listCtx = api.WithResponseBodyLimit(listCtx, protocolpkg.EffectiveInventoryByteBudget(options.Config.InventoryByteBudget))
	listCtx = api.WithJSONCollectionLimit(listCtx, options.Config.MaxCatalogModels)
	models, response, listErr := client.ListModels(listCtx)
	cancel()
	if errors.Is(listErr, api.ErrWorkLimit) {
		return protocolpkg.PreparedTarget{}, listErr
	}
	if listErr == nil && response.Is2xx() {
		if err := protocolpkg.ValidateInventoryResponseBudget(models, int64(len(response.Body)), options.Config.MaxCatalogModels, options.Config.InventoryByteBudget); err != nil {
			return protocolpkg.PreparedTarget{}, err
		}
	}
	if discoveryProbe != nil {
		outcome := "completed"
		if listErr != nil || !response.Is2xx() {
			outcome = "blocked"
		}
		discoveryProbe.End(outcome, logging.Fields{"status": outcome})
		discoverySuite.End(outcome, logging.Fields{"status": outcome})
		discoveryProvider.End(outcome, logging.Fields{"status": outcome})
	}
	selected, metadata := selectModels(requestedModels(options.Config, options.Target), models)
	if err := protocolpkg.ValidateSelectionBudget(selected, options.Config.MaxSelectedModels); err != nil {
		return protocolpkg.PreparedTarget{}, err
	}
	overrides, err := resolvedOverrides(options.Target)
	if err != nil {
		return protocolpkg.PreparedTarget{}, err
	}
	executor := probe.NewExecutor(options.Config.Concurrency)
	execution, err := freezeExecution(ctx, options, client, executor, selected, metadata, overrides)
	if err != nil {
		return protocolpkg.PreparedTarget{}, err
	}
	jobs := []protocolpkg.Job{{ID: protocolpkg.JobID(options.Target.ID, "", "inventory"), Target: options.Target.ID, Label: "model inventory"}}
	if options.Config.HasTest("compat") {
		jobs = append(jobs, protocolpkg.Job{ID: protocolpkg.JobID(options.Target.ID, "", "endpoint-compat"), Target: options.Target.ID, Label: "OpenAI endpoint compatibility"})
	}
	for _, model := range selected {
		if options.Config.HasTest("compat") {
			jobs = append(jobs, protocolpkg.Job{ID: protocolpkg.JobID(options.Target.ID, model, "compat"), Target: options.Target.ID, Model: model, Label: "OpenAI compatibility"})
		}
		if options.Config.HasTest("model-param") {
			jobs = append(jobs, protocolpkg.Job{ID: protocolpkg.JobID(options.Target.ID, model, "model-param"), Target: options.Target.ID, Model: model, Label: "model parameters"})
		}
	}
	// A failed inventory is itself the single terminal discovery job; execution
	// will preserve its report evidence without reissuing this request.
	inv := &protocolpkg.Inventory{Models: models, Response: response, Err: listErr, Runtime: protocolpkg.Runtime{Client: client, Executor: executor}}
	prepared := protocolpkg.PreparedTarget{
		Target: options.Target, Profile: options.Profile, Jobs: jobs, Candidates: execution.candidates(),
		Binding: protocolpkg.TargetBinding{
			TargetID: options.Target.ID, ProfileRevision: options.Profile.Revision,
			ResolvedOverrides: canonicalOverrides(overrides), SelectedModels: append([]string(nil), selected...),
			Classification: "openai-capability-selection-v1",
		},
	}
	prepared.Execute = func(runCtx context.Context, sink progress.Sink) (protocolpkg.RunResult, error) {
		runOptions := options
		runOptions.Progress, runOptions.Inventory = sink, inv
		return protocolpkg.ExecuteFrozen(runCtx, sink, jobs, func(ctx context.Context) (protocolpkg.RunResult, error) {
			return runTarget(ctx, runOptions, &execution)
		})
	}
	return prepared, nil
}

func RunTarget(ctx context.Context, options protocolpkg.RunOptions) (protocolpkg.RunResult, error) {
	return runTarget(ctx, options, nil)
}

func runTarget(ctx context.Context, options protocolpkg.RunOptions, frozen *frozenExecution) (protocolpkg.RunResult, error) {
	target, cfg, p := options.Target, options.Config, options.Profile

	client := (*api.Client)(nil)
	var err error
	if options.Inventory != nil && options.Inventory.Runtime.Client != nil {
		client = options.Inventory.Runtime.Client
	} else {
		client, err = api.NewClient(target.BaseURL, target.APIKey, options.UserAgent, target.RequestInterval)
		if err != nil {
			return protocolpkg.RunResult{}, err
		}
	}
	client.SetConcurrency(cfg.Concurrency)
	if err := protocolpkg.ConfigureClientScheduler(client, options); err != nil {
		return protocolpkg.RunResult{}, err
	}
	client.SetLogger(options.Logger)
	client.SetLogFields(logging.Fields{"target": target.ID, "protocol": string(target.Protocol)})
	executor := probe.NewExecutor(cfg.Concurrency)
	if options.Inventory != nil && options.Inventory.Runtime.Executor != nil {
		executor = options.Inventory.Runtime.Executor
	}
	executor.SetLogger(options.Logger)
	executor.SetLogFields(logging.Fields{"target": target.ID, "protocol": string(target.Protocol)})
	client.SetTelemetry(options.Telemetry)
	// Direct RunTarget callers do not pass through Prepare's client setup.
	// Attach the optional transport observer here too; runtime owners are added
	// only at the exact frozen-job boundaries below.
	client.SetExecutionObserver(options.ExecutionHooks.Observe)
	if options.Progress != nil {
		client.SetPacingObserver(func(target string, waiting bool, duration time.Duration) {
			options.Progress.Pacing(target, waiting, duration)
		})
	}
	client.SetOTelPropagationAllowlist(cfg.OTelPropagationTo)
	executor.SetTelemetry(options.Telemetry)
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
		logf = func(logging.Level, string, ...any) {}
	}
	plan := newProgressPlan(options.Progress, target.ID)
	env := suites.Environment{TargetID: target.ID, Client: client, Config: cfg, Profile: p, Logf: logf, Probes: executor, Telemetry: options.Telemetry, Progress: options.Progress, PlanRevision: plan.Add}

	// Discovery is a provider-scoped native test, not an unowned transport
	// setup call. Keep its concrete probe live through derivation of every
	// report-visible discovery result.
	discoveryCtx := ctx
	var models []api.Model
	var listResp api.Response
	var listErr error
	var discoveryProvider, discoverySuite, discoveryProbe interface{ End(string, logging.Fields) }
	if options.Inventory != nil {
		models, listResp, listErr = options.Inventory.Models, options.Inventory.Response, options.Inventory.Err
	} else {
		if options.Telemetry != nil {
			var providerCtx, suiteCtx context.Context
			providerCtx, discoveryProvider = options.Telemetry.Start(logging.Scope(ctx, target.ID, "", "", "", ""), "provider", logging.Fields{"target": target.ID})
			suiteCtx, discoverySuite = options.Telemetry.Start(logging.Scope(providerCtx, target.ID, "", "discovery", "", ""), "suite", logging.Fields{"suite": "discovery"})
			discoveryCtx, discoveryProbe = options.Telemetry.Start(logging.Scope(suiteCtx, target.ID, "", "discovery", "models.list.http", ""), "probe", logging.Fields{"probe": "aiqa.native.models.list.http"})
		}
		listCtx, cancel := api.WithRequestTimeout(openAIJobContext(discoveryCtx, target.ID, "", "inventory"), cfg.Timeout)
		listCtx = api.WithResponseBodyLimit(listCtx, protocolpkg.EffectiveInventoryByteBudget(cfg.InventoryByteBudget))
		listCtx = api.WithJSONCollectionLimit(listCtx, cfg.MaxCatalogModels)
		models, listResp, listErr = client.ListModels(listCtx)
		cancel()
		if errors.Is(listErr, api.ErrWorkLimit) {
			return protocolpkg.RunResult{}, listErr
		}
	}
	if listErr == nil && listResp.Is2xx() {
		if err := protocolpkg.ValidateInventoryResponseBudget(models, int64(len(listResp.Body)), cfg.MaxCatalogModels, cfg.InventoryByteBudget); err != nil {
			return protocolpkg.RunResult{}, err
		}
	}
	tmp.Discovery.ModelCount = len(models)
	for _, model := range models {
		tmp.Discovery.Models = append(tmp.Discovery.Models, cloneMap(model.Raw))
	}
	discoveryTests := suites.DiscoveryTests(p, listResp, models, listErr)
	if discoveryProbe != nil {
		outcome := protocolpkg.AggregateTestOutcome(discoveryTests)
		for _, test := range discoveryTests {
			discoveryProbe.(interface {
				Event(logging.Level, string, logging.Fields)
			}).Event(logging.Trace, "validator", logging.Fields{"result_id": test.ID, "status": string(test.Status), "http_status": test.HTTP.StatusCode, "duration_ms": test.DurationMS, "error_present": test.Error != ""})
		}
		discoveryProbe.End(outcome, logging.Fields{"status": outcome, "result_count": len(discoveryTests)})
		discoverySuite.End(outcome, logging.Fields{"status": outcome})
		discoveryProvider.End(outcome, logging.Fields{"status": outcome})
	}
	if cfg.HasTest("compat") {
		tmp.Discovery.Tests = append(tmp.Discovery.Tests, discoveryTests...)
	}
	if options.Inventory != nil {
		outcome := "completed"
		if listErr != nil || !listResp.Is2xx() {
			outcome = "blocked"
		}
		protocolpkg.StartJob(options.Progress, target.ID, "", "inventory")(outcome)
	}

	execution := frozenExecution{}
	if frozen != nil {
		execution = *frozen
	} else {
		selected, metadata := selectModels(requestedModels(cfg, target), models)
		if err := protocolpkg.ValidateSelectionBudget(selected, cfg.MaxSelectedModels); err != nil {
			return protocolpkg.RunResult{}, err
		}
		overrides, freezeErr := resolvedOverrides(target)
		if freezeErr != nil {
			return protocolpkg.RunResult{}, freezeErr
		}
		execution, freezeErr = freezeExecution(ctx, options, client, executor, selected, metadata, overrides)
		if freezeErr != nil {
			return protocolpkg.RunResult{}, freezeErr
		}
	}
	selected, metadata := execution.selected, execution.metadata
	if len(selected) == 0 {
		// Endpoint compatibility is reserved in the frozen OpenAI plan whenever
		// compat is selected. With no usable model there is no representative
		// endpoint to exercise, so close the conditional unit explicitly instead
		// of leaving an otherwise-complete run below 100%.
		if cfg.HasTest("compat") {
			protocolpkg.StartJob(options.Progress, target.ID, "", "endpoint-compat")("skipped")
		}
		if !cfg.HasTest("compat") {
			tmp.Discovery.Tests = append(tmp.Discovery.Tests, discoveryTests...)
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
	logf(logging.Info, "[%s] testing %d model(s): %s", target.ID, len(selected), joinModels(selected))
	if cfg.HasTest("model-param") {
		logf(logging.Warn, "[%s] context probing is enabled; the per-model prompt budget is %d tokens", target.ID, cfg.ContextBudget)
	}

	classifications, modulePlans := execution.classifications, execution.modulePlans
	preflights, preflightErrors, preflightTests := execution.preflights, execution.preflightErrs, execution.preflightTests
	routeBlocked := execution.routeBlocked
	chatRepresentative := representativeModel(selected, modulePlans, capability.ModuleChat)
	if options.Inventory == nil {
		plan.Set(plannedNativeProbes(cfg, modulePlans, routeBlocked, chatRepresentative))
	}
	if cfg.HasTest("compat") {
		endpointDone := protocolpkg.StartJob(options.Progress, target.ID, "", "endpoint-compat")
		if chatRepresentative != "" {
			endpointCtx := openAIJobContext(ctx, target.ID, "", "endpoint-compat")
			tmp.Discovery.Compatibility = append(tmp.Discovery.Compatibility, suites.RunEndpointCompat(endpointCtx, env, chatRepresentative)...)
		} else {
			// Endpoint-level chat error probes are provider-scoped, but there is
			// no valid text route from which to execute them for an image-only or
			// inventory-only selection. Mark them N/A instead of manufacturing a
			// provider evidence gap that would make an otherwise valid image run
			// inconclusive.
			if hasUnknownRequestedModule(modulePlans, capability.ModuleChat) {
				tmp.Discovery.Compatibility = append(tmp.Discovery.Compatibility, chatEndpointUnknown(p)...)
			} else {
				tmp.Discovery.Compatibility = append(tmp.Discovery.Compatibility, chatEndpointNotApplicable(p)...)
			}
		}
		suites.SortTests(tmp.Discovery.Compatibility)
		if len(tmp.Discovery.Compatibility) == 0 {
			endpointDone("skipped")
		} else {
			endpointDone("completed")
		}
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
				modelCtx := logging.Scope(ctx, target.ID, model, "", "", "")
				var modelEnd func(string, logging.Fields)
				if options.Telemetry != nil {
					var modelOp interface{ End(string, logging.Fields) }
					modelCtx, modelOp = options.Telemetry.Start(modelCtx, "model", logging.Fields{"model": model})
					modelEnd = modelOp.End
				} else {
					modelEnd = func(string, logging.Fields) {}
				}
				mr := result.ModelReport{ID: model, Metadata: cloneMap(metadata[model]), Modules: modulePlans[index]}
				for _, evidence := range classifications[index].Capabilities {
					mr.Capabilities = append(mr.Capabilities, result.CapabilityAssessment{ID: evidence.Capability, Source: evidence.Source, Rule: evidence.Rule, Confidence: evidence.Confidence, Reason: evidence.Reason})
				}
				chatModule := capability.Module(mr.Modules, capability.ModuleChat)
				imageModule := capability.Module(mr.Modules, capability.ModuleImages)
				compatDone := func(string) {}
				if cfg.HasTest("compat") {
					compatDone = protocolpkg.StartJob(options.Progress, target.ID, model, "compat")
				}
				if cfg.HasTest("compat") && preflightTests[index] != nil && routeBlocked[index] {
					mr.Compatibility = append(mr.Compatibility, *preflightTests[index])
				} else if cfg.HasTest("compat") && chatModule != nil && chatModule.Execution == result.ExecutionExecuted {
					compatCtx := openAIJobContext(modelCtx, target.ID, model, "compat")
					mr.Compatibility = suites.RunModelCompatWithPreflight(compatCtx, env, model, preflights[index], preflightErrors[index])
				}
				if cfg.HasTest("compat") && imageModule != nil && imageModule.Execution == result.ExecutionExecuted {
					compatCtx := openAIJobContext(modelCtx, target.ID, model, "compat")
					imageTests := suites.RunImageCompat(compatCtx, env, model)
					resolveProbePolicyImageRejection(classifications[index], imageModule, imageTests)
					mr.Compatibility = append(mr.Compatibility, imageTests...)
				}
				if cfg.HasTest("compat") {
					if len(mr.Compatibility) == 0 {
						compatDone("skipped")
					} else {
						compatDone("completed")
					}
				}
				suites.SortTests(mr.Compatibility)
				if routeBlocked[index] && options.Telemetry != nil {
					protocolpkg.LogTestResultsWithTelemetry(modelCtx, options.Telemetry, target.ID, model, mr.Compatibility)
				} else if routeBlocked[index] {
					protocolpkg.LogTestResults(logf, target.ID, model, mr.Compatibility)
				}
				paramDone := func(string) {}
				if cfg.HasTest("model-param") {
					paramDone = protocolpkg.StartJob(options.Progress, target.ID, model, "model-param")
				}
				if cfg.HasTest("model-param") && chatModule != nil && chatModule.Execution == result.ExecutionExecuted {
					paramCtx := openAIJobContext(modelCtx, target.ID, model, "model-param")
					mr.Parameters, mr.Context = suites.RunModelParameters(paramCtx, env, model, metadata[model])
					paramDone("completed")
				} else if cfg.HasTest("model-param") {
					paramDone("skipped")
				}
				stats := client.PacingStatsForModel(model)
				mr.RateLimit = protocolpkg.RateLimitStats(stats)
				modelEnd("completed", logging.Fields{"status": "completed"})
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

func resolveProbePolicyImageRejection(classification capability.Classification, imageModule *result.ModuleAssessment, tests []result.TestResult) bool {
	if !classification.ProbeEligible || imageModule == nil || imageModule.Source != "probe_policy" || !suites.MarkProbePolicyImageModelUnsupported(tests) {
		return false
	}
	imageModule.Applicability = result.ApplicabilityNotApplicable
	imageModule.Reason = "image endpoint rejected the probe-policy model as unsupported"
	return true
}

func openAIJobContext(ctx context.Context, target, model, kind string) context.Context {
	return api.WithExecutionOwner(ctx, api.ExecutionOwner{TargetID: target, JobID: protocolpkg.JobID(target, model, kind)})
}

// progressPlan serializes revisions from concurrently executing models. A
// revision is published before the corresponding suite can start a probe, so
// the reporter can never observe more planned probe ends than its total.
type progressPlan struct {
	sink   progress.Sink
	target string
	mu     sync.Mutex
	total  int
}

func newProgressPlan(sink progress.Sink, target string) *progressPlan {
	return &progressPlan{sink: sink, target: target}
}

func (p *progressPlan) Set(total int) {
	if p == nil || p.sink == nil {
		return
	}
	if total < 0 {
		total = 0
	}
	p.mu.Lock()
	p.total = total
	p.sink.Plan(p.target, total)
	p.mu.Unlock()
}

func (p *progressPlan) Add(units int) {
	if p == nil || p.sink == nil || units <= 0 {
		return
	}
	p.mu.Lock()
	p.total += units
	p.sink.RevisePlan(p.target, p.total)
	p.mu.Unlock()
}

func plannedNativeProbes(cfg config.Config, plans [][]result.ModuleAssessment, routeBlocked []bool, chatRepresentative string) int {
	if !cfg.HasTest("compat") {
		return 0
	}
	total := 0
	if chatRepresentative != "" {
		total += suites.EndpointCompatProbeCount
	}
	for index, modules := range plans {
		if index < len(routeBlocked) && routeBlocked[index] {
			continue
		}
		if module := capability.Module(modules, capability.ModuleChat); module != nil && module.Execution == result.ExecutionExecuted {
			total += suites.ModelCompatProbeCount
		}
		if module := capability.Module(modules, capability.ModuleImages); module != nil && module.Execution == result.ExecutionExecuted {
			total += suites.ImageCompatProbeCount
		}
	}
	return total
}

func hasUnknownRequestedModule(plans [][]result.ModuleAssessment, moduleID string) bool {
	for _, plan := range plans {
		module := capability.Module(plan, moduleID)
		if module != nil && module.Requested && module.Applicability == result.ApplicabilityUnknown && module.Reason == "insufficient capability evidence" {
			return true
		}
	}
	return false
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

func freezeExecution(ctx context.Context, options protocolpkg.RunOptions, client *api.Client, executor *probe.Executor, selected []string, metadata map[string]map[string]any, overrides map[string][]string) (frozenExecution, error) {
	target, cfg, p := options.Target, options.Config, options.Profile
	logf := options.Logf
	if logf == nil {
		logf = func(logging.Level, string, ...any) {}
	}
	env := suites.Environment{TargetID: target.ID, Client: client, Config: cfg, Profile: p, Logf: logf, Probes: executor, Telemetry: options.Telemetry, Progress: options.Progress}
	frozen := frozenExecution{
		selected: append([]string(nil), selected...), metadata: metadata,
		classifications: make([]capability.Classification, len(selected)), modulePlans: make([][]result.ModuleAssessment, len(selected)),
		preflights: make([]*api.Response, len(selected)), preflightErrs: make([]error, len(selected)), preflightTests: make([]*result.TestResult, len(selected)), routeBlocked: make([]bool, len(selected)),
	}
	explicit := len(requestedModels(cfg, target)) > 0
	for index, model := range selected {
		unknownPolicy := target.UnknownModel
		if explicit && unknownPolicy != "probe" {
			unknownPolicy = "inventory"
		}
		frozen.classifications[index] = capability.Classify(model, metadata[model], capability.Options{BaseURL: target.BaseURL, Overrides: overrides, UnknownPolicy: unknownPolicy})
		if explicit && unknownPolicy != "probe" && frozen.classifications[index].Unknown {
			preflightCtx := logging.Scope(ctx, target.ID, model, "", "", "")
			var modelEnd func(string, logging.Fields)
			if options.Telemetry != nil {
				var modelOp *telemetry.Operation
				preflightCtx, modelOp = options.Telemetry.Start(preflightCtx, "model", logging.Fields{"model": model})
				modelEnd = modelOp.End
			} else {
				modelEnd = func(string, logging.Fields) {}
			}
			test, response, preflightErr := suites.RunChatPreflight(preflightCtx, env, model)
			modelEnd(string(test.Status), logging.Fields{"status": string(test.Status), "http_status": test.HTTP.StatusCode})
			frozen.preflightTests[index], frozen.preflights[index], frozen.preflightErrs[index] = &test, &response, preflightErr
			switch {
			case response.Is2xx() && preflightErr == nil:
				frozen.classifications[index].Unknown = false
				frozen.classifications[index].Reason = ""
				frozen.classifications[index].Capabilities = append(frozen.classifications[index].Capabilities, capability.Evidence{Capability: capability.ChatText, Source: "preflight", Rule: "openai.chat.preflight", Confidence: "high", Reason: "minimal Chat Completions preflight accepted the requested model"})
			case isTransientResponse(response, preflightErr):
				frozen.classifications[index].Reason = "model availability is inconclusive after transient preflight failure"
				frozen.routeBlocked[index] = true
				markRouteBlockedTest(frozen.preflightTests[index], "model availability could not be determined after a transient preflight failure")
			default:
				if isModelRouteRejection(response) {
					frozen.classifications[index].Reason = "explicit model route rejection"
					markRouteRejectionTest(frozen.preflightTests[index])
				} else {
					frozen.classifications[index].Reason = "model availability is inconclusive after validation preflight failure"
					markRouteBlockedTest(frozen.preflightTests[index], "model availability could not be determined from this validation response")
				}
				frozen.routeBlocked[index] = true
			}
		}
		frozen.modulePlans[index] = capability.AssessModules(frozen.classifications[index], p.Modules, capability.RoutingOptions{Modules: target.Modules, AllowBillableArtifacts: cfg.AllowBillableArtifacts, AllowHighTokenProbes: cfg.AllowHighTokenProbes, AllowPersistentResources: cfg.AllowPersistentResources})
		if frozen.routeBlocked[index] {
			for moduleIndex := range frozen.modulePlans[index] {
				if frozen.modulePlans[index][moduleIndex].Applicability != result.ApplicabilityNotApplicable {
					frozen.modulePlans[index][moduleIndex].BlockedBy = []string{"chat.basic"}
				}
			}
		}
	}
	return frozen, nil
}

func (f frozenExecution) candidates() []result.ModelReport {
	candidates := make([]result.ModelReport, 0, len(f.selected))
	for index, model := range f.selected {
		candidate := result.ModelReport{ID: model, Metadata: cloneMap(f.metadata[model]), Modules: append([]result.ModuleAssessment(nil), f.modulePlans[index]...)}
		for _, evidence := range f.classifications[index].Capabilities {
			candidate.Capabilities = append(candidate.Capabilities, result.CapabilityAssessment{ID: evidence.Capability, Source: evidence.Source, Rule: evidence.Rule, Confidence: evidence.Confidence, Reason: evidence.Reason})
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func resolvedOverrides(target config.TargetConfig) (map[string][]string, error) {
	overrides, err := capability.LoadOverrideFile(target.CapabilityFile)
	if err != nil {
		return nil, err
	}
	if overrides == nil {
		overrides = map[string][]string{}
	}
	for _, raw := range target.ModelCapabilities {
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("invalid --model-capability %q", raw)
		}
		values := strings.Split(parts[1], "+")
		for index := range values {
			values[index] = strings.TrimSpace(values[index])
			if !capability.Known[values[index]] {
				return nil, fmt.Errorf("unknown capability %q in --model-capability", values[index])
			}
		}
		overrides[strings.TrimSpace(parts[0])] = values
	}
	return overrides, nil
}

func canonicalOverrides(overrides map[string][]string) []string {
	keys := make([]string, 0, len(overrides))
	for model := range overrides {
		keys = append(keys, model)
	}
	sort.Strings(keys)
	resolved := make([]string, 0, len(keys))
	for _, model := range keys {
		resolved = append(resolved, model+"="+strings.Join(overrides[model], "+"))
	}
	return resolved
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

func chatEndpointUnknown(p profile.Profile) []result.TestResult {
	tests := chatEndpointNotApplicable(p)
	for index := range tests {
		tests[index].Applicability = result.ApplicabilityUnknown
		tests[index].Summary = "chat endpoint applicability is unknown because selected model capability evidence is unavailable"
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
