# AIQA report

- **Generated:** 2026-08-30 01:22:01Z
- **Tool:** `aiqa 0.1.0-example` (`example`, built `2026-08-30T00:00:00Z`)
- **Endpoint:** `http://127.0.0.1:46561/v1`
- **Suites:** `compat,model-param`
- **Timeout:** `5s` per request
- **Profile:** `openai-chat-compat@2026-08-30` (`690521b1753d`) – GET /v1/models, GET /v1/models/{model}, POST /v1/chat/completions
- **External checker:** `off`

## Summary

| Overall | Score | Pass | Fail | Warn | Unsupported | Skip | Error | Required |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| ✅ pass | 100.0% | 49 | 0 | 0 | 0 | 0 | 0 | 8/8 |

Observed API usage: **4080 prompt + 282 completion = 4362 total tokens**. Context probing additionally sent approximately **10644 prompt tokens**.

## Model discovery

| Status | Required | Category | Test | Summary | HTTP | Tokens | Time |
|---:|---:|---|---|---|---:|---:|---:|
| ✅ pass | no | errors | `error.invalid_auth` | invalid authentication returned HTTP 401 with an OpenAI-shaped error – invalid API key | 401 | – | 0 ms |
| ✅ pass | yes | errors | `error.invalid_json` | malformed JSON returned HTTP 400 with an OpenAI-shaped error – malformed JSON | 400 | – | 0 ms |
| ✅ pass | yes | errors | `error.invalid_model` | invalid model returned HTTP 404 with an OpenAI-shaped error – unknown model | 404 | – | 3 ms |
| ✅ pass | no | errors | `error.invalid_types` | invalid request field types returned HTTP 400 with an OpenAI-shaped error – messages must be an array | 400 | – | 0 ms |
| ✅ pass | yes | discovery | `models.list.http` | GET /models returned HTTP 200 JSON | 200 | – | 2 ms |
| ✅ pass | yes | schema | `models.list.schema` | model list follows the OpenAI list shape | 200 | – | 2 ms |

Discovered models: `vendor/model-a`.

## Models

| Model | Status | Score | Required | Context | Tests |
|---|---:|---:|---:|---:|---:|
| `vendor/model-a` | ✅ pass | 100.0% | 4/4 | 2,143–~2,720 | 43 |

## `vendor/model-a`

| Overall | Score | Pass | Fail | Warn | Unsupported | Skip | Error | Required |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| ✅ pass | 100.0% | 43 | 0 | 0 | 0 | 0 | 0 | 4/4 |

### OpenAI compatibility

| Status | Required | Category | Test | Summary | HTTP | Tokens | Time |
|---:|---:|---|---|---|---:|---:|---:|
| ✅ pass | yes | core | `chat.basic` | minimal chat completion succeeded – assistant content: AIQA_OK | 200 | 51 | 0 ms |
| ✅ pass | no | messages | `chat.content_parts.text` | array-form text content parts are accepted and interpreted | 200 | 54 | 0 ms |
| ✅ pass | no | sampling | `chat.logprobs` | logprobs and top_logprobs are returned | 200 | 53 | 0 ms |
| ✅ pass | no | messages | `chat.multi_turn` | assistant history is accepted and used | 200 | 84 | 0 ms |
| ✅ pass | no | messages | `chat.role.developer` | developer role is accepted and affects the response | 200 | 66 | 0 ms |
| ✅ pass | no | messages | `chat.role.system` | system role is accepted and affects the response | 200 | 64 | 0 ms |
| ✅ pass | yes | schema | `chat.schema` | chat completion response has the expected OpenAI shape | 200 | – | 0 ms |
| ✅ pass | yes | streaming | `chat.stream.sse` | valid SSE stream with 3 event(s) and [DONE] | 200 | – | 1 ms |
| ✅ pass | no | usage | `chat.stream.usage` | stream reported 64 total tokens | 200 | 64 | 1 ms |
| ✅ pass | no | structured-output | `chat.structured.json_object` | JSON object mode returned valid JSON | 200 | 73 | 0 ms |
| ✅ pass | no | structured-output | `chat.structured.json_schema` | strict JSON schema output conforms to the requested schema | 200 | 139 | 0 ms |
| ✅ pass | no | tools | `chat.tools.auto` | tool_choice=auto emitted a tool call when requested | 200 | 132 | 0 ms |
| ✅ pass | no | tools | `chat.tools.continuation` | assistant tool_call and tool result can be continued – assistant content: It is 21°C in Bratislava. | 200 | 215 | 0 ms |
| ✅ pass | no | tools | `chat.tools.forced` | forced function call returned valid JSON arguments | 200 | 157 | 0 ms |
| ✅ pass | no | tools | `chat.tools.none` | tool_choice=none suppressed tool calls | 200 | 137 | 0 ms |
| ✅ pass | no | tools | `chat.tools.required` | tool_choice=required emitted a tool call | 200 | 134 | 1 ms |
| ✅ pass | no | tools | `chat.tools.stream` | streamed tool-call deltas reconstructed into valid JSON arguments | 200 | – | 0 ms |
| ✅ pass | yes | usage | `chat.usage` | usage reported 47 prompt + 4 completion tokens | 200 | 51 | 0 ms |
| ✅ pass | no | discovery | `models.retrieve` | model metadata can be retrieved by ID | 200 | – | 0 ms |

### Model parameters

