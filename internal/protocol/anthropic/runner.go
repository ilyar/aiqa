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

// Runtime exposes the target-owned transport and executor to the extension
// phase. Native and extension probes therefore share one adaptive pacer and
// one global concurrency budget for the target.
type Runtime struct {
	Client   *api.Client
	Executor *probe.Executor
}

// RunTarget adapts the Anthropic implementation to the shared protocol
// registry contract.
func RunTarget(ctx context.Context, options protocolpkg.RunOptions) (protocolpkg.RunResult, error) {
	runtime := &Runtime{}
	target, err := Run(ctx, RunOptions{
		Target: options.Target, Config: options.Config, Profile: options.Profile,
		UserAgent: options.UserAgent, Logf: options.Logf, Runtime: runtime,
	})
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
	for model, values := range overrides {
		for _, value := range values {
			if !strings.HasPrefix(value, "anthropic.") {
				return result.TargetReport{}, fmt.Errorf("Anthropic capability file assigns non-Anthropic capability %q to %q", value, model)
			}
		}
	}
	for _, raw := range target.ModelCapabilities {
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 {
			return result.TargetReport{}, fmt.Errorf("invalid --anthropic-model-capability %q", raw)
		}
		values := strings.Split(parts[1], "+")
		for index := range values {
			values[index] = strings.TrimSpace(values[index])
			if !capability.Known[values[index]] || !strings.HasPrefix(values[index], "anthropic.") {
				return result.TargetReport{}, fmt.Errorf("unknown or non-Anthropic capability %q", values[index])
			}
		}
		overrides[strings.TrimSpace(parts[0])] = values
	}
	client, err := NewClient(target.BaseURL, target.APIKey, target.APIVersion, options.UserAgent, target.RequestInterval)
	if err != nil {
		return result.TargetReport{}, err
	}
	client.SetConcurrency(cfg.Concurrency)
	probeExecutor := probe.NewExecutor(cfg.Concurrency)
	if options.Runtime != nil {
		options.Runtime.Client = client.TransportClient()
		options.Runtime.Executor = probeExecutor
	}
	tmp := result.Report{SchemaVersion: "aiqa.report.internal", Profile: profileInfo(p), Config: result.PublicConfig{BaseURL: target.BaseURL, Tests: cfg.Tests, RequestedModels: target.Models, Timeout: cfg.Timeout.String(), Formats: cfg.Formats, ExtSuites: cfg.ExtSuites, FailOn: cfg.FailOn, Guidance: cfg.Guidance, GuidancePack: cfg.GuidancePack, GuidanceLocale: cfg.GuidanceLocale}}

	listCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	models, listResp, listErr := client.ListModels(listCtx)
	cancel()
	tmp.Discovery.ModelCount = len(models)
	for _, model := range models {
		tmp.Discovery.Models = append(tmp.Discovery.Models, cloneMap(model.Raw))
	}
	if cfg.HasTest("compat") {
		tmp.Discovery.Tests = append(tmp.Discovery.Tests, discoveryResults(p, listResp, models, listErr)...)
		tmp.Discovery.Tests = append(tmp.Discovery.Tests, paginationResult(ctx, client, cfg.Timeout, p, listResp, models, listErr))
	}
	selected, metadata := selectModels(target.Models, models)
	if len(selected) == 0 {
		reportpkg.Finalize(&tmp)
		guidance.ExtractArtifacts(&tmp)
		guidance.QualifyArtifacts(&tmp, target.ID)
		assessment.ApplyTarget(&tmp, p, target.ID, string(target.Protocol))
		return toTarget(target, tmp), fmt.Errorf("Anthropic target returned no models; set --anthropic-model when /v1/models is unavailable")
	}
	if options.Logf != nil {
		options.Logf("[%s] testing %d model(s): %s", target.ID, len(selected), strings.Join(selected, ", "))
		if containsString(cfg.Modules, "anthropic.thinking") {
			options.Logf("[%s] high-token thinking probes are enabled and may consume at least 4096 output-token budget per model", target.ID)
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
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				model := selected[index]
				if options.Logf != nil {
					options.Logf("[%s/%s] start", target.ID, model)
				}
				classification := capability.ClassifyAnthropic(model, metadata[model], capability.Options{BaseURL: target.BaseURL, Overrides: overrides, UnknownPolicy: target.UnknownModel})
				modules := capability.AssessModules(classification, p.Modules, capability.RoutingOptions{Modules: target.Modules, AllowBillableArtifacts: cfg.AllowBillableArtifacts, AllowHighTokenProbes: cfg.AllowHighTokenProbes})
				mr := result.ModelReport{ID: model, Metadata: cloneMap(metadata[model]), Modules: modules}
				for _, evidence := range classification.Capabilities {
					mr.Capabilities = append(mr.Capabilities, result.CapabilityAssessment{ID: evidence.Capability, Source: evidence.Source, Rule: evidence.Rule, Confidence: evidence.Confidence, Reason: evidence.Reason})
				}
				if cfg.HasTest("compat") {
					mr.Compatibility = runModel(ctx, client, cfg, p, model, modules, probeExecutor)
				}
				sort.Slice(mr.Compatibility, func(i, j int) bool { return mr.Compatibility[i].ID < mr.Compatibility[j].ID })
				if len(selected) > 1 {
					stats := client.PacingStatsForModel(model)
					mr.RateLimit = &result.RateLimitStats{SuccessfulRequests: stats.SuccessfulRequests, RateLimited: stats.RateLimited, RetriesRecovered: stats.RetriesRecovered, BackoffMS: stats.Backoff.Milliseconds(), SafeRate: stats.SafeRate, SafeConcurrency: stats.SafeConcurrency}
					addModelLatency(mr.RateLimit, mr)
				}
				if options.Logf != nil {
					options.Logf("[%s/%s] done", target.ID, model)
				}
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
	tmp.RateLimit = result.RateLimitStats{SuccessfulRequests: stats.SuccessfulRequests, RateLimited: stats.RateLimited, RetriesRecovered: stats.RetriesRecovered, BackoffMS: stats.Backoff.Milliseconds(), SafeRate: stats.SafeRate, SafeConcurrency: stats.SafeConcurrency}
	addLatency(&tmp.RateLimit, tmp)
	reportpkg.Finalize(&tmp)
	guidance.ExtractArtifacts(&tmp)
	guidance.QualifyArtifacts(&tmp, target.ID)
	assessment.ApplyTarget(&tmp, p, target.ID, string(target.Protocol))
	return toTarget(target, tmp), nil
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
		schema.Status = httpTest.Status
		schema.Summary = "Model schema could not be evaluated"
		schema.Error = httpTest.Error
	} else if problems := ValidateModels(resp.JSON); len(problems) > 0 {
		schema.Status = result.StatusFail
		schema.Summary = "Anthropic model-list schema is invalid"
		schema.Observation = strings.Join(problems, "; ")
		schema.Signals = []result.EvidenceSignal{{Code: "anthropic.models.list.invalid_envelope"}}
	} else {
		schema.Status = result.StatusPass
		schema.Summary = fmt.Sprintf("Anthropic model-list schema is valid (%d models)", len(models))
	}
	return []result.TestResult{httpTest, schema}
}

func paginationResult(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, first api.Response, models []api.Model, listErr error) result.TestResult {
	test := testBase(p, "anthropic.models.list.pagination")
	if listErr != nil || !first.Is2xx() {
		test.Status = result.StatusSkip
		test.Summary = "Pagination could not be evaluated because model discovery failed"
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
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	next, resp, err := client.ListModelsPage(ctx, lastID, 1)
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
	timeout := cfg.Timeout
	var out []result.TestResult
	retrieve := probe.Map(executor, func() result.TestResult {
		retrieveResp, retrieveErr := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.RetrieveModel(ctx, model) })
		test := testBase(p, "anthropic.models.retrieve")
		test.DurationMS, test.HTTP = retrieveResp.Duration.Milliseconds(), observation(retrieveResp)
		setHTTPStatus(&test, retrieveResp, retrieveErr, true)
		if test.Status == result.StatusPass && stringValue(retrieveResp.JSON["id"]) != model {
			test.Status, test.Summary = result.StatusFail, "Retrieved model ID does not match the request"
		} else if test.Status == result.StatusPass {
			test.Summary = "Retrieved model ID matches the request"
		}
		return test
	})[0]
	out = append(out, retrieve)
	if !moduleExecuted(modules, "anthropic.messages") {
		return append(out, flattenProbeGroups(probe.Map(executor, optionalModelTasks(parent, client, cfg, p, model, modules, 0)...))...)
	}
	body := basicBody(model)
	type basicPhase struct {
		response api.Response
		test     result.TestResult
	}
	phase := probe.Map(executor, func() basicPhase {
		resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, body) })
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
		return basicPhase{response: resp, test: basic}
	})[0]
	resp, basic := phase.response, phase.test
	out = append(out, basic)
	schema := derived(p, "anthropic.messages.schema", basic)
	if basic.Status == result.StatusPass {
		if problems := ValidateMessage(resp.JSON); len(problems) > 0 {
			schema.Status = result.StatusFail
			schema.Summary = "Anthropic Message schema is invalid"
			schema.Observation = strings.Join(problems, "; ")
			schema.Signals = []result.EvidenceSignal{{Code: "anthropic.message.invalid_envelope"}}
		} else {
			schema.Status = result.StatusPass
			schema.Summary = "Anthropic Message schema is valid"
		}
	}
	out = append(out, schema)
	usage := derived(p, "anthropic.messages.usage", basic)
	usage.Usage = ParseUsage(resp.JSON)
	if basic.Status == result.StatusPass {
		if problems := ValidateUsage(usage.Usage); len(problems) > 0 {
			usage.Status = result.StatusFail
			usage.Summary = "Anthropic usage is invalid"
			usage.Observation = strings.Join(problems, "; ")
		} else {
			usage.Status = result.StatusPass
			usage.Summary = "Anthropic input/output usage is valid"
		}
	}
	out = append(out, usage)
	accepted := derived(p, "anthropic.header.version.accepted", basic)
	accepted.Summary = "anthropic-version 2023-06-01 was accepted"
	if accepted.Status != result.StatusPass {
		accepted.Signals = []result.EvidenceSignal{{Code: "anthropic.header.version.rejected"}}
	}
	out = append(out, accepted)
	auth := derived(p, "anthropic.auth.x_api_key", basic)
	auth.Summary = "x-api-key authentication was accepted"
	if auth.Status != result.StatusPass {
		auth.Signals = []result.EvidenceSignal{{Code: "anthropic.auth.x_api_key.rejected"}}
	}
	out = append(out, auth)
	stringContent := derived(p, "anthropic.messages.content.string", basic)
	stringContent.Summary = "String-form message content is accepted"
	out = append(out, stringContent)

	tasks := []func() []result.TestResult{
		func() []result.TestResult {
			test := credentialProbe(parent, client, timeout, p, "anthropic.header.version.required", body, client.apiKey, "")
			if test.Status != result.StatusPass {
				test.Signals = []result.EvidenceSignal{{Code: "anthropic.header.version.missing"}}
			}
			return []result.TestResult{test}
		},
		func() []result.TestResult {
			return []result.TestResult{credentialProbe(parent, client, timeout, p, "anthropic.header.version.invalid", body, client.apiKey, "1900-01-01")}
		},
		func() []result.TestResult {
			return []result.TestResult{credentialProbe(parent, client, timeout, p, "anthropic.error.invalid_auth", body, "aiqa-invalid-key", StableAPIVersion)}
		},
		func() []result.TestResult {
			blocksBody := basicBody(model)
			blocksBody["messages"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "Reply with exactly AIQA_OK."}}}}
			return []result.TestResult{simpleMarkerProbe(parent, client, timeout, p, "anthropic.messages.content.text_blocks", blocksBody)}
		},
		func() []result.TestResult {
			systemBody := basicBody(model)
			systemBody["system"] = "Always answer with exactly AIQA_OK."
			return []result.TestResult{simpleMarkerProbe(parent, client, timeout, p, "anthropic.messages.system.top_level", systemBody)}
		},
		func() []result.TestResult {
			multiBody := basicBody(model)
			multiBody["messages"] = []any{map[string]any{"role": "user", "content": "Remember AIQA_OK."}, map[string]any{"role": "assistant", "content": "I will remember it."}, map[string]any{"role": "user", "content": "Reply with only the remembered marker."}}
			return []result.TestResult{simpleMarkerProbe(parent, client, timeout, p, "anthropic.messages.multi_turn", multiBody)}
		},
		func() []result.TestResult {
			return []result.TestResult{runAssistantPrefill(parent, client, timeout, p, model)}
		},
		func() []result.TestResult {
			return []result.TestResult{runStopSequence(parent, client, timeout, p, model)}
		},
		func() []result.TestResult {
			return []result.TestResult{runMaxTokens(parent, client, timeout, p, model)}
		},
		func() []result.TestResult {
			metadataBody := basicBody(model)
			metadataBody["metadata"] = map[string]any{"user_id": "aiqa-compat-probe"}
			return []result.TestResult{simpleMarkerProbe(parent, client, timeout, p, "anthropic.messages.metadata", metadataBody)}
		},
		func() []result.TestResult { return runErrors(parent, client, timeout, p, model) },
		func() []result.TestResult { return runStream(parent, client, timeout, p, model) },
	}
	tasks = append(tasks, optionalModelTasks(parent, client, cfg, p, model, modules, usage.Usage.InputTokens)...)
	return append(out, flattenProbeGroups(probe.Map(executor, tasks...))...)
}

