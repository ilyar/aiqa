// Package telemetry provides local-only OpenTelemetry correlation for AIQA.
// It never injects trace context into outbound requests; propagation is an
// explicit transport concern and must be enabled only for allowlisted targets.
package telemetry

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ilyar/aiqa/internal/logging"
	"go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// Provider owns an in-process tracer provider. It intentionally installs no
// global provider, exporter or propagator: traces remain local to this run.
type Provider struct {
	tp          *trace.TracerProvider
	tracer      oteltrace.Tracer
	logger      *logging.Logger
	sequence    atomic.Uint64
	observersMu sync.RWMutex
	observers   []Observer
}

// LifecycleEvent is a redacted, in-process observation of an operation. It is
// deliberately smaller than logging.Fields: observers must never need request
// payloads, response bodies, credentials, or full URLs to render progress.
// Observers are called regardless of diagnostic logging verbosity.
type LifecycleEvent struct {
	Time              time.Time
	Name              string
	Phase             string
	OperationID       string
	ParentOperationID string
	Target            string
	Model             string
	Suite             string
	Probe             string
	Outcome           string
	Wait              time.Duration
}

// Observer receives local lifecycle observations. Implementations must return
// promptly and must not call back into Provider while handling an event.
type Observer interface{ ObserveLifecycle(LifecycleEvent) }

// New constructs local-only tracing. The caller must call Shutdown at the end
// of a run to release SDK resources. No network exporter is configured.
func New(logger *logging.Logger, observers ...Observer) *Provider {
	tp := trace.NewTracerProvider(trace.WithSampler(trace.AlwaysSample()))
	return &Provider{tp: tp, tracer: tp.Tracer("github.com/ilyar/aiqa"), logger: logger, observers: append([]Observer(nil), observers...)}
}

// AddObserver attaches an in-process lifecycle consumer. It is safe to call
// before concurrent execution starts; existing APIs keep working unchanged.
func (p *Provider) AddObserver(observer Observer) {
	if p == nil || observer == nil {
		return
	}
	p.observersMu.Lock()
	p.observers = append(p.observers, observer)
	p.observersMu.Unlock()
}

// TracerProvider exposes the local SDK provider to target-owned instrumentation.
func (p *Provider) TracerProvider() oteltrace.TracerProvider { return p.tp }

// Shutdown releases local tracing resources. It never exports remotely.
func (p *Provider) Shutdown(ctx context.Context) error { return p.tp.Shutdown(ctx) }

// Operation is a typed lifecycle handle. Start, Event and End guarantee the
// corresponding forensic JSONL events share operation_id and OTel correlation.
type Operation struct {
	provider *Provider
	ctx      context.Context
	span     oteltrace.Span
	name     string
	fields   logging.Fields
	ended    atomic.Bool
}

// Start creates a child operation and emits <name>.start. Names must be stable
// domain identifiers such as "probe" or "http.attempt"; not provider payload.
func (p *Provider) Start(ctx context.Context, name string, fields logging.Fields) (context.Context, *Operation) {
	name = safeName(name)
	operationID := fmt.Sprintf("op-%06d", p.sequence.Add(1))
	merged := logging.FieldsFromContext(ctx)
	parent := oteltrace.SpanContextFromContext(ctx)
	if parent.IsValid() {
		merged["parent_span_id"] = parent.SpanID().String()
	} else {
		// Root operations retain the key too so every JSONL record has the
		// same correlation schema.
		merged["parent_span_id"] = ""
	}
	for key, value := range fields {
		merged[key] = value
	}
	if parent, ok := merged["operation_id"].(string); ok && parent != "" {
		merged["parent_operation_id"] = parent
	}
	merged["operation_id"] = operationID
	ctx = logging.WithFields(ctx, merged)
	ctx, span := p.tracer.Start(ctx, name)
	op := &Operation{provider: p, ctx: ctx, span: span, name: name, fields: merged}
	op.Event(logging.Trace, "start", nil)
	return ctx, op
}

// Context returns the operation context for child work.
func (o *Operation) Context() context.Context { return o.ctx }

// Event emits a typed lifecycle event. Span events intentionally contain no
// arbitrary attributes: the redacted JSONL record is the forensic source and
// is the only place where domain fields may be retained.
func (o *Operation) Event(level logging.Level, phase string, fields logging.Fields) {
	phase = safeName(phase)
	merged := copyFields(o.fields)
	for key, value := range fields {
		merged[key] = value
	}
	o.span.AddEvent(o.name + "." + phase)
	if o.provider.logger != nil {
		o.provider.logger.LogContext(o.ctx, level, o.name+"."+phase, merged)
	}
	o.provider.observe(o.name, phase, merged)
}

func (p *Provider) observe(name, phase string, fields logging.Fields) {
	p.observersMu.RLock()
	observers := append([]Observer(nil), p.observers...)
	p.observersMu.RUnlock()
	if len(observers) == 0 {
		return
	}
	event := LifecycleEvent{Time: time.Now(), Name: name, Phase: phase}
	if value, ok := fields["operation_id"].(string); ok {
		event.OperationID = value
	}
	if value, ok := fields["parent_operation_id"].(string); ok {
		event.ParentOperationID = value
	}
	if value, ok := fields["target"].(string); ok {
		event.Target = value
	}
	if value, ok := fields["model"].(string); ok {
		event.Model = value
	}
	if value, ok := fields["suite"].(string); ok {
		event.Suite = value
	}
	if value, ok := fields["probe"].(string); ok {
		event.Probe = value
	}
	if value, ok := fields["outcome"].(string); ok {
		event.Outcome = value
	}
	if millis, ok := fields["wait_ms"].(int64); ok {
		event.Wait = time.Duration(millis) * time.Millisecond
	}
	if millis, ok := fields["wait_ms"].(int); ok {
		event.Wait = time.Duration(millis) * time.Millisecond
	}
	for _, observer := range observers {
		observer.ObserveLifecycle(event)
	}
}

// End emits the terminal event once and closes its local span. Outcome and
// error classification must be supplied by the caller; raw errors are not safe
// telemetry because they can contain provider response data.
func (o *Operation) End(outcome string, fields logging.Fields) {
	if !o.ended.CompareAndSwap(false, true) {
		return
	}
	merged := copyFields(fields)
	merged["outcome"] = outcome
	o.Event(logging.Debug, "end", merged)
	o.span.End()
}

func copyFields(fields logging.Fields) logging.Fields {
	copy := make(logging.Fields, len(fields))
	for key, value := range fields {
		copy[key] = value
	}
	return copy
}

func safeName(name string) string {
	if name == "" {
		return "operation"
	}
	return name
}
