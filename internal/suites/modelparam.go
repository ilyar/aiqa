package suites

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/probe"
	"github.com/ilyar/aiqa/internal/result"
)

type outputLimitSupport struct {
	Parameter string
	Accepted  bool
}

type paramCall struct {
	Resp            api.Response
	Err             error
	Content         string
	FinishReason    string
	Usage           result.Usage
	ReasoningText   string
	ReasoningKeys   []string
	ReasoningTokens int64
}

func RunModelParameters(ctx context.Context, env Environment, model string, metadata map[string]any) ([]result.ParameterResult, *result.ContextProbeResult) {
	var out []result.ParameterResult

	env.logf("[%s] model-param: output limits", model)
	baseline := runParamChat(ctx, env, model, map[string]any{}, longOutputPrompt(), 0)
	outputLimits := probe.Map(env.Probes,
		func() result.ParameterResult {
			return probeOutputLimit(ctx, env, model, "max_completion_tokens", baseline)
		},
		func() result.ParameterResult { return probeOutputLimit(ctx, env, model, "max_tokens", baseline) },
	)
	maxCompletion, maxTokens := outputLimits[0], outputLimits[1]
	// Count the shared unlimited baseline exactly once in aggregate usage.
	maxCompletion.Usage = maxCompletion.Usage.Add(baseline.Usage)
	maxCompletion.DurationMS += baseline.Resp.Duration.Milliseconds()
	maxCompletion.Metrics["baseline_status"] = baseline.Resp.StatusCode
	out = append(out, maxCompletion, maxTokens)
	limit := chooseOutputLimit(maxCompletion, maxTokens)

	env.logf("[%s] model-param: scheduling independent probes", model)
	type group struct {
		parameters []result.ParameterResult
		context    *result.ContextProbeResult
	}
	groups := probe.Map(env.Probes,
		func() group { return group{context: ProbeContextWindow(ctx, env, model, metadata, limit.Parameter)} },
		func() group { return group{parameters: probeReasoningControls(ctx, env, model, limit.Parameter)} },
		func() group {
			return group{parameters: []result.ParameterResult{probeVerbosity(ctx, env, model, limit.Parameter)}}
		},
		func() group {
			return group{parameters: []result.ParameterResult{probeTemperature(ctx, env, model, limit.Parameter)}}
		},
		func() group {
			return group{parameters: []result.ParameterResult{probeTopP(ctx, env, model, limit.Parameter)}}
		},
		func() group {
			return group{parameters: []result.ParameterResult{probeSeed(ctx, env, model, limit.Parameter)}}
		},
		func() group {
			return group{parameters: []result.ParameterResult{probeStop(ctx, env, model, limit.Parameter)}}
		},
		func() group {
			return group{parameters: []result.ParameterResult{probeN(ctx, env, model, limit.Parameter)}}
		},
		func() group {
			return group{parameters: []result.ParameterResult{probeParallelToolCalls(ctx, env, model, limit.Parameter)}}
		},
		func() group {
			return group{parameters: []result.ParameterResult{probeSimpleValidatedParameter(ctx, env, model, limit.Parameter, "presence_penalty", 0.5, 3.0)}}
		},
		func() group {
			return group{parameters: []result.ParameterResult{probeSimpleValidatedParameter(ctx, env, model, limit.Parameter, "frequency_penalty", 0.5, 3.0)}}
		},
		func() group {
			return group{parameters: []result.ParameterResult{probeLogprobsParameter(ctx, env, model, limit.Parameter)}}
		},
	)
	var contextResult *result.ContextProbeResult
	for _, group := range groups {
		out = append(out, group.parameters...)
		if group.context != nil {
			contextResult = group.context
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Variant < out[j].Variant
		}
		return out[i].Name < out[j].Name
	})
	return out, contextResult
}

func runParamChat(parent context.Context, env Environment, model string, patch map[string]any, prompt string, outputLimit int) paramCall {
	body := map[string]any{
		"model":    model,
		"messages": []any{map[string]any{"role": "user", "content": prompt}},
	}
	for key, value := range patch {
		body[key] = value
	}
	if outputLimit > 0 {
		if _, exists := body["max_completion_tokens"]; !exists {
			if _, exists := body["max_tokens"]; !exists {
				body["max_tokens"] = outputLimit
			}
		}
	}
	resp, err := callChat(parent, env, body)
	text, reasoningTokens, reasoningKeys := api.ReasoningEvidence(resp.JSON)
	return paramCall{
		Resp:            resp,
		Err:             err,
		Content:         strings.TrimSpace(api.FirstContent(resp.JSON)),
		FinishReason:    api.FinishReason(resp.JSON),
		Usage:           api.ParseUsage(resp.JSON),
		ReasoningText:   text,
		ReasoningTokens: reasoningTokens,
		ReasoningKeys:   reasoningKeys,
	}
}

