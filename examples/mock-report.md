# AIQA report

_measures observed API behavior, a passing report is not a security audit or an official certification_

- **Generated:** 2026-08-31 20:31:45Z
- **Tool:** `aiqa 1.0.0-alpha.0-example` (`example`, built `2026-08-30T00:00:00Z`)
- **Endpoint:** `http://127.0.0.1:39013/v1`
- **Suites:** `compat,model-param`
- **Timeout:** `5s` per request
- **Profile:** `openai-compat@2026-09-01` (`690521b1753d`) – GET /v1/models, GET /v1/models/{model}, POST /v1/chat/completions, POST /v1/images/generations
- **External checks:** not setup

## Summary

| Overall | Core conformance | Optional coverage | Evidence | Open actions |
|---|---:|---:|---:|---:|
| PASS | 8/8 (100.0%) | 41/41 (100.0%) | 49/49 (100.0%) | 0 |

Raw outcomes: **49 PASS · 0 FAIL · 0 WARN · 0 UNSUP · 0 SKIP · 0 ERROR**.

Observed API usage: **4314 prompt + 290 completion = 4604 total tokens**. Context probing additionally sent approximately **12957 prompt tokens**.

## Actions

No open actions were derived from applicable canonical requirements.

## Rate-limit and latency

- Request pacing: adaptive
- Successful native requests: 59
- 429 responses: 0
- Retries recovered: 0
- Observed safe request rate: 1000.00 req/s
- Observed safe concurrency: 1
- Successful native latency (n=0): mean 0 ms; P50 0 ms; P95 0 ms
- Backoff time: 0 ms

## Model and module details

Discovered models: **1**.

### Model inventory and routing

| Model | Detected capabilities | Source | Confidence | Chat | Images | Embeddings | Evaluation |
|---|---|---|---:|---:|---:|---:|---|
| `vendor/model-a` | chat.completions.text | vendor_metadata | medium | EXECUTED | N/A | – | PASS |

### Model summary

| Model | Status | Required | Context | Tests |
|---|---:|---:|---:|---:|
| `vendor/model-a` | PASS | 4/4 | 2,313–~2,555 | 43 |

### `vendor/model-a`

| Overall | Pass | Fail | Warn | UNSUP | Skip | Error | Required |
|---|---:|---:|---:|---:|---:|---:|---:|
| PASS | 43 | 0 | 0 | 0 | 0 | 0 | 4/4 |

### Module applicability

| Module | Capability | Applicability | Execution | Reason |
|---|---|---:|---:|---|
| `chat-completions` | `chat.completions.text` | APPLICABLE | EXECUTED | metadata capabilities="chat.completions.text" |
| `images-generations` | `images.generations` | N/A | – | classified capabilities do not include images.generations: chat.completions.text |

#### Context window

**PASS:** accepted context boundary is between 2313 and approximately 2555 prompt tokens

| Metadata limit | Server-reported limit | Accepted lower bound | First rejected estimate | Precision | Confidence | Probes | Probe tokens | Output cap |
|---:|---:|---:|---:|---:|---|---:|---:|---|
| 4,096 | – | 2,313 | 2,555 | 512 | high | 7 | 12,957 | `max_completion_tokens` |

<details><summary>Context probe samples</summary>

| Payload bytes | Reported prompt tokens | Estimated prompt tokens | Accepted | Overflow | HTTP | Time | Error |
|---:|---:|---:|---:|---:|---:|---:|---|
| 1,024 | 436 | 470 | yes | no | 200 | 0 ms |  |
| 2,048 | 777 | 872 | yes | no | 200 | 0 ms |  |
| 4,096 | 1,460 | 1,744 | yes | no | 200 | 0 ms |  |
| 8,192 | – | 3,108 | no | yes | 400 | 0 ms | maximum context length exceeded |
| 6,144 | 2,143 | 2,331 | yes | no | 200 | 2 ms |  |
| 7,168 | – | 2,720 | no | yes | 400 | 0 ms | maximum context length exceeded |
| 6,656 | 2,313 | 2,526 | yes | no | 200 | 0 ms |  |

</details>

## Passed checks

<details><summary>Show 49 passed checks</summary>

### Discovery

| Status | Required | Category | Test | Summary | HTTP | Tokens | Time |
|---:|---:|---|---|---|---:|---:|---:|
| PASS | yes | discovery | `models.list.http` | GET /models returned HTTP 200 JSON | 200 | – | 0 ms |
| PASS | yes | schema | `models.list.schema` | model list follows the OpenAI list shape | 200 | – | 0 ms |

