package report

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/result"
)

func WriteV3(r result.ReportV3, cfg config.Config) ([]string, error) {
	result.RedactSecrets(&r, reportSecrets(cfg)...)
	r = normalizeV3Collections(r)
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
			data = []byte(MarkdownV3(r))
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

func normalizeV3Collections(r result.ReportV3) result.ReportV3 {
	for index := range r.Targets {
		if r.Targets[index].Models == nil {
			r.Targets[index].Models = []result.ModelReport{}
		}
	}
	return r
}

func MarkdownV3(r result.ReportV3) string {
	var b strings.Builder
	fmt.Fprintln(&b, "# AIQA compatibility report")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "_measures observed API behavior, a passing report is not a security audit or an official certification_")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- **Generated:** %s\n", r.GeneratedAt.Format("2006-01-02 15:04:05Z07:00"))
	fmt.Fprintf(&b, "- **Tool:** `aiqa %s`\n", escapeInline(r.Tool.Version))
	fmt.Fprintf(&b, "- **Service:** %s\n", servicesLink(r.Config.Targets))
	fmt.Fprintf(&b, "- **Suites:** `%s`\n", strings.Join(r.Config.Tests, ","))
	if len(r.Config.RequestedModels) > 0 {
		fmt.Fprintf(&b, "- **Requested models:** `%s` (one global list; each target reports only its selected inventory intersection)\n", escapeInline(strings.Join(r.Config.RequestedModels, ", ")))
	}
	fmt.Fprintf(&b, "- **Timeout:** `%s` per request\n", r.Config.Timeout)
	fmt.Fprintf(&b, "- **External checks:** %s\n\n", runExternalChecks(r.Targets))
	fmt.Fprintln(&b, "## Targets")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Target | Verdict | Core | Optional | Evidence | Models | Actions |")
	fmt.Fprintln(&b, "|---|---:|---:|---:|---:|---:|---:|")
	for _, target := range r.Targets {
		verdict, core, optional, evidence := targetVerdict(target)
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %d | %d |\n", escapeCell(target.ID), statusLabelString(verdict), core, optional, evidence, len(target.Models), len(target.Guidance.Recommendations))
	}
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "**Run verdict:** %s\n\n", statusLabelString(r.Summary.Verdict))
	fmt.Fprintln(&b, "Targets are assessed independently. Percentages are not combined into a cross-protocol score.")
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Rate-limit and latency")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Target | Logical | Attempts | Successful | 429 | Retry attempts | Recovered | Exhausted | Backoff | Safe req/s | Concurrency | Mean | P50 | P95 |")
	fmt.Fprintln(&b, "|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
	for _, target := range r.Targets {
		s := target.RateLimit
		fmt.Fprintf(&b, "| `%s` | %d | %d | %d | %d | %d | %d | %d | %dms | %s | %s | %s | %s | %s |\n", escapeCell(target.ID), s.LogicalRequests, s.PhysicalAttempts, s.SuccessfulRequests, s.RateLimited, s.RetryAttempts, s.RetriesRecovered, s.RetriesExhausted, s.BackoffMS, safeRateText(s), safeConcurrencyText(s), latencyPart(s, "mean"), latencyPart(s, "p50"), latencyPart(s, "p95"))
	}
	fmt.Fprintln(&b)

	for _, target := range r.Targets {
		fmt.Fprintf(&b, "## Target: %s\n\n", protocolName(target.Protocol))
		fmt.Fprintf(&b, "- **Target ID:** `%s`\n", escapeInline(target.ID))
		fmt.Fprintf(&b, "- **Profile:** `%s@%s`\n", escapeInline(target.Profile.ID), escapeInline(target.Profile.Revision))
		if targetConfigNormalized(r.Config.Targets, target.ID) {
			switch target.Protocol {
			case "anthropic":
				fmt.Fprintln(&b, "- **Base URL:** normalized by removing one trailing `/v1`; Anthropic paths append `/v1` themselves")
			case "google-genai":
				fmt.Fprintln(&b, "- **Base URL:** normalized as the prefix before the selected Google API version")
			default:
				fmt.Fprintln(&b, "- **Base URL:** normalized before requests")
			}
		}
		if target.Profile.APIVersion != "" {
			fmt.Fprintf(&b, "- **API version:** `%s`\n", escapeInline(target.Profile.APIVersion))
		}
		fmt.Fprintf(&b, "- **External checks:** %s\n", targetExternalChecks(target))
		for _, note := range target.Notes {
			fmt.Fprintf(&b, "- **Note:** %s\n", escapeInline(note))
		}
		fmt.Fprintf(&b, "- **Discovered models:** %d\n", target.Discovery.ModelCount)
		fmt.Fprintf(&b, "- **Selected models:** %s\n\n", selectedModelsText(target.Models))
		writeTargetGuidance(&b, target)
		writeRequirementAssessment(&b, target.Requirements)
		writeTargetModels(&b, target)
		writeTargetExtensions(&b, target.Extensions, target.ExtensionRateLimit)
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
	return strings.TrimRight(b.String(), "\n")
}

func servicesLink(targets []result.TargetPublicConfig) string {
	byOrigin := make(map[string]string)
	for _, target := range targets {
		if link := serviceLink(target.BaseURL); link != "" {
			u, err := url.Parse(strings.TrimSpace(target.BaseURL))
			if err == nil {
				origin := (&url.URL{Scheme: u.Scheme, Host: u.Host}).String()
				byOrigin[origin] = link
			}
		}
	}
	if len(byOrigin) == 0 {
		return "not specified"
	}
	origins := make([]string, 0, len(byOrigin))
	for origin := range byOrigin {
		origins = append(origins, origin)
	}
	sort.Strings(origins)
	links := make([]string, 0, len(origins))
	for _, origin := range origins {
		links = append(links, byOrigin[origin])
	}
	return strings.Join(links, ", ")
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
	case "google-genai":
		row.input, row.output = usage.InputTokens, usage.OutputTokens
		row.total = usage.TotalTokens
		if row.total == 0 {
			row.total = row.input + row.output + row.reasoning + row.cacheRead
		}
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

func selectedModelsText(models []result.ModelReport) string {
	if len(models) == 0 {
		return "none"
	}
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, "`"+escapeInline(model.ID)+"`")
	}
	return strings.Join(ids, ", ")
}

