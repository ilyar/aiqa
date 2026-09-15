package anthropic

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
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
	"github.com/ilyar/aiqa/internal/scheduler"
	"github.com/ilyar/aiqa/internal/telemetry"
)

type RunOptions struct {
	Target    config.TargetConfig
	Config    config.Config
	Profile   profile.Profile
	UserAgent string
	Logf      func(logging.Level, string, ...any)
	Logger    *logging.Logger
	Telemetry *telemetry.Provider
	// Progress receives the post-discovery native-probe plan. It is optional so
	// package callers that do not render operator progress remain compatible.
	Progress          progress.Sink
	Runtime           *Runtime
	Inventory         *protocolpkg.Inventory
	Scheduler         *scheduler.Broker
	PacingGroup       string
	ExecutionObserver api.ExecutionObserver
	frozen            *frozenExecution
}

// frozenExecution retains every decision and probe observation that Prepare
// makes. Execute must consume this snapshot rather than consulting mutable
// inventory or capability inputs again.
type frozenExecution struct {
	client          *Client
	executor        *probe.Executor
	selected        []string
	metadata        map[string]map[string]any
	classifications []capability.Classification
	modulePlans     [][]result.ModuleAssessment
	preflights      []*api.Response
	preflightErrs   []error
	preflightTests  []*result.TestResult
	routeBlocked    []bool
	overrideBinding []string
}

// Runtime exposes the target-owned transport and executor to the extension
// phase. Native and extension probes therefore share one adaptive pacer and
// one global concurrency budget for the target.
type Runtime struct {
	Client   *api.Client
	Executor *probe.Executor
}

type progressPlan struct {
	mu     sync.Mutex
	sink   progress.Sink
	target string
	total  int
}

func newProgressPlan(sink progress.Sink, target string, total int) *progressPlan {
	if sink == nil {
		return nil
	}
	sink.Plan(target, total)
	return &progressPlan{sink: sink, target: target, total: total}
}

func (p *progressPlan) addDynamicProbe() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.total++
	p.sink.RevisePlan(p.target, p.total)
}

// RunTarget adapts the Anthropic implementation to the shared protocol
// registry contract.
func RunTarget(ctx context.Context, options protocolpkg.RunOptions) (protocolpkg.RunResult, error) {
	return runTarget(ctx, options, nil)
}

func runTarget(ctx context.Context, options protocolpkg.RunOptions, frozen *frozenExecution) (protocolpkg.RunResult, error) {
	runtime := &Runtime{}
	target, err := Run(ctx, RunOptions{
		Target: options.Target, Config: options.Config, Profile: options.Profile,
		UserAgent: options.UserAgent, Logf: options.Logf, Logger: options.Logger, Telemetry: options.Telemetry, Progress: options.Progress, Runtime: runtime, Inventory: options.Inventory, Scheduler: options.Scheduler, PacingGroup: options.PacingGroup, ExecutionObserver: options.ExecutionHooks.Observe, frozen: frozen,
	})
	return protocolpkg.RunResult{
		Target:  target,
		Runtime: protocolpkg.Runtime{Client: runtime.Client, Executor: runtime.Executor},
	}, err
}

// PrepareTarget performs the only models-list request for an Anthropic target.
// The Execute closure carries that observation into RunTarget after the global
// plan has been frozen.
func PrepareTarget(ctx context.Context, options protocolpkg.RunOptions) (protocolpkg.PreparedTarget, error) {
	client, err := NewClient(options.Target.BaseURL, options.Target.APIKey, options.Target.APIVersion, options.UserAgent, options.Target.RequestInterval)
	if err != nil {
		return protocolpkg.PreparedTarget{}, err
	}
	client.SetConcurrency(options.Config.Concurrency)
	if err := protocolpkg.ConfigureClientObservability(client.TransportClient(), options); err != nil {
		return protocolpkg.PreparedTarget{}, err
	}
	discoveryCtx := ctx
	var discoveryProvider, discoverySuite, discoveryProbe interface{ End(string, logging.Fields) }
	if options.Telemetry != nil {
		var providerCtx, suiteCtx context.Context
		providerCtx, discoveryProvider = options.Telemetry.Start(logging.Scope(ctx, options.Target.ID, "", "", "", ""), "provider", logging.Fields{"target": options.Target.ID})
		suiteCtx, discoverySuite = options.Telemetry.Start(logging.Scope(providerCtx, options.Target.ID, "", "discovery", "", ""), "suite", logging.Fields{"suite": "discovery"})
		discoveryCtx, discoveryProbe = options.Telemetry.Start(logging.Scope(suiteCtx, options.Target.ID, "", "discovery", "anthropic.models.list.http", ""), "probe", logging.Fields{"probe": "aiqa.native.anthropic.models.list.http"})
	}
	listCtx, cancel := api.WithRequestTimeout(discoveryCtx, options.Config.Timeout)
	models, response, listErr := client.ListModels(listCtx)
	cancel()
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
	executor := probe.NewExecutor(options.Config.Concurrency)
	frozen, err := freezeExecution(ctx, options, client, executor, selected, metadata)
	if err != nil {
		return protocolpkg.PreparedTarget{}, err
	}
	jobs := []protocolpkg.Job{{ID: protocolpkg.JobID(options.Target.ID, "", "inventory"), Target: options.Target.ID, Label: "model inventory"}}
	for _, model := range frozen.selected {
		if options.Config.HasTest("compat") {
			jobs = append(jobs, protocolpkg.Job{ID: protocolpkg.JobID(options.Target.ID, model, "compat"), Target: options.Target.ID, Model: model, Label: "Anthropic compatibility"})
		}
		if options.Config.HasTest("model-param") {
			jobs = append(jobs, protocolpkg.Job{ID: protocolpkg.JobID(options.Target.ID, model, "model-param"), Target: options.Target.ID, Model: model, Label: "model parameters"})
		}
	}
	inv := &protocolpkg.Inventory{Models: models, Response: response, Err: listErr, Runtime: protocolpkg.Runtime{Client: client.TransportClient(), Executor: executor}}
	prepared := protocolpkg.PreparedTarget{Target: options.Target, Profile: options.Profile, Jobs: jobs, Candidates: frozen.candidates(), Binding: protocolpkg.TargetBinding{TargetID: options.Target.ID, ProfileRevision: options.Profile.Revision, ResolvedOverrides: frozen.overrides(), SelectedModels: append([]string(nil), frozen.selected...), Classification: "anthropic-capability-selection-v1"}}
	prepared.Execute = func(runCtx context.Context, sink progress.Sink) (protocolpkg.RunResult, error) {
		runOptions := options
		runOptions.Progress, runOptions.Inventory = sink, inv
		return protocolpkg.ExecuteFrozen(runCtx, sink, jobs, func(ctx context.Context) (protocolpkg.RunResult, error) { return runTarget(ctx, runOptions, &frozen) })
	}
	return prepared, nil
}

