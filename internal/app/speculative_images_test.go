package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ilyar/aiqa/internal/result"
	"github.com/ilyar/aiqa/internal/schema"
)

const probePolicyImageRejection = `{"error":{"message":"The model does not exist","code":"invalid_value","param":"model","type":"image_generation_user_error"}}`

func TestProbePolicyImageRejectionRetainsNativeAndSDKEvidenceWithoutProviderFailure(t *testing.T) {
	schemaPath, err := filepath.Abs(filepath.Join("..", "..", "schema", "report-v3.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	server := speculativeImagesFixture(t, nil)
	defer server.Close()
	withTempWorkingDirectory(t)
	cfg := testConfig(server.URL+"/v1", []string{"compat", "sdk"})
	cfg.UnknownModel = "probe"
	cfg.Modules = []string{"auto", "images-generations"}
	cfg.AllowBillableArtifacts = true
	cfg.MaxArtifactsPerModel = 2
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.ValidateFile(schemaPath, "report.json"); err != nil {
		t.Fatalf("resolved capability evidence broke report v3: %v", err)
	}
	target := out.ReportV3.Targets[0]
	model := target.Models[0]
	images := moduleByID(model.Modules, "images-generations")
	if out.ExitCode != 0 || target.Summary.Assessment.Verdict != "pass" || images == nil || images.Applicability != result.ApplicabilityNotApplicable || images.Execution != result.ExecutionExecuted || images.Source != "probe_policy" {
		t.Fatalf("unexpected speculative outcome: exit=%d verdict=%s image=%+v", out.ExitCode, target.Summary.Assessment.Verdict, images)
	}
	chatPassed, nativeUnsupported, sdkUnsupported := false, false, false
	for _, requirement := range target.Requirements {
		if requirement.Status == result.RequirementFail {
			t.Fatalf("provider failure remained in canonical assessment: %+v", requirement)
		}
		if requirement.ID == "chat.basic" && requirement.Status == result.RequirementPass {
			chatPassed = true
		}
		if requirement.Module != "images-generations" {
			continue
		}
		if requirement.Applicability != result.ApplicabilityNotApplicable {
			t.Fatalf("image requirement not excluded: %+v", requirement)
		}
		for _, evidence := range requirement.Evidence {
			if evidence.Kind == "native" && evidence.Status == result.StatusUnsupported && evidence.HTTPStatus == http.StatusBadRequest && hasSignal(evidence.Signals, "capability.probe.unsupported") {
				nativeUnsupported = true
			}
			if evidence.Kind == "extension" && evidence.Status == result.StatusUnsupported {
				sdkUnsupported = true
			}
		}
	}
	if !chatPassed || !nativeUnsupported || !sdkUnsupported {
		t.Fatalf("required evidence missing: chat=%v native=%v sdk=%v requirements=%+v", chatPassed, nativeUnsupported, sdkUnsupported, target.Requirements)
	}
}

func TestProbePolicyImageOnlyRejectionIsInconclusive(t *testing.T) {
	server := speculativeImagesFixture(t, nil)
	defer server.Close()
	withTempWorkingDirectory(t)
	cfg := testConfig(server.URL+"/v1", []string{"compat", "sdk"})
	cfg.UnknownModel = "probe"
	cfg.Modules = []string{"images-generations"}
	cfg.AllowBillableArtifacts = true
	cfg.MaxArtifactsPerModel = 2
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ExitCode != 1 || out.ReportV3.Targets[0].Summary.Assessment.Verdict != "inconclusive" {
		t.Fatalf("image-only rejection must not pass: exit=%d summary=%+v", out.ExitCode, out.ReportV3.Targets[0].Summary.Assessment)
	}
}

func TestAdvertisedImageRejectionRemainsProviderFailure(t *testing.T) {
	server := speculativeImagesFixture(t, []any{"images.generations"})
	defer server.Close()
	withTempWorkingDirectory(t)
	cfg := testConfig(server.URL+"/v1", []string{"compat"})
	cfg.Models = []string{"vendor/model-a"}
	cfg.Modules = []string{"images-generations"}
	cfg.AllowBillableArtifacts = true
	out, err := Run(context.Background(), cfg, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ExitCode != 1 || out.ReportV3.Targets[0].Summary.Assessment.Verdict != "fail" {
		t.Fatalf("advertised image rejection was hidden: exit=%d summary=%+v", out.ExitCode, out.ReportV3.Targets[0].Summary.Assessment)
	}
}

func speculativeImagesFixture(t *testing.T, capabilities []any) *httptest.Server {
	t.Helper()
	return newMockOpenAIWithMiddleware(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v1/models":
				model := map[string]any{"id": "vendor/model-a", "object": "model", "created": 1, "owned_by": "fixture"}
				if capabilities != nil {
					model["capabilities"] = capabilities
				}
				writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": []any{model}})
			case "/v1/images/generations":
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(probePolicyImageRejection))
			default:
				next.ServeHTTP(w, r)
			}
		})
	})
}

func hasSignal(signals []result.EvidenceSignal, code string) bool {
	for _, signal := range signals {
		if signal.Code == code {
			return true
		}
	}
	return false
}
