package guidance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

type Options struct {
	Mode   string
	Pack   string
	Locale string
}

type findingGroup struct {
	code           string
	subject        result.SubjectRef
	requirementIDs map[string]bool
	evidenceRefs   map[string]bool
	artifactRefs   map[string]bool
	details        map[string]any
	expected       map[string]bool
	observed       map[string]bool
	statuses       map[result.RequirementStatus]bool
	failureKinds   map[string]bool
}

// Apply builds deterministic findings and recommendations from canonical
// requirements. It never mutates requirement outcomes or summary assessment.
func Apply(r *result.Report, p profile.Profile, opts Options) {
	artifacts := r.Guidance.Artifacts
	r.Guidance = result.GuidanceReport{
		SchemaVersion: "aiqa.guidance.v1",
		Status:        "complete",
		Artifacts:     artifacts,
	}
	if opts.Mode == "off" {
		r.Guidance.Status = "disabled"
		return
	}
	if opts.Locale == "" {
		opts.Locale = "en"
	}
	if opts.Locale != "en" {
		guidanceError(r, fmt.Errorf("unsupported guidance locale %q", opts.Locale))
		return
	}
	catalog, err := LoadCatalog()
	if err != nil {
		guidanceError(r, err)
		return
	}
	pack, packInfo, err := LoadPack(opts.Pack)
	if err != nil {
		guidanceError(r, err)
		return
	}
	r.Guidance.Engine = result.GuidanceEngineInfo{
		Version:         EngineVersion,
		CatalogRevision: catalog.Revision,
		Packs:           []result.GuidancePackInfo{packInfo},
	}

	findings := buildFindings(*r, p, catalog)
	recommendations := buildRecommendations(*r, p, catalog, pack, findings)
	r.Guidance.Findings = findings
	r.Guidance.Recommendations = recommendations
	r.Guidance.Summary = summarizeGuidance(r.Requirements, findings, recommendations)
	if r.Guidance.Summary.CoveredRequirements < r.Guidance.Summary.EligibleRequirements {
		r.Guidance.Status = "partial"
	}
}

// ExtractArtifacts stores each sanitized external checker log once and replaces
// repeated inline output with a stable content-addressed reference.
func ExtractArtifacts(r *result.Report) {
	if r.Guidance.Artifacts == nil {
		r.Guidance.Artifacts = map[string]result.GuidanceArtifact{}
	}
	visit := func(external *result.ExternalResult) {
		if external.Output == "" {
			return
		}
		content, truncated := sanitizeArtifact(external.Output, 16*1024)
		sum := sha256.Sum256([]byte(content))
		hexDigest := hex.EncodeToString(sum[:])
		ref := "sha256:" + hexDigest
		r.Guidance.Artifacts[ref] = result.GuidanceArtifact{
			Kind:      "external_checker_log",
			MediaType: "text/plain",
			Bytes:     len(content),
			SHA256:    hexDigest,
			Content:   content,
			Truncated: truncated,
		}
		external.ArtifactRef = ref
		external.Output = ""
	}
	for i := range r.Discovery.External {
		visit(&r.Discovery.External[i])
	}
	for modelIndex := range r.Models {
		for i := range r.Models[modelIndex].External {
			visit(&r.Models[modelIndex].External[i])
		}
	}
}

