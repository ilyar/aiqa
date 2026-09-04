package googlegenai

import (
	"context"
	"crypto/sha256"
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
	"github.com/ilyar/aiqa/internal/probe"
	"github.com/ilyar/aiqa/internal/profile"
	protocolpkg "github.com/ilyar/aiqa/internal/protocol"
	reportpkg "github.com/ilyar/aiqa/internal/report"
	"github.com/ilyar/aiqa/internal/result"
)

type RunOptions struct {
	Target    config.TargetConfig
	Config    config.Config
	Profile   profile.Profile
	UserAgent string
	Logf      func(string, ...any)
	Runtime   *Runtime
}

type Runtime struct {
	Client   *api.Client
	Executor *probe.Executor
}

// RunTarget adapts the Google GenAI implementation to the shared protocol
// registry contract.
func RunTarget(ctx context.Context, options protocolpkg.RunOptions) (protocolpkg.RunResult, error) {
	runtime := &Runtime{}
	target, err := Run(ctx, RunOptions{
		Target: options.Target, Config: options.Config, Profile: options.Profile,
		UserAgent: options.UserAgent, Logf: options.Logf, Runtime: runtime,
	})
	target.Notes = []string{"Official Google Gen AI Go SDK v1.71.0 does not expose public Interactions API methods; no SDK Interactions suite is claimed."}
	return protocolpkg.RunResult{
		Target:  target,
		Runtime: protocolpkg.Runtime{Client: runtime.Client, Executor: runtime.Executor},
	}, err
}

func Run(ctx context.Context, options RunOptions) (result.TargetReport, error) {
	target, cfg, p := options.Target, options.Config, options.Profile
	overrides, err := capability.LoadOverrideFile(target.CapabilityFile)
	if err != nil {
		return result.TargetReport{}, err
	}
	if overrides == nil {
		overrides = map[string][]string{}
	}
	for _, raw := range target.ModelCapabilities {
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 {
			return result.TargetReport{}, fmt.Errorf("invalid --google-genai-model-capability %q", raw)
		}
		values := strings.Split(parts[1], "+")
		for index := range values {
			values[index] = strings.TrimSpace(values[index])
			if !capability.Known[values[index]] || !strings.HasPrefix(values[index], "google-genai.") {
				return result.TargetReport{}, fmt.Errorf("unknown or non-Google capability %q", values[index])
			}
		}
		overrides[strings.TrimSpace(parts[0])] = values
	}
	client, err := NewClient(target.BaseURL, target.APIKey, target.APIVersion, options.UserAgent, target.RequestInterval)
	if err != nil {
		return result.TargetReport{}, err
	}
	client.SetConcurrency(cfg.Concurrency)
	executor := probe.NewExecutor(cfg.Concurrency)
	if options.Runtime != nil {
		options.Runtime.Client = client.TransportClient()
		options.Runtime.Executor = executor
	}
	requested := requestedModels(cfg, target)
	tmp := result.Report{SchemaVersion: "aiqa.report.internal", Profile: profileInfo(p), Config: result.PublicConfig{BaseURL: target.BaseURL, Tests: append([]string(nil), cfg.Tests...), RequestedModels: requested, Timeout: cfg.Timeout.String(), Formats: cfg.Formats, ExtSuites: cfg.ExtSuites, FailOn: cfg.FailOn, Guidance: cfg.Guidance, GuidancePack: cfg.GuidancePack, GuidanceLocale: cfg.GuidanceLocale}}

	listCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	models, listResp, paginationResp, listErr := discoverAll(listCtx, client)
	cancel()
	tmp.Discovery.ModelCount = len(models)
	for _, model := range models {
		tmp.Discovery.Models = append(tmp.Discovery.Models, cloneMap(model.Raw))
	}
	if cfg.HasTest("compat") {
		tmp.Discovery.Tests = append(tmp.Discovery.Tests, discoveryResults(p, listResp, paginationResp, models, listErr)...)
	}
	selected, metadata := selectModels(requested, models)
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
		options.Logf("[%s] testing %d model(s): %s", target.ID, len(selected), strings.Join(selected, ", "))
	}
	modelResults := make([]result.ModelReport, len(selected))
	preflights := make([]*api.Response, len(selected))
	preflightErrors := make([]error, len(selected))
	preflightTests := make([]*result.TestResult, len(selected))
	routeBlocked := make([]bool, len(selected))
	classifications := make([]capability.Classification, len(selected))
	modulePlans := make([][]result.ModuleAssessment, len(selected))
	for index, model := range selected {
		unknownPolicy := target.UnknownModel
		if len(requested) > 0 {
			unknownPolicy = "inventory"
		}
		classification := capability.ClassifyGoogleGenAI(model, metadata[model], capability.Options{BaseURL: target.BaseURL, Overrides: overrides, UnknownPolicy: unknownPolicy})
		if len(requested) > 0 && classification.Unknown {
			test, response, preflightErr := runGooglePreflight(ctx, client, cfg.Timeout, p, model)
			preflightTests[index], preflights[index], preflightErrors[index] = &test, &response, preflightErr
			switch {
			case response.Is2xx() && preflightErr == nil:
				classification.Unknown = false
				classification.Reason = ""
				capabilityID := capability.GoogleGenerateText
				if !strings.Contains(p.ID, "generate-content") {
					capabilityID = capability.GoogleInteractionsText
				}
				classification.Capabilities = append(classification.Capabilities, capability.Evidence{Capability: capabilityID, Source: "preflight", Rule: "google-genai.text.preflight", Confidence: "high", Reason: "minimal Google text preflight accepted the requested model"})
			case isTransientResponse(response, preflightErr):
				classification.Reason = "model availability is inconclusive after transient preflight failure"
				routeBlocked[index] = true
				markRouteBlockedTest(preflightTests[index], "model availability could not be determined after a transient preflight failure")
			default:
				if isModelRouteRejection(response) {
					classification.Reason = "explicit model route rejection"
					markRouteRejectionTest(preflightTests[index])
				} else {
					classification.Reason = "model availability is inconclusive after validation preflight failure"
					markRouteBlockedTest(preflightTests[index], "model availability could not be determined from this validation response")
				}
				routeBlocked[index] = true
			}
		}
		if interactionEligible(requested, p, model) && (classification.Unknown || classification.Has(capability.GoogleGenerateText) || classification.Has(capability.GoogleInteractionsText)) {
			// Interactions is selected by explicit/profile policy, not by the
			// legacy generateContent inventory method.
			classification.Capabilities = append(classification.Capabilities,
				capability.Evidence{Capability: capability.GoogleInteractionsText, Source: "profile_allowlist", Rule: "google-genai.interactions.allowlist", Confidence: "high", Reason: "model is explicitly eligible for the selected Interactions surface"},
				capability.Evidence{Capability: capability.GoogleInteractionsTools, Source: "profile_allowlist", Rule: "google-genai.interactions.allowlist", Confidence: "medium", Reason: "text model is eligible for client-side Interactions tool probing"},
				capability.Evidence{Capability: capability.GoogleInteractionsStructured, Source: "profile_allowlist", Rule: "google-genai.interactions.allowlist", Confidence: "medium", Reason: "text model is eligible for Interactions structured-output probing"},
			)
			if cfg.AllowPersistentResources {
				classification.Capabilities = append(classification.Capabilities,
					capability.Evidence{Capability: capability.GoogleInteractionsStateful, Source: "explicit_opt_in", Rule: "google-genai.interactions.stateful.opt_in", Confidence: "high", Reason: "persistent Interaction probing was explicitly authorized"},
				)
			}
		}
		classification.Capabilities = uniqueEvidence(classification.Capabilities)
		classifications[index] = classification
		modulePlans[index] = capability.AssessModules(classification, p.Modules, capability.RoutingOptions{Modules: target.Modules, AllowBillableArtifacts: cfg.AllowBillableArtifacts, AllowHighTokenProbes: cfg.AllowHighTokenProbes, AllowPersistentResources: cfg.AllowPersistentResources})
		if routeBlocked[index] {
			for moduleIndex := range modulePlans[index] {
				if modulePlans[index][moduleIndex].Applicability != result.ApplicabilityNotApplicable {
					modulePlans[index][moduleIndex].BlockedBy = []string{"google.text.basic"}
				}
			}
		}
	}
	var wg sync.WaitGroup
	for index, model := range selected {
		index, model := index, model
		wg.Add(1)
		go func() {
			defer wg.Done()
			classification := classifications[index]
			modules := modulePlans[index]
			mr := result.ModelReport{ID: model, Metadata: cloneMap(metadata[model]), Modules: modules}
			for _, evidence := range classification.Capabilities {
				mr.Capabilities = append(mr.Capabilities, result.CapabilityAssessment{ID: evidence.Capability, Source: evidence.Source, Rule: evidence.Rule, Confidence: evidence.Confidence, Reason: evidence.Reason})
			}
			if cfg.HasTest("compat") && preflightTests[index] != nil && routeBlocked[index] {
				mr.Compatibility = append(mr.Compatibility, *preflightTests[index])
			} else if cfg.HasTest("compat") {
				mr.Compatibility = runModelWithPreflight(ctx, client, cfg, p, model, modules, executor, strings.Contains(p.ID, "generate-content"), preflights[index], preflightErrors[index])
			}
			if cfg.HasTest("model-param") && hasAnyGenerationModule(modules, strings.Contains(p.ID, "generate-content")) {
				mr.Compatibility = append(mr.Compatibility, runGoogleModelParamNotice(p)...)
			}
			sort.Slice(mr.Compatibility, func(i, j int) bool { return mr.Compatibility[i].ID < mr.Compatibility[j].ID })
			stats := client.PacingStatsForModel(model)
			mr.RateLimit = protocolpkg.RateLimitStats(stats)
			modelResults[index] = mr
		}()
	}
	wg.Wait()
	tmp.Models = modelResults
	if cfg.HasTest("compat") && len(p.InteractionModels()) > 0 {
		selection := testBase(p, "google.interactions.model_selection")
		eligible := false
		for _, model := range modelResults {
			for _, item := range model.Capabilities {
				if item.ID == capability.GoogleInteractionsText {
					eligible = true
					break
				}
			}
			if eligible {
				break
			}
		}
		if eligible {
			selection.Status = result.StatusPass
			selection.Summary = "An explicit or profile-allowlisted model was selected for Interactions"
		} else {
			selection.Status = result.StatusError
			selection.Summary = "No eligible Interactions model was selected; core evidence is inconclusive"
			selection.Error = "select a documented Interactions model with --model"
		}
		tmp.Discovery.Tests = append(tmp.Discovery.Tests, selection)
	}
	stats := client.PacingStats()
	tmp.RateLimit = *protocolpkg.RateLimitStats(stats)
	reportpkg.Finalize(&tmp)
	guidance.ExtractArtifacts(&tmp)
	guidance.QualifyArtifacts(&tmp, target.ID)
	assessment.ApplyTarget(&tmp, p, target.ID, string(target.Protocol))
	return toTarget(target, tmp), nil
}

