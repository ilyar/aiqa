// Package protocol contains the target runner contract shared by native
// protocol implementations.  Keeping the registration boundary here prevents
// protocol-specific clients from leaking into configuration, reporting, or
// extension planning code.
package protocol

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/probe"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/progress"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/scheduler"
	"github.com/ilyar/aiqa/internal/telemetry"
)

// JobID produces a stable namespace across targets and models. It is safe for
// progress sinks which deduplicate IDs in the frozen global plan.
func JobID(target, model, kind string) string {
	return strings.Join([]string{"native", strings.TrimSpace(target), strings.TrimSpace(model), strings.TrimSpace(kind)}, ":")
}

// ExecuteFrozen remains a narrow compatibility wrapper. Native runners own
// their logical boundaries and call StartJob/CompleteJob at those boundaries.
func ExecuteFrozen(ctx context.Context, sink progress.Sink, jobs []Job, run func(context.Context) (RunResult, error)) (RunResult, error) {
	return run(ctx)
}

// StartJob returns an idempotent terminal callback for one frozen native job.
// A nil callback is deliberately harmless for legacy/non-rendering callers.
func StartJob(sink progress.Sink, target, model, kind string) func(string) {
	frozen, ok := sink.(progress.FrozenSink)
	if !ok {
		return func(string) {}
	}
	id := JobID(target, model, kind)
	frozen.Start(id)
	return func(outcome string) { frozen.Complete(id, outcome) }
}

// RunOptions is the protocol-neutral portion of a native target invocation.
type RunOptions struct {
	Target    config.TargetConfig
	Config    config.Config
	Profile   profile.Profile
	UserAgent string
	Logf      func(logging.Level, string, ...any)
	// Logger is the private diagnostic sink shared with the target transport.
	// Logf remains for compatibility with protocol implementations and tests.
	Logger    *logging.Logger
	Telemetry *telemetry.Provider
	// Progress is an operator-facing state sink. It is independent of verbose
	// JSONL logging and may be nil for compatibility with existing callers.
	Progress progress.Sink
	// Inventory is set only by a Preparer. Native execution consumes it rather
	// than issuing another model-list request.
	Inventory *Inventory
	// Scheduler is the run-owned physical-attempt admission broker. It is
	// deliberately optional so direct protocol callers retain their historical
	// transport behavior. The application supplies one shared broker for a run.
	Scheduler *scheduler.Broker
	// PacingGroup optionally aliases several target origins that consume one
	// provider quota. It is a safe local label, never a URL or credential.
	PacingGroup string
	// ExecutionHooks optionally connects privacy-safe transport facts to the
	// run reducer. It is deliberately data-only at this boundary: native
	// execution jobs are not inferred until a later frozen-plan mapping step.
	ExecutionHooks ExecutionHooks
}

// ExecutionHooks is the protocol-neutral execution-observability contract.
// PreparationOwner is declared before discovery and therefore may carry only
// a registered PreparationID, never a runtime JobID.
type ExecutionHooks struct {
	Observe               api.ExecutionObserver
	ObserveScheduler      api.SchedulerExecutionObserver
	ObserveSchedulerGroup api.SchedulerGroupObserver
	PreparationOwner      api.ExecutionOwner
}

func (h ExecutionHooks) validate(target string) error {
	owner := h.PreparationOwner
	if owner.TargetID == "" && owner.PreparationID == "" && owner.JobID == "" {
		return nil
	}
	if owner.TargetID != target || owner.PreparationID == "" || owner.JobID != "" {
		return fmt.Errorf("invalid execution preparation owner")
	}
	return nil
}

// PreparationContext attaches the declared discovery owner to a Prepare
// request context. It intentionally never derives or attaches a runtime job.
func PreparationContext(ctx context.Context, options RunOptions) context.Context {
	owner := options.ExecutionHooks.PreparationOwner
	if owner.PreparationID == "" {
		return ctx
	}
	return api.WithExecutionOwner(ctx, owner)
}

// ConfigureClientScheduler binds a target-owned API client to the optional
// run broker. Target identity comes exclusively from the configured target ID;
// api.Client validates both it and an explicitly supplied pacing alias.
func ConfigureClientScheduler(client *api.Client, options RunOptions) error {
	if client == nil {
		return fmt.Errorf("nil API client")
	}
	if options.Scheduler == nil {
		if options.PacingGroup != "" {
			return fmt.Errorf("pacing group requires a scheduler")
		}
		return nil
	}
	client.SetSchedulerExecutionObserver(options.ExecutionHooks.ObserveScheduler)
	client.SetSchedulerGroupObserver(options.ExecutionHooks.ObserveSchedulerGroup)
	return client.SetScheduler(api.SchedulerConfig{
		Broker:       options.Scheduler,
		Target:       options.Target.ID,
		Group:        options.PacingGroup,
		QueueTimeout: options.Config.QueueTimeout,
	})
}

