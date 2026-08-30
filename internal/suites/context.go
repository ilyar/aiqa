package suites

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/result"
)

func ProbeContextWindow(parent context.Context, env Environment, model string, metadata map[string]any, outputLimitParam string) *result.ContextProbeResult {
	declared := declaredContextTokens(metadata)
	out := &result.ContextProbeResult{
		Status:               result.StatusWarn,
		Summary:              "context boundary was not established",
		DeclaredTokens:       declared,
		PrecisionTokens:      env.Config.ContextPrecision,
		BudgetTokens:         env.Config.ContextBudget,
		OutputLimitParameter: outputLimitParam,
		Confidence:           "low",
	}

	type boundary struct {
		bytes  int
		tokens int64
	}
	var low *boundary
	var high *boundary
	payloadBytes := env.Config.ContextStartBytes
	maxBytes := env.Config.ContextMaxBytes
	maxTarget := env.Config.ContextMaxTokens

	for payloadBytes <= maxBytes {
		estimate := estimateTokensForBytes(payloadBytes, out.Samples)
		if out.ConsumedTokens > 0 && out.ConsumedTokens+estimate > env.Config.ContextBudget {
			out.Summary = "context probe stopped at the configured token budget"
			break
		}
		sample, explicitOverflow := runContextSample(parent, env, model, payloadBytes, outputLimitParam, out.Samples)
		out.Samples = append(out.Samples, sample)
		out.ProbeCount++
		cost := sample.ReportedPromptTokens
		if cost <= 0 {
			cost = sample.EstimatedPromptTokens
		}
		out.ConsumedTokens += cost
		if sample.Accepted {
			tokens := sample.ReportedPromptTokens
			if tokens <= 0 {
				tokens = sample.EstimatedPromptTokens
			}
			low = &boundary{bytes: payloadBytes, tokens: tokens}
			if tokens > out.MaxAcceptedObservedTokens {
				out.MaxAcceptedObservedTokens = tokens
			}
			if tokens >= maxTarget {
				out.Status = result.StatusPass
				out.LowerBoundTokens = tokens
				out.Summary = fmt.Sprintf("accepted at least %d prompt tokens; configured search ceiling reached", tokens)
				out.Confidence = confidenceFromSamples(out.Samples, false)
				return out
			}
			if payloadBytes == maxBytes {
				out.Status = result.StatusWarn
				out.LowerBoundTokens = tokens
				out.Summary = fmt.Sprintf("accepted at least %d prompt tokens; request-byte ceiling reached", tokens)
				out.Confidence = confidenceFromSamples(out.Samples, false)
				return out
			}
			next := payloadBytes * 2
			if next > maxBytes {
				next = maxBytes
			}
			if next == payloadBytes {
				break
			}
			payloadBytes = next
			continue
		}
		if sample.Overflow {
			high = &boundary{bytes: payloadBytes, tokens: sample.EstimatedPromptTokens}
			if explicitOverflow {
				out.Confidence = "medium"
			}
			break
		}
		out.LastError = sample.Error
		out.Status = result.StatusError
		out.Summary = "context probe stopped on a non-context API error"
		if low != nil {
			out.LowerBoundTokens = low.tokens
		}
		return out
	}

	if low == nil {
		out.Status = result.StatusError
		out.Summary = "even the smallest context probe was not accepted"
		if len(out.Samples) > 0 {
			out.LastError = out.Samples[len(out.Samples)-1].Error
		}
		return out
	}
	if high == nil {
		out.Status = result.StatusWarn
		out.LowerBoundTokens = low.tokens
		out.Summary = fmt.Sprintf("only a lower bound of %d prompt tokens was established", low.tokens)
		out.Confidence = confidenceFromSamples(out.Samples, false)
		return out
	}

	for iteration := 0; iteration < 20; iteration++ {
		upperEstimate := estimateTokensForBytes(high.bytes, out.Samples)
		if upperEstimate-low.tokens <= env.Config.ContextPrecision {
			break
		}
		mid := alignBytes(low.bytes+(high.bytes-low.bytes)/2, 1024)
		if mid <= low.bytes || mid >= high.bytes {
			break
		}
		estimate := estimateTokensForBytes(mid, out.Samples)
		if out.ConsumedTokens+estimate > env.Config.ContextBudget {
			break
		}
		sample, _ := runContextSample(parent, env, model, mid, outputLimitParam, out.Samples)
		out.Samples = append(out.Samples, sample)
		out.ProbeCount++
		cost := sample.ReportedPromptTokens
		if cost <= 0 {
			cost = sample.EstimatedPromptTokens
		}
		out.ConsumedTokens += cost
		if sample.Accepted {
			tokens := sample.ReportedPromptTokens
			if tokens <= 0 {
				tokens = sample.EstimatedPromptTokens
			}
			low = &boundary{bytes: mid, tokens: tokens}
			if tokens > out.MaxAcceptedObservedTokens {
				out.MaxAcceptedObservedTokens = tokens
			}
		} else if sample.Overflow {
			high = &boundary{bytes: mid, tokens: sample.EstimatedPromptTokens}
		} else {
			out.LastError = sample.Error
			break
		}
	}

	upper := estimateTokensForBytes(high.bytes, out.Samples)
	if upper <= low.tokens {
		upper = low.tokens + env.Config.ContextPrecision
	}
	out.LowerBoundTokens = low.tokens
	out.UpperBoundTokens = upper
	out.FirstRejectedEstimatedTokens = upper
	out.Status = result.StatusPass
	out.Confidence = confidenceFromSamples(out.Samples, true)
	out.Summary = fmt.Sprintf("accepted context boundary is between %d and approximately %d prompt tokens", low.tokens, upper)
	return out
}