func hasAnyGenerationModule(modules []result.ModuleAssessment, legacy bool) bool {
	if legacy {
		return hasModule(modules, "google-genai.generate-content")
	}
	return hasModule(modules, "google-genai.interactions")
}

func runGoogleModelParamNotice(p profile.Profile) []result.TestResult {
	t := testBase(p, "google.model-param.unsupported")
	t.Suite = "model-param"
	t.Applicability = result.ApplicabilityApplicable
	t.Status = result.StatusUnsupported
	t.Summary = "Google model-parameter analysis is not implemented; no parameter calls were made"
	return []result.TestResult{t}
}

func runGooglePreflight(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) (result.TestResult, api.Response, error) {
	legacy := strings.Contains(p.ID, "generate-content")
	var resp api.Response
	var err error
	if legacy {
		resp, err = call(parent, timeout, func(ctx context.Context) (api.Response, error) {
			return client.GenerateContent(ctx, model, generationBody(model, "Reply with exactly AIQA_OK."))
		})
	} else {
		resp, err = call(parent, timeout, func(ctx context.Context) (api.Response, error) {
			return client.Interaction(ctx, map[string]any{"model": model, "input": "Reply with exactly AIQA_OK.", "store": false})
		})
	}
	id := "google.interactions.basic"
	if legacy {
		id = "google.generate-content.basic"
	}
	test := testBase(p, id)
	test.DurationMS, test.HTTP = resp.Duration.Milliseconds(), observation(resp)
	test.Usage = ParseUsage(resp.JSON)
	setHTTPStatus(&test, resp, err, false)
	if test.Status == result.StatusPass {
		text := interactionText(resp.JSON)
		if legacy {
			text = generateText(resp.JSON)
		}
		if strings.Contains(text, "AIQA_OK") {
			test.Summary = "Minimal Google text preflight returned the requested marker"
		} else {
			test.Status, test.Summary = result.StatusWarn, "Google text preflight completed but did not return AIQA_OK"
		}
	}
	return test, resp, err
}

func discoverAll(ctx context.Context, client *Client) ([]api.Model, api.Response, api.Response, error) {
	models, first, err := client.ListModelsPage(ctx, "")
	if err != nil || !first.Is2xx() {
		return models, first, first, err
	}
	last := first
	for page := 0; page < 10; page++ {
		token := client.NextPageToken(last)
		if token == "" {
			return models, first, last, nil
		}
		next, response, nextErr := client.ListModelsPage(ctx, token)
		last = response
		if nextErr != nil {
			return models, first, last, nextErr
		}
		models = append(models, next...)
	}
	return models, first, last, fmt.Errorf("Google models list pagination exceeded the 10-page safety bound")
}

func discoveryResults(p profile.Profile, resp, paginationResp api.Response, models []api.Model, err error) []result.TestResult {
	httpTest := testBase(p, "google.models.list.http")
	httpTest.DurationMS, httpTest.HTTP = resp.Duration.Milliseconds(), observation(resp)
	setHTTPStatus(&httpTest, resp, err, false)
	if httpTest.Status == result.StatusPass {
		httpTest.Summary = "Google Models endpoint returned JSON"
	}
	schema := testBase(p, "google.models.list.schema")
	schema.DurationMS, schema.HTTP = httpTest.DurationMS, httpTest.HTTP
	if httpTest.Status != result.StatusPass {
		schema.Status, schema.Summary, schema.Error = result.StatusSkip, "Model schema could not be evaluated", httpTest.Error
		schema.Signals = []result.EvidenceSignal{{
			Code: "blocked.google.models.list.schema", RootCause: "blocked-prerequisite",
			Parameters: map[string]any{"blocked_by_requirement": "google.models.list.http"},
		}}
	} else if problems := ValidateModels(resp.JSON); len(problems) > 0 {
		schema.Status = result.StatusFail
		schema.Summary = "Google model-list schema is invalid"
		schema.Observation = strings.Join(problems, "; ")
	} else {
		schema.Status = result.StatusPass
		schema.Summary = fmt.Sprintf("Google model-list schema is valid (%d models)", len(models))
	}
	unique := testBase(p, "google.models.list.unique_names")
	unique.DurationMS, unique.HTTP = schema.DurationMS, schema.HTTP
	seen := map[string]bool{}
	if schema.Status != result.StatusPass {
		unique.Status = result.StatusSkip
		unique.Summary = "Unique model names were not checked because model discovery was incomplete"
		unique.Signals = []result.EvidenceSignal{{
			Code: "blocked.google.models.list.unique_names", RootCause: "blocked-prerequisite",
			Parameters: map[string]any{"blocked_by_requirement": "google.models.list.schema"},
		}}
	} else {
		unique.Status = result.StatusPass
		for _, model := range models {
			if seen[model.ID] {
				unique.Status = result.StatusFail
			}
			seen[model.ID] = true
		}
		if unique.Status == result.StatusPass {
			unique.Summary = "Google model names are unique after removing models/ prefix"
		} else {
			unique.Summary = "Google model names are duplicated"
		}
	}
	auth := derived(p, "google.auth.x_goog_api_key", httpTest)
	auth.Summary = "x-goog-api-key authentication was accepted"
	if auth.Status != result.StatusPass {
		auth.Signals = []result.EvidenceSignal{{Code: "google.auth.x_goog_api_key.rejected"}}
	}
	pagination := testBase(p, "google.models.list.pagination")
	if schema.Status != result.StatusPass {
		pagination.Status = result.StatusSkip
		pagination.Summary = "Pagination could not be evaluated because model discovery failed"
		pagination.Signals = []result.EvidenceSignal{{
			Code: "blocked.google.models.list.pagination", RootCause: "blocked-prerequisite",
			Parameters: map[string]any{"blocked_by_requirement": "google.models.list.schema"},
		}}
	} else if token := stringValue(resp.JSON["nextPageToken"]); token == "" {
		pagination.Applicability, pagination.Status = result.ApplicabilityNotApplicable, result.StatusSkip
		pagination.Summary = "Model inventory fit in one page; pagination was not applicable"
	} else if err != nil || !paginationResp.Is2xx() {
		pagination.Status = result.StatusError
		if paginationResp.StatusCode != 0 && !isTransientStatus(paginationResp.StatusCode) {
			pagination.Status = result.StatusFail
		}
		pagination.Summary = "Model-list pagination could not retrieve the follow-up page"
		if err != nil {
			pagination.Error = err.Error()
		}
	} else {
		pagination.Status = result.StatusPass
		pagination.Summary = "Model-list pagination returned a follow-up page"
	}
	return []result.TestResult{httpTest, schema, unique, auth, pagination}
}

