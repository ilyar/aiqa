package googlegenai

import (
	"context"
	"errors"
	"fmt"
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
)

func inventorySecurityOptions(t *testing.T, baseURL string, cfg config.Config) protocolpkg.RunOptions {
	t.Helper()
	p, err := profile.LoadFile("google-genai-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Concurrency == 0 {
		cfg.Concurrency = 1
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = time.Second
	}
	return protocolpkg.RunOptions{
		Target:  config.TargetConfig{ID: "inventory-fixture", Protocol: config.ProtocolGoogleGenAI, BaseURL: baseURL, APIKey: "fixture-key", APIVersion: "v1beta"},
		Config:  cfg,
		Profile: p,
	}
}

func TestPrepareTargetInventoryRejectsUnsafeRawIDsBeforePlanning(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   string
	}{
		{name: "raw newline", id: "models/bad\nname"},
		{name: "unicode line separator", id: "models/bad\u2028name"},
		{name: "oversized", id: "models/" + strings.Repeat("x", 1025)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"models":[{"name":%q}]}`, tc.id)
			}))
			defer server.Close()

			prepared, err := PrepareTarget(context.Background(), inventorySecurityOptions(t, server.URL, config.Config{MaxCatalogModels: 4, InventoryByteBudget: 4096}))
			if !errors.Is(err, api.ErrWorkLimit) {
				t.Fatalf("PrepareTarget error = %v, want work limit", err)
			}
			if len(prepared.Jobs) != 0 || len(prepared.Candidates) != 0 || len(prepared.Binding.SelectedModels) != 0 || prepared.Execute != nil {
				t.Fatalf("unsafe inventory reached planning: %#v", prepared)
			}
		})
	}
}

func TestPrepareTargetInventoryCountsRawDuplicateAndMalformedEntries(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "duplicate raw entries", body: `{"models":[{"name":"models/a"},{"name":"models/a"}]}`},
		{name: "malformed entries", body: `{"models":[{"name":"models/a"},42]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			prepared, err := PrepareTarget(context.Background(), inventorySecurityOptions(t, server.URL, config.Config{MaxCatalogModels: 1, InventoryByteBudget: 4096}))
			if !errors.Is(err, api.ErrWorkLimit) {
				t.Fatalf("PrepareTarget error = %v, want work limit", err)
			}
			if len(prepared.Jobs) != 0 || len(prepared.Binding.SelectedModels) != 0 || prepared.Execute != nil {
				t.Fatalf("over-limit raw entries reached planning: %#v", prepared)
			}
		})
	}
}

func TestGoogleInventoryValidMultiPageAllowsConformanceImperfection(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("pageToken") == "" {
			_, _ = w.Write([]byte(`{"models":[{"name":"models/a"}],"nextPageToken":"second"}`))
			return
		}
		// Missing generation metadata is a conformance imperfection, not an
		// inventory-safety failure. Both safe entries remain available.
		_, _ = w.Write([]byte(`{"models":[{"name":"models/b"}]}`))
	}))
	defer server.Close()

	prepared, err := PrepareTarget(context.Background(), inventorySecurityOptions(t, server.URL, config.Config{MaxCatalogModels: 2, MaxSelectedModels: 2, InventoryByteBudget: 4096}))
	if err != nil {
		t.Fatalf("PrepareTarget valid inventory: %v", err)
	}
	if got := prepared.Binding.SelectedModels; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("selected = %v, want [a b]", got)
	}
	if len(prepared.Jobs) != 1 || requests.Load() != 2 {
		t.Fatalf("jobs=%d requests=%d, want one inventory job and two pages", len(prepared.Jobs), requests.Load())
	}
}

func TestGoogleInventorySecondPageFailureDoesNotPlanOrReportModels(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("pageToken") == "" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[{"name":"models/a"}],"nextPageToken":"second"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":`)) // syntactically invalid response
	}))
	defer server.Close()

	options := inventorySecurityOptions(t, server.URL, config.Config{MaxCatalogModels: 4, MaxSelectedModels: 4, InventoryByteBudget: 4096})
	prepared, err := PrepareTarget(context.Background(), options)
	if err != nil {
		t.Fatalf("PrepareTarget must retain bounded discovery diagnostics: %v", err)
	}
	if len(prepared.Jobs) != 1 || prepared.Jobs[0].Model != "" || len(prepared.Candidates) != 0 || len(prepared.Binding.SelectedModels) != 0 || prepared.Execute == nil {
		t.Fatalf("failed inventory scheduled model work: %#v", prepared)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("prepare requests=%d, want two pages", got)
	}

	run, err := prepared.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("Execute succeeded after second-page decode failure")
	}
	report := run.Target
	if len(report.Models) != 0 || len(report.Discovery.Models) != 0 || report.Discovery.ModelCount != 0 {
		t.Fatalf("failed inventory reached report models: %#v", report)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("execute repeated or issued model requests: %d", got)
	}

	// The non-prepared execution path must offer the same guarantee. It may
	// retain discovery evidence, but must not materialize the first page.
	directReport, err := Run(context.Background(), RunOptions{Target: options.Target, Config: options.Config, Profile: options.Profile})
	if err == nil {
		t.Fatal("direct Run succeeded after second-page decode failure")
	}
	if len(directReport.Models) != 0 || len(directReport.Discovery.Models) != 0 || directReport.Discovery.ModelCount != 0 {
		t.Fatalf("direct failed inventory reached report models: %#v", directReport)
	}
	if got := requests.Load(); got != 4 {
		t.Fatalf("direct run requests=%d, want exactly two additional discovery pages", got)
	}
}