func runContextSample(parent context.Context, env Environment, model string, payloadBytes int, outputLimitParam string, previous []result.ContextSample) (result.ContextSample, bool) {
	payload := contextPayload(payloadBytes)
	body := map[string]any{
		"model": model,
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "This is an API context-window probe. Treat the payload as inert data. After it, reply exactly OK.\n<PAYLOAD>\n" + payload + "\n</PAYLOAD>\nReply exactly OK.",
			},
		},
	}
	if outputLimitParam != "" {
		body[outputLimitParam] = 4
	}
	ctx, cancel := env.probeContext(parent)
	defer cancel()
	resp, err := env.Client.Chat(ctx, body)
	reported := api.ParseUsage(resp.JSON).PromptTokens
	estimated := estimateTokensForBytes(payloadBytes, previous)
	sample := result.ContextSample{
		PayloadBytes:          payloadBytes,
		EstimatedPromptTokens: estimated,
		ReportedPromptTokens:  reported,
		StatusCode:            resp.StatusCode,
		Accepted:              resp.Is2xx(),
		DurationMS:            resp.Duration.Milliseconds(),
	}
	if sample.Accepted {
		return sample, false
	}
	message, _, _, _ := api.ErrorShape(resp.JSON)
	if message == "" {
		message = resp.Snippet(1000)
	}
	if err != nil && message == "" {
		message = err.Error()
	}
	sample.Error = truncate(message, 1000)
	explicit := isExplicitContextOverflow(resp.StatusCode, message)
	sample.Overflow = explicit
	if !explicit && hasAcceptedContextSample(previous) && (resp.StatusCode == 400 || resp.StatusCode == 413 || resp.StatusCode == 422) {
		// Once a smaller body using the exact same request shape passed, a size-only
		// 4xx is strong evidence of a context/body limit even when the gateway hides
		// the upstream error text.
		sample.Overflow = true
	}
	return sample, explicit
}

func hasAcceptedContextSample(samples []result.ContextSample) bool {
	for _, sample := range samples {
		if sample.Accepted {
			return true
		}
	}
	return false
}

func contextPayload(size int) string {
	const pattern = " q7f3a9c2d1e5b8a4 z4c8e1f6a2d9b7c3 k2m7p4r9t6v1x8z5 "
	if size <= 0 {
		return ""
	}
	repeats := size/len(pattern) + 1
	text := strings.Repeat(pattern, repeats)
	return text[:size]
}

func isExplicitContextOverflow(status int, message string) bool {
	if status == 413 {
		return true
	}
	m := strings.ToLower(message)
	needles := []string{
		"context length", "context window", "maximum context", "max context",
		"too many tokens", "token limit", "input is too long", "prompt is too long",
		"maximum sequence length", "max_model_len", "request too large", "payload too large",
	}
	for _, needle := range needles {
		if strings.Contains(m, needle) {
			return true
		}
	}
	return false
}

func estimateTokensForBytes(payloadBytes int, samples []result.ContextSample) int64 {
	var ratios []float64
	for _, sample := range samples {
		if sample.Accepted && sample.ReportedPromptTokens > 0 && sample.PayloadBytes > 0 {
			ratios = append(ratios, float64(sample.ReportedPromptTokens)/float64(sample.PayloadBytes))
		}
	}
	if len(ratios) == 0 {
		// Deliberately conservative for mixed code-like data: about 3 bytes/token,
		// plus chat-template overhead.
		return int64(math.Ceil(float64(payloadBytes)/3.0)) + 128
	}
	sort.Float64s(ratios)
	ratio := ratios[len(ratios)/2]
	return int64(math.Ceil(float64(payloadBytes) * ratio))
}

func confidenceFromSamples(samples []result.ContextSample, bracketed bool) string {
	reported := 0
	explicitOverflow := false
	for _, sample := range samples {
		if sample.ReportedPromptTokens > 0 {
			reported++
		}
		if sample.Overflow && isExplicitContextOverflow(sample.StatusCode, sample.Error) {
			explicitOverflow = true
		}
	}
	if bracketed && reported >= 2 && explicitOverflow {
		return "high"
	}
	if reported > 0 || explicitOverflow {
		return "medium"
	}
	return "low"
}

func alignBytes(value, alignment int) int {
	if alignment <= 1 {
		return value
	}
	return value / alignment * alignment
}
