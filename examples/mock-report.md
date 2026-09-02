# AIQA compatibility report

_Measures observed API behavior; a passing report is not a security audit or an official certification._

- **Generated:** 2026-09-01 05:28:14Z
- **Tool:** `aiqa 1.0.0-alpha.3-example`
- **Service:** [127.0.0.1:34631](http://127.0.0.1:34631)
- **Suites:** `compat,model-param`
- **Timeout:** `5s` per request
- **External checks:** not setup

## Targets

| Target | Protocol | Profile | Verdict | Core | Optional | Evidence | Models | Actions |
|---|---|---|---:|---:|---:|---:|---:|---:|
| `openai` | OpenAI | `openai-compat@2026-09-01` | PASS | 8/8 (100.0%) | 41/41 (100.0%) | 49/49 (100.0%) | 1 | 0 |

**Run verdict:** PASS

Targets are assessed independently. Percentages are not combined into a cross-protocol score.

## Rate-limit and latency

| Target | Successful | 429 | Recovered retries | Backoff | Safe req/s | Safe concurrency | Mean | P50 | P95 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| `openai` | 59 | 0 | 0 | 0ms | 1000.00 | 1 | 0ms | 0ms | 0ms |

## Target: OpenAI

- **Target ID:** `openai`
- **Profile:** `openai-compat@2026-09-01`
- **External checks:** not setup
- **Discovered models:** 1

### Actions

No actionable compatibility issue was identified.

### Models

| Model | Verdict | PASS | FAIL | WARN | UNSUP | ERROR |
|---|---:|---:|---:|---:|---:|---:|
| `vendor/model-a` | PASS | 43 | 0 | 0 | 0 | 0 |

#### Model details: `vendor/model-a`

| Module | Capability | Applicability | Execution | Reason |
|---|---|---:|---:|---|
| `chat-completions` | `chat.completions.text` | APPLICABLE | EXECUTED | metadata capabilities="chat.completions.text" |
| `images-generations` | `images.generations` | NOT-APPLICABLE | – | classified capabilities do not include images.generations: chat.completions.text |

## Observed API usage

Provider-reported token usage is normalized to common input and output columns. For Anthropic, cached input is reported separately from uncached input. Thinking/reasoning tokens are a subset of output and are not added twice.

| Target | Input | Output | Cache creation | Cache read | Thinking/reasoning | Total |
|---|---:|---:|---:|---:|---:|---:|
| `openai` | 4267 | 286 | 0 | 0 | 58 | **4553** |
