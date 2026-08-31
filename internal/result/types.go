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
	PromptTokens     int64 `json:"prompt_tokens,omitempty"`
	CompletionTokens int64 `json:"completion_tokens,omitempty"`
	TotalTokens      int64 `json:"total_tokens,omitempty"`
	ReasoningTokens  int64 `json:"reasoning_tokens,omitempty"`
}

func (u Usage) Add(v Usage) Usage {
	return Usage{
		PromptTokens:     u.PromptTokens + v.PromptTokens,
		CompletionTokens: u.CompletionTokens + v.CompletionTokens,
		TotalTokens:      u.TotalTokens + v.TotalTokens,
		ReasoningTokens:  u.ReasoningTokens + v.ReasoningTokens,
	}
}

type HTTPObservation struct {
	StatusCode  int               `json:"status_code,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
	RequestID   string            `json:"request_id,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Snippet     string            `json:"snippet,omitempty"`
}

type TestResult struct {
	ID            string                `json:"id"`
	RequirementID string                `json:"requirement_id,omitempty"`
	ProbeID       string                `json:"probe_id,omitempty"`
	Name          string                `json:"name"`
	Suite         string                `json:"suite"`
	Category      string                `json:"category"`
	Required      bool                  `json:"required"`
	Status        Status                `json:"status"`
	DurationMS    int64                 `json:"duration_ms,omitempty"`
	Summary       string                `json:"summary"`
	Expected      string                `json:"expected,omitempty"`
	Observation   string                `json:"observation,omitempty"`
	HTTP          HTTPObservation       `json:"http,omitempty"`
	Usage         Usage                 `json:"usage,omitempty"`
	Metrics       map[string]any        `json:"metrics,omitempty"`
	Signals       []EvidenceSignal      `json:"signals,omitempty"`
	Error         string                `json:"error,omitempty"`
	Artifacts     []ArtifactObservation `json:"artifacts,omitempty"`
}

type ParameterResult struct {
	Name        string           `json:"name"`
	Variant     string           `json:"variant,omitempty"`
	Status      Status           `json:"status"`
	Accepted    *bool            `json:"accepted,omitempty"`
	Effective   *bool            `json:"effective,omitempty"`
	Confidence  string           `json:"confidence,omitempty"`
	DurationMS  int64            `json:"duration_ms,omitempty"`
	Summary     string           `json:"summary"`
	Observation string           `json:"observation,omitempty"`
	HTTP        HTTPObservation  `json:"http,omitempty"`
	Usage       Usage            `json:"usage,omitempty"`
	Metrics     map[string]any   `json:"metrics,omitempty"`
	Signals     []EvidenceSignal `json:"signals,omitempty"`
	Error       string           `json:"error,omitempty"`
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
	Applicability Applicability  `json:"applicability"`
	Execution     ExecutionState `json:"execution,omitempty"`
	Reason        string         `json:"reason"`
	Source        string         `json:"source,omitempty"`
	Confidence    string         `json:"confidence,omitempty"`
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

type ExternalResult struct {
	Tool                  string           `json:"tool"`
	Suite                 string           `json:"suite,omitempty"`
	Model                 string           `json:"model,omitempty"`
	Status                Status           `json:"status"`
	ExitCode              int              `json:"exit_code,omitempty"`
	DurationMS            int64            `json:"duration_ms,omitempty"`
	Summary               string           `json:"summary"`
	Output                string           `json:"output,omitempty"`
	ArtifactRef           string           `json:"artifact_ref,omitempty"`
	RequirementIDs        []string         `json:"requirement_ids,omitempty"`
	RelatedRequirementIDs []string         `json:"related_requirement_ids,omitempty"`
	Signals               []EvidenceSignal `json:"signals,omitempty"`
	Error                 string           `json:"error,omitempty"`
}

type ModelReport struct {
	ID            string                 `json:"id"`
	Metadata      map[string]any         `json:"metadata,omitempty"`
	Compatibility []TestResult           `json:"compatibility,omitempty"`
	Parameters    []ParameterResult      `json:"parameters,omitempty"`
	Context       *ContextProbeResult    `json:"context,omitempty"`
	External      []ExternalResult       `json:"external,omitempty"`
	RateLimit     *RateLimitStats        `json:"rate_limit,omitempty"`
	Capabilities  []CapabilityAssessment `json:"capabilities,omitempty"`
	Modules       []ModuleAssessment     `json:"modules,omitempty"`
	Assessment    ModelAssessment        `json:"assessment"`
	Summary       Summary                `json:"summary"`
}

type ProfileInfo struct {
	ID                string `json:"id"`
	Revision          string `json:"revision"`
	Source            string `json:"source"`
	SourceCommit      string `json:"source_commit"`
	SourceRefRevision string `json:"source_ref_revision,omitempty"`
	Scope             string `json:"scope"`
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
	ExternalMode           string   `json:"external_mode"`
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
	External      []ExternalResult `json:"external,omitempty"`
}

type Report struct {
	SchemaVersion string                  `json:"schema_version"`
	GeneratedAt   time.Time               `json:"generated_at"`
	Tool          ToolInfo                `json:"tool"`
	Profile       ProfileInfo             `json:"profile"`
	Config        PublicConfig            `json:"config"`
	Discovery     Discovery               `json:"discovery"`
	Models        []ModelReport           `json:"models"`
	Summary       Summary                 `json:"summary"`
	RateLimit     RateLimitStats          `json:"rate_limit"`
	Requirements  []RequirementAssessment `json:"requirements,omitempty"`
	Guidance      GuidanceReport          `json:"guidance"`
}

type RateLimitStats struct {
	SuccessfulRequests int     `json:"successful_requests"`
	RateLimited        int     `json:"rate_limited_responses"`
	RetriesRecovered   int     `json:"retries_recovered"`
	BackoffMS          int64   `json:"backoff_ms"`
	SafeRate           float64 `json:"observed_safe_requests_per_second"`
	SafeConcurrency    int     `json:"observed_safe_concurrency"`
	LatencySamples     int     `json:"latency_samples"`
	MeanLatencyMS      int64   `json:"mean_latency_ms"`
	P50LatencyMS       int64   `json:"p50_latency_ms"`
	P95LatencyMS       int64   `json:"p95_latency_ms"`
}

func Bool(v bool) *bool { return &v }