func Run(ctx context.Context, options RunOptions) (result.TargetReport, error) {
	target, cfg, p := options.Target, options.Config, options.Profile
	var overrides map[string][]string
	var err error
	if options.frozen == nil {
		overrides, err = resolvedOverrides(target)
		if err != nil {
			return result.TargetReport{}, err
		}
	}
	client := options.frozenClient()
	if client == nil {
		client, err = NewClient(target.BaseURL, target.APIKey, target.APIVersion, options.UserAgent, target.RequestInterval)
		if err != nil {
			return result.TargetReport{}, err
		}
	}
	client.SetConcurrency(cfg.Concurrency)
	if err := protocolpkg.ConfigureClientScheduler(client.TransportClient(), protocolpkg.RunOptions{Target: target, Scheduler: options.Scheduler, PacingGroup: options.PacingGroup}); err != nil {
		return result.TargetReport{}, err
	}
	client.TransportClient().SetLogger(options.Logger)
	client.TransportClient().SetLogFields(logging.Fields{"target": target.ID, "protocol": string(target.Protocol)})
	probeExecutor := options.frozenExecutor()
	if probeExecutor == nil {
		probeExecutor = probe.NewExecutor(cfg.Concurrency)
	}
	probeExecutor.SetLogger(options.Logger)
	probeExecutor.SetLogFields(logging.Fields{"target": target.ID, "protocol": string(target.Protocol)})
	client.TransportClient().SetTelemetry(options.Telemetry)
	client.TransportClient().SetExecutionObserver(options.ExecutionObserver)
	if options.Progress != nil {
		client.TransportClient().SetPacingObserver(func(target string, waiting bool, duration time.Duration) {
			options.Progress.Pacing(target, waiting, duration)
		})
	}
	client.TransportClient().SetOTelPropagationAllowlist(cfg.OTelPropagationTo)
	probeExecutor.SetTelemetry(options.Telemetry)
	if options.Runtime != nil {
		options.Runtime.Client = client.TransportClient()
		options.Runtime.Executor = probeExecutor
	}
	requested := requestedModels(cfg, target)
	tmp := result.Report{SchemaVersion: "aiqa.report.internal", Profile: profileInfo(p), Config: result.PublicConfig{BaseURL: target.BaseURL, Tests: cfg.Tests, RequestedModels: requested, Timeout: cfg.Timeout.String(), Formats: cfg.Formats, ExtSuites: cfg.ExtSuites, FailOn: cfg.FailOn, Guidance: cfg.Guidance, GuidancePack: cfg.GuidancePack, GuidanceLocale: cfg.GuidanceLocale}}

	discoveryCtx := ctx
	var discoveryProvider, discoverySuite, discoveryProbe interface{ End(string, logging.Fields) }
	if options.Inventory == nil && options.Telemetry != nil {
		var providerCtx, suiteCtx context.Context
		providerCtx, discoveryProvider = options.Telemetry.Start(logging.Scope(ctx, target.ID, "", "", "", ""), "provider", logging.Fields{"target": target.ID})
		suiteCtx, discoverySuite = options.Telemetry.Start(logging.Scope(providerCtx, target.ID, "", "discovery", "", ""), "suite", logging.Fields{"suite": "discovery"})
		discoveryCtx, discoveryProbe = options.Telemetry.Start(logging.Scope(suiteCtx, target.ID, "", "discovery", "anthropic.models.list.http", ""), "probe", logging.Fields{"probe": "aiqa.native.anthropic.models.list.http"})
	}
	var models []api.Model
	var listResp api.Response
	var listErr error
	if options.Inventory != nil {
		models, listResp, listErr = options.Inventory.Models, options.Inventory.Response, options.Inventory.Err
	} else {
		listCtx, cancel := api.WithRequestTimeout(anthropicJobContext(discoveryCtx, target.ID, "", "inventory"), cfg.Timeout)
		models, listResp, listErr = client.ListModels(listCtx)
		cancel()
	}
	tmp.Discovery.ModelCount = len(models)
	for _, model := range models {
		tmp.Discovery.Models = append(tmp.Discovery.Models, cloneMap(model.Raw))
	}
	discoveryTests := discoveryResults(p, listResp, models, listErr)
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
		tmp.Discovery.Tests = append(tmp.Discovery.Tests, paginationResult(ctx, client, cfg.Timeout, p, listResp, models, listErr, options.Telemetry))
	}
	if options.Inventory != nil {
		outcome := "completed"
		if listErr != nil || !listResp.Is2xx() {
			outcome = "blocked"
		}
		protocolpkg.StartJob(options.Progress, target.ID, "", "inventory")(outcome)
	}
	selected, metadata := selectModels(requested, models)
	if options.frozen != nil {
		selected, metadata = options.frozen.selected, options.frozen.metadata
	}
	if len(selected) == 0 {
		reportpkg.Finalize(&tmp)
		guidance.ExtractArtifacts(&tmp)
		guidance.QualifyArtifacts(&tmp, target.ID)
		assessment.ApplyTarget(&tmp, p, target.ID, string(target.Protocol))
		if listErr != nil || !listResp.Is2xx() {
			if listErr != nil {
				return toTarget(target, tmp), fmt.Errorf("model discovery failed: %w", listErr)
			}
			return toTarget(target, tmp), fmt.Errorf("model discovery returned HTTP %d", listResp.StatusCode)
		}
		return toTarget(target, tmp), nil
	}
	if options.Logf != nil {
		options.Logf(logging.Info, "[%s] testing %d model(s): %s", target.ID, len(selected), strings.Join(selected, ", "))
		if containsString(cfg.Modules, "anthropic.thinking") {
			options.Logf(logging.Warn, "[%s] high-token thinking probes are enabled and may consume at least 4096 output-token budget per model", target.ID)
		}
	}
	type modelItem struct {
		index  int
		report result.ModelReport
	}
	preflights := make([]*api.Response, len(selected))
	preflightErrors := make([]error, len(selected))
	preflightTests := make([]*result.TestResult, len(selected))
	routeBlocked := make([]bool, len(selected))
	classifications := make([]capability.Classification, len(selected))
	modulePlans := make([][]result.ModuleAssessment, len(selected))
	if options.frozen != nil {
		preflights, preflightErrors, preflightTests = options.frozen.preflights, options.frozen.preflightErrs, options.frozen.preflightTests
		routeBlocked, classifications, modulePlans = options.frozen.routeBlocked, options.frozen.classifications, options.frozen.modulePlans
	} else {
		for index, model := range selected {
			unknownPolicy := target.UnknownModel
			if len(requested) > 0 && unknownPolicy != "probe" {
				unknownPolicy = "inventory"
			}
			classifications[index] = capability.ClassifyAnthropic(model, metadata[model], capability.Options{BaseURL: target.BaseURL, Overrides: overrides, UnknownPolicy: unknownPolicy})
			if len(requested) > 0 && unknownPolicy != "probe" && classifications[index].Unknown {
				var test result.TestResult
				var response api.Response
				var preflightErr error
				preflightCtx := logging.Scope(ctx, target.ID, model, "", "", "")
				var modelEnd func(string, logging.Fields)
				if options.Telemetry != nil {
					var modelOp *telemetry.Operation
					preflightCtx, modelOp = options.Telemetry.Start(preflightCtx, "model", logging.Fields{"model": model})
					modelEnd = modelOp.End
				} else {
					modelEnd = func(string, logging.Fields) {}
				}
				// The preflight is itself the report-visible basic-message test. Its
				// request and validator must therefore share one concrete lifecycle.
				runNativeProbe(preflightCtx, options.Telemetry, "anthropic.messages.basic", func(probeCtx context.Context) result.TestResult {
					test, response, preflightErr = runMessagesPreflight(probeCtx, client, cfg.Timeout, p, model)
					return test
				})
				modelEnd(string(test.Status), logging.Fields{"status": string(test.Status), "http_status": test.HTTP.StatusCode})
				preflightTests[index], preflights[index], preflightErrors[index] = &test, &response, preflightErr
				switch {
				case response.Is2xx() && preflightErr == nil:
					classifications[index].Unknown = false
					classifications[index].Reason = ""
					classifications[index].Capabilities = append(classifications[index].Capabilities, capability.Evidence{Capability: capability.AnthropicMessagesText, Source: "preflight", Rule: "anthropic.messages.preflight", Confidence: "high", Reason: "minimal Messages preflight accepted the requested model"})
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
						modulePlans[index][moduleIndex].BlockedBy = []string{"anthropic.messages.basic"}
					}
				}
			}
		}
	}
	var plan *progressPlan
	if options.Inventory == nil {
		plan = newProgressPlan(options.Progress, target.ID, plannedProbeCount(cfg, selected, modulePlans, routeBlocked))
	}
	jobs := make(chan int)
	modelResults := make(chan modelItem, len(selected))
	workers := cfg.Concurrency
	if workers > len(selected) {
		workers = len(selected)
	}
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				model := selected[index]
				modelCtx := logging.Scope(ctx, target.ID, model, "", "", "")
				var modelEnd func(string, logging.Fields)
				if options.Telemetry != nil {
					_, modelOp := options.Telemetry.Start(modelCtx, "model", logging.Fields{"model": model})
					modelCtx = modelOp.Context()
					modelEnd = modelOp.End
				} else {
					modelEnd = func(string, logging.Fields) {}
				}
				classification := classifications[index]
				modules := modulePlans[index]
				mr := result.ModelReport{ID: model, Metadata: cloneMap(metadata[model]), Modules: modules}
				for _, evidence := range classification.Capabilities {
					mr.Capabilities = append(mr.Capabilities, result.CapabilityAssessment{ID: evidence.Capability, Source: evidence.Source, Rule: evidence.Rule, Confidence: evidence.Confidence, Reason: evidence.Reason})
				}
				compatDone := func(string) {}
				if cfg.HasTest("compat") {
					compatDone = protocolpkg.StartJob(options.Progress, target.ID, model, "compat")
				}
				if cfg.HasTest("compat") && preflightTests[index] != nil && routeBlocked[index] {
					mr.Compatibility = append(mr.Compatibility, *preflightTests[index])
				} else if cfg.HasTest("compat") {
					compatCtx := anthropicJobContext(modelCtx, target.ID, model, "compat")
					mr.Compatibility = runModelWithPreflight(compatCtx, client, cfg, p, model, modules, probeExecutor, options.Telemetry, preflights[index], preflightErrors[index], plan)
				}
				if cfg.HasTest("compat") {
					if len(mr.Compatibility) == 0 {
						compatDone("skipped")
					} else {
						compatDone("completed")
					}
				}
				if cfg.HasTest("model-param") {
					protocolpkg.StartJob(options.Progress, target.ID, model, "model-param")("skipped")
				}
				sort.Slice(mr.Compatibility, func(i, j int) bool { return mr.Compatibility[i].ID < mr.Compatibility[j].ID })
				if options.Telemetry != nil {
					protocolpkg.LogTestResultsWithTelemetry(modelCtx, options.Telemetry, target.ID, model, mr.Compatibility)
				} else {
					protocolpkg.LogTestResults(options.Logf, target.ID, model, mr.Compatibility)
				}
				stats := client.PacingStatsForModel(model)
				mr.RateLimit = rateLimitStats(stats)
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
	stats := client.PacingStats()
	tmp.RateLimit = *rateLimitStats(stats)
	reportpkg.Finalize(&tmp)
	guidance.ExtractArtifacts(&tmp)
	guidance.QualifyArtifacts(&tmp, target.ID)
	assessment.ApplyTarget(&tmp, p, target.ID, string(target.Protocol))
	return toTarget(target, tmp), nil
}

func anthropicJobContext(ctx context.Context, target, model, kind string) context.Context {
	return api.WithExecutionOwner(ctx, api.ExecutionOwner{TargetID: target, JobID: protocolpkg.JobID(target, model, kind)})
}

func (o RunOptions) frozenClient() *Client {
	if o.frozen != nil {
		return o.frozen.client
	}
	return nil
}

func (o RunOptions) frozenExecutor() *probe.Executor {
	if o.frozen != nil {
		return o.frozen.executor
	}
	return nil
}