func longOutputPrompt() string {
	return "Write exactly forty distinct lowercase words separated by spaces. Do not use punctuation or numbering."
}

func probeOutputLimit(ctx context.Context, env Environment, model, parameter string, baseline paramCall) result.ParameterResult {
	valid := runParamChat(ctx, env, model, map[string]any{parameter: 8}, longOutputPrompt(), 0)
	invalid := runParamChat(ctx, env, model, map[string]any{parameter: -1}, "Reply OK.", 0)
	accepted := valid.Resp.Is2xx()
	invalidRejected := !invalid.Resp.Is2xx() && invalid.Resp.StatusCode >= 400 && invalid.Resp.StatusCode < 500
	effective := false
	if accepted {
		if valid.Usage.CompletionTokens > 0 && valid.Usage.CompletionTokens <= 8 {
			effective = true
		}
		if valid.FinishReason == "length" {
			effective = true
		}
		if baseline.Usage.CompletionTokens > 8 && valid.Usage.CompletionTokens > 0 && valid.Usage.CompletionTokens < baseline.Usage.CompletionTokens {
			effective = true
		}
	}
	p := result.ParameterResult{
		Name:       parameter,
		Variant:    "limit=8",
		Accepted:   result.Bool(accepted),
		Effective:  result.Bool(effective),
		DurationMS: valid.Resp.Duration.Milliseconds() + invalid.Resp.Duration.Milliseconds(),
		HTTP:       httpObservation(valid.Resp),
		Usage:      valid.Usage.Add(invalid.Usage),
		Metrics: map[string]any{
			"baseline_completion_tokens": baseline.Usage.CompletionTokens,
			"limited_completion_tokens":  valid.Usage.CompletionTokens,
			"finish_reason":              valid.FinishReason,
			"invalid_value_rejected":     invalidRejected,
			"invalid_status":             invalid.Resp.StatusCode,
		},
	}
	if valid.Err != nil && valid.Resp.StatusCode == 0 {
		p.Status = result.StatusError
		p.Summary = parameter + " could not be evaluated"
		p.Error = errorText(valid.Err)
		return p
	}
	if !accepted {
		if valid.Resp.StatusCode >= 400 && valid.Resp.StatusCode < 500 {
			p.Status = result.StatusUnsupported
			p.Summary = parameter + " is rejected"
		} else {
			p.Status = result.StatusError
			p.Summary = parameter + " probe failed"
			p.Error = errorText(valid.Err)
		}
		return p
	}
	if effective && invalidRejected {
		p.Status = result.StatusPass
		p.Confidence = "high"
		p.Summary = parameter + " is accepted, enforced, and validates invalid values"
	} else if effective {
		p.Status = result.StatusWarn
		p.Confidence = "medium"
		p.Summary = parameter + " appears effective, but invalid values are not rejected"
	} else {
		p.Status = result.StatusWarn
		p.Confidence = "low"
		p.Summary = parameter + " is accepted, but its limiting effect was not demonstrated"
	}
	return p
}

func chooseOutputLimit(results ...result.ParameterResult) outputLimitSupport {
	for _, p := range results {
		if p.Accepted != nil && *p.Accepted && p.Effective != nil && *p.Effective {
			return outputLimitSupport{Parameter: p.Name, Accepted: true}
		}
	}
	for _, p := range results {
		if p.Accepted != nil && *p.Accepted {
			return outputLimitSupport{Parameter: p.Name, Accepted: true}
		}
	}
	return outputLimitSupport{}
}

func withOutputLimit(patch map[string]any, parameter string, value int) map[string]any {
	out := make(map[string]any, len(patch)+1)
	for key, v := range patch {
		out[key] = v
	}
	if parameter != "" && value > 0 {
		out[parameter] = value
	}
	return out
}