func runModel(parent context.Context, client *Client, cfg config.Config, p profile.Profile, model string, modules []result.ModuleAssessment, executor *probe.Executor, legacy bool) []result.TestResult {
	return runModelWithPreflight(parent, client, cfg, p, model, modules, executor, legacy, nil, nil)
}

func runModelWithPreflight(parent context.Context, client *Client, cfg config.Config, p profile.Profile, model string, modules []result.ModuleAssessment, executor *probe.Executor, legacy bool, preflight *api.Response, preflightErr error) []result.TestResult {
	tasks := make([]func() []result.TestResult, 0, 12)
	tasks = append(tasks, func() []result.TestResult { return runModelGet(parent, client, cfg.Timeout, p, model) })
	if hasModule(modules, "google-genai.interactions") {
		tasks = append(tasks, func() []result.TestResult {
			return runInteractionsWithPreflight(parent, client, cfg.Timeout, p, model, preflight, preflightErr)
		})
	}
	if hasModule(modules, "google-genai.interactions-stream") {
		tasks = append(tasks, func() []result.TestResult { return runInteractionStream(parent, client, cfg.Timeout, p, model) })
	}
	if hasModule(modules, "google-genai.interactions-tools") {
		tasks = append(tasks, func() []result.TestResult { return runInteractionTools(parent, client, cfg.Timeout, p, model) })
	}
	if hasModule(modules, "google-genai.interactions-structured-output") {
		tasks = append(tasks, func() []result.TestResult { return runInteractionStructured(parent, client, cfg.Timeout, p, model) })
	}
	if hasModule(modules, "google-genai.generate-content") {
		tasks = append(tasks, func() []result.TestResult {
			return runGenerateContentWithPreflight(parent, client, cfg.Timeout, p, model, preflight, preflightErr)
		})
	}
	if hasModule(modules, "google-genai.generate-content-stream") {
		tasks = append(tasks, func() []result.TestResult { return runGenerateStream(parent, client, cfg.Timeout, p, model) })
	}
	if hasModule(modules, "google-genai.generate-content-tools") {
		tasks = append(tasks, func() []result.TestResult { return runGenerateTools(parent, client, cfg.Timeout, p, model) })
	}
	if hasModule(modules, "google-genai.generate-content-structured-output") {
		tasks = append(tasks, func() []result.TestResult { return runGenerateStructured(parent, client, cfg.Timeout, p, model) })
	}
	if hasModule(modules, "google-genai.token-counting") {
		tasks = append(tasks, func() []result.TestResult { return runCountTokens(parent, client, cfg.Timeout, p, model) })
	}
	if hasModule(modules, "google-genai.embeddings") {
		tasks = append(tasks, func() []result.TestResult { return runEmbedding(parent, client, cfg.Timeout, p, model) })
	}
	if hasModule(modules, "google-genai.interactions-thinking") || hasModule(modules, "google-genai.thinking") {
		tasks = append(tasks, func() []result.TestResult { return runThinking(parent, client, cfg.Timeout, p, model, legacy) })
	}
	if hasModule(modules, "google-genai.interactions-stateful") {
		tasks = append(tasks, func() []result.TestResult { return runInteractionStateful(parent, client, cfg.Timeout, p, model) })
	}
	if hasModule(modules, "google-genai.interactions-vision") {
		tasks = append(tasks, func() []result.TestResult { return runInteractionVision(parent, client, cfg.Timeout, p, model) })
	}
	if hasModule(modules, "google-genai.vision") {
		tasks = append(tasks, func() []result.TestResult { return runGenerateVision(parent, client, cfg.Timeout, p, model) })
	}
	if (legacy && hasModule(modules, "google-genai.generate-content")) || (!legacy && hasModule(modules, "google-genai.interactions")) {
		tasks = append(tasks, func() []result.TestResult { return runGoogleErrors(parent, client, cfg.Timeout, p, model, legacy) })
	}
	var out []result.TestResult
	for _, group := range probe.Map(executor, tasks...) {
		out = append(out, group...)
	}
	return out
}

func runInteractions(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) []result.TestResult {
	return runInteractionsWithPreflight(parent, client, timeout, p, model, nil, nil)
}

func runInteractionsWithPreflight(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string, preflight *api.Response, preflightErr error) []result.TestResult {
	var resp api.Response
	var err error
	if preflight != nil {
		resp, err = *preflight, preflightErr
	} else {
		body := map[string]any{"model": model, "input": "Reply with exactly AIQA_OK.", "store": false}
		resp, err = call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Interaction(ctx, body) })
	}
	base := testBase(p, "google.interactions.basic")
	base.DurationMS, base.HTTP = resp.Duration.Milliseconds(), observation(resp)
	base.Usage = ParseUsage(resp.JSON)
	setHTTPStatus(&base, resp, err, !p.Probe(base.ID).Required)
	if base.Status == result.StatusPass {
		if !strings.Contains(interactionText(resp.JSON), "AIQA_OK") {
			base.Status, base.Summary = result.StatusWarn, "Interaction completed but did not return AIQA_OK"
		} else {
			base.Summary = "Interactions API returned the requested marker"
		}
	}
	schema := derived(p, "google.interactions.schema", base)
	if base.Status == result.StatusPass {
		if problems := ValidateInteraction(resp.JSON); len(problems) > 0 {
			schema.Status, schema.Summary, schema.Observation = result.StatusFail, "Interaction response schema is invalid", strings.Join(problems, "; ")
		} else if status := stringValue(resp.JSON["status"]); !knownInteractionStatus(status) {
			schema.Status, schema.Summary = result.StatusWarn, "Interaction response uses an unknown future status; the rest of the schema is valid"
		} else {
			schema.Summary = "Interaction response schema is valid"
		}
	}
	textTest := derived(p, "google.interactions.output_text", base)
	textTest.Summary = "Interaction output text was extracted without exposing private thought content"
	usage := derived(p, "google.interactions.usage", base)
	usage.Usage = ParseUsage(resp.JSON)
	if base.Status == result.StatusPass && len(ValidateUsage(usageMap(resp.JSON))) > 0 {
		usage.Status, usage.Summary, usage.Observation = result.StatusFail, "Interaction usage metadata is invalid", strings.Join(ValidateUsage(usageMap(resp.JSON)), "; ")
	} else {
		usage.Summary = "Interaction usage was normalized"
	}
	store := derived(p, "google.interactions.store_false", base)
	store.Summary = "Stateless Interaction request used store=false"
	status := derived(p, "google.interactions.status", base)
	if status.Status == result.StatusPass && !knownInteractionStatus(stringValue(resp.JSON["status"])) {
		status.Status, status.Summary = result.StatusWarn, "Interaction returned an unknown future status"
	} else {
		status.Summary = "Interaction status is a recognized terminal state"
	}
	return []result.TestResult{base, schema, textTest, usage, store, status}
}