func freezeExecution(ctx context.Context, options protocolpkg.RunOptions, client *Client, executor *probe.Executor, selected []string, metadata map[string]map[string]any) (frozenExecution, error) {
	overrides, err := resolvedOverrides(options.Target)
	if err != nil {
		return frozenExecution{}, err
	}
	frozen := frozenExecution{client: client, executor: executor, selected: append([]string(nil), selected...), metadata: metadata, overrideBinding: canonicalOverrides(overrides), classifications: make([]capability.Classification, len(selected)), modulePlans: make([][]result.ModuleAssessment, len(selected)), preflights: make([]*api.Response, len(selected)), preflightErrs: make([]error, len(selected)), preflightTests: make([]*result.TestResult, len(selected)), routeBlocked: make([]bool, len(selected))}
	explicit := len(requestedModels(options.Config, options.Target)) > 0
	for index, model := range selected {
		unknown := options.Target.UnknownModel
		if explicit && unknown != "probe" {
			unknown = "inventory"
		}
		classification := capability.ClassifyAnthropic(model, metadata[model], capability.Options{BaseURL: options.Target.BaseURL, Overrides: overrides, UnknownPolicy: unknown})
		if explicit && unknown != "probe" && classification.Unknown {
			var test result.TestResult
			var response api.Response
			var preflightErr error
			probeCtx := logging.Scope(ctx, options.Target.ID, model, "", "", "")
			runNativeProbe(probeCtx, options.Telemetry, "anthropic.messages.basic", func(probeCtx context.Context) result.TestResult {
				test, response, preflightErr = runMessagesPreflight(probeCtx, client, options.Config.Timeout, options.Profile, model)
				return test
			})
			frozen.preflightTests[index], frozen.preflights[index], frozen.preflightErrs[index] = &test, &response, preflightErr
			switch {
			case response.Is2xx() && preflightErr == nil:
				classification.Unknown, classification.Reason = false, ""
				classification.Capabilities = append(classification.Capabilities, capability.Evidence{Capability: capability.AnthropicMessagesText, Source: "preflight", Rule: "anthropic.messages.preflight", Confidence: "high", Reason: "minimal Messages preflight accepted the requested model"})
			case isTransientResponse(response, preflightErr):
				classification.Reason, frozen.routeBlocked[index] = "model availability is inconclusive after transient preflight failure", true
				markRouteBlockedTest(&test, "model availability could not be determined after a transient preflight failure")
			default:
				frozen.routeBlocked[index] = true
				if isModelRouteRejection(response) {
					classification.Reason = "explicit model route rejection"
					markRouteRejectionTest(&test)
				} else {
					classification.Reason = "model availability is inconclusive after validation preflight failure"
					markRouteBlockedTest(&test, "model availability could not be determined from this validation response")
				}
			}
		}
		frozen.classifications[index] = classification
		frozen.modulePlans[index] = capability.AssessModules(classification, options.Profile.Modules, capability.RoutingOptions{Modules: options.Target.Modules, AllowBillableArtifacts: options.Config.AllowBillableArtifacts, AllowHighTokenProbes: options.Config.AllowHighTokenProbes, AllowPersistentResources: options.Config.AllowPersistentResources})
		if frozen.routeBlocked[index] {
			for moduleIndex := range frozen.modulePlans[index] {
				if frozen.modulePlans[index][moduleIndex].Applicability != result.ApplicabilityNotApplicable {
					frozen.modulePlans[index][moduleIndex].BlockedBy = []string{"anthropic.messages.basic"}
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

func (f frozenExecution) overrides() []string { return append([]string(nil), f.overrideBinding...) }

func resolvedOverrides(target config.TargetConfig) (map[string][]string, error) {
	overrides, err := capability.LoadOverrideFile(target.CapabilityFile)
	if err != nil {
		return nil, err
	}
	if overrides == nil {
		overrides = map[string][]string{}
	}
	for model, values := range overrides {
		for _, value := range values {
			if !strings.HasPrefix(value, "anthropic.") {
				return nil, fmt.Errorf("Anthropic capability file assigns non-Anthropic capability %q to %q", value, model)
			}
		}
	}
	for _, raw := range target.ModelCapabilities {
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("invalid --anthropic-model-capability %q", raw)
		}
		values := strings.Split(parts[1], "+")
		for index := range values {
			values[index] = strings.TrimSpace(values[index])
			if !capability.Known[values[index]] || !strings.HasPrefix(values[index], "anthropic.") {
				return nil, fmt.Errorf("unknown or non-Anthropic capability %q", values[index])
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

// plannedProbeCount counts lifecycle probes, not result rows, HTTP attempts,
// or validators. Discovery and requested-model preflights have already ended
// when this immutable post-classification snapshot is published.
func plannedProbeCount(cfg config.Config, selected []string, modules [][]result.ModuleAssessment, routeBlocked []bool) int {
	if !cfg.HasTest("compat") {
		return 0
	}
	total := 0
	for index := range selected {
		if routeBlocked[index] {
			continue
		}
		// retrieve, basic message, ten direct message checks, three error
		// checks, and one streaming check.
		count := 16
		if !moduleExecuted(modules[index], "anthropic.messages") {
			count = 1 // retrieve is always performed
		}
		if moduleExecuted(modules[index], "anthropic.tools") {
			count += 6 // choices, specific, stream, parallel; continuation is conditional
		}
		if moduleExecuted(modules[index], "anthropic.token-counting") {
			count += 3
		}
		if moduleExecuted(modules[index], "anthropic.structured-output") || moduleExecuted(modules[index], "anthropic.vision") {
			count++
		}
		if moduleExecuted(modules[index], "anthropic.thinking") && cfg.AllowHighTokenProbes {
			count += 5
		}
		total += count
	}
	return total
}

func discoveryResults(p profile.Profile, resp api.Response, models []api.Model, err error) []result.TestResult {
	httpTest := testBase(p, "anthropic.models.list.http")
	httpTest.DurationMS, httpTest.HTTP = resp.Duration.Milliseconds(), observation(resp)
	setHTTPStatus(&httpTest, resp, err, false)
	if httpTest.Status == result.StatusPass {
		httpTest.Summary = "Anthropic Models endpoint returned JSON"
	}
	schema := testBase(p, "anthropic.models.list.schema")
	schema.DurationMS, schema.HTTP = httpTest.DurationMS, httpTest.HTTP
	if httpTest.Status != result.StatusPass {
		schema.Status = result.StatusSkip
		schema.Summary = "Model schema could not be evaluated"
		schema.Error = httpTest.Error
		schema.Signals = []result.EvidenceSignal{{
			Code: "blocked.anthropic.models.list.schema", RootCause: "blocked-prerequisite",
			Parameters: map[string]any{"blocked_by_requirement": "anthropic.models.list.http"},
		}}
	} else if problems := ValidateModels(resp.JSON); len(problems) > 0 {
		schema.Status = result.StatusFail
		schema.Summary = "Anthropic model-list schema is invalid"
		schema.Observation = strings.Join(problems, "; ")
		diagnostics := ModelSchemaDiagnostics(resp.JSON)
		parameters := make([]any, 0, len(diagnostics))
		for _, diagnostic := range diagnostics {
			parameters = append(parameters, map[string]any{"path": diagnostic.Path, "model_id": diagnostic.ModelID, "issue": diagnostic.Issue, "expected": diagnostic.Expected})
		}
		schema.Signals = []result.EvidenceSignal{{Code: "anthropic.models.list.invalid_envelope", Parameters: map[string]any{"diagnostics": parameters}}}
	} else {
		schema.Status = result.StatusPass
		schema.Summary = fmt.Sprintf("Anthropic model-list schema is valid (%d models)", len(models))
	}
	return []result.TestResult{httpTest, schema}
}

func paginationResult(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, first api.Response, models []api.Model, listErr error, telemetryProvider *telemetry.Provider) (test result.TestResult) {
	test = testBase(p, "anthropic.models.list.pagination")
	if listErr != nil || !first.Is2xx() || len(ValidateModels(first.JSON)) != 0 {
		test.Status = result.StatusSkip
		test.Summary = "Pagination could not be evaluated because model discovery failed"
		test.Signals = []result.EvidenceSignal{{
			Code: "blocked.anthropic.models.list.pagination", RootCause: "blocked-prerequisite",
			Parameters: map[string]any{"blocked_by_requirement": "anthropic.models.list.schema"},
		}}
		return test
	}
	hasMore, _ := first.JSON["has_more"].(bool)
	if !hasMore {
		test.Applicability = result.ApplicabilityNotApplicable
		test.Status = result.StatusSkip
		test.Summary = "The complete inventory fit in one page; no cursor request was applicable"
		return test
	}
	lastID := stringValue(first.JSON["last_id"])
	if lastID == "" {
		test.Status = result.StatusFail
		test.Summary = "has_more=true without a last_id cursor"
		return test
	}
	probeCtx := parent
	var suite, probeOp *telemetry.Operation
	if telemetryProvider != nil {
		var suiteCtx context.Context
		suiteCtx, suite = telemetryProvider.Start(logging.Scope(parent, "", "", "discovery", "", ""), "suite", logging.Fields{"suite": "discovery"})
		probeCtx, probeOp = telemetryProvider.Start(logging.Scope(suiteCtx, "", "", "discovery", "anthropic.models.list.pagination", ""), "probe", logging.Fields{"probe": "aiqa.native.anthropic.models.list.pagination"})
		defer func() {
			fields := logging.Fields{"result_id": test.ID, "status": string(test.Status), "http_status": test.HTTP.StatusCode, "duration_ms": test.DurationMS, "error_present": test.Error != ""}
			probeOp.Event(logging.Trace, "validator", fields)
			probeOp.End(string(test.Status), fields)
			suite.End(string(test.Status), logging.Fields{"status": string(test.Status)})
		}()
	}
	var next []api.Model
	var resp api.Response
	var err error
	ctx, cancel := api.WithRequestTimeout(probeCtx, timeout)
	next, resp, err = client.ListModelsPage(ctx, lastID, 1)
	cancel()
	test.DurationMS = resp.Duration.Milliseconds()
	test.HTTP = observation(resp)
	setHTTPStatus(&test, resp, err, true)
	if test.Status != result.StatusPass {
		test.Summary = "The next Anthropic Models page could not be retrieved"
		return test
	}
	if len(next) == 0 {
		test.Status = result.StatusFail
		test.Summary = "has_more=true but the next page was empty"
		return test
	}
	seen := map[string]bool{}
	for _, model := range models {
		seen[model.ID] = true
	}
	for _, model := range next {
		if seen[model.ID] {
			test.Status = result.StatusFail
			test.Summary = "The next Models page repeated an earlier model ID"
			return test
		}
	}
	test.Summary = "Cursor pagination returned a non-overlapping next page"
	return test
}

func runModel(parent context.Context, client *Client, cfg config.Config, p profile.Profile, model string, modules []result.ModuleAssessment, executor *probe.Executor) []result.TestResult {
	return runModelWithPreflight(parent, client, cfg, p, model, modules, executor, nil, nil, nil, nil)
}

func runModelWithPreflight(parent context.Context, client *Client, cfg config.Config, p profile.Profile, model string, modules []result.ModuleAssessment, executor *probe.Executor, telemetryProvider *telemetry.Provider, preflight *api.Response, preflightErr error, plan *progressPlan) []result.TestResult {
	timeout := cfg.Timeout
	var out []result.TestResult
	retrieve := probe.Map(executor, func() result.TestResult {
		return runNativeProbe(parent, telemetryProvider, "anthropic.models.retrieve", func(probeCtx context.Context) result.TestResult {
			retrieveResp, retrieveErr := call(probeCtx, timeout, func(ctx context.Context) (api.Response, error) { return client.RetrieveModel(ctx, model) })
			test := testBase(p, "anthropic.models.retrieve")
			test.DurationMS, test.HTTP = retrieveResp.Duration.Milliseconds(), observation(retrieveResp)
			setHTTPStatus(&test, retrieveResp, retrieveErr, true)
			if test.Status == result.StatusPass && stringValue(retrieveResp.JSON["id"]) != model {
				test.Status, test.Summary = result.StatusFail, "Retrieved model ID does not match the request"
			} else if test.Status == result.StatusPass {
				test.Summary = "Retrieved model ID matches the request"
			}
			return test
		})
	})[0]
	out = append(out, retrieve)
	if !moduleExecuted(modules, "anthropic.messages") {
		return append(out, flattenProbeGroups(probe.Map(executor, optionalModelTasks(parent, client, cfg, p, model, modules, 0, telemetryProvider, plan)...))...)
	}
	body := basicBody(model)
	var basicDerived []result.TestResult
	var observedInputTokens int64
	basicResults := runNativeProbeResults(parent, telemetryProvider, "anthropic.messages.basic", func(probeCtx context.Context) []result.TestResult {
		resp, err := api.Response{}, preflightErr
		if preflight != nil {
			resp = *preflight
		} else {
			resp, err = call(probeCtx, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, body) })
		}
		basic := testBase(p, "anthropic.messages.basic")
		basic.DurationMS = resp.Duration.Milliseconds()
		basic.HTTP = observation(resp)
		setHTTPStatus(&basic, resp, err, false)
		if basic.Status == result.StatusPass {
			if strings.Contains(Text(resp.JSON), "AIQA_OK") {
				basic.Summary = "Minimal Anthropic Message returned the requested marker"
			} else {
				basic.Status = result.StatusFail
				basic.Summary = "Message succeeded but did not return AIQA_OK"
				basic.Observation = truncate(Text(resp.JSON), 200)
			}
		}
		schema := derived(p, "anthropic.messages.schema", basic)
		if basic.Status == result.StatusPass {
			if problems := ValidateMessage(resp.JSON); len(problems) > 0 {
				schema.Status, schema.Summary, schema.Observation = result.StatusFail, "Anthropic Message schema is invalid", strings.Join(problems, "; ")
				schema.Signals = []result.EvidenceSignal{{Code: "anthropic.message.invalid_envelope"}}
			} else {
				schema.Status, schema.Summary = result.StatusPass, "Anthropic Message schema is valid"
			}
		}
		usage := derived(p, "anthropic.messages.usage", basic)
		usage.Usage = ParseUsage(resp.JSON)
		observedInputTokens = usage.Usage.InputTokens
		if basic.Status == result.StatusPass {
			if problems := ValidateUsage(usage.Usage); len(problems) > 0 {
				usage.Status, usage.Summary, usage.Observation = result.StatusFail, "Anthropic usage is invalid", strings.Join(problems, "; ")
			} else {
				usage.Status, usage.Summary = result.StatusPass, "Anthropic input/output usage is valid"
			}
		}
		accepted := derived(p, "anthropic.header.version.accepted", basic)
		accepted.Summary = "anthropic-version 2023-06-01 was accepted"
		if accepted.Status != result.StatusPass {
			accepted.Signals = []result.EvidenceSignal{{Code: "anthropic.header.version.rejected"}}
		}
		auth := derived(p, "anthropic.auth.x_api_key", basic)
		auth.Summary = "x-api-key authentication was accepted"
		if auth.Status != result.StatusPass {
			auth.Signals = []result.EvidenceSignal{{Code: "anthropic.auth.x_api_key.rejected"}}
		}
		stringContent := derived(p, "anthropic.messages.content.string", basic)
		stringContent.Summary = "String-form message content is accepted"
		basicDerived = []result.TestResult{schema, usage, accepted, auth, stringContent}
		return append([]result.TestResult{basic}, basicDerived...)
	})
	out = append(out, basicResults...)

	tasks := []func() []result.TestResult{
		func() []result.TestResult {
			test := runNativeProbe(parent, telemetryProvider, "anthropic.header.version.required", func(probeCtx context.Context) result.TestResult {
				return credentialProbe(probeCtx, client, timeout, p, "anthropic.header.version.required", body, client.apiKey, "")
			})
			if test.Status != result.StatusPass {
				test.Signals = []result.EvidenceSignal{{Code: "anthropic.header.version.missing"}}
			}
			return []result.TestResult{test}
		},
		func() []result.TestResult {
			return []result.TestResult{runNativeProbe(parent, telemetryProvider, "anthropic.header.version.invalid", func(probeCtx context.Context) result.TestResult {
				return credentialProbe(probeCtx, client, timeout, p, "anthropic.header.version.invalid", body, client.apiKey, "1900-01-01")
			})}
		},
		func() []result.TestResult {
			return []result.TestResult{runNativeProbe(parent, telemetryProvider, "anthropic.error.invalid_auth", func(probeCtx context.Context) result.TestResult {
				return credentialProbe(probeCtx, client, timeout, p, "anthropic.error.invalid_auth", body, "aiqa-invalid-key", StableAPIVersion)
			})}
		},
		func() []result.TestResult {
			blocksBody := basicBody(model)
			blocksBody["messages"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "Reply with exactly AIQA_OK."}}}}
			return []result.TestResult{runNativeProbe(parent, telemetryProvider, "anthropic.messages.content.text_blocks", func(probeCtx context.Context) result.TestResult {
				return simpleMarkerProbe(probeCtx, client, timeout, p, "anthropic.messages.content.text_blocks", blocksBody)
			})}
		},
		func() []result.TestResult {
			systemBody := basicBody(model)
			systemBody["system"] = "Always answer with exactly AIQA_OK."
			return []result.TestResult{runNativeProbe(parent, telemetryProvider, "anthropic.messages.system.top_level", func(probeCtx context.Context) result.TestResult {
				return simpleMarkerProbe(probeCtx, client, timeout, p, "anthropic.messages.system.top_level", systemBody)
			})}
		},
		func() []result.TestResult {
			multiBody := basicBody(model)
			multiBody["messages"] = []any{map[string]any{"role": "user", "content": "Remember AIQA_OK."}, map[string]any{"role": "assistant", "content": "I will remember it."}, map[string]any{"role": "user", "content": "Reply with only the remembered marker."}}
			return []result.TestResult{runNativeProbe(parent, telemetryProvider, "anthropic.messages.multi_turn", func(probeCtx context.Context) result.TestResult {
				return simpleMarkerProbe(probeCtx, client, timeout, p, "anthropic.messages.multi_turn", multiBody)
			})}
		},
		func() []result.TestResult {
			return []result.TestResult{runNativeProbe(parent, telemetryProvider, "anthropic.messages.assistant_prefill", func(probeCtx context.Context) result.TestResult {
				return runAssistantPrefill(probeCtx, client, timeout, p, model)
			})}
		},
		func() []result.TestResult {
			return []result.TestResult{runNativeProbe(parent, telemetryProvider, "anthropic.messages.stop_sequences", func(probeCtx context.Context) result.TestResult {
				return runStopSequence(probeCtx, client, timeout, p, model)
			})}
		},
		func() []result.TestResult {
			return []result.TestResult{runNativeProbe(parent, telemetryProvider, "anthropic.messages.max_tokens", func(probeCtx context.Context) result.TestResult {
				return runMaxTokens(probeCtx, client, timeout, p, model)
			})}
		},
		func() []result.TestResult {
			metadataBody := basicBody(model)
			metadataBody["metadata"] = map[string]any{"user_id": "aiqa-compat-probe"}
			return []result.TestResult{runNativeProbe(parent, telemetryProvider, "anthropic.messages.metadata", func(probeCtx context.Context) result.TestResult {
				return simpleMarkerProbe(probeCtx, client, timeout, p, "anthropic.messages.metadata", metadataBody)
			})}
		},
		func() []result.TestResult {
			return runErrors(parent, client, timeout, p, model, telemetryProvider)
		},
		func() []result.TestResult {
			return runStream(parent, client, timeout, p, model, telemetryProvider)
		},
	}
	tasks = append(tasks, optionalModelTasks(parent, client, cfg, p, model, modules, observedInputTokens, telemetryProvider, plan)...)
	return append(out, flattenProbeGroups(probe.Map(executor, tasks...))...)
}

func optionalModelTasks(parent context.Context, client *Client, cfg config.Config, p profile.Profile, model string, modules []result.ModuleAssessment, observedInputTokens int64, telemetryProvider *telemetry.Provider, plan *progressPlan) []func() []result.TestResult {
	var tasks []func() []result.TestResult
	if moduleExecuted(modules, "anthropic.tools") {
		tasks = append(tasks, func() []result.TestResult {
			return runTools(parent, client, cfg.Timeout, p, model, telemetryProvider, plan)
		})
	}
	if moduleExecuted(modules, "anthropic.token-counting") {
		tasks = append(tasks, func() []result.TestResult {
			return runCountTokens(parent, client, cfg.Timeout, p, model, observedInputTokens, telemetryProvider)
		})
	}
	if moduleExecuted(modules, "anthropic.structured-output") {
		tasks = append(tasks, func() []result.TestResult {
			return runStructured(parent, client, cfg.Timeout, p, model, telemetryProvider)
		})
	}
	if moduleExecuted(modules, "anthropic.vision") {
		tasks = append(tasks, func() []result.TestResult {
			return runVision(parent, client, cfg.Timeout, p, model, telemetryProvider)
		})
	}
	if moduleExecuted(modules, "anthropic.thinking") && cfg.AllowHighTokenProbes {
		tasks = append(tasks, func() []result.TestResult {
			return runThinking(parent, client, cfg.Timeout, p, model, telemetryProvider)
		})
	}
	return tasks
}

func flattenProbeGroups(groups [][]result.TestResult) []result.TestResult {
	var out []result.TestResult
	for _, group := range groups {
		out = append(out, group...)
	}
	return out
}

// runNativeProbe establishes the concrete probe scope before any request is
// constructed. The transport inherits this context, so logical_request and
// attempt spans are children of the test that caused them rather than a
// post-hoc result record. Validation runs inside fn and therefore completes
// before the probe and suite terminals.
func runNativeProbe(parent context.Context, provider *telemetry.Provider, id string, fn func(context.Context) result.TestResult) result.TestResult {
	if provider == nil {
		return fn(parent)
	}
	suiteCtx, suite := provider.Start(logging.Scope(parent, "", "", "compat", "", ""), "suite", logging.Fields{"suite": "compat"})
	probeCtx, probeOp := provider.Start(logging.Scope(suiteCtx, "", "", "compat", "aiqa.native."+id, ""), "probe", logging.Fields{"probe": "aiqa.native." + id})
	test := fn(probeCtx)
	fields := logging.Fields{"status": string(test.Status), "http_status": test.HTTP.StatusCode, "duration_ms": test.DurationMS, "error_present": test.Error != ""}
	probeOp.Event(logging.Trace, "validator", fields)
	probeOp.End(string(test.Status), fields)
	suite.End(string(test.Status), logging.Fields{"status": string(test.Status)})
	return test
}

// runNativeProbeResults keeps a composite native probe alive through every
// derived report result produced from the same HTTP observation.
func runNativeProbeResults(parent context.Context, provider *telemetry.Provider, id string, fn func(context.Context) []result.TestResult) []result.TestResult {
	if provider == nil {
		return fn(parent)
	}
	suiteCtx, suite := provider.Start(logging.Scope(parent, "", "", "compat", "", ""), "suite", logging.Fields{"suite": "compat"})
	probeCtx, probeOp := provider.Start(logging.Scope(suiteCtx, "", "", "compat", "aiqa.native."+id, ""), "probe", logging.Fields{"probe": "aiqa.native." + id})
	tests := fn(probeCtx)
	outcome := protocolpkg.AggregateTestOutcome(tests)
	for _, test := range tests {
		fields := logging.Fields{"result_id": test.ID, "status": string(test.Status), "http_status": test.HTTP.StatusCode, "duration_ms": test.DurationMS, "error_present": test.Error != ""}
		if test.DerivedFrom != "" {
			fields["derived_from"] = test.DerivedFrom
		}
		probeOp.Event(logging.Trace, "validator", fields)
	}
	probeOp.End(outcome, logging.Fields{"status": outcome, "result_count": len(tests)})
	suite.End(outcome, logging.Fields{"status": outcome})
	return tests
}

func runAssistantPrefill(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) result.TestResult {
	body := basicBody(model)
	body["messages"] = []any{map[string]any{"role": "user", "content": "Complete the marker AIQA_OK."}, map[string]any{"role": "assistant", "content": "AIQA_"}}
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, body) })
	test := testBase(p, "anthropic.messages.assistant_prefill")
	test.DurationMS, test.HTTP = resp.Duration.Milliseconds(), observation(resp)
	test.Usage = ParseUsage(resp.JSON)
	setHTTPStatus(&test, resp, err, true)
	if test.Status == result.StatusPass && strings.TrimSpace(Text(resp.JSON)) == "" {
		test.Status = result.StatusFail
		test.Summary = "Assistant prefill returned no continuation"
	} else if test.Status == result.StatusPass {
		test.Summary = "Assistant prefill returned a non-empty continuation"
	}
	return test
}

