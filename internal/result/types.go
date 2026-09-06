package result

import "time"

type Status string

const (
	StatusPass        Status = "pass"
	StatusFail        Status = "fail"
	StatusWarn        Status = "warn"
	StatusUnsupported Status = "unsupported"
	StatusSkip        Status = "skip"
	StatusError       Status = "error"
)

type Usage struct {
	InputTokens              int64            `json:"input_tokens,omitempty"`
	OutputTokens             int64            `json:"output_tokens,omitempty"`
	CacheCreationInputTokens int64            `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int64            `json:"cache_read_input_tokens,omitempty"`
	ThinkingTokens           int64            `json:"thinking_tokens,omitempty"`
	PromptTokens             int64            `json:"prompt_tokens,omitempty"`
	CompletionTokens         int64            `json:"completion_tokens,omitempty"`
	TotalTokens              int64            `json:"total_tokens,omitempty"`
	ReasoningTokens          int64            `json:"reasoning_tokens,omitempty"`
	ProviderFields           map[string]int64 `json:"provider_fields,omitempty"`
}

func (u Usage) Add(v Usage) Usage {
	out := Usage{
		InputTokens:              u.InputTokens + v.InputTokens,
		OutputTokens:             u.OutputTokens + v.OutputTokens,
		CacheCreationInputTokens: u.CacheCreationInputTokens + v.CacheCreationInputTokens,
		CacheReadInputTokens:     u.CacheReadInputTokens + v.CacheReadInputTokens,
		ThinkingTokens:           u.ThinkingTokens + v.ThinkingTokens,
		PromptTokens:             u.PromptTokens + v.PromptTokens,
		CompletionTokens:         u.CompletionTokens + v.CompletionTokens,
		TotalTokens:              u.TotalTokens + v.TotalTokens,
		ReasoningTokens:          u.ReasoningTokens + v.ReasoningTokens,
	}
	if len(u.ProviderFields)+len(v.ProviderFields) > 0 {
		out.ProviderFields = make(map[string]int64, len(u.ProviderFields)+len(v.ProviderFields))
		for key, value := range u.ProviderFields {
			out.ProviderFields[key] += value
		}
		for key, value := range v.ProviderFields {
			out.ProviderFields[key] += value
		}
	}
	return out
}

type HTTPObservation struct {
	StatusCode  int               `json:"status_code,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
	RequestID   string            `json:"request_id,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Snippet     string            `json:"snippet,omitempty"`
	// TraceID correlates a private verbose-log transport operation with this
	// terminal probe. It is deliberately excluded from report artifacts.
	TraceID string `json:"-"`
}

// Classification separates the observed result status from the strength and
// provenance of the evidence. It is public metadata only: the referenced
// operation IDs identify records in the private verbose sidecar, not requests.
type Classification struct {
	Kind            string   `json:"kind"`
	RequirementID   string   `json:"requirement_id,omitempty"`
	ProfileRevision string   `json:"profile_revision,omitempty"`
	OperationIDs    []string `json:"operation_ids,omitempty"`
	Reason          string   `json:"reason,omitempty"`
}

type TestResult struct {
	ID            string `json:"id"`
	RequirementID string `json:"requirement_id,omitempty"`
	ProbeID       string `json:"probe_id,omitempty"`
	// DerivedFrom identifies the prerequisite report result for a composite
	// check. It is an internal stable requirement ID, never provider data.
	DerivedFrom    string                `json:"derived_from,omitempty"`
	Name           string                `json:"name"`
	Suite          string                `json:"suite"`
	Category       string                `json:"category"`
	Required       bool                  `json:"required"`
	Applicability  Applicability         `json:"applicability,omitempty"`
	Status         Status                `json:"status"`
	DurationMS     int64                 `json:"duration_ms,omitempty"`
	Summary        string                `json:"summary"`
	Expected       string                `json:"expected,omitempty"`
	Observation    string                `json:"observation,omitempty"`
	HTTP           HTTPObservation       `json:"http,omitempty"`
	Usage          Usage                 `json:"usage,omitempty"`
	Metrics        map[string]any        `json:"metrics,omitempty"`
	Signals        []EvidenceSignal      `json:"signals,omitempty"`
	Error          string                `json:"error,omitempty"`
	Artifacts      []ArtifactObservation `json:"artifacts,omitempty"`
	Classification *Classification       `json:"classification,omitempty"`
}

type ParameterResult struct {
	Name           string           `json:"name"`
	Variant        string           `json:"variant,omitempty"`
	Status         Status           `json:"status"`
	Accepted       *bool            `json:"accepted,omitempty"`
	Effective      *bool            `json:"effective,omitempty"`
	Confidence     string           `json:"confidence,omitempty"`
	DurationMS     int64            `json:"duration_ms,omitempty"`
	Summary        string           `json:"summary"`
	Observation    string           `json:"observation,omitempty"`
	HTTP           HTTPObservation  `json:"http,omitempty"`
	Usage          Usage            `json:"usage,omitempty"`
	Metrics        map[string]any   `json:"metrics,omitempty"`
	Signals        []EvidenceSignal `json:"signals,omitempty"`
	Error          string           `json:"error,omitempty"`
	Classification *Classification  `json:"classification,omitempty"`
}

type ContextProbeResult struct {
	Status                       Status          `json:"status"`
	Summary                      string          `json:"summary"`
	DeclaredTokens               int64           `json:"declared_tokens,omitempty"`
	ServerLimitTokens            int64           `json:"server_limit_tokens,omitempty"`
	MaxAcceptedObservedTokens    int64           `json:"max_accepted_observed_tokens,omitempty"`
	FirstRejectedEstimatedTokens int64           `json:"first_rejected_estimated_tokens,omitempty"`
	LowerBoundTokens             int64           `json:"lower_bound_tokens,omitempty"`
	UpperBoundTokens             int64           `json:"upper_bound_tokens,omitempty"`
	PrecisionTokens              int64           `json:"precision_tokens,omitempty"`
	Confidence                   string          `json:"confidence,omitempty"`
	ProbeCount                   int             `json:"probe_count"`
	BudgetTokens                 int64           `json:"budget_tokens,omitempty"`
	ConsumedTokens               int64           `json:"consumed_tokens,omitempty"`
	OutputLimitParameter         string          `json:"output_limit_parameter,omitempty"`
	LastError                    string          `json:"last_error,omitempty"`
	Samples                      []ContextSample `json:"samples,omitempty"`
	Classification               *Classification `json:"classification,omitempty"`
}

type ContextSample struct {
	PayloadBytes          int    `json:"payload_bytes"`
	EstimatedPromptTokens int64  `json:"estimated_prompt_tokens,omitempty"`
	ReportedPromptTokens  int64  `json:"reported_prompt_tokens,omitempty"`
	StatusCode            int    `json:"status_code,omitempty"`
	Accepted              bool   `json:"accepted"`
	Overflow              bool   `json:"overflow"`
	DurationMS            int64  `json:"duration_ms,omitempty"`
	Error                 string `json:"error,omitempty"`
}

type Applicability string

const (
	ApplicabilityApplicable    Applicability = "applicable"
	ApplicabilityNotApplicable Applicability = "not_applicable"
	ApplicabilityUnknown       Applicability = "unknown"
)

type ExecutionState string

const (
	ExecutionExecuted ExecutionState = "executed"
	ExecutionSkipped  ExecutionState = "skipped"
)

type CapabilityAssessment struct {
	ID         string `json:"id"`
	Source     string `json:"source"`
	Rule       string `json:"rule,omitempty"`
	Confidence string `json:"confidence"`
	Reason     string `json:"reason"`
}

type ModuleAssessment struct {
	ID            string         `json:"id"`
	Capability    string         `json:"capability"`
	Requested     bool           `json:"requested"`
	Applicability Applicability  `json:"applicability"`
	Execution     ExecutionState `json:"execution,omitempty"`
	Reason        string         `json:"reason"`
	Source        string         `json:"source,omitempty"`
	Confidence    string         `json:"confidence,omitempty"`
	BlockedBy     []string       `json:"blocked_by,omitempty"`
	TestIDs       []string       `json:"test_ids,omitempty"`
	Summary       *Summary       `json:"summary,omitempty"`
}

type ModelAssessment struct {
	Verdict string `json:"verdict"`
}

type ArtifactObservation struct {
	Kind      string `json:"kind"`
	Source    string `json:"source"`
	MediaType string `json:"media_type"`
	Format    string `json:"format"`
	Bytes     int64  `json:"bytes"`
	SHA256    string `json:"sha256"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

type Summary struct {
	Pass               int                    `json:"pass"`
	Fail               int                    `json:"fail"`
	Warn               int                    `json:"warn"`
	Unsupported        int                    `json:"unsupported"`
	Skip               int                    `json:"skip"`
	Error              int                    `json:"error"`
	RequiredPass       int                    `json:"required_pass"`
	RequiredFail       int                    `json:"required_fail"`
	Status             string                 `json:"status"`
	Assessment         *ConformanceAssessment `json:"assessment,omitempty"`
	Usage              Usage                  `json:"usage,omitempty"`
	ContextProbeTokens int64                  `json:"context_probe_tokens,omitempty"`
}

// ExtensionOutcome is the raw observation reported by an extension suite.
// It deliberately does not include conformance terms such as "unsupported";
// AIQA derives the public Status from this observation and the active profile.
type ExtensionOutcome string

const (
	ExtensionOutcomePass            ExtensionOutcome = "pass"
	ExtensionOutcomeAssertionFailed ExtensionOutcome = "assertion_failed"
	ExtensionOutcomeRequestRejected ExtensionOutcome = "request_rejected"
	ExtensionOutcomeExecutionError  ExtensionOutcome = "execution_error"
	ExtensionOutcomeSkip            ExtensionOutcome = "skip"
)

type ExtensionProvenance struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Version         string `json:"version"`
	Source          string `json:"source"`
	ProtocolVersion string `json:"protocol_version"`
	Path            string `json:"path,omitempty"`
	SHA256          string `json:"sha256,omitempty"`
}