func runModelGet(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) []result.TestResult {
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.GetModel(ctx, model) })
	t := testBase(p, "google.models.get")
	t.DurationMS, t.HTTP = resp.Duration.Milliseconds(), observation(resp)
	setHTTPStatus(&t, resp, err, true)
	if t.Status == result.StatusPass {
		if got := NormalizeModelID(stringValue(resp.JSON["name"])); got != "" && got != NormalizeModelID(model) {
			t.Status, t.Summary = result.StatusFail, "Model metadata name does not match the request"
		} else {
			t.Summary = "Model metadata was retrieved for the selected model"
		}
	}
	limits := derived(p, "google.models.metadata.limits", t)
	methods := derived(p, "google.models.metadata.methods", t)
	thinking := derived(p, "google.models.metadata.thinking", t)
	if t.Status != result.StatusPass {
		limits.Status, methods.Status, thinking.Status = result.StatusSkip, result.StatusSkip, result.StatusSkip
		limits.Summary, methods.Summary, thinking.Summary = "Model metadata was unavailable", "Model metadata was unavailable", "Model metadata was unavailable"
		return []result.TestResult{t, limits, methods, thinking}
	}
	input, inputOK := nonNegativeMetadataInteger(resp.JSON["inputTokenLimit"])
	output, outputOK := nonNegativeMetadataInteger(resp.JSON["outputTokenLimit"])
	if !inputOK && !outputOK {
		limits.Status, limits.Summary = result.StatusSkip, "The provider did not advertise token limits"
	} else if (inputOK && input < 0) || (outputOK && output < 0) || (inputOK && outputOK && output > input) {
		limits.Status, limits.Summary = result.StatusFail, "Advertised token limits are invalid or incoherent"
	} else {
		limits.Status, limits.Summary = result.StatusPass, "Advertised token limits are non-negative and coherent"
		limits.Metrics = map[string]any{}
		if inputOK {
			limits.Metrics["input_token_limit"] = input
		}
		if outputOK {
			limits.Metrics["output_token_limit"] = output
		}
	}
	if raw, exists := resp.JSON["supportedGenerationMethods"]; exists {
		values := stringValues(raw)
		if len(values) == 0 {
			methods.Status, methods.Summary = result.StatusFail, "supportedGenerationMethods is not a non-empty string array"
		} else {
			methods.Status, methods.Summary = result.StatusPass, "supportedGenerationMethods is a string array"
			methods.Metrics = map[string]any{"method_count": len(values)}
		}
	} else {
		methods.Status, methods.Summary = result.StatusSkip, "The provider did not advertise supportedGenerationMethods"
	}
	if raw, exists := resp.JSON["thinking"]; exists {
		if _, ok := metadataBool(raw); !ok {
			thinking.Status, thinking.Summary = result.StatusFail, "thinking metadata is not boolean"
		} else {
			thinking.Status, thinking.Summary = result.StatusPass, "thinking metadata is boolean"
		}
	} else {
		thinking.Status, thinking.Summary = result.StatusSkip, "The provider did not advertise thinking metadata"
	}
	return []result.TestResult{t, limits, methods, thinking}
}

func runInteractionStream(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) []result.TestResult {
	body := map[string]any{"model": model, "input": "Reply with exactly AIQA_OK.", "store": false, "stream": true}
	stream, err := callStream(parent, timeout, func(ctx context.Context) (api.StreamResponse, error) { return client.StreamInteraction(ctx, body) })
	validation := ValidateInteractionStream(stream)
	httpTest := testBase(p, "google.interactions.stream.http")
	httpTest.DurationMS = stream.Duration.Milliseconds()
	httpTest.HTTP = streamObservation(stream)
	if err != nil {
		setStreamError(&httpTest, err, stream.StatusCode, !p.Probe(httpTest.ID).Required)
	} else if stream.StatusCode < 200 || stream.StatusCode >= 300 || !strings.Contains(strings.ToLower(stream.ContentType), "text/event-stream") {
		setStreamHTTPStatus(&httpTest, stream.StatusCode, !p.Probe(httpTest.ID).Required, "Interactions stream did not return a successful SSE response")
	} else {
		httpTest.Status, httpTest.Summary = result.StatusPass, "Interactions stream uses SSE"
	}
	sequence := derived(p, "google.interactions.stream.sequence", httpTest)
	sequence.Usage = validation.Usage
	if httpTest.Status == result.StatusPass {
		switch {
		case validation.ProviderError:
			sequence.Status, sequence.Summary = result.StatusError, "Provider emitted an error event during the Interactions stream"
		case len(validation.Problems) > 0:
			sequence.Status, sequence.Summary, sequence.Observation = result.StatusFail, "Interactions SSE sequence is invalid", strings.Join(validation.Problems, "; ")
		case !validation.Complete:
			sequence.Status, sequence.Summary = result.StatusError, "Interactions stream ended without completed/[DONE]"
		default:
			sequence.Status, sequence.Summary = result.StatusPass, "Interactions SSE sequence is valid"
		}
	}
	syntax := derived(p, "google.interactions.stream.syntax", httpTest)
	if httpTest.Status == result.StatusPass && len(stream.InvalidEvents) > 0 {
		syntax.Status, syntax.Summary, syntax.Observation = result.StatusFail, "Interactions SSE framing or JSON syntax is invalid", strings.Join(stream.InvalidEvents, "; ")
	} else {
		syntax.Summary = "Interactions SSE framing and JSON data are valid"
	}
	eventTypes := derived(p, "google.interactions.stream.event_type_match", sequence)
	if sequence.Status == result.StatusPass {
		if hasProblem(validation.Problems, "event name") {
			eventTypes.Status, eventTypes.Summary = result.StatusFail, "Known SSE event names do not match JSON event types"
		} else {
			eventTypes.Summary = "Known SSE event names match JSON event types"
		}
	}
	textTest := derived(p, "google.interactions.stream.text", sequence)
	if sequence.Status == result.StatusPass && !strings.Contains(validation.Text, "AIQA_OK") {
		textTest.Status, textTest.Summary = result.StatusFail, "Interactions stream did not reconstruct AIQA_OK"
	} else if sequence.Status == result.StatusPass {
		textTest.Summary = "Interactions stream reconstructed response text"
	}
	completed := derived(p, "google.interactions.stream.completed", sequence)
	if sequence.Status == result.StatusPass {
		if validation.SawCompleted {
			completed.Summary = "Interactions stream contains interaction.completed after step lifecycle"
		} else {
			completed.Status, completed.Summary = result.StatusFail, "Interactions stream omitted interaction.completed"
		}
	}
	usage := derived(p, "google.interactions.stream.usage", sequence)
	usage.Usage = validation.Usage
	if sequence.Status == result.StatusPass {
		if validation.SawUsage {
			usage.Summary = "Interactions stream exposed valid final usage"
		} else {
			usage.Status, usage.Summary = result.StatusFail, "Interactions stream omitted valid final usage"
		}
	}
	done := derived(p, "google.interactions.stream.done", sequence)
	if sequence.Status == result.StatusPass {
		if validation.SawDone {
			done.Summary = "Interactions stream ended with [DONE]"
		} else {
			done.Status, done.Summary = result.StatusFail, "Interactions stream omitted [DONE]"
		}
	}
	return []result.TestResult{httpTest, syntax, eventTypes, sequence, textTest, completed, usage, done}
}

func runInteractionTools(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) []result.TestResult {
	body := map[string]any{"model": model, "input": "Call get_status for resource-alpha.", "store": false, "tools": []any{map[string]any{"type": "function", "name": "get_status", "description": "Return status", "parameters": map[string]any{"type": "object", "properties": map[string]any{"resource_id": map[string]any{"type": "string"}}, "required": []string{"resource_id"}}}}}
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Interaction(ctx, body) })
	t := testBase(p, "google.interactions.tools")
	t.DurationMS, t.HTTP = resp.Duration.Milliseconds(), observation(resp)
	t.Usage = ParseUsage(resp.JSON)
	setHTTPStatus(&t, resp, err, true)
	if t.Status == result.StatusPass {
		name, args, found := interactionToolCall(resp.JSON)
		t.Metrics = map[string]any{"tool_call_observed": found}
		if found && name == "get_status" && json.Valid([]byte(args)) {
			t.Summary = "Interactions tool declaration was accepted and a valid function call was returned"
		} else {
			// A model is allowed to decline an optional tool call.  Keep the
			// transport result conclusive while making the behavioral gap visible.
			t.Status, t.Summary = result.StatusWarn, "Interactions accepted the tool declaration but did not return the expected valid function call"
		}
	}
	return []result.TestResult{t}
}

