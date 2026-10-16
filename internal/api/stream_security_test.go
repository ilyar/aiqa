package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func streamSecurityClient(t *testing.T, body string) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, "test-key", "aiqa-test", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestStreamManyTinyFragmentsHasLinearAllocation(t *testing.T) {
	const fragments = 4_000
	var body strings.Builder
	for range fragments {
		body.WriteString(`data: {"choices":[{"delta":{"content":"abcdefgh","tool_calls":[{"index":0,"function":{"arguments":"{}"}}]}}]}` + "\n\n")
	}
	client := streamSecurityClient(t, body.String())
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	response, err := client.StreamJSON(context.Background(), "stream", map[string]any{})
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Content) != fragments*8 {
		t.Fatalf("content bytes=%d", len(response.Content))
	}
	function, _ := response.ToolCalls[0]["function"].(map[string]any)
	if got := len(function["arguments"].(string)); got != fragments*2 {
		t.Fatalf("arguments bytes=%d", got)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 48<<20 {
		t.Fatalf("allocated %d bytes for %d tiny fragments; expected bounded linear accumulation", allocated, fragments)
	}
}

func TestStreamRejectsOversizeLineAndDataLineCount(t *testing.T) {
	for name, body := range map[string]string{
		"line":       strings.Repeat("x", maxSSELineBytes+1) + "\n",
		"data_lines": strings.Repeat("data: x\n", maxSSEDataLines+1) + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := streamSecurityClient(t, body).StreamJSON(context.Background(), "stream", map[string]any{})
			if !errors.Is(err, ErrWorkLimit) {
				t.Fatalf("error=%v, want work limit", err)
			}
		})
	}
}

func TestStreamBoundsEventsAndInvalidDiagnostics(t *testing.T) {
	t.Run("event_flood", func(t *testing.T) {
		body := strings.Repeat("data: {}\n\n", maxSSEEvents+1)
		_, err := streamSecurityClient(t, body).StreamJSON(context.Background(), "stream", map[string]any{})
		if !errors.Is(err, ErrWorkLimit) {
			t.Fatalf("error=%v, want work limit", err)
		}
	})
	t.Run("invalid_retention", func(t *testing.T) {
		response, err := streamSecurityClient(t, strings.Repeat("bad-field: x\n", maxSSEInvalidEvents+20)+"\n").StreamJSON(context.Background(), "stream", map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		if got := len(response.InvalidEvents); got != maxSSEInvalidEvents {
			t.Fatalf("invalid diagnostics=%d, want %d", got, maxSSEInvalidEvents)
		}
	})
}

func toolDelta(index string, args string) string {
	return fmt.Sprintf("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":%s,\"function\":{\"arguments\":%q}}]}}]}\n\n", index, args)
}

func TestStreamToolCallIndexesAndFieldsAreBounded(t *testing.T) {
	t.Run("choice_array", func(t *testing.T) {
		choices := strings.Repeat("{},", maxSSEToolCalls)
		_, err := streamSecurityClient(t, "data: {\"choices\":["+choices+"{}]}\n\n").StreamJSON(context.Background(), "stream", map[string]any{})
		if !errors.Is(err, ErrWorkLimit) {
			t.Fatalf("error=%v, want work limit", err)
		}
	})
	t.Run("tool_call_array", func(t *testing.T) {
		calls := strings.Repeat("null,", maxSSEToolCalls)
		body := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[" + calls + "null]}}]}\n\n"
		_, err := streamSecurityClient(t, body).StreamJSON(context.Background(), "stream", map[string]any{})
		if !errors.Is(err, ErrWorkLimit) {
			t.Fatalf("error=%v, want work limit", err)
		}
	})
	for _, index := range []string{"-1", "1.5", "999999"} {
		t.Run("reject_"+strings.ReplaceAll(index, ".", "_"), func(t *testing.T) {
			_, err := streamSecurityClient(t, toolDelta(index, "x")).StreamJSON(context.Background(), "stream", map[string]any{})
			if !errors.Is(err, ErrWorkLimit) {
				t.Fatalf("index=%s error=%v", index, err)
			}
		})
	}
	t.Run("bounded_sparse_is_accepted", func(t *testing.T) {
		response, err := streamSecurityClient(t, toolDelta("4", "x")).StreamJSON(context.Background(), "stream", map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		if len(response.ToolCalls) != 5 {
			t.Fatalf("tool calls=%d, want 5", len(response.ToolCalls))
		}
	})
	t.Run("arguments", func(t *testing.T) {
		body := toolDelta("0", strings.Repeat("a", maxSSEToolFieldBytes/2+1)) + toolDelta("0", strings.Repeat("b", maxSSEToolFieldBytes/2+1))
		_, err := streamSecurityClient(t, body).StreamJSON(context.Background(), "stream", map[string]any{})
		if !errors.Is(err, ErrWorkLimit) {
			t.Fatalf("error=%v, want work limit", err)
		}
	})
	t.Run("eof_flush", func(t *testing.T) {
		body := strings.TrimSuffix(toolDelta("-1", "x"), "\n")
		_, err := streamSecurityClient(t, body).StreamJSON(context.Background(), "stream", map[string]any{})
		if !errors.Is(err, ErrWorkLimit) {
			t.Fatalf("error=%v, want work limit at EOF", err)
		}
	})
}