func probeVerbosity(ctx context.Context, env Environment, model, limitParam string) result.ParameterResult {
	prompt := "This is a verbosity-control probe. Explain in plain language why checksums can detect accidental data corruption."
	low := runParamChat(ctx, env, model, withOutputLimit(map[string]any{"verbosity": "low"}, limitParam, 256), prompt, 0)
	high := runParamChat(ctx, env, model, withOutputLimit(map[string]any{"verbosity": "high"}, limitParam, 256), prompt, 0)
	invalid := runParamChat(ctx, env, model, withOutputLimit(map[string]any{"verbosity": "aiqa-invalid"}, limitParam, 64), prompt, 0)
	accepted := low.Resp.Is2xx() && high.Resp.Is2xx()
	lowMeasure := low.Usage.CompletionTokens
	highMeasure := high.Usage.CompletionTokens
	measure := "completion_tokens"
	if lowMeasure == 0 || highMeasure == 0 {
		lowMeasure = int64(len(strings.Fields(low.Content)))
		highMeasure = int64(len(strings.Fields(high.Content)))
		measure = "output_words"
	}
	effective := accepted && highMeasure >= lowMeasure+5 && highMeasure*100 >= lowMeasure*125
	invalidRejected := !invalid.Resp.Is2xx() && invalid.Resp.StatusCode >= 400 && invalid.Resp.StatusCode < 500
	p := result.ParameterResult{
		Name:       "verbosity",
		Variant:    "low vs high",
		Accepted:   result.Bool(accepted),
		Effective:  result.Bool(effective),
		DurationMS: low.Resp.Duration.Milliseconds() + high.Resp.Duration.Milliseconds() + invalid.Resp.Duration.Milliseconds(),
		HTTP:       httpObservation(low.Resp),
		Usage:      low.Usage.Add(high.Usage).Add(invalid.Usage),
		Metrics: map[string]any{
			"measure":                measure,
			"low":                    lowMeasure,
			"high":                   highMeasure,
			"invalid_value_rejected": invalidRejected,
			"invalid_status":         invalid.Resp.StatusCode,
		},
	}
	if !accepted {
		p.Status = statusFromParamCall(low)
		if low.Resp.Is2xx() {
			p.Status = statusFromParamCall(high)
		}
		p.Summary = "verbosity low/high is rejected or failed"
	} else if effective && invalidRejected {
		p.Status = result.StatusPass
		p.Confidence = "medium"
		p.Summary = "verbosity is validated and high produced a materially longer answer than low"
	} else if effective {
		p.Status = result.StatusWarn
		p.Confidence = "low"
		p.Summary = "verbosity changed output length, but an invalid value was accepted"
	} else if invalidRejected {
		p.Status = result.StatusWarn
		p.Confidence = "low"
		p.Summary = "verbosity is accepted and validated, but its semantic effect was not demonstrated"
	} else {
		p.Status = result.StatusWarn
		p.Confidence = "low"
		p.Summary = "verbosity is accepted, but may be ignored"
	}
	return p
}

func probeTemperature(ctx context.Context, env Environment, model, limitParam string) result.ParameterResult {
	invalid := runParamChat(ctx, env, model, withOutputLimit(map[string]any{"temperature": 99.0}, limitParam, 16), creativePrompt(), 0)
	lowCalls := make([]paramCall, 0, env.Config.Samples)
	highCalls := make([]paramCall, 0, env.Config.Samples)
	for i := 0; i < env.Config.Samples; i++ {
		lowCalls = append(lowCalls, runParamChat(ctx, env, model, withOutputLimit(map[string]any{"temperature": 0.0}, limitParam, 16), creativePrompt(), 0))
		highCalls = append(highCalls, runParamChat(ctx, env, model, withOutputLimit(map[string]any{"temperature": 1.8}, limitParam, 16), creativePrompt(), 0))
	}
	return evaluateDiversityParameter("temperature", "0.0 vs 1.8", lowCalls, highCalls, invalid)
}

func probeTopP(ctx context.Context, env Environment, model, limitParam string) result.ParameterResult {
	invalid := runParamChat(ctx, env, model, withOutputLimit(map[string]any{"top_p": 2.0}, limitParam, 16), creativePrompt(), 0)
	lowCalls := make([]paramCall, 0, env.Config.Samples)
	highCalls := make([]paramCall, 0, env.Config.Samples)
	for i := 0; i < env.Config.Samples; i++ {
		lowCalls = append(lowCalls, runParamChat(ctx, env, model, withOutputLimit(map[string]any{"top_p": 0.01}, limitParam, 16), creativePrompt(), 0))
		highCalls = append(highCalls, runParamChat(ctx, env, model, withOutputLimit(map[string]any{"top_p": 1.0}, limitParam, 16), creativePrompt(), 0))
	}
	return evaluateDiversityParameter("top_p", "0.01 vs 1.0", lowCalls, highCalls, invalid)
}

func creativePrompt() string {
	return "Invent one pronounceable nonsense word of exactly six lowercase ASCII letters. Return only that word."
}

var wordPattern = regexp.MustCompile(`[A-Za-z]+`)

func normalizeSample(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if word := wordPattern.FindString(s); word != "" {
		return word
	}
	return s
}

