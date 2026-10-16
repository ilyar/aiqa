package suites

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
)

func TestStreamingDeadlineAfterValidPartialSSEIsExecutionGap(t *testing.T) {
	for _, includeUsage := range []bool{false, true} {
		t.Run(fmt.Sprintf("include_usage=%t", includeUsage), func(t *testing.T) {
			server := interruptedSSEServer(t, validSSEChunk())
			defer server.Close()
			got := probeStreaming(context.Background(), streamTestEnvironment(t, server.URL), "test-model", includeUsage)
			if got.Status != result.StatusError || !hasStreamInterruptionSignal(got) {
				t.Fatalf("partial stream=%+v", got)
			}
			if got.HTTP.StatusCode != http.StatusOK || len(got.HTTP.Snippet) == 0 {
				t.Fatalf("partial stream lost HTTP evidence: %+v", got.HTTP)
			}
		})
	}
}

func TestStreamingCleanEOFWithoutDoneRemainsProtocolFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, validSSEChunk())
	}))
	defer server.Close()
	got := probeStreaming(context.Background(), streamTestEnvironment(t, server.URL), "test-model", false)
	if got.Status != result.StatusFail || !strings.Contains(got.Observation, "[DONE] terminator is missing") {
		t.Fatalf("clean EOF must remain a protocol failure: %+v", got)
	}
	if hasStreamInterruptionSignal(got) {
		t.Fatalf("clean EOF was mislabeled interrupted: %+v", got.Signals)
	}
}

func TestMalformedSSEBeforeDeadlineRemainsProtocolFailure(t *testing.T) {
	server := interruptedSSEServer(t, "data: {not-json}\n\n")
	defer server.Close()
	got := probeStreaming(context.Background(), streamTestEnvironment(t, server.URL), "test-model", false)
	if got.Status != result.StatusFail || !strings.Contains(got.Summary, "protocol differs") {
		t.Fatalf("malformed stream must remain a protocol failure: %+v", got)
	}
	if hasStreamInterruptionSignal(got) {
		t.Fatalf("malformed stream was mislabeled interrupted: %+v", got.Signals)
	}
}

func TestStreamingToolDeadlineAfterValidPartialSSEIsExecutionGap(t *testing.T) {
	server := interruptedSSEServer(t, validSSEChunk())
	defer server.Close()
	got := probeStreamingTool(context.Background(), streamTestEnvironment(t, server.URL), "test-model")
	if got.Status != result.StatusError || !hasStreamInterruptionSignal(got) {
		t.Fatalf("partial tool stream=%+v", got)
	}
}

func TestWrongStreamContentTypeBeforeDeadlineRemainsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, validSSEChunk())
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	for _, usage := range []bool{false, true} {
		got := probeStreaming(context.Background(), streamTestEnvironment(t, server.URL), "test-model", usage)
		if got.Status != result.StatusFail || !strings.Contains(got.Observation, "Content-Type") || hasStreamInterruptionSignal(got) {
			t.Fatalf("wrong content type (usage=%v) was hidden by interruption: %+v", usage, got)
		}
	}
}

func streamTestEnvironment(t *testing.T, baseURL string) Environment {
	t.Helper()
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	client, err := api.NewClient(baseURL, "test-key", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	return Environment{Client: client, Config: config.Config{Timeout: 250 * time.Millisecond}, Profile: p}
}

func interruptedSSEServer(t *testing.T, payload string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, payload)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
}

func validSSEChunk() string {
	return "data: {\"id\":\"chunk\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n"
}

func hasStreamInterruptionSignal(test result.TestResult) bool {
	for _, signal := range test.Signals {
		if signal.Code == "execution.stream_interrupted" {
			return true
		}
	}
	return false
}
