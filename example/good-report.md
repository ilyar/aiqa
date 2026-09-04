# AIQA compatibility report

_measures observed API behavior, a passing report is not a security audit or an official certification_

- **Generated:** 2026-09-04 07:30:16Z
- **Tool:** `aiqa 1.0.0-alpha.3`
- **Service:** [127.0.0.1:8080](http://127.0.0.1:8080)
- **Suites:** `compat,ext-suite`
- **Timeout:** `1m0s` per request
- **External checks:** [Official SDK – Google Gen AI Go v1.71.0](https://github.com/googleapis/go-genai/releases/tag/v1.71.0)

## Targets

| Target | Verdict | Core | Optional | Evidence | Models | Actions |
|---|---:|---:|---:|---:|---:|---:|
| `google-genai` | PASS | 16/16 (100.0%) | 13/13 (100.0%) | 29/29 (100.0%) | 1 | 0 |

**Run verdict:** PASS

Targets are assessed independently. Percentages are not combined into a cross-protocol score.

## Rate-limit and latency

| Target | Logical | Attempts | Successful | 429 | Retry attempts | Recovered | Exhausted | Backoff | Safe req/s | Concurrency | Mean | P50 | P95 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| `google-genai` | 10 | 10 | 6 | 0 | 0 | 0 | 0 | 0ms | 3.16 req/s | 6 | 409ms | 262ms | 1232ms |

## Target: Google GenAI

- **Target ID:** `google-genai`
- **Profile:** `google-genai-generate-content-compat@2026-09-03`
- **API version:** `v1beta`
- **External checks:** [Official SDK – Google Gen AI Go v1.71.0](https://github.com/googleapis/go-genai/releases/tag/v1.71.0)
- **Note:** Official Google Gen AI Go SDK v1.71.0 does not expose public Interactions API methods; no SDK Interactions suite is claimed.
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

#### Official SDK – Google Gen AI Go v1.71.0

| Suite | Subject | Status | Duration | Summary |
|---|---|---:|---:|---|
| `generate-content` | `model:Ternary-Bonsai-1.7B` | PASS | 139ms | Official Google Gen AI Go SDK sent and parsed the suite |
| `generate-content-stream` | `model:Ternary-Bonsai-1.7B` | PASS | 92ms | Official Google Gen AI Go SDK sent and parsed the suite |
| `models.get` | `model:Ternary-Bonsai-1.7B` | PASS | 4ms | Official Google Gen AI Go SDK sent and parsed the suite |
| `structured-output` | `model:Ternary-Bonsai-1.7B` | PASS | 337ms | Official Google Gen AI Go SDK sent and parsed the suite |
| `tools` | `model:Ternary-Bonsai-1.7B` | PASS | 783ms | Official Google Gen AI Go SDK sent and parsed the suite |
| `models.list` | `provider:http://127.0.0.1:8080/genai` | PASS | 5ms | Official Google Gen AI Go SDK sent and parsed the suite |

## Observed API usage

Provider-reported token usage is normalized to common input and output columns. For Anthropic, cached input is reported separately from uncached input. Thinking/reasoning tokens are a subset of output and are not added twice.

| Target | Input | Output | Cache creation | Cache read | Thinking/reasoning | Total |
|---|---:|---:|---:|---:|---:|---:|
| `google-genai` | 474 | 112 | 0 | 167 | 0 | **586** |
