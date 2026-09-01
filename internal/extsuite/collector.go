package extsuite

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

type Collector struct {
	manifest Manifest
	request  RunRequest
	profile  profile.Profile
	secret   string
	planned  map[string]int
	seen     map[string]bool
	results  []result.ExtensionSuiteResult
}

func NewCollector(manifest Manifest, request RunRequest, p profile.Profile) (*Collector, error) {
	if err := ValidateManifest(&manifest); err != nil {
		return nil, err
	}
	planned := make(map[string]int, len(request.SuiteIDs))
	known := map[string]SuiteDescriptor{}
	for _, suite := range manifest.Suites {
		known[suite.ID] = suite
	}
	for index, suiteID := range request.SuiteIDs {
		if _, duplicate := planned[suiteID]; duplicate {
			return nil, fmt.Errorf("duplicate planned extension suite %q", suiteID)
		}
		suite, ok := known[suiteID]
		if !ok {
			return nil, fmt.Errorf("unknown planned extension suite %q", suiteID)
		}
		if suite.Subject != request.Subject.Kind {
			return nil, fmt.Errorf("suite %q requires %s subject, got %s", suiteID, suite.Subject, request.Subject.Kind)
		}
		if binding, bound := p.ExtensionBinding(manifest.Extension.ID, suiteID); bound {
			if binding.Subject != suite.Subject {
				return nil, fmt.Errorf("profile binding for %q has subject %q, manifest declares %q", suiteID, binding.Subject, suite.Subject)
			}
			module, ok := p.Module(binding.Module)
			if !ok {
				return nil, fmt.Errorf("profile binding for %q references unknown module %q", suiteID, binding.Module)
			}
			if suite.Cost == "billable_artifact" && module.CostClass != "billable-artifact" {
				return nil, fmt.Errorf("billable suite %q is bound to module %q without billable-artifact cost metadata", suiteID, binding.Module)
			}
		}
		planned[suiteID] = index
	}
	return &Collector{manifest: manifest, request: request, profile: p, secret: request.Credential.Value, planned: planned, seen: map[string]bool{}}, nil
}

func (c *Collector) Emit(event SuiteEvent) error {
	if event.SchemaVersion != ProtocolVersion || event.Type != "suite_result" || event.RunID != c.request.RunID || event.ExtensionID != c.manifest.Extension.ID {
		return fmt.Errorf("invalid extension suite envelope")
	}
	if event.Subject != c.request.Subject {
		return fmt.Errorf("suite %q reported the wrong subject", event.SuiteID)
	}
	if _, ok := c.planned[event.SuiteID]; !ok {
		return fmt.Errorf("extension reported unplanned suite %q", event.SuiteID)
	}
	if c.seen[event.SuiteID] {
		return fmt.Errorf("extension reported suite %q more than once", event.SuiteID)
	}
	if !validOutcome(event.Outcome) || event.DurationMS < 0 || strings.TrimSpace(event.Summary) == "" {
		return fmt.Errorf("suite %q reported an invalid outcome, duration, or summary", event.SuiteID)
	}
	if event.HTTP.StatusCode < 0 || event.HTTP.StatusCode > 599 || event.Outcome == result.ExtensionOutcomeRequestRejected && event.HTTP.StatusCode < 100 {
		return fmt.Errorf("suite %q reported invalid HTTP evidence", event.SuiteID)
	}
	if event.ErrorKind != "" && event.ErrorKind != "capability_rejection" {
		return fmt.Errorf("suite %q reported invalid error kind %q", event.SuiteID, event.ErrorKind)
	}
	if !validUsage(event.Usage) {
		return fmt.Errorf("suite %q reported invalid usage", event.SuiteID)
	}
	for _, signal := range event.Signals {
		if !stableID.MatchString(signal.Code) {
			return fmt.Errorf("suite %q reported invalid signal code %q", event.SuiteID, signal.Code)
		}
	}
	c.seen[event.SuiteID] = true
	binding, bound := c.profile.ExtensionBinding(event.ExtensionID, event.SuiteID)
	item := result.ExtensionSuiteResult{
		ExtensionID: event.ExtensionID,
		SuiteID:     event.SuiteID,
		Subject:     event.Subject,
		Outcome:     event.Outcome,
		Status:      deriveStatus(event, binding, bound, c.profile),
		DurationMS:  event.DurationMS,
		Summary:     redact(event.Summary, c.secret),
		HTTP:        event.HTTP,
		Usage:       event.Usage,
		Signals:     append([]result.EvidenceSignal(nil), event.Signals...),
		Bound:       bound,
		Error:       redact(event.Error, c.secret),
	}
	item.HTTP.Snippet = redact(item.HTTP.Snippet, c.secret)
	result.RedactSecrets(&item, c.secret)
	if bound {
		item.RequirementID = binding.RequirementID
		item.RelatedRequirementIDs = append([]string(nil), binding.RelatedRequirementIDs...)
	}
	c.results = append(c.results, item)
	return nil
}