func runStopSequence(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) result.TestResult {
	marker := "AIQA_STOP_7f3a"
	body := basicBody(model)
	body["stop_sequences"] = []any{marker}
	body["messages"] = []any{map[string]any{"role": "user", "content": "Reply with AIQA_OK, then " + marker + ", then ignored text."}}
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, body) })
	test := testBase(p, "anthropic.messages.stop_sequences")
	test.DurationMS, test.HTTP = resp.Duration.Milliseconds(), observation(resp)
	test.Usage = ParseUsage(resp.JSON)
	setHTTPStatus(&test, resp, err, true)
	if test.Status == result.StatusPass {
		if stringValue(resp.JSON["stop_reason"]) != "stop_sequence" || stringValue(resp.JSON["stop_sequence"]) != marker {
			test.Status = result.StatusWarn
			test.Summary = "Stop sequence was accepted but matching stop fields were not demonstrated"
		} else {
			test.Summary = "Unique stop sequence produced matching stop fields"
		}
	}
	return test
}

func runMaxTokens(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) result.TestResult {
	body := basicBody(model)
	body["max_tokens"] = 1
	body["messages"] = []any{map[string]any{"role": "user", "content": "Write a long explanation of AIQA compatibility."}}
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, body) })
	test := testBase(p, "anthropic.messages.max_tokens")
	test.DurationMS, test.HTTP = resp.Duration.Milliseconds(), observation(resp)
	test.Usage = ParseUsage(resp.JSON)
	setHTTPStatus(&test, resp, err, true)
	if test.Status == result.StatusPass {
		reason := stringValue(resp.JSON["stop_reason"])
		if reason == "" {
			test.Status = result.StatusFail
			test.Summary = "Small max_tokens response omitted stop_reason"
		} else {
			test.Summary = "Small max_tokens request returned compatible stop semantics (" + reason + ")"
		}
	}
	return test
}

