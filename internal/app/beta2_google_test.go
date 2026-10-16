package app

import (
	"context"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/result"
)

// This fixture lacks the generate-content profile's invalid-model envelope, so
// this test scopes itself to successful generation rather than target verdict.
// Marker/truncation classification is covered at the protocol/assessment layer.
func TestBeta2GoogleGenerateContentHappyPath(t *testing.T) {
	server := newGoogleSDKFixture(t)
	defer server.Close()
	withTempWorkingDirectory(t)
	cfg := config.Config{Targets: []config.TargetConfig{{ID: "google-genai", Protocol: config.ProtocolGoogleGenAI, BaseURL: server.URL, APIKey: "fixture-key", APIVersion: "v1beta", Models: []string{"fixture-text"}, ProfileFile: "google-genai-generate-content-compat-2026-09.json", Modules: []string{"auto"}, RequestInterval: time.Millisecond}}, Tests: []string{"compat"}, Timeout: 5 * time.Second, Concurrency: 2, Modules: []string{"auto"}, MaxArtifactsPerModel: 1, FailOn: "never", Guidance: "off"}
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil || out.ExitCode != 0 || out.ReportV3 == nil {
		t.Fatalf("run: exit=%d err=%v", out.ExitCode, err)
	}
	found, fixtureInvalidModelFailure := false, false
	for _, test := range out.ReportV3.Targets[0].Models[0].Compatibility {
		if test.ID == "google.generate-content.output-text" && test.Status != result.StatusPass {
			t.Fatalf("output text=%s: %+v", test.Status, test)
		}
		if test.ID == "google.generate-content.output-text" {
			found = true
		}
		if test.ID == "google.error.invalid_model" && test.Status == result.StatusFail {
			fixtureInvalidModelFailure = true
		}
	}
	if !found {
		t.Fatal("generate-content profile did not execute output-text requirement")
	}
	if !fixtureInvalidModelFailure {
		t.Fatal("fixture limitation changed: expected invalid-model envelope failure")
	}
}