func buildFindings(r result.Report, p profile.Profile, catalog Catalog) []result.Finding {
	groups := map[string]*findingGroup{}
	blocked := map[string][]string{}
	for _, requirement := range r.Requirements {
		if requirement.Status == result.RequirementPass || requirement.Status == result.RequirementNotApplicable || requirement.Applicability == result.ApplicabilityNotApplicable {
			continue
		}
		if len(requirement.BlockedBy) > 0 {
			for _, parent := range requirement.BlockedBy {
				key := subjectKey(requirement.Subject) + "\x00" + parent
				blocked[key] = append(blocked[key], requirement.ID)
			}
			continue
		}
		codes := findingCodes(requirement, catalog)
		for _, code := range codes {
			key := code + "\x00" + subjectKey(requirement.Subject) + "\x00" + rootCauseKey(requirement, code)
			group := groups[key]
			if group == nil {
				group = &findingGroup{
					code:           code,
					subject:        requirement.Subject,
					requirementIDs: map[string]bool{},
					evidenceRefs:   map[string]bool{},
					artifactRefs:   map[string]bool{},
					details:        map[string]any{},
					expected:       map[string]bool{},
					observed:       map[string]bool{},
					statuses:       map[result.RequirementStatus]bool{},
					failureKinds:   map[string]bool{},
				}
				groups[key] = group
			}
			group.requirementIDs[requirement.ID] = true
			group.statuses[requirement.Status] = true
			if requirement.FailureKind != "" {
				group.failureKinds[requirement.FailureKind] = true
			}
			if requirement.Description != "" {
				group.expected[requirement.Description] = true
			}
			for _, evidence := range requirement.Evidence {
				group.evidenceRefs[evidence.ID] = true
				if evidence.ArtifactRef != "" {
					group.artifactRefs[evidence.ArtifactRef] = true
				}
				if evidence.Summary != "" {
					group.observed[evidence.Summary] = true
				}
				for _, signal := range evidence.Signals {
					if signal.Code == code {
						mergeDetails(group.details, signal.Parameters)
					}
				}
			}
		}
	}

	findings := make([]result.Finding, 0, len(groups))
	for _, group := range groups {
		requirementIDs := sortedSet(group.requirementIDs)
		entry, curated := catalog.Entry(group.code)
		fingerprint := findingFingerprint(p, group.code, group.subject, group.details)
		finding := result.Finding{
			ID:             "finding:" + slug(group.code) + ":" + shortFingerprint(fingerprint),
			Fingerprint:    fingerprint,
			Code:           group.code,
			Class:          findingClass(group.code, group.statuses, group.failureKinds),
			EvidenceState:  evidenceState(group.code, group.statuses, group.failureKinds),
			Confidence:     findingConfidence(group.code, group.statuses, group.failureKinds),
			Title:          genericFindingTitle(requirementIDs, group.statuses),
			Subject:        group.subject,
			RequirementIDs: requirementIDs,
			EvidenceRefs:   sortedSet(group.evidenceRefs),
			Expected:       strings.Join(sortedSet(group.expected), " "),
			Observed:       strings.Join(sortedSet(group.observed), " "),
			Attribution:    defaultAttribution(group.code, group.subject, group.statuses, group.failureKinds),
			Details:        group.details,
			ArtifactRefs:   sortedSet(group.artifactRefs),
			BaselineState:  "unknown",
		}
		if curated {
			finding.Title = entry.FindingTitle
			finding.Attribution = entry.Attribution
		}
		finding.ObservedScope = observedScope(finding)
		for _, requirementID := range requirementIDs {
			key := subjectKey(group.subject) + "\x00" + requirementID
			finding.BlockedRequirementIDs = append(finding.BlockedRequirementIDs, blocked[key]...)
		}
		finding.BlockedRequirementIDs = uniqueStrings(finding.BlockedRequirementIDs)
		findings = append(findings, finding)
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].ID < findings[j].ID })
	return findings
}

func rootCauseKey(requirement result.RequirementAssessment, code string) string {
	root := map[string]any{}
	for _, evidence := range requirement.Evidence {
		for _, signal := range evidence.Signals {
			if signal.Code != code {
				continue
			}
			mergeDetails(root, stableDetails(signal.Parameters))
		}
	}
	encoded, _ := json.Marshal(root)
	return string(encoded)
}

