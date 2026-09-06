# AIQA compatibility report

_measures observed API behavior, a passing report is not a security audit or an official certification_

- **Generated:** 2026-09-06 12:14:06Z
- **Tool:** `aiqa 1.0.0-alpha.6`
- **Service:** [127.0.0.1:8080](http://127.0.0.1:8080)
- **Suites:** `compat,sdk`
- **Timeout:** `1m0s` per request
- **External checks:** [Anthropic Go SDK v1.68.0](https://github.com/anthropics/anthropic-sdk-go/releases/tag/v1.68.0), [Google Gen AI Go SDK v1.71.0](https://github.com/googleapis/go-genai/releases/tag/v1.71.0), [OpenAI Go SDK v3.54.0](https://github.com/openai/openai-go/releases/tag/v3.54.0)

## Targets

| Target | Verdict | Core | Optional | Evidence | Models | Actions |
|---|---:|---:|---:|---:|---:|---:|
| `anthropic` | PASS | 14/14 (100.0%) | 33/33 (100.0%) | 47/47 (100.0%) | 1 | 0 |
| `google-genai` | PASS | 22/22 (100.0%) | 5/5 (100.0%) | 27/27 (100.0%) | 1 | 0 |
| `openai` | PASS | 8/8 (100.0%) | 27/27 (100.0%) | 35/35 (100.0%) | 1 | 0 |

**Run verdict:** PASS

Targets are assessed independently. Percentages are not combined into a cross-protocol score.

## Rate-limit and latency

| Target | Logical | Attempts | Successful | 429 | Retry attempts | Recovered | Exhausted | Backoff | Safe req/s | Concurrency | Mean | P50 | P95 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| `anthropic` | 24 | 24 | 16 | 0 | 0 | 0 | 0 | 0ms | 5.29 req/s | 9 | 314ms | 126ms | 1263ms |
| `google-genai` | 8 | 8 | 4 | 0 | 0 | 0 | 0 | 0ms | 2.00 req/s | 4 | 40ms | 0ms | 86ms |
| `openai` | 26 | 26 | 20 | 0 | 0 | 0 | 0 | 0ms | 5.62 req/s | 9 | 600ms | 427ms | 1332ms |

## Target: Anthropic

- **Target ID:** `anthropic`
- **Profile:** `anthropic-compat@2026-09-01`
- **Requested coverage:** 5 routes; 3 executed, 0 unresolved (0 core, 0 optional), 2 excluded.
- **API version:** `2023-06-01`
- **External checks:** [Anthropic Go SDK v1.68.0](https://github.com/anthropics/anthropic-sdk-go/releases/tag/v1.68.0)
- **Discovered models:** 1
- **Selected models:** `Ternary-Bonsai-1.7B`

### Actions

No actionable compatibility issue was identified.

### Models

| Model | Verdict | PASS | FAIL | WARN | UNSUP | ERROR |
|---|---:|---:|---:|---:|---:|---:|
| `Ternary-Bonsai-1.7B` | PASS | 44 | 0 | 0 | 0 | 0 |

#### Model details: `Ternary-Bonsai-1.7B`

| Module | Capability | Requested | Applicability | Execution | Reason |
|---|---|---:|---:|---:|---|
| `anthropic.messages` | `anthropic.messages.text` | true | APPLICABLE | EXECUTED | Anthropic model metadata declares text support |
| `anthropic.tools` | `anthropic.messages.tools` | true | NOT-APPLICABLE | – | classified capabilities do not include anthropic.messages.tools: anthropic.messages.structured_output, anthropic.messages.text |
| `anthropic.token-counting` | `anthropic.messages.text` | true | APPLICABLE | EXECUTED | Anthropic model metadata declares text support |
| `anthropic.structured-output` | `anthropic.messages.structured_output` | true | APPLICABLE | EXECUTED | Anthropic model metadata declares structured_outputs support |
| `anthropic.vision` | `anthropic.messages.image_input` | true | NOT-APPLICABLE | – | classified capabilities do not include anthropic.messages.image_input: anthropic.messages.structured_output, anthropic.messages.text |
| `anthropic.thinking` | `anthropic.messages.thinking.adaptive,anthropic.messages.thinking.manual` | false | NOT-APPLICABLE | – | classified capabilities do not include anthropic.messages.thinking.adaptive, anthropic.messages.thinking.manual: anthropic.messages.structured_output, anthropic.messages.text |

### Extension suites

Built-in extension transport counts are separate from native latency percentiles. Safe rate and concurrency show the target pacer's state after extension execution; executable extensions report only validated suite evidence.

| Logical | Attempts | Successful | 429 | Retry attempts | Recovered | Exhausted | Backoff | Safe req/s | Concurrency |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 9 | 9 | 8 | 0 | 0 | 0 | 0 | 0ms | 5.62 req/s | 9 |

#### Anthropic Go SDK v1.68.0

| Suite | Subject | Status | Duration | Summary |
|---|---|---:|---:|---|
| `count-tokens` | `model:Ternary-Bonsai-1.7B` | PASS | 1ms | Anthropic Go SDK sent and parsed the suite |
| `error.responses` | `model:Ternary-Bonsai-1.7B` | PASS | 1ms | Anthropic Go SDK sent and parsed the suite |
| `messages` | `model:Ternary-Bonsai-1.7B` | PASS | 1195ms | Anthropic Go SDK sent and parsed the suite |
| `messages.multi-turn` | `model:Ternary-Bonsai-1.7B` | PASS | 523ms | Anthropic Go SDK sent and parsed the suite |
| `messages.stream` | `model:Ternary-Bonsai-1.7B` | PASS | 341ms | Anthropic Go SDK sent and parsed the suite |
| `messages.system` | `model:Ternary-Bonsai-1.7B` | PASS | 136ms | Anthropic Go SDK sent and parsed the suite |
| `models.get` | `model:Ternary-Bonsai-1.7B` | PASS | 1ms | Anthropic Go SDK sent and parsed the suite |
| `structured-output` | `model:Ternary-Bonsai-1.7B` | PASS | 304ms | Anthropic Go SDK sent and parsed the suite |
| `models.list` | `provider:http://127.0.0.1:8080/anthropic` | PASS | 1ms | Anthropic Go SDK sent and parsed the suite |

## Target: Google GenAI

- **Target ID:** `google-genai`
- **Profile:** `google-genai-compat@2026-09-03`
- **Requested coverage:** 12 routes; 2 executed, 0 unresolved (0 core, 0 optional), 10 excluded.
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
| `Ternary-Bonsai-1.7B` | PASS | 21 | 0 | 0 | 0 | 0 |

#### Model details: `Ternary-Bonsai-1.7B`

| Module | Capability | Requested | Applicability | Execution | Reason |
|---|---|---:|---:|---:|---|
| `google-genai.interactions` | `google-genai.interactions.text` | true | APPLICABLE | EXECUTED | Google metadata declares Interactions support |
| `google-genai.interactions-stream` | `google-genai.interactions.text` | true | APPLICABLE | EXECUTED | Google metadata declares Interactions support |
| `google-genai.interactions-tools` | `google-genai.interactions.tools` | true | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.tools: google-genai.interactions.text |
| `google-genai.interactions-structured-output` | `google-genai.interactions.structured-output` | true | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.structured-output: google-genai.interactions.text |
| `google-genai.interactions-vision` | `google-genai.interactions.image-input` | true | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.image-input: google-genai.interactions.text |
| `google-genai.interactions-thinking` | `google-genai.interactions.thinking` | false | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.thinking: google-genai.interactions.text |
| `google-genai.interactions-stateful` | `google-genai.interactions.stateful` | false | NOT-APPLICABLE | – | classified capabilities do not include google-genai.interactions.stateful: google-genai.interactions.text |
| `google-genai.generate-content` | `google-genai.generate-content.text` | true | NOT-APPLICABLE | – | classified capabilities do not include google-genai.generate-content.text: google-genai.interactions.text |
| `google-genai.generate-content-stream` | `google-genai.generate-content.text` | true | NOT-APPLICABLE | – | classified capabilities do not include google-genai.generate-content.text: google-genai.interactions.text |
| `google-genai.generate-content-tools` | `google-genai.generate-content.tools` | true | NOT-APPLICABLE | – | classified capabilities do not include google-genai.generate-content.tools: google-genai.interactions.text |
| `google-genai.generate-content-structured-output` | `google-genai.generate-content.structured-output` | true | NOT-APPLICABLE | – | classified capabilities do not include google-genai.generate-content.structured-output: google-genai.interactions.text |
| `google-genai.token-counting` | `google-genai.count-tokens` | true | NOT-APPLICABLE | – | classified capabilities do not include google-genai.count-tokens: google-genai.interactions.text |
| `google-genai.embeddings` | `google-genai.embed-content` | true | NOT-APPLICABLE | – | classified capabilities do not include google-genai.embed-content: google-genai.interactions.text |
| `google-genai.vision` | `google-genai.generate-content.image-input` | true | NOT-APPLICABLE | – | classified capabilities do not include google-genai.generate-content.image-input: google-genai.interactions.text |
| `google-genai.thinking` | `google-genai.generate-content.thinking` | false | NOT-APPLICABLE | – | classified capabilities do not include google-genai.generate-content.thinking: google-genai.interactions.text |

### Extension suites

Built-in extension transport counts are separate from native latency percentiles. Safe rate and concurrency show the target pacer's state after extension execution; executable extensions report only validated suite evidence.

| Logical | Attempts | Successful | 429 | Retry attempts | Recovered | Exhausted | Backoff | Safe req/s | Concurrency |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 1 | 1 | 1 | 0 | 0 | 0 | 0 | 0ms | 2.11 req/s | 4 |

#### Google Gen AI Go SDK v1.71.0

| Suite | Subject | Status | Duration | Summary |
|---|---|---:|---:|---|
| `models.list` | `provider:http://127.0.0.1:8080/genai` | PASS | 0ms | Google Gen AI Go SDK sent and parsed the suite |

## Target: OpenAI

- **Target ID:** `openai`
- **Profile:** `openai-compat@2026-09-01`
- **Requested coverage:** 1 routes; 1 executed, 0 unresolved (0 core, 0 optional), 0 excluded.
- **External checks:** [OpenAI Go SDK v3.54.0](https://github.com/openai/openai-go/releases/tag/v3.54.0)
- **Discovered models:** 1
- **Selected models:** `prism-ml/Ternary-Bonsai-1.7B`

### Actions

No actionable compatibility issue was identified.

### Models

| Model | Verdict | PASS | FAIL | WARN | UNSUP | ERROR |
|---|---:|---:|---:|---:|---:|---:|
| `prism-ml/Ternary-Bonsai-1.7B` | PASS | 28 | 0 | 0 | 0 | 0 |

#### Model details: `prism-ml/Ternary-Bonsai-1.7B`

| Module | Capability | Requested | Applicability | Execution | Reason |
|---|---|---:|---:|---:|---|
| `chat-completions` | `chat.completions.text` | true | APPLICABLE | EXECUTED | metadata capabilities="chat.completions.text" |
| `images-generations` | `images.generations` | false | NOT-APPLICABLE | – | classified capabilities do not include images.generations: chat.completions.text, responses.text |

### Extension suites

Built-in extension transport counts are separate from native latency percentiles. Safe rate and concurrency show the target pacer's state after extension execution; executable extensions report only validated suite evidence.

| Logical | Attempts | Successful | 429 | Retry attempts | Recovered | Exhausted | Backoff | Safe req/s | Concurrency |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 11 | 11 | 10 | 0 | 0 | 0 | 0 | 0ms | 6.43 req/s | 9 |

#### OpenAI Go SDK v3.54.0

| Suite | Subject | Status | Duration | Summary |
|---|---|---:|---:|---|
| `chat.completions` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 253ms | OpenAI Go SDK sent and parsed the suite |
| `chat.json` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 129ms | OpenAI Go SDK sent and parsed the suite |
| `chat.multi-turn` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 900ms | OpenAI Go SDK sent and parsed the suite |
| `chat.stream` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 218ms | OpenAI Go SDK sent and parsed the suite |
| `chat.stream.usage` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 744ms | OpenAI Go SDK sent and parsed the suite |
| `chat.tools` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 1421ms | OpenAI Go SDK sent and parsed the suite |
| `chat.tools.stream` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 1112ms | OpenAI Go SDK sent and parsed the suite |
| `error.responses` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 0ms | OpenAI Go SDK sent and parsed the suite |
| `models.get` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 0ms | OpenAI Go SDK sent and parsed the suite |
| `models.list` | `provider:http://127.0.0.1:8080/openai/v1` | PASS | 0ms | OpenAI Go SDK sent and parsed the suite |

## Observed API usage

Provider-reported token usage is normalized to common input and output columns. For Anthropic, cached input is reported separately from uncached input. Thinking/reasoning tokens are a subset of output and are not added twice.

| Target | Input | Output | Cache creation | Cache read | Thinking/reasoning | Total |
|---|---:|---:|---:|---:|---:|---:|
| `anthropic` | 739 | 143 | 0 | 0 | 0 | **882** |
| `google-genai` | 38 | 8 | 0 | 0 | 0 | **46** |
| `openai` | 1561 | 192 | 0 | 0 | 0 | **1753** |
| **All targets** | **2338** | **343** | **0** | **0** | **0** | **2681** |