func evaluateDiversityParameter(name, variant string, lowCalls, highCalls []paramCall, invalid paramCall) result.ParameterResult {
	lowValues, highValues := callValues(lowCalls), callValues(highCalls)
	lowUnique, highUnique := uniqueCount(lowValues), uniqueCount(highValues)
	accepted := all2xx(lowCalls) && all2xx(highCalls)
	invalidRejected := !invalid.Resp.Is2xx() && invalid.Resp.StatusCode >= 400 && invalid.Resp.StatusCode < 500
	effective := accepted && highUnique > lowUnique && highUnique >= 2
	p := result.ParameterResult{
		Name:       name,
		Variant:    variant,
		Accepted:   result.Bool(accepted),
		Effective:  result.Bool(effective),
		DurationMS: sumCallDuration(lowCalls) + sumCallDuration(highCalls) + invalid.Resp.Duration.Milliseconds(),
		Usage:      sumCallUsage(append(append([]paramCall{}, lowCalls...), append(highCalls, invalid)...)),
		Metrics: map[string]any{
			"low_outputs":            lowValues,
			"high_outputs":           highValues,
			"low_unique":             lowUnique,
			"high_unique":            highUnique,
			"invalid_value_rejected": invalidRejected,
			"invalid_status":         invalid.Resp.StatusCode,
		},
	}
	if len(lowCalls) > 0 {
		p.HTTP = httpObservation(lowCalls[0].Resp)
	}
	if !accepted {
		status := firstFailureStatus(append(lowCalls, highCalls...))
		if status >= 400 && status < 500 {
			p.Status = result.StatusUnsupported
			p.Summary = name + " is rejected for at least one valid value"
		} else {
			p.Status = result.StatusError
			p.Summary = name + " could not be evaluated reliably"
		}
		return p
	}
	if effective && invalidRejected {
		p.Status = result.StatusPass
		p.Confidence = "medium"
		p.Summary = name + " is accepted, validated, and changed sample diversity"
	} else if effective {
		p.Status = result.StatusWarn
		p.Confidence = "low"
		p.Summary = name + " changed diversity, but an invalid value was also accepted"
	} else if invalidRejected {
		p.Status = result.StatusWarn
		p.Confidence = "low"
		p.Summary = name + " is validated and accepted, but a semantic effect was not demonstrated"
	} else {
		p.Status = result.StatusWarn
		p.Confidence = "low"
		p.Summary = name + " is accepted, but may be ignored or clamped"
	}
	return p
}

func probeSeed(ctx context.Context, env Environment, model, limitParam string) result.ParameterResult {
	count := env.Config.Samples
	if count > 6 {
		count = 6
	}
	same, different := make([]paramCall, 0, count), make([]paramCall, 0, count)
	for i := 0; i < count; i++ {
		same = append(same, runParamChat(ctx, env, model, withOutputLimit(map[string]any{"seed": 424242, "temperature": 1.0}, limitParam, 16), creativePrompt(), 0))
		different = append(different, runParamChat(ctx, env, model, withOutputLimit(map[string]any{"seed": 424242 + i, "temperature": 1.0}, limitParam, 16), creativePrompt(), 0))
	}
	invalid := runParamChat(ctx, env, model, withOutputLimit(map[string]any{"seed": "not-an-integer"}, limitParam, 16), creativePrompt(), 0)
	accepted := all2xx(same) && all2xx(different)
	sameValues, differentValues := callValues(same), callValues(different)
	sameUnique, differentUnique := uniqueCount(sameValues), uniqueCount(differentValues)
	effective := accepted && sameUnique == 1 && differentUnique > 1
	invalidRejected := !invalid.Resp.Is2xx() && invalid.Resp.StatusCode >= 400 && invalid.Resp.StatusCode < 500
	p := result.ParameterResult{
		Name:       "seed",
		Variant:    "same vs different seeds",
		Accepted:   result.Bool(accepted),
		Effective:  result.Bool(effective),
		DurationMS: sumCallDuration(same) + sumCallDuration(different) + invalid.Resp.Duration.Milliseconds(),
		Usage:      sumCallUsage(append(append([]paramCall{}, same...), append(different, invalid)...)),
		Metrics: map[string]any{
			"same_seed_outputs":      sameValues,
			"different_seed_outputs": differentValues,
			"same_seed_unique":       sameUnique,
			"different_seed_unique":  differentUnique,
			"invalid_value_rejected": invalidRejected,
		},
	}
	if len(same) > 0 {
		p.HTTP = httpObservation(same[0].Resp)
	}
	if !accepted {
		p.Status = result.StatusUnsupported
		p.Summary = "seed is rejected or not consistently accepted"
	} else if effective {
		p.Status = result.StatusPass
		p.Confidence = "medium"
		p.Summary = "seed produced stable same-seed output and varied different-seed output"
	} else {
		p.Status = result.StatusWarn
		p.Confidence = "low"
		p.Summary = "seed is accepted, but best-effort determinism was not demonstrated"
	}
	return p
}