func optionalModelTasks(parent context.Context, client *Client, cfg config.Config, p profile.Profile, model string, modules []result.ModuleAssessment, observedInputTokens int64) []func() []result.TestResult {
	var tasks []func() []result.TestResult
	if moduleExecuted(modules, "anthropic.tools") {
		tasks = append(tasks, func() []result.TestResult { return runTools(parent, client, cfg.Timeout, p, model) })
	}
	if moduleExecuted(modules, "anthropic.token-counting") {
		tasks = append(tasks, func() []result.TestResult {
			return runCountTokens(parent, client, cfg.Timeout, p, model, observedInputTokens)
		})
	}
	if moduleExecuted(modules, "anthropic.structured-output") {
		tasks = append(tasks, func() []result.TestResult { return runStructured(parent, client, cfg.Timeout, p, model) })
	}
	if moduleExecuted(modules, "anthropic.vision") {
		tasks = append(tasks, func() []result.TestResult { return runVision(parent, client, cfg.Timeout, p, model) })
	}
	if moduleExecuted(modules, "anthropic.thinking") && cfg.AllowHighTokenProbes {
		tasks = append(tasks, func() []result.TestResult { return runThinking(parent, client, cfg.Timeout, p, model) })
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

func runErrors(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) []result.TestResult {
	missing := "aiqa-missing-" + fmt.Sprint(time.Now().UnixNano())
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) {
		b := basicBody(missing)
		return client.Message(ctx, b)
	})
	invalidModel := testBase(p, "anthropic.error.invalid_model")
	invalidModel.DurationMS = resp.Duration.Milliseconds()
	invalidModel.HTTP = observation(resp)
	invalidModel.Usage = ParseUsage(resp.JSON)
	setExpectedError(&invalidModel, resp, err)
	if invalidModel.Status == result.StatusPass {
		invalidModel.Summary = "Invalid model returned a client-side Anthropic error"
	}
	shape := derived(p, "anthropic.error.shape", invalidModel)
	if invalidModel.Status == result.StatusPass {
		if problems := ValidateError(resp.JSON); len(problems) > 0 {
			shape.Status = result.StatusFail
			shape.Summary = "Error envelope is not Anthropic-compatible"
			shape.Observation = strings.Join(problems, "; ")
		} else {
			shape.Status = result.StatusPass
			shape.Summary = "Error envelope is Anthropic-compatible"
		}
	}
	requestID := derived(p, "anthropic.error.request_id", invalidModel)
	if invalidModel.Status == result.StatusPass {
		if resp.RequestID != "" || stringValue(resp.JSON["request_id"]) != "" {
			requestID.Status = result.StatusPass
			requestID.Summary = "Error exposes a request ID"
		} else {
			requestID.Status = result.StatusWarn
			requestID.Summary = "Error did not expose a request ID"
		}
	}
	variants := [][]byte{[]byte(`{"model":`), []byte(`{"messages":[}`), []byte(`{"model":"` + model + `"} trailing`)}
	invalidJSON := testBase(p, "anthropic.error.invalid_json")
	invalidJSON.Status = result.StatusPass
	var observations []string
	for i, raw := range variants {
		r, e := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.MessageRaw(ctx, raw) })
		invalidJSON.DurationMS += r.Duration.Milliseconds()
		invalidJSON.Usage = invalidJSON.Usage.Add(ParseUsage(r.JSON))
		if i == 0 {
			invalidJSON.HTTP = observation(r)
		}
		if e != nil && r.StatusCode == 0 {
			invalidJSON.Status = result.StatusError
			observations = append(observations, e.Error())
			continue
		}
		if isTransientStatus(r.StatusCode) {
			invalidJSON.Status = result.StatusError
		} else if r.StatusCode < 400 {
			invalidJSON.Status = result.StatusFail
		}
		observations = append(observations, fmt.Sprintf("variant %d: HTTP %d", i+1, r.StatusCode))
	}
	invalidJSON.Summary = "Three malformed JSON variants returned client errors"
	if invalidJSON.Status != result.StatusPass {
		invalidJSON.Summary = "Malformed JSON handling is not consistently client-side"
	}
	invalidJSON.Observation = strings.Join(observations, "; ")
	wrong := basicBody(model)
	wrong["max_tokens"] = "32"
	r, e := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, wrong) })
	invalidTypes := testBase(p, "anthropic.error.invalid_types")
	invalidTypes.DurationMS = r.Duration.Milliseconds()
	invalidTypes.HTTP = observation(r)
	invalidTypes.Usage = ParseUsage(r.JSON)
	setExpectedError(&invalidTypes, r, e)
	invalidTypes.Summary = "Invalid field types are rejected with a client error"
	return []result.TestResult{invalidModel, shape, requestID, invalidJSON, invalidTypes}
}

