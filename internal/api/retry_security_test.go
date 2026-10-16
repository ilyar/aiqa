package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/logging"
)

func TestRetryAfterClampsDistantDateAndOverflow(t *testing.T) {
	date := time.Now().Add(24 * time.Hour).Format(http.TimeFormat)
	for _, value := range []string{date, "999999999999999999999999999999"} {
		if got := retryAfter(http.Header{"Retry-After": []string{value}}); got != maxRetryAfter {
			t.Fatalf("Retry-After %q=%s, want %s", value, got, maxRetryAfter)
		}
	}
}

func TestRetryAfterClipIsTracedAndDeadlineCancelsCooldown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "999999999999999999999999")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "key", "aiqa-test", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	logger := logging.New(4)
	client.SetLogger(logger)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = client.DoRaw(ctx, http.MethodGet, "models", nil, "application/json")
	if err == nil || time.Since(started) > time.Second {
		t.Fatalf("err=%v elapsed=%s", err, time.Since(started))
	}
	if !strings.Contains(strings.Join(logger.Lines(), "\n"), `"event":"http.retry_after.clipped"`) {
		t.Fatalf("clip trace missing: %s", strings.Join(logger.Lines(), "\n"))
	}
}
