package report

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/result"
)

func WriteV2(r result.ReportV2, cfg config.Config) ([]string, error) {
	result.RedactSecrets(&r, reportSecrets(cfg)...)
	var paths []string
	for _, format := range cfg.Formats {
		var data []byte
		var ext string
		var err error
		switch format {
		case "json":
			data, err = json.MarshalIndent(r, "", "  ")
			ext = ".json"
		case "markdown":
			data = []byte(MarkdownV2(r))
			ext = ".md"
		default:
			return paths, fmt.Errorf("unsupported format %q", format)
		}
		if err != nil {
			return paths, err
		}
		path := cfg.Name + ext
		if err := atomicWrite(path, append(data, '\n'), 0o644); err != nil {
			return paths, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func MarkdownV2(r result.ReportV2) string {
	var b strings.Builder
	fmt.Fprintln(&b, "# AIQA compatibility report")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "_Measures observed API behavior; a passing report is not a security audit or an official certification._")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- **Generated:** %s\n", r.GeneratedAt.Format("2006-01-02 15:04:05Z07:00"))
	fmt.Fprintf(&b, "- **Tool:** `aiqa %s`\n", escapeInline(r.Tool.Version))
	fmt.Fprintf(&b, "- **Suites:** `%s`\n", strings.Join(r.Config.Tests, ","))
	fmt.Fprintf(&b, "- **Timeout:** `%s` per request\n\n", r.Config.Timeout)
	fmt.Fprintln(&b, "## Targets")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Target | Protocol | Profile | Verdict | Core | Optional | Evidence | Models | Actions |")
	fmt.Fprintln(&b, "|---|---|---|---:|---:|---:|---:|---:|---:|")
	for _, target := range r.Targets {
		verdict, core, optional, evidence := targetVerdict(target)
		fmt.Fprintf(&b, "| `%s` | %s | `%s@%s` | %s | %s | %s | %s | %d | %d |\n", escapeCell(target.ID), protocolName(target.Protocol), escapeCell(target.Profile.ID), escapeCell(target.Profile.Revision), statusLabelString(verdict), core, optional, evidence, len(target.Models), len(target.Guidance.Recommendations))
	}
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "**Run verdict:** %s\n\n", statusLabelString(r.Summary.Verdict))
	fmt.Fprintln(&b, "Targets are assessed independently. Percentages are not combined into a cross-protocol score.")
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Rate-limit and latency")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Target | Successful | 429 | Recovered retries | Backoff | Safe req/s | Safe concurrency | Mean | P50 | P95 |")
	fmt.Fprintln(&b, "|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
	for _, target := range r.Targets {
		s := target.RateLimit
		fmt.Fprintf(&b, "| `%s` | %d | %d | %d | %dms | %.2f | %d | %dms | %dms | %dms |\n", escapeCell(target.ID), s.SuccessfulRequests, s.RateLimited, s.RetriesRecovered, s.BackoffMS, s.SafeRate, s.SafeConcurrency, s.MeanLatencyMS, s.P50LatencyMS, s.P95LatencyMS)
	}
	fmt.Fprintln(&b)

	for _, target := range r.Targets {
		fmt.Fprintf(&b, "## Target: %s\n\n", protocolName(target.Protocol))
		fmt.Fprintf(&b, "- **Target ID:** `%s`\n", escapeInline(target.ID))
		fmt.Fprintf(&b, "- **Profile:** `%s@%s`\n", escapeInline(target.Profile.ID), escapeInline(target.Profile.Revision))
		if targetConfigNormalized(r.Config.Targets, target.ID) {
			fmt.Fprintln(&b, "- **Base URL:** normalized by removing one trailing `/v1`; Anthropic paths append `/v1` themselves")
		}
		if target.Profile.APIVersion != "" {
			fmt.Fprintf(&b, "- **API version:** `%s`\n", escapeInline(target.Profile.APIVersion))
		}
		fmt.Fprintf(&b, "- **External checks:** %s\n", targetExternalChecks(target))
		fmt.Fprintf(&b, "- **Discovered models:** %d\n\n", target.Discovery.ModelCount)
		writeTargetGuidance(&b, target)
		writeRequirementAssessment(&b, target.Requirements)
		writeTargetModels(&b, target)
	}

	fmt.Fprintln(&b, "## Observed API usage")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Provider-reported token usage is normalized to common input and output columns. For Anthropic, cached input is reported separately from uncached input. Thinking/reasoning tokens are a subset of output and are not added twice.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Target | Input | Output | Cache creation | Cache read | Thinking/reasoning | Total |")
	fmt.Fprintln(&b, "|---|---:|---:|---:|---:|---:|---:|")
	var combined observedUsage
	for _, target := range r.Targets {
		u := normalizeObservedUsage(target.Protocol, target.Summary.Usage)
		combined = combined.add(u)
		fmt.Fprintf(&b, "| `%s` | %d | %d | %d | %d | %d | **%d** |\n", escapeCell(target.ID), u.input, u.output, u.cacheCreation, u.cacheRead, u.reasoning, u.total)
	}
	if len(r.Targets) > 1 {
		fmt.Fprintf(&b, "| **All targets** | **%d** | **%d** | **%d** | **%d** | **%d** | **%d** |\n", combined.input, combined.output, combined.cacheCreation, combined.cacheRead, combined.reasoning, combined.total)
	}
	return b.String()
}

type observedUsage struct {
	input         int64
	output        int64
	cacheCreation int64
	cacheRead     int64
	reasoning     int64
	total         int64
}

func normalizeObservedUsage(protocol string, usage result.Usage) observedUsage {
	row := observedUsage{
		cacheCreation: usage.CacheCreationInputTokens,
		cacheRead:     usage.CacheReadInputTokens,
		reasoning:     usage.ThinkingTokens + usage.ReasoningTokens,
	}
	switch protocol {
	case "anthropic":
		row.input, row.output = usage.InputTokens, usage.OutputTokens
		row.total = row.input + row.output + row.cacheCreation + row.cacheRead
	default:
		row.input, row.output = usage.PromptTokens, usage.CompletionTokens
		row.total = usage.TotalTokens
		if row.total == 0 {
			row.total = row.input + row.output
		}
	}
	return row
}

func (u observedUsage) add(v observedUsage) observedUsage {
	return observedUsage{
		input:         u.input + v.input,
		output:        u.output + v.output,
		cacheCreation: u.cacheCreation + v.cacheCreation,
		cacheRead:     u.cacheRead + v.cacheRead,
		reasoning:     u.reasoning + v.reasoning,
		total:         u.total + v.total,
	}
}

func targetConfigNormalized(configs []result.TargetPublicConfig, targetID string) bool {
	for _, config := range configs {
		if config.ID == targetID {
			return config.BaseURLNormalized
		}
	}
	return false
}

func targetExternalChecks(target result.TargetReport) string {
	var entries []result.ExternalResult
	entries = append(entries, target.Discovery.External...)
	for _, model := range target.Models {
		entries = append(entries, model.External...)
	}
	var tools []string
	for _, entry := range entries {
		if entry.Status == result.StatusSkip && entry.Suite == "" {
			continue
		}
		if entry.Tool != "" {
			tools = append(tools, entry.Tool)
		}
	}
	tools = uniqueStrings(tools)
	if len(tools) == 0 {
		return "not setup"
	}
	if target.Protocol == "anthropic" {
		for _, tool := range tools {
			if strings.Contains(tool, "anthropic-official-sdk") {
				return "[Official Anthropic Go SDK v1.68.0](https://github.com/anthropics/anthropic-sdk-go/releases/tag/v1.68.0)"
			}
		}
	}
	return strings.Join(tools, ", ")
}

func targetVerdict(target result.TargetReport) (string, string, string, string) {
	if target.Summary.Assessment == nil {
		return target.Summary.Status, "N/S", "N/S", "N/S"
	}
	a := target.Summary.Assessment
	return a.Verdict, requirementMeasureCell(a.Core), requirementMeasureCell(a.Optional), evidenceMeasureCell(a.Evidence)
}

func protocolName(protocol string) string {
	if protocol == "openai" {
		return "OpenAI"
	}
	if protocol == "anthropic" {
		return "Anthropic"
	}
	return protocol
}

func writeTargetGuidance(b *strings.Builder, target result.TargetReport) {
	if len(target.Guidance.Recommendations) == 0 {
		fmt.Fprintln(b, "### Actions")
		fmt.Fprintln(b)
		fmt.Fprintln(b, "No actionable compatibility issue was identified.")
		fmt.Fprintln(b)
		return
	}
	fmt.Fprintln(b, "### Actions")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "| PRI | Recommendation | Owner | Expected impact | Confidence |")
	fmt.Fprintln(b, "|:---:|---|---|---|---:|")
	for _, recommendation := range target.Guidance.Recommendations {
		fmt.Fprintf(b, "| %s | %s | `%s` | %s | %s |\n",
			strings.ToUpper(recommendation.Priority), escapeCell(recommendation.Title), escapeCell(recommendation.Target.Component),
			escapeCell(impactSummary(recommendation.PotentialImpact)), strings.ToUpper(recommendation.Confidence))
	}
	fmt.Fprintln(b)
	for _, recommendation := range target.Guidance.Recommendations {
		fmt.Fprintf(b, "#### %s · %s\n\n", strings.ToUpper(recommendation.Priority), escapeCell(recommendation.Title))
		fmt.Fprintf(b, "**Why this matters:** %s\n\n", escapeCell(recommendation.Why))
		if recommendation.Observed != "" {
			fmt.Fprintf(b, "**Observed:** %s\n\n", escapeCell(recommendation.Observed))
		}
		if len(recommendation.Steps) > 0 {
			fmt.Fprintln(b, "**Change or investigate:**")
			fmt.Fprintln(b)
			for index, step := range recommendation.Steps {
				fmt.Fprintf(b, "%d. %s\n", index+1, escapeCell(step.Text))
			}
			fmt.Fprintln(b)
		}
		if len(recommendation.Verification) > 0 {
			fmt.Fprintln(b, "**Verification targets:**")
			fmt.Fprintln(b)
			for _, verification := range recommendation.Verification {
				label := "Checks"
				if verification.Kind == "requirement" || verification.Kind == "requirements" {
					label = "Requirements"
				} else if verification.Kind == "external_suite" || verification.Kind == "external_suites" {
					label = "Official SDK suites"
				}
				fmt.Fprintf(b, "- %s: %s\n", label, codeList(verification.IDs))
			}
			fmt.Fprintln(b)
		}
	}
}