func runStream(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) []result.TestResult {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	stream, err := client.StreamMessage(ctx, basicBody(model))
	validation := ValidateStream(stream)
	base := testBase(p, "anthropic.messages.stream.http")
	base.DurationMS = stream.Duration.Milliseconds()
	base.HTTP = result.HTTPObservation{StatusCode: stream.StatusCode, ContentType: stream.ContentType, RequestID: stream.RequestID, Snippet: truncate(stream.RawSnippet, 1000)}
	if err != nil {
		if isTransientStatus(stream.StatusCode) {
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
	sequence := derived(p, "anthropic.messages.stream.sequence", base)
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
	if validation.SawDoneSentinel {
		noDone.Status = result.StatusFail
		noDone.Summary = "Stream emitted the OpenAI [DONE] sentinel"
	} else {
		noDone.Summary = "Stream ended with message_stop and no [DONE] sentinel"
	}
	return []result.TestResult{base, sequence, typeMatch, textProbe, usage, noDone}
}

func runTools(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) []result.TestResult {
	tool := map[string]any{"name": "get_status", "description": "Return status for a resource.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"resource_id": map[string]any{"type": "string"}}, "required": []any{"resource_id"}, "additionalProperties": false}}
	auto := runToolChoiceProbe(parent, client, timeout, p, model, "anthropic.tools.auto", tool, map[string]any{"type": "auto"}, false)
	anyProbe := runToolChoiceProbe(parent, client, timeout, p, model, "anthropic.tools.any", tool, map[string]any{"type": "any"}, true)
	none := runToolChoiceProbe(parent, client, timeout, p, model, "anthropic.tools.none", tool, map[string]any{"type": "none"}, false)
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
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, body) })
	specific := testBase(p, "anthropic.tools.specific")
	specific.DurationMS = resp.Duration.Milliseconds()
	specific.HTTP = observation(resp)
	specific.Usage = ParseUsage(resp.JSON)
	setHTTPStatus(&specific, resp, err, true)
	uses := ToolUses(resp.JSON)
	if specific.Status == result.StatusPass {
		if len(uses) == 0 || stringValue(uses[0]["name"]) != "get_status" {
			specific.Status = result.StatusFail
			specific.Summary = "Forced tool did not return get_status"
		} else {
			specific.Summary = "Forced named tool returned a valid tool_use block"
		}
	}
	inputSchema := derived(p, "anthropic.tools.input_schema", specific)
	if len(uses) > 0 {
		input, _ := uses[0]["input"].(map[string]any)
		if stringValue(input["resource_id"]) == "" {
			inputSchema.Status = result.StatusFail
			inputSchema.Summary = "Tool input violates its required schema"
		} else {
			inputSchema.Summary = "Tool input conforms to the requested schema"
		}
	}
	continuation := testBase(p, "anthropic.tools.continuation")
	if len(uses) == 0 {
		continuation.Status = result.StatusSkip
		continuation.Summary = "Continuation blocked because no tool_use was returned"
		continuation.Signals = []result.EvidenceSignal{{Code: "anthropic.tools.result_roundtrip_failed", Parameters: map[string]any{"blocked_by_requirement": "anthropic.tools.specific"}}}
	} else {
		id := stringValue(uses[0]["id"])
		cont := basicBody(model)
		cont["messages"] = []any{body["messages"].([]any)[0], map[string]any{"role": "assistant", "content": resp.JSON["content"]}, map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": "resource-alpha is available"}}}}
		r, e := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, cont) })
		continuation.DurationMS = r.Duration.Milliseconds()
		continuation.HTTP = observation(r)
		continuation.Usage = ParseUsage(r.JSON)
		setHTTPStatus(&continuation, r, e, true)
		if continuation.Status == result.StatusPass && strings.Contains(strings.ToLower(Text(r.JSON)), "available") {
			continuation.Summary = "tool_result was accepted and used in the final answer"
		} else if continuation.Status == result.StatusPass {
			continuation.Status = result.StatusWarn
			continuation.Summary = "tool_result was accepted but semantic use was not demonstrated"
		}
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	stream, streamErr := client.StreamMessage(ctx, body)
	cancel()
	streamProbe := testBase(p, "anthropic.tools.stream")
	streamProbe.DurationMS = stream.Duration.Milliseconds()
	streamProbe.HTTP = result.HTTPObservation{StatusCode: stream.StatusCode, ContentType: stream.ContentType, RequestID: stream.RequestID, Snippet: truncate(stream.RawSnippet, 1000)}
	validation := ValidateStream(stream)
	streamProbe.Usage = validation.Usage
	if streamErr != nil {
		if stream.StatusCode == 400 || stream.StatusCode == 404 || stream.StatusCode == 422 {
			streamProbe.Status = result.StatusUnsupported
		} else if isTransientStatus(stream.StatusCode) {
			streamProbe.Status = result.StatusError
		} else {
			streamProbe.Status = result.StatusFail
		}
		streamProbe.Summary = "Streaming tool call could not be evaluated"
		streamProbe.Error = streamErr.Error()
	} else if validation.ProviderError {
		streamProbe.Status = result.StatusError
		streamProbe.Summary = "Provider emitted a stream error during tool use"
	} else if len(validation.Problems) > 0 {
		streamProbe.Status = result.StatusFail
		streamProbe.Summary = "Streaming tool input is invalid"
		streamProbe.Observation = strings.Join(validation.Problems, "; ")
	} else if validation.Incomplete {
		streamProbe.Status = result.StatusError
		streamProbe.Summary = "Streaming tool call ended before message_stop"
	} else if len(validation.ToolInputs) == 0 {
		streamProbe.Status = result.StatusFail
		streamProbe.Summary = "Stream contained no tool input deltas"
	} else {
		streamProbe.Status = result.StatusPass
		streamProbe.Summary = "Streamed tool input reconstructed as valid JSON"
	}
	parallelTool := map[string]any{"name": "get_owner", "description": "Return an owner.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"resource_id": map[string]any{"type": "string"}}, "required": []any{"resource_id"}, "additionalProperties": false}}
	parallelBody := basicBody(model)
	parallelBody["tools"] = []any{tool, parallelTool}
	parallelBody["tool_choice"] = map[string]any{"type": "any", "disable_parallel_tool_use": false}
	parallelBody["messages"] = []any{map[string]any{"role": "user", "content": "Call get_status and get_owner for resource-alpha before answering."}}
	parallelResp, parallelErr := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, parallelBody) })
	parallel := testBase(p, "anthropic.tools.parallel")
	parallel.DurationMS, parallel.HTTP = parallelResp.Duration.Milliseconds(), observation(parallelResp)
	parallel.Usage = ParseUsage(parallelResp.JSON)
	setHTTPStatus(&parallel, parallelResp, parallelErr, true)
	parallelUses := ToolUses(parallelResp.JSON)
	if parallel.Status == result.StatusPass {
		ids := map[string]bool{}
		for _, use := range parallelUses {
			if id := stringValue(use["id"]); id != "" {
				ids[id] = true
			}
		}
		if len(parallelUses) < 2 || len(ids) != len(parallelUses) {
			parallel.Status = result.StatusWarn
			parallel.Summary = "Parallel tool use was accepted but two distinct calls were not demonstrated"
		} else {
			parallel.Summary = "Parallel tool use returned distinct tool_use IDs"
		}
	}

	strictTool := map[string]any{"name": "get_status", "description": "Return status for a resource.", "strict": true, "input_schema": tool["input_schema"]}
	strict := runToolChoiceProbe(parent, client, timeout, p, model, "anthropic.tools.strict", strictTool, map[string]any{"type": "tool", "name": "get_status"}, true)
	if strict.Status == result.StatusPass {
		strict.Summary = "Strict tool choice returned schema-conforming input"
	}
	return []result.TestResult{auto, anyProbe, none, specific, inputSchema, continuation, parallel, streamProbe, strict}
}

