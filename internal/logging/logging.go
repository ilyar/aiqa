// Package logging provides aiqa's private, structured diagnostic log.
//
// It deliberately owns the domain safety policy, but delegates JSON encoding,
// levels and concurrent handler semantics to log/slog.
package logging

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel/trace"
)

// Level identifies a diagnostic category. The strings are part of the private
// JSONL contract, so changing them is a compatibility change.
type Level string

const (
	Trace Level = "TRACE"
	Debug Level = "DEBUG"
	Info  Level = "INFO"
	Warn  Level = "WARN"
	Error Level = "ERROR"
	Fatal Level = "FATAL"
)

const (
	slogTrace = slog.Level(-8)
	slogFatal = slog.Level(12)

	// DefaultSinkMaxBytes bounds a private streaming sidecar. A full sidecar is
	// useful for forensics; an unbounded one can still exhaust the operator's
	// disk when a provider misbehaves or a long run is left unattended.
	DefaultSinkMaxBytes int64 = 32 << 20
)

var (
	// ErrSinkLimit is retained by a Sink when its next complete JSONL record
	// would exceed MaxBytes. No partial JSON record is written.
	ErrSinkLimit = errors.New("private diagnostic sink limit reached")
)

// Verbosity returns the minimum --verbose value that includes this level.
func (level Level) Verbosity() int {
	switch level {
	case Trace:
		return 4
	case Debug:
		return 3
	case Info:
		return 2
	case Warn:
		return 1
	case Error, Fatal:
		return 0
	default:
		return -1
	}
}

func slogLevel(level Level) slog.Level {
	switch level {
	case Trace:
		return slogTrace
	case Debug:
		return slog.LevelDebug
	case Info:
		return slog.LevelInfo
	case Warn:
		return slog.LevelWarn
	case Error:
		return slog.LevelError
	case Fatal:
		return slogFatal
	default:
		return slogFatal + 1
	}
}

func externalLevel(level slog.Level) string {
	switch level {
	case slogTrace:
		return string(Trace)
	case slog.LevelDebug:
		return string(Debug)
	case slog.LevelInfo:
		return string(Info)
	case slog.LevelWarn:
		return string(Warn)
	case slog.LevelError:
		return string(Error)
	case slogFatal:
		return string(Fatal)
	default:
		return "UNKNOWN"
	}
}

// Fields contains control-flow metadata. The logger applies a strict allowlist
// before values reach the JSONL stream. Do not put provider payloads, prompts,
// completions, tool arguments, headers, credentials, full URLs or queries here.
type Fields map[string]any

type contextKey uint8

const (
	loggerContextKey contextKey = iota
	fieldsContextKey
)

// Logger retains a private JSONL stream. It is safe for concurrent probes and
// does not terminate the process for Fatal.
type Logger struct {
	verbosity int
	sequence  atomic.Uint64
	sink      *lineSink
	tee       *teeSink
	handler   *redactingHandler
	slog      *slog.Logger
	emitMu    sync.Mutex // makes LogfLine's returned record unambiguous.
}

// New constructs a logger. Invalid values are clamped to the documented range.
func New(verbosity int) *Logger {
	if verbosity < 0 {
		verbosity = 0
	}
	if verbosity > 4 {
		verbosity = 4
	}
	sink := &lineSink{}
	tee := &teeSink{memory: sink}
	base := slog.NewJSONHandler(tee, &slog.HandlerOptions{
		Level: slogTrace,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			switch attr.Key {
			case slog.LevelKey:
				level, _ := attr.Value.Any().(slog.Level)
				return slog.String(slog.LevelKey, externalLevel(level))
			case slog.MessageKey:
				return slog.String("event", safeEventName(attr.Value.String()))
			default:
				return attr
			}
		},
	})
	logger := &Logger{verbosity: verbosity, sink: sink, tee: tee}
	logger.handler = &redactingHandler{base: base, sequence: &logger.sequence, writeMu: &sync.Mutex{}}
	logger.slog = slog.New(logger.handler)
	return logger
}

// Enabled reports whether a category will be retained.
func (l *Logger) Enabled(level Level) bool {
	return level.Verbosity() >= 0 && level.Verbosity() <= l.verbosity
}

// Logf formats a control-flow message and records it if the category is enabled.
func (l *Logger) Logf(level Level, format string, args ...any) bool {
	_, ok := l.LogfLine(level, format, args...)
	return ok
}

