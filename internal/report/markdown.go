package report

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/ilyar/aiqa/internal/result"
)

func Markdown(r result.Report) string {
	var b strings.Builder
	fmt.Fprintln(&b, "# AIQA report")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "_measures observed API behavior, a passing report is not a security audit or an official certification_")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- **Generated:** %s\n", r.GeneratedAt.Format("2006-01-02 15:04:05Z07:00"))
	fmt.Fprintf(&b, "- **Tool:** `aiqa %s`", escapeInline(r.Tool.Version))
	if r.Tool.Commit != "" || r.Tool.BuildDate != "" {
		fmt.Fprintf(&b, " (`%s`, built `%s`)", escapeInline(r.Tool.Commit), escapeInline(r.Tool.BuildDate))
	}
	fmt.Fprintln(&b)
	if service := serviceLink(r.Config.BaseURL); service != "" {
		fmt.Fprintf(&b, "- **Service:** %s\n", service)
	}
	fmt.Fprintf(&b, "- **Suites:** `%s`\n", strings.Join(r.Config.Tests, ","))
	if len(r.Config.RequestedModels) > 0 {
		fmt.Fprintf(&b, "- **Requested models:** `%s` (the same global list was intersected independently with this target inventory)\n", escapeInline(strings.Join(r.Config.RequestedModels, ", ")))
	}
	fmt.Fprintf(&b, "- **Timeout:** `%s` per request\n", r.Config.Timeout)
	fmt.Fprintf(&b, "- **Profile:** `%s@%s` (`%s`) – %s\n", r.Profile.ID, r.Profile.Revision, shortSHA(r.Profile.SourceCommit), r.Profile.Scope)
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Summary")
	fmt.Fprintln(&b)
	writeExecutiveSummary(&b, r)
	if assessment := r.Summary.Assessment; assessment != nil {
		coverage := assessment.Coverage
		fmt.Fprintf(&b, "\n- **Requested coverage:** %d routes; %d executed, %d unresolved, %d excluded.\n", coverage.Expected, coverage.Executed, coverage.Unresolved, coverage.Excluded)
	}
	fmt.Fprintln(&b)
	writeRateLimit(&b, r.RateLimit)
	writeGuidance(&b, r)

	fmt.Fprintln(&b, "## Model and module details")
	fmt.Fprintln(&b)
	if nonPass := filterTests(r.Discovery.Tests, false); len(nonPass) > 0 {
		fmt.Fprintln(&b, "### Model discovery issues")
		fmt.Fprintln(&b)
		writeTestTable(&b, nonPass)
		fmt.Fprintln(&b)
	}
	if nonPass := filterTests(r.Discovery.Compatibility, false); len(nonPass) > 0 {
		fmt.Fprintln(&b, "### Provider endpoint issues")
		fmt.Fprintln(&b)
		writeTestTable(&b, nonPass)
		fmt.Fprintln(&b)
	}
	if len(r.Discovery.Models) > 0 {
		fmt.Fprintf(&b, "Discovered models: **%d**.", len(r.Discovery.Models))
		if len(r.Models) != len(r.Discovery.Models) {
			fmt.Fprint(&b, " Full model metadata is available in the JSON report.")
		}
		fmt.Fprintln(&b)
		fmt.Fprintln(&b)
	}

	fmt.Fprintln(&b, "### Model inventory and routing")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Model | Detected capabilities | Source | Confidence | Chat | Images | Embeddings | Evaluation |")
	fmt.Fprintln(&b, "|---|---|---|---:|---:|---:|---:|---|")
	for _, m := range r.Models {
		capabilities, sources, confidence := capabilityCells(m)
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s | %s | %s |\n", escapeCell(m.ID), escapeCell(capabilities), escapeCell(sources), confidence, moduleCell(m.Modules, "chat-completions"), moduleCell(m.Modules, "images-generations"), moduleCell(m.Modules, "embeddings"), statusLabelString(m.Assessment.Verdict))
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "### Model summary")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Model | Status | Required | Context | Tests |")
	fmt.Fprintln(&b, "|---|---:|---:|---:|---:|")
	for _, m := range r.Models {
		contextText := "not tested"
		if m.Context != nil {
			if m.Context.UpperBoundTokens > 0 {
				contextText = fmt.Sprintf("%s–~%s", humanInt(m.Context.LowerBoundTokens), humanInt(m.Context.UpperBoundTokens))
			} else if m.Context.LowerBoundTokens > 0 {
				contextText = "≥" + humanInt(m.Context.LowerBoundTokens)
			} else {
				contextText = string(m.Context.Status)
			}
		}
		count := m.Summary.Pass + m.Summary.Fail + m.Summary.Warn + m.Summary.Unsupported + m.Summary.Skip + m.Summary.Error
		fmt.Fprintf(&b, "| `%s` | %s | %d/%d | %s | %d |\n",
			escapeCell(m.ID), statusLabelString(m.Summary.Status),
			m.Summary.RequiredPass, m.Summary.RequiredPass+m.Summary.RequiredFail,
			escapeCell(contextText), count)
	}
	fmt.Fprintln(&b)

	for _, m := range r.Models {
		fmt.Fprintf(&b, "### `%s`\n\n", escapeInline(m.ID))
		writeSummaryTable(&b, m.Summary)
		fmt.Fprintln(&b)
		if len(m.Modules) > 0 {
			fmt.Fprintln(&b, "### Module applicability")
			fmt.Fprintln(&b)
			fmt.Fprintln(&b, "| Module | Capability | Requested | Applicability | Execution | Reason |")
			fmt.Fprintln(&b, "|---|---|---:|---:|---:|---|")
			for _, module := range m.Modules {
				fmt.Fprintf(&b, "| `%s` | `%s` | %t | %s | %s | %s |\n", escapeCell(module.ID), escapeCell(module.Capability), module.Requested, applicabilityCell(module.Applicability), executionCell(module.Execution), escapeCell(module.Reason))
			}
			fmt.Fprintln(&b)
		}
		if len(r.Models) > 1 && m.RateLimit != nil {
			writeModelRateLimit(&b, *m.RateLimit)
		}

		if nonPass := filterTests(m.Compatibility, false); len(nonPass) > 0 {
			fmt.Fprintln(&b, "#### OpenAI compatibility issues")
			fmt.Fprintln(&b)
			writeTestTable(&b, nonPass)
			fmt.Fprintln(&b)
		}
		if nonPass := filterParameters(m.Parameters, false); len(nonPass) > 0 {
			fmt.Fprintln(&b, "#### Model parameter issues")
			fmt.Fprintln(&b)
			writeParameterTable(&b, nonPass)
			fmt.Fprintln(&b)
		}
		if m.Context != nil {
			writeContext(&b, *m.Context)
		}
	}

	writeRequirementAssessment(&b, r.Requirements)
	writePassedChecks(&b, r)
	writeRawEvidence(&b, r.Guidance.Artifacts)

	fmt.Fprintln(&b, "## Methodology and limitations")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "- **PASS** means the expected protocol or observable behavior was demonstrated.")
	fmt.Fprintln(&b, "- **WARN** means the semantic effect or correctness was not proved. **SKIP** means the check did not run.")
	fmt.Fprintln(&b, "- **UNSUP** means an optional capability was explicitly rejected with a suitable client error. **N/A** means the requirement does not apply.")
	fmt.Fprintln(&b, "- **FAIL** means observed behavior violated the applicable contract. **ERROR** is an inconclusive transport, provider, or runner failure.")
	fmt.Fprintln(&b, "- Findings and recommendations are deterministic guidance derived from canonical requirements and typed evidence. They do not change conformance status or exit policy.")
	fmt.Fprintln(&b, "- Context results distinguish a measured accepted lower bound from an estimated first rejected boundary. Token counts are exact only when the provider returns `usage.prompt_tokens`.")
	fmt.Fprintln(&b, "- Temperature, `top_p`, and seed checks are statistical. A warning does not prove that a parameter is ignored; it says this run did not produce enough evidence to claim effectiveness.")
	fmt.Fprintln(&b)
	writeUsage(&b, r.Summary)
	return strings.TrimRight(b.String(), "\n")
}

