package openai

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"reflect"
	"runtime/debug"
	"sync"
	"testing"

	"github.com/ilyar/aiqa/internal/extsuite"
	"github.com/ilyar/aiqa/internal/logging"
	"github.com/ilyar/aiqa/internal/probe"
	"github.com/ilyar/aiqa/internal/profile"
	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/telemetry"
)

func TestManifestRetainsUsefulLegacyCheckerCoverage(t *testing.T) {
	var got []string
	for _, suite := range New().Manifest().Suites {
		got = append(got, suite.ID)
	}
	want := []string{"chat.completions", "chat.json", "chat.multi-turn", "chat.stream", "chat.stream.usage", "chat.tools", "chat.tools.stream", "error.responses", "images.generations", "models.get", "models.list"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("OpenAI Official SDK parity changed: got %v, want %v", got, want)
	}
}

func TestRunPassesNamedSuiteProbeContextToSDKHTTP(t *testing.T) {
	p, err := profile.LoadFile("openai-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	logger := logging.New(4)
	tracing := telemetry.New(logger)
	defer tracing.Shutdown(context.Background())
	transport := &recordingRoundTripper{}
	subject := result.SubjectRef{TargetID: "target", Protocol: "openai", Kind: "provider", ID: "provider"}
	request := extsuite.RunRequest{
		SchemaVersion: extsuite.ProtocolVersion,
		Type:          "run_request",
		RunID:         "run",
		Target:        extsuite.Target{ID: "target", Protocol: "openai", BaseURL: "https://provider.test/v1"},
		Subject:       subject,
		SuiteIDs:      []string{"models.list"},
		Credential:    extsuite.Credential{Type: "api_key", Value: "secret"},
		Runtime: extsuite.RuntimeServices{
			HTTPClient: &http.Client{Transport: transport},
			Executor:   probe.NewExecutor(1),
			Telemetry:  tracing,
		},
	}
	collector, err := extsuite.NewCollector(New().Manifest(), request, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New().Run(context.Background(), request, collector); err != nil {
		t.Fatal(err)
	}
	transport.mu.Lock()
	probeID := transport.probeID
	transport.mu.Unlock()
	if probeID != extensionID+".models.list" {
		t.Fatalf("SDK HTTP request probe=%q, want %q", probeID, extensionID+".models.list")
	}
}

type recordingRoundTripper struct {
	mu      sync.Mutex
	probeID string
}

func (r *recordingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	fields := logging.FieldsFromContext(request.Context())
	probeID, _ := fields["probe"].(string)
	r.mu.Lock()
	r.probeID = probeID
	r.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(`{"data":[{"id":"model-a"}]}`)),
		Request:    request,
	}, nil
}

func TestManifestSuitesAreProfileBoundAndVersionMatchesDependency(t *testing.T) {
	p, err := profile.LoadFile("openai-compat-2026-09.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, suite := range New().Manifest().Suites {
		if _, ok := p.ExtensionBinding(extensionID, suite.ID); !ok {
			t.Errorf("suite %q has no explicit profile binding", suite.ID)
		}
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("Go build information is unavailable")
	}
	for _, dependency := range info.Deps {
		if dependency.Path == "github.com/openai/openai-go/v3" {
			if dependency.Version != sdkVersion {
				t.Fatalf("embedded SDK version=%q, manifest=%q", dependency.Version, sdkVersion)
			}
			return
		}
	}
	t.Fatal("embedded OpenAI SDK dependency is missing")
}
