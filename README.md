# aiqa

Compatibility and behavior testing for OpenAI and Anthropic API facades.

## Quick start

```bash
export OPENAI_BASE_URL="<URL>/v1"
export OPENAI_API_KEY="<API_KEY>"

aiqa test=compat \
  --timeout=120 \
  --format=markdown,json \
  --name=compat-report
```

Configure both facades to assess them in one run:

```bash
export OPENAI_BASE_URL="http://127.0.0.1:8080/openai/v1"
export OPENAI_API_KEY="<OPENAI_API_KEY>"
export ANTHROPIC_BASE_URL="http://127.0.0.1:8080/anthropic"
export ANTHROPIC_API_KEY="<ANTHROPIC_API_KEY>"

aiqa --verbose test=compat \
  --format=markdown,json \
  --name=compat-report
```

The OpenAI base URL includes `/v1`. The Anthropic base URL is the prefix before `/v1`; AIQA appends `/v1/models`, `/v1/messages`, and `/v1/messages/count_tokens`. A configured base URL without its matching key is a configuration error. Use `--openai-model` and `--anthropic-model` for protocol-specific selection. The shared `--model` flag is rejected when both targets are active.

Test the parameters of every discovered model classified as Chat Completions capable:

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

When `--model` is not specified, `aiqa` preserves every `/models` entry in the inventory and runs only modules supported by the model's evidence-backed capabilities. Models are processed sequentially by default so parallel context probing does not unexpectedly test the provider's rate limiter instead of compatibility.

## Capability routing

OpenAI's standard model object does not declare endpoint capabilities. AIQA therefore uses explicit overrides first, then recognized vendor metadata and conservative model-name rules. When no capability evidence exists and nothing identifies the model as non-text, AIQA treats it as a text chat model with low confidence. Explicit image, embedding, audio, or video evidence still prevents chat routing, and conflicting evidence remains unknown.

Declare a known model explicitly:

```bash
aiqa test=compat \
  --model=custom-llm \
  --model-capability='custom-llm=chat.completions.text'
```

Multiple capabilities use `+`, and larger catalogs can use `--capability-file`:

```json
{
  "models": {
    "custom-llm": ["chat.completions.text"],
    "image-model": ["images.generations"]
  }
}
```

The default is `--unknown-model=chat`. Use `--unknown-model=inventory` when unclassified models must remain inventory-only and receive no capability tests.

Image generation is a billable module and remains skipped by default. Enable it deliberately for a selected model:

```bash
aiqa test=compat \
  --model=image-model \
  --model-capability='image-model=images.generations' \
  --module=images-generations \
  --allow-billable-artifacts \
  --max-artifacts-per-model=1
```

Reports retain only the observed image format, media type, byte size, dimensions, and SHA-256 digest. Base64 payloads, binary data, and signed URL query strings are not stored.

The native image probe consumes one artifact slot. Set `--max-artifacts-per-model=2` when the Official SDK image suite should run as well; the default limit of one prevents the external checker from creating a second billable image.

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

The current modular profile is in [`profile/openai-compat-2026-09.json`](profile/openai-compat-2026-09.json) and is embedded in the binary. The immutable [`profile/openai-compat-2026-08.json`](profile/openai-compat-2026-08.json) remains available for historical reproducibility. The profiles are based on the official OpenAI OpenAPI specification and augmented with semantic probes that cannot be expressed in JSON Schema alone.

The Anthropic compatibility contract is in [`profile/anthropic-compat-2026-09.json`](profile/anthropic-compat-2026-09.json). It pins `anthropic-version: 2023-06-01` and the official Go SDK v1.68.0. AIQA sends `x-api-key`, does not inherit `ANTHROPIC_AUTH_TOKEN` or beta headers, parses Anthropic content blocks and named SSE events, and keeps Anthropic token fields distinct from OpenAI usage fields. The profile covers Models, Messages, streaming, tools, Count Tokens, structured output, vision routing, and opt-in thinking. It does not claim coverage of the entire Anthropic Platform API.

See [`docs/anthropic-compatibility.md`](docs/anthropic-compatibility.md) for the complete activation, routing, module, streaming, SDK checker, report, and maintenance contract.

## Official SDK checkers

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

The native suite remains mandatory: the external checker is good at testing the SDK surface, but it does not prove that a parameter accepted by the server actually affected generation. Official SDK parseability is recorded as related interoperability evidence, so it does not receive a second vote for the native wire requirement.

Anthropic uses the separate checker in [`tools/anthropic-sdk-checker`](tools/anthropic-sdk-checker). It pins `anthropic-sdk-go v1.68.0`, disables SDK retries, accepts only explicit target credentials, and emits one `aiqa.external.v1` JSONL object per suite. `--anthropic-external=auto` uses an installed `anthropic-sdk-checker` binary and otherwise reports `not setup`; it does not download a binary or image. Use `--anthropic-external=docker` and `--anthropic-external-image` for an explicitly configured image.

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
--context-precision 512 tokens
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

## Statuses and verdicts