func writeUsage(b *strings.Builder, summary result.Summary) {
	fmt.Fprintln(b, "## Observed API usage")
	fmt.Fprintln(b)
	fmt.Fprintf(b, "**%d prompt + %d completion = %d total tokens**", summary.Usage.PromptTokens, summary.Usage.CompletionTokens, summary.Usage.TotalTokens)
	if summary.ContextProbeTokens > 0 {
		fmt.Fprintf(b, ". Context probing additionally sent approximately **%d prompt tokens**.", summary.ContextProbeTokens)
	}
	fmt.Fprintln(b)
}

func writeModelRateLimit(b *strings.Builder, s result.RateLimitStats) {
	fmt.Fprintln(b, "#### Rate-limit and latency")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "- Pacer state: shared adaptive scheduler")
	fmt.Fprintf(b, "- Logical requests: %d\n", s.LogicalRequests)
	fmt.Fprintf(b, "- Physical attempts: %d\n", s.PhysicalAttempts)
	fmt.Fprintf(b, "- Successful native requests: %d\n", s.SuccessfulRequests)
	fmt.Fprintf(b, "- 429 responses: %d\n", s.RateLimited)
	fmt.Fprintf(b, "- Retry attempts: %d\n", s.RetryAttempts)
	fmt.Fprintf(b, "- Retries recovered: %d\n", s.RetriesRecovered)
	fmt.Fprintf(b, "- Retries exhausted: %d\n", s.RetriesExhausted)
	fmt.Fprintf(b, "- Observed safe request rate: %s\n", safeRateText(s))
	fmt.Fprintf(b, "- Observed safe concurrency: %s\n", safeConcurrencyText(s))
	fmt.Fprintf(b, "- Unique successful HTTP latency (n=%d): %s\n", s.LatencySamples, latencyText(s))
	fmt.Fprintf(b, "- Backoff time: %d ms\n\n", s.BackoffMS)
}