func runErrors(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string, telemetryProvider *telemetry.Provider) []result.TestResult {
	missing := "aiqa-missing-" + fmt.Sprint(time.Now().UnixNano())
	invalidModelResults := runNativeProbeResults(parent, telemetryProvider, "anthropic.error.invalid_model", func(probeCtx context.Context) []result.TestResult {
		resp, err := call(probeCtx, timeout, func(ctx context.Context) (api.Response, error) {
			b := basicBody(missing)
			return client.Message(ctx, b)
		})
		test := testBase(p, "anthropic.error.invalid_model")
		test.DurationMS, test.HTTP, test.Usage = resp.Duration.Milliseconds(), observation(resp), ParseUsage(resp.JSON)
		setExpectedError(&test, resp, err)
		if test.Status == result.StatusPass {
			test.Summary = "Invalid model returned a client-side Anthropic error"
		}
		shape := derived(p, "anthropic.error.shape", test)
		if test.Status == result.StatusPass {
			if problems := ValidateError(resp.JSON); len(problems) > 0 {
				shape.Status = result.StatusFail
				shape.Summary = "Error envelope is not Anthropic-compatible"
				shape.Observation = strings.Join(problems, "; ")
			} else {
				shape.Status = result.StatusPass
				shape.Summary = "Error envelope is Anthropic-compatible"
			}
		}
		requestID := derived(p, "anthropic.error.request_id", test)
		if test.Status == result.StatusPass {
			if resp.RequestID != "" || stringValue(resp.JSON["request_id"]) != "" {
				requestID.Status = result.StatusPass
				requestID.Summary = "Error exposes a request ID"
			} else {
				requestID.Status = result.StatusWarn
				requestID.Summary = "Error did not expose a request ID"
			}
		}
		return []result.TestResult{test, shape, requestID}
	})
	variants := [][]byte{[]byte(`{"model":`), []byte(`{"messages":[}`), []byte(`{"model":"` + model + `"} trailing`)}
	invalidJSON := runNativeProbe(parent, telemetryProvider, "anthropic.error.invalid_json", func(probeCtx context.Context) result.TestResult {
		test := testBase(p, "anthropic.error.invalid_json")
		test.Status = result.StatusPass
		var observations []string
		for i, raw := range variants {
			r, e := call(probeCtx, timeout, func(ctx context.Context) (api.Response, error) { return client.MessageRawForModel(ctx, raw, model) })
			test.DurationMS += r.Duration.Milliseconds()
			test.Usage = test.Usage.Add(ParseUsage(r.JSON))
			if i == 0 {
				test.HTTP = observation(r)
			}
			if e != nil && r.StatusCode == 0 {
				test.Status = result.StatusError
				observations = append(observations, e.Error())
				continue
			}
			if isTransientStatus(r.StatusCode) {
				test.Status = result.StatusError
			} else if r.StatusCode < 400 {
				test.Status = result.StatusFail
			}
			observations = append(observations, fmt.Sprintf("variant %d: HTTP %d", i+1, r.StatusCode))
		}
		test.Summary = "Three malformed JSON variants returned client errors"
		if test.Status != result.StatusPass {
			test.Summary = "Malformed JSON handling is not consistently client-side"
		}
		test.Observation = strings.Join(observations, "; ")
		return test
	})
	wrong := basicBody(model)
	wrong["max_tokens"] = "32"
	invalidTypes := runNativeProbe(parent, telemetryProvider, "anthropic.error.invalid_types", func(probeCtx context.Context) result.TestResult {
		r, e := call(probeCtx, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, wrong) })
		test := testBase(p, "anthropic.error.invalid_types")
		test.DurationMS, test.HTTP, test.Usage = r.Duration.Milliseconds(), observation(r), ParseUsage(r.JSON)
		setExpectedError(&test, r, e)
		test.Summary = "Invalid field types are rejected with a client error"
		return test
	})
	return append(invalidModelResults, invalidJSON, invalidTypes)
}

func runStream(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string, telemetryProvider *telemetry.Provider) []result.TestResult {
	return runNativeProbeResults(parent, telemetryProvider, "anthropic.messages.stream.http", func(probeCtx context.Context) []result.TestResult {
		ctx, cancel := api.WithRequestTimeout(probeCtx, timeout)
		defer cancel()
		stream, err := client.StreamMessage(ctx, basicBody(model))
		base := streamHTTPTest(p, stream, err)
		// Validate the received SSE structure while its concrete HTTP probe is
		// still active, so validator lifecycle cannot outlive the request span.
		return streamResults(p, base, stream, ValidateStream(stream))
	})
}

func streamHTTPTest(p profile.Profile, stream api.StreamResponse, err error) result.TestResult {
	base := testBase(p, "anthropic.messages.stream.http")
	base.DurationMS = stream.Duration.Milliseconds()
	base.HTTP = result.HTTPObservation{StatusCode: stream.StatusCode, ContentType: stream.ContentType, RequestID: stream.RequestID, Snippet: truncate(stream.RawSnippet, 1000), TraceID: stream.TraceID}
	base.Metrics = map[string]any{"sse_event_types": streamEventTypes(stream)}
	if err != nil {
		if stream.StatusCode == 0 || stream.StatusCode >= 200 && stream.StatusCode < 300 || isTransientStatus(stream.StatusCode) {
			base.Status = result.StatusError
		} else {
			base.Status = result.StatusFail
		}
		base.Summary = "Anthropic stream could not be evaluated"
		base.Error = err.Error()
	} else if stream.StatusCode < 200 || stream.StatusCode >= 300 {
		base.Status = result.StatusFail
		base.Summary = "Anthropic stream returned non-success status"
	} else if !strings.Contains(strings.ToLower(stream.ContentType), "text/event-stream") {
		base.Status = result.StatusFail
		base.Summary = "Streaming response is not text/event-stream"
	} else {
		base.Status = result.StatusPass
		base.Summary = "Streaming response uses SSE"
	}
	return base
}

func streamResults(p profile.Profile, base result.TestResult, stream api.StreamResponse, validation StreamValidation) []result.TestResult {
	sequence := derived(p, "anthropic.messages.stream.sequence", base)
	sequence.Metrics = map[string]any{"sse_event_types": streamEventTypes(stream)}
	if base.Status == result.StatusPass {
		if validation.ProviderError {
			sequence.Status = result.StatusError
			sequence.Summary = "Provider emitted an error event during the stream"
		} else if len(validation.Problems) > 0 {
			sequence.Status = result.StatusFail
			sequence.Summary = "Anthropic event sequence is invalid"
			sequence.Observation = strings.Join(validation.Problems, "; ")
			sequence.Signals = []result.EvidenceSignal{{Code: "anthropic.stream.sequence_invalid"}}
		} else if validation.Incomplete {
			sequence.Status = result.StatusError
			sequence.Summary = "Anthropic stream ended before message_stop"
		} else {
			sequence.Status = result.StatusPass
			sequence.Summary = "Anthropic named-event sequence is valid"
		}
	}
	typeMatch := derived(p, "anthropic.messages.stream.event_type_match", sequence)
	typeMatch.Summary = "Known SSE event names match JSON event types"
	textProbe := derived(p, "anthropic.messages.stream.text", sequence)
	if sequence.Status == result.StatusPass && !strings.Contains(validation.Text, "AIQA_OK") {
		textProbe.Status = result.StatusFail
		textProbe.Summary = "Stream did not reconstruct AIQA_OK"
	} else {
		textProbe.Summary = "Streamed text reconstructed the requested marker"
	}
	usage := derived(p, "anthropic.messages.stream.usage", sequence)
	usage.Usage = validation.Usage
	if sequence.Status == result.StatusPass && validation.Usage.OutputTokens < 0 {
		usage.Status = result.StatusFail
	}
	usage.Summary = "Streaming usage was reconstructed"
	noDone := derived(p, "anthropic.messages.stream.no_done_sentinel", sequence)
	if sequence.Status == result.StatusPass {
		if validation.SawDoneSentinel {
			noDone.Status = result.StatusFail
			noDone.Summary = "Stream emitted the OpenAI [DONE] sentinel"
		} else {
			noDone.Summary = "Stream ended with message_stop and no [DONE] sentinel"
		}
	}
	return []result.TestResult{base, sequence, typeMatch, textProbe, usage, noDone}
}