func findingCodes(requirement result.RequirementAssessment, catalog Catalog) []string {
	if requirement.FailureKind == "evidence_conflict" {
		if requirement.ID == "chat.stream.sse" || requirement.ID == "chat.stream.usage" || requirement.ID == "official-sdk.chat.stream" || requirement.ID == "official-sdk.chat.stream.usage" {
			return []string{"evidence.chat.stream.native_sdk_conflict"}
		}
		return []string{"evidence." + findingCodeID(requirement.ID) + ".native_sdk_conflict"}
	}
	curated := map[string]bool{}
	runner := map[string]bool{}
	for _, evidence := range requirement.Evidence {
		for _, signal := range evidence.Signals {
			if _, ok := catalog.Entry(signal.Code); ok {
				curated[signal.Code] = true
			}
			if strings.HasPrefix(signal.Code, "external.runner.") {
				runner[signal.Code] = true
			}
		}
	}
	if len(curated) > 0 {
		return sortedSet(curated)
	}
	if len(runner) > 0 {
		return sortedSet(runner)
	}
	prefix := "requirement"
	suffix := string(requirement.Status)
	switch requirement.Status {
	case result.RequirementUnsupported:
		prefix = "unsupported"
	case result.RequirementWarn:
		prefix = "evidence"
		suffix = "uncertain"
	case result.RequirementSkip:
		prefix = "skipped"
	case result.RequirementError:
		prefix = "execution"
	}
	return []string{prefix + "." + findingCodeID(requirement.ID) + "." + suffix}
}

func buildRecommendations(r result.Report, p profile.Profile, catalog Catalog, pack Pack, findings []result.Finding) []result.Recommendation {
	requirements := requirementIndex(r.Requirements)
	recommendations := make([]result.Recommendation, 0, len(findings))
	for _, finding := range findings {
		entry, curated := catalog.Entry(finding.Code)
		priority := priorityFor(finding, requirements)
		actionKind := actionFor(finding, requirements)
		guidanceLevel := 1
		title := genericRecommendationTitle(finding, actionKind)
		why := genericWhy(finding, requirements)
		steps := genericSteps(finding, actionKind)
		verification := []result.Verification{{Kind: "requirement", IDs: append([]string(nil), finding.RequirementIDs...)}}
		references := profileReferences(p)
		audiences := []string{"qa_operator"}
		workarounds, caveats := []string(nil), []string(nil)
		source := result.RecommendationSource{Kind: "heuristic", ID: "no_safe_automated_guidance", Revision: EngineVersion}
		if curated {
			if moreUrgent(entry.DefaultPriority, priority) {
				priority = entry.DefaultPriority
			}
			actionKind = entry.ActionKind
			guidanceLevel = entry.GuidanceLevel
			title, why = entry.RecommendationTitle, entry.Why
			steps = cloneSteps(entry.Steps)
			verification = cloneVerification(entry.Verification)
			references = append([]result.Reference(nil), entry.References...)
			audiences = append([]string(nil), entry.Audiences...)
			workarounds = append([]string(nil), entry.Workarounds...)
			caveats = append([]string(nil), entry.Caveats...)
			source = result.RecommendationSource{Kind: "catalog", ID: entry.ID, Revision: entry.Revision}
		}
		applyPack(&guidanceLevel, &steps, &workarounds, &caveats, &references, pack, finding.Code)
		if finding.EvidenceState == "conflicting" || finding.EvidenceState == "inconclusive" {
			caveats = append(caveats, "Do not attribute or implement a provider fix until the linked evidence becomes conclusive.")
		}
		verification = ensureRerunVerification(verification, finding.Subject)
		impact := potentialImpact(r, finding, actionKind, requirements)
		fingerprint := recommendationFingerprint(finding.Fingerprint, source)
		recommendations = append(recommendations, result.Recommendation{
			ID:              "recommendation:" + slug(finding.Code) + ":" + shortFingerprint(fingerprint),
			Fingerprint:     fingerprint,
			FindingIDs:      []string{finding.ID},
			Title:           title,
			Priority:        priority,
			ActionKind:      actionKind,
			GuidanceLevel:   guidanceLevel,
			Confidence:      recommendationConfidence(finding, curated),
			Audiences:       uniqueStrings(audiences),
			Target:          result.RecommendationTarget{Component: finding.Attribution.Component, Endpoint: finding.ObservedScope.Endpoint},
			Why:             why,
			Observed:        finding.Observed,
			Steps:           steps,
			Verification:    verification,
			Workarounds:     uniqueStrings(workarounds),
			Caveats:         uniqueStrings(caveats),
			PotentialImpact: impact,
			References:      references,
			Source:          source,
		})
	}
	findingIndex := make(map[string]result.Finding, len(findings))
	for _, finding := range findings {
		findingIndex[finding.ID] = finding
	}
	sort.Slice(recommendations, func(i, j int) bool { return recommendationLess(recommendations[i], recommendations[j], findingIndex) })
	return recommendations
}