func writeRateLimit(b *strings.Builder, s result.RateLimitStats) {
	fmt.Fprintln(b, "## Rate-limit and latency")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "- Pacer state: adaptive target-owned transport")
	fmt.Fprintf(b, "- Logical requests: %d\n", s.LogicalRequests)
	fmt.Fprintf(b, "- Physical attempts: %d\n", s.PhysicalAttempts)
	fmt.Fprintf(b, "- Successful native requests: %d\n", s.SuccessfulRequests)
	fmt.Fprintf(b, "- 429 responses: %d\n", s.RateLimited)
	fmt.Fprintf(b, "- Retry attempts: %d\n", s.RetryAttempts)
	fmt.Fprintf(b, "- Retries recovered: %d\n", s.RetriesRecovered)
	fmt.Fprintf(b, "- Retries exhausted: %d\n", s.RetriesExhausted)
	fmt.Fprintf(b, "- Observed safe request rate: %s\n", safeRateText(s))
	fmt.Fprintf(b, "- Observed safe concurrency: %s\n", safeConcurrencyText(s))
	fmt.Fprintf(b, "- Unique successful HTTP latency (n=%d): %s\n", s.LatencySamples, latencyText(s))
	fmt.Fprintf(b, "- Backoff time: %d ms\n\n", s.BackoffMS)
}

func safeRateText(s result.RateLimitStats) string {
	if s.SuccessfulRequests == 0 {
		return "N/S"
	}
	return fmt.Sprintf("%.2f req/s", s.SafeRate)
}

func safeConcurrencyText(s result.RateLimitStats) string {
	if s.SuccessfulRequests == 0 {
		return "N/S"
	}
	return strconv.Itoa(s.SafeConcurrency)
}

func latencyText(s result.RateLimitStats) string {
	if s.LatencySamples == 0 {
		return "N/S"
	}
	return fmt.Sprintf("mean %d ms; P50 %d ms; P95 %d ms", s.MeanLatencyMS, s.P50LatencyMS, s.P95LatencyMS)
}