| Status | Parameter | Variant | Accepted | Effective | Confidence | Summary | Tokens | Time |
|---:|---|---|---:|---:|---|---|---:|---:|
| ✅ pass | `frequency_penalty` | `0.5` | yes | – | medium | frequency_penalty is accepted and an out-of-range value is rejected | 61 | 0 ms |
| ✅ pass | `logprobs/top_logprobs` | `true/2` | yes | yes | high | logprobs are returned and invalid top_logprobs is rejected | 62 | 0 ms |
| ✅ pass | `max_completion_tokens` | `limit=8` | yes | yes | high | max_completion_tokens is accepted, enforced, and validates invalid values | 189 | 0 ms |
| ✅ pass | `max_tokens` | `limit=8` | yes | yes | high | max_tokens is accepted, enforced, and validates invalid values | 79 | 0 ms |
| ✅ pass | `n` | `2` | yes | yes | high | n=2 returned two choices and n=0 was rejected | 57 | 0 ms |
| ✅ pass | `parallel_tool_calls` | `true` | yes | yes | high | parallel_tool_calls emitted multiple independently identified calls and validates its type | 176 | 0 ms |
| ✅ pass | `presence_penalty` | `0.5` | yes | – | medium | presence_penalty is accepted and an out-of-range value is rejected | 60 | 0 ms |
| ✅ pass | `reasoning_effort` | `high` | yes | yes | high | reasoning_effort=high is accepted and exposes reasoning evidence | 81 | 0 ms |
| ✅ pass | `reasoning_effort` | `low` | yes | yes | high | reasoning_effort=low is accepted and exposes reasoning evidence | 80 | 0 ms |
| ✅ pass | `reasoning_effort` | `max` | yes | yes | high | reasoning_effort=max is accepted and exposes reasoning evidence | 80 | 0 ms |
| ✅ pass | `reasoning_effort` | `medium` | yes | yes | high | reasoning_effort=medium is accepted and exposes reasoning evidence | 81 | 0 ms |
| ✅ pass | `reasoning_effort` | `minimal` | yes | yes | medium | reasoning_effort=minimal participates in a validated family whose observable reasoning signal changes across modes | 82 | 0 ms |
| ✅ pass | `reasoning_effort` | `none` | yes | yes | medium | reasoning_effort=none participates in a validated family whose observable reasoning signal changes across modes | 81 | 0 ms |
| ✅ pass | `reasoning_effort` | `xhigh` | yes | yes | high | reasoning_effort=xhigh is accepted and exposes reasoning evidence | 81 | 0 ms |
| ✅ pass | `reasoning_split` | `false` | yes | yes | high | reasoning_split=false produced a validated, observably different reasoning format | 80 | 0 ms |
| ✅ pass | `reasoning_split` | `true` | yes | yes | high | reasoning_split=true produced a validated, observably different reasoning format | 80 | 0 ms |
| ✅ pass | `seed` | `same vs different seeds` | yes | yes | medium | seed produced stable same-seed output and varied different-seed output | 356 | 0 ms |
| ✅ pass | `stop` | `[STOP]` | yes | yes | high | stop is accepted, effective, and validates invalid values | 69 | 0 ms |
| ✅ pass | `temperature` | `0.0 vs 1.8` | yes | yes | medium | temperature is accepted, validated, and changed sample diversity | 338 | 1 ms |
| ✅ pass | `thinking` | `disabled` | yes | yes | medium | thinking=disabled participates in a validated family whose observable reasoning signal changes across modes | 82 | 0 ms |
| ✅ pass | `thinking` | `enabled` | yes | yes | high | thinking=enabled is accepted and exposes reasoning evidence | 82 | 0 ms |
| ✅ pass | `top_p` | `0.01 vs 1.0` | yes | yes | medium | top_p is accepted, validated, and changed sample diversity | 330 | 0 ms |
| ✅ pass | `verbosity` | `low vs high` | yes | yes | medium | verbosity is validated and high produced a materially longer answer than low | 221 | 12 ms |

### Context window

**✅ pass:** accepted context boundary is between 2143 and approximately 2720 prompt tokens

| Declared | Accepted lower bound | First rejected estimate | Precision | Confidence | Probes | Probe tokens | Output cap |
|---:|---:|---:|---:|---|---:|---:|---|
| 4,096 | 2,143 | 2,720 | 512 | high | 6 | 10,644 | `max_completion_tokens` |

<details><summary>Context probe samples</summary>

| Payload bytes | Reported prompt tokens | Estimated prompt tokens | Accepted | Overflow | HTTP | Time | Error |
|---:|---:|---:|---:|---:|---:|---:|---|
| 1,024 | 436 | 470 | yes | no | 200 | 0 ms |  |
| 2,048 | 777 | 872 | yes | no | 200 | 0 ms |  |
| 4,096 | 1,460 | 1,744 | yes | no | 200 | 1 ms |  |
| 8,192 | – | 3,108 | no | yes | 400 | 0 ms | maximum context length exceeded |
| 6,144 | 2,143 | 2,331 | yes | no | 200 | 0 ms |  |
| 7,168 | – | 2,720 | no | yes | 400 | 1 ms | maximum context length exceeded |

</details>

## Interpretation

- **Pass** means the expected protocol or observable behavior was demonstrated.
- **Warn** means the request was accepted, but semantic effect could not be proved, validation was lax, or evidence was statistical.
- **Unsupported** is an explicit 4xx rejection of an optional feature. It is a capability gap, not necessarily a broken core API.
- **Fail** means a required contract or an accepted feature behaved incompatibly. **Error** is an inconclusive transport/provider failure.
- Context results distinguish a measured accepted lower bound from an estimated first rejected boundary. Token counts are exact only when the provider returns `usage.prompt_tokens`.
- Temperature, `top_p`, and seed checks are statistical. A warning does not prove that a parameter is ignored; it says this run did not produce enough evidence to claim effectiveness.