type ExtensionRunnerResult struct {
	Status            Status `json:"status"`
	StartupDurationMS int64  `json:"startup_duration_ms"`
	TotalDurationMS   int64  `json:"total_duration_ms"`
	ExitCode          int    `json:"exit_code"`
	PacingCompliance  string `json:"pacing_compliance"`
	Summary           string `json:"summary"`
	ArtifactRef       string `json:"artifact_ref,omitempty"`
	Error             string `json:"error,omitempty"`
	Output            string `json:"-"`
}

type ExtensionSuiteResult struct {
	ExtensionID           string           `json:"extension_id"`
	SuiteID               string           `json:"suite_id"`
	Subject               SubjectRef       `json:"subject"`
	Outcome               ExtensionOutcome `json:"outcome"`
	Status                Status           `json:"status"`
	DurationMS            int64            `json:"duration_ms"`
	Summary               string           `json:"summary"`
	HTTP                  HTTPObservation  `json:"http,omitempty"`
	Usage                 Usage            `json:"usage,omitempty"`
	Signals               []EvidenceSignal `json:"signals,omitempty"`
	RequirementID         string           `json:"requirement_id,omitempty"`
	RelatedRequirementIDs []string         `json:"related_requirement_ids,omitempty"`
	Bound                 bool             `json:"bound"`
	ArtifactRef           string           `json:"artifact_ref,omitempty"`
	Error                 string           `json:"error,omitempty"`
	ErrorKind             string           `json:"error_kind,omitempty"`
	ErrorCode             string           `json:"error_code,omitempty"`
	Classification        *Classification  `json:"classification,omitempty"`
}