func latencyPart(s result.RateLimitStats, part string) string {
	if s.LatencySamples == 0 {
		return "N/S"
	}
	var value int64
	switch part {
	case "mean":
		value = s.MeanLatencyMS
	case "p50":
		value = s.P50LatencyMS
	case "p95":
		value = s.P95LatencyMS
	}
	return fmt.Sprintf("%dms", value)
}

func targetExternalChecks(target result.TargetReport) string {
	byID := map[string]result.ExtensionProvenance{}
	for _, run := range target.Extensions {
		byID[run.Provenance.ID] = run.Provenance
	}
	if len(byID) == 0 {
		return "not setup"
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	checks := make([]string, 0, len(ids))
	for _, id := range ids {
		checks = append(checks, extensionLabel(byID[id]))
	}
	return strings.Join(checks, ", ")
}

func runExternalChecks(targets []result.TargetReport) string {
	byKey := map[string]result.ExtensionProvenance{}
	for _, target := range targets {
		for _, run := range target.Extensions {
			key := run.Provenance.ID + "\x00" + run.Provenance.Version
			byKey[key] = run.Provenance
		}
	}
	if len(byKey) == 0 {
		return "not setup"
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		provenance := byKey[key]
		parts = append(parts, extensionLabel(provenance))
	}
	return strings.Join(parts, ", ")
}

func extensionLabel(provenance result.ExtensionProvenance) string {
	label := strings.TrimSpace(strings.Join([]string{extensionDisplayName(provenance), provenance.Version}, " "))
	switch provenance.ID {
	case "official-sdk.openai":
		return "[" + label + "](https://github.com/openai/openai-go/releases/tag/" + provenance.Version + ")"
	case "official-sdk.anthropic":
		return "[" + label + "](https://github.com/anthropics/anthropic-sdk-go/releases/tag/" + provenance.Version + ")"
	case "official-sdk.google-genai":
		return "[" + label + "](https://github.com/googleapis/go-genai/releases/tag/" + provenance.Version + ")"
	default:
		return escapeInline(label)
	}
}

func extensionDisplayName(provenance result.ExtensionProvenance) string {
	switch provenance.ID {
	case "official-sdk.openai":
		return "OpenAI Go SDK"
	case "official-sdk.anthropic":
		return "Anthropic Go SDK"
	case "official-sdk.google-genai":
		return "Google Gen AI Go SDK"
	default:
		return strings.TrimSpace(provenance.Name)
	}
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
	if protocol == "google-genai" {
		return "Google GenAI"
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
					label = "SDK suites"
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
		fmt.Fprintln(b, "| Model | Logical | Attempts | Successful | 429 | Retry attempts | Recovered | Exhausted | Safe req/s | Mean | P50 | P95 |")
		fmt.Fprintln(b, "|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
		for _, model := range target.Models {
			if model.RateLimit == nil {
				fmt.Fprintf(b, "| `%s` | N/S | N/S | N/S | N/S | N/S | N/S | N/S | N/S | N/S | N/S | N/S |\n", escapeCell(model.ID))
				continue
			}
			s := model.RateLimit
			fmt.Fprintf(b, "| `%s` | %d | %d | %d | %d | %d | %d | %d | %s | %s | %s | %s |\n",
				escapeCell(model.ID), s.LogicalRequests, s.PhysicalAttempts, s.SuccessfulRequests, s.RateLimited, s.RetryAttempts, s.RetriesRecovered, s.RetriesExhausted, safeRateText(*s), latencyPart(*s, "mean"), latencyPart(*s, "p50"), latencyPart(*s, "p95"))
		}
		fmt.Fprintln(b)
	}
	for _, model := range target.Models {
		if len(model.Modules) == 0 {
			continue
		}
		fmt.Fprintf(b, "#### Model details: `%s`\n\n", escapeInline(model.ID))
		fmt.Fprintln(b, "| Module | Capability | Applicability | Execution | Reason |")
		fmt.Fprintln(b, "|---|---|---:|---:|---|")
		for _, module := range model.Modules {
			applicability := strings.ToUpper(strings.ReplaceAll(string(module.Applicability), "_", "-"))
			execution := strings.ToUpper(strings.ReplaceAll(string(module.Execution), "_", "-"))
			if execution == "" {
				execution = "–"
			}
			fmt.Fprintf(b, "| `%s` | `%s` | %s | %s | %s |\n", escapeCell(module.ID), escapeCell(module.Capability), applicability, execution, escapeCell(module.Reason))
		}
		fmt.Fprintln(b)
	}
}

func writeTargetExtensions(b *strings.Builder, runs []result.ExtensionRun, rateLimit *result.RateLimitStats) {
	if len(runs) == 0 {
		return
	}
	groups := map[string][]result.ExtensionRun{}
	provenance := map[string]result.ExtensionProvenance{}
	for _, run := range runs {
		groups[run.Provenance.ID] = append(groups[run.Provenance.ID], run)
		provenance[run.Provenance.ID] = run.Provenance
	}
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	fmt.Fprintln(b, "### Extension suites")
	fmt.Fprintln(b)
	if rateLimit != nil {
		fmt.Fprintln(b, "Built-in extension transport counts are separate from native latency percentiles. Safe rate and concurrency show the target pacer's state after extension execution; executable extensions report only validated suite evidence.")
		fmt.Fprintln(b)
		fmt.Fprintln(b, "| Logical | Attempts | Successful | 429 | Retry attempts | Recovered | Exhausted | Backoff | Safe req/s | Concurrency |")
		fmt.Fprintln(b, "|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
		fmt.Fprintf(b, "| %d | %d | %d | %d | %d | %d | %d | %dms | %s | %s |\n\n", rateLimit.LogicalRequests, rateLimit.PhysicalAttempts, rateLimit.SuccessfulRequests, rateLimit.RateLimited, rateLimit.RetryAttempts, rateLimit.RetriesRecovered, rateLimit.RetriesExhausted, rateLimit.BackoffMS, safeRateText(*rateLimit), safeConcurrencyText(*rateLimit))
	}
	for _, id := range ids {
		p := provenance[id]
		fmt.Fprintf(b, "#### %s %s\n\n", escapeCell(extensionDisplayName(p)), escapeCell(p.Version))
		fmt.Fprintln(b, "| Suite | Subject | Status | Duration | Summary |")
		fmt.Fprintln(b, "|---|---|---:|---:|---|")
		for _, run := range groups[id] {
			for _, suite := range run.Suites {
				fmt.Fprintf(b, "| `%s` | `%s:%s` | %s | %dms | %s |\n", escapeCell(suite.SuiteID), escapeCell(suite.Subject.Kind), escapeCell(suite.Subject.ID), statusLabel(suite.Status), suite.DurationMS, escapeCell(suite.Summary))
			}
		}
		fmt.Fprintln(b)
		var failures []result.ExtensionRun
		for _, run := range groups[id] {
			if run.Runner.Status == result.StatusError {
				failures = append(failures, run)
			}
		}
		if len(failures) > 0 {
			fmt.Fprintln(b, "**Runner issues:**")
			fmt.Fprintln(b)
			for _, run := range failures {
				fmt.Fprintf(b, "- `%s:%s`: %s\n", escapeInline(run.Subject.Kind), escapeInline(run.Subject.ID), escapeCell(run.Runner.Summary))
			}
			fmt.Fprintln(b)
		}
	}
}
