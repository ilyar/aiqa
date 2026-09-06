package extsuite

import (
	"context"
	"net/http"
	"time"

	"github.com/ilyar/aiqa/internal/probe"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/telemetry"
)

// ProtocolVersion is intentionally alpha-breaking: executable extensions must
// implement cooperative v2 lifecycle reporting rather than return only a
// terminal result after their HTTP work has finished.
const ProtocolVersion = "aiqa.ext.v2"

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
	// Telemetry is host-only runtime state. Built-in SDK suites use it to
	// establish a real suite -> probe context before they call their SDK.
	// It is never serialized to executable extensions.
	Telemetry *telemetry.Provider
	// InvocationContext is host-only. It is the parent of cooperative
	// executable trace operations and is never serialized to a child process.
	InvocationContext context.Context
	// DiagnosticLogger is host-only runtime state and is never serialized to an
	// executable extension. External extensions remain responsible for their
	// own internal diagnostics.
	DiagnosticLogger DiagnosticLogger
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

// TraceEvent is a safe, cooperative lifecycle record emitted by executable
// extensions on stdout. EventID and ParentEventID are local opaque IDs: they
// are validated by the host and never become provider request identifiers.
// No URL, header, body, prompt, completion, tool call, or credential field is
// part of this contract.
type TraceEvent struct {
	SchemaVersion string `json:"schema_version"`
	Type          string `json:"type"`
	RunID         string `json:"run_id"`
	ExtensionID   string `json:"extension_id"`
	SuiteID       string `json:"suite_id"`
	EventID       string `json:"event_id"`
	ParentEventID string `json:"parent_event_id,omitempty"`
	Operation     string `json:"operation"`
	Phase         string `json:"phase"`
	Method        string `json:"method,omitempty"`
	Attempt       int    `json:"attempt,omitempty"`
	Status        int    `json:"status,omitempty"`
	DurationMS    int64  `json:"duration_ms,omitempty"`
	Outcome       string `json:"outcome,omitempty"`
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

// TraceEventSink is optional for non-executable callers. Executable.Run uses
// it when available; a collector that does not implement it rejects v2 trace
// records instead of silently discarding forensic evidence.
type TraceEventSink interface {
	EmitTrace(TraceEvent) error
}

type Extension interface {
	Manifest() Manifest
	Provenance() result.ExtensionProvenance
	Run(context.Context, RunRequest, EventSink) (RunnerTelemetry, error)
}
