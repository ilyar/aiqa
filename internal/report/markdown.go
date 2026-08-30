package report

import (
	"fmt"
	"sort"
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
	fmt.Fprintf(&b, "- **Endpoint:** `%s`\n", escapeInline(r.Config.BaseURL))
	fmt.Fprintf(&b, "- **Suites:** `%s`\n", strings.Join(r.Config.Tests, ","))
	fmt.Fprintf(&b, "- **Timeout:** `%s` per request\n", r.Config.Timeout)
	fmt.Fprintf(&b, "- **Profile:** `%s@%s` (`%s`) – %s\n", r.Profile.ID, r.Profile.Revision, shortSHA(r.Profile.SourceCommit), r.Profile.Scope)
	fmt.Fprintf(&b, "- **External checks:** %s\n", externalChecks(r))
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Summary")
	fmt.Fprintln(&b)
	writeSummaryTable(&b, r.Summary)
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Observed API usage: **%d prompt + %d completion = %d total tokens**", r.Summary.Usage.PromptTokens, r.Summary.Usage.CompletionTokens, r.Summary.Usage.TotalTokens)
	if r.Summary.ContextProbeTokens > 0 {
		fmt.Fprintf(&b, ". Context probing additionally sent approximately **%d prompt tokens**.", r.Summary.ContextProbeTokens)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Model discovery")
	fmt.Fprintln(&b)
	writeTestTable(&b, r.Discovery.Tests)
	fmt.Fprintln(&b)
	if len(r.Discovery.Models) > 0 {
		fmt.Fprintf(&b, "Discovered models: **%d**.", len(r.Discovery.Models))
		if len(r.Models) != len(r.Discovery.Models) {
			fmt.Fprint(&b, " Full model metadata is available in the JSON report.")
		}
		fmt.Fprintln(&b)
		fmt.Fprintln(&b)
	}

	fmt.Fprintln(&b, "## Models")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Model | Status | Score | Required | Context | Tests |")
	fmt.Fprintln(&b, "|---|---:|---:|---:|---:|---:|")
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
		fmt.Fprintf(&b, "| `%s` | %s | %.1f%% | %d/%d | %s | %d |\n",
			escapeCell(m.ID), statusLabelString(m.Summary.Status), m.Summary.Score,
			m.Summary.RequiredPass, m.Summary.RequiredPass+m.Summary.RequiredFail,
			escapeCell(contextText), count)
	}
	fmt.Fprintln(&b)

	for _, m := range r.Models {
		fmt.Fprintf(&b, "## `%s`\n\n", escapeInline(m.ID))
		writeSummaryTable(&b, m.Summary)
		fmt.Fprintln(&b)

		if len(m.Compatibility) > 0 {
			fmt.Fprintln(&b, "### OpenAI compatibility")
			fmt.Fprintln(&b)
			writeTestTable(&b, m.Compatibility)
			fmt.Fprintln(&b)
		}
		if len(m.Parameters) > 0 {
			fmt.Fprintln(&b, "### Model parameters")
			fmt.Fprintln(&b)
			writeParameterTable(&b, m.Parameters)
			fmt.Fprintln(&b)
		}
		if m.Context != nil {
			writeContext(&b, *m.Context)
		}
		if len(m.External) > 0 {
			fmt.Fprintln(&b, "### Official SDK")
			fmt.Fprintln(&b)
			fmt.Fprintln(&b, "| Suite | Status | Summary | Duration |")
			fmt.Fprintln(&b, "|---|---:|---|---:|")
			for _, e := range m.External {
				suite := e.Suite
				if suite == "" {
					suite = "runner"
				}
				fmt.Fprintf(&b, "| `%s` | %s | %s | %d ms |\n", escapeCell(suite), statusLabel(e.Status), escapeCell(e.Summary), e.DurationMS)
			}
			fmt.Fprintln(&b)
			for _, e := range m.External {
				if e.Output != "" && e.Status != result.StatusPass {
					fmt.Fprintln(&b, "<details><summary>External checker output</summary>")
					fmt.Fprintln(&b)
					fmt.Fprintln(&b, "```text")
					fmt.Fprintln(&b, truncateMD(e.Output, 16000))
					fmt.Fprintln(&b, "```")
					fmt.Fprintln(&b, "</details>")
					fmt.Fprintln(&b)
				}
			}
		}
	}

	fmt.Fprintln(&b, "## Interpretation")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "- **Pass** means the expected protocol or observable behavior was demonstrated.")
	fmt.Fprintln(&b, "- **Warn** means the request was accepted, but semantic effect could not be proved, validation was lax, or evidence was statistical.")
	fmt.Fprintln(&b, "- **N/S** means an explicit 4xx rejection of an optional feature. It is a capability gap, not necessarily a broken core API.")
	fmt.Fprintln(&b, "- **Fail** means a required contract or an accepted feature behaved incompatibly. **Error** is an inconclusive transport/provider failure.")
	fmt.Fprintln(&b, "- Context results distinguish a measured accepted lower bound from an estimated first rejected boundary. Token counts are exact only when the provider returns `usage.prompt_tokens`.")
	fmt.Fprintln(&b, "- Temperature, `top_p`, and seed checks are statistical. A warning does not prove that a parameter is ignored; it says this run did not produce enough evidence to claim effectiveness.")
	return strings.TrimRight(b.String(), "\n")
}

func externalChecks(r result.Report) string {
	checks := map[string]bool{}
	for _, model := range r.Models {
		for _, check := range model.External {
			if check.Suite != "" {
				checks[check.Tool] = true
			}
		}
	}
	if len(checks) == 0 {
		return "not setup"
	}
	names := make([]string, 0, len(checks))
	for name := range checks {
		switch name {
		case "official-SDK":
			names = append(names, "Official SDK")
		default:
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func writeSummaryTable(b *strings.Builder, s result.Summary) {
	fmt.Fprintln(b, "| Overall | Score | Pass | Fail | Warn | Unsupported | Skip | Error | Required |")
	fmt.Fprintln(b, "|---|---:|---:|---:|---:|---:|---:|---:|---:|")
	fmt.Fprintf(b, "| %s | %.1f%% | %d | %d | %d | %d | %d | %d | %d/%d |\n",
		statusLabelString(s.Status), s.Score, s.Pass, s.Fail, s.Warn, s.Unsupported, s.Skip, s.Error,
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
	fmt.Fprintln(b, "### Context window")
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
		return "N/S"
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
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func escapeInline(s string) string {
	return strings.ReplaceAll(s, "`", "\\`")
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
