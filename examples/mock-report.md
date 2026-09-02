# AIQA compatibility report

_Measures observed API behavior; a passing report is not a security audit or an official certification._

- **Generated:** 2026-09-02 19:52:45Z
- **Tool:** `aiqa 1.0.0-alpha.3`
- **Service:** [127.0.0.1:8080](http://127.0.0.1:8080)
- **Suites:** `compat,ext-suite`
- **Timeout:** `1m0s` per request
- **External checks:** [Official SDK — Anthropic Go v1.68.0](https://github.com/anthropics/anthropic-sdk-go/releases/tag/v1.68.0), [Official SDK — OpenAI Go v3.54.0](https://github.com/openai/openai-go/releases/tag/v3.54.0)

## Targets

| Target | Protocol | Profile | Verdict | Core | Optional | Evidence | Models | Actions |
|---|---|---|---:|---:|---:|---:|---:|---:|
| `anthropic` | Anthropic | `anthropic-compat@2026-09-01` | PASS-WITH-GAPS | 14/14 (100.0%) | 39/44 (88.6%) | 53/58 (91.4%) | 1 | 5 |
| `openai` | OpenAI | `openai-compat@2026-09-01` | PASS-WITH-GAPS | 8/8 (100.0%) | 26/27 (96.3%) | 34/35 (97.1%) | 1 | 1 |

**Run verdict:** PASS-WITH-GAPS

Targets are assessed independently. Percentages are not combined into a cross-protocol score.

## Rate-limit and latency

| Target | Successful | 429 | Recovered retries | Backoff | Safe req/s | Safe concurrency | Mean | P50 | P95 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| `anthropic` | 24 | 0 | 0 | 0ms | 6.88 | 11 | 472ms | 248ms | 1134ms |
| `openai` | 20 | 0 | 0 | 0ms | 5.62 | 9 | 588ms | 517ms | 2188ms |

## Target: Anthropic

- **Target ID:** `anthropic`
- **Profile:** `anthropic-compat@2026-09-01`
- **API version:** `2023-06-01`
- **External checks:** [Official SDK — Anthropic Go v1.68.0](https://github.com/anthropics/anthropic-sdk-go/releases/tag/v1.68.0)
- **Discovered models:** 1

### Actions

| PRI | Recommendation | Owner | Expected impact | Confidence |
|:---:|---|---|---|---:|
| P1 | Investigate the incomplete or conflicting evidence | `unknown` | recover 1 optional | LOW |
| P1 | Investigate the incomplete or conflicting evidence | `unknown` | recover 1 optional | LOW |
| P1 | Investigate the incomplete or conflicting evidence | `unknown` | recover 1 optional | LOW |
| P1 | Investigate the incomplete or conflicting evidence | `unknown` | recover 1 optional | LOW |
| P2 | Investigate the incomplete or conflicting evidence | `unknown` | recover 1 optional | LOW |

#### P1 · Investigate the incomplete or conflicting evidence

**Why this matters:** Trustworthy evidence sources disagree, so the mismatch must be localized before choosing a fix.

**Observed:** Multi-turn messages is supported SDK parsed a Message without the requested marker

**Change or investigate:**

1. Compare the structured evidence records and request settings for the affected requirement.
2. Localize the mismatch before changing the provider or checker.

**Verification targets:**

- Requirements: `anthropic.official-sdk.messages.multi_turn`

#### P1 · Investigate the incomplete or conflicting evidence

**Why this matters:** Trustworthy evidence sources disagree, so the mismatch must be localized before choosing a fix.

**Observed:** SDK parsed a message that violated the requested JSON schema Structured output conforms to the requested JSON schema

**Change or investigate:**

1. Compare the structured evidence records and request settings for the affected requirement.
2. Localize the mismatch before changing the provider or checker.

**Verification targets:**

- Requirements: `anthropic.official-sdk.structured_output`

#### P1 · Investigate the incomplete or conflicting evidence

**Why this matters:** Trustworthy evidence sources disagree, so the mismatch must be localized before choosing a fix.

**Observed:** Forced named tool returned a valid tool_use block SDK parsed no get_status tool_use block tool_result was accepted and used in the final answer

**Change or investigate:**

1. Compare the structured evidence records and request settings for the affected requirement.
2. Localize the mismatch before changing the provider or checker.

**Verification targets:**

- Requirements: `anthropic.official-sdk.tools`

#### P1 · Investigate the incomplete or conflicting evidence

**Why this matters:** Trustworthy evidence sources disagree, so the mismatch must be localized before choosing a fix.

**Observed:** SDK parsed tool_use with invalid input Streamed tool input reconstructed as valid JSON

**Change or investigate:**

1. Compare the structured evidence records and request settings for the affected requirement.
2. Localize the mismatch before changing the provider or checker.

**Verification targets:**

- Requirements: `anthropic.official-sdk.tools.stream`

#### P2 · Investigate the incomplete or conflicting evidence

**Why this matters:** The affected optional requirement reduces demonstrated interoperability or capability coverage.

**Observed:** Count Tokens differs materially from Message usage

**Change or investigate:**

1. Compare the structured evidence records and request settings for the affected requirement.
2. Localize the mismatch before changing the provider or checker.

**Verification targets:**

- Requirements: `anthropic.tokens.count.consistency`

### Canonical requirements needing attention

| Status | Level | Subject | Requirement | Evidence | Blocked by |
|---:|---:|---|---|---:|---|
| WARN | OPTIONAL | `model:Ternary-Bonsai-1.7B` | `anthropic.official-sdk.messages.multi_turn` | 2 |  |
| WARN | OPTIONAL | `model:Ternary-Bonsai-1.7B` | `anthropic.official-sdk.structured_output` | 2 |  |
| WARN | OPTIONAL | `model:Ternary-Bonsai-1.7B` | `anthropic.official-sdk.tools` | 3 |  |
| WARN | OPTIONAL | `model:Ternary-Bonsai-1.7B` | `anthropic.official-sdk.tools.stream` | 2 |  |
| WARN | OPTIONAL | `model:Ternary-Bonsai-1.7B` | `anthropic.tokens.count.consistency` | 1 |  |

### Models

| Model | Verdict | PASS | FAIL | WARN | UNSUP | ERROR |
|---|---:|---:|---:|---:|---:|---:|
| `Ternary-Bonsai-1.7B` | PASS-WITH-GAPS | 50 | 4 | 1 | 0 | 0 |

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
| 10 | 0 | 0 | 0ms | 7.86 | 11 |

#### Official SDK — Anthropic Go v1.68.0

| Suite | Subject | Status | Duration | Summary |
|---|---|---:|---:|---|
| `count-tokens` | `model:Ternary-Bonsai-1.7B` | PASS | 4ms | Official Anthropic Go SDK sent and parsed the suite |
| `error.responses` | `model:Ternary-Bonsai-1.7B` | PASS | 4ms | Official Anthropic Go SDK sent and parsed the suite |
| `messages` | `model:Ternary-Bonsai-1.7B` | PASS | 96ms | Official Anthropic Go SDK sent and parsed the suite |
| `messages.multi-turn` | `model:Ternary-Bonsai-1.7B` | FAIL | 720ms | SDK parsed a Message without the requested marker |
| `messages.stream` | `model:Ternary-Bonsai-1.7B` | PASS | 56ms | Official Anthropic Go SDK sent and parsed the suite |
| `messages.system` | `model:Ternary-Bonsai-1.7B` | PASS | 208ms | Official Anthropic Go SDK sent and parsed the suite |
| `models.get` | `model:Ternary-Bonsai-1.7B` | PASS | 3ms | Official Anthropic Go SDK sent and parsed the suite |
| `structured-output` | `model:Ternary-Bonsai-1.7B` | FAIL | 1099ms | SDK parsed a message that violated the requested JSON schema |
| `tools` | `model:Ternary-Bonsai-1.7B` | FAIL | 444ms | SDK parsed no get_status tool_use block |
| `tools.stream` | `model:Ternary-Bonsai-1.7B` | FAIL | 1059ms | SDK parsed tool_use with invalid input |
| `models.list` | `provider:http://127.0.0.1:8080/anthropic` | PASS | 3ms | Official Anthropic Go SDK sent and parsed the suite |

## Target: OpenAI

- **Target ID:** `openai`
- **Profile:** `openai-compat@2026-09-01`
- **External checks:** [Official SDK — OpenAI Go v3.54.0](https://github.com/openai/openai-go/releases/tag/v3.54.0)
- **Discovered models:** 1

### Actions

| PRI | Recommendation | Owner | Expected impact | Confidence |
|:---:|---|---|---|---:|
| P1 | Localize the streaming interoperability conflict | `unknown` | recover 1 optional | LOW |

#### P1 · Localize the streaming interoperability conflict

**Why this matters:** A provider fix is unsafe until the differing request settings or parser expectations are identified.

**Observed:** SDK parsed an incomplete Chat Completion valid SSE stream with 4 event(s) and [DONE]

**Change or investigate:**

1. Compare the exact native and Official SDK request bodies.
2. Compare missing versus empty `choices`, usage placement, finish reasons, and chunks after completion.
3. Replay the captured stream through the pinned official SDK parser before changing the provider.

**Verification targets:**

- Requirements: `chat.stream.sse`
- Official SDK suites: `chat_completions_stream`

### Canonical requirements needing attention

| Status | Level | Subject | Requirement | Evidence | Blocked by |
|---:|---:|---|---|---:|---|
| WARN | OPTIONAL | `model:prism-ml/Ternary-Bonsai-1.7B` | `official-sdk.openai.chat.stream` | 2 |  |

### Models

| Model | Verdict | PASS | FAIL | WARN | UNSUP | ERROR |
|---|---:|---:|---:|---:|---:|---:|
| `prism-ml/Ternary-Bonsai-1.7B` | PASS-WITH-GAPS | 27 | 1 | 0 | 0 | 0 |

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

#### Official SDK — OpenAI Go v3.54.0

| Suite | Subject | Status | Duration | Summary |
|---|---|---:|---:|---|
| `chat.completions` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 127ms | Official OpenAI Go SDK sent and parsed the suite |
| `chat.json` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 265ms | Official OpenAI Go SDK sent and parsed the suite |
| `chat.multi-turn` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 1162ms | Official OpenAI Go SDK sent and parsed the suite |
| `chat.stream` | `model:prism-ml/Ternary-Bonsai-1.7B` | FAIL | 94ms | SDK parsed an incomplete Chat Completion |
| `chat.stream.usage` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 463ms | Official OpenAI Go SDK sent and parsed the suite |
| `chat.tools` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 1703ms | Official OpenAI Go SDK sent and parsed the suite |
| `chat.tools.stream` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 1349ms | Official OpenAI Go SDK sent and parsed the suite |
| `error.responses` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 2ms | Official OpenAI Go SDK sent and parsed the suite |
| `models.get` | `model:prism-ml/Ternary-Bonsai-1.7B` | PASS | 0ms | Official OpenAI Go SDK sent and parsed the suite |
| `models.list` | `provider:http://127.0.0.1:8080/openai/v1` | PASS | 0ms | Official OpenAI Go SDK sent and parsed the suite |

## Observed API usage

Provider-reported token usage is normalized to common input and output columns. For Anthropic, cached input is reported separately from uncached input. Thinking/reasoning tokens are a subset of output and are not added twice.

| Target | Input | Output | Cache creation | Cache read | Thinking/reasoning | Total |
|---|---:|---:|---:|---:|---:|---:|
| `anthropic` | 461 | 379 | 0 | 1459 | 0 | **2299** |
| `openai` | 1433 | 199 | 0 | 0 | 0 | **1632** |
| **All targets** | **1894** | **578** | **0** | **1459** | **0** | **3931** |