func probeStop(ctx context.Context, env Environment, model, limitParam string) result.ParameterResult {
	prompt := "Output exactly these words and spaces: ALPHA STOP OMEGA"
	valid := runParamChat(ctx, env, model, withOutputLimit(map[string]any{"stop": []any{"STOP"}}, limitParam, 32), prompt, 0)
	invalid := runParamChat(ctx, env, model, withOutputLimit(map[string]any{"stop": map[string]any{"bad": true}}, limitParam, 16), "Reply OK.", 0)
	accepted := valid.Resp.Is2xx()
	upper := strings.ToUpper(valid.Content)
	effective := accepted && strings.Contains(upper, "ALPHA") && !strings.Contains(upper, "STOP") && !strings.Contains(upper, "OMEGA")
	invalidRejected := !invalid.Resp.Is2xx() && invalid.Resp.StatusCode >= 400 && invalid.Resp.StatusCode < 500
	p := result.ParameterResult{
		Name:       "stop",
		Variant:    "[STOP]",
		Accepted:   result.Bool(accepted),
		Effective:  result.Bool(effective),
		DurationMS: valid.Resp.Duration.Milliseconds() + invalid.Resp.Duration.Milliseconds(),
		HTTP:       httpObservation(valid.Resp),
		Usage:      valid.Usage.Add(invalid.Usage),
		Metrics: map[string]any{
			"output":                 truncate(valid.Content, 200),
			"invalid_value_rejected": invalidRejected,
		},
	}
	if !accepted {
		p.Status = statusFromParamCall(valid)
		p.Summary = "stop is rejected or failed"
	} else if effective && invalidRejected {
		p.Status = result.StatusPass
		p.Confidence = "high"
		p.Summary = "stop is accepted, effective, and validates invalid values"
	} else if effective {
		p.Status = result.StatusWarn
		p.Confidence = "medium"
		p.Summary = "stop appears effective, but invalid values are not rejected"
	} else {
		p.Status = result.StatusWarn
		p.Confidence = "low"
		p.Summary = "stop is accepted, but the stop sequence was not reliably enforced"
	}
	return p
}

func probeN(ctx context.Context, env Environment, model, limitParam string) result.ParameterResult {
	valid := runParamChat(ctx, env, model, withOutputLimit(map[string]any{"n": 2}, limitParam, 16), "Reply with one lowercase word.", 0)
	invalid := runParamChat(ctx, env, model, withOutputLimit(map[string]any{"n": 0}, limitParam, 16), "Reply OK.", 0)
	accepted := valid.Resp.Is2xx()
	count := api.ChoiceCount(valid.Resp.JSON)
	effective := accepted && count == 2
	invalidRejected := !invalid.Resp.Is2xx() && invalid.Resp.StatusCode >= 400 && invalid.Resp.StatusCode < 500
	p := result.ParameterResult{
		Name:       "n",
		Variant:    "2",
		Accepted:   result.Bool(accepted),
		Effective:  result.Bool(effective),
		DurationMS: valid.Resp.Duration.Milliseconds() + invalid.Resp.Duration.Milliseconds(),
		HTTP:       httpObservation(valid.Resp),
		Usage:      valid.Usage.Add(invalid.Usage),
		Metrics:    map[string]any{"choices": count, "invalid_value_rejected": invalidRejected},
	}
	if !accepted {
		p.Status = statusFromParamCall(valid)
		p.Summary = "n is rejected or failed"
	} else if effective && invalidRejected {
		p.Status = result.StatusPass
		p.Confidence = "high"
		p.Summary = "n=2 returned two choices and n=0 was rejected"
	} else if effective {
		p.Status = result.StatusWarn
		p.Confidence = "medium"
		p.Summary = "n=2 returned two choices, but invalid n was accepted"
	} else {
		p.Status = result.StatusFail
		p.Confidence = "high"
		p.Summary = fmt.Sprintf("n=2 was accepted but returned %d choice(s)", count)
	}
	return p
}

func probeParallelToolCalls(ctx context.Context, env Environment, model, limitParam string) result.ParameterResult {
	prompt := "Call get_status once for resource-alpha and once for resource-beta. Return both calls in the same assistant turn."
	base := map[string]any{
		"tools":               []any{toolDefinition()},
		"tool_choice":         "required",
		"parallel_tool_calls": true,
	}
	valid := runParamChat(ctx, env, model, withOutputLimit(base, limitParam, 128), prompt, 0)
	invalidPatch := map[string]any{
		"tools":               []any{toolDefinition()},
		"tool_choice":         "required",
		"parallel_tool_calls": "aiqa-invalid",
	}
	invalid := runParamChat(ctx, env, model, withOutputLimit(invalidPatch, limitParam, 128), prompt, 0)
	accepted := valid.Resp.Is2xx()
	calls := api.FirstToolCalls(valid.Resp.JSON)
	ids := map[string]bool{}
	for _, call := range calls {
		if id, _ := call["id"].(string); id != "" {
			ids[id] = true
		}
	}
	effective := accepted && len(calls) >= 2 && len(ids) >= 2
	invalidRejected := !invalid.Resp.Is2xx() && invalid.Resp.StatusCode >= 400 && invalid.Resp.StatusCode < 500
	p := result.ParameterResult{
		Name:       "parallel_tool_calls",
		Variant:    "true",
		Accepted:   result.Bool(accepted),
		Effective:  result.Bool(effective),
		DurationMS: valid.Resp.Duration.Milliseconds() + invalid.Resp.Duration.Milliseconds(),
		HTTP:       httpObservation(valid.Resp),
		Usage:      valid.Usage.Add(invalid.Usage),
		Metrics: map[string]any{
			"tool_calls":             len(calls),
			"unique_tool_call_ids":   len(ids),
			"invalid_value_rejected": invalidRejected,
			"invalid_status":         invalid.Resp.StatusCode,
		},
	}
	if !accepted {
		p.Status = statusFromParamCall(valid)
		p.Summary = "parallel_tool_calls is rejected or failed"
	} else if effective && invalidRejected {
		p.Status = result.StatusPass
		p.Confidence = "high"
		p.Summary = "parallel_tool_calls emitted multiple independently identified calls and validates its type"
	} else if effective {
		p.Status = result.StatusWarn
		p.Confidence = "medium"
		p.Summary = "parallel tool calls were demonstrated, but an invalid value was accepted"
	} else if invalidRejected {
		p.Status = result.StatusWarn
		p.Confidence = "low"
		p.Summary = "parallel_tool_calls is accepted and validated, but multiple calls were not demonstrated"
	} else {
		p.Status = result.StatusWarn
		p.Confidence = "low"
		p.Summary = "parallel_tool_calls is accepted, but may be ignored"
	}
	return p
}

