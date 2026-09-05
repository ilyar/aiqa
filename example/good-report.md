# AIQA compatibility report

_measures observed API behavior, a passing report is not a security audit or an official certification_

- **Generated:** 2026-09-05 02:53:06Z
- **Tool:** `aiqa 1.0.0-alpha.4`
- **Service:** [127.0.0.1:8080](http://127.0.0.1:8080)
- **Suites:** `compat,sdk`
- **Timeout:** `1m0s` per request
- **External checks:** [Anthropic Go SDK v1.68.0](https://github.com/anthropics/anthropic-sdk-go/releases/tag/v1.68.0), [Google Gen AI Go SDK v1.71.0](https://github.com/googleapis/go-genai/releases/tag/v1.71.0), [OpenAI Go SDK v3.54.0](https://github.com/openai/openai-go/releases/tag/v3.54.0)

## Targets

| Target | Verdict | Core | Optional | Evidence | Models | Actions |
|---|---:|---:|---:|---:|---:|---:|
| `anthropic` | FAIL | 1/2 (50.0%) | 0/2 (0.0%) | 2/4 (50.0%) | 0 | 2 |
| `google-genai` | PASS | 16/16 (100.0%) | 13/13 (100.0%) | 29/29 (100.0%) | 1 | 0 |
| `openai` | PASS | 2/2 (100.0%) | 1/1 (100.0%) | 3/3 (100.0%) | 1 | 0 |

**Run verdict:** FAIL

Targets are assessed independently. Percentages are not combined into a cross-protocol score.

## Rate-limit and latency

| Target | Logical | Attempts | Successful | 429 | Retry attempts | Recovered | Exhausted | Backoff | Safe req/s | Concurrency | Mean | P50 | P95 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| `anthropic` | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0ms | N/S | N/S | N/S | N/S | N/S |
| `google-genai` | 10 | 10 | 6 | 0 | 0 | 0 | 0 | 0ms | 3.16 req/s | 6 | 270ms | 106ms | 953ms |
| `openai` | 1 | 1 | 1 | 0 | 0 | 0 | 0 | 0ms | 0.50 req/s | 1 | 0ms | 0ms | 0ms |

## Target: Anthropic

- **Target ID:** `anthropic`
- **Profile:** `anthropic-compat@2026-09-01`
- **API version:** `2023-06-01`
- **External checks:** [Anthropic Go SDK v1.68.0](https://github.com/anthropics/anthropic-sdk-go/releases/tag/v1.68.0)
- **Discovered models:** 1
- **Selected models:** none

### Actions

| PRI | Recommendation | Owner | Expected impact | Confidence |
|:---:|---|---|---|---:|
| P0 | Correct the incompatible behavior | `provider_gateway` | recover 1 core; projected PASS-WITH-GAPS | LOW |
| P1 | Investigate the incomplete or conflicting evidence | `unknown` | recover 1 optional | LOW |

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
| SKIP | OPTIONAL | `provider:http://127.0.0.1:8080/anthropic` | `anthropic.models.list.pagination` | 1 | anthropic.models.list.schema |
| FAIL | CORE | `provider:http://127.0.0.1:8080/anthropic` | `anthropic.models.list.schema` | 1 |  |
| WARN | OPTIONAL | `provider:http://127.0.0.1:8080/anthropic` | `anthropic.official-sdk.models.list` | 3 |  |

### Extension suites

Built-in extension transport counts are separate from native latency percentiles. Safe rate and concurrency show the target pacer's state after extension execution; executable extensions report only validated suite evidence.

| Logical | Attempts | Successful | 429 | Retry attempts | Recovered | Exhausted | Backoff | Safe req/s | Concurrency |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 1 | 1 | 1 | 0 | 0 | 0 | 0 | 0ms | 0.50 req/s | 1 |

#### Anthropic Go SDK v1.68.0

| Suite | Subject | Status | Duration | Summary |
|---|---|---:|---:|---|
| `models.list` | `provider:http://127.0.0.1:8080/anthropic` | PASS | 0ms | Anthropic Go SDK sent and parsed the suite |

## Target: Google GenAI

- **Target ID:** `google-genai`
- **Profile:** `google-genai-generate-content-compat@2026-09-03`
- **API version:** `v1beta`
- **External checks:** [Google Gen AI Go SDK v1.71.0](https://github.com/googleapis/go-genai/releases/tag/v1.71.0)
- **Note:** Google Gen AI Go SDK v1.71.0 does not expose public Interactions API methods; no SDK Interactions suite is claimed.
- **Discovered models:** 1
- **Selected models:** `Ternary-Bonsai-1.7B`

### Actions

No actionable compatibility issue was identified.

### Models

| Model | Verdict | PASS | FAIL | WARN | UNSUP | ERROR |
|---|---:|---:|---:|---:|---:|---:|
| `Ternary-Bonsai-1.7B` | PASS | 24 | 0 | 0 | 0 | 0 |

#### Model details: `Ternary-Bonsai-1.7B`

| Module | Capability | Applicability | Execution | Reason |
|---|---|---:|---:|---|
| `google-genai.generate-content` | `google-genai.generate-content.text` | APPLICABLE | EXECUTED | Google metadata declares generateContent support |
| `google-genai.generate-content-stream` | `google-genai.generate-content.text` | APPLICABLE | EXECUTED | Google metadata declares generateContent support |
| `google-genai.generate-content-tools` | `google-genai.generate-content.tools` | APPLICABLE | EXECUTED | generateContent is eligible for client-side tool probing |
| `google-genai.generate-content-structured-output` | `google-genai.generate-content.structured-output` | APPLICABLE | EXECUTED | generateContent is eligible for local structured-output probing |
| `google-genai.token-counting` | `google-genai.count-tokens` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.count-tokens: google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools |
| `google-genai.embeddings` | `google-genai.embed-content` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.embed-content: google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools |
| `google-genai.vision` | `google-genai.generate-content.image-input` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.generate-content.image-input: google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools |
| `google-genai.thinking` | `google-genai.generate-content.thinking` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.generate-content.thinking: google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools |
| `google-genai.interactions` | `google-genai.interactions.text` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.text: google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools |
| `google-genai.interactions-stream` | `google-genai.interactions.text` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.text: google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools |
| `google-genai.interactions-tools` | `google-genai.interactions.tools` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.tools: google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools |
| `google-genai.interactions-structured-output` | `google-genai.interactions.structured-output` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.structured-output: google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools |
| `google-genai.interactions-vision` | `google-genai.interactions.image-input` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.image-input: google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools |
| `google-genai.interactions-thinking` | `google-genai.interactions.thinking` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.thinking: google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools |
| `google-genai.interactions-stateful` | `google-genai.interactions.stateful` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.stateful: google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools |

### Extension suites

Built-in extension transport counts are separate from native latency percentiles. Safe rate and concurrency show the target pacer's state after extension execution; executable extensions report only validated suite evidence.

| Logical | Attempts | Successful | 429 | Retry attempts | Recovered | Exhausted | Backoff | Safe req/s | Concurrency |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 6 | 6 | 6 | 0 | 0 | 0 | 0 | 0ms | 3.33 req/s | 6 |

#### Google Gen AI Go SDK v1.71.0

| Suite | Subject | Status | Duration | Summary |
|---|---|---:|---:|---|
| `generate-content` | `model:Ternary-Bonsai-1.7B` | PASS | 234ms | Google Gen AI Go SDK sent and parsed the suite |
| `generate-content-stream` | `model:Ternary-Bonsai-1.7B` | PASS | 687ms | Google Gen AI Go SDK sent and parsed the suite |
| `models.get` | `model:Ternary-Bonsai-1.7B` | PASS | 0ms | Google Gen AI Go SDK sent and parsed the suite |
| `structured-output` | `model:Ternary-Bonsai-1.7B` | PASS | 200ms | Google Gen AI Go SDK sent and parsed the suite |
| `tools` | `model:Ternary-Bonsai-1.7B` | PASS | 642ms | Google Gen AI Go SDK sent and parsed the suite |
| `models.list` | `provider:http://127.0.0.1:8080/genai` | PASS | 0ms | Google Gen AI Go SDK sent and parsed the suite |

## Target: OpenAI

- **Target ID:** `openai`
- **Profile:** `openai-compat@2026-09-01`
- **External checks:** [OpenAI Go SDK v3.54.0](https://github.com/openai/openai-go/releases/tag/v3.54.0)
- **Discovered models:** 1
- **Selected models:** `prism-ml/Ternary-Bonsai-1.7B`

### Actions

No actionable compatibility issue was identified.

### Models

| Model | Verdict | PASS | FAIL | WARN | UNSUP | ERROR |
|---|---:|---:|---:|---:|---:|---:|
| `prism-ml/Ternary-Bonsai-1.7B` | NOT-EVALUATED | 0 | 0 | 0 | 0 | 0 |

#### Model details: `prism-ml/Ternary-Bonsai-1.7B`

| Module | Capability | Applicability | Execution | Reason |
|---|---|---:|---:|---|
| `chat-completions` | `chat.completions.text` | UNKNOWN | SKIPPED | insufficient capability evidence |
| `images-generations` | `images.generations` | UNKNOWN | SKIPPED | insufficient capability evidence |

### Extension suites

Built-in extension transport counts are separate from native latency percentiles. Safe rate and concurrency show the target pacer's state after extension execution; executable extensions report only validated suite evidence.

| Logical | Attempts | Successful | 429 | Retry attempts | Recovered | Exhausted | Backoff | Safe req/s | Concurrency |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 1 | 1 | 1 | 0 | 0 | 0 | 0 | 0ms | 0.50 req/s | 1 |

#### OpenAI Go SDK v3.54.0

| Suite | Subject | Status | Duration | Summary |
|---|---|---:|---:|---|
| `models.list` | `provider:http://127.0.0.1:8080/openai/v1` | PASS | 0ms | OpenAI Go SDK sent and parsed the suite |

## Observed API usage

Provider-reported token usage is normalized to common input and output columns. For Anthropic, cached input is reported separately from uncached input. Thinking/reasoning tokens are a subset of output and are not added twice.

| Target | Input | Output | Cache creation | Cache read | Thinking/reasoning | Total |
|---|---:|---:|---:|---:|---:|---:|
| `anthropic` | 0 | 0 | 0 | 0 | 0 | **0** |
| `google-genai` | 474 | 112 | 0 | 167 | 0 | **586** |
| `openai` | 0 | 0 | 0 | 0 | 0 | **0** |
| **All targets** | **474** | **112** | **0** | **167** | **0** | **586** |