type ExtensionRun struct {
	Provenance ExtensionProvenance    `json:"provenance"`
	TargetID   string                 `json:"target_id"`
	Subject    SubjectRef             `json:"subject"`
	Runner     ExtensionRunnerResult  `json:"runner"`
	Suites     []ExtensionSuiteResult `json:"suites"`
}

type ModelReport struct {
	ID            string                 `json:"id"`
	Metadata      map[string]any         `json:"metadata,omitempty"`
	Compatibility []TestResult           `json:"compatibility,omitempty"`
	Parameters    []ParameterResult      `json:"parameters,omitempty"`
	Context       *ContextProbeResult    `json:"context,omitempty"`
	RateLimit     *RateLimitStats        `json:"rate_limit,omitempty"`
	Capabilities  []CapabilityAssessment `json:"capabilities,omitempty"`
	Modules       []ModuleAssessment     `json:"modules,omitempty"`
	Assessment    ModelAssessment        `json:"assessment"`
	Summary       Summary                `json:"summary"`
}

type ProfileInfo struct {
	SchemaVersion     string `json:"schema_version,omitempty"`
	ID                string `json:"id"`
	Protocol          string `json:"protocol,omitempty"`
	Revision          string `json:"revision"`
	APIVersion        string `json:"api_version,omitempty"`
	BaseURLSemantics  string `json:"base_url_semantics,omitempty"`
	Source            string `json:"source"`
	SourceCommit      string `json:"source_commit"`
	SourceRefRevision string `json:"source_ref_revision,omitempty"`
	Scope             string `json:"scope"`
}

