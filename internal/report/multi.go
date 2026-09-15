package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/result"
)

// FinalV3 is one immutable public-report snapshot.  It is created only after
// all public attachments (including a forensic verdict) have their final
// values.  File output and an explicitly selected stdout payload must use the
// same snapshot, so neither can make a stale pending/final claim.
type FinalV3 struct {
	payloads map[string][]byte
}

// FinalizeV3 redacts and normalizes a report once, then serializes every
// configured format before any output path is modified.  This is deliberately
// separate from Commit so callers can write an exact final payload to stdout
// without re-rendering it.
func FinalizeV3(r result.ReportV3, cfg config.Config) (FinalV3, error) {
	result.RedactSecrets(&r, reportSecrets(cfg)...)
	r = normalizeV3Collections(r)
	if err := validateExecutionSummary(r.Execution); err != nil {
		return FinalV3{}, err
	}

	payloads := make(map[string][]byte, len(cfg.Formats))
	for _, format := range cfg.Formats {
		if _, exists := payloads[format]; exists {
			return FinalV3{}, fmt.Errorf("duplicate report format %q", format)
		}
		var data []byte
		var err error
		switch format {
		case "json":
			data, err = json.MarshalIndent(r, "", "  ")
		case "markdown":
			data = []byte(MarkdownV3(r))
		default:
			return FinalV3{}, fmt.Errorf("unsupported format %q", format)
		}
		if err != nil {
			return FinalV3{}, fmt.Errorf("serialize %s report: %w", format, err)
		}
		payloads[format] = append(data, '\n')
	}
	return FinalV3{payloads: payloads}, nil
}

// Bytes returns a defensive copy of one configured report payload.  It is the
// public stdout seam: the caller is responsible for validating its CLI option,
// while this package guarantees the bytes are identical to the committed file.
func (f FinalV3) Bytes(format string) ([]byte, error) {
	data, ok := f.payloads[format]
	if !ok {
		return nil, fmt.Errorf("report format %q was not finalized", format)
	}
	return append([]byte(nil), data...), nil
}

// Commit writes all payloads using a staged, best-effort transactional commit.
// All files are fully rendered and fsynced before any destination is replaced.
// If a replacement fails, prior destinations are restored and no paths are
// claimed as written.  POSIX has no crash-atomic multi-file rename primitive;
// this contract covers ordinary write errors, not abrupt process termination.
func (f FinalV3) Commit(name string) ([]string, error) {
	return f.commit(name, os.Rename)
}

// WriteV3 preserves the historical file-writer API while using the final
// snapshot transaction.  Callers which also need stdout should call
// FinalizeV3 once, then Commit and Bytes on the returned value.
func WriteV3(r result.ReportV3, cfg config.Config) ([]string, error) {
	final, err := FinalizeV3(r, cfg)
	if err != nil {
		return nil, err
	}
	return final.Commit(cfg.Name)
}

type stagedV3Output struct {
	path    string
	tmp     string
	backup  string
	existed bool
	placed  bool
}

func (f FinalV3) commit(name string, rename func(string, string) error) ([]string, error) {
	formats := []string{"json", "markdown"}
	staged := make([]stagedV3Output, 0, len(f.payloads))
	for _, format := range formats {
		data, ok := f.payloads[format]
		if !ok {
			continue
		}
		ext := map[string]string{"json": ".json", "markdown": ".md"}[format]
		path := name + ext
		tmp, err := stageReport(path, data)
		if err != nil {
			removeStagedV3(staged)
			return nil, err
		}
		staged = append(staged, stagedV3Output{path: path, tmp: tmp})
	}

	if err := commitStagedV3(staged, rename); err != nil {
		return nil, err
	}
	paths := make([]string, len(staged))
	for i := range staged {
		paths[i] = staged[i].path
	}
	return paths, nil
}