func runInteractionStructured(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) []result.TestResult {
	body := map[string]any{"model": model, "input": "Return JSON with ok=true and value=AIQA_OK.", "store": false, "response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "aiqa", "schema": map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}, "value": map[string]any{"type": "string"}}, "required": []string{"ok", "value"}, "additionalProperties": false}}}}
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Interaction(ctx, body) })
	t := testBase(p, "google.interactions.structured-output")
	t.DurationMS, t.HTTP = resp.Duration.Milliseconds(), observation(resp)
	t.Usage = ParseUsage(resp.JSON)
	setHTTPStatus(&t, resp, err, true)
	if t.Status == result.StatusPass {
		if !validStructured(interactionText(resp.JSON)) {
			t.Status, t.Summary = result.StatusFail, "Interactions structured output violated the requested JSON schema"
		} else {
			t.Summary = "Interactions structured output conforms to the local JSON schema"
		}
	}
	return []result.TestResult{t}
}

func runGenerateContent(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) []result.TestResult {
	return runGenerateContentWithPreflight(parent, client, timeout, p, model, nil, nil)
}

func runGenerateContentWithPreflight(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string, preflight *api.Response, preflightErr error) []result.TestResult {
	var resp api.Response
	var err error
	if preflight != nil {
		resp, err = *preflight, preflightErr
	} else {
		body := generationBody(model, "Reply with exactly AIQA_OK.")
		resp, err = call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.GenerateContent(ctx, model, body) })
	}
	base := testBase(p, "google.generate-content.basic")
	base.DurationMS, base.HTTP = resp.Duration.Milliseconds(), observation(resp)
	base.Usage = ParseUsage(resp.JSON)
	setHTTPStatus(&base, resp, err, !p.Probe(base.ID).Required)
	if base.Status == result.StatusPass {
		if !strings.Contains(generateText(resp.JSON), "AIQA_OK") {
			base.Status, base.Summary = result.StatusWarn, "generateContent completed but did not return AIQA_OK"
		} else {
			base.Summary = "generateContent returned the requested marker"
		}
	}
	schema := derived(p, "google.generate-content.schema", base)
	if base.Status == result.StatusPass {
		if problems := ValidateGenerateContent(resp.JSON); len(problems) > 0 {
			schema.Status, schema.Summary, schema.Observation = result.StatusFail, "GenerateContent response schema is invalid", strings.Join(problems, "; ")
		} else {
			schema.Summary = "GenerateContent response schema is valid"
		}
	}
	textTest := derived(p, "google.generate-content.output-text", base)
	textTest.Summary = "GenerateContent output text was extracted"
	usage := derived(p, "google.generate-content.usage", base)
	usage.Usage = ParseUsage(resp.JSON)
	if base.Status == result.StatusPass && len(ValidateUsage(usageMap(resp.JSON))) > 0 {
		usage.Status, usage.Summary, usage.Observation = result.StatusFail, "GenerateContent usage metadata is invalid", strings.Join(ValidateUsage(usageMap(resp.JSON)), "; ")
	} else {
		usage.Summary = "GenerateContent usage metadata was normalized"
	}
	return []result.TestResult{base, schema, textTest, usage}
}

func runGenerateStream(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) []result.TestResult {
	stream, err := callStream(parent, timeout, func(ctx context.Context) (api.StreamResponse, error) {
		return client.StreamGenerateContent(ctx, model, generationBody(model, "Reply with exactly AIQA_OK."))
	})
	validation := ValidateGenerateContentStream(stream)
	httpTest := testBase(p, "google.generate-content.stream.http")
	httpTest.DurationMS, httpTest.HTTP = stream.Duration.Milliseconds(), streamObservation(stream)
	if err != nil {
		setStreamError(&httpTest, err, stream.StatusCode, !p.Probe(httpTest.ID).Required)
	} else if stream.StatusCode < 200 || stream.StatusCode >= 300 || !strings.Contains(strings.ToLower(stream.ContentType), "text/event-stream") {
		setStreamHTTPStatus(&httpTest, stream.StatusCode, !p.Probe(httpTest.ID).Required, "GenerateContent stream did not return a successful SSE response")
	} else {
		httpTest.Status, httpTest.Summary = result.StatusPass, "GenerateContent stream uses SSE"
	}
	sequence := derived(p, "google.generate-content.stream.sequence", httpTest)
	sequence.Usage = validation.Usage
	if httpTest.Status == result.StatusPass {
		if validation.ProviderError {
			sequence.Status, sequence.Summary = result.StatusError, "Provider emitted an error during GenerateContent stream"
		} else if len(validation.Problems) > 0 {
			sequence.Status, sequence.Summary, sequence.Observation = result.StatusFail, "GenerateContent stream contained invalid response chunks", strings.Join(validation.Problems, "; ")
		} else {
			sequence.Summary = "GenerateContent stream chunks were parsed to EOF"
		}
	}
	schema := derived(p, "google.generate-content.stream.schema", sequence)
	if sequence.Status == result.StatusPass {
		if len(validation.Problems) > 0 {
			schema.Status, schema.Summary, schema.Observation = result.StatusFail, "GenerateContent stream chunk schema is invalid", strings.Join(validation.Problems, "; ")
		} else {
			schema.Summary = "GenerateContent stream chunks have valid response shapes"
		}
	}
	textTest := derived(p, "google.generate-content.stream.text", sequence)
	if sequence.Status == result.StatusPass && !strings.Contains(validation.Text, "AIQA_OK") {
		textTest.Status, textTest.Summary = result.StatusFail, "GenerateContent stream did not reconstruct AIQA_OK"
	} else if sequence.Status == result.StatusPass {
		textTest.Summary = "GenerateContent stream reconstructed response text"
	}
	finish := derived(p, "google.generate-content.stream.finish_reason", sequence)
	if sequence.Status == result.StatusPass {
		if validation.SawFinishReason || len(stream.FinishReasons) > 0 {
			finish.Summary = "GenerateContent stream exposed a finish reason"
		} else {
			finish.Status, finish.Summary = result.StatusWarn, "GenerateContent stream did not expose a finish reason"
		}
	}
	usage := derived(p, "google.generate-content.stream.usage", sequence)
	usage.Usage = validation.Usage
	if sequence.Status == result.StatusPass {
		if validation.SawUsage {
			usage.Summary = "GenerateContent stream exposed valid usage metadata"
		} else {
			usage.Status, usage.Summary = result.StatusWarn, "GenerateContent stream did not expose usage metadata"
		}
	}
	return []result.TestResult{httpTest, schema, sequence, textTest, finish, usage}
}

func runGenerateTools(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) []result.TestResult {
	body := generationBody(model, "Call get_status for resource-alpha.")
	body["tools"] = []any{map[string]any{"functionDeclarations": []any{map[string]any{"name": "get_status", "description": "Return status", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{"resource_id": map[string]any{"type": "STRING"}}, "required": []string{"resource_id"}}}}}}
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.GenerateContent(ctx, model, body) })
	t := testBase(p, "google.generate-content.tools")
	t.DurationMS, t.HTTP = resp.Duration.Milliseconds(), observation(resp)
	t.Usage = ParseUsage(resp.JSON)
	setHTTPStatus(&t, resp, err, true)
	if t.Status == result.StatusPass {
		name, args, found := generateToolCall(resp.JSON)
		t.Metrics = map[string]any{"tool_call_observed": found}
		if found && name == "get_status" && json.Valid([]byte(args)) {
			t.Summary = "GenerateContent function declaration was accepted and a valid function call was returned"
		} else {
			t.Status, t.Summary = result.StatusWarn, "GenerateContent accepted the tool declaration but did not return the expected valid function call"
		}
	}
	return []result.TestResult{t}
}