type TargetPublicConfig struct {
	ID                string   `json:"id"`
	Protocol          string   `json:"protocol"`
	BaseURL           string   `json:"base_url"`
	BaseURLNormalized bool     `json:"base_url_normalized,omitempty"`
	RequestedModels   []string `json:"requested_models,omitempty"`
	APIVersion        string   `json:"api_version,omitempty"`
}

type PublicConfigV3 struct {
	Tests                    []string             `json:"tests"`
	Targets                  []TargetPublicConfig `json:"targets"`
	RequestedModels          []string             `json:"requested_models,omitempty"`
	Timeout                  string               `json:"timeout"`
	Formats                  []string             `json:"formats"`
	FailOn                   string               `json:"fail_on"`
	Guidance                 string               `json:"guidance,omitempty"`
	ExtSuites                []string             `json:"ext_suites,omitempty"`
	AllowHighTokenProbes     bool                 `json:"allow_high_token_probes,omitempty"`
	AllowPersistentResources bool                 `json:"allow_persistent_resources,omitempty"`
}

type TargetReport struct {
	ID                 string                  `json:"id"`
	Protocol           string                  `json:"protocol"`
	Profile            ProfileInfo             `json:"profile"`
	Discovery          Discovery               `json:"discovery"`
	Models             []ModelReport           `json:"models"`
	Requirements       []RequirementAssessment `json:"requirements,omitempty"`
	Summary            Summary                 `json:"summary"`
	Guidance           GuidanceReport          `json:"guidance"`
	RateLimit          RateLimitStats          `json:"rate_limit"`
	ExtensionRateLimit *RateLimitStats         `json:"extension_rate_limit,omitempty"`
	Extensions         []ExtensionRun          `json:"extensions,omitempty"`
	Notes              []string                `json:"notes,omitempty"`
}

type RunSummary struct {
	Verdict         string         `json:"verdict"`
	VerdictReason   string         `json:"verdict_reason,omitempty"`
	TargetCounts    map[string]int `json:"target_counts"`
	BlockingTargets []string       `json:"blocking_targets,omitempty"`
}

