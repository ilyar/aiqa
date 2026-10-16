package googlegenai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/profile"
	protocolpkg "github.com/ilyar/aiqa/internal/protocol"
	"github.com/ilyar/aiqa/internal/result"
)

func TestStatefulCreateWithoutIDRecordsCleanupUnprovenOrSafeSkip(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   result.Status
	}{
		{"success without id", http.StatusOK, `{}`, result.StatusError},
		{"deterministic client rejection", http.StatusBadRequest, `{"error":{"message":"bad request"}}`, result.StatusSkip},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var creates atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1beta/interactions" {
					t.Fatalf("path=%q", r.URL.Path)
				}
				creates.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client, err := NewClient(server.URL, "key", "v1beta", "test", 0)
			if err != nil {
				t.Fatal(err)
			}
			p, err := profile.LoadFile("google-genai-compat-2026-09.json")
			if err != nil {
				t.Fatal(err)
			}
			got := runInteractionStateful(context.Background(), client, time.Second, p, "model", nil, nil)
			if creates.Load() != 1 {
				t.Fatalf("creates=%d, want 1", creates.Load())
			}
			if got[1].Status != tc.want {
				t.Fatalf("cleanup=%+v", got[1])
			}
		})
	}
}

func TestDiscoverAllStopsSecondPageAtRemainingRawByteBudget(t *testing.T) {
	var pages atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("pageToken") == "" {
			_, _ = w.Write([]byte(`{"models":[{"name":"models/a"}],"nextPageToken":"next"}`))
			return
		}
		_, _ = w.Write([]byte(`{"models":[]}` + strings.Repeat(" ", 1024)))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "key", "v1beta", "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = discoverAll(context.Background(), client, 10, 128)
	if !errors.Is(err, api.ErrWorkLimit) {
		t.Fatalf("err=%v, want work limit", err)
	}
	if pages.Load() != 2 {
		t.Fatalf("pages=%d, want 2", pages.Load())
	}
}

func TestStatefulResponseLossIsCleanupUnprovenWithoutRetry(t *testing.T) {
	var creates atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		creates.Add(1)
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("server writer does not support hijacking")
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "key", "v1beta", "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	p, err := profile.LoadFile("google-genai-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	got := runInteractionStateful(context.Background(), client, time.Second, p, "model", nil, nil)
	if creates.Load() != 1 {
		t.Fatalf("creates=%d, want 1", creates.Load())
	}
	if got[1].Status != result.StatusError || got[1].Signals[0].Code != "execution.persistent_resource.cleanup_unproven" {
		t.Fatalf("cleanup=%+v", got[1])
	}
}

func TestGoogleInventoryValidationFailsClosedBeforePlanning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"models/bad\u202e"}]}`))
	}))
	defer server.Close()
	p, err := profile.LoadFile("google-genai-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareTarget(context.Background(), protocolpkg.RunOptions{Target: config.TargetConfig{ID: "fixture", Protocol: config.ProtocolGoogleGenAI, BaseURL: server.URL, APIKey: "key", APIVersion: "v1beta"}, Config: config.Config{Concurrency: 1, Timeout: time.Second, MaxCatalogModels: 4, InventoryByteBudget: 1024}, Profile: p})
	if !errors.Is(err, api.ErrWorkLimit) {
		t.Fatalf("err=%v, want work limit", err)
	}
	if len(prepared.Jobs) != 0 || len(prepared.Candidates) != 0 {
		t.Fatalf("unsafe inventory reached planning: %+v", prepared)
	}
}

func TestDiscoverAllCumulativePageCountFailsClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("pageToken") == "" {
			_, _ = w.Write([]byte(`{"models":[{"name":"models/a"}],"nextPageToken":"n"}`))
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"models/a"}]}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "key", "v1beta", "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	models, _, _, err := discoverAll(context.Background(), client, 1, 1024)
	if !errors.Is(err, api.ErrWorkLimit) || models != nil {
		t.Fatalf("models=%v err=%v", models, err)
	}
}

func TestDiscoverAllFollowupTransportFailureDropsAccumulatedModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pageToken") == "" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[{"name":"models/a"}],"nextPageToken":"n"}`))
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("server writer does not support hijacking")
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "key", "v1beta", "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	models, _, _, err := discoverAll(context.Background(), client, 4, 1024)
	if err == nil || models != nil {
		t.Fatalf("models=%v err=%v", models, err)
	}
}