func summarizeGuidance(requirements []result.RequirementAssessment, findings []result.Finding, recommendations []result.Recommendation) result.GuidanceSummary {
	eligible := map[string]bool{}
	covered := map[string]bool{}
	for _, requirement := range requirements {
		if requirement.Status == result.RequirementPass || requirement.Status == result.RequirementNotApplicable || requirement.Applicability == result.ApplicabilityNotApplicable || len(requirement.BlockedBy) > 0 {
			continue
		}
		eligible[subjectKey(requirement.Subject)+"\x00"+requirement.ID] = true
	}
	findingByID := map[string]result.Finding{}
	for _, finding := range findings {
		findingByID[finding.ID] = finding
	}
	var summary result.GuidanceSummary
	summary.EligibleFindings = len(findings)
	summary.Findings = len(findings)
	summary.Recommendations = len(recommendations)
	actionable := 0
	for _, recommendation := range recommendations {
		switch recommendation.Priority {
		case "p0":
			summary.ByPriority.P0++
		case "p1":
			summary.ByPriority.P1++
		case "p2":
			summary.ByPriority.P2++
		case "p3":
			summary.ByPriority.P3++
		}
		if recommendation.GuidanceLevel >= 2 {
			actionable++
		}
		for _, findingID := range recommendation.FindingIDs {
			finding := findingByID[findingID]
			for _, requirementID := range finding.RequirementIDs {
				covered[subjectKey(finding.Subject)+"\x00"+requirementID] = true
			}
		}
	}
	summary.EligibleRequirements = len(eligible)
	for key := range covered {
		if eligible[key] {
			summary.CoveredRequirements++
		}
	}
	if len(eligible) > 0 {
		summary.GuidanceCoveragePercent = roundedPercent(summary.CoveredRequirements, len(eligible))
	} else {
		summary.GuidanceCoveragePercent = 100
	}
	if len(findings) > 0 {
		summary.ActionableCoveragePercent = roundedPercent(actionable, len(findings))
	} else {
		summary.ActionableCoveragePercent = 100
	}
	return summary
}

func guidanceError(r *result.Report, err error) {
	r.Guidance.Status = "error"
	r.Guidance.Error = err.Error()
}

func findingClass(code string, statuses map[result.RequirementStatus]bool, failureKinds map[string]bool) string {
	switch {
	case failureKinds["evidence_conflict"] || strings.HasPrefix(code, "evidence."):
		return "evidence_conflict"
	case strings.HasPrefix(code, "contract."):
		return "contract_violation"
	case strings.HasPrefix(code, "behavior."):
		return "behavior_mismatch"
	case strings.HasPrefix(code, "unsupported."):
		return "unsupported_capability"
	case strings.HasPrefix(code, "configuration."):
		return "configuration_constraint"
	case strings.HasPrefix(code, "external.runner"):
		return "runner_defect"
	case strings.HasPrefix(code, "execution.") || statuses[result.RequirementError]:
		return "execution_failure"
	case statuses[result.RequirementWarn] || statuses[result.RequirementSkip]:
		return "evidence_gap"
	default:
		return "behavior_mismatch"
	}
}

func evidenceState(code string, statuses map[result.RequirementStatus]bool, failureKinds map[string]bool) string {
	if failureKinds["evidence_conflict"] {
		return "conflicting"
	}
	if code == "contract.chat.invalid_request.upstream_5xx" {
		return "confirmed"
	}
	if statuses[result.RequirementError] || statuses[result.RequirementSkip] {
		return "inconclusive"
	}
	if statuses[result.RequirementWarn] {
		return "probable"
	}
	return "confirmed"
}