func stageReport(path string, data []byte) (string, error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".aiqa-report-stage-*")
	if err != nil {
		return "", fmt.Errorf("create staged report %s: %w", path, err)
	}
	tmpName := tmp.Name()
	fail := func(cause error) (string, error) {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", cause
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(fmt.Errorf("write staged report %s: %w", path, err))
	}
	if err := tmp.Chmod(0o644); err != nil {
		return fail(fmt.Errorf("chmod staged report %s: %w", path, err))
	}
	if err := tmp.Sync(); err != nil {
		return fail(fmt.Errorf("sync staged report %s: %w", path, err))
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("close staged report %s: %w", path, err)
	}
	return tmpName, nil
}

func commitStagedV3(staged []stagedV3Output, rename func(string, string) error) (err error) {
	defer func() {
		if err == nil {
			for _, output := range staged {
				if output.backup != "" {
					_ = os.Remove(output.backup)
				}
			}
			return
		}
		err = errors.Join(err, rollbackStagedV3(staged, rename))
	}()

	for i := range staged {
		output := &staged[i]
		if _, statErr := os.Lstat(output.path); statErr == nil {
			backup, createErr := os.CreateTemp(filepath.Dir(output.path), ".aiqa-report-backup-*")
			if createErr != nil {
				return fmt.Errorf("create report backup %s: %w", output.path, createErr)
			}
			output.backup = backup.Name()
			if closeErr := backup.Close(); closeErr != nil {
				return fmt.Errorf("close report backup %s: %w", output.path, closeErr)
			}
			if renameErr := rename(output.path, output.backup); renameErr != nil {
				return fmt.Errorf("backup report %s: %w", output.path, renameErr)
			}
			output.existed = true
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf("stat report destination %s: %w", output.path, statErr)
		}
		if renameErr := rename(output.tmp, output.path); renameErr != nil {
			return fmt.Errorf("commit report %s: %w", output.path, renameErr)
		}
		output.tmp = ""
		output.placed = true
	}
	return nil
}

func rollbackStagedV3(staged []stagedV3Output, rename func(string, string) error) error {
	var errs []error
	for i := len(staged) - 1; i >= 0; i-- {
		output := staged[i]
		if output.placed {
			if removeErr := os.Remove(output.path); removeErr != nil && !os.IsNotExist(removeErr) {
				errs = append(errs, fmt.Errorf("remove partial report %s: %w", output.path, removeErr))
			}
		}
		if output.existed {
			if restoreErr := rename(output.backup, output.path); restoreErr != nil {
				errs = append(errs, fmt.Errorf("restore report %s: %w", output.path, restoreErr))
			}
		} else if output.backup != "" {
			_ = os.Remove(output.backup)
		}
		if output.tmp != "" {
			_ = os.Remove(output.tmp)
		}
	}
	return errors.Join(errs...)
}

func removeStagedV3(staged []stagedV3Output) {
	for _, output := range staged {
		if output.tmp != "" {
			_ = os.Remove(output.tmp)
		}
	}
}

func validateExecutionSummary(execution *result.ExecutionSummary) error {
	if execution == nil {
		return nil
	}
	if execution.SchemaVersion != "aiqa.execution-summary.v1" {
		return fmt.Errorf("unsupported execution summary schema %q", execution.SchemaVersion)
	}
	validPhases := map[string]bool{
		"validating": true, "preparing": true, "plan_frozen": true, "running": true,
		"finalizing": true, "completed": true, "interrupted": true, "failed_runtime": true,
	}
	if !validPhases[execution.Phase] {
		return fmt.Errorf("unsupported execution summary phase %q", execution.Phase)
	}
	if execution.ElapsedMS < 0 || execution.SumRequestWaitMS < 0 || execution.WallTimeWithWaitersMS < 0 {
		return errors.New("execution summary durations must be non-negative")
	}
	if execution.GlobalRequestCapacity < 1 || execution.ObservedPeakInflight < 0 || execution.ObservedPeakInflight > execution.GlobalRequestCapacity {
		return errors.New("execution summary request capacity is inconsistent")
	}
	if execution.PlannedJobs < 0 || execution.FinishedJobs < 0 || execution.CancelledJobs < 0 || execution.FinishedJobs+execution.CancelledJobs > execution.PlannedJobs {
		return errors.New("execution summary job counts are inconsistent")
	}
	if execution.Phase == "completed" && (execution.FinishedJobs != execution.PlannedJobs || execution.CancelledJobs != 0) {
		return errors.New("completed execution summary must account for every planned job")
	}
	if execution.PacingGroupCount < 0 || execution.SchedulerPolicy == "" || execution.EstimateMethod == "" {
		return errors.New("execution summary scheduler metadata is incomplete")
	}
	return nil
}

