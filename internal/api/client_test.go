package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

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
	if got := client.PacingStats().RateLimited; got != 3 {
		t.Fatalf("rate-limited stats=%d, want 3", got)
	}
}