### Provider endpoints

| Status | Required | Category | Test | Summary | HTTP | Tokens | Time |
|---:|---:|---|---|---|---:|---:|---:|
| PASS | no | errors | `chat.error.invalid_auth` | invalid authentication returned HTTP 401 with an OpenAI-shaped error – invalid API key | 401 | – | 0 ms |
| PASS | yes | errors | `chat.error.invalid_json` | all malformed JSON variants returned client errors with OpenAI-shaped bodies | 400 | – | 0 ms |
| PASS | yes | errors | `chat.error.invalid_model` | invalid model returned HTTP 404 with an OpenAI-shaped error – unknown model | 404 | – | 0 ms |
| PASS | no | errors | `chat.error.invalid_types` | invalid request field types returned HTTP 400 with an OpenAI-shaped error – messages must be an array | 400 | – | 0 ms |

### `vendor/model-a`

#### Native compatibility

| Status | Required | Category | Test | Summary | HTTP | Tokens | Time |
|---:|---:|---|---|---|---:|---:|---:|
| PASS | yes | core | `chat.basic` | minimal chat completion succeeded – assistant content: AIQA_OK | 200 | 51 | 0 ms |
| PASS | no | messages | `chat.content_parts.text` | array-form text content parts are accepted and interpreted | 200 | 54 | 0 ms |
| PASS | no | sampling | `chat.logprobs` | logprobs and top_logprobs are returned | 200 | 53 | 0 ms |
| PASS | no | messages | `chat.multi_turn` | assistant history is accepted and used | 200 | 84 | 0 ms |
| PASS | no | messages | `chat.role.developer` | developer role is accepted and affects the response | 200 | 66 | 0 ms |
| PASS | no | messages | `chat.role.system` | system role is accepted and affects the response | 200 | 64 | 0 ms |
| PASS | yes | schema | `chat.schema` | chat completion response has the expected OpenAI shape | 200 | – | 0 ms |
| PASS | yes | streaming | `chat.stream.sse` | valid SSE stream with 3 event(s) and [DONE] | 200 | – | 0 ms |
| PASS | no | usage | `chat.stream.usage` | stream reported 64 total tokens | 200 | 64 | 0 ms |
| PASS | no | structured-output | `chat.structured.json_object` | JSON object mode returned raw JSON in the majority of attempts | 200 | 270 | 0 ms |
| PASS | no | structured-output | `chat.structured.json_schema` | strict JSON schema output conforms to the requested schema | 200 | 139 | 0 ms |
| PASS | no | tools | `chat.tools.auto` | tool_choice=auto emitted a tool call when requested | 200 | 140 | 0 ms |
| PASS | no | tools | `chat.tools.continuation` | assistant tool_call and tool result can be continued – assistant content: resource-alpha is available. | 200 | 231 | 0 ms |
| PASS | no | tools | `chat.tools.forced` | forced function call returned valid JSON arguments | 200 | 155 | 0 ms |
| PASS | no | tools | `chat.tools.none` | tool_choice=none suppressed tool calls | 200 | 143 | 0 ms |
| PASS | no | tools | `chat.tools.required` | tool_choice=required emitted a tool call | 200 | 141 | 0 ms |
| PASS | no | tools | `chat.tools.stream` | streamed tool-call deltas reconstructed into valid JSON arguments | 200 | – | 0 ms |
| PASS | yes | usage | `chat.usage` | usage reported 47 prompt + 4 completion tokens | 200 | 51 | 0 ms |
| PASS | no | discovery | `models.retrieve` | model metadata can be retrieved by ID | 200 | – | 0 ms |

#### Model parameters

