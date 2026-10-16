package app

import (
	"context"
	"testing"
	"time"

	"github.com/ilyar/aiqa/internal/config"
	"github.com/ilyar/aiqa/internal/result"
)

func TestConfiguredCredentialRejectionIsInconclusiveForNativeProtocols(t *testing.T) {
	openAI := newMockOpenAI(t)
	defer openAI.Close()
	anthropic := newMockAnthropic(t)
	defer anthropic.Close()
	google := newGoogleSDKFixture(t)
	defer google.Close()
	withTempWorkingDirectory(t)

	openAIConfig := testConfig(openAI.URL+"/v1", []string{"compat"})
	openAIConfig.APIKey = "rejected-key"
	openAIConfig.Formats = nil
	openAIConfig.Name = "openai-auth"

	for _, tc := range []struct {
		name          string
		cfg           config.Config
		requirementID string
	}{
		{name: "openai", cfg: openAIConfig, requirementID: "models.list.http"},
		{name: "anthropic", cfg: credentialBlockedTargetConfig("anthropic", config.ProtocolAnthropic, anthropic.URL+"/anthropic", "rejected-key", "claude-mock", "anthropic-compat-2026-09.json", ""), requirementID: "anthropic.models.list.http"},
		{name: "google", cfg: credentialBlockedTargetConfig("google-genai", config.ProtocolGoogleGenAI, google.URL, "rejected-key", "fixture-text", "google-genai-compat-2026-09.json", "v1beta"), requirementID: "google.models.list.http"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Run(context.Background(), tc.cfg, BuildInfo{Version: "test"})
			// Inventory authentication stops the runner before it can complete,
			// so Run reports an operational error. Its emitted report must still
			// distinguish that blocker from a protocol verdict.
			if err == nil || out.ReportV3 == nil || len(out.ReportV3.Targets) != 1 {
				t.Fatalf("run: exit=%d err=%v report=%+v", out.ExitCode, err, out.ReportV3)
			}
			target := out.ReportV3.Targets[0]
			if got := target.Summary.Assessment; got == nil || got.Verdict != "inconclusive" || got.Comparison.Rankable {
				t.Fatalf("credential rejection became a compatibility verdict: %+v", got)
			}
			requirement := findRequirement(target.Requirements, tc.requirementID)
			if requirement == nil || requirement.Status != result.RequirementError || !hasEvidenceSignal(requirement.Evidence, "execution.credentials_rejected") {
				t.Fatalf("credential blocker evidence=%+v", requirement)
			}
			if !hasGuidanceCode(target.Guidance.Findings, "execution.credentials_rejected") {
				t.Fatalf("credential guidance missing: %+v", target.Guidance.Findings)
			}
			if !hasCredentialConfigurationGuidance(target.Guidance.Recommendations) {
				t.Fatalf("credential guidance must be a configuration action: %+v", target.Guidance.Recommendations)
			}
			if target.RateLimit.LogicalRequests == 0 || target.RateLimit.PhysicalAttempts == 0 {
				t.Fatalf("%s rejected inventory was omitted from rate-limit telemetry: %+v", tc.name, target.RateLimit)
			}
		})
	}
}

func credentialBlockedTargetConfig(id string, protocol config.Protocol, baseURL, key, model, profileFile, apiVersion string) config.Config {
	return config.Config{
		Targets: []config.TargetConfig{{ID: id, Protocol: protocol, BaseURL: baseURL, APIKey: key, Models: []string{model}, ProfileFile: profileFile, Modules: []string{"auto"}, RequestInterval: time.Millisecond, APIVersion: apiVersion}},
		Tests:   []string{"compat"}, Timeout: 5 * time.Second, Concurrency: 1, Modules: []string{"auto"},
		MaxArtifactsPerModel: 1, FailOn: "core", Guidance: "auto", Formats: nil, Name: id + "-auth",
	}
}

func findRequirement(requirements []result.RequirementAssessment, id string) *result.RequirementAssessment {
	for i := range requirements {
		if requirements[i].ID == id {
			return &requirements[i]
		}
	}
	return nil
}

func hasEvidenceSignal(evidence []result.EvidenceRef, code string) bool {
	for _, item := range evidence {
		for _, signal := range item.Signals {
			if signal.Code == code {
				return true
			}
		}
	}
	return false
}

func hasGuidanceCode(findings []result.Finding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func hasCredentialConfigurationGuidance(recommendations []result.Recommendation) bool {
	for _, recommendation := range recommendations {
		if recommendation.ActionKind == "configure" && recommendation.Priority == "p2" {
			return true
		}
	}
	return false
}