func validUsage(usage result.Usage) bool {
	values := []int64{usage.InputTokens, usage.OutputTokens, usage.CacheCreationInputTokens, usage.CacheReadInputTokens, usage.ThinkingTokens, usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens, usage.ReasoningTokens}
	for _, value := range values {
		if value < 0 {
			return false
		}
	}
	for _, value := range usage.ProviderFields {
		if value < 0 {
			return false
		}
	}
	return true
}

func (c *Collector) Finish(provenance result.ExtensionProvenance, telemetry RunnerTelemetry, runErr error, started time.Time) result.ExtensionRun {
	problems := []string{}
	if runErr != nil {
		problems = append(problems, redact(runErr.Error(), c.secret))
	}
	for _, suiteID := range c.request.SuiteIDs {
		if c.seen[suiteID] {
			continue
		}
		problems = append(problems, "extension did not report planned suite "+suiteID)
		binding, bound := c.profile.ExtensionBinding(c.manifest.Extension.ID, suiteID)
		missing := result.ExtensionSuiteResult{ExtensionID: c.manifest.Extension.ID, SuiteID: suiteID, Subject: c.request.Subject, Outcome: result.ExtensionOutcomeExecutionError, Status: result.StatusError, Summary: "Extension did not report the planned suite", Bound: bound, Signals: []result.EvidenceSignal{{Code: "extension.runner.missing_result"}}}
		if bound {
			missing.RequirementID = binding.RequirementID
			missing.RelatedRequirementIDs = append([]string(nil), binding.RelatedRequirementIDs...)
		}
		c.results = append(c.results, missing)
	}
	sort.Slice(c.results, func(i, j int) bool { return c.planned[c.results[i].SuiteID] < c.planned[c.results[j].SuiteID] })
	if telemetry.TotalDuration <= 0 {
		telemetry.TotalDuration = time.Since(started)
	}
	pacingCompliance := "host_enforced"
	if provenance.Source == "executable" {
		pacingCompliance = "extension_required_unverified"
	}
	runner := result.ExtensionRunnerResult{Status: result.StatusPass, StartupDurationMS: telemetry.StartupDuration.Milliseconds(), TotalDurationMS: telemetry.TotalDuration.Milliseconds(), ExitCode: telemetry.ExitCode, PacingCompliance: pacingCompliance}
	if len(problems) > 0 {
		runner.Status = result.StatusError
		runner.Summary = "Extension runner did not complete cleanly"
		runner.Error = strings.Join(problems, "; ")
		runner.Output = telemetry.Stderr
	} else {
		runner.Summary = "Extension runner completed"
	}
	return result.ExtensionRun{Provenance: provenance, TargetID: c.request.Target.ID, Subject: c.request.Subject, Runner: runner, Suites: append([]result.ExtensionSuiteResult(nil), c.results...)}
}

func validOutcome(outcome result.ExtensionOutcome) bool {
	switch outcome {
	case result.ExtensionOutcomePass, result.ExtensionOutcomeAssertionFailed, result.ExtensionOutcomeRequestRejected, result.ExtensionOutcomeExecutionError, result.ExtensionOutcomeSkip:
		return true
	default:
		return false
	}
}

func deriveStatus(event SuiteEvent, binding profile.ExtensionBinding, bound bool, p profile.Profile) result.Status {
	switch event.Outcome {
	case result.ExtensionOutcomePass:
		return result.StatusPass
	case result.ExtensionOutcomeAssertionFailed:
		return result.StatusFail
	case result.ExtensionOutcomeExecutionError:
		return result.StatusError
	case result.ExtensionOutcomeSkip:
		return result.StatusSkip
	case result.ExtensionOutcomeRequestRejected:
		status := event.HTTP.StatusCode
		if status == 408 || status == 429 || status >= 500 || status == 0 {
			return result.StatusError
		}
		if bound && event.ErrorKind == "capability_rejection" && (status == 400 || status == 404 || status == 422) && !p.Probe(binding.RequirementID).Required {
			return result.StatusUnsupported
		}
		return result.StatusFail
	default:
		return result.StatusError
	}
}

func redact(value, secret string) string {
	return sanitizeDiagnostic([]byte(value), secret)
}
