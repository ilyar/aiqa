package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLoggerThresholdsAndFormatting(t *testing.T) {
	logger := New(2)
	if logger.Logf(Debug, "hidden") {
		t.Fatal("debug line was not filtered")
	}
	for _, level := range []Level{Error, Fatal, Warn, Info} {
		if !logger.Logf(level, "%s event", level) {
			t.Fatalf("%s was unexpectedly filtered", level)
		}
	}
	for index, line := range logger.Lines() {
		if !strings.HasPrefix(line, "{") {
			t.Fatalf("line %d is not a structured event: %q", index, line)
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode line %d: %v", index, err)
		}
		if event["event"] != "message" || event["message"] == "" || event["time"] == "" || event["level"] == "" || event["seq"] != float64(index+1) || event["operation_id"] != "" {
			t.Fatalf("event %d = %#v", index, event)
		}
	}
}

func TestLoggerRedactsUnknownAndUnsafeValues(t *testing.T) {
	logger := New(4)
	logger.Log(Trace, "safe.event", Fields{
		"unreviewed_field": "do-not-retain",
		"message":          "request to https://provider.example/v1 must not retain URL",
		"path":             "/v1/messages",
	})
	var event map[string]any
	if err := json.Unmarshal([]byte(logger.Lines()[0]), &event); err != nil {
		t.Fatal(err)
	}
	if _, present := event["unreviewed_field"]; present || event["message"] != "[REDACTED]" || event["path"] != "/v1/messages" {
		t.Fatalf("redaction = %#v", event)
	}
}

func TestLoggerStructuredContextAndRedaction(t *testing.T) {
	logger := New(4)
	ctx := Scope(WithLogger(context.Background(), logger), "openai", "gpt-test", "compat", "chat.basic", "probe-7")
	if !logger.LogContext(ctx, Trace, "http.attempt.begin", Fields{"method": "POST", "path": "/v1/chat/completions", "query": "do-not-log", "api_key": "do-not-log"}) {
		t.Fatal("trace event was filtered")
	}
	line := logger.Lines()[0]
	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatal(err)
	}
	if event["target"] != "openai" || event["model"] != "gpt-test" || event["probe"] != "chat.basic" || event["operation_id"] != "probe-7" {
		t.Fatalf("scope lost: %#v", event)
	}
	if _, present := event["query"]; present {
		t.Fatalf("query field must be dropped: %#v", event)
	}
	if _, present := event["api_key"]; present {
		t.Fatalf("credential field must be dropped: %#v", event)
	}
	if event["path"] != "/v1/chat/completions" {
		t.Fatalf("path = %#v", event["path"])
	}
}

func TestLoggerDropsEveryForbiddenDiagnosticFieldClass(t *testing.T) {
	for key, value := range map[string]any{
		"authorization": "Bearer fixture-secret", "headers": map[string]any{"x": "secret"},
		"request_body": "prompt", "response_body": "output", "prompt": "private prompt",
		"tool_arguments": "private args", "media": "base64-data", "credential": "secret",
		"query": "key=secret", "api_key": "secret",
	} {
		t.Run(key, func(t *testing.T) {
			logger := New(4)
			logger.Log(Trace, "safe.event", Fields{key: value})
			var event map[string]any
			if err := json.Unmarshal([]byte(logger.Lines()[0]), &event); err != nil {
				t.Fatal(err)
			}
			if _, present := event[key]; present {
				t.Fatalf("forbidden %q retained: %#v", key, event)
			}
			encoded, _ := json.Marshal(event)
			if bytes.Contains(encoded, []byte("fixture-secret")) || bytes.Contains(encoded, []byte("private prompt")) || bytes.Contains(encoded, []byte("base64-data")) {
				t.Fatalf("forbidden value leaked: %s", encoded)
			}
		})
	}
}