func runToolChoiceProbe(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model, id string, tool, choice map[string]any, requireUse bool) result.TestResult {
	body := basicBody(model)
	body["tools"] = []any{tool}
	body["tool_choice"] = choice
	body["messages"] = []any{map[string]any{"role": "user", "content": "Call get_status for resource-alpha before answering."}}
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, body) })
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
}

func runCountTokens(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string, observed int64) []result.TestResult {
	body := basicBody(model)
	delete(body, "max_tokens")
	basic, count := countTokensProbe(parent, client, timeout, p, "anthropic.tokens.count.basic", body)
	if basic.Status == result.StatusUnsupported {
		basic.Signals = []result.EvidenceSignal{{Code: "anthropic.tokens.endpoint_missing"}}
	}
	schema := derived(p, "anthropic.tokens.count.schema", basic)
	schema.Summary = "Count Tokens response has a positive integer input_tokens"
	systemBody := cloneMap(body)
	systemBody["system"] = "Always answer tersely."
	systemProbe, systemCount := countTokensProbe(parent, client, timeout, p, "anthropic.tokens.count.system", systemBody)
	if systemProbe.Status == result.StatusPass && systemCount <= count {
		systemProbe.Status = result.StatusWarn
		systemProbe.Summary = "System content was accepted but did not increase the reported count"
	}
	toolsBody := cloneMap(body)
	toolsBody["tools"] = []any{map[string]any{"name": "get_status", "description": "Return status.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"resource_id": map[string]any{"type": "string"}}, "required": []any{"resource_id"}}}}
	toolsProbe, toolsCount := countTokensProbe(parent, client, timeout, p, "anthropic.tokens.count.tools", toolsBody)
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

func countTokensProbe(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, id string, body map[string]any) (result.TestResult, int64) {
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.CountTokens(ctx, body) })
	test := testBase(p, id)
	test.DurationMS, test.HTTP = resp.Duration.Milliseconds(), observation(resp)
	setHTTPStatus(&test, resp, err, true)
	count := int64Value(resp.JSON["input_tokens"])
	if test.Status == result.StatusPass && count <= 0 {
		test.Status = result.StatusFail
		test.Summary = "Count Tokens returned no positive input_tokens"
	} else if test.Status == result.StatusPass {
		test.Summary = fmt.Sprintf("Count Tokens returned %d input tokens", count)
	}
	return test, count
}

func runStructured(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) []result.TestResult {
	body := basicBody(model)
	body["messages"] = []any{map[string]any{"role": "user", "content": "Return JSON with ok=true and value=AIQA_OK."}}
	body["output_config"] = map[string]any{"format": map[string]any{"type": "json_schema", "schema": map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}, "value": map[string]any{"type": "string"}}, "required": []any{"ok", "value"}, "additionalProperties": false}}}
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, body) })
	probe := testBase(p, "anthropic.structured.json_schema")
	probe.DurationMS = resp.Duration.Milliseconds()
	probe.HTTP = observation(resp)
	probe.Usage = ParseUsage(resp.JSON)
	setHTTPStatus(&probe, resp, err, true)
	if probe.Status == result.StatusPass {
		if !validStructuredOutput(Text(resp.JSON)) {
			probe.Status = result.StatusFail
			probe.Summary = "Structured output is not schema-conforming JSON"
			probe.Signals = []result.EvidenceSignal{{Code: "anthropic.structured.schema_violation"}}
		} else {
			probe.Summary = "Structured output conforms to the requested JSON schema"
		}
	}
	stop := derived(p, "anthropic.structured.stop_reason", probe)
	reason := stringValue(resp.JSON["stop_reason"])
	if probe.Status == result.StatusPass && (reason == "max_tokens" || reason == "refusal") {
		stop.Status = result.StatusWarn
		stop.Summary = "Structured output ended with " + reason
	} else {
		stop.Summary = "Structured output completed with compatible stop semantics"
	}
	strictTool := map[string]any{"name": "get_status", "description": "Return status.", "strict": true, "input_schema": map[string]any{"type": "object", "properties": map[string]any{"resource_id": map[string]any{"type": "string"}}, "required": []any{"resource_id"}, "additionalProperties": false}}
	strict := runToolChoiceProbe(parent, client, timeout, p, model, "anthropic.tools.strict_schema", strictTool, map[string]any{"type": "tool", "name": "get_status"}, true)
	if strict.Status == result.StatusPass {
		strict.Summary = "Strict tool input conforms to the local schema"
	}
	return []result.TestResult{probe, stop, strict}
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

