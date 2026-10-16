package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/telemetry"
	"github.com/ilyar/aiqa/internal/traceverify"
)

func TestPacingObserverReportsOnlyActualWaitsAcrossTransportPaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	type pacingEvent struct {
		target   string
		waiting  bool
		duration time.Duration
	}
	for _, test := range []struct {
		name string
		run  func(*Client) error
	}{
		{"raw", func(client *Client) error {
			_, err := client.DoRaw(context.Background(), http.MethodGet, "raw", nil, "application/json")
			return err
		}},
		{"stream", func(client *Client) error {
			_, err := client.StreamJSON(context.Background(), "stream", map[string]any{})
			return err
		}},
		{"sdk", func(client *Client) error {
			request, err := http.NewRequest(http.MethodGet, server.URL+"/sdk", nil)
			if err != nil {
				return err
			}
			response, err := client.PacedHTTPClient("model").Do(request)
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewClient(server.URL, "secret", "aiqa-test", 25*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			client.SetLogFields(logging.Fields{"target": "fixture"})
			if err := test.run(client); err != nil {
				t.Fatal(err)
			} // consumes the immediate first reservation

			var mu sync.Mutex
			var events []pacingEvent
			client.SetPacingObserver(func(target string, waiting bool, duration time.Duration) {
				mu.Lock()
				events = append(events, pacingEvent{target: target, waiting: waiting, duration: duration})
				mu.Unlock()
			})
			if err := test.run(client.CloneWithKey("other-secret")); err != nil {
				t.Fatal(err)
			}

			mu.Lock()
			defer mu.Unlock()
			if len(events) != 2 || !events[0].waiting || events[1].waiting {
				t.Fatalf("pacing lifecycle=%+v, want one begin/end pair", events)
			}
			if events[0].target != "fixture" || events[0].duration <= 0 || events[1].duration <= 0 {
				t.Fatalf("unsafe or non-wait pacing event=%+v", events)
			}
		})
	}
}

func TestArtifactCreationEndpointsDoNotRetryAfterDispatch(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(*Client, string) error
	}{
		{
			name: "native",
			run: func(client *Client, path string) error {
				_, err := client.DoJSON(context.Background(), http.MethodPost, path, map[string]any{"model": "image-model", "prompt": "fixture"})
				return err
			},
		},
		{
			name: "sdk",
			run: func(client *Client, path string) error {
				request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, client.BaseURL()+"/"+path, strings.NewReader(`{"model":"image-model"}`))
				if err != nil {
					return err
				}
				response, err := client.PacedHTTPClient("image-model").Do(request)
				if response != nil && response.Body != nil {
					_ = response.Body.Close()
				}
				return err
			},
		},
	} {
		for _, path := range []string{"images/generations", "v1/images/generations", "proxy/v1/images/generations"} {
			t.Run(test.name+"/"+path, func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					calls.Add(1)
					w.WriteHeader(http.StatusServiceUnavailable)
				}))
				defer server.Close()
				client, err := NewClient(server.URL, "secret", "aiqa-test", time.Nanosecond)
				if err != nil {
					t.Fatal(err)
				}
				if err := test.run(client, path); err != nil {
					t.Fatal(err)
				}
				if got := calls.Load(); got != 1 {
					t.Fatalf("provider attempts=%d, want 1", got)
				}
			})
		}
	}
}

func TestAttemptBudgetAndRetryAfterAreBounded(t *testing.T) {
	if got := retryAfter(http.Header{"Retry-After": []string{"999999999999"}}); got != maxRetryAfter {
		t.Fatalf("retry-after=%s, want %s", got, maxRetryAfter)
	}
	ctx := WithAttemptBudget(context.Background(), 1)
	if err := consumeAttempt(ctx); err != nil {
		t.Fatal(err)
	}
	if err := consumeAttempt(ctx); !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("budget error=%v", err)
	}
}

