package extsuite

import (
	"context"
	"net/http"
	"time"

	"github.com/ilyar/aiqa/internal/probe"
	"github.com/ilyar/aiqa/internal/result"
)

const ProtocolVersion = "aiqa.ext.v1"

type ExtensionInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type SuiteDescriptor struct {
	ID                  string   `json:"id"`
	Subject             string   `json:"subject"`
	Capabilities        []string `json:"capabilities,omitempty"`
	Cost                string   `json:"cost"`
	InternalConcurrency int      `json:"internal_concurrency"`
}

type Manifest struct {
	SchemaVersion    string            `json:"schema_version"`
	Type             string            `json:"type"`
	Extension        ExtensionInfo     `json:"extension"`
	ReportContracts  []string          `json:"report_contracts"`
	ProfileContracts []string          `json:"profile_contracts"`
	Protocols        []string          `json:"protocols"`
	Suites           []SuiteDescriptor `json:"suites"`
}

type HostInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ManifestRequest struct {
	SchemaVersion string   `json:"schema_version"`
	Type          string   `json:"type"`
	Host          HostInfo `json:"host"`
}

type Target struct {
	ID         string `json:"id"`
	Protocol   string `json:"protocol"`
	BaseURL    string `json:"base_url"`
	APIVersion string `json:"api_version,omitempty"`
}

type ProfileRef struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

type Limits struct {
	RequestTimeoutMS              int64   `json:"request_timeout_ms"`
	DeadlineMS                    int64   `json:"deadline_ms"`
	MaxConcurrency                int     `json:"max_concurrency"`
	RequestIntervalMS             int64   `json:"request_interval_ms"`
	RetryLimit                    int     `json:"retry_limit"`
	ObservedSafeRequestsPerSecond float64 `json:"observed_safe_requests_per_second"`
	ObservedSafeConcurrency       int     `json:"observed_safe_concurrency"`
	MaxOutputBytes                int64   `json:"max_output_bytes"`
}

type Credential struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type RuntimeServices struct {
	HTTPClient *http.Client
	Executor   *probe.Executor
}

type RunRequest struct {
	SchemaVersion string            `json:"schema_version"`
	Type          string            `json:"type"`
	RunID         string            `json:"run_id"`
	Target        Target            `json:"target"`
	Subject       result.SubjectRef `json:"subject"`
	SuiteIDs      []string          `json:"suite_ids"`
	Profile       ProfileRef        `json:"profile"`
	Limits        Limits            `json:"limits"`
	Credential    Credential        `json:"credential"`
	Runtime       RuntimeServices   `json:"-"`
}

type SuiteEvent struct {
	SchemaVersion string                  `json:"schema_version"`
	Type          string                  `json:"type"`
	RunID         string                  `json:"run_id"`
	ExtensionID   string                  `json:"extension_id"`
	SuiteID       string                  `json:"suite_id"`
	Subject       result.SubjectRef       `json:"subject"`
	Outcome       result.ExtensionOutcome `json:"outcome"`
	DurationMS    int64                   `json:"duration_ms"`
	Summary       string                  `json:"summary"`
	ErrorKind     string                  `json:"error_kind,omitempty"`
	HTTP          result.HTTPObservation  `json:"http,omitempty"`
	Usage         result.Usage            `json:"usage,omitempty"`
	Signals       []result.EvidenceSignal `json:"signals,omitempty"`
	Error         string                  `json:"error,omitempty"`
}

type RunnerTelemetry struct {
	StartupDuration time.Duration
	TotalDuration   time.Duration
	ExitCode        int
	Stderr          string
}

type EventSink interface {
	Emit(SuiteEvent) error
}

type Extension interface {
	Manifest() Manifest
	Provenance() result.ExtensionProvenance
	Run(context.Context, RunRequest, EventSink) (RunnerTelemetry, error)
}
