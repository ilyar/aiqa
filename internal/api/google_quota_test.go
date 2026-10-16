package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsGoogleDailyQuota(t *testing.T) {
	daily := map[string]any{"error": map[string]any{"details": []any{map[string]any{
		"@type":      "type.googleapis.com/google.rpc.QuotaFailure",
		"violations": []any{map[string]any{"quotaId": "GenerateRequestsPerDayPerProjectPerModel-FreeTier", "quotaMetric": "generativelanguage.googleapis.com/generate_content_free_tier_requests"}},
	}}}}
	if !IsGoogleDailyQuota(http.StatusTooManyRequests, daily) {
		t.Fatal("daily QuotaFailure was not recognized")
	}
	for _, body := range []map[string]any{
		{},
		{"error": map[string]any{"details": []any{map[string]any{"@type": "type.googleapis.com/google.rpc.QuotaFailure", "violations": []any{map[string]any{"quotaId": "GenerateRequestsPerMinute", "quotaMetric": "generativelanguage.googleapis.com/generate_content_free_tier_requests"}}}}}},
		{"error": map[string]any{"details": []any{map[string]any{"@type": "type.googleapis.com/google.rpc.QuotaFailure", "violations": []any{map[string]any{"quotaId": "GenerateRequestsPerDay", "quotaMetric": "generativelanguage.googleapis.com/other"}}}}}},
	} {
		if IsGoogleDailyQuota(http.StatusTooManyRequests, body) {
			t.Fatalf("non-daily quota matched: %#v", body)
		}
	}
}

func TestGoogleDailyQuotaDoesNotRetryNativeSDKOrStream(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.QuotaFailure","violations":[{"quotaId":"GenerateRequestsPerDayPerProjectPerModel-FreeTier","quotaMetric":"generativelanguage.googleapis.com/generate_content_free_tier_requests"}]}]}}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "key", "test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	path := "models/opaque:generateContent"
	response, err := client.DoJSONForModel(context.Background(), http.MethodPost, path, map[string]any{"contents": []any{}}, "opaque")
	if err != nil || response.StatusCode != http.StatusTooManyRequests || calls.Load() != 1 {
		t.Fatalf("native calls=%d response=%+v err=%v", calls.Load(), response, err)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/"+path, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	sdkResponse, err := client.PacedHTTPClient("opaque").Do(request)
	if err != nil || sdkResponse.StatusCode != http.StatusTooManyRequests || calls.Load() != 2 {
		t.Fatalf("sdk calls=%d response=%v err=%v", calls.Load(), sdkResponse, err)
	}
	if err := sdkResponse.Body.Close(); err != nil {
		t.Fatal(err)
	}
	stream, err := client.StreamJSONForModel(context.Background(), "models/opaque:streamGenerateContent?alt=sse", map[string]any{"contents": []any{}}, "opaque")
	if err == nil || stream.StatusCode != http.StatusTooManyRequests || calls.Load() != 3 {
		t.Fatalf("stream calls=%d response=%+v err=%v", calls.Load(), stream, err)
	}
}

func TestGoogleMinuteQuotaStillRetries(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.QuotaFailure","violations":[{"quotaId":"GenerateRequestsPerMinutePerProjectPerModel-FreeTier","quotaMetric":"generativelanguage.googleapis.com/generate_content_free_tier_requests"}]}]}}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "key", "test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.DoJSONForModel(context.Background(), http.MethodPost, "models/opaque:generateContent", map[string]any{}, "opaque")
	if err != nil || response.StatusCode != http.StatusTooManyRequests || calls.Load() != 3 {
		t.Fatalf("minute quota calls=%d response=%+v err=%v", calls.Load(), response, err)
	}
}

func TestGoogleDailyQuotaCancelledContextDoesNotDispatch(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, err := NewClient(server.URL, "key", "test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.DoJSONForModel(ctx, http.MethodPost, "models/opaque:generateContent", map[string]any{}, "opaque")
	if !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("cancelled call err=%v calls=%d", err, calls.Load())
	}
}

func TestGoogleDailyQuotaHTTPResponsePreservesBody(t *testing.T) {
	body := []byte(`{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.QuotaFailure","violations":[{"quotaId":"GenerateRequestsPerDayPerProjectPerModel-FreeTier","quotaMetric":"generativelanguage.googleapis.com/generate_content_free_tier_requests"}]}]}}`)
	response := &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(bytes.NewReader(body))}
	if !isGoogleDailyQuotaHTTPResponse(response) {
		t.Fatal("response was not recognized")
	}
	got, err := io.ReadAll(response.Body)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("body changed: %q err=%v", got, err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestGoogleDailyQuotaInspectionPreservesCloseReadErrorAndBound(t *testing.T) {
	daily := []byte(`{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.QuotaFailure","violations":[{"quotaId":"GenerateRequestsPerDayPerProjectPerModel-FreeTier","quotaMetric":"generativelanguage.googleapis.com/generate_content_free_tier_requests"}]}]}}`)
	t.Run("close", func(t *testing.T) {
		want := errors.New("close")
		response := &http.Response{StatusCode: http.StatusTooManyRequests, Body: &quotaTestBody{Reader: bytes.NewReader(daily), closeErr: want}}
		if !isGoogleDailyQuotaHTTPResponse(response) {
			t.Fatal("daily quota was not recognized")
		}
		if err := response.Body.Close(); !errors.Is(err, want) {
			t.Fatalf("close error=%v, want %v", err, want)
		}
	})
	t.Run("read error", func(t *testing.T) {
		want := errors.New("read")
		response := &http.Response{StatusCode: http.StatusTooManyRequests, Body: &quotaTestBody{Reader: &errorAfterReader{data: daily[:12], err: want}}}
		if isGoogleDailyQuotaHTTPResponse(response) {
			t.Fatal("partial body was accepted")
		}
		got, err := io.ReadAll(response.Body)
		if !bytes.Equal(got, daily[:12]) || !errors.Is(err, want) {
			t.Fatalf("replay=%q err=%v", got, err)
		}
	})
	t.Run("oversize", func(t *testing.T) {
		body := append(append([]byte(nil), daily...), bytes.Repeat([]byte("x"), maxGoogleQuotaInspectionBytes)...)
		response := &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(bytes.NewReader(body))}
		if isGoogleDailyQuotaHTTPResponse(response) {
			t.Fatal("oversize body was inspected")
		}
		got, err := io.ReadAll(response.Body)
		if err != nil || !bytes.Equal(got, body) {
			t.Fatalf("oversize replay changed: %d/%d err=%v", len(got), len(body), err)
		}
	})
}

func TestGoogleGeneratePathIsStrict(t *testing.T) {
	for path, want := range map[string]bool{
		"/v1beta/models/opaque:generateContent":       true,
		"models/opaque:streamGenerateContent?alt=sse": true,
		"models/opaque:generateContentElse":           false,
		"other:generateContent":                       false,
		"models/opaque:GenerateContent":               false,
	} {
		if got := isGoogleGeneratePath(path); got != want {
			t.Fatalf("path %q = %v, want %v", path, got, want)
		}
	}
}

type quotaTestBody struct {
	io.Reader
	closeErr error
}

func (b *quotaTestBody) Close() error { return b.closeErr }

type errorAfterReader struct {
	data []byte
	err  error
}

func (r *errorAfterReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}