// WriteVerboseLog writes the optional private diagnostic sidecar.  It is kept
// out of the public report contract because it records run progress, rather
// than compatibility evidence.
func WriteVerboseLog(cfg config.Config, lines []string) ([]string, error) {
	if cfg.Verbosity == 0 {
		return nil, nil
	}
	data := make([]byte, 0)
	for _, line := range lines {
		data = append(data, sanitizeVerboseLogLine(line, reportSecrets(cfg))...)
		data = append(data, '\n')
	}
	path := cfg.Name + ".log"
	if err := atomicWrite(path, data, 0o600); err != nil {
		return nil, err
	}
	return []string{path}, nil
}

func normalizeV3Collections(r result.ReportV3) result.ReportV3 {
	for index := range r.Targets {
		if r.Targets[index].Models == nil {
			r.Targets[index].Models = []result.ModelReport{}
		}
		normalizeTargetClassifications(&r.Targets[index])
	}
	return r
}

// normalizeTargetClassifications materializes evidence classifications at the
// report boundary. Producers keep their canonical statuses; this metadata
// makes public JSON and Markdown explicit about what a non-pass proves.
func normalizeTargetClassifications(target *result.TargetReport) {
	classifyTests := func(tests []result.TestResult) {
		for i := range tests {
			if tests[i].Classification == nil {
				tests[i].Classification = result.ClassifyTestResult(tests[i], target.Profile.Revision)
			}
		}
	}
	classifyTests(target.Discovery.Tests)
	classifyTests(target.Discovery.Compatibility)
	for mi := range target.Models {
		classifyTests(target.Models[mi].Compatibility)
		for pi := range target.Models[mi].Parameters {
			p := &target.Models[mi].Parameters[pi]
			if p.Classification == nil {
				p.Classification = result.ClassifyResult(p.Status, "parameter."+p.Name, "", target.Profile.Revision, p.HTTP)
			}
		}
		if context := target.Models[mi].Context; context != nil && context.Classification == nil {
			context.Classification = result.ClassifyResult(context.Status, "context."+target.Models[mi].ID, "", target.Profile.Revision, result.HTTPObservation{})
		}
	}
	for ri := range target.Extensions {
		for si := range target.Extensions[ri].Suites {
			s := &target.Extensions[ri].Suites[si]
			if s.Classification == nil {
				s.Classification = result.ClassifyResult(s.Status, "extension."+s.ExtensionID+"."+s.SuiteID, s.RequirementID, target.Profile.Revision, s.HTTP)
				if s.ErrorKind == "deadline_exceeded" || s.ErrorKind == "cancelled" || s.ErrorKind == "transport" || s.ErrorKind == "process" {
					s.Classification.Kind = "execution_gap"
					s.Classification.Reason = "extension execution ended without conclusive provider evidence"
				}
			}
		}
	}
}