func probeSimpleValidatedParameter(ctx context.Context, env Environment, model, limitParam, name string, validValue, invalidValue any) result.ParameterResult {
	valid := runParamChat(ctx, env, model, withOutputLimit(map[string]any{name: validValue}, limitParam, 16), "Reply with exactly OK.", 0)
	invalid := runParamChat(ctx, env, model, withOutputLimit(map[string]any{name: invalidValue}, limitParam, 16), "Reply with exactly OK.", 0)
	accepted := valid.Resp.Is2xx()
	invalidRejected := !invalid.Resp.Is2xx() && invalid.Resp.StatusCode >= 400 && invalid.Resp.StatusCode < 500
	p := result.ParameterResult{
		Name:       name,
		Variant:    fmt.Sprint(validValue),
		Accepted:   result.Bool(accepted),
		DurationMS: valid.Resp.Duration.Milliseconds() + invalid.Resp.Duration.Milliseconds(),
		HTTP:       httpObservation(valid.Resp),
		Usage:      valid.Usage.Add(invalid.Usage),
		Metrics:    map[string]any{"invalid_value": invalidValue, "invalid_value_rejected": invalidRejected, "invalid_status": invalid.Resp.StatusCode},
	}
	if !accepted {
		p.Status = statusFromParamCall(valid)
		p.Summary = name + " is rejected or failed"
	} else if invalidRejected {
		p.Status = result.StatusPass
		p.Confidence = "medium"
		p.Summary = name + " is accepted and an out-of-range value is rejected"
	} else {
		p.Status = result.StatusWarn
		p.Confidence = "low"
		p.Summary = name + " is accepted, but an out-of-range value is also accepted; semantic effect is unverified"
	}
	return p
}

func probeLogprobsParameter(ctx context.Context, env Environment, model, limitParam string) result.ParameterResult {
	valid := runParamChat(ctx, env, model, withOutputLimit(map[string]any{"logprobs": true, "top_logprobs": 2}, limitParam, 16), "Reply with only A.", 0)
	invalid := runParamChat(ctx, env, model, withOutputLimit(map[string]any{"logprobs": true, "top_logprobs": 99}, limitParam, 16), "Reply with only A.", 0)
	accepted := valid.Resp.Is2xx()
	choice, _ := api.FirstChoice(valid.Resp.JSON)
	_, returned := choice["logprobs"]
	effective := accepted && returned
	invalidRejected := !invalid.Resp.Is2xx() && invalid.Resp.StatusCode >= 400 && invalid.Resp.StatusCode < 500
	p := result.ParameterResult{
		Name:       "logprobs/top_logprobs",
		Variant:    "true/2",
		Accepted:   result.Bool(accepted),
		Effective:  result.Bool(effective),
		DurationMS: valid.Resp.Duration.Milliseconds() + invalid.Resp.Duration.Milliseconds(),
		HTTP:       httpObservation(valid.Resp),
		Usage:      valid.Usage.Add(invalid.Usage),
		Metrics:    map[string]any{"logprobs_returned": returned, "invalid_value_rejected": invalidRejected},
	}
	if !accepted {
		p.Status = statusFromParamCall(valid)
		p.Summary = "logprobs/top_logprobs are unsupported or failed"
	} else if effective && invalidRejected {
		p.Status = result.StatusPass
		p.Confidence = "high"
		p.Summary = "logprobs are returned and invalid top_logprobs is rejected"
	} else if effective {
		p.Status = result.StatusWarn
		p.Confidence = "medium"
		p.Summary = "logprobs are returned, but invalid top_logprobs is accepted"
	} else {
		p.Status = result.StatusFail
		p.Confidence = "high"
		p.Summary = "logprobs request is accepted but logprobs are absent"
	}
	return p
}