// ConfigureClientObservability installs the target-owned diagnostic and trace
// scope before any network activity. Preparers must call it before inventory so
// the first provider request belongs to the same target lifecycle as execution.
func ConfigureClientObservability(client *api.Client, options RunOptions) error {
	if client == nil {
		return fmt.Errorf("nil API client")
	}
	if err := ConfigureClientScheduler(client, options); err != nil {
		return err
	}
	if err := options.ExecutionHooks.validate(options.Target.ID); err != nil {
		return err
	}
	client.SetLogger(options.Logger)
	client.SetLogFields(logging.Fields{"target": options.Target.ID, "protocol": string(options.Target.Protocol)})
	client.SetTelemetry(options.Telemetry)
	client.SetOTelPropagationAllowlist(options.Config.OTelPropagationTo)
	client.SetExecutionObserver(options.ExecutionHooks.Observe)
	return nil
}

// Inventory is the transport observation obtained during preparation.
type Inventory struct {
	Models   []api.Model
	Response api.Response
	Err      error
	Runtime  Runtime
}

// Runtime is the protocol-neutral execution state shared with extension
// suites after native probes complete. The protocol runner owns both values.
type Runtime struct {
	Client   *api.Client
	Executor *probe.Executor
}

// RunResult is the complete result of one native target invocation.
type RunResult struct {
	Target  result.TargetReport
	Runtime Runtime
}

// Job is one report-visible logical unit of work. It intentionally does not
// describe HTTP attempts: a composite check (notably model-param) is still one
// job even when its implementation makes adaptive requests.
type Job struct {
	ID     string
	Target string
	Model  string
	Label  string
}

// PreparedTarget is the immutable result of target discovery and selection.
// Execute is deliberately renderer-agnostic; the application may concatenate
// Jobs from every target and freeze one global progress total before invoking
// any closure.
type PreparedTarget struct {
	Target     config.TargetConfig
	Profile    profile.Profile
	Jobs       []Job
	Candidates []result.ModelReport
	// Binding preserves the resolved selection and exact SDK invocations from
	// Prepare. It is intentionally data-only so existing native runners remain
	// compatible while application orchestration moves to RunPlan.
	Binding TargetBinding
	Execute func(context.Context, progress.Sink) (RunResult, error)
}

// ExecutePlan is the mandatory application boundary between a frozen RunPlan
// and a captured preparation closure. It rejects target/job drift before any
// execution can start; protocol-specific bindings remain private to Prepare.
func (p PreparedTarget) ExecutePlan(ctx context.Context, sink progress.Sink, plan TargetExecutionPlan) (RunResult, error) {
	if p.Execute == nil || plan.TargetID != p.Target.ID || plan.Binding.TargetID != p.Target.ID {
		return RunResult{}, fmt.Errorf("prepared target does not match frozen execution plan")
	}
	seen := make(map[string]struct{}, len(plan.Jobs))
	for _, job := range plan.Jobs {
		seen[job.ID] = struct{}{}
	}
	for _, job := range p.Jobs {
		if _, ok := seen[job.ID]; !ok {
			return RunResult{}, fmt.Errorf("prepared job %q is absent from frozen execution plan", job.ID)
		}
	}
	return p.Execute(ctx, sink)
}

// Preparer performs only inventory, selection and planning. Implementations
// must retain the discovered inventory for Execute and must not list models a
// second time.
type Preparer func(context.Context, RunOptions) (PreparedTarget, error)

// Runner is the adapter used by the application registry.
type Runner func(context.Context, RunOptions) (RunResult, error)

// Registry maps stable protocol IDs to native runners.
type Registry struct {
	runners   map[config.Protocol]Runner
	preparers map[config.Protocol]Preparer
}

func NewRegistry() *Registry {
	return &Registry{runners: make(map[config.Protocol]Runner), preparers: make(map[config.Protocol]Preparer)}
}

func (r *Registry) Register(protocol config.Protocol, runner Runner) error {
	if r == nil {
		return fmt.Errorf("nil protocol registry")
	}
	if protocol == "" || runner == nil {
		return fmt.Errorf("protocol and runner are required")
	}
	if _, exists := r.runners[protocol]; exists {
		return fmt.Errorf("duplicate protocol runner %q", protocol)
	}
	r.runners[protocol] = runner
	return nil
}

func (r *Registry) RegisterPreparer(protocol config.Protocol, preparer Preparer) error {
	if r == nil {
		return fmt.Errorf("nil protocol registry")
	}
	if protocol == "" || preparer == nil {
		return fmt.Errorf("protocol and preparer are required")
	}
	if _, exists := r.preparers[protocol]; exists {
		return fmt.Errorf("duplicate protocol preparer %q", protocol)
	}
	r.preparers[protocol] = preparer
	return nil
}

func (r *Registry) Preparer(protocol config.Protocol) (Preparer, bool) {
	if r == nil {
		return nil, false
	}
	p, ok := r.preparers[protocol]
	return p, ok
}

func (r *Registry) Runner(protocol config.Protocol) (Runner, bool) {
	if r == nil {
		return nil, false
	}
	runner, ok := r.runners[protocol]
	return runner, ok
}

func (r *Registry) Protocols() []config.Protocol {
	if r == nil {
		return nil
	}
	protocols := make([]config.Protocol, 0, len(r.runners))
	for protocol := range r.runners {
		protocols = append(protocols, protocol)
	}
	sort.Slice(protocols, func(i, j int) bool { return protocols[i] < protocols[j] })
	return protocols
}