func runTools(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string, telemetryProvider *telemetry.Provider, plan *progressPlan) []result.TestResult {
	tool := map[string]any{"name": "get_status", "description": "Return status for a resource.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"resource_id": map[string]any{"type": "string"}}, "required": []any{"resource_id"}, "additionalProperties": false}}
	auto := runToolChoiceProbe(parent, client, timeout, p, model, "anthropic.tools.auto", tool, map[string]any{"type": "auto"}, false, telemetryProvider)
	anyProbe := runToolChoiceProbe(parent, client, timeout, p, model, "anthropic.tools.any", tool, map[string]any{"type": "any"}, true, telemetryProvider)
	none := runToolChoiceProbe(parent, client, timeout, p, model, "anthropic.tools.none", tool, map[string]any{"type": "none"}, false, telemetryProvider)
	if none.Status == result.StatusPass && none.Metrics["tool_use_count"].(int) != 0 {
		none.Status = result.StatusFail
		none.Summary = "tool_choice=none still emitted a tool_use block"
	} else if none.Status == result.StatusPass {
		none.Summary = "tool_choice=none suppressed tool use"
	}
	body := basicBody(model)
	body["tools"] = []any{tool}
	body["tool_choice"] = map[string]any{"type": "tool", "name": "get_status"}
	body["messages"] = []any{map[string]any{"role": "user", "content": "Call get_status for resource-alpha. Do not answer directly."}}
	var resp api.Response
	specificResults := runNativeProbeResults(parent, telemetryProvider, "anthropic.tools.specific", func(probeCtx context.Context) []result.TestResult {
		var err error
		resp, err = call(probeCtx, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, body) })
		test := testBase(p, "anthropic.tools.specific")
		test.DurationMS, test.HTTP, test.Usage = resp.Duration.Milliseconds(), observation(resp), ParseUsage(resp.JSON)
		setHTTPStatus(&test, resp, err, true)
		uses := ToolUses(resp.JSON)
		if test.Status == result.StatusPass {
			if len(uses) == 0 || stringValue(uses[0]["name"]) != "get_status" {
				test.Status, test.Summary = result.StatusFail, "Forced tool did not return get_status"
			} else {
				test.Summary = "Forced named tool returned a valid tool_use block"
			}
		}
		inputSchema := derived(p, "anthropic.tools.input_schema", test)
		if test.Status == result.StatusPass && len(uses) > 0 {
			input, _ := uses[0]["input"].(map[string]any)
			if stringValue(input["resource_id"]) == "" {
				inputSchema.Status = result.StatusFail
				inputSchema.Summary = "Tool input violates its required schema"
			} else {
				inputSchema.Summary = "Tool input conforms to the requested schema"
			}
		}
		return []result.TestResult{test, inputSchema}
	})
	specific := specificResults[0]
	uses := ToolUses(resp.JSON)
	continuation := testBase(p, "anthropic.tools.continuation")
	if specific.Status != result.StatusPass || len(uses) == 0 {
		continuation.Status = result.StatusSkip
		continuation.Summary = "Continuation blocked because no tool_use was returned"
		continuation.Signals = []result.EvidenceSignal{{Code: "anthropic.tools.result_roundtrip_failed", Parameters: map[string]any{"blocked_by_requirement": "anthropic.tools.specific"}}}
	} else {
		plan.addDynamicProbe()
		id := stringValue(uses[0]["id"])
		cont := basicBody(model)
		cont["messages"] = []any{body["messages"].([]any)[0], map[string]any{"role": "assistant", "content": resp.JSON["content"]}, map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": "resource-alpha is available"}}}}
		continuation = runNativeProbe(parent, telemetryProvider, "anthropic.tools.continuation", func(probeCtx context.Context) result.TestResult {
			r, e := call(probeCtx, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, cont) })
			test := testBase(p, "anthropic.tools.continuation")
			test.DurationMS, test.HTTP, test.Usage = r.Duration.Milliseconds(), observation(r), ParseUsage(r.JSON)
			setHTTPStatus(&test, r, e, true)
			if test.Status == result.StatusPass && strings.Contains(strings.ToLower(Text(r.JSON)), "available") {
				test.Summary = "tool_result was accepted and used in the final answer"
			} else if test.Status == result.StatusPass {
				test.Status, test.Summary = result.StatusWarn, "tool_result was accepted but semantic use was not demonstrated"
			}
			return test
		})
	}
	var stream api.StreamResponse
	var streamErr error
	streamProbe := runNativeProbe(parent, telemetryProvider, "anthropic.tools.stream", func(probeCtx context.Context) result.TestResult {
		ctx, cancel := api.WithRequestTimeout(probeCtx, timeout)
		defer cancel()
		stream, streamErr = client.StreamMessage(ctx, body)
		test := testBase(p, "anthropic.tools.stream")
		test.DurationMS = stream.Duration.Milliseconds()
		test.HTTP = result.HTTPObservation{StatusCode: stream.StatusCode, ContentType: stream.ContentType, RequestID: stream.RequestID, Snippet: truncate(stream.RawSnippet, 1000), TraceID: stream.TraceID}
		validation := ValidateStream(stream)
		test.Usage = validation.Usage
		test.Metrics = map[string]any{"sse_event_types": streamEventTypes(stream)}
		if streamErr != nil {
			if stream.StatusCode == 0 || stream.StatusCode >= 200 && stream.StatusCode < 300 || isTransientStatus(stream.StatusCode) {
				test.Status = result.StatusError
			} else if stream.StatusCode == 400 || stream.StatusCode == 404 || stream.StatusCode == 422 {
				test.Status = result.StatusUnsupported
			} else {
				test.Status = result.StatusFail
			}
			test.Summary, test.Error = "Streaming tool call could not be evaluated", streamErr.Error()
		} else if validation.ProviderError {
			test.Status, test.Summary = result.StatusError, "Provider emitted a stream error during tool use"
		} else if len(validation.Problems) > 0 {
			test.Status, test.Summary, test.Observation = result.StatusFail, "Streaming tool input is invalid", strings.Join(validation.Problems, "; ")
		} else if validation.Incomplete {
			test.Status, test.Summary = result.StatusError, "Streaming tool call ended before message_stop"
		} else if len(validation.ToolInputs) == 0 {
			test.Status, test.Summary = result.StatusFail, "Stream contained no tool input deltas"
		} else {
			test.Status, test.Summary = result.StatusPass, "Streamed tool input reconstructed as valid JSON"
		}
		return test
	})
	parallelTool := map[string]any{"name": "get_owner", "description": "Return an owner.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"resource_id": map[string]any{"type": "string"}}, "required": []any{"resource_id"}, "additionalProperties": false}}
	parallelBody := basicBody(model)
	parallelBody["tools"] = []any{tool, parallelTool}
	parallelBody["tool_choice"] = map[string]any{"type": "any", "disable_parallel_tool_use": false}
	parallelBody["messages"] = []any{map[string]any{"role": "user", "content": "Call get_status and get_owner for resource-alpha before answering."}}
	parallel := runNativeProbe(parent, telemetryProvider, "anthropic.tools.parallel", func(probeCtx context.Context) result.TestResult {
		parallelResp, parallelErr := call(probeCtx, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, parallelBody) })
		test := testBase(p, "anthropic.tools.parallel")
		test.DurationMS, test.HTTP, test.Usage = parallelResp.Duration.Milliseconds(), observation(parallelResp), ParseUsage(parallelResp.JSON)
		setHTTPStatus(&test, parallelResp, parallelErr, true)
		parallelUses := ToolUses(parallelResp.JSON)
		if test.Status == result.StatusPass {
			ids := map[string]bool{}
			for _, use := range parallelUses {
				if id := stringValue(use["id"]); id != "" {
					ids[id] = true
				}
			}
			if len(parallelUses) < 2 || len(ids) != len(parallelUses) {
				test.Status, test.Summary = result.StatusWarn, "Parallel tool use was accepted but two distinct calls were not demonstrated"
			} else {
				test.Summary = "Parallel tool use returned distinct tool_use IDs"
			}
		}
		return test
	})

	strictTool := map[string]any{"name": "get_status", "description": "Return status for a resource.", "strict": true, "input_schema": tool["input_schema"]}
	strict := runToolChoiceProbe(parent, client, timeout, p, model, "anthropic.tools.strict", strictTool, map[string]any{"type": "tool", "name": "get_status"}, true, telemetryProvider)
	if strict.Status == result.StatusPass {
		strict.Summary = "Strict tool choice returned schema-conforming input"
	}
	return append([]result.TestResult{auto, anyProbe, none}, append(specificResults, continuation, parallel, streamProbe, strict)...)
}

func runToolChoiceProbe(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model, id string, tool, choice map[string]any, requireUse bool, telemetryProvider *telemetry.Provider) result.TestResult {
	body := basicBody(model)
	body["tools"] = []any{tool}
	body["tool_choice"] = choice
	body["messages"] = []any{map[string]any{"role": "user", "content": "Call get_status for resource-alpha before answering."}}
	return runNativeProbe(parent, telemetryProvider, id, func(probeCtx context.Context) result.TestResult {
		resp, err := call(probeCtx, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, body) })
		test := testBase(p, id)
		test.DurationMS, test.HTTP = resp.Duration.Milliseconds(), observation(resp)
		test.Usage = ParseUsage(resp.JSON)
		setHTTPStatus(&test, resp, err, true)
		uses := ToolUses(resp.JSON)
		test.Metrics = map[string]any{"tool_use_count": len(uses)}
		if test.Status != result.StatusPass {
			return test
		}
		if requireUse && len(uses) == 0 {
			test.Status = result.StatusFail
			test.Summary = p.Probe(id).Name + " did not emit tool_use"
			return test
		}
		if len(uses) > 0 {
			input, _ := uses[0]["input"].(map[string]any)
			if stringValue(uses[0]["id"]) == "" || stringValue(uses[0]["name"]) != "get_status" || stringValue(input["resource_id"]) == "" {
				test.Status = result.StatusFail
				test.Summary = p.Probe(id).Name + " returned an invalid tool_use block"
				return test
			}
			test.Summary = p.Probe(id).Name + " returned a valid tool_use block"
		} else {
			test.Summary = p.Probe(id).Name + " was accepted without selecting a tool"
		}
		return test
	})
}

