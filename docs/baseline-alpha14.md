# Original-provider baseline: alpha.14

The alpha.13 baseline run `01a0aa84-a99a-85a9-b331-a29a3189d518`
does not establish that all three original providers violate their protocols.
Provider identity is not a reason to force a passing verdict.

| Target | Baseline evidence | Alpha.14 correction or remaining prerequisite |
| --- | --- | --- |
| Anthropic | All 14 core requirements passed. The strict-tool response stopped at `max_tokens` with the probe's 32-token limit. | Use a 128-token strict-tool budget. Truncated output is incomplete evidence; a completed invalid tool input still fails. |
| Google | Native countTokens sent top-level `generationConfig` and returned 400; the SDK countTokens call passed. Generation also returned 503 and daily-quota errors. | Send contents only for countTokens. Quota and service availability remain external prerequisites; those errors must not become PASS. |
| OpenAI | GPT Image 2 passed image checks. A speculative image call for GPT-5.4 Mini was rejected for the selected model. | Resolve that narrow speculative rejection as an unavailable model route, without deleting observations. A rejected route with positive declared support remains a failure. |

The image exception requires a speculative module and an exact structured
model-rejection error. Generic bad requests, authorization errors, rate limits,
server errors, and malformed successful responses are not capability absence.
Successful positive probes prevent exclusion. If no requested model route was
evaluated, the existing no-match guard keeps the report inconclusive.

The report remains `aiqa.report.v3`. Native observations remain in the report;
canonical requirement applicability records the resolved scope. A model catalog
entry does not imply support for every endpoint.

## Official contracts

- [Anthropic stop reasons](https://platform.claude.com/docs/en/build-with-claude/handling-stop-reasons): incomplete tool use at the output limit requires a larger budget, not a schema-conformance conclusion.
- [Google countTokens](https://ai.google.dev/api/tokens): the request accepts contents or a nested generateContentRequest, not top-level generationConfig.
- [GPT-5.4 Mini](https://developers.openai.com/api/docs/models/gpt-5.4-mini): image input and use of an image-generation tool are distinct from being the image-generation model.
- [GPT Image 2](https://developers.openai.com/api/docs/models/gpt-image-2): the selected image-generation model.

## Verification boundaries

Local loopback tests check the corrected request shapes, truncation handling,
negative capability discovery, and preservation of genuine failures. They do not
prove live provider availability or account quota. Preserve the original report
and use a separate output name for any authorized live recheck. A daily-quota
failure requires quota restoration; lowering concurrency does not replenish it.

## Follow-up limited to compat and SDK

The six-model alpha.14 recheck completed 58 jobs in 19m58s (run
`01a0aad0-29f1-8cd5-b86e-23cebf856d1e`). It exposed further checker issues;
that run predates the following corrections and does not verify them live.

- Partial Anthropic metadata no longer excludes every unmentioned capability.
  Individually unknown safe Messages features remain probeable, without being
  reported as advertised support. Nested thinking types are recognized; explicit
  negative evidence, ambiguity and high-token authorization remain authoritative.
- The documented Sonnet 5 assistant-prefill rejection is retained as HTTP 400 /
  UNSUPPORTED evidence. Only the matching optional positive requirement becomes
  N/A. Generic failures, unknown model IDs and stricter required profiles are not
  exempted. No hostname receives an automatic passing verdict.
- Google's explicit `NOT_FOUND` message that a model is no longer available to
  new users is an execution prerequisite failure, not evidence of malformed
  protocol behavior. Native and SDK checks retain HTTP 404 and remain incomplete.
  Generic 404 responses are not reclassified by this narrow rule.
- A structured Google daily generation quota rejection stops retries of that
  individual request. Other models, countTokens and remaining jobs are not
  cancelled. This is not a run-wide circuit breaker or a promise of short runs.

Google model availability and sufficient account quota remain external
prerequisites. Replacement models must be selected explicitly; the checker does
not silently substitute a different model. `test=model-param` behavior is
unchanged; deferred issues and acceptance criteria are in
[`model-param-backlog.md`](../model-param-backlog.md).
