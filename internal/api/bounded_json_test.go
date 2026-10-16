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

func boundedJSONClient(t *testing.T, body string) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	c, err := NewClient(server.URL, "key", "aiqa-test", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestJSONCollectionLimitRejectsInventoryBeforeFullDecode(t *testing.T) {
	for _, field := range []string{"data", "models"} {
		t.Run(field, func(t *testing.T) {
			body := `{"` + field + `":[` + strings.Repeat(`{"id":"m"},`, 4096) + `{"id":"m"}]}`
			_, err := boundedJSONClient(t, body).DoRaw(WithJSONCollectionLimit(context.Background(), 4096), http.MethodGet, "models", nil, "application/json")
			if !errors.Is(err, ErrWorkLimit) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestJSONCollectionLimitAllowsMultiFieldModelAndNormalInventory(t *testing.T) {
	body := `{"data":[{"id":"m","object":"model","created":1},{"id":"n","object":"model","created":2}]}`
	response, err := boundedJSONClient(t, body).DoRaw(WithJSONCollectionLimit(context.Background(), 2), http.MethodGet, "models", nil, "application/json")
	if err != nil || len(response.JSON["data"].([]any)) != 2 {
		t.Fatalf("response=%+v error=%v", response, err)
	}
}

func TestJSONCollectionLimitRejectsExcessiveNesting(t *testing.T) {
	body := `{"data":[] ,"nested":` + strings.Repeat(`{"x":`, maxJSONNesting+1) + `0` + strings.Repeat(`}`, maxJSONNesting+1) + `}`
	_, err := boundedJSONClient(t, body).DoRaw(WithJSONCollectionLimit(context.Background(), 2), http.MethodGet, "models", nil, "application/json")
	if !errors.Is(err, ErrWorkLimit) {
		t.Fatalf("error=%v", err)
	}
}