func runGenerateStructured(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) []result.TestResult {
	body := generationBody(model, "Return JSON with ok=true and value=AIQA_OK.")
	body["generationConfig"] = map[string]any{"responseMimeType": "application/json", "responseSchema": map[string]any{"type": "OBJECT", "properties": map[string]any{"ok": map[string]any{"type": "BOOLEAN"}, "value": map[string]any{"type": "STRING"}}, "required": []string{"ok", "value"}}}
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.GenerateContent(ctx, model, body) })
	t := testBase(p, "google.generate-content.structured-output")
	t.DurationMS, t.HTTP = resp.Duration.Milliseconds(), observation(resp)
	t.Usage = ParseUsage(resp.JSON)
	setHTTPStatus(&t, resp, err, true)
	if t.Status == result.StatusPass {
		if !validStructured(generateText(resp.JSON)) {
			t.Status, t.Summary = result.StatusFail, "GenerateContent structured output violated the local JSON schema"
		} else {
			t.Summary = "GenerateContent structured output conforms to the local JSON schema"
		}
	}
	return []result.TestResult{t}
}

func runCountTokens(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) []result.TestResult {
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) {
		return client.CountTokens(ctx, model, generationBody(model, "Count this text."))
	})
	t := testBase(p, "google.tokens.count.basic")
	t.DurationMS, t.HTTP = resp.Duration.Milliseconds(), observation(resp)
	setHTTPStatus(&t, resp, err, true)
	if t.Status == result.StatusPass {
		count := numberValue(resp.JSON["totalTokens"])
		if count <= 0 {
			t.Status, t.Summary = result.StatusFail, "countTokens returned no positive totalTokens"
		} else {
			t.Metrics = map[string]any{"total_tokens": count}
			t.Summary = fmt.Sprintf("countTokens returned %d tokens", count)
		}
	}
	schema := derived(p, "google.tokens.count.schema", t)
	schema.Summary = "countTokens response contains a numeric totalTokens"
	return []result.TestResult{t, schema}
}

func runEmbedding(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) []result.TestResult {
	body := map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": "AIQA embedding probe"}}}}
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.EmbedContent(ctx, model, body) })
	t := testBase(p, "google.embeddings.basic")
	t.DurationMS, t.HTTP = resp.Duration.Milliseconds(), observation(resp)
	setHTTPStatus(&t, resp, err, true)
	if t.Status == result.StatusPass {
		if problems := ValidateEmbedding(resp.JSON); len(problems) > 0 {
			t.Status, t.Summary, t.Observation = result.StatusFail, "Embedding vector is invalid", strings.Join(problems, "; ")
		} else {
			t.Summary = "Embedding vector contains finite values"
		}
	}
	return []result.TestResult{t}
}

func runThinking(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string, legacy bool) []result.TestResult {
	body := generationBody(model, "Reply with exactly AIQA_OK.")
	body["generationConfig"] = map[string]any{"thinkingConfig": map[string]any{"thinkingBudget": 256}}
	var resp api.Response
	var err error
	probeID := "google.generate-content.thinking.basic"
	if legacy {
		resp, err = call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.GenerateContent(ctx, model, body) })
	} else {
		probeID = "google.interactions.thinking"
		interactionBody := map[string]any{"model": model, "input": "Reply with exactly AIQA_OK.", "store": false, "thinking": map[string]any{"budget": 256}}
		resp, err = call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Interaction(ctx, interactionBody) })
	}
	t := testBase(p, probeID)
	t.DurationMS, t.HTTP = resp.Duration.Milliseconds(), metadataObservation(resp)
	t.Usage = ParseUsage(resp.JSON)
	setHTTPStatus(&t, resp, err, true)
	if t.Status == result.StatusPass {
		t.Summary = "Thinking request was accepted; private thought content is not rendered"
	}
	return []result.TestResult{t}
}

func runInteractionStateful(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) []result.TestResult {
	created := testBase(p, "google.interactions.stateful")
	createBody := map[string]any{"model": model, "input": "Remember the token AIQA_STATEFUL_OK.", "store": true}
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Interaction(ctx, createBody) })
	created.DurationMS, created.HTTP = resp.Duration.Milliseconds(), metadataObservation(resp)
	created.Usage = ParseUsage(resp.JSON)
	setHTTPStatus(&created, resp, err, true)
	id := stringValue(resp.JSON["id"])
	if created.Status == result.StatusPass && id == "" {
		created.Status = result.StatusFail
		created.Summary = "Stateful Interaction succeeded without returning a resource id"
	} else if created.Status == result.StatusPass {
		created.Summary = "Stateful Interaction resource was created with explicit store=true"
		created.Metrics = map[string]any{"interaction_id_sha256_12": shortHash(id)}
	}
	cleanup := testBase(p, "google.interactions.stateful.cleanup")
	if id == "" {
		cleanup.Status = result.StatusSkip
		cleanup.Summary = "Cleanup was skipped because the create response had no resource id"
		return []result.TestResult{created, cleanup}
	}
	continuationBody := map[string]any{"model": model, "previous_interaction_id": id, "input": "What token did I ask you to remember?", "store": false}
	continuation, continuationErr := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Interaction(ctx, continuationBody) })
	if created.Status == result.StatusPass {
		if continuationErr != nil || !continuation.Is2xx() || !strings.Contains(interactionText(continuation.JSON), "AIQA_STATEFUL_OK") {
			created.Status = result.StatusFail
			created.Summary = "Stateful Interaction did not continue the stored context"
		}
	}
	cleanupResp, cleanupErr := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.DeleteInteraction(ctx, model, id) })
	cleanup.DurationMS = cleanupResp.Duration.Milliseconds()
	cleanup.HTTP = metadataObservation(cleanupResp)
	if cleanupErr != nil && cleanupResp.StatusCode == 0 {
		cleanup.Status, cleanup.Summary, cleanup.Error = result.StatusError, "Stateful Interaction cleanup could not be completed", cleanupErr.Error()
	} else if cleanupResp.Is2xx() || cleanupResp.StatusCode == 404 {
		// A 404 means the provider already expired/deleted the resource; it is
		// safe from AIQA's point of view and is explicitly recorded.
		cleanup.Status = result.StatusPass
		cleanup.Summary = "Every AIQA-created stateful resource was deleted or was already absent"
		cleanup.Metrics = map[string]any{"interaction_id_sha256_12": shortHash(id)}
	} else {
		cleanup.Status = result.StatusFail
		cleanup.Summary = "Stateful Interaction cleanup failed (P0 operational finding)"
		cleanup.Error = fmt.Sprintf("cleanup returned HTTP %d", cleanupResp.StatusCode)
		cleanup.Signals = []result.EvidenceSignal{{Code: "contract.google.interactions.stateful.cleanup", Parameters: map[string]any{"endpoint": "interactions/{id}", "status_code": cleanupResp.StatusCode}}}
	}
	return []result.TestResult{created, cleanup}
}