func MarkdownV3(r result.ReportV3) string {
	r = normalizeV3Collections(r)
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
	if r.ForensicTrace != nil {
		fmt.Fprintf(&b, "- **Forensic trace verdict:** `%s` (%s)\n\n", escapeInline(r.ForensicTrace.VerdictPath), statusLabelString(r.ForensicTrace.Status))
	}
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
	writeExecutionSummary(&b, r.Execution)

	fmt.Fprintln(&b, "## Rate-limit and latency")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Target | Logical | Attempts | Successful | 429 | Retry attempts | Recovered | Exhausted | Backoff | Safe req/s | Concurrency | Mean | P50 | P95 |")
	fmt.Fprintln(&b, "|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
	for _, target := range r.Targets {
		s := target.RateLimit
		fmt.Fprintf(&b, "| `%s` | %d | %d | %d | %d | %d | %d | %d | %s | %s | %s | %s | %s | %s |\n", escapeCell(target.ID), s.LogicalRequests, s.PhysicalAttempts, s.SuccessfulRequests, s.RateLimited, s.RetryAttempts, s.RetriesRecovered, s.RetriesExhausted, backoffPart(s.BackoffMS), safeRateText(s), safeConcurrencyText(s), latencyPart(s, "mean"), latencyPart(s, "p50"), latencyPart(s, "p95"))
	}
	fmt.Fprintln(&b)

	for _, target := range r.Targets {
		fmt.Fprintf(&b, "## Target: %s\n\n", protocolName(target.Protocol))
		fmt.Fprintf(&b, "- **Target ID:** `%s`\n", escapeInline(target.ID))
		fmt.Fprintf(&b, "- **Profile:** `%s@%s`\n", escapeInline(target.Profile.ID), escapeInline(target.Profile.Revision))
		if assessment := target.Summary.Assessment; assessment != nil {
			coverage := assessment.Coverage
			fmt.Fprintf(&b, "- **Requested coverage:** %d routes; %d executed, %d unresolved (%d core, %d optional), %d excluded.\n", coverage.Expected, coverage.Executed, coverage.Unresolved, coverage.CoreUnresolved, coverage.OptionalUnresolved, coverage.Excluded)
		}
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
			fmt.Fprintf(&b, "- **Note:** %s\n", escapeNote(note))
		}
		fmt.Fprintf(&b, "- **Discovered models:** %d\n", target.Discovery.ModelCount)
		fmt.Fprintf(&b, "- **Selected models:** %s\n\n", selectedModelsText(target.Models))
		writeTargetGuidance(&b, target)
		writeRequirementAssessment(&b, target.Requirements)
		writeTargetModels(&b, target)
		writeTargetClassifications(&b, target)
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

func writeExecutionSummary(b *strings.Builder, execution *result.ExecutionSummary) {
	if execution == nil {
		return
	}
	fmt.Fprintln(b, "## Execution")
	fmt.Fprintln(b)
	fmt.Fprintf(b, "- **Phase:** `%s`\n", escapeInline(execution.Phase))
	fmt.Fprintf(b, "- **Jobs:** %d planned; %d finished; %d cancelled\n", execution.PlannedJobs, execution.FinishedJobs, execution.CancelledJobs)
	fmt.Fprintf(b, "- **HTTP in flight:** %d/%d observed peak\n", execution.ObservedPeakInflight, execution.GlobalRequestCapacity)
	fmt.Fprintf(b, "- **Pacing groups:** %d\n", execution.PacingGroupCount)
	fmt.Fprintf(b, "- **Elapsed:** %dms\n\n", execution.ElapsedMS)
}

func writeTargetClassifications(b *strings.Builder, target result.TargetReport) {
	type row struct {
		id             string
		status         result.Status
		classification *result.Classification
	}
	var rows []row
	add := func(tests []result.TestResult) {
		for _, t := range tests {
			if t.Classification != nil {
				rows = append(rows, row{t.ID, t.Status, t.Classification})
			}
		}
	}
	add(target.Discovery.Tests)
	add(target.Discovery.Compatibility)
	for _, model := range target.Models {
		add(model.Compatibility)
		for _, p := range model.Parameters {
			if p.Classification != nil {
				rows = append(rows, row{"parameter." + p.Name, p.Status, p.Classification})
			}
		}
		if model.Context != nil && model.Context.Classification != nil {
			rows = append(rows, row{"context." + model.ID, model.Context.Status, model.Context.Classification})
		}
	}
	if len(rows) == 0 {
		return
	}
	fmt.Fprintln(b, "### Evidence classification")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "Classification describes the evidence for a non-pass result; it is not certification or a change to the canonical verdict.")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "| Check | Status | Classification | Evidence |")
	fmt.Fprintln(b, "|---|---:|---|---|")
	for _, r := range rows {
		fmt.Fprintf(b, "| `%s` | %s | `%s` | %s |\n", escapeCell(r.id), statusLabel(r.status), escapeCell(r.classification.Kind), escapeCell(r.classification.Reason))
	}
	fmt.Fprintln(b)
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
	if value == 0 {
		return "<1ms"
	}
	return fmt.Sprintf("%dms", value)
}

