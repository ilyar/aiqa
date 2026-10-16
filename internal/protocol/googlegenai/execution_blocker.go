package googlegenai

import (
	"encoding/json"
	"strings"

	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/result"
)

// ExecutionBlocker recognizes only explicit, structured prerequisite failures.
// A generic NOT_FOUND is still compatibility evidence, not a model-access gap.
// Returned text is fixed: provider messages, prompts and bodies are not copied.
func ExecutionBlocker(resp api.Response) (string, result.EvidenceSignal) {
	obj, _ := resp.JSON["error"].(map[string]any)
	message, _ := obj["message"].(string)
	if resp.StatusCode == 404 && obj["status"] == "NOT_FOUND" && strings.HasPrefix(message, "This model models/") {
		model, _, found := strings.Cut(strings.TrimPrefix(message, "This model models/"), " is no longer available to new users.")
		if found && model != "" && !strings.ContainsAny(model, " \t\r\n") {
			return "The selected model is unavailable to new users; model access must be restored before compatibility can be evaluated", result.EvidenceSignal{Code: "execution.model_access_unavailable", RootCause: "model-access-unavailable"}
		}
	}
	if api.IsGoogleDailyQuota(resp.StatusCode, resp.JSON) {
		return "The provider reported exhausted daily generation quota; compatibility cannot be evaluated until quota is available", result.EvidenceSignal{Code: "execution.daily_quota_exhausted", RootCause: "daily-quota-exhausted"}
	}
	return "", result.EvidenceSignal{}
}

func applyExecutionBlocker(test *result.TestResult, resp api.Response) bool {
	summary, signal := ExecutionBlocker(resp)
	if signal.Code == "" {
		return false
	}
	test.Status, test.Summary, test.Error = result.StatusError, summary, ""
	test.Signals = append(test.Signals, signal)
	return true
}

func applyStreamExecutionBlocker(test *result.TestResult, stream api.StreamResponse) {
	if stream.StatusCode != 404 && stream.StatusCode != 429 {
		return
	}
	var body map[string]any
	if json.Unmarshal([]byte(stream.RawSnippet), &body) == nil {
		applyExecutionBlocker(test, api.Response{StatusCode: stream.StatusCode, JSON: body})
	}
}
