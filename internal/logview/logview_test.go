package logview

import (
	"encoding/json"
	"strings"
	"testing"
)

func line(t *testing.T, fields map[string]any) []byte {
	t.Helper()
	base := map[string]any{"schema_version": SchemaVersion, "time": "2026-09-09T12:00:00Z", "seq": 1, "level": "INFO", "event": "probe.end", "trace_id": "t", "span_id": "s", "parent_span_id": "p", "operation_id": "job-1"}
	for key, value := range fields {
		base[key] = value
	}
	encoded, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func records(items []Item) []Record {
	var out []Record
	for _, item := range items {
		if item.Record != nil {
			out = append(out, *item.Record)
		}
	}
	return out
}

func TestFiltersHonorSeverityOutcomeAndScope(t *testing.T) {
	decoder := NewDecoder()
	input := append(line(t, map[string]any{"level": "DEBUG", "outcome": "assertion_failed", "target": "a", "model": "m"}), '\n')
	input = append(input, line(t, map[string]any{"seq": 2, "level": "INFO", "status": "pass", "http_status": 401, "target": "b", "model": "m"})...)
	input = append(input, '\n')
	got := records(decoder.Feed(input, true))
	failed := Filter{Failed: true}
	if err := failed.Normalize(); err != nil {
		t.Fatal(err)
	}
	if !failed.Match(got[0]) || failed.Match(got[1]) {
		t.Fatalf("failed selection = %#v", got)
	}
	level := Error
	severity := Filter{Minimum: &level}
	if err := severity.Normalize(); err != nil {
		t.Fatal(err)
	}
	if severity.Match(got[0]) {
		t.Fatal("ERROR severity must not include DEBUG failed event")
	}
	trace := Trace
	scope := Filter{Minimum: &trace, Target: "a", Model: "m", Job: "job-1", Event: `^probe\.`}
	if err := scope.Normalize(); err != nil || !scope.Match(got[0]) || scope.Match(got[1]) {
		t.Fatalf("scope filter err=%v", err)
	}
}

func TestFilterRejectsConflictingAndInvalidBeforeRead(t *testing.T) {
	level := Info
	for _, filter := range []*Filter{{Minimum: &level, Only: map[Level]bool{Info: true}}, {Event: "["}, {Only: map[Level]bool{"NO": true}}} {
		if err := filter.Normalize(); err == nil {
			t.Fatal("Normalize accepted invalid filter")
		}
	}
}

func TestOnlyLevelAndStatusAreExact(t *testing.T) {
	decoder := NewDecoder()
	input := append(line(t, map[string]any{"level": "WARN", "outcome": "retrying"}), '\n')
	input = append(input, line(t, map[string]any{"seq": 2, "level": "ERROR", "outcome": "error"})...)
	input = append(input, '\n')
	got := records(decoder.Feed(input, true))
	filter := Filter{Only: map[Level]bool{Warn: true}, Statuses: map[string]bool{"retrying": true}}
	if err := filter.Normalize(); err != nil {
		t.Fatal(err)
	}
	if !filter.Match(got[0]) || filter.Match(got[1]) {
		t.Fatalf("only/status selection = %#v", got)
	}
}

func TestDecoderPartialMalformedOversizedAndSanitizes(t *testing.T) {
	decoder := NewDecoder()
	valid := append(line(t, map[string]any{"message": "safe\u001b[2J"}), '\n')
	if got := decoder.Feed(valid[:10], false); len(got) != 0 {
		t.Fatalf("partial = %#v", got)
	}
	got := decoder.Feed(valid[10:], false)
	if len(records(got)) != 1 {
		t.Fatalf("completed = %#v", got)
	}
	if text := SafeText("x\u001b[2J\ny"); strings.ContainsRune(text, '\x1b') || !strings.Contains(text, `\u001b`) || !strings.Contains(text, `\n`) {
		t.Fatalf("unsafe text %q", text)
	}
	if got := decoder.Feed([]byte("{bad}\n"), false); len(got) != 1 || got[0].Diagnostic.Code != "malformed_record" {
		t.Fatalf("malformed = %#v", got)
	}
	big := append([]byte(strings.Repeat("x", MaxRecordSize+1)), '\n')
	if got := decoder.Feed(big, false); len(got) != 1 || got[0].Diagnostic.Code != "record_too_large" {
		t.Fatalf("oversized = %#v", got)
	}
	if got := decoder.Feed([]byte(`{"schema_version":"aiqa.trace.v1"}`), true); len(got) != 1 || got[0].Diagnostic.Code != "partial_record" {
		t.Fatalf("final partial = %#v", got)
	}
	unsupported := append(line(t, map[string]any{"schema_version": "other.v2"}), '\n')
	if got := decoder.Feed(unsupported, false); len(got) != 1 || got[0].Diagnostic.Code != "unsupported_schema" {
		t.Fatalf("schema mismatch = %#v", got)
	}
}