// ReportV3 is the public multi-target report contract. Extension provenance,
// runner state, and suite evidence are grouped per target.
type ReportV3 struct {
	SchemaVersion string         `json:"schema_version"`
	GeneratedAt   time.Time      `json:"generated_at"`
	Tool          ToolInfo       `json:"tool"`
	Config        PublicConfigV3 `json:"config"`
	Targets       []TargetReport `json:"targets"`
	Summary       RunSummary     `json:"summary"`
	ForensicTrace *ForensicTrace `json:"forensic_trace,omitempty"`
}

// ForensicTrace is the public, non-sensitive link to a private trace verdict.
// It intentionally contains neither trace records nor provider diagnostics.
type ForensicTrace struct {
	VerdictPath string `json:"verdict_path"`
	Status      string `json:"status"`
}

type ToolInfo struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	BuildDate string `json:"build_date,omitempty"`
	GoVersion string `json:"go_version"`
}

type PublicConfig struct {
	BaseURL                string   `json:"base_url"`
	Tests                  []string `json:"tests"`
	RequestedModels        []string `json:"requested_models,omitempty"`
	Timeout                string   `json:"timeout"`
	Formats                []string `json:"formats"`
	ExtSuites              []string `json:"ext_suites,omitempty"`
	FailOn                 string   `json:"fail_on"`
	ContextMaxTokens       int64    `json:"context_max_tokens,omitempty"`
	ContextPrecision       int64    `json:"context_precision_tokens,omitempty"`
	ContextBudget          int64    `json:"context_budget_tokens,omitempty"`
	Samples                int      `json:"samples,omitempty"`
	Modules                []string `json:"modules,omitempty"`
	UnknownModel           string   `json:"unknown_model,omitempty"`
	AllowBillableArtifacts bool     `json:"allow_billable_artifacts,omitempty"`
	MaxArtifactsPerModel   int      `json:"max_artifacts_per_model,omitempty"`
	Guidance               string   `json:"guidance,omitempty"`
	GuidancePack           string   `json:"guidance_pack,omitempty"`
	GuidanceLocale         string   `json:"guidance_locale,omitempty"`
}

type Discovery struct {
	Models        []map[string]any `json:"models,omitempty"`
	Tests         []TestResult     `json:"tests,omitempty"`
	Compatibility []TestResult     `json:"compatibility,omitempty"`
	ModelCount    int              `json:"model_count"`
}

type Report struct {
	SchemaVersion      string                  `json:"schema_version"`
	GeneratedAt        time.Time               `json:"generated_at"`
	Tool               ToolInfo                `json:"tool"`
	Profile            ProfileInfo             `json:"profile"`
	Config             PublicConfig            `json:"config"`
	Discovery          Discovery               `json:"discovery"`
	Models             []ModelReport           `json:"models"`
	Summary            Summary                 `json:"summary"`
	RateLimit          RateLimitStats          `json:"rate_limit"`
	ExtensionRateLimit *RateLimitStats         `json:"-"`
	Requirements       []RequirementAssessment `json:"requirements,omitempty"`
	Guidance           GuidanceReport          `json:"guidance"`
	Extensions         []ExtensionRun          `json:"extensions,omitempty"`
}

type RateLimitStats struct {
	LogicalRequests    int     `json:"logical_requests"`
	PhysicalAttempts   int     `json:"physical_attempts"`
	SuccessfulRequests int     `json:"successful_requests"`
	RateLimited        int     `json:"rate_limited_responses"`
	RetryAttempts      int     `json:"retry_attempts"`
	RetriesRecovered   int     `json:"retries_recovered"`
	RetriesExhausted   int     `json:"retries_exhausted"`
	BackoffMS          int64   `json:"backoff_ms"`
	SafeRate           float64 `json:"observed_safe_requests_per_second,omitempty"`
	SafeConcurrency    int     `json:"observed_safe_concurrency,omitempty"`
	LatencySamples     int     `json:"latency_samples"`
	MeanLatencyMS      int64   `json:"mean_latency_ms"`
	P50LatencyMS       int64   `json:"p50_latency_ms"`
	P95LatencyMS       int64   `json:"p95_latency_ms"`
}

func Bool(v bool) *bool { return &v }