func writeTargetModels(b *strings.Builder, target result.TargetReport) {
	if len(target.Models) == 0 {
		return
	}
	fmt.Fprintln(b, "### Models")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "| Model | Verdict | PASS | FAIL | WARN | UNSUP | ERROR |")
	fmt.Fprintln(b, "|---|---:|---:|---:|---:|---:|---:|")
	for _, model := range target.Models {
		fmt.Fprintf(b, "| `%s` | %s | %d | %d | %d | %d | %d |\n", escapeCell(model.ID), statusLabelString(model.Assessment.Verdict), model.Summary.Pass, model.Summary.Fail, model.Summary.Warn, model.Summary.Unsupported, model.Summary.Error)
	}
	fmt.Fprintln(b)
	if len(target.Models) > 1 {
		fmt.Fprintln(b, "#### Per-model rate-limit and latency")
		fmt.Fprintln(b)
		fmt.Fprintln(b, "| Model | Successful | 429 | Recovered retries | Safe req/s | Mean | P50 | P95 |")
		fmt.Fprintln(b, "|---|---:|---:|---:|---:|---:|---:|---:|")
		for _, model := range target.Models {
			if model.RateLimit == nil {
				fmt.Fprintf(b, "| `%s` | N/S | N/S | N/S | N/S | N/S | N/S | N/S |\n", escapeCell(model.ID))
				continue
			}
			s := model.RateLimit
			fmt.Fprintf(b, "| `%s` | %d | %d | %d | %.2f | %dms | %dms | %dms |\n",
				escapeCell(model.ID), s.SuccessfulRequests, s.RateLimited, s.RetriesRecovered, s.SafeRate,
				s.MeanLatencyMS, s.P50LatencyMS, s.P95LatencyMS)
		}
		fmt.Fprintln(b)
	}
}