// LogfLine returns the exact JSONL record retained by this logger.
func (l *Logger) LogfLine(level Level, format string, args ...any) (string, bool) {
	return l.logLine(context.Background(), level, "message", Fields{"message": strings.TrimRight(fmt.Sprintf(format, args...), "\n")})
}

// Log writes a structured event.
func (l *Logger) Log(level Level, event string, fields Fields) bool {
	_, ok := l.logLine(context.Background(), level, event, fields)
	return ok
}

// LogContext merges execution scope and OTel trace/span identifiers into an event.
func (l *Logger) LogContext(ctx context.Context, level Level, event string, fields Fields) bool {
	merged := FieldsFromContext(ctx)
	for key, value := range fields {
		merged[key] = value
	}
	_, ok := l.logLine(ctx, level, event, merged)
	return ok
}

func (l *Logger) logLine(ctx context.Context, level Level, event string, fields Fields) (string, bool) {
	if !l.Enabled(level) {
		return "", false
	}
	attrs := make([]slog.Attr, 0, len(fields))
	for key, value := range fields {
		attrs = append(attrs, slog.Any(key, value))
	}
	// A Log call has no return value. Serialize calls only long enough to map
	// this event to its emitted JSONL record; the handler remains concurrency-safe
	// for callers using Slog directly.
	l.emitMu.Lock()
	before := l.sink.Len()
	l.slog.LogAttrs(ctx, slogLevel(level), safeEventName(event), attrs...)
	line := l.sink.At(before)
	l.emitMu.Unlock()
	return line, true
}

// Lines returns a stable snapshot of raw JSONL records (without trailing newlines).
func (l *Logger) Lines() []string { return l.sink.Lines() }

// SinkOptions limits one streaming sidecar. MaxBytes of zero selects
// DefaultSinkMaxBytes. MaxBytes == -1 explicitly selects an unbounded sink;
// this is appropriate for the application forensic sidecar, while zero keeps
// the defensive default for embedded callers.
type SinkOptions struct {
	MaxBytes int64
}

// Sink is a thread-safe handle for a private streaming JSONL sidecar. Its
// writer is supplied by the caller and remains caller-owned; Close only removes
// the tee and does not close the writer. This allows the caller to pre-open a
// 0600 file before a run and keep crash diagnostics available immediately.
type Sink struct {
	tee       *teeSink
	writer    io.Writer
	maxBytes  int64
	mu        sync.Mutex
	written   int64
	err       error
	closed    bool
	truncated bool
}

// AddSink starts teeing each completed JSONL record to writer. If writer is an
// *os.File, it must already be private (no group/other permissions); callers
// should create it with O_CREATE|O_EXCL and mode 0600. AddSink never opens a
// pathname and therefore cannot follow an attacker-controlled replacement.
func (l *Logger) AddSink(writer io.Writer, options SinkOptions) (*Sink, error) {
	if writer == nil {
		return nil, errors.New("private diagnostic sink writer is nil")
	}
	if options.MaxBytes < -1 {
		return nil, fmt.Errorf("private diagnostic sink max bytes must not be negative: %d", options.MaxBytes)
	}
	if file, ok := writer.(*os.File); ok {
		info, err := file.Stat()
		if err != nil {
			return nil, fmt.Errorf("stat private diagnostic sink: %w", err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("private diagnostic sink is not a regular file: %s", info.Mode().Type())
		}
		if info.Mode().Perm()&0o077 != 0 {
			return nil, fmt.Errorf("private diagnostic sink must not be group/world accessible: mode %04o", info.Mode().Perm())
		}
	}
	maxBytes := options.MaxBytes
	if maxBytes == 0 {
		maxBytes = DefaultSinkMaxBytes
	}
	sink := &Sink{tee: l.tee, writer: writer, maxBytes: maxBytes}
	l.tee.add(sink)
	return sink, nil
}

// Close stops future writes to this sink. It is idempotent and deliberately
// does not close the caller-owned writer.
func (s *Sink) Close() error {
	if s == nil {
		return nil
	}
	s.tee.remove(s)
	s.mu.Lock()
	s.closed = true
	err := s.err
	s.mu.Unlock()
	return err
}

// Err reports a write failure or ErrSinkLimit. A logger continues writing its
// in-memory stream and other sinks after one sink fails.
func (s *Sink) Err() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// BytesWritten reports complete JSONL bytes successfully written to this sink.
func (s *Sink) BytesWritten() int64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.written
}