func runCountTokens(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string, observed int64, telemetryProvider *telemetry.Provider) []result.TestResult {
	body := basicBody(model)
	delete(body, "max_tokens")
	basic, count := countTokensProbe(parent, client, timeout, p, "anthropic.tokens.count.basic", body, telemetryProvider)
	if basic.Status == result.StatusUnsupported {
		basic.Signals = []result.EvidenceSignal{{Code: "anthropic.tokens.endpoint_missing"}}
	}
	schema := derived(p, "anthropic.tokens.count.schema", basic)
	schema.Summary = "Count Tokens response has a positive integer input_tokens"
	systemBody := cloneMap(body)
	systemBody["system"] = "Always answer tersely."
	systemProbe, systemCount := countTokensProbe(parent, client, timeout, p, "anthropic.tokens.count.system", systemBody, telemetryProvider)
	if systemProbe.Status == result.StatusPass && systemCount <= count {
		systemProbe.Status = result.StatusWarn
		systemProbe.Summary = "System content was accepted but did not increase the reported count"
	}
	toolsBody := cloneMap(body)
	toolsBody["tools"] = []any{map[string]any{"name": "get_status", "description": "Return status.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"resource_id": map[string]any{"type": "string"}}, "required": []any{"resource_id"}}}}
	toolsProbe, toolsCount := countTokensProbe(parent, client, timeout, p, "anthropic.tokens.count.tools", toolsBody, telemetryProvider)
	if toolsProbe.Status == result.StatusPass && toolsCount <= count {
		toolsProbe.Status = result.StatusWarn
		toolsProbe.Summary = "Tool schema was accepted but did not increase the reported count"
	}
	consistency := derived(p, "anthropic.tokens.count.consistency", basic)
	if basic.Status == result.StatusPass && observed > 0 {
		delta := count - observed
		if delta < 0 {
			delta = -delta
		}
		consistency.Metrics = map[string]any{"count_tokens_input": count, "message_usage_input": observed, "absolute_delta": delta, "aiqa_heuristic": "max(16,10%)"}
		threshold := int64(16)
		if ten := max(count, observed) / 10; ten > threshold {
			threshold = ten
		}
		if delta > threshold {
			consistency.Status = result.StatusWarn
			consistency.Summary = "Count Tokens differs materially from Message usage"
		} else {
			consistency.Summary = "Count Tokens is consistent with Message usage"
		}
	}
	return []result.TestResult{basic, schema, systemProbe, toolsProbe, consistency}
}

func countTokensProbe(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, id string, body map[string]any, telemetryProvider *telemetry.Provider) (result.TestResult, int64) {
	var count int64
	test := runNativeProbe(parent, telemetryProvider, id, func(probeCtx context.Context) result.TestResult {
		resp, err := call(probeCtx, timeout, func(ctx context.Context) (api.Response, error) { return client.CountTokens(ctx, body) })
		test := testBase(p, id)
		test.DurationMS, test.HTTP = resp.Duration.Milliseconds(), observation(resp)
		setHTTPStatus(&test, resp, err, true)
		count = int64Value(resp.JSON["input_tokens"])
		if test.Status == result.StatusPass && count <= 0 {
			test.Status, test.Summary = result.StatusFail, "Count Tokens returned no positive input_tokens"
		} else if test.Status == result.StatusPass {
			test.Summary = fmt.Sprintf("Count Tokens returned %d input tokens", count)
		}
		return test
	})
	return test, count
}

func runStructured(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string, telemetryProvider *telemetry.Provider) []result.TestResult {
	body := basicBody(model)
	body["messages"] = []any{map[string]any{"role": "user", "content": "Return JSON with ok=true and value=AIQA_OK."}}
	body["output_config"] = map[string]any{"format": map[string]any{"type": "json_schema", "schema": map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}, "value": map[string]any{"type": "string"}}, "required": []any{"ok", "value"}, "additionalProperties": false}}}
	structuredResults := runNativeProbeResults(parent, telemetryProvider, "anthropic.structured.json_schema", func(probeCtx context.Context) []result.TestResult {
		resp, err := call(probeCtx, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, body) })
		test := testBase(p, "anthropic.structured.json_schema")
		test.DurationMS, test.HTTP, test.Usage = resp.Duration.Milliseconds(), observation(resp), ParseUsage(resp.JSON)
		setHTTPStatus(&test, resp, err, true)
		if test.Status == result.StatusPass {
			if !validStructuredOutput(Text(resp.JSON)) {
				test.Status, test.Summary, test.Signals = result.StatusFail, "Structured output is not schema-conforming JSON", []result.EvidenceSignal{{Code: "anthropic.structured.schema_violation"}}
			} else {
				test.Summary = "Structured output conforms to the requested JSON schema"
			}
		}
		stop := derived(p, "anthropic.structured.stop_reason", test)
		reason := stringValue(resp.JSON["stop_reason"])
		if test.Status == result.StatusPass && (reason == "max_tokens" || reason == "refusal") {
			stop.Status = result.StatusWarn
			stop.Summary = "Structured output ended with " + reason
		} else {
			stop.Summary = "Structured output completed with compatible stop semantics"
		}
		return []result.TestResult{test, stop}
	})
	strictTool := map[string]any{"name": "get_status", "description": "Return status.", "strict": true, "input_schema": map[string]any{"type": "object", "properties": map[string]any{"resource_id": map[string]any{"type": "string"}}, "required": []any{"resource_id"}, "additionalProperties": false}}
	strict := runToolChoiceProbe(parent, client, timeout, p, model, "anthropic.tools.strict_schema", strictTool, map[string]any{"type": "tool", "name": "get_status"}, true, telemetryProvider)
	if strict.Status == result.StatusPass {
		strict.Summary = "Strict tool input conforms to the local schema"
	}
	return append(structuredResults, strict)
}

func validStructuredOutput(text string) bool {
	var value map[string]any
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil || len(value) != 2 || value["ok"] != true {
		return false
	}
	_, ok := value["value"].(string)
	return ok
}

func runThinking(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string, telemetryProvider *telemetry.Provider) []result.TestResult {
	adaptiveBody := basicBody(model)
	adaptiveBody["max_tokens"] = 2048
	adaptiveBody["thinking"] = map[string]any{"type": "adaptive"}
	adaptive := thinkingMessageProbe(parent, client, timeout, p, "anthropic.thinking.adaptive", adaptiveBody, "adaptive", "Adaptive thinking returned a compatible message; thinking content was not retained", telemetryProvider)
	effortBody := cloneMap(adaptiveBody)
	effortBody["output_config"] = map[string]any{"effort": "high"}
	effort := thinkingMessageProbe(parent, client, timeout, p, "anthropic.thinking.effort", effortBody, "effort", "High thinking effort was accepted", telemetryProvider)
	manualBody := basicBody(model)
	manualBody["max_tokens"] = 2048
	manualBody["thinking"] = map[string]any{"type": "enabled", "budget_tokens": 1024}
	manual := thinkingMessageProbe(parent, client, timeout, p, "anthropic.thinking.manual", manualBody, "manual", "Manual thinking budget was accepted; thinking content was not retained", telemetryProvider)
	invalidBody := basicBody(model)
	invalidBody["thinking"] = map[string]any{"type": "enabled", "budget_tokens": 1}
	validation := runNativeProbe(parent, telemetryProvider, "anthropic.thinking.manual.validation", func(probeCtx context.Context) result.TestResult {
		invalidResp, invalidErr := call(probeCtx, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, invalidBody) })
		test := testBase(p, "anthropic.thinking.manual.validation")
		test.DurationMS, test.HTTP = invalidResp.Duration.Milliseconds(), metadataOnlyObservation(invalidResp)
		setExpectedError(&test, invalidResp, invalidErr)
		if test.Status == result.StatusPass {
			test.Summary = "Invalid manual thinking budget was rejected"
		}
		return test
	})
	streamProbe := runNativeProbe(parent, telemetryProvider, "anthropic.thinking.stream", func(probeCtx context.Context) result.TestResult {
		ctx, cancel := api.WithRequestTimeout(probeCtx, timeout)
		defer cancel()
		stream, streamErr := client.StreamMessage(ctx, manualBody)
		test := testBase(p, "anthropic.thinking.stream")
		test.DurationMS = stream.Duration.Milliseconds()
		test.HTTP = result.HTTPObservation{StatusCode: stream.StatusCode, ContentType: stream.ContentType, RequestID: stream.RequestID, TraceID: stream.TraceID}
		streamValidation := ValidateStream(stream)
		test.Usage = streamValidation.Usage
		if streamErr != nil {
			test.Status, test.Summary, test.Error = result.StatusError, "Thinking stream could not be evaluated", streamErr.Error()
		} else if streamValidation.ProviderError {
			test.Status, test.Summary = result.StatusError, "Provider emitted an error during thinking stream"
		} else if len(streamValidation.Problems) > 0 {
			test.Status, test.Summary, test.Observation = result.StatusFail, "Thinking stream sequence is invalid", strings.Join(streamValidation.Problems, "; ")
		} else if streamValidation.Incomplete {
			test.Status, test.Summary = result.StatusError, "Thinking stream ended before message_stop"
		} else {
			test.Status, test.Summary = result.StatusPass, "Thinking stream used a legal Anthropic event sequence; thinking content was not retained"
		}
		return test
	})
	return []result.TestResult{adaptive, effort, manual, validation, streamProbe}
}

func thinkingMessageProbe(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, id string, body map[string]any, mode, success string, telemetryProvider *telemetry.Provider) result.TestResult {
	return runNativeProbe(parent, telemetryProvider, id, func(probeCtx context.Context) result.TestResult {
		resp, err := call(probeCtx, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, body) })
		test := testBase(p, id)
		test.DurationMS, test.HTTP, test.Usage = resp.Duration.Milliseconds(), metadataOnlyObservation(resp), ParseUsage(resp.JSON)
		setHTTPStatus(&test, resp, err, true)
		if test.Status == result.StatusUnsupported {
			test.Signals = []result.EvidenceSignal{{Code: "anthropic.thinking.unsupported", Parameters: map[string]any{"mode": mode}}}
		}
		if test.Status == result.StatusPass {
			test.Summary = success
		}
		return test
	})
}