func TestLoggerVerbosityBoundaries(t *testing.T) {
	for _, test := range []struct {
		verbosity int
		enabled   []Level
	}{
		{0, []Level{Error, Fatal}},
		{1, []Level{Error, Fatal, Warn}},
		{2, []Level{Error, Fatal, Warn, Info}},
		{3, []Level{Error, Fatal, Warn, Info, Debug}},
		{4, []Level{Error, Fatal, Warn, Info, Debug, Trace}},
	} {
		logger := New(test.verbosity)
		for _, level := range []Level{Trace, Debug, Info, Warn, Error, Fatal} {
			want := false
			for _, candidate := range test.enabled {
				want = want || level == candidate
			}
			if got := logger.Enabled(level); got != want {
				t.Fatalf("verbosity %d: %s enabled = %v, want %v", test.verbosity, level, got, want)
			}
		}
	}
}

func TestLoggerIsConcurrent(t *testing.T) {
	logger := New(4)
	const count = 50
	var group sync.WaitGroup
	for index := 0; index < count; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			logger.Logf(Trace, "event %d", index)
		}(index)
	}
	group.Wait()
	if got := len(logger.Lines()); got != count {
		t.Fatalf("line count = %d, want %d", got, count)
	}
}

func TestLoggerAddSinkStreamsCompleteJSONLAndPreservesMemory(t *testing.T) {
	logger := New(4)
	var stream bytes.Buffer
	sink, err := logger.AddSink(&stream, SinkOptions{MaxBytes: 4096})
	if err != nil {
		t.Fatalf("add sink: %v", err)
	}
	logger.Log(Trace, "probe.start", Fields{"target": "openai"})
	logger.Log(Debug, "probe.end", Fields{"outcome": "success"})
	if err := sink.Close(); err != nil {
		t.Fatalf("close sink: %v", err)
	}
	lines := logger.Lines()
	if got := strings.Split(strings.TrimSuffix(stream.String(), "\n"), "\n"); len(got) != len(lines) || got[0] != lines[0] || got[1] != lines[1] {
		t.Fatalf("stream did not preserve JSONL records: %q vs %#v", stream.String(), lines)
	}
	if sink.BytesWritten() != int64(stream.Len()) || sink.Truncated() || sink.Err() != nil {
		t.Fatalf("sink state = bytes=%d truncated=%t err=%v", sink.BytesWritten(), sink.Truncated(), sink.Err())
	}
}

func TestLoggerAddSinkBoundsWithoutPartialJSON(t *testing.T) {
	logger := New(4)
	var stream bytes.Buffer
	sink, err := logger.AddSink(&stream, SinkOptions{MaxBytes: 1})
	if err != nil {
		t.Fatalf("add sink: %v", err)
	}
	logger.Log(Trace, "probe.start", Fields{"target": "openai"})
	if stream.Len() != 0 {
		t.Fatalf("limited sink wrote partial JSONL record: %q", stream.String())
	}
	if !errors.Is(sink.Err(), ErrSinkLimit) || !sink.Truncated() {
		t.Fatalf("sink limit state = err=%v truncated=%t", sink.Err(), sink.Truncated())
	}
	if got := len(logger.Lines()); got != 1 {
		t.Fatalf("memory log lost event after sink limit: %d", got)
	}
}

func TestLoggerAddSinkRejectsInsecureFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diagnostics.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if _, err := New(4).AddSink(file, SinkOptions{}); err == nil || !strings.Contains(err.Error(), "group/world") {
		t.Fatalf("AddSink accepted insecure file: %v", err)
	}
}

func TestLoggerAddSinkIsConcurrent(t *testing.T) {
	logger := New(4)
	var stream bytes.Buffer
	sink, err := logger.AddSink(&stream, SinkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	const count = 40
	var group sync.WaitGroup
	for index := 0; index < count; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			logger.Log(Trace, "probe.event", Fields{"attempt": index})
		}(index)
	}
	group.Wait()
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if got := len(strings.Split(strings.TrimSuffix(stream.String(), "\n"), "\n")); got != count {
		t.Fatalf("stream records = %d, want %d", got, count)
	}
}
