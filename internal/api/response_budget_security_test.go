package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResponseBudgetStopsBeforeJSONDecode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}` + strings.Repeat(" ", 4096)))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "test-credential", "aiqa-test", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.DoJSON(WithResponseBodyLimit(context.Background(), 32), http.MethodGet, "models", nil)
	if !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("error = %v", err)
	}
	if len(response.Body) != 33 || response.JSON != nil {
		t.Fatalf("body bytes=%d parsed=%v; oversized response was retained/decoded", len(response.Body), response.JSON != nil)
	}
}
