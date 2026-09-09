// Package logview reads AIQA private forensic JSONL without executing or
// trusting its contents. It contains no filesystem polling or terminal I/O;
// callers supply bytes and snapshots so CLI and tests can choose their own
// clock and source.
package logview

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const (
	SchemaVersion = "aiqa.trace.v1"
	MaxRecordSize = 1 << 20
)

var ErrUnsupportedSchema = errors.New("unsupported log schema")

var eventName = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)

// Level is a forensic severity. Its ordering is deliberately independent of
// runner verbosity: a lower bound includes the more severe entries.
type Level string

const (
	Trace Level = "TRACE"
	Debug Level = "DEBUG"
	Info  Level = "INFO"
	Warn  Level = "WARN"
	Error Level = "ERROR"
	Fatal Level = "FATAL"
)

func (l Level) rank() (int, bool) {
	switch l {
	case Trace:
		return 0, true
	case Debug:
		return 1, true
	case Info:
		return 2, true
	case Warn:
		return 3, true
	case Error:
		return 4, true
	case Fatal:
		return 5, true
	default:
		return 0, false
	}
}

// Record is the schema-v1 subset the viewer may filter. Fields contains only
// JSON scalar values from the record; the reader never makes network requests
// or treats any field as an instruction.
type Record struct {
	SchemaVersion string
	Time          time.Time
	Seq           uint64
	Level         Level
	Event         string
	TraceID       string
	SpanID        string
	ParentSpanID  string
	OperationID   string
	Fields        map[string]json.RawMessage
	Raw           map[string]json.RawMessage
}

func (r Record) String(key string) string {
	raw := r.Fields[key]
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}

func (r Record) Target() string { return r.String("target") }
func (r Record) Model() string  { return r.String("model") }

// Job accepts the forthcoming execution job_id while keeping current trace-v1
// operation_id useful to the CLI during the migration.
func (r Record) Job() string {
	if value := r.String("job_id"); value != "" {
		return value
	}
	if value := r.String("job"); value != "" {
		return value
	}
	return r.OperationID
}

func (r Record) Outcome() string {
	if value := r.String("outcome"); value != "" {
		return value
	}
	return r.String("status")
}

// Filter implements aiqa log selection. All non-empty dimensions are ANDed.
// Failed internally ORs approved failure outcomes with ERROR/FATAL severity.
type Filter struct {
	Minimum  *Level
	Only     map[Level]bool
	Failed   bool
	Statuses map[string]bool
	Target   string
	Model    string
	Job      string
	Event    string

	eventRE *regexp.Regexp
}

// Normalize validates flag semantics before a source is opened. The default
// level is INFO, except that --failed alone must expose low-severity failures.
func (f *Filter) Normalize() error {
	if f.Minimum != nil && len(f.Only) != 0 {
		return errors.New("--level and --only-level are mutually exclusive")
	}
	if f.Minimum == nil && len(f.Only) == 0 && !f.Failed {
		level := Info
		f.Minimum = &level
	}
	if f.Minimum != nil {
		if _, ok := f.Minimum.rank(); !ok {
			return fmt.Errorf("invalid log level %q", *f.Minimum)
		}
	}
	for level := range f.Only {
		if _, ok := level.rank(); !ok {
			return fmt.Errorf("invalid only-level %q", level)
		}
	}
	if f.Event != "" {
		re, err := regexp.Compile(f.Event)
		if err != nil {
			return fmt.Errorf("invalid event regexp: %w", err)
		}
		f.eventRE = re
	}
	return nil
}

func (f Filter) Match(r Record) bool {
	if f.Minimum != nil {
		actual, _ := r.Level.rank()
		minimum, _ := f.Minimum.rank()
		if actual < minimum {
			return false
		}
	}
	if len(f.Only) != 0 && !f.Only[r.Level] {
		return false
	}
	if f.Failed && !r.failed() {
		return false
	}
	if len(f.Statuses) != 0 && !f.Statuses[r.Outcome()] {
		return false
	}
	if f.Target != "" && r.Target() != f.Target || f.Model != "" && r.Model() != f.Model || f.Job != "" && r.Job() != f.Job {
		return false
	}
	if f.eventRE != nil && !f.eventRE.MatchString(r.Event) {
		return false
	}
	return true
}

func (r Record) failed() bool {
	if r.Level == Error || r.Level == Fatal {
		return true
	}
	switch strings.ToLower(r.Outcome()) {
	case "fail", "assertion_failed", "execution_error", "error":
		return true
	default:
		return false
	}
}

// Diagnostic is a safe, local reader observation. It never includes input
// bytes, so malformed or hostile records cannot be echoed to a terminal.
type Diagnostic struct {
	Code string
	Text string
}

