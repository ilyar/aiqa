# AIQA compatibility report

_measures observed API behavior, a passing report is not a security audit or an official certification_

- **Generated:** 2026-09-06 19:28:47Z
- **Tool:** `aiqa 1.0.0-alpha.7`
- **Service:** [127.0.0.1:32786](http://127.0.0.1:32786)
- **Suites:** `compat,sdk`
- **Requested models:** `wiremock-broken-model` (one global list; each target reports only its selected inventory intersection)
- **Timeout:** `1m0s` per request
- **External checks:** [Anthropic Go SDK v1.68.0](https://github.com/anthropics/anthropic-sdk-go/releases/tag/v1.68.0), [Google Gen AI Go SDK v1.71.0](https://github.com/googleapis/go-genai/releases/tag/v1.71.0), [OpenAI Go SDK v3.54.0](https://github.com/openai/openai-go/releases/tag/v3.54.0)

## Targets

| Target | Verdict | Core | Optional | Evidence | Models | Actions |
|---|---:|---:|---:|---:|---:|---:|
| `anthropic` | FAIL | 1/14 (7.1%) | 0/38 (0.0%) | 2/52 (3.8%) | 1 | 3 |
| `google-genai` | INCONCLUSIVE | 5/22 (22.7%) | 1/29 (3.4%) | 6/51 (11.8%) | 1 | 20 |
| `openai` | INCONCLUSIVE | 2/6 (33.3%) | 1/16 (6.3%) | 3/22 (13.6%) | 1 | 1 |

**Run verdict:** FAIL

Targets are assessed independently. Percentages are not combined into a cross-protocol score.

## Rate-limit and latency

| Target | Logical | Attempts | Successful | 429 | Retry attempts | Recovered | Exhausted | Backoff | Safe req/s | Concurrency | Mean | P50 | P95 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| `anthropic` | 2 | 4 | 1 | 0 | 2 | 0 | 1 | 300ms | 0.50 req/s | 1 | 26ms | 26ms | 26ms |
| `google-genai` | 16 | 46 | 1 | 0 | 30 | 0 | 15 | 4500ms | 0.50 req/s | 1 | 3ms | 3ms | 3ms |
| `openai` | 2 | 4 | 1 | 0 | 2 | 0 | 1 | 300ms | 0.50 req/s | 1 | 3ms | 3ms | 3ms |

## Target: Anthropic

- **Target ID:** `anthropic`
- **Profile:** `anthropic-compat@2026-09-01`
- **Requested coverage:** 5 routes; 0 executed, 5 unresolved (1 core, 4 optional), 0 excluded.
- **API version:** `2023-06-01`
- **External checks:** [Anthropic Go SDK v1.68.0](https://github.com/anthropics/anthropic-sdk-go/releases/tag/v1.68.0)
- **Discovered models:** 1
- **Selected models:** `wiremock-broken-model`

### Actions

| PRI | Recommendation | Owner | Expected impact | Confidence |
|:---:|---|---|---|---:|
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Correct the incompatible behavior | `provider_gateway` | recover 1 core; projected INCONCLUSIVE | LOW |
| P1 | Investigate the incomplete or conflicting evidence | `unknown` | recover 1 optional | LOW |

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** model availability could not be determined after a transient preflight failure

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.messages.basic`

#### P0 · Correct the incompatible behavior

**Why this matters:** The affected core requirement blocks the authoritative compatibility verdict.

**Observed:** Anthropic model-list schema is invalid

**Change or investigate:**

1. Inspect the linked structured evidence for the first incompatible boundary.
2. Make the implementation satisfy the canonical requirement without weakening validation.

**Verification targets:**

- Requirements: `anthropic.models.list.schema`

#### P1 · Investigate the incomplete or conflicting evidence

**Why this matters:** Trustworthy evidence sources disagree, so the mismatch must be localized before choosing a fix.

**Observed:** Anthropic Go SDK sent and parsed the suite Anthropic Models endpoint returned JSON Anthropic model-list schema is invalid

**Change or investigate:**

1. Compare the structured evidence records and request settings for the affected requirement.
2. Localize the mismatch before changing the provider or checker.

**Verification targets:**

- Requirements: `anthropic.official-sdk.models.list`

### Canonical requirements needing attention

| Status | Level | Subject | Requirement | Evidence | Blocked by |
|---:|---:|---|---|---:|---|
| SKIP | CORE | `model:wiremock-broken-model` | `anthropic.auth.x_api_key` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.error.invalid_auth` | 0 | anthropic.messages.basic |
| SKIP | CORE | `model:wiremock-broken-model` | `anthropic.error.invalid_json` | 0 | anthropic.messages.basic |
| SKIP | CORE | `model:wiremock-broken-model` | `anthropic.error.invalid_model` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.error.invalid_types` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.error.request_id` | 0 | anthropic.messages.basic |
| SKIP | CORE | `model:wiremock-broken-model` | `anthropic.error.shape` | 0 | anthropic.messages.basic |
| SKIP | CORE | `model:wiremock-broken-model` | `anthropic.header.version.accepted` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.header.version.invalid` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.header.version.required` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.messages.assistant_prefill` | 0 | anthropic.messages.basic |
| ERROR | CORE | `model:wiremock-broken-model` | `anthropic.messages.basic` | 1 |  |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.messages.content.string` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.messages.content.text_blocks` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.messages.max_tokens` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.messages.metadata` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.messages.multi_turn` | 0 | anthropic.messages.basic |
| SKIP | CORE | `model:wiremock-broken-model` | `anthropic.messages.schema` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.messages.stop_sequences` | 0 | anthropic.messages.basic |
| SKIP | CORE | `model:wiremock-broken-model` | `anthropic.messages.stream.event_type_match` | 0 | anthropic.messages.basic |
| SKIP | CORE | `model:wiremock-broken-model` | `anthropic.messages.stream.http` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.messages.stream.no_done_sentinel` | 0 | anthropic.messages.basic |
| SKIP | CORE | `model:wiremock-broken-model` | `anthropic.messages.stream.sequence` | 0 | anthropic.messages.basic |
| SKIP | CORE | `model:wiremock-broken-model` | `anthropic.messages.stream.text` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.messages.stream.usage` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.messages.system.top_level` | 0 | anthropic.messages.basic |
| SKIP | CORE | `model:wiremock-broken-model` | `anthropic.messages.usage` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.models.retrieve` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.structured.json_schema` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.structured.stop_reason` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tokens.count.basic` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tokens.count.consistency` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tokens.count.schema` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tokens.count.system` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tokens.count.tools` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tools.any` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tools.auto` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tools.continuation` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tools.input_schema` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tools.none` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tools.parallel` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tools.specific` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tools.stream` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tools.strict` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tools.strict_schema` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.vision.image_base64` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.vision.image_schema` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.vision.usage` | 0 | anthropic.messages.basic |
| SKIP | OPTIONAL | `provider:http://127.0.0.1:32786/anthropic` | `anthropic.models.list.pagination` | 1 | anthropic.models.list.schema |
| FAIL | CORE | `provider:http://127.0.0.1:32786/anthropic` | `anthropic.models.list.schema` | 1 |  |
| WARN | OPTIONAL | `provider:http://127.0.0.1:32786/anthropic` | `anthropic.official-sdk.models.list` | 3 |  |

### Models

| Model | Verdict | PASS | FAIL | WARN | UNSUP | ERROR |
|---|---:|---:|---:|---:|---:|---:|
| `wiremock-broken-model` | INCONCLUSIVE | 0 | 0 | 0 | 0 | 1 |

#### Model details: `wiremock-broken-model`

| Module | Capability | Requested | Applicability | Execution | Reason |
|---|---|---:|---:|---:|---|
| `anthropic.messages` | `anthropic.messages.text` | true | UNKNOWN | SKIPPED | model availability is inconclusive after transient preflight failure |
| `anthropic.tools` | `anthropic.messages.tools` | true | UNKNOWN | SKIPPED | model availability is inconclusive after transient preflight failure |
| `anthropic.token-counting` | `anthropic.messages.text` | true | UNKNOWN | SKIPPED | model availability is inconclusive after transient preflight failure |
| `anthropic.structured-output` | `anthropic.messages.structured_output` | true | UNKNOWN | SKIPPED | model availability is inconclusive after transient preflight failure |
| `anthropic.vision` | `anthropic.messages.image_input` | true | UNKNOWN | SKIPPED | model availability is inconclusive after transient preflight failure |
| `anthropic.thinking` | `anthropic.messages.thinking.adaptive,anthropic.messages.thinking.manual` | false | UNKNOWN | SKIPPED | model availability is inconclusive after transient preflight failure |

### Evidence classification

Classification describes the evidence for a non-pass result; it is not certification or a change to the canonical verdict.

| Check | Status | Classification | Evidence |
|---|---:|---|---|
| `anthropic.models.list.schema` | FAIL | `provider_nonconformance` | applicable checker requirement did not pass |
| `anthropic.messages.basic` | ERROR | `provider_nonconformance` | applicable checker requirement did not pass |

### Extension suites

Built-in extension transport counts are separate from native latency percentiles. Safe rate and concurrency show the target pacer's state after extension execution; executable extensions report only validated suite evidence.

| Logical | Attempts | Successful | 429 | Retry attempts | Recovered | Exhausted | Backoff | Safe req/s | Concurrency |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 1 | 1 | 1 | 0 | 0 | 0 | 0 | 0ms | 0.50 req/s | 1 |

#### Anthropic Go SDK v1.68.0

| Suite | Subject | Status | Classification | Duration | Summary |
|---|---|---:|---|---:|---|
| `models.list` | `provider:http://127.0.0.1:32786/anthropic` | PASS | – | 2ms | Anthropic Go SDK sent and parsed the suite |

## Target: Google GenAI

- **Target ID:** `google-genai`
- **Profile:** `google-genai-compat@2026-09-03`
- **Requested coverage:** 12 routes; 10 executed, 0 unresolved (0 core, 0 optional), 2 excluded.
- **API version:** `v1beta`
- **External checks:** [Google Gen AI Go SDK v1.71.0](https://github.com/googleapis/go-genai/releases/tag/v1.71.0)
- **Note:** Google Gen AI Go SDK v1.71.0 does not expose public Interactions API methods; no SDK Interactions suite is claimed.
- **Discovered models:** 1
- **Selected models:** `wiremock-broken-model`

### Actions

| PRI | Recommendation | Owner | Expected impact | Confidence |
|:---:|---|---|---|---:|
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Malformed JSON handling is not consistently client-side

**Change or investigate:**

1. Reject every malformed JSON request at the gateway boundary with a stable 4xx Google error envelope.

**Verification targets:**

- Requirements: `google.error.invalid_json`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error makes the negative probe inconclusive

**Change or investigate:**

1. Return a Google RPC-style `error` object with a client-error HTTP status for unknown models.

**Verification targets:**

- Requirements: `google.error.invalid_model`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.interactions.basic`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Google stream could not be evaluated

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.interactions.stream.http`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.embeddings.basic`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.generate-content.basic`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Google stream could not be evaluated

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.generate-content.stream.http`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.generate-content.structured-output`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.generate-content.tools`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.interactions.structured-output`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.interactions.tools`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.models.get`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.tokens.count.basic`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: []

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `official-sdk.google-genai.count-tokens`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: []

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `official-sdk.google-genai.embeddings`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: []

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `official-sdk.google-genai.generate-content`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: []

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `official-sdk.google-genai.generate-content-stream`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: []

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `official-sdk.google-genai.models.get`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: []

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `official-sdk.google-genai.structured-output`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: []

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `official-sdk.google-genai.tools`

### Canonical requirements needing attention

| Status | Level | Subject | Requirement | Evidence | Blocked by |
|---:|---:|---|---|---:|---|
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.embeddings.basic` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `google.error.invalid_json` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `google.error.invalid_model` | 1 |  |
| SKIP | CORE | `model:wiremock-broken-model` | `google.error.shape` | 1 | google.error.invalid_model |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.basic` | 1 |  |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.output-text` | 1 | google.generate-content.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.schema` | 1 | google.generate-content.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.stream.finish_reason` | 1 | google.generate-content.stream.sequence |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.stream.http` | 1 |  |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.stream.schema` | 1 | google.generate-content.stream.sequence |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.stream.sequence` | 1 | google.generate-content.stream.http |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.stream.text` | 1 | google.generate-content.stream.sequence |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.stream.usage` | 1 | google.generate-content.stream.sequence |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.structured-output` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.tools` | 1 |  |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.usage` | 1 | google.generate-content.basic |
| ERROR | CORE | `model:wiremock-broken-model` | `google.interactions.basic` | 1 |  |
| SKIP | CORE | `model:wiremock-broken-model` | `google.interactions.output_text` | 1 | google.interactions.basic |
| SKIP | CORE | `model:wiremock-broken-model` | `google.interactions.schema` | 1 | google.interactions.basic |
| SKIP | CORE | `model:wiremock-broken-model` | `google.interactions.status` | 1 | google.interactions.basic |
| SKIP | CORE | `model:wiremock-broken-model` | `google.interactions.store_false` | 1 | google.interactions.basic |
| SKIP | CORE | `model:wiremock-broken-model` | `google.interactions.stream.completed` | 1 | google.interactions.stream.sequence |
| SKIP | CORE | `model:wiremock-broken-model` | `google.interactions.stream.done` | 1 | google.interactions.stream.sequence |
| SKIP | CORE | `model:wiremock-broken-model` | `google.interactions.stream.event_type_match` | 1 | google.interactions.stream.sequence |
| ERROR | CORE | `model:wiremock-broken-model` | `google.interactions.stream.http` | 1 |  |
| SKIP | CORE | `model:wiremock-broken-model` | `google.interactions.stream.sequence` | 1 | google.interactions.stream.http |
| SKIP | CORE | `model:wiremock-broken-model` | `google.interactions.stream.syntax` | 1 | google.interactions.stream.http |
| SKIP | CORE | `model:wiremock-broken-model` | `google.interactions.stream.text` | 1 | google.interactions.stream.sequence |
| SKIP | CORE | `model:wiremock-broken-model` | `google.interactions.stream.usage` | 1 | google.interactions.stream.sequence |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.interactions.structured-output` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.interactions.tools` | 1 |  |
| SKIP | CORE | `model:wiremock-broken-model` | `google.interactions.usage` | 1 | google.interactions.basic |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.models.get` | 1 |  |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `google.models.metadata.limits` | 1 | google.models.get |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `google.models.metadata.methods` | 1 | google.models.get |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `google.models.metadata.thinking` | 1 | google.models.get |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.tokens.count.basic` | 1 |  |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `google.tokens.count.schema` | 1 | google.tokens.count.basic |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `official-sdk.google-genai.count-tokens` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `official-sdk.google-genai.embeddings` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `official-sdk.google-genai.generate-content` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `official-sdk.google-genai.generate-content-stream` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `official-sdk.google-genai.models.get` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `official-sdk.google-genai.structured-output` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `official-sdk.google-genai.tools` | 1 |  |

### Models

| Model | Verdict | PASS | FAIL | WARN | UNSUP | ERROR |
|---|---:|---:|---:|---:|---:|---:|
| `wiremock-broken-model` | INCONCLUSIVE | 0 | 0 | 0 | 0 | 20 |

#### Model details: `wiremock-broken-model`

| Module | Capability | Requested | Applicability | Execution | Reason |
|---|---|---:|---:|---:|---|
| `google-genai.interactions` | `google-genai.interactions.text` | true | APPLICABLE | EXECUTED | model is explicitly eligible for the selected Interactions surface |
| `google-genai.interactions-stream` | `google-genai.interactions.text` | true | APPLICABLE | EXECUTED | model is explicitly eligible for the selected Interactions surface |
| `google-genai.interactions-tools` | `google-genai.interactions.tools` | true | APPLICABLE | EXECUTED | text model is eligible for client-side Interactions tool probing |
| `google-genai.interactions-structured-output` | `google-genai.interactions.structured-output` | true | APPLICABLE | EXECUTED | text model is eligible for Interactions structured-output probing |
| `google-genai.interactions-vision` | `google-genai.interactions.image-input` | true | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.image-input: google-genai.count-tokens, google-genai.embed-content, google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools, google-genai.interactions.structured-output, google-genai.interactions.text, google-genai.interactions.tools |
| `google-genai.interactions-thinking` | `google-genai.interactions.thinking` | false | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.thinking: google-genai.count-tokens, google-genai.embed-content, google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools, google-genai.interactions.structured-output, google-genai.interactions.text, google-genai.interactions.tools |
| `google-genai.interactions-stateful` | `google-genai.interactions.stateful` | false | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.stateful: google-genai.count-tokens, google-genai.embed-content, google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools, google-genai.interactions.structured-output, google-genai.interactions.text, google-genai.interactions.tools |
| `google-genai.generate-content` | `google-genai.generate-content.text` | true | APPLICABLE | EXECUTED | Google metadata declares generateContent support |
| `google-genai.generate-content-stream` | `google-genai.generate-content.text` | true | APPLICABLE | EXECUTED | Google metadata declares generateContent support |
| `google-genai.generate-content-tools` | `google-genai.generate-content.tools` | true | APPLICABLE | EXECUTED | generateContent is eligible for client-side tool probing |
| `google-genai.generate-content-structured-output` | `google-genai.generate-content.structured-output` | true | APPLICABLE | EXECUTED | generateContent is eligible for local structured-output probing |
| `google-genai.token-counting` | `google-genai.count-tokens` | true | APPLICABLE | EXECUTED | Google metadata declares countTokens support |
| `google-genai.embeddings` | `google-genai.embed-content` | true | APPLICABLE | EXECUTED | Google metadata declares embedContent support |
| `google-genai.vision` | `google-genai.generate-content.image-input` | true | NOT-APPLICABLE | – | classified capabilities do not include google-genai.generate-content.image-input: google-genai.count-tokens, google-genai.embed-content, google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools, google-genai.interactions.structured-output, google-genai.interactions.text, google-genai.interactions.tools |
| `google-genai.thinking` | `google-genai.generate-content.thinking` | false | NOT-APPLICABLE | – | classified capabilities do not include google-genai.generate-content.thinking: google-genai.count-tokens, google-genai.embed-content, google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools, google-genai.interactions.structured-output, google-genai.interactions.text, google-genai.interactions.tools |

### Evidence classification

Classification describes the evidence for a non-pass result; it is not certification or a change to the canonical verdict.

| Check | Status | Classification | Evidence |
|---|---:|---|---|
| `google.embeddings.basic` | ERROR | `provider_nonconformance` | applicable checker requirement did not pass |
| `google.error.invalid_json` | ERROR | `insufficient_evidence` | HTTP error has no safe response shape evidence |
| `google.error.invalid_model` | ERROR | `insufficient_evidence` | HTTP error has no safe response shape evidence |
| `google.generate-content.basic` | ERROR | `provider_nonconformance` | applicable checker requirement did not pass |
| `google.generate-content.stream.http` | ERROR | `provider_nonconformance` | applicable checker requirement did not pass |
| `google.generate-content.structured-output` | ERROR | `behavioral_observation` | semantic model assertion is not provider nonconformance by itself |
| `google.generate-content.tools` | ERROR | `provider_nonconformance` | applicable checker requirement did not pass |
| `google.interactions.basic` | ERROR | `provider_nonconformance` | applicable checker requirement did not pass |
| `google.interactions.stream.http` | ERROR | `provider_nonconformance` | applicable checker requirement did not pass |
| `google.interactions.structured-output` | ERROR | `behavioral_observation` | semantic model assertion is not provider nonconformance by itself |
| `google.interactions.tools` | ERROR | `provider_nonconformance` | applicable checker requirement did not pass |
| `google.models.get` | ERROR | `provider_nonconformance` | applicable checker requirement did not pass |
| `google.tokens.count.basic` | ERROR | `provider_nonconformance` | applicable checker requirement did not pass |

### Extension suites

Built-in extension transport counts are separate from native latency percentiles. Safe rate and concurrency show the target pacer's state after extension execution; executable extensions report only validated suite evidence.

| Logical | Attempts | Successful | 429 | Retry attempts | Recovered | Exhausted | Backoff | Safe req/s | Concurrency |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 8 | 22 | 1 | 0 | 14 | 0 | 7 | 2100ms | 0.50 req/s | 1 |

#### Google Gen AI Go SDK v1.71.0

| Suite | Subject | Status | Classification | Duration | Summary |
|---|---|---:|---|---:|---|
| `count-tokens` | `model:wiremock-broken-model` | ERROR | `provider_nonconformance` | 2003ms | Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: [] |
| `embeddings` | `model:wiremock-broken-model` | ERROR | `provider_nonconformance` | 3375ms | Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: [] |
| `generate-content` | `model:wiremock-broken-model` | ERROR | `provider_nonconformance` | 3378ms | Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: [] |
| `generate-content-stream` | `model:wiremock-broken-model` | ERROR | `provider_nonconformance` | 3378ms | Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: [] |
| `models.get` | `model:wiremock-broken-model` | ERROR | `provider_nonconformance` | 1998ms | Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: [] |
| `structured-output` | `model:wiremock-broken-model` | ERROR | `behavioral_observation` | 3378ms | Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: [] |
| `tools` | `model:wiremock-broken-model` | ERROR | `provider_nonconformance` | 2003ms | Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: [] |
| `models.list` | `provider:http://127.0.0.1:32786/google` | PASS | – | 3ms | Google Gen AI Go SDK sent and parsed the suite |

## Target: OpenAI

- **Target ID:** `openai`
- **Profile:** `openai-compat@2026-09-01`
- **Requested coverage:** 1 routes; 0 executed, 1 unresolved (1 core, 0 optional), 0 excluded.
- **External checks:** [OpenAI Go SDK v3.54.0](https://github.com/openai/openai-go/releases/tag/v3.54.0)
- **Discovered models:** 1
- **Selected models:** `wiremock-broken-model`

### Actions

| PRI | Recommendation | Owner | Expected impact | Confidence |
|:---:|---|---|---|---:|
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** model availability could not be determined after a transient preflight failure

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `chat.basic`

### Canonical requirements needing attention

| Status | Level | Subject | Requirement | Evidence | Blocked by |
|---:|---:|---|---|---:|---|
| ERROR | CORE | `model:wiremock-broken-model` | `chat.basic` | 1 |  |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `chat.content_parts.text` | 0 | chat.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `chat.logprobs` | 0 | chat.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `chat.multi_turn` | 0 | chat.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `chat.role.developer` | 0 | chat.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `chat.role.system` | 0 | chat.basic |
| SKIP | CORE | `model:wiremock-broken-model` | `chat.schema` | 0 | chat.basic |
| SKIP | CORE | `model:wiremock-broken-model` | `chat.stream.sse` | 0 | chat.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `chat.stream.usage` | 0 | chat.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `chat.structured.json_object` | 0 | chat.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `chat.structured.json_schema` | 0 | chat.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `chat.tools.auto` | 0 | chat.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `chat.tools.continuation` | 0 | chat.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `chat.tools.forced` | 0 | chat.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `chat.tools.none` | 0 | chat.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `chat.tools.required` | 0 | chat.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `chat.tools.stream` | 0 | chat.basic |
| SKIP | CORE | `model:wiremock-broken-model` | `chat.usage` | 0 | chat.basic |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `models.retrieve` | 0 | chat.basic |

### Models

| Model | Verdict | PASS | FAIL | WARN | UNSUP | ERROR |
|---|---:|---:|---:|---:|---:|---:|
| `wiremock-broken-model` | INCONCLUSIVE | 0 | 0 | 0 | 0 | 1 |

#### Model details: `wiremock-broken-model`

| Module | Capability | Requested | Applicability | Execution | Reason |
|---|---|---:|---:|---:|---|
| `chat-completions` | `chat.completions.text` | true | UNKNOWN | SKIPPED | model availability is inconclusive after transient preflight failure |
| `images-generations` | `images.generations` | false | UNKNOWN | SKIPPED | model availability is inconclusive after transient preflight failure |

### Evidence classification

Classification describes the evidence for a non-pass result; it is not certification or a change to the canonical verdict.

| Check | Status | Classification | Evidence |
|---|---:|---|---|
| `chat.basic` | ERROR | `provider_nonconformance` | applicable checker requirement did not pass |

### Extension suites

Built-in extension transport counts are separate from native latency percentiles. Safe rate and concurrency show the target pacer's state after extension execution; executable extensions report only validated suite evidence.

| Logical | Attempts | Successful | 429 | Retry attempts | Recovered | Exhausted | Backoff | Safe req/s | Concurrency |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 1 | 1 | 1 | 0 | 0 | 0 | 0 | 0ms | 0.50 req/s | 1 |

#### OpenAI Go SDK v3.54.0

| Suite | Subject | Status | Classification | Duration | Summary |
|---|---|---:|---|---:|---|
| `models.list` | `provider:http://127.0.0.1:32786/openai/v1` | PASS | – | 1ms | OpenAI Go SDK sent and parsed the suite |

## Observed API usage

Provider-reported token usage is normalized to common input and output columns. For Anthropic, cached input is reported separately from uncached input. Thinking/reasoning tokens are a subset of output and are not added twice.

| Target | Input | Output | Cache creation | Cache read | Thinking/reasoning | Total |
|---|---:|---:|---:|---:|---:|---:|
| `anthropic` | 0 | 0 | 0 | 0 | 0 | **0** |
| `google-genai` | 0 | 0 | 0 | 0 | 0 | **0** |
| `openai` | 0 | 0 | 0 | 0 | 0 | **0** |
| **All targets** | **0** | **0** | **0** | **0** | **0** | **0** |
