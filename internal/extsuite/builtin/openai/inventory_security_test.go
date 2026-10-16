package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/extsuite"
	"github.com/ilyar/aiqa/internal/result"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func TestSDKModelListInventoryLimitIsExecutionError(t *testing.T) {
	body := `{"data":[` + strings.Repeat(`{"id":"m"},`, 4096) + `{"id":"m"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	transport, err := api.NewClient(server.URL, "key", "aiqa-test", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	client := openaisdk.NewClient(option.WithBaseURL(server.URL+"/"), option.WithAPIKey("key"), option.WithHTTPClient(transport.PacedHTTPClient("")), option.WithMaxRetries(0))
	event := runSuite(context.Background(), &client, extsuite.RunRequest{RunID: "run", Subject: result.SubjectRef{ID: "provider"}}, "models.list")
	if event.Outcome != result.ExtensionOutcomeExecutionError || len(event.Signals) != 1 || event.Signals[0].Code != "execution.work_limit" {
		t.Fatalf("event=%+v", event)
	}
}

func TestSDKModelListHonorsOperatorInventoryLimits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		runtime extsuite.RuntimeServices
		want    result.ExtensionOutcome
	}{
		{"valid", extsuite.RuntimeServices{MaxCatalogModels: 2, InventoryByteBudget: 1024}, result.ExtensionOutcomePass},
		{"lowered_model_limit", extsuite.RuntimeServices{MaxCatalogModels: 1, InventoryByteBudget: 1024}, result.ExtensionOutcomeExecutionError},
		{"lowered_byte_limit", extsuite.RuntimeServices{MaxCatalogModels: 2, InventoryByteBudget: 16}, result.ExtensionOutcomeExecutionError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[{"id":"m1"},{"id":"m2"}]}`))
			}))
			defer server.Close()
			transport, err := api.NewClient(server.URL, "key", "aiqa-test", time.Nanosecond)
			if err != nil {
				t.Fatal(err)
			}
			client := openaisdk.NewClient(option.WithBaseURL(server.URL+"/"), option.WithAPIKey("key"), option.WithHTTPClient(transport.PacedHTTPClient("")), option.WithMaxRetries(0))
			event := runSuite(context.Background(), &client, extsuite.RunRequest{Runtime: tc.runtime, RunID: "run", Subject: result.SubjectRef{ID: "provider"}}, "models.list")
			if event.Outcome != tc.want {
				t.Fatalf("outcome=%s, want %s: %+v", event.Outcome, tc.want, event)
			}
			if calls.Load() != 1 {
				t.Fatalf("provider requests=%d, want 1", calls.Load())
			}
		})
	}
}
