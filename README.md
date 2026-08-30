# aiqa

QA utility for testing AI things

## Quick start

```bash
export OPENAI_BASE_URL="<URL>/v1"
export OPENAI_API_KEY="<API_KEY>"

aiqa test=compat \
  --timeout=120 \
  --format=markdown,json \
  --name=compat-report
```

Test the parameters of every model returned by `/models`:

```bash
aiqa test=model-param \
  --timeout 120 \
  --format markdown,json \
  --name=model-param-report
```

Run both suites at once:

```bash
aiqa test=compat,model-param \
  --timeout=120 \
  --format=markdown,json \
  --name=report
```

Test one or more explicitly selected models:

```bash
aiqa test=compat,model-param \
  --model moonshotai/Kimi-K2.6,deepseek-ai/DeepSeek-V4-Flash-0731
```

When `--model` is not specified, `aiqa` retrieves the list through `/models` and tests every discovered model, models are processed sequentially by default so parallel context probing does not unexpectedly test the provider's rate limiter's ability to reincarnate

## Installation

Go 1.27 or later is required, the native core has no external Go dependencies

```bash
make install
```

## `test=compat`

The native profile checks several levels of compatibility

### Discovery and schema

- HTTP and JSON for `GET /models`
- OpenAI list shape and unique model IDs
- retrieval of `/models/{model}`
- required fields of a regular Chat Completion
- correct `usage`

### Messages

- `system` and `developer` roles
- array-form text content parts
- multi-turn history with an input `assistant` message

### Streaming

- `Content-Type: text/event-stream`
- SSE syntax
- JSON shape of every `chat.completion.chunk`
- content assembly
- `finish_reason`
- `[DONE]` terminator
- `stream_options.include_usage`

### Tool calling

- `tool_choice=none`
- `tool_choice=auto`
- `tool_choice=required`
- forced function choice
- valid JSON arguments
- assembly of tool-call arguments from streaming deltas
- the complete `user → assistant(tool_calls) → tool → assistant` cycle

### Structured output and sampling

- `response_format=json_object`
- strict `json_schema` with actual result validation
- `logprobs` and `top_logprobs`

### Errors

- unknown model
- invalid API key
- malformed JSON
- incorrect request-field types
- expected HTTP class and OpenAI-shaped `error` object

The current profile is in [`profile/openai-compat-2026-08.json`](profile/openai-compat-2026-08.json) and is embedded in the binary. It is based on the official OpenAI OpenAPI specification and augmented with semantic probes that cannot be expressed in JSON Schema alone.

## Existing official SDK checker

