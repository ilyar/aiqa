package report

import (
	"fmt"
	"html"
	"sort"
	"strings"

	"github.com/ilyar/aiqa/internal/result"
)

func writeExecutiveSummary(b *strings.Builder, r result.Report) {
	assessment := r.Summary.Assessment
	if assessment == nil {
		writeSummaryTable(b, r.Summary)
		return
	}
	fmt.Fprintln(b, "| Overall | Core conformance | Optional coverage | Evidence | Open actions |")
	fmt.Fprintln(b, "|---|---:|---:|---:|---:|")
	fmt.Fprintf(b, "| %s | %s | %s | %s | %s |\n",
		statusLabelString(assessment.Verdict),
		requirementMeasureCell(assessment.Core),
		requirementMeasureCell(assessment.Optional),
		evidenceMeasureCell(assessment.Evidence),
		prioritySummary(r.Guidance.Summary.ByPriority))
	fmt.Fprintln(b)
	fmt.Fprintf(b, "Raw outcomes: **%d PASS · %d FAIL · %d WARN · %d UNSUP · %d SKIP · %d ERROR**.\n",
		r.Summary.Pass, r.Summary.Fail, r.Summary.Warn, r.Summary.Unsupported, r.Summary.Skip, r.Summary.Error)
	if len(r.Guidance.Recommendations) > 0 {
		top := r.Guidance.Recommendations[0]
		fmt.Fprintf(b, "\n**Fix first:** %s\n", top.Title)
		if top.PotentialImpact.ProjectedVerdict != nil {
			fmt.Fprintf(b, "\nProjected after this action: **%s → %s**, assuming no other requirement changes or regresses.\n",
				statusLabelString(top.PotentialImpact.CurrentVerdict), statusLabelString(*top.PotentialImpact.ProjectedVerdict))
		}
	}
}

func writeGuidance(b *strings.Builder, r result.Report) {
	switch r.Guidance.Status {
	case "disabled":
		fmt.Fprintln(b, "## Actions")
		fmt.Fprintln(b)
		fmt.Fprintln(b, "Actionable guidance was disabled with `--guidance=off`. Conformance results are unchanged.")
		fmt.Fprintln(b)
		return
	case "error":
		fmt.Fprintln(b, "## Investigate / rerun")
		fmt.Fprintln(b)
		fmt.Fprintf(b, "The guidance engine failed: %s. Conformance results and exit policy are unchanged.\n\n", escapeCell(r.Guidance.Error))
		return
	}
	if len(r.Guidance.Recommendations) == 0 {
		fmt.Fprintln(b, "## Actions")
		fmt.Fprintln(b)
		fmt.Fprintln(b, "No open actions were derived from applicable canonical requirements.")
		fmt.Fprintln(b)
		return
	}

	buckets := []struct {
		title string
		pick  func(result.Recommendation, *result.Finding) bool
	}{
		{"Fix first", func(rec result.Recommendation, finding *result.Finding) bool {
			return (rec.Priority == "p0" || rec.Priority == "p1") && rec.ActionKind != "investigate" && rec.ActionKind != "rerun" && rec.ActionKind != "manual_review" && (finding == nil || finding.Class != "unsupported_capability")
		}},
		{"Improve compatibility", func(rec result.Recommendation, finding *result.Finding) bool {
			return (rec.ActionKind == "fix" || rec.ActionKind == "configure" || rec.ActionKind == "workaround") && rec.Priority != "p0" && rec.Priority != "p1"
		}},
		{"Investigate / rerun", func(rec result.Recommendation, _ *result.Finding) bool {
			return rec.ActionKind == "investigate" || rec.ActionKind == "rerun" || rec.ActionKind == "manual_review"
		}},
		{"Capability gaps", func(rec result.Recommendation, finding *result.Finding) bool {
			return rec.ActionKind == "document" || (finding != nil && finding.Class == "unsupported_capability")
		}},
	}
	findings := map[string]*result.Finding{}
	for i := range r.Guidance.Findings {
		findings[r.Guidance.Findings[i].ID] = &r.Guidance.Findings[i]
	}
	used := map[string]bool{}
	for _, bucket := range buckets {
		var recommendations []result.Recommendation
		for _, recommendation := range r.Guidance.Recommendations {
			var finding *result.Finding
			if len(recommendation.FindingIDs) > 0 {
				finding = findings[recommendation.FindingIDs[0]]
			}
			if !used[recommendation.ID] && bucket.pick(recommendation, finding) {
				recommendations = append(recommendations, recommendation)
				used[recommendation.ID] = true
			}
		}
		if len(recommendations) == 0 {
			continue
		}
		fmt.Fprintf(b, "## %s\n\n", bucket.title)
		writeRecommendationTable(b, recommendations, findings)
		if r.Config.Guidance != "summary" {
			for _, recommendation := range recommendations {
				var finding *result.Finding
				if len(recommendation.FindingIDs) > 0 {
					finding = findings[recommendation.FindingIDs[0]]
				}
				writeRecommendationDetail(b, recommendation, finding)
			}
		}
	}
}