func backoffPart(ms int64) string {
	if ms == 0 {
		return "–"
	}
	return fmt.Sprintf("%dms", ms)
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
		fmt.Fprintln(b, "| Module | Capability | Requested | Applicability | Execution | Reason |")
		fmt.Fprintln(b, "|---|---|---:|---:|---:|---|")
		for _, module := range model.Modules {
			applicability := strings.ToUpper(strings.ReplaceAll(string(module.Applicability), "_", "-"))
			execution := strings.ToUpper(strings.ReplaceAll(string(module.Execution), "_", "-"))
			if execution == "" {
				execution = "–"
			}
			fmt.Fprintf(b, "| `%s` | `%s` | %t | %s | %s | %s |\n", escapeCell(module.ID), escapeCell(module.Capability), module.Requested, applicability, execution, escapeCell(module.Reason))
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
		fmt.Fprintf(b, "| %d | %d | %d | %d | %d | %d | %d | %s | %s | %s |\n\n", rateLimit.LogicalRequests, rateLimit.PhysicalAttempts, rateLimit.SuccessfulRequests, rateLimit.RateLimited, rateLimit.RetryAttempts, rateLimit.RetriesRecovered, rateLimit.RetriesExhausted, backoffPart(rateLimit.BackoffMS), safeRateText(*rateLimit), safeConcurrencyText(*rateLimit))
	}
	for _, id := range ids {
		p := provenance[id]
		fmt.Fprintf(b, "#### %s %s\n\n", escapeCell(extensionDisplayName(p)), escapeCell(p.Version))
		fmt.Fprintln(b, "| Suite | Subject | Status | Duration | Summary |")
		fmt.Fprintln(b, "|---|---|---:|---:|---|")
		for _, run := range groups[id] {
			for _, suite := range run.Suites {
				fmt.Fprintf(b, "| `%s` | `%s:%s` | %s | %s | %s |\n", escapeCell(suite.SuiteID), escapeCell(suite.Subject.Kind), escapeCell(suite.Subject.ID), statusLabel(suite.Status), formatExtensionDuration(suite.DurationNS, suite.DurationMS), extensionSummary(suite))
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

func formatExtensionDuration(durationNS, durationMS int64) string {
	if durationNS > 0 {
		return formatMeasuredDuration(durationNS)
	}
	if durationMS > 0 {
		return fmt.Sprintf("%dms", durationMS)
	}
	// Legacy executable extensions report only integer milliseconds. A zero
	// therefore means an observed duration below that resolution, not no time.
	return "<1ms"
}

// formatMeasuredDuration keeps a report column stable: use the largest unit
// at or below the measured Go-clock duration and retain at most three
// significant digits. Go measures only to nanoseconds, so ps is intentionally
// never fabricated.
func formatMeasuredDuration(ns int64) string {
	for _, unit := range []struct {
		ns     int64
		suffix string
	}{{int64(time.Millisecond), "ms"}, {int64(time.Microsecond), "µs"}, {1, "ns"}} {
		if ns >= unit.ns {
			value := float64(ns) / float64(unit.ns)
			digits := len(strconv.FormatInt(int64(value), 10))
			precision := 3 - digits
			if precision < 0 {
				precision = 0
			}
			formatted := strconv.FormatFloat(value, 'f', precision, 64)
			if precision > 0 {
				formatted = strings.TrimRight(strings.TrimRight(formatted, "0"), ".")
			}
			return formatted + unit.suffix
		}
	}
	return "<1ns"
}

// extensionSummary keeps the compact table useful for passing suites while
// preserving the evidence classification and its reason alongside each
// non-pass explanation. The structured classification remains in JSON.
func extensionSummary(suite result.ExtensionSuiteResult) string {
	summary := escapeCell(suite.Summary)
	if suite.Classification == nil {
		return summary
	}
	return fmt.Sprintf("%s (`%s`: %s)", summary, escapeCell(suite.Classification.Kind), escapeCell(suite.Classification.Reason))
}
