package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestPacedSDKTransportOwnsRetriesAndCountsExhaustedRateLimit(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down"}}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "test", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	client.SetConcurrency(4)
	request, _ := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader([]byte(`{}`)))
	response, err := client.PacedHTTPClient("model-a").Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	stats := client.PacingStats()
	if calls.Load() != 3 || stats.RateLimited != 3 {
		t.Fatalf("calls=%d rate_limited=%d, want 3/3", calls.Load(), stats.RateLimited)
	}
	if stats.LogicalRequests != 1 || stats.PhysicalAttempts != 3 || stats.RetryAttempts != 2 || stats.RetriesExhausted != 1 || stats.SuccessfulRequests != 0 || stats.LatencySamples != nil {
		t.Fatalf("exhausted SDK telemetry=%+v", stats)
	}
	if got := client.PacingConstraints().SafeConcurrency; got != 1 {
		t.Fatalf("current adaptive concurrency=%d after 429, want 1", got)
	}
}

func TestDoRawStopsAfterThreeRateLimitedAttempts(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down"}}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.DoJSON(context.Background(), http.MethodGet, "models", nil)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status=%d, want 429", resp.StatusCode)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("requests=%d, want exactly 3 rate-limited attempts", got)
	}
	stats := client.PacingStats()
	if got := stats.RateLimited; got != 3 {
		t.Fatalf("rate-limited stats=%d, want 3", got)
	}
	if stats.LogicalRequests != 1 || stats.PhysicalAttempts != 3 || stats.RetryAttempts != 2 || stats.RetriesExhausted != 1 || stats.SuccessfulRequests != 0 || stats.LatencySamples != nil || stats.SafeRate != 0 || stats.SafeConcurrency != 0 {
		t.Fatalf("exhausted raw telemetry=%+v", stats)
	}
}

func TestDoRawTelemetryCountsRecoveredRetryAndUniqueLatency(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"temporary"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.DoJSON(context.Background(), http.MethodGet, "models", nil); err != nil {
		t.Fatal(err)
	}
	stats := client.PacingStats()
	if stats.LogicalRequests != 1 || stats.PhysicalAttempts != 2 || stats.SuccessfulRequests != 1 || stats.RetryAttempts != 1 || stats.RetriesRecovered != 1 || stats.RetriesExhausted != 0 || stats.RateLimited != 0 {
		t.Fatalf("recovered raw telemetry=%+v", stats)
	}
	if len(stats.LatencySamples) != 1 || stats.Backoff <= 0 || stats.SafeRate <= 0 || stats.SafeConcurrency <= 0 {
		t.Fatalf("recovered latency/pacer telemetry=%+v", stats)
	}
}

func TestDoRawRetriesAnthropicOverloadUnlessServerOptsOut(t *testing.T) {
	for _, test := range []struct {
		name      string
		optOut    bool
		wantCalls int32
	}{{name: "retry", wantCalls: 3}, {name: "x-should-retry-false", optOut: true, wantCalls: 1}} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if test.optOut {
					w.Header().Set("x-should-retry", "false")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(529)
				_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`))
			}))
			defer server.Close()
			client, err := NewClientWithHeaders(server.URL, "aiqa-test", time.Millisecond, http.Header{"x-api-key": []string{"key"}})
			if err != nil {
				t.Fatal(err)
			}
			resp, err := client.DoJSON(context.Background(), http.MethodPost, "v1/messages", map[string]any{"model": "m"})
			if err != nil || resp.StatusCode != 529 {
				t.Fatalf("status=%d err=%v", resp.StatusCode, err)
			}
			if got := calls.Load(); got != test.wantCalls {
				t.Fatalf("calls=%d, want %d", got, test.wantCalls)
			}
		})
	}
}

func TestModelScopedRequestsAttributePacingToURLAddressedModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "test", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.DoJSONForModel(context.Background(), http.MethodGet, "models/gemini-test", nil, "gemini-test"); err != nil {
		t.Fatal(err)
	}
	stats := client.PacingStatsForModel("gemini-test")
	if stats.LogicalRequests != 1 || stats.PhysicalAttempts != 1 || stats.SuccessfulRequests != 1 || len(stats.LatencySamples) != 1 {
		t.Fatalf("model-scoped pacing telemetry=%+v", stats)
	}
}

func TestDeltaPacingStatsOmitsSafeStateWhenOperationHasNoSuccess(t *testing.T) {
	before := PacingStats{SuccessfulRequests: 2, SafeRate: 4, SafeConcurrency: 2, LatencySamples: []int64{10, 20}}
	after := PacingStats{SuccessfulRequests: 2, SafeRate: 1, SafeConcurrency: 1, LatencySamples: []int64{10, 20}}
	got := DeltaPacingStats(before, after)
	if got.SuccessfulRequests != 0 || got.SafeRate != 0 || got.SafeConcurrency != 0 || len(got.LatencySamples) != 0 {
		t.Fatalf("delta with no successful operation retained positive safe state: %+v", got)
	}
}