func findingConfidence(code string, statuses map[result.RequirementStatus]bool, failureKinds map[string]bool) string {
	state := evidenceState(code, statuses, failureKinds)
	switch state {
	case "confirmed":
		return "high"
	case "probable", "conflicting":
		return "medium"
	default:
		return "low"
	}
}

func defaultAttribution(code string, subject result.SubjectRef, statuses map[result.RequirementStatus]bool, failureKinds map[string]bool) result.Attribution {
	if failureKinds["evidence_conflict"] {
		return result.Attribution{Component: "unknown", Confidence: "low", Basis: "The evidence sources disagree, so the responsible component is not localized."}
	}
	if strings.HasPrefix(code, "external.runner") {
		return result.Attribution{Component: "external_checker", Confidence: "low", Basis: "The external checker failed before it produced requirement evidence."}
	}
	if statuses[result.RequirementError] {
		return result.Attribution{Component: "network_or_runtime", Confidence: "low", Basis: "Execution failed before compatibility could be established."}
	}
	if subject.Kind == "provider" {
		return result.Attribution{Component: "provider_gateway", Confidence: "medium", Basis: "The behavior was observed at a provider-scoped endpoint."}
	}
	return result.Attribution{Component: "unknown", Confidence: "low", Basis: "A model-scoped result alone does not localize the defect to the gateway, adapter, or upstream model."}
}

func observedScope(finding result.Finding) result.ObservedScope {
	scope := result.ObservedScope{}
	if endpoint, ok := finding.Details["endpoint"].(string); ok {
		scope.Endpoint = endpoint
	}
	if finding.Subject.Kind == "model" {
		scope.Models = []string{finding.Subject.ID}
	}
	scope.Affected = intValue(finding.Details["missing_entries"])
	scope.Total = intValue(finding.Details["total_entries"])
	if finding.Subject.Kind == "provider" && scope.Affected > 0 {
		scope.Models = []string{"*"}
	}
	return scope
}

func genericFindingTitle(requirementIDs []string, statuses map[result.RequirementStatus]bool) string {
	status := "not proved"
	for _, candidate := range []result.RequirementStatus{result.RequirementFail, result.RequirementError, result.RequirementUnsupported, result.RequirementWarn, result.RequirementSkip} {
		if statuses[candidate] {
			status = strings.ReplaceAll(string(candidate), "_", " ")
			break
		}
	}
	return strings.Join(requirementIDs, ", ") + " is " + status
}

func priorityFor(finding result.Finding, requirements map[string]result.RequirementAssessment) string {
	priority := "p3"
	for _, id := range finding.RequirementIDs {
		requirement := requirements[subjectKey(finding.Subject)+"\x00"+id]
		candidate := "p3"
		switch {
		case requirement.Level == result.RequirementCore:
			candidate = "p0"
		case finding.EvidenceState == "conflicting" || requirement.Status == result.RequirementFail:
			candidate = "p1"
		case requirement.Status == result.RequirementUnsupported || requirement.Status == result.RequirementWarn || requirement.Status == result.RequirementError:
			candidate = "p2"
		}
		if moreUrgent(candidate, priority) {
			priority = candidate
		}
	}
	return priority
}

func actionFor(finding result.Finding, requirements map[string]result.RequirementAssessment) string {
	if finding.EvidenceState == "conflicting" {
		return "investigate"
	}
	for _, id := range finding.RequirementIDs {
		requirement := requirements[subjectKey(finding.Subject)+"\x00"+id]
		switch requirement.Status {
		case result.RequirementError:
			return "rerun"
		case result.RequirementWarn:
			return "investigate"
		case result.RequirementUnsupported:
			return "document"
		case result.RequirementFail:
			return "fix"
		}
	}
	return "manual_review"
}

func genericRecommendationTitle(finding result.Finding, action string) string {
	switch action {
	case "rerun":
		return "Rerun the affected requirement after execution recovers"
	case "investigate":
		return "Investigate the incomplete or conflicting evidence"
	case "document":
		return "Document the optional capability gap"
	case "fix":
		return "Correct the incompatible behavior"
	default:
		return "Review the affected requirement"
	}
}