func probeReasoningControls(ctx context.Context, env Environment, model, limitParam string) []result.ParameterResult {
	prompt := "Solve x^2 - 5x + 6 = 0. Give the two roots and one short verification sentence."
	type family struct {
		name     string
		invalid  map[string]any
		variants []struct {
			label string
			patch map[string]any
		}
	}
	families := []family{
		{
			name:    "reasoning_effort",
			invalid: map[string]any{"reasoning_effort": "aiqa-invalid"},
			variants: []struct {
				label string
				patch map[string]any
			}{
				{"none", map[string]any{"reasoning_effort": "none"}},
				{"minimal", map[string]any{"reasoning_effort": "minimal"}},
				{"low", map[string]any{"reasoning_effort": "low"}},
				{"medium", map[string]any{"reasoning_effort": "medium"}},
				{"high", map[string]any{"reasoning_effort": "high"}},
				{"xhigh", map[string]any{"reasoning_effort": "xhigh"}},
				{"max", map[string]any{"reasoning_effort": "max"}},
			},
		},
		{
			name:    "thinking",
			invalid: map[string]any{"thinking": map[string]any{"type": "aiqa-invalid"}},
			variants: []struct {
				label string
				patch map[string]any
			}{
				{"disabled", map[string]any{"thinking": map[string]any{"type": "disabled"}}},
				{"enabled", map[string]any{"thinking": map[string]any{"type": "enabled"}}},
			},
		},
		{
			name:    "reasoning_split",
			invalid: map[string]any{"reasoning_split": "aiqa-invalid"},
			variants: []struct {
				label string
				patch map[string]any
			}{
				{"false", map[string]any{"reasoning_split": false}},
				{"true", map[string]any{"reasoning_split": true}},
			},
		},
	}
	var out []result.ParameterResult
	for _, fam := range families {
		invalidCall := runParamChat(ctx, env, model, withOutputLimit(fam.invalid, limitParam, 256), prompt, 0)
		invalidRejected := !invalidCall.Resp.Is2xx() && invalidCall.Resp.StatusCode >= 400 && invalidCall.Resp.StatusCode < 500
		measurements := make([]reasoningMeasurement, 0, len(fam.variants))
		for _, variant := range fam.variants {
			call := runParamChat(ctx, env, model, withOutputLimit(variant.patch, limitParam, 256), prompt, 0)
			accepted := call.Resp.Is2xx()
			evidence := call.ReasoningTokens > 0 || strings.TrimSpace(call.ReasoningText) != "" || len(call.ReasoningKeys) > 0
			p := result.ParameterResult{
				Name:       fam.name,
				Variant:    variant.label,
				Accepted:   result.Bool(accepted),
				Effective:  result.Bool(evidence),
				DurationMS: call.Resp.Duration.Milliseconds(),
				HTTP:       httpObservation(call.Resp),
				Usage:      call.Usage,
				Metrics: map[string]any{
					"reasoning_tokens":       call.ReasoningTokens,
					"reasoning_chars":        len(call.ReasoningText),
					"reasoning_fields":       call.ReasoningKeys,
					"finish_reason":          call.FinishReason,
					"invalid_value_rejected": invalidRejected,
					"invalid_status":         invalidCall.Resp.StatusCode,
				},
			}
			if !accepted {
				p.Status = statusFromParamCall(call)
				p.Summary = fam.name + "=" + variant.label + " is rejected or failed"
			} else if evidence && invalidRejected {
				p.Status = result.StatusPass
				p.Confidence = "high"
				p.Summary = fam.name + "=" + variant.label + " is accepted and exposes reasoning evidence"
			} else if evidence {
				p.Status = result.StatusWarn
				p.Confidence = "medium"
				p.Summary = fam.name + "=" + variant.label + " exposes reasoning evidence, but invalid values are accepted"
			} else if invalidRejected {
				p.Status = result.StatusWarn
				p.Confidence = "medium"
				p.Summary = fam.name + "=" + variant.label + " is accepted and validated, but no observable reasoning field/token count is returned"
			} else {
				p.Status = result.StatusWarn
				p.Confidence = "low"
				p.Summary = fam.name + "=" + variant.label + " is accepted, but may be silently ignored"
			}
			measurements = append(measurements, reasoningMeasurement{result: p, call: call})
		}
		measurements = postProcessReasoningFamily(fam.name, measurements)
		for i, m := range measurements {
			// The invalid-value canary is shared by the family; account for it once.
			if i == 0 {
				m.result.Usage = m.result.Usage.Add(invalidCall.Usage)
				m.result.DurationMS += invalidCall.Resp.Duration.Milliseconds()
			}
			out = append(out, m.result)
		}
	}
	return out
}