The built-in checks are complemented by [`beranekio/openai-compatibility-tester`](https://github.com/beranekio/openai-compatibility-tester), it calls the endpoint through the official OpenAI Go SDK and detects incompatibilities that prevent a real SDK from sending a request or parsing a response

```bash
aiqa test=compat --external=auto
```

Modes:

| Mode | Behavior |
|---|---|
| `auto` | Uses an installed binary, otherwise, uses an already local Docker image, it does not silently download anything |
| `off` | Native probes only |
| `binary` | Requires `openai-compatibility-tester` in `PATH`, its absence is an error |
| `docker` | Runs the Docker image, Docker can download it if it is missing |

For reproducibility, the image can be pinned to a tag or digest:

```bash
aiqa test=compat \
  --external=docker \
  --external-image='ghcr.io/beranekio/openai-compatibility-tester:latest'
```

The same value is read from `AIQA_EXTERNAL_IMAGE`

The native suite remains mandatory: the external checker is good at testing the SDK surface, but it does not prove that a parameter accepted by the server actually affected generation

## `test=model-param`

Each test distinguishes three claims whenever possible:

1. **Accepted** – the request received a 2xx response
2. **Validated** – a deliberately invalid value was rejected with 4xx instead of silently swallowed by a gateway
3. **Effective** – observed behavior changed as it should have

`HTTP 200` only means that the server was in a good mood, it is not proof that a parameter is supported

The following are tested:

- `max_completion_tokens` and legacy `max_tokens`
- context-window boundary
- `reasoning_effort`: `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`
- vendor extensions `thinking.enabled/disabled` and `reasoning_split=true/false`
- `temperature`
- `top_p`
- `seed`
- `stop`
- `n`
- `presence_penalty`
- `frequency_penalty`
- `logprobs/top_logprobs`
- `verbosity=low/high`
- `parallel_tool_calls`

Statistical parameters (`temperature`, `top_p`, `seed`, `verbosity`) are checked with several calls, a lack of observable difference produces `warn`, not an overconfident verdict: a small sample cannot read the sampling engine's karma

### Context window

Algorithm:

1. Sends a deterministic, incompressible payload
2. Increases its size exponentially until the first context overflow
3. Performs a binary search
4. Uses `usage.prompt_tokens` when the provider returns it
5. Otherwise, calibrates an estimate by bytes per token
6. Separately reports the accepted lower bound and estimated first rejected boundary

By default:

```text
--context-max       1,100,000 tokens
--context-precision 16,384 tokens
--context-budget    8,000,000 prompt tokens per model
--context-max-bytes 32 MiB per request
```

This incurs actual API usage, for three models, the upper budget bound is about 24M prompt tokens, though the search often completes earlier, changing the limits as follows:

```bash
aiqa test=model-param \
  --context-max=1200k \
  --context-precision=8k \
  --context-budget=12m
```

`aiqa` measures **accepted context**, it does not assess how well a model uses information from the beginning of a long document, that requires a separate needle-in-a-haystack or multi-hop benchmark, which is planned for future QA suites

## Statuses

| Status | Meaning |
|---|---|
| `pass` | The expected contract or observable behavior was demonstrated |
| `warn` | The request was accepted, but the effect was not proven, validation is weak, or the conclusion is statistical |
| `unsupported` | An optional feature was explicitly rejected with 4xx |
| `fail` | The contract was violated, or an accepted feature behaves incompatibly |
| `error` | Transport/provider failure, timeout, 429, or 5xx, the conclusion is incomplete |
| `skip` | The check is inapplicable, or an external tool was not found in `auto` mode |

### Exit codes

```text
0  The selected policy passed
1  The QA policy was violated
2  A configuration or report-writing error occurred
```

Policies:

| `--fail-on` | Exit 1 when |
|---|---|
| `core` | There is a required failure, any `fail`, or an `error`, `warn/unsupported` are allowed |
| `any` | There is any `fail/error/warn/unsupported` |
| `never` | Never because of probe results, the report still includes statuses |

## Reports

JSON has a versioned root:

```json
{
  "schema_version": "aiqa.report.v1"
}
```

It contains raw model metadata, results for every probe, HTTP observations, token usage, context samples, confidence, and an aggregate summary, the API key is not written to the report, and external-checker output is additionally redacted

Markdown is intended for people and CI artifacts, the JSON Schema is in [`schema/report.schema.json`](schema/report.schema.json), an example of a complete combined run against a deterministic mock provider is available in [`examples/mock-report.md`](examples/mock-report.md) and [`examples/mock-report.json`](examples/mock-report.json)

## Full CLI reference

```bash
aiqa --help
```

Main additional options:

```text
--concurrency N
--samples N
--allow-http
--verbose
--fail-on core|any|never
--external auto|off|binary|docker
```

Plain HTTP is automatically allowed only for loopback, any other address requires explicit `--allow-http`

## Development

```bash
make check
make race
make build
```

Integration tests run a deterministic mock endpoint and check:

- both report formats
- all built-in compatibility probes
- model parameter probes
- discovery of an artificial context-window boundary
- absence of the API key in JSON
- exit policies
- external runner modes

See [`docs/adding-tests.md`](docs/adding-tests.md) for how to add a new suite or probe

The profile pins the SHA of the official OpenAI OpenAPI snapshot, check whether upstream has moved forward:

```bash
make profile-upstream-check
```

An upstream change does not automatically update the profile: first review the diff, adjust semantic probes, update `revision`/`source_commit`, and rerun the quality gates. Automatically rewriting a contract from a moving `main` would be convenient only until the first quiet-breaking change.

## Contract sources

- OpenAI OpenAPI specification: <https://github.com/openai/openai-openapi>
- Official OpenAI Go SDK: <https://github.com/openai/openai-go>
- External SDK compatibility tester: <https://github.com/beranekio/openai-compatibility-tester>

## License

MIT
