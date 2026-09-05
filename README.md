# aiqa

Compatibility and behavior testing for OpenAI, Anthropic, and Google GenAI API facades.

## Quick start

```bash
export OPENAI_BASE_URL="<URL>/v1"
export OPENAI_API_KEY="<API_KEY>"

aiqa test=compat \
  --timeout=120 \
  --format=markdown,json \
  --name=compat-report
```

For a Gemini Developer API target, set the prefix before the API version. SDK checks follow the same target selection:

```bash
export GOOGLE_GENAI_BASE_URL="https://generativelanguage.googleapis.com"
export GOOGLE_GENAI_API_KEY="<GEMINI_API_KEY>"

aiqa test=compat,sdk \
  --target=google-genai \
  --model=gemini-2.5-flash \
  --format=markdown,json \
  --name=google-compat-report
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

The OpenAI base URL includes `/v1`. The Anthropic base URL is the prefix before `/v1`; AIQA appends `/v1/models`, `/v1/messages`, and `/v1/messages/count_tokens`. A configured base URL without its matching key is a configuration error. The global `--model` list is intersected independently with every active target inventory, so the same ID may be tested by more than one protocol. Removed protocol-specific model selectors and `GOOGLE_GENAI_MODEL` produce a migration error.

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

When `--model` is not specified, `aiqa` preserves every `/models` entry in the inventory, schedules only trusted entries, and runs only modules supported by evidence-backed capabilities. Ambiguous entries remain inventory-only. When an explicit ID is absent from a successfully retrieved target inventory, that target performs no model request for it. Models are processed sequentially by default so parallel context probing does not unexpectedly test the provider's rate limiter instead of compatibility.

The complete global-request, target-local routing, preflight, report, and
telemetry contract is in [`docs/unified-model-routing.md`](docs/unified-model-routing.md).

## Capability routing

OpenAI's standard model object does not declare endpoint capabilities. AIQA therefore uses explicit overrides first, then recognized vendor metadata and conservative model-name rules. When no capability evidence exists and nothing identifies the model as non-text, the default inventory policy leaves the entry untested; `--unknown-model=chat` opts into the low-confidence text fallback. Explicit image, embedding, audio, or video evidence still prevents chat routing, and conflicting evidence remains unknown.

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

The default is `--unknown-model=inventory`. Use `--unknown-model=chat` only when an unclassified OpenAI-compatible entry should receive the low-confidence text fallback. Anthropic uses the same conservative inventory default; `--anthropic-unknown-model=messages` is the opt-in fallback.

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

The native image probe consumes one artifact slot. Set `--max-artifacts-per-model=2` for a combined native and SDK image run; the default limit of one prevents the extension suite from creating a second billable image.

## Installation

Go 1.27 or later is required. The main module pins the official OpenAI, Anthropic, and Google Gen AI Go SDKs.

```bash
make install
```

## Example reports

Generate a successful report against the local model provider and an intentionally failing report against WireMock:

```bash
make example
```

The target builds the current AIQA source first, then runs the Go maintenance command `script/example.go`. If `model-provider` is not installed or cannot be started/read, it prints a warning and exits without changing the reports. If the configured provider is stopped, the target starts it with storage `/arch/model` on `0.0.0.0:8080`. The provider example targets all three compatible facades (OpenAI, Anthropic, and Google GenAI) and writes `example/good-report.md` and `example/good-report.json` using the native compatibility and SDK suites. It uses `--fail-on=never` so an incomplete facade does not prevent report generation; each target's actual verdict remains visible in the report. The failure example exercises all three protocol facades. Set `AIQA_EXAMPLE_PROVIDER_CONFIG` when the provider registry must be kept in a different writable location; the value is passed as the provider's global `--config` option.

The failure example uses the pinned official WireMock Docker image and the fixtures in `test/wiremock`. Separate OpenAI, Anthropic, and Google GenAI model inventories let discovery complete for every target, while all other API operations return an intentional `503`; the resulting `example/bad-report.md` and `example/bad-report.json` demonstrate execution errors without representing a checker startup failure. Set `AIQA_EXAMPLE_PROVIDER_STORAGE`, `AIQA_EXAMPLE_PROVIDER_LISTEN`, or `AIQA_EXAMPLE_WIREMOCK_IMAGE` to override the defaults.

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

See [`docs/anthropic-compatibility.md`](docs/anthropic-compatibility.md) for the complete activation, routing, module, streaming, SDK, report, and maintenance contract.

The Google GenAI compatibility contract has two profiles: [`profile/google-genai-compat-2026-09.json`](profile/google-genai-compat-2026-09.json) tests the modern Interactions surface with optional legacy methods, while [`profile/google-genai-generate-content-compat-2026-09.json`](profile/google-genai-generate-content-compat-2026-09.json) is a strict legacy `generateContent` contract. Google is an independent Gemini Developer API target, defaults to `v1beta`, and authenticates with `x-goog-api-key`. Configure it with `GOOGLE_GENAI_BASE_URL` and `GOOGLE_GENAI_API_KEY`; `GOOGLE_GEMINI_BASE_URL`, `GOOGLE_API_KEY`, and `GEMINI_API_KEY` are compatibility aliases. See [`docs/google-genai-compatibility.md`](docs/google-genai-compatibility.md).

Google routing is metadata-first: `supportedGenerationMethods` enables `generateContent`, `countTokens`, and `embedContent` modules independently. `generateContent` never implies Interactions. The modern profile selects Interactions only for an explicitly selected model or its reviewed allowlist; an unknown or embedding-only model is not sent through that surface. Unclassified Google models remain inventory-only by default (`--google-genai-unknown-model=text` opts into a text fallback).

## Extension suites and SDK checks

SDK checks are compiled into AIQA and use the same extension contract as installed third-party suites. Select the built-in checks directly with `sdk`:

```bash
aiqa test=sdk
aiqa test=compat,sdk
aiqa test=ext-suite
aiqa test=compat,ext-suite
aiqa test=ext-suite --ext-suite=official-sdk.openai
```

`sdk` expands to the built-in OpenAI, Anthropic, and/or Google Gen AI SDK checks for the active targets. It does not add a check whose matching base URL and API key are absent. `ext-suite` remains the generic extension boundary: it runs all applicable built-ins and trusted executables, with `--ext-suite` available for exact ID filters.

The binary pins the official OpenAI Go SDK `v3.54.0`, Anthropic Go SDK `v1.68.0`, and Google Gen AI Go SDK `v1.71.0`. SDK retries are disabled, and requests share AIQA's global concurrency and adaptive pacing policy. Each suite contributes separately through an explicit `aiqa.profile.v3` binding. The pinned Google SDK has no public Interactions methods, so AIQA does not claim SDK coverage for that surface.

Third-party suites are trusted executables. Supply an absolute path or install a matching `aiqa-*-ext-suite` command:

```bash
aiqa test=ext-suite \
  --ext-suite=acme.gateway \
  --ext-suite-path=/opt/aiqa/aiqa-acme-ext-suite