// Truncated reports whether this sink reached its byte bound.
func (s *Sink) Truncated() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.truncated
}

// WithLogger attaches a logger to a request context.
func WithLogger(ctx context.Context, logger *Logger) context.Context {
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerContextKey, logger)
}

// FromContext returns the logger attached with WithLogger, if any.
func FromContext(ctx context.Context) *Logger {
	logger, _ := ctx.Value(loggerContextKey).(*Logger)
	return logger
}

// WithFields adds immutable execution scope to a context.
func WithFields(ctx context.Context, fields Fields) context.Context {
	merged := FieldsFromContext(ctx)
	for key, value := range fields {
		merged[key] = value
	}
	return context.WithValue(ctx, fieldsContextKey, merged)
}

// Scope is a convenience helper for the standard reconstructable trace keys.
func Scope(ctx context.Context, target, model, suite, probe, operation string) context.Context {
	fields := Fields{}
	if target != "" {
		fields["target"] = target
	}
	if model != "" {
		fields["model"] = model
	}
	if suite != "" {
		fields["suite"] = suite
	}
	if probe != "" {
		fields["probe"] = probe
	}
	if operation != "" {
		fields["operation_id"] = operation
	}
	return WithFields(ctx, fields)
}

// FieldsFromContext returns a copy safe for caller mutation.
func FieldsFromContext(ctx context.Context) Fields {
	fields, _ := ctx.Value(fieldsContextKey).(Fields)
	copy := make(Fields, len(fields))
	for key, value := range fields {
		copy[key] = value
	}
	return copy
}

type lineSink struct {
	mu    sync.Mutex
	lines []string
}

// teeSink writes complete JSONL records to memory first and then every active
// sidecar while holding one mutex. This preserves record boundaries and the
// observed order in every sink, even when probes log concurrently.
type teeSink struct {
	mu     sync.Mutex
	memory *lineSink
	sinks  []*Sink
}

func (t *teeSink) Write(data []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, err := t.memory.Write(data); err != nil {
		return 0, err
	}
	for _, sink := range t.sinks {
		sink.write(data)
	}
	return len(data), nil
}

func (t *teeSink) add(sink *Sink) {
	t.mu.Lock()
	t.sinks = append(t.sinks, sink)
	t.mu.Unlock()
}

func (t *teeSink) remove(sink *Sink) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for index, candidate := range t.sinks {
		if candidate == sink {
			t.sinks = append(t.sinks[:index], t.sinks[index+1:]...)
			return
		}
	}
}

func (s *Sink) write(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.err != nil {
		return
	}
	if s.maxBytes >= 0 && int64(len(data)) > s.maxBytes-s.written {
		s.err = ErrSinkLimit
		s.truncated = true
		return
	}
	written, err := s.writer.Write(data)
	s.written += int64(written)
	if err != nil {
		s.err = err
		return
	}
	if written != len(data) {
		s.err = io.ErrShortWrite
	}
}

func (s *lineSink) Write(data []byte) (int, error) {
	line := strings.TrimSuffix(string(data), "\n")
	s.mu.Lock()
	s.lines = append(s.lines, line)
	s.mu.Unlock()
	return len(data), nil
}
func (s *lineSink) Len() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.lines) }
func (s *lineSink) At(index int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index >= len(s.lines) {
		return ""
	}
	return s.lines[index]
}
func (s *lineSink) Lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.lines...)
}

type redactingHandler struct {
	base     slog.Handler
	sequence *atomic.Uint64
	// writeMu covers sequence allocation and sink serialization as one critical
	// section. Without it concurrent handlers could assign seq=2, write it, and
	// only then write an already allocated seq=1, making a streamed timeline
	// impossible to verify in file order.
	writeMu *sync.Mutex
	attrs   []slog.Attr
	groups  []string
}

func (h *redactingHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slogTrace
}

func (h *redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	attrs := append([]slog.Attr(nil), h.attrs...)
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})
	clean := slog.NewRecord(record.Time.UTC(), record.Level, record.Message, record.PC)
	clean.AddAttrs(slog.String("schema_version", "aiqa.trace.v1"))
	clean.AddAttrs(slog.Uint64("seq", h.sequence.Add(1)))
	span := trace.SpanContextFromContext(ctx)
	if span.IsValid() {
		clean.AddAttrs(slog.String("trace_id", span.TraceID().String()), slog.String("span_id", span.SpanID().String()))
	} else {
		// Non-operation diagnostics still have a uniform, parseable schema.
		clean.AddAttrs(slog.String("trace_id", ""), slog.String("span_id", ""), slog.String("parent_span_id", ""))
	}
	if !containsAttr(attrs, "operation_id") {
		// Every record exposes the correlation key. Empty is permitted only for
		// an unscoped application message; lifecycle records are checked by the
		// offline verifier and must provide a non-empty value.
		clean.AddAttrs(slog.String("operation_id", ""))
	}
	for _, attr := range attrs {
		if cleaned, ok := safeAttr(attr); ok {
			clean.AddAttrs(h.group(cleaned))
		}
	}
	return h.base.Handle(ctx, clean)
}