func genericWhy(finding result.Finding, requirements map[string]result.RequirementAssessment) string {
	if finding.EvidenceState == "inconclusive" {
		return "The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet."
	}
	if finding.EvidenceState == "conflicting" {
		return "Trustworthy evidence sources disagree, so the mismatch must be localized before choosing a fix."
	}
	for _, id := range finding.RequirementIDs {
		if requirements[subjectKey(finding.Subject)+"\x00"+id].Level == result.RequirementCore {
			return "The affected core requirement blocks the authoritative compatibility verdict."
		}
	}
	return "The affected optional requirement reduces demonstrated interoperability or capability coverage."
}

func genericSteps(finding result.Finding, action string) []result.RecommendationStep {
	switch action {
	case "rerun":
		return []result.RecommendationStep{{ID: "restore-execution", Text: "Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result."}, {ID: "rerun", Text: "Rerun the affected requirement and require a conclusive response."}}
	case "investigate":
		return []result.RecommendationStep{{ID: "compare-evidence", Text: "Compare the structured evidence records and request settings for the affected requirement."}, {ID: "localize", Text: "Localize the mismatch before changing the provider or checker."}}
	case "document":
		return []result.RecommendationStep{{ID: "confirm-gap", Text: "Confirm the optional feature is deliberately unsupported with a stable client error."}, {ID: "document", Text: "Publish the limitation in the model or provider capability matrix."}}
	default:
		return []result.RecommendationStep{{ID: "inspect-evidence", Text: "Inspect the linked structured evidence for the first incompatible boundary."}, {ID: "correct-behavior", Text: "Make the implementation satisfy the canonical requirement without weakening validation."}}
	}
}

func ensureRerunVerification(in []result.Verification, subject result.SubjectRef) []result.Verification {
	out := cloneVerification(in)
	command := "aiqa test=compat --format=markdown,json --name=verify"
	if subject.Kind == "model" {
		command = "aiqa test=compat --model \"$MODEL\" --format=markdown,json --name=verify"
	}
	for _, verification := range out {
		if verification.Kind == "command" {
			return out
		}
	}
	return append(out, result.Verification{Kind: "command", Shell: command})
}

func potentialImpact(r result.Report, finding result.Finding, action string, requirements map[string]result.RequirementAssessment) result.PotentialImpact {
	current := ""
	if r.Summary.Assessment != nil {
		current = r.Summary.Assessment.Verdict
	}
	impact := result.PotentialImpact{CurrentVerdict: current}
	for _, id := range finding.RequirementIDs {
		requirement := requirements[subjectKey(finding.Subject)+"\x00"+id]
		if requirement.Level == result.RequirementCore {
			impact.BlocksCore = true
			impact.CoreRequirementsRecovered++
		} else {
			impact.OptionalRequirementsRecovered++
		}
		if requirement.Status == result.RequirementWarn || requirement.Status == result.RequirementSkip || requirement.Status == result.RequirementError {
			impact.EvidenceRequirementsRecovered++
		}
	}
	if finding.EvidenceState != "confirmed" || (action != "fix" && action != "configure") {
		return impact
	}
	projected := projectedVerdict(r.Requirements, finding)
	impact.ProjectedVerdict = &projected
	impact.Assumptions = []string{"Only the affected requirements change to pass", "No other requirement regresses"}
	return impact
}

func projectedVerdict(requirements []result.RequirementAssessment, finding result.Finding) string {
	affected := map[string]bool{}
	for _, id := range finding.RequirementIDs {
		affected[subjectKey(finding.Subject)+"\x00"+id] = true
	}
	clone := append([]result.RequirementAssessment(nil), requirements...)
	for i := range clone {
		if affected[subjectKey(clone[i].Subject)+"\x00"+clone[i].ID] {
			clone[i].Status = result.RequirementPass
			clone[i].FailureKind = ""
		}
	}
	return verdictFor(clone)
}

