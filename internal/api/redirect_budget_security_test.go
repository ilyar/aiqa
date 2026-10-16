package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSDKRedirectChargesEachPhysicalDispatchOnce(t *testing.T) {
	for _, budget := range []int{1, 2} {
		t.Run(string(rune('0'+budget)), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path == "/first" {
					http.Redirect(w, r, "/second", http.StatusFound)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			client, err := NewClient(server.URL, "test-credential", "aiqa-test", time.Nanosecond)
			if err != nil {
				t.Fatal(err)
			}
			request, err := http.NewRequestWithContext(WithAttemptBudget(context.Background(), budget), http.MethodGet, server.URL+"/first", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.PacedHTTPClient("m").Do(request)
			if response != nil {
				response.Body.Close()
			}
			if budget == 1 && !errors.Is(err, ErrWorkLimit) {
				t.Fatalf("error = %v", err)
			}
			if budget == 2 && err != nil {
				t.Fatalf("two dispatch budget failed: %v", err)
			}
			if got := calls.Load(); got != int32(budget) {
				t.Fatalf("dispatches = %d, want %d", got, budget)
			}
		})
	}
}