func runVision(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string, telemetryProvider *telemetry.Provider) []result.TestResult {
	const encoded = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	body := basicBody(model)
	body["messages"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": encoded}}, map[string]any{"type": "text", "text": "Reply with exactly AIQA_OK."}}}}
	visionResults := runNativeProbeResults(parent, telemetryProvider, "anthropic.vision.image_base64", func(probeCtx context.Context) []result.TestResult {
		resp, err := call(probeCtx, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, body) })
		test := testBase(p, "anthropic.vision.image_base64")
		test.DurationMS, test.HTTP = resp.Duration.Milliseconds(), observation(resp)
		setHTTPStatus(&test, resp, err, true)
		if test.Status == result.StatusPass && strings.Contains(Text(resp.JSON), "AIQA_OK") {
			test.Summary = "Embedded PNG was accepted and produced the requested semantic answer"
		} else if test.Status == result.StatusPass {
			test.Status, test.Summary = result.StatusWarn, "Embedded PNG was accepted but semantic use was not demonstrated"
		}
		decoded, _ := base64.StdEncoding.DecodeString(encoded)
		digest := sha256.Sum256(decoded)
		test.Artifacts = []result.ArtifactObservation{{Kind: "input_fixture", Source: "embedded", MediaType: "image/png", Format: "png", Bytes: int64(len(decoded)), SHA256: hex.EncodeToString(digest[:]), Width: 1, Height: 1}}
		schema := derived(p, "anthropic.vision.image_schema", test)
		schema.Artifacts = append([]result.ArtifactObservation(nil), test.Artifacts...)
		schema.Summary = "Anthropic base64 image block was accepted"
		usage := derived(p, "anthropic.vision.usage", test)
		usage.Usage = ParseUsage(resp.JSON)
		usage.Summary = "Vision usage is non-negative"
		return []result.TestResult{test, schema, usage}
	})
	return visionResults
}

func basicBody(model string) map[string]any {
	return map[string]any{"model": model, "max_tokens": 32, "messages": []any{map[string]any{"role": "user", "content": "Reply with exactly AIQA_OK and nothing else."}}}
}
func simpleMarkerProbe(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, id string, body map[string]any) result.TestResult {
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, body) })
	t := testBase(p, id)
	t.DurationMS = resp.Duration.Milliseconds()
	t.HTTP = observation(resp)
	t.Usage = ParseUsage(resp.JSON)
	setHTTPStatus(&t, resp, err, true)
	if t.Status == result.StatusPass && strings.Contains(Text(resp.JSON), "AIQA_OK") {
		t.Summary = p.Probe(id).Name + " is supported"
	} else if t.Status == result.StatusPass {
		t.Status = result.StatusWarn
		t.Summary = p.Probe(id).Name + " was accepted but semantic effect was not demonstrated"
	}
	return t
}

func credentialProbe(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, id string, body map[string]any, key, version string) result.TestResult {
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) {
		return client.MessageWithCredentials(ctx, body, key, version)
	})
	t := testBase(p, id)
	t.DurationMS, t.HTTP = resp.Duration.Milliseconds(), observation(resp)
	t.Usage = ParseUsage(resp.JSON)
	setExpectedError(&t, resp, err)
	if t.Status == result.StatusPass {
		t.Summary = p.Probe(id).Name + " returned a compatible client error"
	}
	return t
}
func testBase(p profile.Profile, id string) result.TestResult {
	spec := p.Probe(id)
	return result.TestResult{ID: id, RequirementID: id, ProbeID: "aiqa.native." + id, Name: spec.Name, Suite: "compat", Category: spec.Category, Required: spec.Required, Expected: spec.Description}
}
func derived(p profile.Profile, id string, source result.TestResult) result.TestResult {
	t := testBase(p, id)
	t.DerivedFrom = source.ID
	t.DurationMS = source.DurationMS
	t.HTTP = source.HTTP
	if source.Status != result.StatusPass {
		t.Status = result.StatusSkip
		t.Summary = fmt.Sprintf("skipped because prerequisite %s did not pass", source.RequirementID)
		t.Signals = []result.EvidenceSignal{{Code: "blocked." + strings.ReplaceAll(id, "_", "."), RootCause: "blocked-prerequisite", Parameters: map[string]any{"blocked_by_requirement": source.RequirementID}}}
		return t
	}
	t.Status = result.StatusPass
	return t
}
func call(parent context.Context, timeout time.Duration, fn func(context.Context) (api.Response, error)) (api.Response, error) {
	ctx, cancel := api.WithRequestTimeout(parent, timeout)
	defer cancel()
	return fn(ctx)
}
func setHTTPStatus(t *result.TestResult, resp api.Response, err error, optional bool) {
	if err != nil && resp.StatusCode == 0 {
		t.Status = result.StatusError
		t.Summary = "Request could not be completed"
		t.Error = err.Error()
		return
	}
	if isTransientStatus(resp.StatusCode) {
		t.Status = result.StatusError
		t.Summary = fmt.Sprintf("Provider/runtime error HTTP %d makes compatibility inconclusive", resp.StatusCode)
		return
	}
	if resp.Is2xx() {
		t.Status = result.StatusPass
		return
	}
	if optional && (resp.StatusCode == 400 || resp.StatusCode == 404 || resp.StatusCode == 422) {
		t.Status = result.StatusUnsupported
		t.Summary = fmt.Sprintf("Optional capability is not supported (HTTP %d)", resp.StatusCode)
		return
	}
	t.Status = result.StatusFail
	t.Summary = fmt.Sprintf("Request returned HTTP %d", resp.StatusCode)
	if err != nil {
		t.Error = err.Error()
	}
}
func setExpectedError(t *result.TestResult, resp api.Response, err error) {
	if err != nil && resp.StatusCode == 0 {
		t.Status = result.StatusError
		t.Summary = "Error probe could not be completed"
		t.Error = err.Error()
		return
	}
	if isTransientStatus(resp.StatusCode) {
		t.Status = result.StatusError
		t.Summary = "Provider/runtime error makes the negative probe inconclusive"
		return
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		if len(ValidateError(resp.JSON)) == 0 {
			t.Status = result.StatusPass
		} else {
			t.Status = result.StatusFail
			t.Summary = "Client error does not use Anthropic error shape"
		}
		return
	}
	t.Status = result.StatusFail
	t.Summary = fmt.Sprintf("Invalid request was not rejected with a client error (HTTP %d)", resp.StatusCode)
}

func isTransientStatus(status int) bool {
	return status == 408 || status == 429 || status >= 500
}

func streamEventTypes(stream api.StreamResponse) []string {
	values := make([]string, 0, len(stream.Events))
	for _, event := range stream.Events {
		kind := event.Event
		if event.Data == "[DONE]" {
			kind = "[DONE]"
		}
		if kind == "" {
			kind = stringValue(event.JSON["type"])
		}
		if kind == "" {
			kind = "<unnamed>"
		}
		values = append(values, kind)
	}
	return values
}
func observation(resp api.Response) result.HTTPObservation {
	return result.HTTPObservation{StatusCode: resp.StatusCode, ContentType: resp.ContentType, RequestID: resp.RequestID, Snippet: truncate(resp.Snippet(1000), 1000), TraceID: resp.TraceID}
}
func metadataOnlyObservation(resp api.Response) result.HTTPObservation {
	return result.HTTPObservation{StatusCode: resp.StatusCode, ContentType: resp.ContentType, RequestID: resp.RequestID, TraceID: resp.TraceID}
}
func profileInfo(p profile.Profile) result.ProfileInfo {
	return result.ProfileInfo{SchemaVersion: p.SchemaVersion, ID: p.ID, Protocol: p.Protocol, Revision: p.Revision, APIVersion: p.APIVersion, BaseURLSemantics: p.BaseURLSemantics, Source: p.Source, SourceCommit: p.SourceCommit, SourceRefRevision: p.SourceRefRevision, Scope: p.Scope}
}
func toTarget(target config.TargetConfig, r result.Report) result.TargetReport {
	return result.TargetReport{ID: target.ID, Protocol: string(target.Protocol), Profile: r.Profile, Discovery: r.Discovery, Models: r.Models, Requirements: r.Requirements, Summary: r.Summary, Guidance: r.Guidance, RateLimit: r.RateLimit}
}
func selectModels(requested []string, models []api.Model) ([]string, map[string]map[string]any) {
	return protocolpkg.SelectModels(requested, models, protocolpkg.ExactModelID, anthropicInventoryTrusted)
}

func requestedModels(cfg config.Config, target config.TargetConfig) []string {
	if len(cfg.Models) > 0 {
		return append([]string(nil), cfg.Models...)
	}
	return append([]string(nil), target.Models...)
}

func anthropicInventoryTrusted(metadata map[string]any) bool {
	if metadata == nil {
		return false
	}
	root := map[string]any{"data": []any{metadata}, "has_more": false, "first_id": nil, "last_id": nil}
	return len(ValidateModels(root)) == 0
}

func runMessagesPreflight(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) (result.TestResult, api.Response, error) {
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, basicBody(model)) })
	test := testBase(p, "anthropic.messages.basic")
	test.DurationMS, test.HTTP = resp.Duration.Milliseconds(), observation(resp)
	test.Usage = ParseUsage(resp.JSON)
	setHTTPStatus(&test, resp, err, false)
	if test.Status == result.StatusPass {
		if strings.Contains(Text(resp.JSON), "AIQA_OK") {
			test.Summary = "Minimal Anthropic Message returned the requested marker"
		} else {
			test.Status = result.StatusFail
			test.Summary = "Message succeeded but did not return AIQA_OK"
			test.Observation = truncate(Text(resp.JSON), 200)
		}
	}
	return test, resp, err
}

func isTransientResponse(resp api.Response, err error) bool {
	return err != nil && resp.StatusCode == 0 || isTransientStatus(resp.StatusCode)
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

func rateLimitStats(stats api.PacingStats) *result.RateLimitStats {
	return protocolpkg.RateLimitStats(stats)
}
func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}
func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func moduleExecuted(modules []result.ModuleAssessment, id string) bool {
	for _, module := range modules {
		if module.ID == id {
			return module.Execution == result.ExecutionExecuted
		}
	}
	return false
}

var _ = http.MethodPost
