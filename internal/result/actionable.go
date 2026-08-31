package result

type RequirementLevel string

const (
	RequirementCore     RequirementLevel = "core"
	RequirementOptional RequirementLevel = "optional"
)

type RequirementStatus string

const (
	RequirementPass          RequirementStatus = "pass"
	RequirementFail          RequirementStatus = "fail"
	RequirementWarn          RequirementStatus = "warn"
	RequirementUnsupported   RequirementStatus = "unsupported"
	RequirementSkip          RequirementStatus = "skip"
	RequirementError         RequirementStatus = "error"
	RequirementNotApplicable RequirementStatus = "not_applicable"
)

type SubjectRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type EvidenceSignal struct {
	Code       string         `json:"code"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

type EvidenceRef struct {
	ID                    string           `json:"id"`
	Kind                  string           `json:"kind"`
	ProbeID               string           `json:"probe_id,omitempty"`
	Suite                 string           `json:"suite,omitempty"`
	Status                Status           `json:"status"`
	Summary               string           `json:"summary,omitempty"`
	HTTPStatus            int              `json:"http_status,omitempty"`
	ArtifactRef           string           `json:"artifact_ref,omitempty"`
	RelatedRequirementIDs []string         `json:"related_requirement_ids,omitempty"`
	Signals               []EvidenceSignal `json:"signals,omitempty"`
}

type RequirementAssessment struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Description   string            `json:"description,omitempty"`
	Level         RequirementLevel  `json:"level"`
	Subject       SubjectRef        `json:"subject"`
	Module        string            `json:"module,omitempty"`
	Applicability Applicability     `json:"applicability"`
	Status        RequirementStatus `json:"status"`
	FailureKind   string            `json:"failure_kind,omitempty"`
	Evidence      []EvidenceRef     `json:"evidence,omitempty"`
	BlockedBy     []string          `json:"blocked_by,omitempty"`
}

type RequirementMeasure struct {
	Total       int      `json:"total"`
	Pass        int      `json:"pass"`
	Fail        int      `json:"fail"`
	Unsupported int      `json:"unsupported,omitempty"`
	Warn        int      `json:"warn"`
	Skip        int      `json:"skip"`
	Error       int      `json:"error"`
	Percent     *float64 `json:"percent"`
}

type EvidenceMeasure struct {
	Total             int      `json:"total"`
	Executed          int      `json:"executed"`
	Conclusive        int      `json:"conclusive"`
	ExecutionPercent  *float64 `json:"execution_percent"`
	ConclusivePercent *float64 `json:"conclusive_percent"`
}

type ComparisonPolicy struct {
	Policy      string `json:"policy"`
	Rankable    bool   `json:"rankable"`
	VerdictRank *int   `json:"verdict_rank"`
}

type ConformanceAssessment struct {
	Verdict       string             `json:"verdict"`
	VerdictReason string             `json:"verdict_reason"`
	Core          RequirementMeasure `json:"core"`
	Optional      RequirementMeasure `json:"optional"`
	Evidence      EvidenceMeasure    `json:"evidence"`
	Comparison    ComparisonPolicy   `json:"comparison"`
}

type Attribution struct {
	Component  string `json:"component"`
	Confidence string `json:"confidence"`
	Basis      string `json:"basis"`
}

type ObservedScope struct {
	Endpoint string   `json:"endpoint,omitempty"`
	Models   []string `json:"models,omitempty"`
	Affected int      `json:"affected,omitempty"`
	Total    int      `json:"total,omitempty"`
}

type Finding struct {
	ID                    string         `json:"id"`
	Fingerprint           string         `json:"fingerprint"`
	Code                  string         `json:"code"`
	Class                 string         `json:"class"`
	EvidenceState         string         `json:"evidence_state"`
	Confidence            string         `json:"confidence"`
	Title                 string         `json:"title"`
	Subject               SubjectRef     `json:"subject"`
	ObservedScope         ObservedScope  `json:"observed_scope"`
	RequirementIDs        []string       `json:"requirement_ids"`
	EvidenceRefs          []string       `json:"evidence_refs"`
	Expected              string         `json:"expected,omitempty"`
	Observed              string         `json:"observed"`
	Attribution           Attribution    `json:"attribution"`
	Details               map[string]any `json:"details,omitempty"`
	ArtifactRefs          []string       `json:"artifact_refs,omitempty"`
	BlockedRequirementIDs []string       `json:"blocked_requirement_ids,omitempty"`
	BaselineState         string         `json:"baseline_state"`
}

type RecommendationStep struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type Verification struct {
	Kind string   `json:"kind"`
	IDs  []string `json:"ids"`
}

type Reference struct {
	Title      string `json:"title"`
	URL        string `json:"url"`
	SourceKind string `json:"source_kind"`
}

type RecommendationTarget struct {
	Component string `json:"component"`
	Endpoint  string `json:"endpoint,omitempty"`
}

type PotentialImpact struct {
	BlocksCore                    bool     `json:"blocks_core"`
	CoreRequirementsRecovered     int      `json:"core_requirements_recovered"`
	OptionalRequirementsRecovered int      `json:"optional_requirements_recovered"`
	EvidenceRequirementsRecovered int      `json:"evidence_requirements_recovered"`
	CurrentVerdict                string   `json:"current_verdict"`
	ProjectedVerdict              *string  `json:"projected_verdict"`
	Assumptions                   []string `json:"assumptions,omitempty"`
}

type RecommendationSource struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

type Recommendation struct {
	ID              string               `json:"id"`
	Fingerprint     string               `json:"fingerprint"`
	FindingIDs      []string             `json:"finding_ids"`
	Title           string               `json:"title"`
	Priority        string               `json:"priority"`
	ActionKind      string               `json:"action_kind"`
	GuidanceLevel   int                  `json:"guidance_level"`
	Confidence      string               `json:"confidence"`
	Audiences       []string             `json:"audiences"`
	Target          RecommendationTarget `json:"target"`
	Why             string               `json:"why"`
	Observed        string               `json:"observed"`
	Steps           []RecommendationStep `json:"steps"`
	Verification    []Verification       `json:"verification"`
	Workarounds     []string             `json:"workarounds,omitempty"`
	Caveats         []string             `json:"caveats,omitempty"`
	PotentialImpact PotentialImpact      `json:"potential_impact"`
	References      []Reference          `json:"references"`
	Source          RecommendationSource `json:"source"`
}

type GuidanceArtifact struct {
	Kind      string `json:"kind"`
	MediaType string `json:"media_type"`
	Bytes     int    `json:"bytes"`
	SHA256    string `json:"sha256"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

type GuidancePackInfo struct {
	ID              string `json:"id"`
	Revision        string `json:"revision"`
	Source          string `json:"source"`
	DetectionSource string `json:"detection_source,omitempty"`
	Confidence      string `json:"confidence,omitempty"`
}

type GuidanceEngineInfo struct {
	Version         string             `json:"version"`
	CatalogRevision string             `json:"catalog_revision"`
	Packs           []GuidancePackInfo `json:"packs"`
}

type PriorityCounts struct {
	P0 int `json:"p0"`
	P1 int `json:"p1"`
	P2 int `json:"p2"`
	P3 int `json:"p3"`
}

type GuidanceSummary struct {
	EligibleFindings          int            `json:"eligible_findings"`
	EligibleRequirements      int            `json:"eligible_requirements"`
	CoveredRequirements       int            `json:"covered_requirements"`
	Findings                  int            `json:"findings"`
	Recommendations           int            `json:"recommendations"`
	ByPriority                PriorityCounts `json:"by_priority"`
	GuidanceCoveragePercent   float64        `json:"guidance_coverage_percent"`
	ActionableCoveragePercent float64        `json:"actionable_coverage_percent"`
}

type GuidanceReport struct {
	SchemaVersion   string                      `json:"schema_version"`
	Status          string                      `json:"status"`
	Error           string                      `json:"error,omitempty"`
	Engine          GuidanceEngineInfo          `json:"engine"`
	Summary         GuidanceSummary             `json:"summary"`
	Findings        []Finding                   `json:"findings"`
	Recommendations []Recommendation            `json:"recommendations"`
	Artifacts       map[string]GuidanceArtifact `json:"artifacts,omitempty"`
}