func runThinking(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) []result.TestResult {
	adaptiveBody := basicBody(model)
	adaptiveBody["max_tokens"] = 2048
	adaptiveBody["thinking"] = map[string]any{"type": "adaptive"}
	adaptiveResp, adaptiveErr := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, adaptiveBody) })
	adaptive := testBase(p, "anthropic.thinking.adaptive")
	adaptive.DurationMS = adaptiveResp.Duration.Milliseconds()
	adaptive.HTTP = metadataOnlyObservation(adaptiveResp)
	adaptive.Usage = ParseUsage(adaptiveResp.JSON)
	setHTTPStatus(&adaptive, adaptiveResp, adaptiveErr, true)
	if adaptive.Status == result.StatusUnsupported {
		adaptive.Signals = []result.EvidenceSignal{{Code: "anthropic.thinking.unsupported", Parameters: map[string]any{"mode": "adaptive"}}}
	}
	if adaptive.Status == result.StatusPass {
		adaptive.Summary = "Adaptive thinking returned a compatible message; thinking content was not retained"
	}
	effortBody := cloneMap(adaptiveBody)
	effortBody["output_config"] = map[string]any{"effort": "high"}
	effortResp, effortErr := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, effortBody) })
	effort := testBase(p, "anthropic.thinking.effort")
	effort.DurationMS = effortResp.Duration.Milliseconds()
	effort.HTTP = metadataOnlyObservation(effortResp)
	effort.Usage = ParseUsage(effortResp.JSON)
	setHTTPStatus(&effort, effortResp, effortErr, true)
	if effort.Status == result.StatusUnsupported {
		effort.Signals = []result.EvidenceSignal{{Code: "anthropic.thinking.unsupported", Parameters: map[string]any{"mode": "effort"}}}
	}
	if effort.Status == result.StatusPass {
		effort.Summary = "High thinking effort was accepted"
	}
	manualBody := basicBody(model)
	manualBody["max_tokens"] = 2048
	manualBody["thinking"] = map[string]any{"type": "enabled", "budget_tokens": 1024}
	manualResp, manualErr := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, manualBody) })
	manual := testBase(p, "anthropic.thinking.manual")
	manual.DurationMS = manualResp.Duration.Milliseconds()
	manual.HTTP = metadataOnlyObservation(manualResp)
	manual.Usage = ParseUsage(manualResp.JSON)
	setHTTPStatus(&manual, manualResp, manualErr, true)
	if manual.Status == result.StatusUnsupported {
		manual.Signals = []result.EvidenceSignal{{Code: "anthropic.thinking.unsupported", Parameters: map[string]any{"mode": "manual"}}}
	}
	if manual.Status == result.StatusPass {
		manual.Summary = "Manual thinking budget was accepted; thinking content was not retained"
	}
	invalidBody := basicBody(model)
	invalidBody["thinking"] = map[string]any{"type": "enabled", "budget_tokens": 1}
	invalidResp, invalidErr := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, invalidBody) })
	validation := testBase(p, "anthropic.thinking.manual.validation")
	validation.DurationMS = invalidResp.Duration.Milliseconds()
	validation.HTTP = metadataOnlyObservation(invalidResp)
	setExpectedError(&validation, invalidResp, invalidErr)
	if validation.Status == result.StatusPass {
		validation.Summary = "Invalid manual thinking budget was rejected"
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	stream, streamErr := client.StreamMessage(ctx, manualBody)
	cancel()
	streamProbe := testBase(p, "anthropic.thinking.stream")
	streamProbe.DurationMS = stream.Duration.Milliseconds()
	streamProbe.HTTP = result.HTTPObservation{StatusCode: stream.StatusCode, ContentType: stream.ContentType, RequestID: stream.RequestID}
	streamValidation := ValidateStream(stream)
	streamProbe.Usage = streamValidation.Usage
	if streamErr != nil {
		streamProbe.Status = result.StatusError
		streamProbe.Summary = "Thinking stream could not be evaluated"
		streamProbe.Error = streamErr.Error()
	} else if streamValidation.ProviderError {
		streamProbe.Status = result.StatusError
		streamProbe.Summary = "Provider emitted an error during thinking stream"
	} else if len(streamValidation.Problems) > 0 {
		streamProbe.Status = result.StatusFail
		streamProbe.Summary = "Thinking stream sequence is invalid"
		streamProbe.Observation = strings.Join(streamValidation.Problems, "; ")
	} else if streamValidation.Incomplete {
		streamProbe.Status = result.StatusError
		streamProbe.Summary = "Thinking stream ended before message_stop"
	} else {
		streamProbe.Status = result.StatusPass
		streamProbe.Summary = "Thinking stream used a legal Anthropic event sequence; thinking content was not retained"
	}
	return []result.TestResult{adaptive, effort, manual, validation, streamProbe}
}