```

AIQA passes credentials to executables only through stdin. Executables are not sandboxed and run with the user's OS privileges. See [`docs/extension-suites.md`](docs/extension-suites.md) for discovery, `aiqa.ext.v1`, profile bindings, status derivation, security limits, and the author fixture.

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

Every run uses one report contract:

```json
{
  "schema_version": "aiqa.report.v3"
}
```

Each target has its own profile, discovery inventory, models, canonical requirements, verdict, guidance, usage, rate-limit telemetry, and grouped extension results. The root verdict applies the hardest target outcome and never computes a cross-protocol percentage.

It contains raw model metadata, probe evidence, canonical requirements, the authoritative assessment, findings, recommendations, HTTP observations, token usage, context samples, extension provenance, and confidence. The API key is not written to the report. Failed extension-runner diagnostics are redacted, deduplicated, and stored once as referenced artifacts.

Markdown leads with core conformance, optional coverage, evidence completeness, and prioritized actions. Passed checks and raw evidence are collapsed by default. AIQA emits no overall numeric compatibility score; compare reports by canonical verdict, then by the separate core, optional, and evidence measures.

Recommendations name exact verification targets but do not emit shell commands. A full compatibility rerun would not verify one finding in isolation; targeted execution and opt-in command rendering remain [backlog work](tasks/backlog.md).

Guidance is deterministic and uses no additional API calls:

```text
--guidance=full|summary|off
--guidance-pack=auto|generic|<id>
--guidance-locale=en
```

The public schemas are [`schema/report-v3.schema.json`](schema/report-v3.schema.json), [`schema/profile-v3.schema.json`](schema/profile-v3.schema.json), and [`schema/ext-v1.schema.json`](schema/ext-v1.schema.json). See [`docs/reports.md`](docs/reports.md) for exact verdict, status, priority, confidence, and multi-target semantics.

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
--ext-suite ID[,ID]
--ext-suite-path /absolute/path
--target auto|openai|anthropic|google-genai|...
--model ID[,ID]
--google-genai-profile FILE
--guidance full|summary|off
--guidance-pack auto|generic|ID
--guidance-locale en
```

`--concurrency` limits concurrently executing native and built-in extension requests and reserves capacity for executable extensions. Independent checks run in parallel, while checks with data dependencies remain ordered. The adaptive pacer may temporarily reduce the active request count after rate-limit responses.

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
- built-in and executable extension runners
- canonical requirement aggregation and actionable guidance
- Draft 2020-12 report-schema validation and deterministic guidance fixtures
- independent OpenAI, Anthropic, and Google GenAI execution, protocol-specific headers, named SSE events, and v3 report isolation
- pinned built-in OpenAI, Anthropic, and Google GenAI SDK suites

Live CI jobs are disabled unless the repository variables `AIQA_LIVE_OPENAI`, `AIQA_LIVE_ANTHROPIC`, `AIQA_LIVE_DUAL`, or `AIQA_LIVE_GOOGLE_GENAI` equal `1`. They also require matching secret URLs and keys plus an explicit model variable. OpenAI and Anthropic jobs are non-blocking, run one model, and omit `ext-suite`; the Google job is equally bounded but exercises the explicitly selected native-plus-SDK path. No job scans a production catalog by default.

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
- Anthropic API versioning and Messages documentation: <https://platform.claude.com/docs/en/api/versioning>
- Official Anthropic Go SDK v1.68.0: <https://github.com/anthropics/anthropic-sdk-go/releases/tag/v1.68.0>

## License

MIT