func verdictFor(requirements []result.RequirementAssessment) string {
	coreFail, coreUnknown, optionalFail, optionalGap := false, false, false, false
	for _, requirement := range requirements {
		if requirement.Status == result.RequirementNotApplicable || requirement.Applicability == result.ApplicabilityNotApplicable {
			continue
		}
		if requirement.Level == result.RequirementCore {
			if requirement.Status == result.RequirementFail {
				coreFail = true
			} else if requirement.Status != result.RequirementPass {
				coreUnknown = true
			}
			continue
		}
		if requirement.Status == result.RequirementFail {
			optionalFail = true
		} else if requirement.Status != result.RequirementPass {
			optionalGap = true
		}
	}
	switch {
	case coreFail:
		return "fail"
	case coreUnknown:
		return "inconclusive"
	case optionalFail:
		return "partial"
	case optionalGap:
		return "pass-with-gaps"
	default:
		return "pass"
	}
}

func recommendationLess(a, b result.Recommendation, findings map[string]result.Finding) bool {
	if priorityRank(a.Priority) != priorityRank(b.Priority) {
		return priorityRank(a.Priority) < priorityRank(b.Priority)
	}
	if a.PotentialImpact.BlocksCore != b.PotentialImpact.BlocksCore {
		return a.PotentialImpact.BlocksCore
	}
	if projectedGain(a.PotentialImpact) != projectedGain(b.PotentialImpact) {
		return projectedGain(a.PotentialImpact) > projectedGain(b.PotentialImpact)
	}
	if affectedRequirementCount(a.PotentialImpact) != affectedRequirementCount(b.PotentialImpact) {
		return affectedRequirementCount(a.PotentialImpact) > affectedRequirementCount(b.PotentialImpact)
	}
	if recommendationReach(a, findings) != recommendationReach(b, findings) {
		return recommendationReach(a, findings) > recommendationReach(b, findings)
	}
	if a.GuidanceLevel != b.GuidanceLevel {
		return a.GuidanceLevel > b.GuidanceLevel
	}
	if confidenceRank(a.Confidence) != confidenceRank(b.Confidence) {
		return confidenceRank(a.Confidence) > confidenceRank(b.Confidence)
	}
	return a.ID < b.ID
}

func affectedRequirementCount(impact result.PotentialImpact) int {
	return impact.CoreRequirementsRecovered + impact.OptionalRequirementsRecovered
}

func recommendationReach(recommendation result.Recommendation, findings map[string]result.Finding) int {
	reach := 0
	for _, id := range recommendation.FindingIDs {
		finding := findings[id]
		candidate := finding.ObservedScope.Affected
		if candidate == 0 {
			candidate = len(finding.ObservedScope.Models)
		}
		if candidate == 0 {
			candidate = 1
		}
		reach += candidate
	}
	return reach
}

func applyPack(level *int, steps *[]result.RecommendationStep, workarounds, caveats *[]string, references *[]result.Reference, pack Pack, code string) {
	for _, override := range pack.Overrides {
		if override.FindingCode != code {
			continue
		}
		if override.GuidanceLevel > 0 {
			*level = override.GuidanceLevel
		}
		if len(override.Steps) > 0 {
			*steps = cloneSteps(override.Steps)
		}
		*workarounds = append(*workarounds, override.Workarounds...)
		*caveats = append(*caveats, override.Caveats...)
		*references = append(*references, override.References...)
		return
	}
}

func mergeDetails(dst, src map[string]any) {
	for key, value := range src {
		if existing, ok := dst[key]; !ok {
			dst[key] = value
		} else if !reflect.DeepEqual(existing, value) {
			values := []any{}
			if list, ok := existing.([]any); ok {
				values = append(values, list...)
			} else {
				values = append(values, existing)
			}
			found := false
			for _, candidate := range values {
				if reflect.DeepEqual(candidate, value) {
					found = true
				}
			}
			if !found {
				values = append(values, value)
			}
			sort.Slice(values, func(i, j int) bool { return fmt.Sprint(values[i]) < fmt.Sprint(values[j]) })
			dst[key] = values
		}
	}
}