func runVision(parent context.Context, client *Client, timeout time.Duration, p profile.Profile, model string) []result.TestResult {
	const encoded = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	body := basicBody(model)
	body["messages"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": encoded}}, map[string]any{"type": "text", "text": "Reply with exactly AIQA_OK."}}}}
	resp, err := call(parent, timeout, func(ctx context.Context) (api.Response, error) { return client.Message(ctx, body) })
	probe := testBase(p, "anthropic.vision.image_base64")
	probe.DurationMS = resp.Duration.Milliseconds()
	probe.HTTP = observation(resp)
	setHTTPStatus(&probe, resp, err, true)
	decoded, _ := base64.StdEncoding.DecodeString(encoded)
	digest := sha256.Sum256(decoded)
	probe.Artifacts = []result.ArtifactObservation{{Kind: "input_fixture", Source: "embedded", MediaType: "image/png", Format: "png", Bytes: int64(len(decoded)), SHA256: hex.EncodeToString(digest[:]), Width: 1, Height: 1}}
	if probe.Status == result.StatusPass && strings.Contains(Text(resp.JSON), "AIQA_OK") {
		probe.Summary = "Embedded PNG was accepted and produced the requested semantic answer"
	} else if probe.Status == result.StatusPass {
		probe.Status = result.StatusWarn
		probe.Summary = "Embedded PNG was accepted but semantic use was not demonstrated"
	}
	schema := derived(p, "anthropic.vision.image_schema", probe)
	schema.Artifacts = append([]result.ArtifactObservation(nil), probe.Artifacts...)
	schema.Summary = "Anthropic base64 image block was accepted"
	usage := derived(p, "anthropic.vision.usage", probe)
	usage.Usage = ParseUsage(resp.JSON)
	usage.Summary = "Vision usage is non-negative"
	return []result.TestResult{probe, schema, usage}
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
	t.Status = source.Status
	t.DurationMS = source.DurationMS
	t.HTTP = source.HTTP
	t.Error = source.Error
	return t
}
func call(parent context.Context, timeout time.Duration, fn func(context.Context) (api.Response, error)) (api.Response, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
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
func observation(resp api.Response) result.HTTPObservation {
	return result.HTTPObservation{StatusCode: resp.StatusCode, ContentType: resp.ContentType, RequestID: resp.RequestID, Snippet: truncate(resp.Snippet(1000), 1000)}
}
func metadataOnlyObservation(resp api.Response) result.HTTPObservation {
	return result.HTTPObservation{StatusCode: resp.StatusCode, ContentType: resp.ContentType, RequestID: resp.RequestID}
}
func profileInfo(p profile.Profile) result.ProfileInfo {
	return result.ProfileInfo{SchemaVersion: p.SchemaVersion, ID: p.ID, Protocol: p.Protocol, Revision: p.Revision, APIVersion: p.APIVersion, BaseURLSemantics: p.BaseURLSemantics, Source: p.Source, SourceCommit: p.SourceCommit, SourceRefRevision: p.SourceRefRevision, Scope: p.Scope}
}
func toTarget(target config.TargetConfig, r result.Report) result.TargetReport {
	return result.TargetReport{ID: target.ID, Protocol: string(target.Protocol), Profile: r.Profile, Discovery: r.Discovery, Models: r.Models, Requirements: r.Requirements, Summary: r.Summary, Guidance: r.Guidance, RateLimit: r.RateLimit}
}
func selectModels(requested []string, models []api.Model) ([]string, map[string]map[string]any) {
	metadata := map[string]map[string]any{}
	for _, m := range models {
		metadata[m.ID] = m.Raw
	}
	if len(requested) > 0 {
		out := append([]string(nil), requested...)
		for _, id := range out {
			if metadata[id] == nil {
				metadata[id] = map[string]any{"id": id, "type": "model", "source": "--anthropic-model"}
			}
		}
		return out, metadata
	}
	out := make([]string, 0, len(models))
	for _, m := range models {
		out = append(out, m.ID)
	}
	sort.Strings(out)
	return out, metadata
}
func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}
func addLatency(out *result.RateLimitStats, r result.Report) {
	var values []int64
	for _, t := range r.Discovery.Tests {
		if t.HTTP.StatusCode >= 200 && t.HTTP.StatusCode < 300 && t.DurationMS > 0 {
			values = append(values, t.DurationMS)
		}
	}
	for _, m := range r.Models {
		for _, t := range m.Compatibility {
			if t.HTTP.StatusCode >= 200 && t.HTTP.StatusCode < 300 && t.DurationMS > 0 {
				values = append(values, t.DurationMS)
			}
		}
	}
	if len(values) == 0 {
		return
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	var sum int64
	for _, v := range values {
		sum += v
	}
	out.LatencySamples = len(values)
	out.MeanLatencyMS = sum / int64(len(values))
	out.P50LatencyMS = values[(len(values)-1)/2]
	out.P95LatencyMS = values[(95*len(values)-1)/100]
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

func addModelLatency(out *result.RateLimitStats, model result.ModelReport) {
	var values []int64
	for _, test := range model.Compatibility {
		if test.HTTP.StatusCode >= 200 && test.HTTP.StatusCode < 300 && test.DurationMS > 0 {
			values = append(values, test.DurationMS)
		}
	}
	if len(values) == 0 {
		return
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	var sum int64
	for _, value := range values {
		sum += value
	}
	out.LatencySamples = len(values)
	out.MeanLatencyMS = sum / int64(len(values))
	out.P50LatencyMS = values[(len(values)-1)/2]
	out.P95LatencyMS = values[(95*len(values)-1)/100]
}

var _ = http.MethodPost
