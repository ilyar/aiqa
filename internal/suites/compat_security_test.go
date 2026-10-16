package suites

import (
	"context"
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

func TestStreamingWorkLimitIsExecutionGapAfterHTTP200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + strings.Repeat("x", 70<<10) + "\n\n"))
	}))
	defer server.Close()
	p, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	client, err := api.NewClient(server.URL, "test-key", "aiqa-test", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	got := probeStreaming(context.Background(), Environment{Client: client, Config: config.Config{Timeout: time.Second}, Profile: p}, "test-model", false)
	if got.Status != result.StatusError || !strings.Contains(got.Summary, "work limit") {
		t.Fatalf("result=%+v", got)
	}
	if len(got.Signals) != 1 || got.Signals[0].Code != "execution.work_limit" {
		t.Fatalf("signals=%+v", got.Signals)
	}
}