func capabilityCells(m result.ModelReport) (string, string, string) {
	if len(m.Capabilities) == 0 {
		return "unknown", "insufficient metadata", "low"
	}
	var capabilities, sources []string
	confidence := "high"
	for _, item := range m.Capabilities {
		capabilities = append(capabilities, item.ID)
		sources = append(sources, item.Source)
		if item.Confidence == "low" {
			confidence = "low"
		} else if item.Confidence == "medium" && confidence == "high" {
			confidence = "medium"
		}
	}
	return strings.Join(capabilities, ", "), strings.Join(uniqueStrings(sources), ", "), confidence
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func moduleCell(modules []result.ModuleAssessment, id string) string {
	for _, module := range modules {
		if module.ID == id {
			if module.Applicability == result.ApplicabilityNotApplicable {
				return "N/A"
			}
			if module.Applicability == result.ApplicabilityUnknown {
				return "UNKNOWN"
			}
			if module.Execution == result.ExecutionExecuted {
				return "EXECUTED"
			}
			return "SKIP"
		}
	}
	return "–"
}
func applicabilityCell(value result.Applicability) string {
	switch value {
	case result.ApplicabilityApplicable:
		return "APPLICABLE"
	case result.ApplicabilityNotApplicable:
		return "N/A"
	default:
		return "UNKNOWN"
	}
}
func executionCell(value result.ExecutionState) string {
	if value == "" {
		return "–"
	}
	return strings.ToUpper(string(value))
}

func writeSummaryTable(b *strings.Builder, s result.Summary) {
	fmt.Fprintln(b, "| Overall | Pass | Fail | Warn | UNSUP | Skip | Error | Required |")
	fmt.Fprintln(b, "|---|---:|---:|---:|---:|---:|---:|---:|")
	fmt.Fprintf(b, "| %s | %d | %d | %d | %d | %d | %d | %d/%d |\n",
		statusLabelString(s.Status), s.Pass, s.Fail, s.Warn, s.Unsupported, s.Skip, s.Error,
		s.RequiredPass, s.RequiredPass+s.RequiredFail)
}

func writeTestTable(b *strings.Builder, tests []result.TestResult) {
	fmt.Fprintln(b, "| Status | Required | Category | Test | Summary | HTTP | Tokens | Time |")
	fmt.Fprintln(b, "|---:|---:|---|---|---|---:|---:|---:|")
	for _, t := range tests {
		httpStatus := "–"
		if t.HTTP.StatusCode > 0 {
			httpStatus = strconv.Itoa(t.HTTP.StatusCode)
		}
		tokens := "–"
		if t.Usage.TotalTokens > 0 {
			tokens = humanInt(t.Usage.TotalTokens)
		}
		required := "no"
		if t.Required {
			required = "yes"
		}
		summary := t.Summary
		if t.Observation != "" {
			summary += " – " + t.Observation
		}
		if t.Error != "" {
			summary += " – error: " + t.Error
		}
		fmt.Fprintf(b, "| %s | %s | %s | `%s` | %s | %s | %s | %d ms |\n",
			statusLabel(t.Status), required, escapeCell(t.Category), escapeCell(t.ID), escapeCell(summary), httpStatus, tokens, t.DurationMS)
	}
}

func writeParameterTable(b *strings.Builder, params []result.ParameterResult) {
	fmt.Fprintln(b, "| Status | Parameter | Variant | Accepted | Effective | Confidence | Summary | Tokens | Time |")
	fmt.Fprintln(b, "|---:|---|---|---:|---:|---|---|---:|---:|")
	for _, p := range params {
		accepted := triBool(p.Accepted)
		effective := triBool(p.Effective)
		tokens := "–"
		if p.Usage.TotalTokens > 0 {
			tokens = humanInt(p.Usage.TotalTokens)
		}
		summary := p.Summary
		if p.Observation != "" {
			summary += " – " + p.Observation
		}
		if p.Error != "" {
			summary += " – error: " + p.Error
		}
		fmt.Fprintf(b, "| %s | `%s` | `%s` | %s | %s | %s | %s | %s | %d ms |\n",
			statusLabel(p.Status), escapeCell(p.Name), escapeCell(p.Variant), accepted, effective,
			escapeCell(p.Confidence), escapeCell(summary), tokens, p.DurationMS)
	}
}

func writeContext(b *strings.Builder, c result.ContextProbeResult) {
	fmt.Fprintln(b, "#### Context window")
	fmt.Fprintln(b)
	fmt.Fprintf(b, "**%s:** %s\n\n", statusLabel(c.Status), escapeCell(c.Summary))
	fmt.Fprintln(b, "| Metadata limit | Server-reported limit | Accepted lower bound | First rejected estimate | Precision | Confidence | Probes | Probe tokens | Output cap |")
	fmt.Fprintln(b, "|---:|---:|---:|---:|---:|---|---:|---:|---|")
	fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %d | %s | `%s` |\n",
		humanOrDash(c.DeclaredTokens), humanOrDash(c.ServerLimitTokens), humanOrDash(c.LowerBoundTokens), humanOrDash(c.UpperBoundTokens),
		humanOrDash(c.PrecisionTokens), escapeCell(c.Confidence), c.ProbeCount, humanOrDash(c.ConsumedTokens), escapeCell(c.OutputLimitParameter))
	fmt.Fprintln(b)
	if len(c.Samples) == 0 {
		return
	}
	fmt.Fprintln(b, "<details><summary>Context probe samples</summary>")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "| Payload bytes | Reported prompt tokens | Estimated prompt tokens | Accepted | Overflow | HTTP | Time | Error |")
	fmt.Fprintln(b, "|---:|---:|---:|---:|---:|---:|---:|---|")
	for _, s := range c.Samples {
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %d | %d ms | %s |\n",
			humanInt(int64(s.PayloadBytes)), humanOrDash(s.ReportedPromptTokens), humanOrDash(s.EstimatedPromptTokens),
			yesNo(s.Accepted), yesNo(s.Overflow), s.StatusCode, s.DurationMS, escapeCell(s.Error))
	}
	fmt.Fprintln(b)
	fmt.Fprintln(b, "</details>")
	fmt.Fprintln(b)
}