func runInteractionVision(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) []result.TestResult {
	body := map[string]any{"model": model, "input": []any{map[string]any{"type": "input_text", "text": "Reply with exactly AIQA_OK."}, map[string]any{"type": "input_image", "image_url": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="}}, "store": false}
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Interaction(ctx, body) })
	t := testBase(p, "google.interactions.vision")
	t.DurationMS, t.HTTP = resp.Duration.Milliseconds(), metadataObservation(resp)
	t.Usage = ParseUsage(resp.JSON)
	setHTTPStatus(&t, resp, err, true)
	if t.Status == result.StatusPass {
		t.Summary = "Interactions accepted bounded image input without retaining media"
	}
	return []result.TestResult{t}
}

func runGenerateVision(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) []result.TestResult {
	body := generationBody(model, "Reply with exactly AIQA_OK.")
	body["contents"] = []any{map[string]any{"role": "user", "parts": []any{map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="}}, map[string]any{"text": "Reply with exactly AIQA_OK."}}}}
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.GenerateContent(ctx, model, body) })
	t := testBase(p, "google.vision.image")
	t.DurationMS, t.HTTP = resp.Duration.Milliseconds(), metadataObservation(resp)
	t.Usage = ParseUsage(resp.JSON)
	setHTTPStatus(&t, resp, err, true)
	if t.Status == result.StatusPass {
		t.Summary = "GenerateContent accepted bounded image input without rendering media"
	}
	return []result.TestResult{t}
}

func runGoogleErrors(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string, legacy bool) []result.TestResult {
	missing := "aiqa-missing-model"
	var resp api.Response
	var err error
	if legacy {
		resp, err = call(parent, timeout, func(ctx context.Context) (api.Response, error) {
			return client.GenerateContent(ctx, missing, generationBody(missing, "AIQA error probe"))
		})
	} else {
		resp, err = call(parent, timeout, func(ctx context.Context) (api.Response, error) {
			return client.Interaction(ctx, map[string]any{"model": missing, "input": "AIQA error probe", "store": false})
		})
	}
	id := "google.error.invalid_model"
	t := testBase(p, id)
	t.DurationMS, t.HTTP = resp.Duration.Milliseconds(), observation(resp)
	setExpectedError(&t, resp, err)
	if t.Status == result.StatusPass {
		t.Summary = "Invalid model returned a structured Google API error"
	}
	shape := derived(p, "google.error.shape", t)
	if t.Status == result.StatusPass {
		if problems := ValidateError(resp.JSON); len(problems) > 0 {
			shape.Status, shape.Summary, shape.Observation = result.StatusFail, "Google error envelope is invalid", strings.Join(problems, "; ")
		} else {
			shape.Summary = "Google error envelope is valid"
		}
	}
	variants := [][]byte{[]byte(`{"model":`), []byte(`{"contents":[}`), []byte(`{"model":"` + model + `"} trailing`)}
	invalid := testBase(p, "google.error.invalid_json")
	invalid.Status = result.StatusPass
	var obs []string
	for index, raw := range variants {
		r, e := call(parent, timeout, func(ctx context.Context) (api.Response, error) {
			if legacy {
				return client.GenerateContentRaw(ctx, model, raw)
			}
			return client.InteractionRawForModel(ctx, raw, model)
		})
		invalid.DurationMS += r.Duration.Milliseconds()
		if index == 0 {
			invalid.HTTP = observation(r)
		}
		if e != nil && r.StatusCode == 0 {
			invalid.Status = result.StatusError
		} else if isTransientStatus(r.StatusCode) {
			invalid.Status = result.StatusError
		} else if r.StatusCode < 400 {
			invalid.Status = result.StatusFail
		}
		obs = append(obs, fmt.Sprintf("variant %d: HTTP %d", index+1, r.StatusCode))
	}
	invalid.Summary = "Three malformed JSON variants returned client errors"
	if invalid.Status != result.StatusPass {
		invalid.Summary = "Malformed JSON handling is not consistently client-side"
	}
	invalid.Observation = strings.Join(obs, "; ")
	return []result.TestResult{t, shape, invalid}
}

func generationBody(model, prompt string) map[string]any {
	return map[string]any{"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": prompt}}}}, "generationConfig": map[string]any{"maxOutputTokens": 32}}
}

func interactionText(root map[string]any) string {
	if value := stringValue(root["output_text"]); value != "" {
		return value
	}
	if value := stringValue(root["text"]); value != "" {
		return value
	}
	if steps, ok := root["steps"].([]any); ok {
		var out strings.Builder
		for _, raw := range steps {
			step, _ := raw.(map[string]any)
			kind := strings.ToLower(stringValue(step["type"]))
			if strings.Contains(kind, "thought") || kind == "analysis" || kind == "reasoning" {
				continue
			}
			if value := stringValue(step["text"]); value != "" {
				out.WriteString(value)
			}
			if value := stringValue(step["output_text"]); value != "" {
				out.WriteString(value)
			}
			if content, ok := step["content"].([]any); ok {
				for _, item := range content {
					part, _ := item.(map[string]any)
					if value := stringValue(part["text"]); value != "" {
						out.WriteString(value)
					}
				}
			}
		}
		return out.String()
	}
	return ""
}

func usageMap(root map[string]any) map[string]any {
	if usage, ok := root["usageMetadata"].(map[string]any); ok {
		return usage
	}
	if usage, ok := root["usage"].(map[string]any); ok {
		return usage
	}
	return nil
}

func interactionEligible(requested []string, p profile.Profile, model string) bool {
	for _, explicit := range requested {
		if NormalizeModelID(explicit) == NormalizeModelID(model) {
			return true
		}
	}
	for _, allowed := range p.InteractionModels() {
		allowed = NormalizeModelID(allowed)
		if strings.HasSuffix(allowed, "*") && strings.HasPrefix(NormalizeModelID(model), strings.TrimSuffix(allowed, "*")) {
			return true
		}
		if allowed == NormalizeModelID(model) {
			return true
		}
	}
	return false
}

func uniqueEvidence(in []capability.Evidence) []capability.Evidence {
	seen := map[string]bool{}
	out := make([]capability.Evidence, 0, len(in))
	for _, item := range in {
		key := item.Capability + "\x00" + item.Rule
		if !seen[key] {
			seen[key] = true
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Capability < out[j].Capability })
	return out
}

func hasModule(modules []result.ModuleAssessment, id string) bool {
	module := capability.Module(modules, id)
	return module != nil && module.Execution == result.ExecutionExecuted
}

func testBase(p profile.Profile, id string) result.TestResult {
	spec := p.Probe(id)
	return result.TestResult{ID: id, RequirementID: id, ProbeID: "aiqa.native." + id, Name: spec.Name, Suite: "compat", Category: spec.Category, Required: spec.Required, Expected: spec.Description}
}

func derived(p profile.Profile, id string, source result.TestResult) result.TestResult {
	t := testBase(p, id)
	t.DurationMS, t.HTTP = source.DurationMS, source.HTTP
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
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	return fn(ctx)
}

func callStream(parent context.Context, timeout time.Duration, fn func(context.Context) (api.StreamResponse, error)) (api.StreamResponse, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	return fn(ctx)
}

func setHTTPStatus(t *result.TestResult, resp api.Response, err error, optional bool) {
	if err != nil && resp.StatusCode == 0 {
		t.Status, t.Summary, t.Error = result.StatusError, "Request could not be completed", err.Error()
		return
	}
	if isTransientStatus(resp.StatusCode) {
		t.Status, t.Summary = result.StatusError, fmt.Sprintf("Provider/runtime error HTTP %d makes compatibility inconclusive", resp.StatusCode)
		return
	}
	if resp.Is2xx() {
		if err != nil {
			t.Status, t.Summary, t.Error = result.StatusError, "Successful HTTP response could not be parsed", err.Error()
			return
		}
		t.Status = result.StatusPass
		return
	}
	if optional && (resp.StatusCode == 400 || resp.StatusCode == 404 || resp.StatusCode == 422) {
		t.Status, t.Summary = result.StatusUnsupported, fmt.Sprintf("Optional capability is not supported (HTTP %d)", resp.StatusCode)
		return
	}
	t.Status, t.Summary = result.StatusFail, fmt.Sprintf("Request returned HTTP %d", resp.StatusCode)
	if err != nil {
		t.Error = err.Error()
	}
}

func setExpectedError(t *result.TestResult, resp api.Response, err error) {
	if err != nil && resp.StatusCode == 0 {
		t.Status, t.Summary, t.Error = result.StatusError, "Error probe could not be completed", err.Error()
		return
	}
	if isTransientStatus(resp.StatusCode) {
		t.Status, t.Summary = result.StatusError, "Provider/runtime error makes the negative probe inconclusive"
		return
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		if len(ValidateError(resp.JSON)) == 0 {
			t.Status = result.StatusPass
		} else {
			t.Status, t.Summary = result.StatusFail, "Client error does not use the Google error shape"
		}
		return
	}
	t.Status, t.Summary = result.StatusFail, fmt.Sprintf("Invalid request was not rejected with a client error (HTTP %d)", resp.StatusCode)
}

func isTransientStatus(status int) bool { return status == 408 || status == 429 || status >= 500 }