// Item is either a valid record or a reader diagnostic.
type Item struct {
	Record     *Record
	Diagnostic *Diagnostic
}

func diagnostic(code, text string) Item {
	return Item{Diagnostic: &Diagnostic{Code: code, Text: text}}
}

// Decoder retains at most one unfinished record. Feed may be called with any
// byte partition; only newline-terminated JSON is decoded until Final.
type Decoder struct {
	buf       []byte
	discard   bool
	tooLong   bool
	maxRecord int
}

func NewDecoder() *Decoder { return &Decoder{maxRecord: MaxRecordSize} }

// Feed consumes bytes. A final unterminated line is diagnosed once rather than
// repeatedly parsed as malformed during follow polling.
func (d *Decoder) Feed(data []byte, final bool) []Item {
	var out []Item
	for len(data) != 0 {
		if d.discard {
			if index := bytes.IndexByte(data, '\n'); index >= 0 {
				data = data[index+1:]
				d.discard, d.tooLong = false, false
				continue
			}
			return out
		}
		index := bytes.IndexByte(data, '\n')
		if index < 0 {
			if len(d.buf)+len(data) > d.maxRecord {
				if !d.tooLong {
					out = append(out, diagnostic("record_too_large", "record exceeds 1 MiB and was discarded"))
					d.tooLong = true
				}
				d.buf = d.buf[:0]
				d.discard = true
				return out
			}
			d.buf = append(d.buf, data...)
			break
		}
		line := data[:index]
		data = data[index+1:]
		if len(d.buf)+len(line) > d.maxRecord {
			if !d.tooLong {
				out = append(out, diagnostic("record_too_large", "record exceeds 1 MiB and was discarded"))
			}
			d.buf, d.tooLong = d.buf[:0], false
			continue
		}
		d.buf = append(d.buf, line...)
		if len(bytes.TrimSpace(d.buf)) != 0 {
			record, err := parse(d.buf)
			if err != nil {
				if errors.Is(err, ErrUnsupportedSchema) {
					out = append(out, diagnostic("unsupported_schema", "unsupported schema; select a compatible log reader"))
				} else {
					out = append(out, diagnostic("malformed_record", "record is not a supported aiqa.trace.v1 JSON object"))
				}
			} else {
				out = append(out, Item{Record: &record})
			}
		}
		d.buf = d.buf[:0]
	}
	if final && (len(d.buf) != 0 || d.discard) {
		out = append(out, diagnostic("partial_record", "source ended without a complete JSONL record"))
		d.buf, d.discard, d.tooLong = d.buf[:0], false, false
	}
	return out
}

func parse(line []byte) (Record, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return Record{}, err
	}
	var wire struct {
		SchemaVersion string `json:"schema_version"`
		Time          string `json:"time"`
		Seq           uint64 `json:"seq"`
		Level         Level  `json:"level"`
		Event         string `json:"event"`
		TraceID       string `json:"trace_id"`
		SpanID        string `json:"span_id"`
		ParentSpanID  string `json:"parent_span_id"`
		OperationID   string `json:"operation_id"`
	}
	if err := json.Unmarshal(line, &wire); err != nil {
		return Record{}, err
	}
	if wire.SchemaVersion != SchemaVersion {
		return Record{}, fmt.Errorf("%w %q", ErrUnsupportedSchema, wire.SchemaVersion)
	}
	when, err := time.Parse(time.RFC3339Nano, wire.Time)
	if err != nil || wire.Seq == 0 || !eventName.MatchString(wire.Event) {
		return Record{}, errors.New("schema v1 required field is invalid")
	}
	if _, ok := wire.Level.rank(); !ok {
		return Record{}, errors.New("schema v1 level is invalid")
	}
	for _, key := range []string{"schema_version", "time", "seq", "level", "event", "trace_id", "span_id", "parent_span_id", "operation_id"} {
		if _, ok := raw[key]; !ok {
			return Record{}, errors.New("schema v1 mandatory field missing")
		}
	}
	fields := make(map[string]json.RawMessage, len(raw))
	for key, value := range raw {
		fields[key] = value
	}
	return Record{SchemaVersion: wire.SchemaVersion, Time: when, Seq: wire.Seq, Level: wire.Level, Event: wire.Event, TraceID: wire.TraceID, SpanID: wire.SpanID, ParentSpanID: wire.ParentSpanID, OperationID: wire.OperationID, Fields: fields, Raw: raw}, nil
}

// SafeText makes strings safe for a human-oriented terminal renderer. JSON
// renderers should marshal values normally, which also escapes controls.
func SafeText(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case unicode.IsControl(r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