func statusLabel(status result.Status) string {
	switch status {
	case result.StatusPass:
		return "PASS"
	case result.StatusFail:
		return "FAIL"
	case result.StatusWarn:
		return "WARN"
	case result.StatusUnsupported:
		return "UNSUP"
	case result.StatusSkip:
		return "SKIP"
	case result.StatusError:
		return "ERROR"
	default:
		return escapeCell(string(status))
	}
}

func statusLabelString(status string) string {
	switch status {
	case "pass":
		return "PASS"
	case "pass-with-gaps":
		return "PASS-WITH-GAPS"
	case "partial":
		return "PARTIAL"
	case "fail":
		return "FAIL"
	case "inconclusive":
		return "INCONCLUSIVE"
	case "not-evaluated":
		return "NOT-EVALUATED"
	default:
		return escapeCell(status)
	}
}

func triBool(v *bool) string {
	if v == nil {
		return "–"
	}
	if *v {
		return "yes"
	}
	return "no"
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func humanOrDash(n int64) string {
	if n <= 0 {
		return "–"
	}
	return humanInt(n)
}

func humanInt(n int64) string {
	negative := n < 0
	if negative {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if negative {
		return "-" + s
	}
	return s
}

func shortSHA(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

func escapeCell(s string) string {
	return markdownLiteral(s)
}

func escapeInline(s string) string {
	return markdownLiteral(s)
}

// markdownLiteral makes provider-controlled text inert in code spans and table
// cells: it cannot terminate a span, become a link/HTML element, or use bidi
// controls to change what an operator sees.
func markdownLiteral(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r == '\r' || r == '\n':
			b.WriteByte(' ')
		case r == '`':
			// Backslashes do not escape a backtick inside a CommonMark code span.
			// Retain a visible literal representation without a delimiter rune.
			b.WriteString(`\u0060`)
		case r < 0x20 || (r >= 0x7f && r <= 0x9f) || r == '\u2028' || r == '\u2029' || isBidiControl(r):
			fmt.Fprintf(&b, `\\u%04x`, r)
		case strings.ContainsRune("\\\\[]<>|", r):
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '&':
			b.WriteString("&amp;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isBidiControl(r rune) bool {
	return r == '\u061c' || (r >= '\u200e' && r <= '\u200f') || (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069')
}

// Notes are plain text and can contain provider-controlled model identifiers.
func escapeNote(s string) string {
	return strings.NewReplacer(
		"\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_",
		"[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "&", "&amp;",
		"\r", " ", "\n", " ",
	).Replace(s)
}

// serviceLink renders only the origin's host so Markdown identifies the
// provider without exposing API paths, query parameters, or credentials.
func serviceLink(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
		return ""
	}
	origin := (&url.URL{Scheme: u.Scheme, Host: u.Host}).String()
	return fmt.Sprintf("[%s](%s)", escapeInline(u.Host), origin)
}

func codeList(values []string) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, "`"+escapeInline(value)+"`")
	}
	return strings.Join(out, ", ")
}

func truncateMD(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n…"
}
