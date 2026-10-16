package anthropic

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

func TestPaginationFollowupUsesInventoryLimits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}` + strings.Repeat(" ", 1024)))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "key", "v1", "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	p, err := profile.LoadFile("anthropic-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	first := api.Response{StatusCode: http.StatusOK, Body: []byte(`{"data":[],"has_more":true,"last_id":"first","first_id":"first"}`), JSON: map[string]any{"data": []any{}, "has_more": true, "last_id": "first", "first_id": "first"}}
	if got := ValidateModels(first.JSON); len(got) != 0 {
		t.Fatalf("first schema=%v", got)
	}
	test := paginationResult(context.Background(), client, config.Config{Timeout: time.Second, InventoryByteBudget: int64(len(first.Body)) + 64, MaxCatalogModels: 4}, p, first, []api.Model{{ID: "first"}}, nil, nil)
	if test.Status != result.StatusError {
		t.Fatalf("pagination status=%+v", test)
	}
}

func TestPaginationFollowupCollectionLimitAndNormalPage(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       result.Status
	}{
		{"normal", `{"data":[{"id":"second"}]}`, result.StatusPass},
		{"too_many_compact_models", `{"data":[{"id":"second"},{"id":"third"}]}`, result.StatusError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client, err := NewClient(server.URL, "key", "v1", "test", 0)
			if err != nil {
				t.Fatal(err)
			}
			p, err := profile.LoadFile("anthropic-compat-2026-09.json")
			if err != nil {
				t.Fatal(err)
			}
			first := api.Response{StatusCode: http.StatusOK, Body: []byte(`{"data":[]}`), JSON: map[string]any{"data": []any{}, "has_more": true, "last_id": "first", "first_id": "first"}}
			got := paginationResult(context.Background(), client, config.Config{Timeout: time.Second, InventoryByteBudget: 4096, MaxCatalogModels: 1}, p, first, []api.Model{{ID: "first"}}, nil, nil)
			if got.Status != tc.want {
				t.Fatalf("status=%s, want %s: %+v", got.Status, tc.want, got)
			}
			if tc.want == result.StatusError {
				found := false
				for _, signal := range got.Signals {
					found = found || signal.Code == "execution.work_limit"
				}
				if !found {
					t.Fatalf("missing local work-limit evidence: %+v", got)
				}
			}
		})
	}
}