func writeRecommendationTable(b *strings.Builder, recommendations []result.Recommendation, findings map[string]*result.Finding) {
	fmt.Fprintln(b, "| PRI | Recommendation | Scope | Expected impact | Confidence |")
	fmt.Fprintln(b, "|:---:|---|---|---|---:|")
	for _, recommendation := range recommendations {
		var finding *result.Finding
		if len(recommendation.FindingIDs) > 0 {
			finding = findings[recommendation.FindingIDs[0]]
		}
		scope := recommendation.Target.Component
		if recommendation.Target.Endpoint != "" {
			scope += " · `" + recommendation.Target.Endpoint + "`"
		}
		if finding != nil && len(finding.ObservedScope.Models) > 0 {
			scope += " · `" + strings.Join(finding.ObservedScope.Models, "`, `") + "`"
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s |\n",
			strings.ToUpper(recommendation.Priority), escapeCell(recommendation.Title), escapeCell(scope), escapeCell(impactSummary(recommendation.PotentialImpact)), strings.ToUpper(recommendation.Confidence))
	}
	fmt.Fprintln(b)
}

func writeRecommendationDetail(b *strings.Builder, recommendation result.Recommendation, finding *result.Finding) {
	fmt.Fprintf(b, "<a id=\"%s\"></a>\n\n", html.EscapeString(recommendation.ID))
	fmt.Fprintf(b, "### %s · %s\n\n", strings.ToUpper(recommendation.Priority), recommendation.Title)
	fmt.Fprintf(b, "**Why this matters:** %s\n\n", recommendation.Why)
	if recommendation.Observed != "" {
		fmt.Fprintf(b, "**Observed:** %s\n\n", escapeCell(recommendation.Observed))
	}
	fmt.Fprintf(b, "- **Likely owner:** `%s`\n", escapeInline(recommendation.Target.Component))
	if finding != nil {
		fmt.Fprintf(b, "- **Attribution confidence:** %s\n", strings.ToUpper(finding.Attribution.Confidence))
	}
	fmt.Fprintf(b, "- **Recommendation confidence:** %s\n", strings.ToUpper(recommendation.Confidence))
	fmt.Fprintf(b, "- **Guidance level:** %d/3\n\n", recommendation.GuidanceLevel)

	if len(recommendation.Steps) > 0 {
		fmt.Fprintln(b, "#### Change or investigate")
		fmt.Fprintln(b)
		for i, step := range recommendation.Steps {
			fmt.Fprintf(b, "%d. %s\n", i+1, step.Text)
		}
		fmt.Fprintln(b)
	}
	if len(recommendation.Verification) > 0 {
		fmt.Fprintln(b, "#### Verification targets")
		fmt.Fprintln(b)
		for _, verification := range recommendation.Verification {
			switch verification.Kind {
			case "requirement", "requirements":
				fmt.Fprintf(b, "Requirements: %s\n\n", codeList(verification.IDs))
			case "external_suite", "external_suites":
				fmt.Fprintf(b, "Official SDK suites: %s\n\n", codeList(verification.IDs))
			}
		}
	}
	if len(recommendation.Workarounds) > 0 {
		fmt.Fprintln(b, "**Workarounds:**")
		for _, workaround := range recommendation.Workarounds {
			fmt.Fprintf(b, "- %s\n", workaround)
		}
		fmt.Fprintln(b)
	}
	if len(recommendation.Caveats) > 0 {
		fmt.Fprintln(b, "**Caveats:**")
		for _, caveat := range recommendation.Caveats {
			fmt.Fprintf(b, "- %s\n", caveat)
		}
		fmt.Fprintln(b)
	}
	fmt.Fprintf(b, "**Potential impact:** %s\n\n", impactSummary(recommendation.PotentialImpact))
	if finding != nil {
		fmt.Fprintf(b, "- **Evidence:** %s\n", codeList(finding.EvidenceRefs))
		fmt.Fprintf(b, "- **Requirements:** %s\n\n", codeList(finding.RequirementIDs))
	}
	if len(recommendation.References) > 0 {
		fmt.Fprint(b, "**References:** ")
		for i, reference := range recommendation.References {
			if i > 0 {
				fmt.Fprint(b, ", ")
			}
			fmt.Fprintf(b, "[%s](%s) (%s)", escapeCell(reference.Title), reference.URL, escapeCell(reference.SourceKind))
		}
		fmt.Fprintln(b)
		fmt.Fprintln(b)
	}
}