func requirementIndex(requirements []result.RequirementAssessment) map[string]result.RequirementAssessment {
	out := make(map[string]result.RequirementAssessment, len(requirements))
	for _, requirement := range requirements {
		out[subjectKey(requirement.Subject)+"\x00"+requirement.ID] = requirement
	}
	return out
}

func subjectKey(subject result.SubjectRef) string {
	return subject.Kind + "\x00" + strings.ToLower(strings.TrimSpace(subject.ID))
}

func profileReferences(p profile.Profile) []result.Reference {
	if strings.HasPrefix(p.Source, "https://") {
		return []result.Reference{{Title: "Selected compatibility profile source", URL: p.Source, SourceKind: "official"}}
	}
	return []result.Reference{{Title: "OpenAI OpenAPI specification", URL: "https://github.com/openai/openai-openapi", SourceKind: "official"}}
}

func moreUrgent(candidate, current string) bool {
	return priorityRank(candidate) < priorityRank(current)
}

func priorityRank(value string) int {
	switch value {
	case "p0":
		return 0
	case "p1":
		return 1
	case "p2":
		return 2
	default:
		return 3
	}
}

func projectedGain(impact result.PotentialImpact) int {
	if impact.ProjectedVerdict == nil {
		return -1
	}
	return verdictRank(*impact.ProjectedVerdict) - verdictRank(impact.CurrentVerdict)
}

func verdictRank(value string) int {
	switch value {
	case "fail":
		return 0
	case "partial":
		return 1
	case "pass-with-gaps":
		return 2
	case "pass":
		return 3
	default:
		return -1
	}
}

func confidenceRank(value string) int {
	switch value {
	case "high":
		return 3
	case "medium":
		return 2
	default:
		return 1
	}
}

func recommendationConfidence(finding result.Finding, curated bool) string {
	if !curated || finding.EvidenceState == "inconclusive" {
		return "low"
	}
	if finding.Attribution.Confidence == "low" || finding.EvidenceState == "conflicting" {
		return "low"
	}
	if finding.Attribution.Confidence == "medium" || finding.EvidenceState == "probable" {
		return "medium"
	}
	return "high"
}

func roundedPercent(numerator, denominator int) float64 {
	if denominator == 0 {
		return 100
	}
	return float64(int(float64(numerator)*1000/float64(denominator)+0.5)) / 10
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func cloneSteps(in []result.RecommendationStep) []result.RecommendationStep {
	return append([]result.RecommendationStep(nil), in...)
}

func cloneVerification(in []result.Verification) []result.Verification {
	out := make([]result.Verification, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].IDs = append([]string(nil), in[i].IDs...)
	}
	return out
}

func sortedSet[T ~string](values map[T]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, string(value))
	}
	sort.Strings(out)
	return out
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		if value != "" {
			seen[value] = true
		}
	}
	return sortedSet(seen)
}

var bearerSecret = regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)[^\s]+`)
var cookieSecret = regexp.MustCompile(`(?i)((?:cookie|set-cookie)\s*:\s*)[^\r\n]+`)
var jsonSecret = regexp.MustCompile(`(?i)("(?:api[_-]?key|authorization|access[_-]?token|cookie|tool[_-]?arguments|arguments)"\s*:\s*")[^"]+(")`)
var signedURL = regexp.MustCompile(`(https?://[^\s"'<>?]+)\?[^\s"'<>)]*`)
var longBase64 = regexp.MustCompile(`[A-Za-z0-9+/]{256,}={0,2}`)

func sanitizeArtifact(input string, limit int) (string, bool) {
	value := bearerSecret.ReplaceAllString(input, `${1}[REDACTED]`)
	value = cookieSecret.ReplaceAllString(value, `${1}[REDACTED]`)
	value = jsonSecret.ReplaceAllString(value, `${1}[REDACTED]${2}`)
	value = signedURL.ReplaceAllString(value, `${1}?[REDACTED]`)
	value = longBase64.ReplaceAllString(value, `[REDACTED BASE64]`)
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value, strings.Contains(value, "… truncated ")
	}
	return value[:limit] + fmt.Sprintf("\n… truncated %d bytes", len(value)-limit), true
}
