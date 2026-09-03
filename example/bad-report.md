# AIQA compatibility report

_measures observed API behavior, a passing report is not a security audit or an official certification_

- **Generated:** 2026-09-03 17:27:28Z
- **Tool:** `aiqa 1.0.0-alpha.3`
- **Service:** [127.0.0.1:32775](http://127.0.0.1:32775)
- **Suites:** `compat,ext-suite`
- **Timeout:** `1m0s` per request
- **External checks:** [Official SDK – Anthropic Go v1.68.0](https://github.com/anthropics/anthropic-sdk-go/releases/tag/v1.68.0), [Official SDK – Google Gen AI Go v1.71.0](https://github.com/googleapis/go-genai/releases/tag/v1.71.0), [Official SDK – OpenAI Go v3.54.0](https://github.com/openai/openai-go/releases/tag/v3.54.0)

## Targets

| Target | Verdict | Core | Optional | Evidence | Models | Actions |
|---|---:|---:|---:|---:|---:|---:|
| `anthropic` | INCONCLUSIVE | 2/14 (14.3%) | 1/44 (2.3%) | 3/58 (5.2%) | 1 | 54 |
| `google-genai` | INCONCLUSIVE | 5/22 (22.7%) | 1/29 (3.4%) | 6/51 (11.8%) | 1 | 45 |
| `openai` | INCONCLUSIVE | 2/8 (25.0%) | 1/27 (3.7%) | 4/35 (11.4%) | 1 | 31 |

**Run verdict:** INCONCLUSIVE

Targets are assessed independently. Percentages are not combined into a cross-protocol score.

## Rate-limit and latency

| Target | Successful | 429 | Retries | Backoff | Safe req/s | Safe concurrency | Mean | P50 | P95 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| `anthropic` | 1 | 0 | 0 | 0ms | 0.50 | 1 | 28ms | 28ms | 28ms |
| `google-genai` | 1 | 0 | 0 | 0ms | 0.50 | 1 | 3ms | 3ms | 3ms |
| `openai` | 1 | 0 | 0 | 0ms | 0.50 | 1 | 2ms | 2ms | 2ms |

## Target: Anthropic

- **Target ID:** `anthropic`
- **Profile:** `anthropic-compat@2026-09-01`
- **API version:** `2023-06-01`
- **External checks:** [Official SDK – Anthropic Go v1.68.0](https://github.com/anthropics/anthropic-sdk-go/releases/tag/v1.68.0)
- **Discovered models:** 1

### Actions

| PRI | Recommendation | Owner | Expected impact | Confidence |
|:---:|---|---|---|---:|
| P0 | Authenticate Anthropic requests with x-api-key | `provider_gateway` | recover 1 core | LOW |
| P0 | Accept the pinned Anthropic API version | `provider_gateway` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
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

#### P0 · Authenticate Anthropic requests with x-api-key

**Why this matters:** The stable Anthropic profile intentionally tests x-api-key rather than OpenAI Bearer authentication.

**Observed:** x-api-key authentication was accepted

**Change or investigate:**

1. Read the credential from x-api-key and keep it isolated from OpenAI Authorization handling.

**Verification targets:**

- Requirements: `anthropic.auth.x_api_key`

#### P0 · Accept the pinned Anthropic API version

**Why this matters:** The stable profile requires anthropic-version: 2023-06-01 on every positive Anthropic request.

**Observed:** anthropic-version 2023-06-01 was accepted

**Change or investigate:**

1. Accept anthropic-version: 2023-06-01 and route the request under that stable contract.
2. Do not translate this header into OpenAI request-version semantics.

**Verification targets:**

- Requirements: `anthropic.header.version.accepted`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Malformed JSON handling is not consistently client-side

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.error.invalid_json`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error makes the negative probe inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.error.invalid_model`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.error.shape`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.messages.basic`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.messages.schema`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Known SSE event names match JSON event types

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.messages.stream.event_type_match`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Anthropic stream could not be evaluated

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.messages.stream.http`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.messages.stream.sequence`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Streamed text reconstructed the requested marker

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.messages.stream.text`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.messages.usage`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error makes the negative probe inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.error.invalid_auth`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Invalid field types are rejected with a client error

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.error.invalid_types`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.error.request_id`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error makes the negative probe inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.header.version.invalid`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error makes the negative probe inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.header.version.required`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.messages.assistant_prefill`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** String-form message content is accepted

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.messages.content.string`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.messages.content.text_blocks`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.messages.max_tokens`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.messages.metadata`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.messages.multi_turn`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.messages.stop_sequences`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Stream ended with message_stop and no [DONE] sentinel

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.messages.stream.no_done_sentinel`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Streaming usage was reconstructed

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.messages.stream.usage`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.messages.system.top_level`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.models.retrieve`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** POST "http://127.0.0.1:32775/anthropic/v1/messages/count_tokens": 503 Service Unavailable {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.official-sdk.count_tokens`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** invalid model returned unexpected HTTP 503: POST "http://127.0.0.1:32775/anthropic/v1/messages": 503 Service Unavailable {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.official-sdk.error.responses`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** POST "http://127.0.0.1:32775/anthropic/v1/messages": 503 Service Unavailable {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.official-sdk.messages`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** POST "http://127.0.0.1:32775/anthropic/v1/messages": 503 Service Unavailable {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.official-sdk.messages.multi_turn`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** POST "http://127.0.0.1:32775/anthropic/v1/messages": 503 Service Unavailable {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.official-sdk.messages.stream`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** POST "http://127.0.0.1:32775/anthropic/v1/messages": 503 Service Unavailable {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.official-sdk.messages.system`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Get "http://127.0.0.1:32775/anthropic/v1/models/wiremock-broken-model": cannot retry SDK request with a non-replayable body

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.official-sdk.models.get`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** POST "http://127.0.0.1:32775/anthropic/v1/messages": 503 Service Unavailable {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.official-sdk.structured_output`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** POST "http://127.0.0.1:32775/anthropic/v1/messages": 503 Service Unavailable {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.official-sdk.tools`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** POST "http://127.0.0.1:32775/anthropic/v1/messages": 503 Service Unavailable {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.official-sdk.tools.stream`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.structured.json_schema`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Structured output completed with compatible stop semantics

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.structured.stop_reason`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.tokens.count.basic`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.tokens.count.consistency`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Count Tokens response has a positive integer input_tokens

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.tokens.count.schema`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.tokens.count.system`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.tokens.count.tools`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.tools.any`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.tools.auto`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.tools.input_schema`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.tools.none`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.tools.parallel`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.tools.specific`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Streaming tool call could not be evaluated

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.tools.stream`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.tools.strict`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Provider/runtime error HTTP 503 makes compatibility inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `anthropic.tools.strict_schema`

### Canonical requirements needing attention

| Status | Level | Subject | Requirement | Evidence | Blocked by |
|---:|---:|---|---|---:|---|
| ERROR | CORE | `model:wiremock-broken-model` | `anthropic.auth.x_api_key` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.error.invalid_auth` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `anthropic.error.invalid_json` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `anthropic.error.invalid_model` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.error.invalid_types` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.error.request_id` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `anthropic.error.shape` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `anthropic.header.version.accepted` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.header.version.invalid` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.header.version.required` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.messages.assistant_prefill` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `anthropic.messages.basic` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.messages.content.string` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.messages.content.text_blocks` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.messages.max_tokens` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.messages.metadata` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.messages.multi_turn` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `anthropic.messages.schema` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.messages.stop_sequences` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `anthropic.messages.stream.event_type_match` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `anthropic.messages.stream.http` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.messages.stream.no_done_sentinel` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `anthropic.messages.stream.sequence` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `anthropic.messages.stream.text` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.messages.stream.usage` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.messages.system.top_level` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `anthropic.messages.usage` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.models.retrieve` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.official-sdk.count_tokens` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.official-sdk.error.responses` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.official-sdk.messages` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.official-sdk.messages.multi_turn` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.official-sdk.messages.stream` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.official-sdk.messages.system` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.official-sdk.models.get` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.official-sdk.structured_output` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.official-sdk.tools` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.official-sdk.tools.stream` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.structured.json_schema` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.structured.stop_reason` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tokens.count.basic` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tokens.count.consistency` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tokens.count.schema` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tokens.count.system` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tokens.count.tools` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tools.any` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tools.auto` | 1 |  |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tools.continuation` | 1 | anthropic.tools.specific |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tools.input_schema` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tools.none` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tools.parallel` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tools.specific` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tools.stream` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tools.strict` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `anthropic.tools.strict_schema` | 1 |  |

### Models

| Model | Verdict | PASS | FAIL | WARN | UNSUP | ERROR |
|---|---:|---:|---:|---:|---:|---:|
| `wiremock-broken-model` | INCONCLUSIVE | 0 | 0 | 0 | 0 | 54 |

#### Model details: `wiremock-broken-model`

| Module | Capability | Applicability | Execution | Reason |
|---|---|---:|---:|---|
| `anthropic.messages` | `anthropic.messages.text` | APPLICABLE | EXECUTED | Anthropic inventory model defaults to Messages text |
| `anthropic.tools` | `anthropic.messages.tools` | APPLICABLE | EXECUTED | tools are safe to probe on a Messages text model |
| `anthropic.token-counting` | `anthropic.messages.text` | APPLICABLE | EXECUTED | Anthropic inventory model defaults to Messages text |
| `anthropic.structured-output` | `anthropic.messages.structured_output` | APPLICABLE | EXECUTED | structured output is safe to probe on a Messages text model |
| `anthropic.vision` | `anthropic.messages.image_input` | NOT-APPLICABLE | – | classified capabilities do not include anthropic.messages.image_input: anthropic.messages.structured_output, anthropic.messages.text, anthropic.messages.tools |
| `anthropic.thinking` | `anthropic.messages.thinking.adaptive,anthropic.messages.thinking.manual` | NOT-APPLICABLE | – | classified capabilities do not include anthropic.messages.thinking.adaptive, anthropic.messages.thinking.manual: anthropic.messages.structured_output, anthropic.messages.text, anthropic.messages.tools |

### Extension suites

Built-in extension transport counts are separate from native latency percentiles. Safe rate and concurrency show the target pacer's state after extension execution; executable extensions report only validated suite evidence.

| Successful | 429 | Recovered retries | Backoff | Safe req/s | Safe concurrency |
|---:|---:|---:|---:|---:|---:|
| 1 | 0 | 0 | 0ms | 0.50 | 1 |

#### Official SDK – Anthropic Go v1.68.0

| Suite | Subject | Status | Duration | Summary |
|---|---|---:|---:|---|
| `count-tokens` | `model:wiremock-broken-model` | ERROR | 2863ms | POST "http://127.0.0.1:32775/anthropic/v1/messages/count_tokens": 503 Service Unavailable {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}} |
| `error.responses` | `model:wiremock-broken-model` | ERROR | 3689ms | invalid model returned unexpected HTTP 503: POST "http://127.0.0.1:32775/anthropic/v1/messages": 503 Service Unavailable {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}} |
| `messages` | `model:wiremock-broken-model` | ERROR | 3172ms | POST "http://127.0.0.1:32775/anthropic/v1/messages": 503 Service Unavailable {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}} |
| `messages.multi-turn` | `model:wiremock-broken-model` | ERROR | 2833ms | POST "http://127.0.0.1:32775/anthropic/v1/messages": 503 Service Unavailable {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}} |
| `messages.stream` | `model:wiremock-broken-model` | ERROR | 3377ms | POST "http://127.0.0.1:32775/anthropic/v1/messages": 503 Service Unavailable {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}} |
| `messages.system` | `model:wiremock-broken-model` | ERROR | 3485ms | POST "http://127.0.0.1:32775/anthropic/v1/messages": 503 Service Unavailable {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}} |
| `models.get` | `model:wiremock-broken-model` | ERROR | 1687ms | Get "http://127.0.0.1:32775/anthropic/v1/models/wiremock-broken-model": cannot retry SDK request with a non-replayable body |
| `structured-output` | `model:wiremock-broken-model` | ERROR | 3692ms | POST "http://127.0.0.1:32775/anthropic/v1/messages": 503 Service Unavailable {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}} |
| `tools` | `model:wiremock-broken-model` | ERROR | 3375ms | POST "http://127.0.0.1:32775/anthropic/v1/messages": 503 Service Unavailable {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}} |
| `tools.stream` | `model:wiremock-broken-model` | ERROR | 2967ms | POST "http://127.0.0.1:32775/anthropic/v1/messages": 503 Service Unavailable {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}} |
| `models.list` | `provider:http://127.0.0.1:32775/anthropic` | PASS | 4ms | Official Anthropic Go SDK sent and parsed the suite |

## Target: Google GenAI

- **Target ID:** `google-genai`
- **Profile:** `google-genai-compat@2026-09-03`
- **API version:** `v1beta`
- **External checks:** [Official SDK – Google Gen AI Go v1.71.0](https://github.com/googleapis/go-genai/releases/tag/v1.71.0)
- **Note:** Official Google Gen AI Go SDK v1.71.0 does not expose public Interactions API methods; no SDK Interactions suite is claimed.
- **Discovered models:** 1

### Actions

| PRI | Recommendation | Owner | Expected impact | Confidence |
|:---:|---|---|---|---:|
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
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
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P3 | Review the affected requirement | `unknown` | recover 1 optional | LOW |
| P3 | Review the affected requirement | `unknown` | recover 1 optional | LOW |
| P3 | Review the affected requirement | `unknown` | recover 1 optional | LOW |

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

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.error.shape`

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

**Observed:** Interaction output text was extracted without exposing private thought content

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.interactions.output_text`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.interactions.schema`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Interaction status is a recognized terminal state

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.interactions.status`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Stateless Interaction request used store=false

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.interactions.store_false`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Interactions stream completion evidence is inconclusive because the sequence probe did not pass

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.interactions.stream.completed`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Interactions stream terminal-marker evidence is inconclusive because the sequence probe did not pass

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.interactions.stream.done`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Known SSE event names match JSON event types

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.interactions.stream.event_type_match`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Google stream could not be evaluated

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.interactions.stream.http`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.interactions.stream.sequence`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Interactions SSE framing and JSON data are valid

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.interactions.stream.syntax`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Interactions stream reconstructed response text

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.interactions.stream.text`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Interactions stream usage evidence is inconclusive because the sequence probe did not pass

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.interactions.stream.usage`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Interaction usage was normalized

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.interactions.usage`

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

**Observed:** GenerateContent output text was extracted

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.generate-content.output-text`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.generate-content.schema`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** GenerateContent finish-reason evidence is inconclusive because the sequence probe did not pass

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.generate-content.stream.finish_reason`

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

**Observed:** GenerateContent stream schema evidence is inconclusive because the stream probe did not pass

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.generate-content.stream.schema`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.generate-content.stream.sequence`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** GenerateContent stream reconstructed response text

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.generate-content.stream.text`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** GenerateContent stream usage evidence is inconclusive because the sequence probe did not pass

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.generate-content.stream.usage`

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

**Observed:** GenerateContent usage metadata was normalized

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.generate-content.usage`

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

**Observed:** countTokens response contains a numeric totalTokens

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `google.tokens.count.schema`

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

#### P3 · Review the affected requirement

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Model metadata was unavailable

**Change or investigate:**

1. Inspect the linked structured evidence for the first incompatible boundary.
2. Make the implementation satisfy the canonical requirement without weakening validation.

**Verification targets:**

- Requirements: `google.models.metadata.limits`

#### P3 · Review the affected requirement

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Model metadata was unavailable

**Change or investigate:**

1. Inspect the linked structured evidence for the first incompatible boundary.
2. Make the implementation satisfy the canonical requirement without weakening validation.

**Verification targets:**

- Requirements: `google.models.metadata.methods`

#### P3 · Review the affected requirement

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Model metadata was unavailable

**Change or investigate:**

1. Inspect the linked structured evidence for the first incompatible boundary.
2. Make the implementation satisfy the canonical requirement without weakening validation.

**Verification targets:**

- Requirements: `google.models.metadata.thinking`

### Canonical requirements needing attention

| Status | Level | Subject | Requirement | Evidence | Blocked by |
|---:|---:|---|---|---:|---|
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.embeddings.basic` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `google.error.invalid_json` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `google.error.invalid_model` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `google.error.shape` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.basic` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.output-text` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.schema` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.stream.finish_reason` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.stream.http` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.stream.schema` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.stream.sequence` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.stream.text` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.stream.usage` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.structured-output` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.tools` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.generate-content.usage` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `google.interactions.basic` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `google.interactions.output_text` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `google.interactions.schema` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `google.interactions.status` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `google.interactions.store_false` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `google.interactions.stream.completed` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `google.interactions.stream.done` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `google.interactions.stream.event_type_match` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `google.interactions.stream.http` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `google.interactions.stream.sequence` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `google.interactions.stream.syntax` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `google.interactions.stream.text` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `google.interactions.stream.usage` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.interactions.structured-output` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.interactions.tools` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `google.interactions.usage` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.models.get` | 1 |  |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `google.models.metadata.limits` | 1 |  |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `google.models.metadata.methods` | 1 |  |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `google.models.metadata.thinking` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.tokens.count.basic` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `google.tokens.count.schema` | 1 |  |
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
| `wiremock-broken-model` | INCONCLUSIVE | 0 | 0 | 0 | 0 | 42 |

#### Model details: `wiremock-broken-model`

| Module | Capability | Applicability | Execution | Reason |
|---|---|---:|---:|---|
| `google-genai.interactions` | `google-genai.interactions.text` | APPLICABLE | EXECUTED | model is explicitly eligible for the selected Interactions surface |
| `google-genai.interactions-stream` | `google-genai.interactions.text` | APPLICABLE | EXECUTED | model is explicitly eligible for the selected Interactions surface |
| `google-genai.interactions-tools` | `google-genai.interactions.tools` | APPLICABLE | EXECUTED | text model is eligible for client-side Interactions tool probing |
| `google-genai.interactions-structured-output` | `google-genai.interactions.structured-output` | APPLICABLE | EXECUTED | text model is eligible for Interactions structured-output probing |
| `google-genai.interactions-vision` | `google-genai.interactions.image-input` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.image-input: google-genai.count-tokens, google-genai.embed-content, google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools, google-genai.interactions.structured-output, google-genai.interactions.text, google-genai.interactions.tools |
| `google-genai.interactions-thinking` | `google-genai.interactions.thinking` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.thinking: google-genai.count-tokens, google-genai.embed-content, google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools, google-genai.interactions.structured-output, google-genai.interactions.text, google-genai.interactions.tools |
| `google-genai.interactions-stateful` | `google-genai.interactions.stateful` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.stateful: google-genai.count-tokens, google-genai.embed-content, google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools, google-genai.interactions.structured-output, google-genai.interactions.text, google-genai.interactions.tools |
| `google-genai.generate-content` | `google-genai.generate-content.text` | APPLICABLE | EXECUTED | Google metadata declares generateContent support |
| `google-genai.generate-content-stream` | `google-genai.generate-content.text` | APPLICABLE | EXECUTED | Google metadata declares generateContent support |
| `google-genai.generate-content-tools` | `google-genai.generate-content.tools` | APPLICABLE | EXECUTED | generateContent is eligible for client-side tool probing |
| `google-genai.generate-content-structured-output` | `google-genai.generate-content.structured-output` | APPLICABLE | EXECUTED | generateContent is eligible for local structured-output probing |
| `google-genai.token-counting` | `google-genai.count-tokens` | APPLICABLE | EXECUTED | Google metadata declares countTokens support |
| `google-genai.embeddings` | `google-genai.embed-content` | APPLICABLE | EXECUTED | Google metadata declares embedContent support |
| `google-genai.vision` | `google-genai.generate-content.image-input` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.generate-content.image-input: google-genai.count-tokens, google-genai.embed-content, google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools, google-genai.interactions.structured-output, google-genai.interactions.text, google-genai.interactions.tools |
| `google-genai.thinking` | `google-genai.generate-content.thinking` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.generate-content.thinking: google-genai.count-tokens, google-genai.embed-content, google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools, google-genai.interactions.structured-output, google-genai.interactions.text, google-genai.interactions.tools |

### Extension suites

Built-in extension transport counts are separate from native latency percentiles. Safe rate and concurrency show the target pacer's state after extension execution; executable extensions report only validated suite evidence.

| Successful | 429 | Recovered retries | Backoff | Safe req/s | Safe concurrency |
|---:|---:|---:|---:|---:|---:|
| 1 | 0 | 0 | 0ms | 0.50 | 1 |

#### Official SDK – Google Gen AI Go v1.71.0

| Suite | Subject | Status | Duration | Summary |
|---|---|---:|---:|---|
| `count-tokens` | `model:wiremock-broken-model` | ERROR | 3384ms | Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: [] |
| `embeddings` | `model:wiremock-broken-model` | ERROR | 1999ms | Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: [] |
| `generate-content` | `model:wiremock-broken-model` | ERROR | 2001ms | Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: [] |
| `generate-content-stream` | `model:wiremock-broken-model` | ERROR | 3382ms | Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: [] |
| `models.get` | `model:wiremock-broken-model` | ERROR | 2001ms | Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: [] |
| `structured-output` | `model:wiremock-broken-model` | ERROR | 3384ms | Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: [] |
| `tools` | `model:wiremock-broken-model` | ERROR | 3384ms | Error 503, Message: {"error":{"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}}, Status: 503 Service Unavailable, Details: [] |
| `models.list` | `provider:http://127.0.0.1:32775/google` | PASS | 2ms | Official Google Gen AI Go SDK sent and parsed the suite |

## Target: OpenAI

- **Target ID:** `openai`
- **Profile:** `openai-compat@2026-09-01`
- **External checks:** [Official SDK – OpenAI Go v3.54.0](https://github.com/openai/openai-go/releases/tag/v3.54.0)
- **Discovered models:** 1

### Actions

| PRI | Recommendation | Owner | Expected impact | Confidence |
|:---:|---|---|---|---:|
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 core | LOW |
| P0 | Review the affected requirement | `unknown` | recover 1 core | LOW |
| P0 | Review the affected requirement | `unknown` | recover 1 core | LOW |
| P1 | Reject invalid field types at the gateway boundary | `provider_gateway` | recover 1 optional; projected INCONCLUSIVE | HIGH |
| P1 | Correct the incompatible behavior | `unknown` | recover 1 optional; projected INCONCLUSIVE | LOW |
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
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |
| P2 | Rerun the affected requirement after execution recovers | `network_or_runtime` | recover 1 optional | LOW |

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** minimal chat completion returned provider HTTP 503; compatibility outcome is inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `chat.basic`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** malformed JSON variants returned provider HTTP 503; compatibility outcome is inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `chat.error.invalid_json`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** invalid model returned provider HTTP 503; compatibility outcome is inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `chat.error.invalid_model`

#### P0 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** stream returned provider HTTP 503; compatibility outcome is inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `chat.stream.sse`

#### P0 · Review the affected requirement

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** response schema was not checked because basic chat failed

**Change or investigate:**

1. Inspect the linked structured evidence for the first incompatible boundary.
2. Make the implementation satisfy the canonical requirement without weakening validation.

**Verification targets:**

- Requirements: `chat.schema`

#### P0 · Review the affected requirement

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** usage was not checked because basic chat failed

**Change or investigate:**

1. Inspect the linked structured evidence for the first incompatible boundary.
2. Make the implementation satisfy the canonical requirement without weakening validation.

**Verification targets:**

- Requirements: `chat.usage`

#### P1 · Reject invalid field types at the gateway boundary

**Why this matters:** Malformed client input should receive an OpenAI-shaped client error instead of becoming an upstream provider failure.

**Observed:** invalid request field types returned provider HTTP 503; compatibility outcome is inconclusive

**Change or investigate:**

1. Validate request field types before dispatching to the model backend.
2. Return HTTP 400 or 422 with an OpenAI-shaped error object.

**Verification targets:**

- Requirements: `chat.error.invalid_types`

#### P1 · Correct the incompatible behavior

**Why this matters:** The affected optional requirement reduces demonstrated interoperability or capability coverage.

**Observed:** invalid model returned unexpected HTTP 503

**Change or investigate:**

1. Inspect the linked structured evidence for the first incompatible boundary.
2. Make the implementation satisfy the canonical requirement without weakening validation.

**Verification targets:**

- Requirements: `official-sdk.openai.error.responses`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** text content-parts support could not be evaluated

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `chat.content_parts.text`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** invalid authentication returned provider HTTP 503; compatibility outcome is inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `chat.error.invalid_auth`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** logprobs could not be evaluated

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `chat.logprobs`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** multi-turn messages could not be evaluated

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `chat.multi_turn`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** developer role could not be evaluated

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `chat.role.developer`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** system role could not be evaluated

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `chat.role.system`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** stream returned provider HTTP 503; compatibility outcome is inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `chat.stream.usage`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** JSON object mode could not be evaluated

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `chat.structured.json_object`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** strict JSON schema could not be evaluated

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `chat.structured.json_schema`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** tool_choice=auto could not be evaluated

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `chat.tools.auto`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** forced tool call could not be evaluated

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `chat.tools.forced`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** tool_choice=none could not be evaluated

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `chat.tools.none`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** tool_choice=required could not be evaluated

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `chat.tools.required`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** streaming tool call returned provider HTTP 503; compatibility outcome is inconclusive

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `chat.tools.stream`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** model retrieval could not be evaluated

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `models.retrieve`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** POST "http://127.0.0.1:32775/openai/v1/chat/completions": 503 Service Unavailable {"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `official-sdk.openai.chat.completions`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** POST "http://127.0.0.1:32775/openai/v1/chat/completions": 503 Service Unavailable {"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `official-sdk.openai.chat.json`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** POST "http://127.0.0.1:32775/openai/v1/chat/completions": 503 Service Unavailable {"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `official-sdk.openai.chat.multi_turn`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** POST "http://127.0.0.1:32775/openai/v1/chat/completions": 503 Service Unavailable {"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `official-sdk.openai.chat.stream`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** POST "http://127.0.0.1:32775/openai/v1/chat/completions": 503 Service Unavailable {"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `official-sdk.openai.chat.stream.usage`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** POST "http://127.0.0.1:32775/openai/v1/chat/completions": 503 Service Unavailable {"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `official-sdk.openai.chat.tools`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** POST "http://127.0.0.1:32775/openai/v1/chat/completions": 503 Service Unavailable {"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"}

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `official-sdk.openai.chat.tools.stream`

#### P2 · Rerun the affected requirement after execution recovers

**Why this matters:** The run did not produce enough evidence to decide compatibility; no provider implementation change is justified yet.

**Observed:** Get "http://127.0.0.1:32775/openai/v1/models/wiremock-broken-model": cannot retry SDK request with a non-replayable body

**Change or investigate:**

1. Resolve the provider, network, rate-limit, or runner condition without treating it as a capability result.
2. Rerun the affected requirement and require a conclusive response.

**Verification targets:**

- Requirements: `official-sdk.openai.models.get`

### Canonical requirements needing attention

| Status | Level | Subject | Requirement | Evidence | Blocked by |
|---:|---:|---|---|---:|---|
| ERROR | CORE | `model:wiremock-broken-model` | `chat.basic` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `chat.content_parts.text` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `chat.logprobs` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `chat.multi_turn` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `chat.role.developer` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `chat.role.system` | 1 |  |
| SKIP | CORE | `model:wiremock-broken-model` | `chat.schema` | 1 |  |
| ERROR | CORE | `model:wiremock-broken-model` | `chat.stream.sse` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `chat.stream.usage` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `chat.structured.json_object` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `chat.structured.json_schema` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `chat.tools.auto` | 1 |  |
| SKIP | OPTIONAL | `model:wiremock-broken-model` | `chat.tools.continuation` | 1 | chat.tools.forced |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `chat.tools.forced` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `chat.tools.none` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `chat.tools.required` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `chat.tools.stream` | 1 |  |
| SKIP | CORE | `model:wiremock-broken-model` | `chat.usage` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `models.retrieve` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `official-sdk.openai.chat.completions` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `official-sdk.openai.chat.json` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `official-sdk.openai.chat.multi_turn` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `official-sdk.openai.chat.stream` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `official-sdk.openai.chat.stream.usage` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `official-sdk.openai.chat.tools` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `official-sdk.openai.chat.tools.stream` | 1 |  |
| FAIL | OPTIONAL | `model:wiremock-broken-model` | `official-sdk.openai.error.responses` | 1 |  |
| ERROR | OPTIONAL | `model:wiremock-broken-model` | `official-sdk.openai.models.get` | 1 |  |
| ERROR | OPTIONAL | `provider:http://127.0.0.1:32775/openai/v1` | `chat.error.invalid_auth` | 1 |  |
| ERROR | CORE | `provider:http://127.0.0.1:32775/openai/v1` | `chat.error.invalid_json` | 1 |  |
| ERROR | CORE | `provider:http://127.0.0.1:32775/openai/v1` | `chat.error.invalid_model` | 1 |  |
| ERROR | OPTIONAL | `provider:http://127.0.0.1:32775/openai/v1` | `chat.error.invalid_types` | 1 |  |

### Models

| Model | Verdict | PASS | FAIL | WARN | UNSUP | ERROR |
|---|---:|---:|---:|---:|---:|---:|
| `wiremock-broken-model` | INCONCLUSIVE | 0 | 1 | 0 | 0 | 24 |

#### Model details: `wiremock-broken-model`

| Module | Capability | Applicability | Execution | Reason |
|---|---|---:|---:|---|
| `chat-completions` | `chat.completions.text` | APPLICABLE | EXECUTED | no evidence of a non-text capability; treated as text by default |
| `images-generations` | `images.generations` | NOT-APPLICABLE | – | classified capabilities do not include images.generations: chat.completions.text |

### Extension suites

Built-in extension transport counts are separate from native latency percentiles. Safe rate and concurrency show the target pacer's state after extension execution; executable extensions report only validated suite evidence.

| Successful | 429 | Recovered retries | Backoff | Safe req/s | Safe concurrency |
|---:|---:|---:|---:|---:|---:|
| 1 | 0 | 0 | 0ms | 1.50 | 3 |

#### Official SDK – OpenAI Go v3.54.0

| Suite | Subject | Status | Duration | Summary |
|---|---|---:|---:|---|
| `chat.completions` | `model:wiremock-broken-model` | ERROR | 2002ms | POST "http://127.0.0.1:32775/openai/v1/chat/completions": 503 Service Unavailable {"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"} |
| `chat.json` | `model:wiremock-broken-model` | ERROR | 2198ms | POST "http://127.0.0.1:32775/openai/v1/chat/completions": 503 Service Unavailable {"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"} |
| `chat.multi-turn` | `model:wiremock-broken-model` | ERROR | 2002ms | POST "http://127.0.0.1:32775/openai/v1/chat/completions": 503 Service Unavailable {"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"} |
| `chat.stream` | `model:wiremock-broken-model` | ERROR | 2204ms | POST "http://127.0.0.1:32775/openai/v1/chat/completions": 503 Service Unavailable {"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"} |
| `chat.stream.usage` | `model:wiremock-broken-model` | ERROR | 2204ms | POST "http://127.0.0.1:32775/openai/v1/chat/completions": 503 Service Unavailable {"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"} |
| `chat.tools` | `model:wiremock-broken-model` | ERROR | 2002ms | POST "http://127.0.0.1:32775/openai/v1/chat/completions": 503 Service Unavailable {"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"} |
| `chat.tools.stream` | `model:wiremock-broken-model` | ERROR | 2002ms | POST "http://127.0.0.1:32775/openai/v1/chat/completions": 503 Service Unavailable {"message":"WireMock intentionally simulates an unavailable API operation","type":"service_unavailable","code":"example_failure"} |
| `error.responses` | `model:wiremock-broken-model` | FAIL | 2002ms | invalid model returned unexpected HTTP 503 |
| `models.get` | `model:wiremock-broken-model` | ERROR | 2000ms | Get "http://127.0.0.1:32775/openai/v1/models/wiremock-broken-model": cannot retry SDK request with a non-replayable body |
| `models.list` | `provider:http://127.0.0.1:32775/openai/v1` | PASS | 4ms | Official OpenAI Go SDK sent and parsed the suite |

## Observed API usage

Provider-reported token usage is normalized to common input and output columns. For Anthropic, cached input is reported separately from uncached input. Thinking/reasoning tokens are a subset of output and are not added twice.

| Target | Input | Output | Cache creation | Cache read | Thinking/reasoning | Total |
|---|---:|---:|---:|---:|---:|---:|
| `anthropic` | 0 | 0 | 0 | 0 | 0 | **0** |
| `google-genai` | 0 | 0 | 0 | 0 | 0 | **0** |
| `openai` | 0 | 0 | 0 | 0 | 0 | **0** |
| **All targets** | **0** | **0** | **0** | **0** | **0** | **0** |