func writeRequirementAssessment(b *strings.Builder, requirements []result.RequirementAssessment) {
	var nonPass []result.RequirementAssessment
	for _, requirement := range requirements {
		if requirement.Status != result.RequirementPass && requirement.Status != result.RequirementNotApplicable {
			nonPass = append(nonPass, requirement)
		}
	}
	if len(nonPass) == 0 {
		return
	}
	fmt.Fprintln(b, "### Canonical requirements needing attention")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "| Status | Level | Subject | Requirement | Evidence | Blocked by |")
	fmt.Fprintln(b, "|---:|---:|---|---|---:|---|")
	for _, requirement := range nonPass {
		fmt.Fprintf(b, "| %s | %s | `%s:%s` | `%s` | %d | %s |\n",
			requirementStatusLabel(requirement.Status), strings.ToUpper(string(requirement.Level)), escapeCell(requirement.Subject.Kind), escapeCell(requirement.Subject.ID), escapeCell(requirement.ID), len(requirement.Evidence), escapeCell(strings.Join(requirement.BlockedBy, ", ")))
	}
	fmt.Fprintln(b)
}

func writePassedChecks(b *strings.Builder, r result.Report) {
	count := len(filterTests(r.Discovery.Tests, true)) + len(filterTests(r.Discovery.Compatibility, true))
	for _, model := range r.Models {
		count += len(filterTests(model.Compatibility, true)) + len(filterParameters(model.Parameters, true))
		if model.Context != nil && model.Context.Status == result.StatusPass {
			count++
		}
	}
	if count == 0 {
		return
	}
	fmt.Fprintln(b, "## Passed checks")
	fmt.Fprintln(b)
	fmt.Fprintf(b, "<details><summary>Show %d passed checks</summary>\n\n", count)
	if tests := filterTests(r.Discovery.Tests, true); len(tests) > 0 {
		fmt.Fprintln(b, "### Discovery")
		fmt.Fprintln(b)
		writeTestTable(b, tests)
		fmt.Fprintln(b)
	}
	if tests := filterTests(r.Discovery.Compatibility, true); len(tests) > 0 {
		fmt.Fprintln(b, "### Provider endpoints")
		fmt.Fprintln(b)
		writeTestTable(b, tests)
		fmt.Fprintln(b)
	}
	for _, model := range r.Models {
		tests := filterTests(model.Compatibility, true)
		parameters := filterParameters(model.Parameters, true)
		if len(tests)+len(parameters) == 0 && (model.Context == nil || model.Context.Status != result.StatusPass) {
			continue
		}
		fmt.Fprintf(b, "### `%s`\n\n", escapeInline(model.ID))
		if len(tests) > 0 {
			fmt.Fprintln(b, "#### Native compatibility")
			fmt.Fprintln(b)
			writeTestTable(b, tests)
			fmt.Fprintln(b)
		}
		if len(parameters) > 0 {
			fmt.Fprintln(b, "#### Model parameters")
			fmt.Fprintln(b)
			writeParameterTable(b, parameters)
			fmt.Fprintln(b)
		}
		if model.Context != nil && model.Context.Status == result.StatusPass {
			fmt.Fprintf(b, "Context window: **PASS** — %s\n\n", escapeCell(model.Context.Summary))
		}
	}
	fmt.Fprintln(b, "</details>")
	fmt.Fprintln(b)
}