// Go does not permit passing a locally declared struct type to another function,
// so reasoning-family post-processing is implemented inline through this generic
// representation.
type reasoningMeasurement struct {
	result result.ParameterResult
	call   paramCall
}

func postProcessReasoningFamily(name string, measurements []reasoningMeasurement) []reasoningMeasurement {
	accepted := make([]int, 0, len(measurements))
	for i := range measurements {
		if measurements[i].result.Accepted != nil && *measurements[i].result.Accepted {
			accepted = append(accepted, i)
		}
	}
	if len(accepted) < 2 {
		return measurements
	}
	minSignal, maxSignal := int64(-1), int64(0)
	for _, idx := range accepted {
		signal := measurements[idx].call.ReasoningTokens
		if signal == 0 {
			signal = int64(len(measurements[idx].call.ReasoningText))
		}
		if minSignal < 0 || signal < minSignal {
			minSignal = signal
		}
		if signal > maxSignal {
			maxSignal = signal
		}
	}
	varies := maxSignal > minSignal && (minSignal == 0 || float64(maxSignal-minSignal)/float64(maxSignal) >= 0.20)
	if varies {
		for _, idx := range accepted {
			r := &measurements[idx].result
			r.Effective = result.Bool(true)
			r.Metrics["family_signal_varies"] = true
			if r.Status == result.StatusWarn && metricBool(r.Metrics, "invalid_value_rejected") {
				r.Status = result.StatusPass
				r.Confidence = "medium"
				r.Summary = name + "=" + r.Variant + " participates in a validated family whose observable reasoning signal changes across modes"
			} else if r.Status == result.StatusWarn && r.Confidence == "low" {
				r.Confidence = "medium"
				r.Summary += "; family-level output differs across modes"
			}
		}
	}
	if name == "reasoning_split" && len(measurements) == 2 {
		left := len(measurements[0].call.ReasoningKeys)
		right := len(measurements[1].call.ReasoningKeys)
		if left != right || strings.Contains(measurements[0].call.Content, "<think>") != strings.Contains(measurements[1].call.Content, "<think>") {
			for i := range measurements {
				r := &measurements[i].result
				r.Effective = result.Bool(true)
				r.Metrics["format_difference_observed"] = true
				if metricBool(r.Metrics, "invalid_value_rejected") {
					r.Status = result.StatusPass
					r.Confidence = "high"
					r.Summary = "reasoning_split=" + r.Variant + " produced a validated, observably different reasoning format"
				}
			}
		}
	}
	return measurements
}

func metricBool(metrics map[string]any, key string) bool {
	value, _ := metrics[key].(bool)
	return value
}

func statusFromParamCall(call paramCall) result.Status {
	if call.Err != nil && call.Resp.StatusCode == 0 {
		return result.StatusError
	}
	if providerFailureStatusCode(call.Resp.StatusCode) {
		return result.StatusError
	}
	if call.Resp.StatusCode >= 400 && call.Resp.StatusCode < 500 {
		return result.StatusUnsupported
	}
	return result.StatusFail
}

func callValues(calls []paramCall) []string {
	values := make([]string, 0, len(calls))
	for _, call := range calls {
		values = append(values, normalizeSample(call.Content))
	}
	return values
}

func uniqueCount(values []string) int {
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	return len(seen)
}

func all2xx(calls []paramCall) bool {
	if len(calls) == 0 {
		return false
	}
	for _, call := range calls {
		if !call.Resp.Is2xx() {
			return false
		}
	}
	return true
}

func sumCallDuration(calls []paramCall) int64 {
	var total int64
	for _, call := range calls {
		total += call.Resp.Duration.Milliseconds()
	}
	return total
}

func sumCallUsage(calls []paramCall) result.Usage {
	var total result.Usage
	for _, call := range calls {
		total = total.Add(call.Usage)
	}
	return total
}

func firstFailureStatus(calls []paramCall) int {
	for _, call := range calls {
		if !call.Resp.Is2xx() {
			return call.Resp.StatusCode
		}
	}
	return 0
}

func declaredContextTokens(metadata map[string]any) int64 {
	keys := map[string]bool{
		"context_length":      true,
		"context_window":      true,
		"max_context_length":  true,
		"max_model_len":       true,
		"max_sequence_length": true,
		"input_token_limit":   true,
		"max_input_tokens":    true,
		"n_ctx":               true,
	}
	var values []int64
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				if keys[strings.ToLower(key)] {
					if n := anyInt64(child); n > 0 {
						values = append(values, n)
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(metadata)
	if len(values) == 0 {
		return 0
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values[len(values)-1]
}

func anyInt64(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	case string:
		var i int64
		_, _ = fmt.Sscan(n, &i)
		return i
	default:
		return 0
	}
}

var _ = time.Second
