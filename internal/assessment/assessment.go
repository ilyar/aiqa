package assessment

import (
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"

	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

type requirementKey struct {
	subjectKind string
	subjectID   string
	id          string
}

// Apply derives canonical requirement assessments from native and Official SDK
// evidence. It is intentionally independent from guidance so recommendations
// cannot influence the conformance result.
func Apply(r *result.Report, p profile.Profile) {
	apply(r, p, "", "")
}

// ApplyTarget derives assessments whose subject and evidence identities remain
// globally unique in a multi-target report.
func ApplyTarget(r *result.Report, p profile.Profile, targetID, protocol string) {
	apply(r, p, targetID, protocol)
}

// ApplyGloballyUnmatchedRequestedScope records an explicit local coverage gap
// after the coordinator has established that no requested model matched any
// target inventory. A missing model on one target is not enough: --model is a
// cross-target filter, so another target may have evaluated that same scope.
//
// The guard deliberately adds no synthetic model ID and makes no provider
// claim. It records the missing AIQA execution scope as incomplete core
// evidence, which keeps an existing core failure authoritative while making
// the result non-rankable.
func ApplyGloballyUnmatchedRequestedScope(r *result.Report, targetID, protocol string, globallyUnmatched bool) bool {
	if !globallyUnmatched || len(r.Config.RequestedModels) == 0 || len(r.Models) != 0 {
		return false
	}
	provider := result.SubjectRef{TargetID: targetID, Protocol: protocol, Kind: "provider", ID: normalizedEndpoint(r.Config.BaseURL)}
	filtered := r.Requirements[:0]
	for _, requirement := range r.Requirements {
		if requirement.Subject == provider && requirement.ID == "selection.evaluated_model_scope" {
			continue
		}
		filtered = append(filtered, requirement)
	}
	r.Requirements = filtered
	for _, requirement := range r.Requirements {
		if requirement.Subject == provider && requirement.ID == "selection.requested_models" {
			return false
		}
	}
	requirement := result.RequirementAssessment{
		ID:            "selection.requested_models",
		Name:          "Requested model selection",
		Description:   "At least one requested model must match an inventory before model conformance can be assessed",
		Level:         result.RequirementCore,
		Subject:       provider,
		Module:        "selection",
		Applicability: result.ApplicabilityApplicable,
		Status:        result.RequirementSkip,
		FailureKind:   "no_evaluated_requested_scope",
		Evidence: []result.EvidenceRef{{
			ID:      evidenceID("coverage", provider, "requested-models-no-match"),
			Kind:    "coverage",
			ProbeID: "aiqa.local.selection.requested_models",
			Status:  result.StatusSkip,
			Summary: "NO_MATCH: no requested model matched any target inventory; no model route was evaluated",
			Signals: []result.EvidenceSignal{{Code: "execution.requested_models.no_match", RootCause: "no-evaluated-requested-scope"}},
		}},
	}
	r.Requirements = append(r.Requirements, requirement)
	sort.Slice(r.Requirements, func(i, j int) bool {
		a, b := r.Requirements[i], r.Requirements[j]
		if a.Subject.Kind != b.Subject.Kind {
			return a.Subject.Kind < b.Subject.Kind
		}
		if a.Subject.ID != b.Subject.ID {
			return a.Subject.ID < b.Subject.ID
		}
		return a.ID < b.ID
	})
	refreshAssessment(r, targetID != "")
	// No evaluated requested scope makes comparison invalid even when a separate
	// provider core failure remains the primary verdict.
	r.Summary.Assessment.Comparison.Rankable = false
	r.Summary.Assessment.Comparison.VerdictRank = nil
	return true
}

func apply(r *result.Report, p profile.Profile, targetID, protocol string) {
	ensureSignals(r)
	normalizeMarkerObservations(r)
	// A 401/403 on a request using the configured credential establishes an
	// account/configuration blocker, not a protocol defect. Preserve the raw
	// HTTP observation, but make its canonical effect incomplete evidence. The
	// explicit invalid-credential controls are excluded: their expected 401/403
	// responses are conformance evidence and a failed control must remain a
	// genuine failure.
	normalizeCredentialRejections(r)
	items := map[requirementKey]*result.RequirementAssessment{}
	provider := result.SubjectRef{TargetID: targetID, Protocol: protocol, Kind: "provider", ID: normalizedEndpoint(r.Config.BaseURL)}

	for i := range r.Discovery.Tests {
		addNative(items, provider, &r.Discovery.Tests[i], p)
	}
	for i := range r.Discovery.Compatibility {
		addNative(items, provider, &r.Discovery.Compatibility[i], p)
	}
	for modelIndex := range r.Models {
		model := &r.Models[modelIndex]
		subject := result.SubjectRef{TargetID: targetID, Protocol: protocol, Kind: "model", ID: model.ID}
		for i := range model.Compatibility {
			addNative(items, subject, &model.Compatibility[i], p)
		}
		for i := range model.Parameters {
			addParameter(items, subject, &model.Parameters[i])
		}
		if model.Context != nil {
			addContext(items, subject, model.Context)
		}
		if nativeCoverageSelected(r.Config.Tests) {
			materializeModuleCoverage(items, subject, model.Modules, p, r.Config.Tests)
		}
		if protocol == "anthropic" && containsString(r.Config.Tests, "model-param") && len(model.Parameters) == 0 && model.Context == nil {
			materializeModelParameterImplementationGap(items, subject)
		}
	}
	for runIndex := range r.Extensions {
		run := &r.Extensions[runIndex]
		for suiteIndex := range run.Suites {
			addExtension(items, &run.Suites[suiteIndex], p)
		}
		addExtensionRunner(items, run, p)
	}
	if nativeCoverageSelected(r.Config.Tests) && len(r.Models) > 0 {
		materializeProviderCoverage(items, provider, p)
	}
	materializeEmptyModelScope(items, r, provider)

	reconcileSpeculativeImageCoverage(items, r)
	for _, item := range items {
		finalizeRequirement(item)
	}
	reconcileRelatedEvidence(items)
	materializeUnmatchedProfileScope(items, r, provider, p)
	requirements := make([]result.RequirementAssessment, 0, len(items))
	for _, item := range items {
		requirements = append(requirements, *item)
	}
	sort.Slice(requirements, func(i, j int) bool {
		a, b := requirements[i], requirements[j]
		if a.Subject.Kind != b.Subject.Kind {
			return a.Subject.Kind < b.Subject.Kind
		}
		if a.Subject.ID != b.Subject.ID {
			return a.Subject.ID < b.Subject.ID
		}
		return a.ID < b.ID
	})
	r.Requirements = requirements
	refreshAssessment(r, targetID != "")
}

// A missing requested marker or tool call is a model-semantic observation only
// when the producing runner explicitly labels it as such. Do not infer this
// from IDs or text: malformed protocol envelopes, SSE frames, tool calls, and
// structured output must retain their independently established failures.
func normalizeMarkerObservations(r *result.Report) {
	normalizeTest := func(test *result.TestResult) {
		if test.Status == result.StatusFail && hasMarkerObservation(test.Signals) {
			test.Status = result.StatusWarn
		}
	}
	for i := range r.Discovery.Tests {
		normalizeTest(&r.Discovery.Tests[i])
	}
	for i := range r.Discovery.Compatibility {
		normalizeTest(&r.Discovery.Compatibility[i])
	}
	for modelIndex := range r.Models {
		for i := range r.Models[modelIndex].Compatibility {
			normalizeTest(&r.Models[modelIndex].Compatibility[i])
		}
	}
	for runIndex := range r.Extensions {
		for suiteIndex := range r.Extensions[runIndex].Suites {
			suite := &r.Extensions[runIndex].Suites[suiteIndex]
			if suite.Status == result.StatusFail && hasMarkerObservation(suite.Signals) {
				suite.Status = result.StatusWarn
			}
		}
	}
}

func hasMarkerObservation(signals []result.EvidenceSignal) bool {
	if len(signals) == 0 {
		return false
	}
	for _, signal := range signals {
		switch signal.Code {
		case "behavior.marker_missing", "behavior.marker_mismatch", "behavior.tool_not_called":
			// Only purely semantic observations can be downgraded. Any other
			// signal may independently establish a protocol failure.
		default:
			return false
		}
	}
	return true
}

func normalizeCredentialRejections(r *result.Report) {
	normalizeTest := func(test *result.TestResult) {
		if !credentialRejectionBlocks(test.ID, test.HTTP.StatusCode) || hasSpecificExecutionBlocker(test.Signals) {
			return
		}
		test.Status = result.StatusError
		test.Summary = "Configured credentials were rejected; compatibility outcome is inconclusive"
		test.Signals = appendCredentialRejectionSignal(test.Signals, test.HTTP.StatusCode)
	}
	normalizeParameter := func(parameter *result.ParameterResult) {
		if (parameter.HTTP.StatusCode != 401 && parameter.HTTP.StatusCode != 403) || hasSpecificExecutionBlocker(parameter.Signals) {
			return
		}
		parameter.Status = result.StatusError
		parameter.Summary = "Configured credentials were rejected; compatibility outcome is inconclusive"
		parameter.Signals = appendCredentialRejectionSignal(parameter.Signals, parameter.HTTP.StatusCode)
	}
	for i := range r.Discovery.Tests {
		normalizeTest(&r.Discovery.Tests[i])
	}
	for i := range r.Discovery.Compatibility {
		normalizeTest(&r.Discovery.Compatibility[i])
	}
	for modelIndex := range r.Models {
		for i := range r.Models[modelIndex].Compatibility {
			normalizeTest(&r.Models[modelIndex].Compatibility[i])
		}
		for i := range r.Models[modelIndex].Parameters {
			normalizeParameter(&r.Models[modelIndex].Parameters[i])
		}
	}
	for runIndex := range r.Extensions {
		for suiteIndex := range r.Extensions[runIndex].Suites {
			suite := &r.Extensions[runIndex].Suites[suiteIndex]
			if (suite.HTTP.StatusCode != 401 && suite.HTTP.StatusCode != 403) || hasSpecificExecutionBlocker(suite.Signals) {
				continue
			}
			suite.Status = result.StatusError
			suite.Summary = "Configured credentials were rejected; compatibility outcome is inconclusive"
			suite.Signals = appendCredentialRejectionSignal(suite.Signals, suite.HTTP.StatusCode)
		}
	}
}

func hasSpecificExecutionBlocker(signals []result.EvidenceSignal) bool {
	for _, signal := range signals {
		switch signal.Code {
		case "execution.model_access_unavailable", "execution.daily_quota_exhausted":
			return true
		}
	}
	return false
}

func credentialRejectionBlocks(id string, status int) bool {
	if status != 401 && status != 403 {
		return false
	}
	// These are deliberately-invalid controls. A response from them tests the
	// error contract rather than the operator's configured credential.
	id = strings.ToLower(id)
	return !strings.Contains(id, "invalid_auth") &&
		!strings.Contains(id, "invalid_api_key") &&
		!strings.Contains(id, "invalid_key")
}

func appendCredentialRejectionSignal(signals []result.EvidenceSignal, status int) []result.EvidenceSignal {
	for _, signal := range signals {
		if signal.Code == "execution.credentials_rejected" {
			return signals
		}
	}
	return append(signals, result.EvidenceSignal{
		Code: "execution.credentials_rejected", RootCause: "configured-credential-rejected",
		Parameters: map[string]any{"http_status": status},
	})
}

// Catalog discovery is not compatibility evidence. If a requested native
// suite has no selected model route at all, retain discovery observations but
// produce explicit core incomplete evidence so the report cannot be ranked as
// a passing target.
func materializeEmptyModelScope(items map[requirementKey]*result.RequirementAssessment, r *result.Report, provider result.SubjectRef) {
	// A global requested-model miss is materialized later by the coordinator;
	// avoid duplicating its more precise evidence here.
	if provider.TargetID == "" || len(r.Config.Tests) == 0 || len(r.Models) != 0 {
		return
	}
	item := get(items, provider, "selection.evaluated_model_scope", "Evaluated model scope", "At least one model route must be evaluated to establish model compatibility", result.RequirementCore, "selection")
	item.Applicability = result.ApplicabilityApplicable
	item.Status = result.RequirementSkip
	item.FailureKind = "no_evaluated_model_scope"
	item.Evidence = []result.EvidenceRef{{
		ID: evidenceID("coverage", provider, "no-evaluated-model-scope"), Kind: "coverage",
		ProbeID: "aiqa.local.selection.evaluated_model_scope", Status: result.StatusSkip,
		Summary: "No selected model route was evaluated; catalog discovery alone does not establish model compatibility",
		Signals: []result.EvidenceSignal{{Code: "execution.model_scope.empty", RootCause: "no-evaluated-model-scope"}},
	}}
}

// A capability trial is not a claim that the model implements that route.
// Resolve only a narrowly identified model rejection, retaining every raw
// observation. Successful positive probes or other failures prevent exclusion.
func reconcileSpeculativeImageCoverage(items map[requirementKey]*result.RequirementAssessment, r *result.Report) {
	for mi := range r.Models {
		model := &r.Models[mi]
		for ai := range model.Modules {
			module := &model.Modules[ai]
			if module.ID != "images-generations" || module.Source != "probe_policy" || !module.Requested {
				continue
			}
			negative, conflicting := false, false
			for _, item := range items {
				if item.Subject.Kind != "model" || item.Subject.ID != model.ID || item.Module != module.ID {
					continue
				}
				for _, ev := range item.Evidence {
					marked := false
					for _, signal := range ev.Signals {
						if signal.Code == "capability.probe.unsupported" && ev.Status == result.StatusUnsupported {
							marked, negative = true, true
						}
					}
					control := item.ID == "images.generate.invalid_json" || item.ID == "images.generate.invalid_model"
					if ev.Status == result.StatusPass && !control || ev.Status == result.StatusFail || ev.Status == result.StatusError || ev.Status == result.StatusWarn {
						conflicting = true
					}
					if ev.Status == result.StatusUnsupported && !marked && ev.Kind != "extension" {
						conflicting = true
					}
				}
			}
			if !negative || conflicting {
				module.Applicability = result.ApplicabilityApplicable
				continue
			}
			module.Applicability = result.ApplicabilityNotApplicable
			module.Reason = "Capability probe found that the selected model is unavailable on the image generation route"
			for _, item := range items {
				if item.Subject.Kind == "model" && item.Subject.ID == model.ID && item.Module == module.ID {
					item.Applicability = result.ApplicabilityNotApplicable
				}
			}
		}
	}
}

// A catalog-only success cannot establish model compatibility when every
// requested route is excluded by the advertised capabilities.
func materializeUnmatchedProfileScope(items map[requirementKey]*result.RequirementAssessment, r *result.Report, provider result.SubjectRef, p profile.Profile) {
	coverage := coverageForModels(r.Models)
	if !nativeCoverageSelected(r.Config.Tests) || coverage.Expected == 0 || coverage.Expected != coverage.Excluded {
		return
	}
	item := get(items, provider, "selection.profile_scope", "Selected profile model coverage", "At least one selected model route must be evaluated to establish model compatibility", result.RequirementCore, "selection")
	item.Applicability = result.ApplicabilityApplicable
	item.Status = result.RequirementSkip
	item.FailureKind = "no_evaluated_profile_scope"
	item.Evidence = []result.EvidenceRef{{
		ID: evidenceID("coverage", provider, "profile-scope-no-match"), Kind: "coverage",
		ProbeID: "aiqa.local.selection.profile_scope", Status: result.StatusSkip,
		Summary: "No selected model advertises a route covered by the requested profile; catalog discovery alone does not establish model compatibility",
		Signals: []result.EvidenceSignal{{Code: "execution.profile_scope.no_match", RootCause: "no-evaluated-profile-scope", Parameters: map[string]any{"profile_id": p.ID}}},
	}}
}

func refreshAssessment(r *result.Report, target bool) {
	assessment := summarize(r.Requirements)
	assessment.Coverage = coverageForModels(r.Models)
	applyCoverageLevels(&assessment, r.Requirements)
	applyCoverageVerdict(&assessment)
	for _, requirement := range r.Requirements {
		if requirement.FailureKind == "no_evaluated_profile_scope" || requirement.FailureKind == "no_evaluated_model_scope" {
			assessment.Comparison.Rankable = false
			assessment.Comparison.VerdictRank = nil
		}
	}
	r.Summary.Assessment = &assessment
	if target {
		applyModelAssessments(r, r.Requirements)
		// Assessment is the canonical target verdict. Raw summaries remain on
		// individual models, where not-evaluated is still meaningful.
		r.Summary.Status = assessment.Verdict
	}
}

func applyCoverageLevels(assessment *result.ConformanceAssessment, requirements []result.RequirementAssessment) {
	core, optional := map[string]bool{}, map[string]bool{}
	for _, requirement := range requirements {
		if requirement.Subject.Kind != "model" || requirement.Applicability != result.ApplicabilityUnknown {
			continue
		}
		key := requirement.Subject.TargetID + "\x00" + requirement.Subject.ID + "\x00" + requirement.Module
		if requirement.Level == result.RequirementCore {
			core[key] = true
		} else {
			optional[key] = true
		}
	}
	assessment.Coverage.CoreUnresolved = len(core)
	for key := range optional {
		if !core[key] {
			assessment.Coverage.OptionalUnresolved++
		}
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func nativeCoverageSelected(tests []string) bool {
	return len(tests) == 0 || containsString(tests, "compat")
}

func applyModelAssessments(r *result.Report, requirements []result.RequirementAssessment) {
	byModel := map[string][]result.RequirementAssessment{}
	for _, requirement := range requirements {
		if requirement.Subject.Kind == "model" && requirement.Applicability != result.ApplicabilityNotApplicable {
			byModel[requirement.Subject.ID] = append(byModel[requirement.Subject.ID], requirement)
		}
	}
	for index := range r.Models {
		coverage := coverageForModels([]result.ModelReport{r.Models[index]})
		if nativeCoverageSelected(r.Config.Tests) && coverage.Expected > 0 && coverage.Expected == coverage.Excluded {
			r.Models[index].Assessment.Verdict = "not-evaluated"
			continue
		}
		modelRequirements := byModel[r.Models[index].ID]
		if len(modelRequirements) == 0 {
			if coverageForModels([]result.ModelReport{r.Models[index]}).Unresolved > 0 {
				r.Models[index].Assessment.Verdict = "not-evaluated"
			}
			continue
		}
		assessment := summarize(modelRequirements)
		assessment.Coverage = coverageForModels([]result.ModelReport{r.Models[index]})
		applyCoverageVerdict(&assessment)
		r.Models[index].Assessment.Verdict = assessment.Verdict
	}
}

func addNative(items map[requirementKey]*result.RequirementAssessment, subject result.SubjectRef, test *result.TestResult, p profile.Profile) {
	id := test.RequirementID
	if id == "" {
		id = test.ID
		test.RequirementID = id
	}
	if test.ProbeID == "" {
		test.ProbeID = "aiqa.native." + test.ID
	}
	spec := p.Probe(id)
	level := result.RequirementOptional
	if test.Required || spec.Required {
		level = result.RequirementCore
	}
	item := get(items, subject, id, test.Name, test.Expected, level, moduleForProbe(p, id))
	if test.Applicability != "" {
		item.Applicability = test.Applicability
	}
	item.Evidence = append(item.Evidence, result.EvidenceRef{
		ID:         evidenceID("native", subject, test.ProbeID),
		Kind:       "native",
		ProbeID:    test.ProbeID,
		Status:     test.Status,
		Summary:    test.Summary,
		HTTPStatus: test.HTTP.StatusCode,
		Signals:    cloneSignals(test.Signals),
	})
	appendBlockedBy(item, test.Signals)
}

func addExtension(items map[requirementKey]*result.RequirementAssessment, suite *result.ExtensionSuiteResult, p profile.Profile) {
	if !suite.Bound || suite.RequirementID == "" {
		return
	}
	spec := p.Probe(suite.RequirementID)
	level := result.RequirementOptional
	if spec.Required {
		level = result.RequirementCore
	}
	item := get(items, suite.Subject, suite.RequirementID, spec.Name, spec.Description, level, moduleForProbe(p, suite.RequirementID))
	item.Evidence = append(item.Evidence, result.EvidenceRef{
		ID:                    evidenceID("extension", suite.Subject, suite.ExtensionID+"."+suite.SuiteID),
		Kind:                  "extension",
		Suite:                 suite.SuiteID,
		Status:                suite.Status,
		Summary:               suite.Summary,
		HTTPStatus:            suite.HTTP.StatusCode,
		ArtifactRef:           suite.ArtifactRef,
		RelatedRequirementIDs: append([]string(nil), suite.RelatedRequirementIDs...),
		Signals:               cloneSignals(suite.Signals),
	})
}

func addExtensionRunner(items map[requirementKey]*result.RequirementAssessment, run *result.ExtensionRun, p profile.Profile) {
	if run.Runner.Status != result.StatusError {
		return
	}
	for _, suite := range run.Suites {
		if !suite.Bound || suite.RequirementID == "" {
			continue
		}
		spec := p.Probe(suite.RequirementID)
		level := result.RequirementOptional
		if spec.Required {
			level = result.RequirementCore
		}
		item := get(items, suite.Subject, suite.RequirementID, spec.Name, spec.Description, level, moduleForProbe(p, suite.RequirementID))
		item.Evidence = append(item.Evidence, result.EvidenceRef{
			ID:          evidenceID("extension-runner", suite.Subject, suite.ExtensionID+"."+suite.SuiteID),
			Kind:        "extension_runner",
			Suite:       suite.SuiteID,
			Status:      result.StatusError,
			Summary:     run.Runner.Summary,
			ArtifactRef: run.Runner.ArtifactRef,
			Signals:     []result.EvidenceSignal{{Code: "extension.runner.failure", Parameters: map[string]any{"extension_id": suite.ExtensionID}}},
		})
	}
}

func reconcileRelatedEvidence(items map[requirementKey]*result.RequirementAssessment) {
	for key, item := range items {
		isExtension := false
		for _, evidence := range item.Evidence {
			if evidence.Kind == "extension" {
				isExtension = true
				break
			}
		}
		if !isExtension {
			continue
		}
		conflict := false
		additional := map[string]result.EvidenceRef{}
		for _, evidence := range item.Evidence {
			for _, relatedID := range evidence.RelatedRequirementIDs {
				related := items[requirementKey{subjectKind: key.subjectKind, subjectID: key.subjectID, id: relatedID}]
				if related == nil {
					continue
				}
				if evidence.Status == result.StatusPass && (related.Status == result.RequirementFail || related.Status == result.RequirementUnsupported) {
					conflict = true
				}
				if (evidence.Status == result.StatusFail || evidence.Status == result.StatusUnsupported) && related.Status == result.RequirementPass {
					conflict = true
				}
				for _, relatedEvidence := range related.Evidence {
					additional[relatedEvidence.ID] = relatedEvidence
				}
			}
		}
		if conflict {
			for _, relatedEvidence := range additional {
				item.Evidence = append(item.Evidence, relatedEvidence)
			}
			sort.Slice(item.Evidence, func(i, j int) bool { return item.Evidence[i].ID < item.Evidence[j].ID })
			item.Status = result.RequirementWarn
			item.FailureKind = "evidence_conflict"
		}
	}
}

func addParameter(items map[requirementKey]*result.RequirementAssessment, subject result.SubjectRef, parameter *result.ParameterResult) {
	id := "parameter." + normalizedID(parameter.Name)
	if parameter.Variant != "" {
		id += "." + normalizedID(parameter.Variant)
	}
	item := get(items, subject, id, "Model parameter: "+parameter.Name, "The parameter is accepted, validated, and effective where observable", result.RequirementOptional, "model-param")
	item.Evidence = append(item.Evidence, result.EvidenceRef{
		ID:         evidenceID("native", subject, id),
		Kind:       "native",
		ProbeID:    "aiqa.native." + id,
		Status:     parameter.Status,
		Summary:    parameter.Summary,
		HTTPStatus: parameter.HTTP.StatusCode,
		Signals:    cloneSignals(parameter.Signals),
	})
}

func addContext(items map[requirementKey]*result.RequirementAssessment, subject result.SubjectRef, contextResult *result.ContextProbeResult) {
	id := "parameter.context_window"
	item := get(items, subject, id, "Context window", "The accepted context boundary is measured within configured limits", result.RequirementOptional, "model-param")
	item.Evidence = append(item.Evidence, result.EvidenceRef{
		ID:      evidenceID("native", subject, id),
		Kind:    "native",
		ProbeID: "aiqa.native." + id,
		Status:  contextResult.Status,
		Summary: contextResult.Summary,
		Signals: []result.EvidenceSignal{{Code: genericSignalCode(id, contextResult.Status)}},
	})
}

// materializeModelParameterImplementationGap is local AIQA coverage evidence,
// not a statement about the provider. Anthropic model-parameter probing has no
// runner yet, so a selected model-param suite must not silently look complete
// or turn into provider UNSUPPORTED evidence.
func materializeModelParameterImplementationGap(items map[requirementKey]*result.RequirementAssessment, subject result.SubjectRef) {
	const id = "aiqa.model_param.implementation"
	item := get(items, subject, id, "AIQA model-parameter implementation", "AIQA has no Anthropic model-parameter runner for this selected suite", result.RequirementOptional, "model-param")
	if len(item.Evidence) != 0 {
		return
	}
	item.FailureKind = "not_implemented_by_aiqa"
	item.Evidence = append(item.Evidence, result.EvidenceRef{
		ID:      evidenceID("aiqa", subject, id),
		Kind:    "aiqa",
		ProbeID: "aiqa.local.model_param.implementation",
		Status:  result.StatusSkip,
		Summary: "not implemented by AIQA: Anthropic model-parameter analysis made no provider calls",
		Signals: []result.EvidenceSignal{{Code: "aiqa.model_param.not_implemented", RootCause: "aiqa-implementation-gap"}},
	})
}

func get(items map[requirementKey]*result.RequirementAssessment, subject result.SubjectRef, id, name, description string, level result.RequirementLevel, module string) *result.RequirementAssessment {
	key := requirementKey{subjectKind: subject.Kind, subjectID: subject.ID, id: id}
	if existing := items[key]; existing != nil {
		if level == result.RequirementCore {
			existing.Level = level
		}
		return existing
	}
	item := &result.RequirementAssessment{
		ID:            id,
		Name:          name,
		Description:   description,
		Level:         level,
		Subject:       subject,
		Module:        module,
		Applicability: result.ApplicabilityApplicable,
	}
	items[key] = item
	return item
}

func finalizeRequirement(item *result.RequirementAssessment) {
	sort.Slice(item.Evidence, func(i, j int) bool { return item.Evidence[i].ID < item.Evidence[j].ID })
	item.BlockedBy = uniqueSorted(item.BlockedBy)
	if item.Applicability == result.ApplicabilityNotApplicable {
		item.Status = result.RequirementNotApplicable
		return
	}
	hasPass, hasFail, hasUnsupported := false, false, false
	hasWarn, hasSkip, hasError := false, false, false
	for _, evidence := range item.Evidence {
		switch evidence.Status {
		case result.StatusPass:
			hasPass = true
		case result.StatusFail:
			hasFail = true
		case result.StatusUnsupported:
			hasUnsupported = true
		case result.StatusWarn:
			hasWarn = true
		case result.StatusSkip:
			hasSkip = true
		case result.StatusError:
			hasError = true
		}
	}
	if hasPass && (hasFail || hasUnsupported) {
		item.Status = result.RequirementWarn
		item.FailureKind = "evidence_conflict"
		return
	}
	switch {
	case hasFail:
		item.Status = result.RequirementFail
	case hasUnsupported && item.Level == result.RequirementCore:
		item.Status = result.RequirementFail
		item.FailureKind = "required_unsupported"
	case hasUnsupported:
		item.Status = result.RequirementUnsupported
	case hasError && hasPass:
		item.Status = result.RequirementWarn
		item.FailureKind = "evidence_gap"
	case hasError:
		item.Status = result.RequirementError
	case hasWarn:
		item.Status = result.RequirementWarn
	case hasPass:
		item.Status = result.RequirementPass
	case hasSkip:
		item.Status = result.RequirementSkip
	default:
		item.Status = result.RequirementSkip
	}
}

func materializeModuleCoverage(items map[requirementKey]*result.RequirementAssessment, subject result.SubjectRef, assessments []result.ModuleAssessment, p profile.Profile, tests []string) {
	modules := make(map[string]profile.Module, len(p.Modules))
	for _, module := range p.Modules {
		modules[module.ID] = module
	}
	for _, assessment := range assessments {
		if !assessment.Requested {
			continue
		}
		module, ok := modules[assessment.ID]
		if !ok {
			continue
		}
		for _, id := range module.ProbeIDs {
			spec := p.Probe(id)
			if !nativeProbeSelected(spec, tests) {
				continue
			}
			level := result.RequirementOptional
			if spec.Required {
				level = result.RequirementCore
			}
			item := get(items, subject, id, spec.Name, spec.Description, level, module.ID)
			if len(item.Evidence) != 0 {
				continue
			}
			switch assessment.Applicability {
			case result.ApplicabilityNotApplicable:
				item.Applicability = result.ApplicabilityNotApplicable
				item.Status = result.RequirementNotApplicable
			case result.ApplicabilityUnknown:
				item.Applicability = result.ApplicabilityUnknown
				item.Status = result.RequirementSkip
			default:
				item.Applicability = result.ApplicabilityApplicable
				if assessment.Execution == result.ExecutionExecuted {
					item.Status = result.RequirementError
					item.FailureKind = "coverage_missing_probe"
					item.Evidence = append(item.Evidence, result.EvidenceRef{
						ID: evidenceID("coverage", subject, assessment.ID), Kind: "coverage", Status: result.StatusError,
						Summary: "module was marked executed but did not return this declared probe",
						Signals: []result.EvidenceSignal{{Code: "execution.module_probe_missing", RootCause: "module-coverage-missing"}},
					})
				} else {
					item.Status = result.RequirementSkip
					item.FailureKind = "module_not_executed"
				}
			}
			item.BlockedBy = append(item.BlockedBy, assessment.BlockedBy...)
		}
	}
}

func coverageForModels(models []result.ModelReport) result.CoverageMeasure {
	var out result.CoverageMeasure
	for _, model := range models {
		for _, module := range model.Modules {
			if !module.Requested {
				continue
			}
			out.Expected++
			switch module.Applicability {
			case result.ApplicabilityUnknown:
				out.Unresolved++
			case result.ApplicabilityNotApplicable:
				out.Excluded++
			case result.ApplicabilityApplicable:
				if module.Execution == result.ExecutionExecuted {
					out.Executed++
				} else {
					out.Unresolved++
				}
			}
		}
	}
	return out
}

func applyCoverageVerdict(assessment *result.ConformanceAssessment) {
	if assessment.Coverage.Unresolved == 0 {
		return
	}
	assessment.Comparison.Rankable = false
	assessment.Comparison.VerdictRank = nil
	if assessment.Core.Fail > 0 {
		return
	}
	if assessment.Coverage.CoreUnresolved > 0 || assessment.Core.Skip > 0 || assessment.Core.Warn > 0 || assessment.Core.Error > 0 {
		assessment.Verdict, assessment.VerdictReason = "inconclusive", "core_coverage_unresolved"
		return
	}
	assessment.Verdict, assessment.VerdictReason = "pass-with-gaps", "optional_coverage_unresolved"
}

func nativeProbeSelected(spec profile.Probe, tests []string) bool {
	switch spec.Category {
	case "parameters":
		return containsString(tests, "model-param")
	default:
		return nativeCoverageSelected(tests)
	}
}

func materializeProviderCoverage(items map[requirementKey]*result.RequirementAssessment, subject result.SubjectRef, p profile.Profile) {
	for _, module := range p.Modules {
		if module.Subject != "provider" {
			continue
		}
		for _, id := range module.ProbeIDs {
			key := requirementKey{subjectKind: subject.Kind, subjectID: subject.ID, id: id}
			if items[key] != nil {
				continue
			}
			spec := p.Probe(id)
			level := result.RequirementOptional
			if spec.Required {
				level = result.RequirementCore
			}
			item := get(items, subject, id, spec.Name, spec.Description, level, module.ID)
			item.Status = result.RequirementError
			item.FailureKind = "coverage_missing_probe"
			item.Evidence = append(item.Evidence, result.EvidenceRef{
				ID: evidenceID("coverage", subject, module.ID), Kind: "coverage", Status: result.StatusError,
				Summary: "provider module was marked executed but did not return this declared probe",
				Signals: []result.EvidenceSignal{{Code: "execution.module_probe_missing", RootCause: "module-coverage-missing"}},
			})
		}
	}
}

func summarize(requirements []result.RequirementAssessment) result.ConformanceAssessment {
	var out result.ConformanceAssessment
	out.Comparison.Policy = "verdict-core-optional-evidence/v1"
	for _, requirement := range requirements {
		if requirement.Status == result.RequirementNotApplicable || requirement.Applicability == result.ApplicabilityNotApplicable {
			continue
		}
		measure := &out.Optional
		if requirement.Level == result.RequirementCore {
			measure = &out.Core
		}
		addRequirementStatus(measure, requirement.Status)
		out.Evidence.Total++
		if requirement.Status != result.RequirementSkip {
			out.Evidence.Executed++
		}
		if requirement.Status == result.RequirementPass || requirement.Status == result.RequirementFail || requirement.Status == result.RequirementUnsupported {
			out.Evidence.Conclusive++
		}
	}
	out.Core.Percent = percent(out.Core.Pass, out.Core.Total)
	out.Optional.Percent = percent(out.Optional.Pass, out.Optional.Total)
	out.Evidence.ExecutionPercent = percent(out.Evidence.Executed, out.Evidence.Total)
	out.Evidence.ConclusivePercent = percent(out.Evidence.Conclusive, out.Evidence.Total)

	switch {
	case out.Core.Fail > 0:
		out.Verdict, out.VerdictReason = "fail", "core_failure"
	case out.Core.Warn > 0 || out.Core.Skip > 0 || out.Core.Error > 0:
		out.Verdict, out.VerdictReason = "inconclusive", "core_evidence_incomplete"
	case out.Optional.Fail > 0:
		out.Verdict, out.VerdictReason = "partial", "optional_failure"
	case out.Optional.Unsupported > 0 || out.Optional.Warn > 0 || out.Optional.Skip > 0 || out.Optional.Error > 0:
		out.Verdict, out.VerdictReason = "pass-with-gaps", "optional_gap_or_incomplete_evidence"
	default:
		out.Verdict, out.VerdictReason = "pass", "all_applicable_requirements_passed"
	}
	if out.Verdict == "inconclusive" {
		out.Comparison.Rankable = false
		return out
	}
	ranks := map[string]int{"fail": 0, "partial": 1, "pass-with-gaps": 2, "pass": 3}
	rank := ranks[out.Verdict]
	out.Comparison.Rankable = true
	out.Comparison.VerdictRank = &rank
	return out
}

func addRequirementStatus(measure *result.RequirementMeasure, status result.RequirementStatus) {
	measure.Total++
	switch status {
	case result.RequirementPass:
		measure.Pass++
	case result.RequirementFail:
		measure.Fail++
	case result.RequirementUnsupported:
		measure.Unsupported++
	case result.RequirementWarn:
		measure.Warn++
	case result.RequirementSkip:
		measure.Skip++
	case result.RequirementError:
		measure.Error++
	}
}

func percent(numerator, denominator int) *float64 {
	if denominator == 0 {
		return nil
	}
	value := math.Round(float64(numerator)*1000/float64(denominator)) / 10
	return &value
}

func ensureSignals(r *result.Report) {
	for i := range r.Discovery.Tests {
		ensureTestSignals(&r.Discovery.Tests[i])
	}
	for i := range r.Discovery.Compatibility {
		ensureTestSignals(&r.Discovery.Compatibility[i])
	}
	for modelIndex := range r.Models {
		model := &r.Models[modelIndex]
		for i := range model.Compatibility {
			ensureTestSignals(&model.Compatibility[i])
		}
		for i := range model.Parameters {
			if model.Parameters[i].Status != result.StatusPass && len(model.Parameters[i].Signals) == 0 {
				model.Parameters[i].Signals = []result.EvidenceSignal{{Code: genericSignalCode("parameter."+normalizedID(model.Parameters[i].Name), model.Parameters[i].Status)}}
			}
		}
	}
	for runIndex := range r.Extensions {
		for suiteIndex := range r.Extensions[runIndex].Suites {
			suite := &r.Extensions[runIndex].Suites[suiteIndex]
			if suite.Status != result.StatusPass && len(suite.Signals) == 0 {
				suite.Signals = []result.EvidenceSignal{{Code: "extension." + normalizedID(suite.ExtensionID+"."+suite.SuiteID) + "." + string(suite.Status)}}
			}
		}
	}
}

func ensureTestSignals(test *result.TestResult) {
	if test.Status == result.StatusPass || len(test.Signals) > 0 {
		return
	}
	id := test.RequirementID
	if id == "" {
		id = test.ID
	}
	test.Signals = []result.EvidenceSignal{{Code: genericSignalCode(id, test.Status), Parameters: map[string]any{"http_status": test.HTTP.StatusCode}}}
}

func genericSignalCode(id string, status result.Status) string {
	prefix := "requirement"
	switch status {
	case result.StatusUnsupported:
		prefix = "unsupported"
	case result.StatusWarn:
		prefix = "evidence"
	case result.StatusSkip:
		prefix = "skipped"
	case result.StatusError:
		prefix = "execution"
	}
	return prefix + "." + normalizedID(id) + "." + string(status)
}

func moduleForProbe(p profile.Profile, id string) string {
	for _, binding := range p.ExtensionBindings {
		if binding.RequirementID == id {
			return binding.Module
		}
	}
	for _, module := range p.Modules {
		for _, probeID := range module.ProbeIDs {
			if probeID == id || (strings.HasPrefix(id, "chat.error.") && probeID == strings.TrimPrefix(id, "chat.")) {
				return module.ID
			}
		}
	}
	return ""
}

func evidenceID(kind string, subject result.SubjectRef, id string) string {
	if subject.TargetID != "" {
		return fmt.Sprintf("%s:%s:%s:%s:%s", kind, normalizedID(subject.TargetID), subject.Kind, normalizedID(subject.ID), normalizedID(id))
	}
	return fmt.Sprintf("%s:%s:%s:%s", kind, subject.Kind, normalizedID(subject.ID), normalizedID(id))
}

func normalizedEndpoint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return strings.TrimRight(raw, "/")
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/")
}

func normalizedID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDot := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDot = false
			continue
		}
		if !lastDot {
			b.WriteByte('.')
			lastDot = true
		}
	}
	return strings.Trim(b.String(), ".")
}

func appendBlockedBy(item *result.RequirementAssessment, signals []result.EvidenceSignal) {
	for _, signal := range signals {
		if value, ok := signal.Parameters["blocked_by_requirement"].(string); ok && value != "" {
			item.BlockedBy = append(item.BlockedBy, value)
		}
	}
}

func cloneSignals(in []result.EvidenceSignal) []result.EvidenceSignal {
	return append([]result.EvidenceSignal(nil), in...)
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		if value != "" {
			seen[value] = true
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
