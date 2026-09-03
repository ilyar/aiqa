# AIQA compatibility report

_measures observed API behavior, a passing report is not a security audit or an official certification_

- **Generated:** 2026-09-03 17:27:13Z
- **Tool:** `aiqa 1.0.0-alpha.3`
- **Service:** [127.0.0.1:8080](http://127.0.0.1:8080)
- **Suites:** `compat,ext-suite`
- **Timeout:** `1m0s` per request
- **External checks:** [Official SDK – Anthropic Go v1.68.0](https://github.com/anthropics/anthropic-sdk-go/releases/tag/v1.68.0), [Official SDK – Google Gen AI Go v1.71.0](https://github.com/googleapis/go-genai/releases/tag/v1.71.0), [Official SDK – OpenAI Go v3.54.0](https://github.com/openai/openai-go/releases/tag/v3.54.0)

## Targets

| Target | Verdict | Core | Optional | Evidence | Models | Actions |
|---|---:|---:|---:|---:|---:|---:|
| `anthropic` | PASS | 14/14 (100.0%) | 44/44 (100.0%) | 58/58 (100.0%) | 1 | 0 |
| `google-genai` | PASS | 16/16 (100.0%) | 13/13 (100.0%) | 29/29 (100.0%) | 1 | 0 |
| `openai` | PASS | 8/8 (100.0%) | 27/27 (100.0%) | 35/35 (100.0%) | 1 | 0 |

**Run verdict:** PASS

Targets are assessed independently. Percentages are not combined into a cross-protocol score.

## Rate-limit and latency

| Target | Successful | 429 | Retries | Backoff | Safe req/s | Safe concurrency | Mean | P50 | P95 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| `anthropic` | 24 | 0 | 0 | 0ms | 7.50 | 12 | 421ms | 282ms | 1296ms |
| `google-genai` | 6 | 0 | 0 | 0ms | 3.16 | 6 | 142ms | 52ms | 733ms |
| `openai` | 20 | 0 | 0 | 0ms | 5.62 | 9 | 855ms | 1026ms | 2085ms |

## Target: Anthropic

- **Target ID:** `anthropic`
- **Profile:** `anthropic-compat@2026-09-01`
- **API version:** `2023-06-01`
- **External checks:** [Official SDK – Anthropic Go v1.68.0](https://github.com/anthropics/anthropic-sdk-go/releases/tag/v1.68.0)
- **Discovered models:** 1

### Actions

No actionable compatibility issue was identified.

### Models

| Model | Verdict | PASS | FAIL | WARN | UNSUP | ERROR |
|---|---:|---:|---:|---:|---:|---:|
| `Ternary-Bonsai-1.7B` | PASS | 55 | 0 | 0 | 0 | 0 |

#### Model details: `Ternary-Bonsai-1.7B`

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
| 11 | 0 | 0 | 0ms | 9.23 | 12 |

#### Official SDK – Anthropic Go v1.68.0

| Suite | Subject | Status | Duration | Summary |
|---|---|---:|---:|---|
| `count-tokens` | `model:Ternary-Bonsai-1.7B` | PASS | 5ms | Official Anthropic Go SDK sent and parsed the suite |
| `error.responses` | `model:Ternary-Bonsai-1.7B` | PASS | 6ms | Official Anthropic Go SDK sent and parsed the suite |
| `messages` | `model:Ternary-Bonsai-1.7B` | PASS | 69ms | Official Anthropic Go SDK sent and parsed the suite |
| `messages.multi-turn` | `model:Ternary-Bonsai-1.7B` | PASS | 1044ms | Official Anthropic Go SDK sent and parsed the suite |
| `messages.stream` | `model:Ternary-Bonsai-1.7B` | PASS | 493ms | Official Anthropic Go SDK sent and parsed the suite |
| `messages.system` | `model:Ternary-Bonsai-1.7B` | PASS | 612ms | Official Anthropic Go SDK sent and parsed the suite |
| `models.get` | `model:Ternary-Bonsai-1.7B` | PASS | 2ms | Official Anthropic Go SDK sent and parsed the suite |
| `structured-output` | `model:Ternary-Bonsai-1.7B` | PASS | 833ms | Official Anthropic Go SDK sent and parsed the suite |
| `tools` | `model:Ternary-Bonsai-1.7B` | PASS | 1527ms | Official Anthropic Go SDK sent and parsed the suite |
| `tools.stream` | `model:Ternary-Bonsai-1.7B` | PASS | 452ms | Official Anthropic Go SDK sent and parsed the suite |
| `models.list` | `provider:http://127.0.0.1:8080/anthropic` | PASS | 3ms | Official Anthropic Go SDK sent and parsed the suite |

## Target: Google GenAI

- **Target ID:** `google-genai`
- **Profile:** `google-genai-generate-content-compat@2026-09-03`
- **API version:** `v1beta`
- **External checks:** [Official SDK – Google Gen AI Go v1.71.0](https://github.com/googleapis/go-genai/releases/tag/v1.71.0)
- **Note:** Official Google Gen AI Go SDK v1.71.0 does not expose public Interactions API methods; no SDK Interactions suite is claimed.
- **Discovered models:** 1

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
| `google-genai.token-counting` | `google-genai.count-tokens` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.count-tokens: google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools, google-genai.interactions.structured-output, google-genai.interactions.text, google-genai.interactions.tools |
| `google-genai.embeddings` | `google-genai.embed-content` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.embed-content: google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools, google-genai.interactions.structured-output, google-genai.interactions.text, google-genai.interactions.tools |
| `google-genai.vision` | `google-genai.generate-content.image-input` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.generate-content.image-input: google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools, google-genai.interactions.structured-output, google-genai.interactions.text, google-genai.interactions.tools |
| `google-genai.thinking` | `google-genai.generate-content.thinking` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.generate-content.thinking: google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools, google-genai.interactions.structured-output, google-genai.interactions.text, google-genai.interactions.tools |
| `google-genai.interactions` | `google-genai.interactions.text` | APPLICABLE | SKIPPED | module was not enabled |
| `google-genai.interactions-stream` | `google-genai.interactions.text` | APPLICABLE | SKIPPED | module was not enabled |
| `google-genai.interactions-tools` | `google-genai.interactions.tools` | APPLICABLE | SKIPPED | module was not enabled |
| `google-genai.interactions-structured-output` | `google-genai.interactions.structured-output` | APPLICABLE | SKIPPED | module was not enabled |
| `google-genai.interactions-vision` | `google-genai.interactions.image-input` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.image-input: google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools, google-genai.interactions.structured-output, google-genai.interactions.text, google-genai.interactions.tools |
| `google-genai.interactions-thinking` | `google-genai.interactions.thinking` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.thinking: google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools, google-genai.interactions.structured-output, google-genai.interactions.text, google-genai.interactions.tools |
| `google-genai.interactions-stateful` | `google-genai.interactions.stateful` | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.stateful: google-genai.generate-content.structured-output, google-genai.generate-content.text, google-genai.generate-content.tools, google-genai.interactions.structured-output, google-genai.interactions.text, google-genai.interactions.tools |

### Extension suites

Built-in extension transport counts are separate from native latency percentiles. Safe rate and concurrency show the target pacer's state after extension execution; executable extensions report only validated suite evidence.

| Successful | 429 | Recovered retries | Backoff | Safe req/s | Safe concurrency |
|---:|---:|---:|---:|---:|---:|
| 6 | 0 | 0 | 0ms | 3.33 | 6 |

#### Official SDK – Google Gen AI Go v1.71.0

| Suite | Subject | Status | Duration | Summary |
|---|---|---:|---:|---|
| `generate-content` | `model:Ternary-Bonsai-1.7B` | PASS | 265ms | Official Google Gen AI Go SDK sent and parsed the suite |
| `generate-content-stream` | `model:Ternary-Bonsai-1.7B` | PASS | 233ms | Official Google Gen AI Go SDK sent and parsed the suite |
| `models.get` | `model:Ternary-Bonsai-1.7B` | PASS | 2ms | Official Google Gen AI Go SDK sent and parsed the suite |
| `structured-output` | `model:Ternary-Bonsai-1.7B` | PASS | 199ms | Official Google Gen AI Go SDK sent and parsed the suite |
| `tools` | `model:Ternary-Bonsai-1.7B` | PASS | 490ms | Official Google Gen AI Go SDK sent and parsed the suite |
| `models.list` | `provider:http://127.0.0.1:8080/genai` | PASS | 2ms | Official Google Gen AI Go SDK sent and parsed the suite |

## Target: OpenAI

- **Target ID:** `openai`
- **Profile:** `openai-compat@2026-09-01`
- **External checks:** [Official SDK – OpenAI Go v3.54.0](https://github.com/openai/openai-go/releases/tag/v3.54.0)
- **Discovered models:** 1

### Actions

No actionable compatibility issue was identified.

### Models

| Model | Verdict | PASS | FAIL | WARN | UNSUP | ERROR |
|---|---:|---:|---:|---:|---:|---:|
| `prism-ml/Ternary-Bonsai-1.7B` | PASS | 28 | 0 | 0 | 0 | 0 |

#### Model details: `prism-ml/Ternary-Bonsai-1.7B`

| Module | Capability | Applicability | Execution | Reason |
|---|---|---:|---:|---|
| `chat-completions` | `chat.completions.text` | APPLICABLE | EXECUTED | no evidence of a non-text capability; treated as text by default |
| `images-generations` | `images.generations` | NOT-APPLICABLE | – | classified capabilities do not include images.generations: chat.completions.text |

### Extension suites

Built-in extension transport counts are separate from native latency percentiles. Safe rate and concurrency show the target pacer's state after extension execution; executable extensions report only validated suite evidence.

| Successful | 429 | Recovered retries | Backoff | Safe req/s | Safe concurrency |
|---:|---:|---:|---:|---:|---:|
| 10 | 0 | 0 | 0ms | 6.43 | 9 |

#### Official SDK – OpenAI Go v3.54.0

| Suite | Subject | Status | Duration | Summary |
|---|---|---:|---:|---|
| `chat.completions` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 316ms | Official OpenAI Go SDK sent and parsed the suite |
| `chat.json` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 249ms | Official OpenAI Go SDK sent and parsed the suite |
| `chat.multi-turn` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 910ms | Official OpenAI Go SDK sent and parsed the suite |
| `chat.stream` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 110ms | Official OpenAI Go SDK sent and parsed the suite |
| `chat.stream.usage` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 283ms | Official OpenAI Go SDK sent and parsed the suite |
| `chat.tools` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 1478ms | Official OpenAI Go SDK sent and parsed the suite |
| `chat.tools.stream` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 1096ms | Official OpenAI Go SDK sent and parsed the suite |
| `error.responses` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 232ms | Official OpenAI Go SDK sent and parsed the suite |
| `models.get` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 0ms | Official OpenAI Go SDK sent and parsed the suite |
| `models.list` | `provider:http://127.0.0.1:8080/openai/v1` | PASS | 0ms | Official OpenAI Go SDK sent and parsed the suite |

## Observed API usage

Provider-reported token usage is normalized to common input and output columns. For Anthropic, cached input is reported separately from uncached input. Thinking/reasoning tokens are a subset of output and are not added twice.

| Target | Input | Output | Cache creation | Cache read | Thinking/reasoning | Total |
|---|---:|---:|---:|---:|---:|---:|
| `anthropic` | 2000 | 407 | 0 | 294 | 0 | **2701** |
| `google-genai` | 474 | 112 | 0 | 341 | 0 | **586** |
| `openai` | 1629 | 190 | 0 | 0 | 0 | **1819** |
| **All targets** | **4103** | **709** | **0** | **635** | **0** | **5106** |