func writeRawEvidence(b *strings.Builder, artifacts map[string]result.GuidanceArtifact) {
	if len(artifacts) == 0 {
		return
	}
	fmt.Fprintln(b, "## Raw evidence")
	fmt.Fprintln(b)
	refs := make([]string, 0, len(artifacts))
	for ref := range artifacts {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	for _, ref := range refs {
		artifact := artifacts[ref]
		fmt.Fprintf(b, "<details><summary>%s · %d bytes</summary>\n\n", escapeCell(ref), artifact.Bytes)
		fmt.Fprintln(b, "<pre>")
		fmt.Fprintln(b, html.EscapeString(artifact.Content))
		fmt.Fprintln(b, "</pre>")
		fmt.Fprintln(b)
		fmt.Fprintln(b, "</details>")
		fmt.Fprintln(b)
	}
}

func filterTests(in []result.TestResult, passed bool) []result.TestResult {
	var out []result.TestResult
	for _, item := range in {
		if (item.Status == result.StatusPass) == passed {
			out = append(out, item)
		}
	}
	return out
}

func filterParameters(in []result.ParameterResult, passed bool) []result.ParameterResult {
	var out []result.ParameterResult
	for _, item := range in {
		if (item.Status == result.StatusPass) == passed {
			out = append(out, item)
		}
	}
	return out
}

func requirementMeasureCell(measure result.RequirementMeasure) string {
	if measure.Percent == nil {
		return "N/A"
	}
	return fmt.Sprintf("%d/%d (%.1f%%)", measure.Pass, measure.Total, *measure.Percent)
}

func evidenceMeasureCell(measure result.EvidenceMeasure) string {
	if measure.ConclusivePercent == nil {
		return "N/A"
	}
	return fmt.Sprintf("%d/%d (%.1f%%)", measure.Conclusive, measure.Total, *measure.ConclusivePercent)
}

func prioritySummary(counts result.PriorityCounts) string {
	var parts []string
	if counts.P0 > 0 {
		parts = append(parts, fmt.Sprintf("%d P0", counts.P0))
	}
	if counts.P1 > 0 {
		parts = append(parts, fmt.Sprintf("%d P1", counts.P1))
	}
	if counts.P2 > 0 {
		parts = append(parts, fmt.Sprintf("%d P2", counts.P2))
	}
	if counts.P3 > 0 {
		parts = append(parts, fmt.Sprintf("%d P3", counts.P3))
	}
	if len(parts) == 0 {
		return "0"
	}
	return strings.Join(parts, " · ")
}

func impactSummary(impact result.PotentialImpact) string {
	var recovered []string
	if impact.CoreRequirementsRecovered > 0 {
		recovered = append(recovered, fmt.Sprintf("recover %d core", impact.CoreRequirementsRecovered))
	}
	if impact.OptionalRequirementsRecovered > 0 {
		recovered = append(recovered, fmt.Sprintf("recover %d optional", impact.OptionalRequirementsRecovered))
	}
	if len(recovered) == 0 {
		recovered = append(recovered, "establish conclusive evidence")
	}
	text := strings.Join(recovered, "; ")
	if impact.ProjectedVerdict != nil {
		text += "; projected " + statusLabelString(*impact.ProjectedVerdict)
	}
	return text
}

func requirementStatusLabel(status result.RequirementStatus) string {
	if status == result.RequirementNotApplicable {
		return "N/A"
	}
	return statusLabel(result.Status(status))
}
