package anthropic

import (
	"github.com/ilyar/aiqa/internal/api"
	"github.com/ilyar/aiqa/internal/result"
)

// Reviewed 2026-09-16. Exact model contracts apply to compatible facades too;
// the endpoint hostname is not evidence of capability support.
const sonnet5PrefillContract = "https://platform.claude.com/docs/en/models/sonnet-5/whats-new-sonnet-5"

func applyPrefillContract(test *result.TestResult, model string, resp api.Response) {
	// Preserve a stronger caller profile, unknown models, unexpected failures,
	// and successful continuations. Only the documented rejection resolves the
	// optional positive-prefill requirement; it does not become a passing probe.
	if model != "claude-sonnet-5" || test.Required || test.Status != result.StatusUnsupported || resp.StatusCode != 400 {
		return
	}
	errorObject, _ := resp.JSON["error"].(map[string]any)
	if resp.JSON["type"] != "error" || errorObject["type"] != "invalid_request_error" ||
		errorObject["message"] != "This model does not support assistant message prefill. The conversation must end with a user message." {
		return
	}
	test.Applicability = result.ApplicabilityNotApplicable
	test.Summary = "Assistant prefill is excluded by the documented model contract; the expected HTTP 400 rejection was observed"
	test.Signals = append(test.Signals, result.EvidenceSignal{
		Code: "contract.anthropic.assistant_prefill.excluded", RootCause: "documented-model-limitation",
		Parameters: map[string]any{"contract_revision": "2026-09-16", "source": sonnet5PrefillContract},
	})
}