func TestAttemptBudgetCountsNativeRedirectDispatches(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/first" {
			http.Redirect(w, r, "/second", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "secret", "aiqa-test", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.DoRaw(WithAttemptBudget(context.Background(), 1), http.MethodGet, "first", nil, "application/json")
	if !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("redirect budget error=%v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("physical dispatches=%d, want 1", got)
	}
}

func TestArtifactCreationRejectsRedirectBeforeSecondDispatch(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Redirect(w, r, "/v1/images/generations/redirected", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "secret", "aiqa-test", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.DoJSON(context.Background(), http.MethodPost, "v1/images/generations", map[string]any{"model": "m"})
	if err == nil {
		t.Fatal("artifact redirect unexpectedly succeeded")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("artifact dispatches=%d, want 1", got)
	}
}

func TestPacingObserverClosesCancelledWait(t *testing.T) {
	client, err := NewClient("https://example.test", "secret", "aiqa-test", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.SetLogFields(logging.Fields{"target": "fixture"})
	if err := client.pacer.wait(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	client.pacer.release("")
	var events []struct {
		waiting  bool
		duration time.Duration
	}
	client.SetPacingObserver(func(_ string, waiting bool, duration time.Duration) {
		events = append(events, struct {
			waiting  bool
			duration time.Duration
		}{waiting, duration})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if err := client.waitForPacing(ctx, ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait error=%v, want deadline exceeded", err)
	}
	if len(events) != 2 || !events[0].waiting || events[1].waiting || events[1].duration <= 0 {
		t.Fatalf("cancelled pacing lifecycle=%+v", events)
	}
}

func TestRequestTimeoutStartsAfterPacingAdmission(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	for _, test := range []struct {
		name string
		run  func(context.Context, *Client) error
	}{
		{"raw", func(ctx context.Context, client *Client) error {
			_, err := client.DoRaw(ctx, http.MethodGet, "raw", nil, "application/json")
			return err
		}},
		{"stream", func(ctx context.Context, client *Client) error {
			_, err := client.StreamJSON(ctx, "stream", map[string]any{})
			return err
		}},
		{"sdk", func(ctx context.Context, client *Client) error {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/sdk", nil)
			if err != nil {
				return err
			}
			resp, err := client.PacedHTTPClient("model").Do(req)
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewClient(server.URL, "secret", "aiqa-test", 40*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			if err := test.run(context.Background(), client); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := WithRequestTimeout(context.Background(), 5*time.Millisecond)
			defer cancel()
			started := time.Now()
			if err := test.run(ctx, client); err != nil {
				t.Fatalf("request cancelled while pacing: %v", err)
			}
			if elapsed := time.Since(started); elapsed < 20*time.Millisecond {
				t.Fatalf("request did not wait for pacing: %s", elapsed)
			}
		})
	}
}

func TestRequestTimeoutLimitsPhysicalAttempt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "secret", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := WithRequestTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err = client.DoRaw(ctx, http.MethodGet, "slow", nil, "application/json")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("attempt error=%v, want deadline exceeded", err)
	}
}

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

func TestTransportTraceIsStructuredAndNeverIncludesQueryOrCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-request-id", "req-test")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	logger := logging.New(4)
	client, err := NewClient(server.URL, "super-secret", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	client.SetLogger(logger)
	client.SetLogFields(logging.Fields{"target": "openai", "protocol": "openai"})
	ctx := logging.Scope(context.Background(), "", "gpt-test", "compat", "chat.basic", "probe-1")
	if _, err := client.DoJSON(ctx, http.MethodGet, "models?api_key=leak", nil); err != nil {
		t.Fatal(err)
	}
	trace := strings.Join(logger.Lines(), "\n")
	for _, forbidden := range []string{"super-secret", "api_key=leak", server.URL} {
		if strings.Contains(trace, forbidden) {
			t.Fatalf("trace leaked %q: %s", forbidden, trace)
		}
	}
	for _, required := range []string{"\"event\":\"http.attempt.begin\"", "\"event\":\"http.attempt.result\"", "\"target\":\"openai\"", "\"model\":\"gpt-test\"", "\"probe\":\"chat.basic\"", "\"path\":\"/models\""} {
		if !strings.Contains(trace, required) {
			t.Fatalf("trace does not contain %s: %s", required, trace)
		}
	}
	if !strings.Contains(trace, `"provider_request_id":"req-test"`) {
		t.Fatalf("trace is missing provider request ID fingerprint: %s", trace)
	}
	if got := strings.Count(trace, `"event":"diagnostic.response_shape"`); got != 1 {
		t.Fatalf("terminal native request emitted %d diagnostic fingerprints, want 1: %s", got, trace)
	}
}

func TestTransportDiagnosticErrorShapeNeverRetainsProviderText(t *testing.T) {
	const secret = "provider-error-text-must-not-leak"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"` + secret + `","details":[{"nested":"` + secret + `"}]}}`))
	}))
	defer server.Close()
	logger := logging.New(4)
	client, err := NewClient(server.URL, "test", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	client.SetLogger(logger)
	response, err := client.DoJSON(context.Background(), http.MethodPost, "messages", map[string]any{"model": "test", "messages": []any{map[string]any{"content": secret}}})
	if err != nil || response.StatusCode != http.StatusBadRequest {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	trace := strings.Join(logger.Lines(), "\n")
	if strings.Contains(trace, secret) {
		t.Fatalf("diagnostic leaked provider error text: %s", trace)
	}
	for _, required := range []string{`"event":"diagnostic.response_shape"`, `"error_present":true`, `"error_code":400`, `"error_status":"invalid_argument"`, `"error_shape_keys":"code,details,message,status"`} {
		if !strings.Contains(trace, required) {
			t.Fatalf("diagnostic missing %s: %s", required, trace)
		}
	}
}

func TestStreamTraceRecordsEventShapeWithoutPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.delta\ndata: {\"id\":\"evt_1\",\"choices\":[{\"delta\":{\"content\":\"secret model output\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	logger := logging.New(4)
	client, err := NewClient(server.URL, "secret", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	client.SetLogger(logger)
	if _, err := client.StreamJSONForModel(context.Background(), "chat/completions?credential=no", map[string]any{"model": "gpt-test"}, "gpt-test"); err != nil {
		t.Fatal(err)
	}
	trace := strings.Join(logger.Lines(), "\n")
	for _, forbidden := range []string{"secret model output", "credential=no", server.URL} {
		if strings.Contains(trace, forbidden) {
			t.Fatalf("stream trace leaked %q: %s", forbidden, trace)
		}
	}
	for _, required := range []string{"\"event\":\"http.sse.event\"", "\"data_kind\":\"json\"", "\"json_key_count\":2", "\"event\":\"diagnostic.response_shape\"", "\"sse_terminal_markers\":\"done,stream_done\""} {
		if !strings.Contains(trace, required) {
			t.Fatalf("stream trace does not contain %s: %s", required, trace)
		}
	}
}

func TestTelemetryCorrelatesNativeAttemptsAndNeverPropagates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("traceparent") != "" || r.Header.Get("tracestate") != "" || r.Header.Get("baggage") != "" {
			t.Fatalf("unexpected external trace propagation: %#v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	logger := logging.New(4)
	provider := telemetry.New(logger)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	client, err := NewClient(server.URL, "secret", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	client.SetLogger(logger)
	client.SetTelemetry(provider)
	response, err := client.DoJSON(context.Background(), http.MethodGet, "models", nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.TraceID == "" {
		t.Fatal("native response missing logical operation ID")
	}
	trace := strings.Join(logger.Lines(), "\n")
	for _, event := range []string{"http.logical_request.start", "http.attempt.start", "http.attempt.end", "http.logical_request.end"} {
		if !strings.Contains(trace, `"event":"`+event+`"`) {
			t.Fatalf("telemetry missing %s: %s", event, trace)
		}
	}
	if !strings.Contains(trace, `"operation_id":"`+response.TraceID+`"`) {
		t.Fatalf("trace missing response operation ID %q: %s", response.TraceID, trace)
	}
}

func TestTelemetryPropagatesOnlyToExactAllowlistedOrigin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("traceparent") == "" {
			t.Fatal("allowlisted controlled origin did not receive traceparent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	logger := logging.New(4)
	provider := telemetry.New(logger)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	client, err := NewClient(server.URL, "secret", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	client.SetTelemetry(provider)
	client.SetOTelPropagationAllowlist([]string{server.URL})
	ctx, op := provider.Start(context.Background(), "probe", nil)
	defer op.End("completed", nil)
	if _, err := client.DoJSON(ctx, http.MethodGet, "models", nil); err != nil {
		t.Fatal(err)
	}
}

func TestTelemetryPropagationIsRemovedOnCrossOriginRedirect(t *testing.T) {
	blocked := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.Header.Get("traceparent") != "" || r.Header.Get("tracestate") != "" || r.Header.Get("baggage") != "" {
			t.Fatalf("redirect leaked propagation to blocked origin: %#v", r.Header)
		}
	}))
	defer blocked.Close()
	allowed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("traceparent") == "" {
			t.Fatal("allowlisted origin did not receive traceparent")
		}
		http.Redirect(w, r, blocked.URL, http.StatusFound)
	}))
	defer allowed.Close()
	logger := logging.New(4)
	provider := telemetry.New(logger)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	client, err := NewClient(allowed.URL, "secret", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	client.SetLogger(logger)
	client.SetTelemetry(provider)
	client.SetOTelPropagationAllowlist([]string{allowed.URL})
	ctx, op := provider.Start(context.Background(), "probe", nil)
	defer op.End("completed", nil)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, allowed.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.PacedHTTPClient("model").Do(req)
	if err == nil {
		t.Fatalf("cross-origin redirect error=%v response=%v, want rejection", err, resp)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	// Native transport has its own http.Client and must apply exactly the same
	// strict configured-origin rule as the SDK adapter.
	if _, err := client.DoJSON(ctx, http.MethodGet, "", nil); err == nil {
		t.Fatal("native cross-origin redirect unexpectedly succeeded")
	}
}

func TestTelemetryEndsSDKRequestWhenBodyCloses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	logger := logging.New(4)
	provider := telemetry.New(logger)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	client, err := NewClient(server.URL, "secret", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	client.SetLogger(logger)
	client.SetTelemetry(provider)
	request, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	response, err := client.PacedHTTPClient("gpt-test").Do(request)
	if err != nil {
		t.Fatal(err)
	}
	before := strings.Join(logger.Lines(), "\n")
	if strings.Contains(before, `"event":"sdk.logical_request.end"`) || strings.Contains(before, `"event":"sdk.probe.end"`) {
		t.Fatalf("SDK operation or its probe ended before body close: %s", before)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	after := strings.Join(logger.Lines(), "\n")
	if !strings.Contains(after, `"event":"sdk.logical_request.end"`) || !strings.Contains(after, `"event":"sdk.http_attempt.end"`) || !strings.Contains(after, `"event":"sdk.probe.end"`) {
		t.Fatalf("SDK lifecycle is incomplete: %s", after)
	}
	assertSDKBodyCloseOrder(t, logger.Lines())
}

func TestTelemetryEndsSDKRequestWithCloseError(t *testing.T) {
	logger := logging.New(4)
	provider := telemetry.New(logger)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	client, err := NewClient("https://example.test", "secret", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	client.SetLogger(logger)
	client.SetTelemetry(provider)
	client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: closeErrorBody{Reader: strings.NewReader(`{}`)}}, nil
	})
	request, _ := http.NewRequest(http.MethodGet, "https://example.test/models", nil)
	response, err := client.PacedHTTPClient("gpt-test").Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err == nil {
		t.Fatal("Close unexpectedly succeeded")
	}
	trace := strings.Join(logger.Lines(), "\n")
	if strings.Count(trace, `"outcome":"close_error"`) != 2 || !strings.Contains(trace, `"error_present":true`) {
		t.Fatalf("close error lifecycle missing: %s", trace)
	}
	assertSDKBodyCloseOrder(t, logger.Lines())
}

func TestSDKCloseErrorTraceFormsVerifiableForensicGraph(t *testing.T) {
	logger := logging.New(4)
	provider := telemetry.New(logger)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	client, err := NewClient("https://example.test", "secret", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	client.SetLogger(logger)
	client.SetTelemetry(provider)
	client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: closeErrorBody{Reader: strings.NewReader(`{}`)}}, nil
	})
	ctx := logging.WithLogger(context.Background(), logger)
	ctx, run := provider.Start(ctx, "run", logging.Fields{"run_id": "sdk-close-fixture"})
	ctx = logging.Scope(ctx, "fixture", "fixture-model", "sdk", "", "")
	ctx, target := provider.Start(ctx, "target", logging.Fields{"target": "fixture", "protocol": "openai"})
	ctx, model := provider.Start(ctx, "model", logging.Fields{"target": "fixture", "protocol": "openai", "model": "fixture-model"})
	ctx, suite := provider.Start(ctx, "suite", logging.Fields{"suite": "sdk", "source": "builtin"})
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.test/models", nil)
	response, err := client.PacedHTTPClient("fixture-model").Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err == nil {
		t.Fatal("Close unexpectedly succeeded")
	}
	suite.End("error", logging.Fields{"status": "error", "error_present": true, "error_kind": "close_error"})
	model.End("error", logging.Fields{"status": "error", "error_present": true})
	target.End("error", logging.Fields{"status": "error", "error_present": true})
	run.End("error", logging.Fields{"status": "error", "error_present": true})
	verdict := traceverify.Verify(strings.NewReader(strings.Join(logger.Lines(), "\n") + "\n"))
	if verdict.Status != traceverify.Pass || verdict.CheckedHTTPAttempts != 1 {
		t.Fatalf("SDK close-error forensic verdict=%+v trace=%s", verdict, strings.Join(logger.Lines(), "\n"))
	}
}

type closeErrorBody struct{ io.Reader }

func (closeErrorBody) Close() error { return errors.New("fixture close error") }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func TestTelemetryTraceCapturesRetryAndCancellationLifecycles(t *testing.T) {
	t.Run("retry", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) == 1 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer server.Close()
		logger := logging.New(4)
		provider := telemetry.New(logger)
		t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
		client, err := NewClient(server.URL, "secret", "aiqa-test", time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		client.SetTelemetry(provider)
		client.SetLogger(logger)
		if _, err := client.DoJSON(context.Background(), http.MethodGet, "models", nil); err != nil {
			t.Fatal(err)
		}
		trace := strings.Join(logger.Lines(), "\n")
		if calls.Load() != 2 || strings.Count(trace, `"event":"http.attempt.start"`) != 2 || !strings.Contains(trace, `"event":"http.retry.scheduled"`) || !strings.Contains(trace, `"event":"http.logical_request.end"`) {
			t.Fatalf("retry lifecycle is incomplete: calls=%d trace=%s", calls.Load(), trace)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		defer server.Close()
		logger := logging.New(4)
		provider := telemetry.New(logger)
		t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
		client, err := NewClient(server.URL, "secret", "aiqa-test", time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		client.SetTelemetry(provider)
		client.SetLogger(logger)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if _, err := client.DoJSON(ctx, http.MethodGet, "models", nil); err == nil {
			t.Fatal("cancelled request unexpectedly succeeded")
		}
		trace := strings.Join(logger.Lines(), "\n")
		if !strings.Contains(trace, `"event":"http.retry.cancelled"`) || !strings.Contains(trace, `"event":"http.logical_request.end"`) {
			t.Fatalf("cancellation lifecycle is incomplete: %s", trace)
		}
	})
}

func TestRetryTraceFormsVerifiableForensicGraph(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	logger := logging.New(4)
	provider := telemetry.New(logger)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	client, err := NewClient(server.URL, "secret", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	client.SetLogger(logger)
	client.SetTelemetry(provider)
	ctx := logging.WithLogger(context.Background(), logger)
	ctx, run := provider.Start(ctx, "run", logging.Fields{"run_id": "fixture-run"})
	ctx = logging.Scope(ctx, "fixture", "fixture-model", "compat", "fixture.probe", "")
	ctx, target := provider.Start(ctx, "target", logging.Fields{"target": "fixture", "protocol": "openai"})
	ctx, model := provider.Start(ctx, "model", logging.Fields{"target": "fixture", "protocol": "openai", "model": "fixture-model"})
	ctx, suite := provider.Start(ctx, "suite", logging.Fields{"suite": "compat", "source": "native"})
	ctx, probe := provider.Start(ctx, "probe", logging.Fields{"probe": "fixture.probe", "source": "native"})
	if _, err := client.DoJSON(ctx, http.MethodGet, "models", nil); err != nil {
		t.Fatal(err)
	}
	probe.Event(logging.Trace, "validator", logging.Fields{"status": "pass"})
	probe.End("pass", logging.Fields{"status": "pass"})
	suite.End("pass", logging.Fields{"status": "pass"})
	model.End("pass", logging.Fields{"status": "pass"})
	target.End("pass", logging.Fields{"status": "pass"})
	run.End("pass", logging.Fields{"status": "pass"})
	verdict := traceverify.Verify(strings.NewReader(strings.Join(logger.Lines(), "\n") + "\n"))
	if verdict.Status != traceverify.Pass || verdict.CheckedHTTPAttempts != 2 || calls.Load() != 2 {
		t.Fatalf("retry forensic verdict=%+v calls=%d trace=%s", verdict, calls.Load(), strings.Join(logger.Lines(), "\n"))
	}
}

func TestCancellationTraceFormsVerifiableForensicGraph(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	logger := logging.New(4)
	provider := telemetry.New(logger)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	client, err := NewClient(server.URL, "secret", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	client.SetLogger(logger)
	client.SetTelemetry(provider)
	ctx := logging.WithLogger(context.Background(), logger)
	ctx, run := provider.Start(ctx, "run", logging.Fields{"run_id": "cancellation-fixture"})
	ctx = logging.Scope(ctx, "fixture", "fixture-model", "compat", "fixture.cancel", "")
	ctx, target := provider.Start(ctx, "target", logging.Fields{"target": "fixture", "protocol": "openai"})
	ctx, model := provider.Start(ctx, "model", logging.Fields{"target": "fixture", "protocol": "openai", "model": "fixture-model"})
	ctx, suite := provider.Start(ctx, "suite", logging.Fields{"suite": "compat", "source": "native"})
	ctx, probe := provider.Start(ctx, "probe", logging.Fields{"probe": "fixture.cancel", "source": "native"})
	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := client.DoJSON(requestCtx, http.MethodGet, "models", nil); err == nil {
		t.Fatal("cancelled request unexpectedly succeeded")
	}
	probe.Event(logging.Trace, "validator", logging.Fields{"status": "error", "error_present": true, "error_kind": "context_canceled"})
	probe.End("error", logging.Fields{"status": "error", "error_present": true})
	suite.End("error", logging.Fields{"status": "error", "error_present": true})
	model.End("error", logging.Fields{"status": "error", "error_present": true})
	target.End("error", logging.Fields{"status": "error", "error_present": true})
	run.End("error", logging.Fields{"status": "error", "error_present": true})
	verdict := traceverify.Verify(strings.NewReader(strings.Join(logger.Lines(), "\n") + "\n"))
	if verdict.Status != traceverify.Pass || verdict.CheckedHTTPAttempts != 1 {
		t.Fatalf("cancellation forensic verdict=%+v trace=%s", verdict, strings.Join(logger.Lines(), "\n"))
	}
}

func TestTelemetryTraceCapturesSSEReadErrorLifecycle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Deliberately truncate a declared response. The client must record a
		// read_error terminal rather than silently treating it as an SSE EOF.
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte("event: response.delta\ndata: {\"id\":\"only-shape\"}\n\n"))
	}))
	defer server.Close()
	logger := logging.New(4)
	provider := telemetry.New(logger)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	client, err := NewClient(server.URL, "secret", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	client.SetTelemetry(provider)
	client.SetLogger(logger)
	if _, err := client.StreamJSON(context.Background(), "chat/completions", map[string]any{"model": "test"}); err == nil {
		t.Fatal("truncated SSE response unexpectedly succeeded")
	}
	trace := strings.Join(logger.Lines(), "\n")
	if !strings.Contains(trace, `"event":"http.sse.event"`) || !strings.Contains(trace, `"event":"http.stream_logical_request.end"`) || !strings.Contains(trace, `"outcome":"read_error"`) {
		t.Fatalf("SSE read-error lifecycle is incomplete: %s", trace)
	}
}

func TestSSEReadErrorTraceFormsVerifiableForensicGraph(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte("event: response.delta\ndata: {\"id\":\"only-shape\"}\n\n"))
	}))
	defer server.Close()
	logger := logging.New(4)
	provider := telemetry.New(logger)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	client, err := NewClient(server.URL, "secret", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	client.SetLogger(logger)
	client.SetTelemetry(provider)
	ctx := logging.WithLogger(context.Background(), logger)
	ctx, run := provider.Start(ctx, "run", logging.Fields{"run_id": "sse-read-error-fixture"})
	ctx = logging.Scope(ctx, "fixture", "fixture-model", "compat", "fixture.stream", "")
	ctx, target := provider.Start(ctx, "target", logging.Fields{"target": "fixture", "protocol": "openai"})
	ctx, model := provider.Start(ctx, "model", logging.Fields{"target": "fixture", "protocol": "openai", "model": "fixture-model"})
	ctx, suite := provider.Start(ctx, "suite", logging.Fields{"suite": "compat", "source": "native"})
	ctx, probe := provider.Start(ctx, "probe", logging.Fields{"probe": "fixture.stream", "source": "native"})
	if _, err := client.StreamJSON(ctx, "chat/completions", map[string]any{"model": "fixture-model"}); err == nil {
		t.Fatal("truncated SSE response unexpectedly succeeded")
	}
	probe.Event(logging.Trace, "validator", logging.Fields{"status": "error", "error_present": true, "error_kind": "sse_read_error"})
	probe.End("error", logging.Fields{"status": "error", "error_present": true})
	suite.End("error", logging.Fields{"status": "error", "error_present": true})
	model.End("error", logging.Fields{"status": "error", "error_present": true})
	target.End("error", logging.Fields{"status": "error", "error_present": true})
	run.End("error", logging.Fields{"status": "error", "error_present": true})
	verdict := traceverify.Verify(strings.NewReader(strings.Join(logger.Lines(), "\n") + "\n"))
	if verdict.Status != traceverify.Pass || verdict.CheckedHTTPAttempts != 1 {
		t.Fatalf("SSE read-error forensic verdict=%+v trace=%s", verdict, strings.Join(logger.Lines(), "\n"))
	}
}

// assertSDKBodyCloseOrder checks the important ownership contract rather than
// mere event presence: the fallback SDK probe must remain live until its
// response body is closed, after its logical request has ended.
func assertSDKBodyCloseOrder(t *testing.T, lines []string) {
	t.Helper()
	var logicalEnd, probeEnd int
	var logicalSpan, probeSpan string
	for index, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode trace line %d: %v", index+1, err)
		}
		event, _ := record["event"].(string)
		switch event {
		case "sdk.logical_request.start":
			logicalSpan, _ = record["span_id"].(string)
			probeSpan, _ = record["parent_span_id"].(string)
		case "sdk.logical_request.end":
			logicalEnd = index + 1
		case "sdk.probe.end":
			probeEnd = index + 1
		}
	}
	if logicalSpan == "" || probeSpan == "" || logicalEnd == 0 || probeEnd == 0 || logicalEnd >= probeEnd {
		t.Fatalf("invalid SDK body-close order: logical=%q probe=%q logical_end=%d probe_end=%d", logicalSpan, probeSpan, logicalEnd, probeEnd)
	}
}