func observation(resp api.Response) result.HTTPObservation {
	// Google response bodies can contain model output, thought parts, tool
	// arguments, or inline media.  Keep the report structural; summaries,
	// status, usage, and validators carry the compatibility signal without
	// copying provider-generated content into evidence.
	return result.HTTPObservation{StatusCode: resp.StatusCode, ContentType: resp.ContentType, RequestID: resp.RequestID}
}
func metadataObservation(resp api.Response) result.HTTPObservation {
	return result.HTTPObservation{StatusCode: resp.StatusCode, ContentType: resp.ContentType, RequestID: resp.RequestID}
}
func streamObservation(resp api.StreamResponse) result.HTTPObservation {
	// Stream snippets contain provider-generated text and can include private
	// thought content or echoed media.  Keep Google stream evidence structural;
	// parsed marker/usage assertions above carry the compatibility signal without
	// rendering raw model output into reports.
	return result.HTTPObservation{StatusCode: resp.StatusCode, ContentType: resp.ContentType, RequestID: resp.RequestID}
}
func setStreamError(t *result.TestResult, err error, status int, optional bool) {
	t.Status, t.Summary, t.Error = result.StatusError, "Google stream could not be evaluated", err.Error()
	// A read/parse error after a successful response is an execution error,
	// not a protocol assertion failure.  Only an actual non-success HTTP
	// response can be classified as FAIL or (for an optional capability) UNSUP.
	if status == 0 || status >= 200 && status < 300 || isTransientStatus(status) {
		return
	}
	if optional && isOptionalRejection(status) {
		t.Status, t.Summary = result.StatusUnsupported, fmt.Sprintf("Optional stream capability is not supported (HTTP %d)", status)
	} else {
		t.Status = result.StatusFail
	}
}

func setStreamHTTPStatus(t *result.TestResult, status int, optional bool, summary string) {
	if isTransientStatus(status) {
		t.Status, t.Summary = result.StatusError, fmt.Sprintf("Provider/runtime error HTTP %d makes streaming compatibility inconclusive", status)
		return
	}
	if optional && isOptionalRejection(status) {
		t.Status, t.Summary = result.StatusUnsupported, fmt.Sprintf("Optional stream capability is not supported (HTTP %d)", status)
		return
	}
	t.Status, t.Summary = result.StatusFail, summary
}

func isOptionalRejection(status int) bool {
	return status == http.StatusBadRequest || status == http.StatusNotFound || status == http.StatusUnprocessableEntity
}
func profileInfo(p profile.Profile) result.ProfileInfo {
	return result.ProfileInfo{SchemaVersion: p.SchemaVersion, ID: p.ID, Protocol: p.Protocol, Revision: p.Revision, APIVersion: p.APIVersion, BaseURLSemantics: p.BaseURLSemantics, Source: p.Source, SourceCommit: p.SourceCommit, SourceRefRevision: p.SourceRefRevision, Scope: p.Scope}
}
func toTarget(target config.TargetConfig, r result.Report) result.TargetReport {
	return result.TargetReport{ID: target.ID, Protocol: string(target.Protocol), Profile: r.Profile, Discovery: r.Discovery, Models: r.Models, Requirements: r.Requirements, Summary: r.Summary, Guidance: r.Guidance, RateLimit: r.RateLimit}
}
func selectModels(requested []string, discovered []api.Model) ([]string, map[string]map[string]any) {
	return protocolpkg.SelectModels(requested, discovered, NormalizeModelID, googleInventoryTrusted)
}

func requestedModels(cfg config.Config, target config.TargetConfig) []string {
	if len(cfg.Models) > 0 {
		return append([]string(nil), cfg.Models...)
	}
	return append([]string(nil), target.Models...)
}

func googleInventoryTrusted(metadata map[string]any) bool {
	if metadata == nil {
		return false
	}
	name, _ := metadata["name"].(string)
	return strings.HasPrefix(strings.TrimSpace(name), "models/") && NormalizeModelID(name) != ""
}

func isTransientResponse(resp api.Response, err error) bool {
	return (err != nil && resp.StatusCode == 0) || isTransientStatus(resp.StatusCode)
}

func isModelRouteRejection(resp api.Response) bool {
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
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
func hasProblem(problems []string, fragment string) bool {
	for _, problem := range problems {
		if strings.Contains(problem, fragment) {
			return true
		}
	}
	return false
}

func validStructured(text string) bool {
	var value any
	if json.Unmarshal([]byte(text), &value) != nil {
		return false
	}
	return validateJSONSchema(value, map[string]any{
		"type":                 "object",
		"required":             []any{"ok", "value"},
		"additionalProperties": false,
		"properties": map[string]any{
			"ok":    map[string]any{"type": "boolean"},
			"value": map[string]any{"type": "string", "const": "AIQA_OK"},
		},
	})
}

// ValidateStructuredJSON exposes the profile's local structured-output
// assertion to the Official SDK binding without sharing provider response
// parsers or trusting a remote schema claim.
func ValidateStructuredJSON(text string) bool {
	return validStructured(text)
}

// validateJSONSchema is intentionally small and deterministic: structured
// probes use a closed local schema (object, required fields and scalar types)
// rather than trusting a provider's 2xx response or reusing its parser.
func validateJSONSchema(value any, schema map[string]any) bool {
	if schema == nil {
		return false
	}
	if expected, ok := schema["const"]; ok && fmt.Sprint(expected) != fmt.Sprint(value) {
		return false
	}
	switch schema["type"] {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return false
		}
		properties, _ := schema["properties"].(map[string]any)
		for _, raw := range stringValues(schema["required"]) {
			if _, exists := object[raw]; !exists {
				return false
			}
		}
		if additional, ok := schema["additionalProperties"].(bool); ok && !additional {
			for key := range object {
				if _, known := properties[key]; !known {
					return false
				}
			}
		}
		for key, child := range properties {
			if raw, exists := object[key]; exists {
				childSchema, ok := child.(map[string]any)
				if !ok || !validateJSONSchema(raw, childSchema) {
					return false
				}
			}
		}
		return true
	case "array":
		items, ok := value.([]any)
		if !ok {
			return false
		}
		child, _ := schema["items"].(map[string]any)
		for _, item := range items {
			if child != nil && !validateJSONSchema(item, child) {
				return false
			}
		}
		return true
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number":
		_, ok := numberFloat(value)
		return ok
	case "integer":
		n, ok := numberFloat(value)
		return ok && n == float64(int64(n))
	default:
		return false
	}
}

func nonNegativeMetadataInteger(value any) (int64, bool) {
	if value == nil {
		return 0, false
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return 0, false
	}
	var number json.Number
	if err := json.Unmarshal(encoded, &number); err != nil {
		return 0, false
	}
	parsed, err := number.Int64()
	return parsed, err == nil
}

func metadataBool(value any) (bool, bool) {
	boolean, ok := value.(bool)
	return boolean, ok
}

func interactionToolCall(root map[string]any) (name, args string, found bool) {
	var visit func(any)
	visit = func(value any) {
		if found {
			return
		}
		switch item := value.(type) {
		case map[string]any:
			kind := strings.ToLower(stringValue(item["type"]))
			for _, key := range []string{"function_call", "functionCall", "tool_call", "toolCall"} {
				if call, ok := item[key].(map[string]any); ok {
					if name == "" {
						name = stringValue(call["name"])
					}
					if args == "" {
						args = stringValue(call["arguments"])
						if args == "" {
							args = stringValue(call["input"])
						}
					}
					if name != "" {
						found = true
						return
					}
				}
			}
			if kind == "function_call" || kind == "functioncall" || kind == "tool_call" {
				name = stringValue(item["name"])
				if name == "" {
					name = stringValue(item["function"])
				}
				args = stringValue(item["arguments"])
				if args == "" {
					args = stringValue(item["input"])
				}
				found = name != ""
				if found {
					return
				}
			}
			for _, child := range item {
				visit(child)
			}
		case []any:
			for _, child := range item {
				visit(child)
			}
		}
	}
	visit(root)
	return name, args, found
}

func generateToolCall(root map[string]any) (name, args string, found bool) {
	var visit func(any)
	visit = func(value any) {
		if found {
			return
		}
		switch item := value.(type) {
		case map[string]any:
			if call, ok := item["functionCall"].(map[string]any); ok {
				name = stringValue(call["name"])
				if encoded, ok := call["args"].(map[string]any); ok {
					data, _ := json.Marshal(encoded)
					args = string(data)
				} else {
					args = stringValue(call["args"])
				}
				found = name != ""
				if found {
					return
				}
			}
			for _, child := range item {
				visit(child)
			}
		case []any:
			for _, child := range item {
				visit(child)
			}
		}
	}
	visit(root)
	return name, args, found
}

func shortHash(value string) string {
	// A bounded digest is sufficient for report identity; resource identifiers
	// themselves must never be written to report output.
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest[:6])
}