| Status | Parameter | Variant | Accepted | Effective | Confidence | Summary | Tokens | Time |
|---:|---|---|---:|---:|---|---|---:|---:|
| PASS | `frequency_penalty` | `0.5` | yes | – | medium | frequency_penalty is accepted and an out-of-range value is rejected | 61 | 0 ms |
| PASS | `logprobs/top_logprobs` | `true/2` | yes | yes | high | logprobs are returned and invalid top_logprobs is rejected | 62 | 0 ms |
| PASS | `max_completion_tokens` | `limit=8` | yes | yes | high | max_completion_tokens is accepted, enforced, and validates invalid values | 189 | 0 ms |
| PASS | `max_tokens` | `limit=8` | yes | yes | high | max_tokens is accepted, enforced, and validates invalid values | 79 | 0 ms |
| PASS | `n` | `2` | yes | yes | high | n=2 returned two choices and n=0 was rejected | 57 | 0 ms |
| PASS | `parallel_tool_calls` | `true` | yes | yes | high | parallel_tool_calls emitted multiple independently identified calls and validates its type | 186 | 0 ms |
| PASS | `presence_penalty` | `0.5` | yes | – | medium | presence_penalty is accepted and an out-of-range value is rejected | 60 | 0 ms |
| PASS | `reasoning_effort` | `high` | yes | yes | high | reasoning_effort=high is accepted and exposes reasoning evidence | 81 | 0 ms |
| PASS | `reasoning_effort` | `low` | yes | yes | high | reasoning_effort=low is accepted and exposes reasoning evidence | 80 | 0 ms |
| PASS | `reasoning_effort` | `max` | yes | yes | high | reasoning_effort=max is accepted and exposes reasoning evidence | 80 | 0 ms |
| PASS | `reasoning_effort` | `medium` | yes | yes | high | reasoning_effort=medium is accepted and exposes reasoning evidence | 81 | 0 ms |
| PASS | `reasoning_effort` | `minimal` | yes | yes | medium | reasoning_effort=minimal participates in a validated family whose observable reasoning signal changes across modes | 82 | 0 ms |
| PASS | `reasoning_effort` | `none` | yes | yes | medium | reasoning_effort=none participates in a validated family whose observable reasoning signal changes across modes | 81 | 0 ms |
| PASS | `reasoning_effort` | `xhigh` | yes | yes | high | reasoning_effort=xhigh is accepted and exposes reasoning evidence | 81 | 0 ms |
| PASS | `reasoning_split` | `false` | yes | yes | high | reasoning_split=false produced a validated, observably different reasoning format | 80 | 0 ms |
| PASS | `reasoning_split` | `true` | yes | yes | high | reasoning_split=true produced a validated, observably different reasoning format | 80 | 0 ms |
| PASS | `seed` | `same vs different seeds` | yes | yes | medium | seed produced stable same-seed output and varied different-seed output | 356 | 0 ms |
| PASS | `stop` | `[STOP]` | yes | yes | high | stop is accepted, effective, and validates invalid values | 69 | 0 ms |
| PASS | `temperature` | `0.0 vs 1.8` | yes | yes | medium | temperature is accepted, validated, and changed sample diversity | 338 | 0 ms |
| PASS | `thinking` | `disabled` | yes | yes | medium | thinking=disabled participates in a validated family whose observable reasoning signal changes across modes | 82 | 0 ms |
| PASS | `thinking` | `enabled` | yes | yes | high | thinking=enabled is accepted and exposes reasoning evidence | 82 | 0 ms |
| PASS | `top_p` | `0.01 vs 1.0` | yes | yes | medium | top_p is accepted, validated, and changed sample diversity | 330 | 0 ms |
| PASS | `verbosity` | `low vs high` | yes | yes | medium | verbosity is validated and high produced a materially longer answer than low | 221 | 0 ms |

Context window: **PASS** — accepted context boundary is between 2313 and approximately 2555 prompt tokens

</details>

## Methodology and limitations

- **PASS** means the expected protocol or observable behavior was demonstrated.
- **WARN** means the semantic effect or correctness was not proved. **SKIP** means the check did not run.
- **UNSUP** means an optional capability was explicitly rejected with a suitable client error. **N/A** means the requirement does not apply.
- **FAIL** means observed behavior violated the applicable contract. **ERROR** is an inconclusive transport, provider, or runner failure.
- Findings and recommendations are deterministic guidance derived from canonical requirements and typed evidence. They do not change conformance status or exit policy.
- Context results distinguish a measured accepted lower bound from an estimated first rejected boundary. Token counts are exact only when the provider returns `usage.prompt_tokens`.
- Temperature, `top_p`, and seed checks are statistical. A warning does not prove that a parameter is ignored; it says this run did not produce enough evidence to claim effectiveness.

<details><summary>Legacy scoring</summary>

Legacy pass rate: 100.0%

Formula: `pass / (pass + fail + warn + unsupported)`. This deprecated value must not be used to rank reports with different verdicts.

</details>