func containsAttr(attrs []slog.Attr, key string) bool {
	for _, attr := range attrs {
		if attr.Key == key {
			return true
		}
	}
	return false
}

func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	copy := *h
	copy.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &copy
}
func (h *redactingHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	copy := *h
	copy.groups = append(append([]string(nil), h.groups...), name)
	return &copy
}
func (h *redactingHandler) group(attr slog.Attr) slog.Attr {
	for index := len(h.groups) - 1; index >= 0; index-- {
		attr = slog.Group(h.groups[index], attr)
	}
	return attr
}

func safeAttr(attr slog.Attr) (slog.Attr, bool) {
	attr.Value = attr.Value.Resolve()
	if !allowedField(attr.Key) {
		// Field names can themselves be provider-controlled (for example a
		// streamed JSON key). Dropping an unreviewed field is safer than
		// retaining its name with a redacted value.
		return slog.Attr{}, false
	}
	if attr.Value.Kind() == slog.KindGroup {
		return slog.String(attr.Key, "[REDACTED]"), true
	}
	if attr.Value.Kind() == slog.KindString {
		return slog.String(attr.Key, safeText(attr.Value.String())), true
	}
	return attr, true
}

func safeEventName(event string) string {
	event = strings.TrimSpace(event)
	if event == "" {
		return "unnamed"
	}
	for _, runeValue := range event {
		if !(runeValue >= 'a' && runeValue <= 'z' || runeValue >= '0' && runeValue <= '9' || runeValue == '.' || runeValue == '_' || runeValue == '-') {
			return "invalid_event"
		}
	}
	return event
}

func safeText(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 512 {
		value = value[:512] + "…"
	}
	lower := strings.ToLower(value)
	if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") || strings.Contains(lower, "authorization:") || strings.Contains(lower, "api_key") || strings.Contains(lower, "bearer ") {
		return "[REDACTED]"
	}
	return value
}

// allowedField is intentionally conservative. Adding a field requires deciding
// whether it can reveal provider input, output or credential material.
func allowedField(key string) bool {
	_, ok := allowedFields[key]
	return ok
}

// IsAllowedField exposes the trace-contract allowlist to the offline verifier.
// It does not make a field safe: callers must still pass scalar, safe values.
func IsAllowedField(key string) bool { return allowedField(key) }

var allowedFields = map[string]struct{}{
	"attempt": {}, "capacity": {}, "deadline_ms": {}, "delay_ms": {}, "done": {}, "duration_ms": {}, "error_kind": {}, "error_present": {}, "event_count": {}, "event_index": {}, "event_present": {}, "exit_code": {}, "extension": {}, "has_event_id": {}, "http_status": {}, "inflight": {}, "invalid_event_count": {}, "json_key_count": {}, "max_concurrency": {}, "message": {}, "method": {}, "model": {}, "next_attempt": {}, "operation_id": {}, "outcome": {}, "path": {}, "probe": {}, "protocol": {}, "reason": {}, "request_interval_ms": {}, "request_timeout_ms": {}, "retry_limit": {}, "run_id": {}, "runner_error": {}, "slots": {}, "source": {}, "status": {}, "stream": {}, "suite": {}, "suite_count": {}, "target": {}, "transport_operation": {}, "wait_ms": {},
	"data_kind": {}, "extension_id": {}, "emitted_suites": {}, "planned_suites": {}, "pacing_safe_concurrency": {}, "pacing_safe_rps": {}, "startup_ms": {}, "subject_kind": {}, "subject_ref": {}, "total_ms": {}, "usage_fields": {}, "trace_id": {}, "span_id": {}, "parent_span_id": {}, "parent_operation_id": {},
	"result_id": {}, "result_count": {}, "derived_from": {},
	"native_selected": {}, "sdk_selected": {}, "extension_selected": {},
}