| Status | Meaning |
|---|---|
| `pass` | The expected contract or observable behavior was demonstrated |
| `warn` | The request was accepted, but the effect was not proven, validation is weak, or the conclusion is statistical |
| `unsupported` | An optional feature was explicitly rejected with 4xx |
| `fail` | The contract was violated, or an accepted feature behaves incompatibly |
| `error` | Transport/provider failure, timeout, 429, or 5xx, the conclusion is incomplete |
| `skip` | An applicable check did not run |
| `not_applicable` | The requirement does not apply to this provider or model and is excluded from coverage |

AIQA derives the authoritative result from canonical requirements, not the number of probes. `FAIL` means a core requirement failed; `INCONCLUSIVE` means core evidence is incomplete; `PARTIAL` means core passed but an optional requirement failed; `PASS-WITH-GAPS` means core passed with optional gaps; `PASS` means every applicable requirement passed.

### Exit codes

```text
0  The selected policy passed
1  The QA policy was violated
2  A configuration or report-writing error occurred
```

Policies:

| `--fail-on` | Exit 1 when |
|---|---|
| `core` | The canonical verdict is `FAIL` or `INCONCLUSIVE` |
| `any` | The canonical verdict is not `PASS` |
| `never` | Never because of the verdict; the report still includes all evidence |

## Reports

OpenAI-only runs retain the v1 root by default:

```json
{
  "schema_version": "aiqa.report.v1"
}
```

Anthropic-only and multi-target runs use `aiqa.report.v2`. Each target has its own profile, discovery inventory, models, canonical requirements, verdict, guidance, usage, and rate-limit telemetry. The root verdict applies the hardest target outcome and never computes a cross-protocol percentage. Select the contract explicitly with `--report-schema=v1|v2`; v1 is valid only for one OpenAI target.

It contains raw model metadata, probe evidence, canonical requirements, the authoritative assessment, findings, recommendations, HTTP observations, token usage, context samples, and confidence. The API key is not written to the report. External-checker output is redacted, deduplicated, and stored once as a referenced artifact.

Markdown leads with core conformance, optional coverage, evidence completeness, and prioritized actions. Passed checks and raw evidence are collapsed by default. AIQA emits no overall numeric compatibility score; compare reports by canonical verdict, then by the separate core, optional, and evidence measures.

Recommendations name exact verification targets but do not emit shell commands. A full compatibility rerun would not verify one finding in isolation; targeted execution and opt-in command rendering remain [backlog work](tasks/backlog.md).

Guidance is deterministic and uses no additional API calls:

```text
--guidance=full|summary|off
--guidance-pack=auto|generic|<id>
--guidance-locale=en
```

The schemas are [`schema/report.schema.json`](schema/report.schema.json), [`schema/report-v2.schema.json`](schema/report-v2.schema.json), and [`schema/profile-v2.schema.json`](schema/profile-v2.schema.json). A complete OpenAI run against a deterministic mock provider is available in [`examples/mock-report.md`](examples/mock-report.md) and [`examples/mock-report.json`](examples/mock-report.json). See [`docs/reports.md`](docs/reports.md) for exact verdict, status, priority, confidence, and multi-target semantics.

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
--target auto|openai|anthropic|openai,anthropic
--openai-model ID[,ID]
--anthropic-model ID[,ID]
--report-schema auto|v1|v2
--guidance full|summary|off
--guidance-pack auto|generic|ID
--guidance-locale en
```

`--concurrency` limits concurrently executing native probes across all model workers. Independent checks run in parallel, while checks with data dependencies remain ordered. The adaptive pacer may temporarily reduce the active request count after rate-limit responses.

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
- canonical requirement aggregation and actionable guidance
- Draft 2020-12 report-schema validation and deterministic guidance fixtures
- dual-facade OpenAI and Anthropic execution, protocol-specific headers, named Anthropic SSE events, and v2 report isolation
- the pinned Anthropic Official SDK checker as a separate Go module

Live CI jobs are disabled unless the repository variables `AIQA_LIVE_OPENAI`, `AIQA_LIVE_ANTHROPIC`, or `AIQA_LIVE_DUAL` equal `1`. They also require matching secret URLs and keys plus an explicit model variable. Live jobs are non-blocking, run one model, disable external checkers, and never scan a production catalog by default.

See [`docs/adding-tests.md`](docs/adding-tests.md) for how to add a new suite or probe

The profile pins the SHA of the official OpenAI OpenAPI snapshot, check whether upstream has moved forward:

```bash
make profile-upstream-check
go run ./script/check-profile-upstream.go --profile profile/anthropic-compat-2026-09.json
```

An upstream change does not automatically update the profile: first review the diff, adjust semantic probes, update `revision`/`source_commit`, and rerun the quality gates. Automatically rewriting a contract from a moving `main` would be convenient only until the first quiet-breaking change.

## Contract sources

- OpenAI OpenAPI specification: <https://github.com/openai/openai-openapi>
- Official OpenAI Go SDK: <https://github.com/openai/openai-go>
- External SDK compatibility tester: <https://github.com/beranekio/openai-compatibility-tester>
- Anthropic API versioning and Messages documentation: <https://platform.claude.com/docs/en/api/versioning>
- Official Anthropic Go SDK v1.68.0: <https://github.com/anthropics/anthropic-sdk-go/releases/tag/v1.68.0>

## License

MIT
