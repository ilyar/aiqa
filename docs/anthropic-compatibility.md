# Anthropic compatibility

AIQA assesses an Anthropic Messages facade as an independent protocol target. It does not translate Anthropic requests into OpenAI Chat Completions, share credentials between targets, or combine their results into one compatibility score.

The embedded contract is [`anthropic-compat-2026-09.json`](../profile/anthropic-compat-2026-09.json). It pins:

- `anthropic-version: 2023-06-01`;
- Official Anthropic Go SDK v1.68.0;
- the stable Models, Messages, and Count Tokens surfaces listed in the profile.

This is an observed compatibility assessment, not Anthropic certification, a security audit, or a model-quality benchmark.

## Configure a target

Set the Anthropic base URL to the prefix before `/v1`:

```bash
export ANTHROPIC_BASE_URL="http://127.0.0.1:8080/anthropic"
export ANTHROPIC_API_KEY="<ANTHROPIC_API_KEY>"

aiqa test=compat \
  --format=markdown,json \
  --name=anthropic-compat-report
```

AIQA calls:

```text
GET  <base>/v1/models
GET  <base>/v1/models/{model_id}
POST <base>/v1/messages
POST <base>/v1/messages/count_tokens
```

A trailing `/v1` is removed once and the normalization is recorded as `config.targets[].base_url_normalized` in report v3. A configured base URL without `ANTHROPIC_API_KEY` is a configuration error. A key without a base URL does not activate the target.

Use the global `--model` option to select one or more exact IDs. The list is intersected with the Anthropic Models inventory; an absent ID is not probed or materialized as a model report. A slash inside a model ID is encoded as one path parameter for native and SDK retrieve checks. If a facade supports Messages but intentionally omits Models, discovery is inconclusive; the current contract does not synthesize a model outside the inventory.

Native requests use `x-api-key` and the pinned `anthropic-version`. They do not inherit `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BETA`, or ambient custom headers.

## Capability routing

Anthropic classification uses only Anthropic target metadata and target-qualified overrides. It does not reuse OpenAI provider adapters or OpenAI model-name rules.

The default `--anthropic-unknown-model=inventory` policy keeps ambiguous inventory entries discovery-only. A complete Anthropic `ModelInfo` identifies a Messages route; incomplete metadata remains ambiguous and receives no automatic preflight. Use `--anthropic-unknown-model=messages` to opt into the low-confidence Messages fallback and its safe optional tools and structured-output probes. A clean 400, 404, or 422 response becomes `UNSUP`, not `N/A`.

Override incomplete metadata with:

```text
--anthropic-model-capability MODEL=CAPABILITY[+CAPABILITY]
--anthropic-capability-file PATH
```

Anthropic capability IDs are protocol-qualified:

```text
anthropic.messages.text
anthropic.messages.image_input
anthropic.messages.tools
anthropic.messages.structured_output
anthropic.messages.thinking.adaptive
anthropic.messages.thinking.manual
anthropic.messages.prompt_caching
```

In a multi-target run, generic model-capability controls are rejected as ambiguous.

## Native modules

| Module | Default | Main evidence |
|---|---:|---|
| `anthropic.discovery` | on | model list shape, pagination, authentication, version, and error envelopes |
| `anthropic.messages` | on | text messages, content forms, system, multi-turn, prefill, stops, usage, errors, and named-event streaming |
| `anthropic.tools` | on when applicable | auto, any, none, forced, parallel, strict input, continuation, and streamed partial JSON |
| `anthropic.token-counting` | on when applicable | Count Tokens shape, system/tools effects, and consistency with observed message input usage |
| `anthropic.structured-output` | on when applicable | JSON Schema request, exact local validation, stop semantics, and strict tool schema |
| `anthropic.vision` | on when applicable | bounded embedded PNG input and response semantics; no base64 payload is persisted |
| `anthropic.thinking` | off | adaptive, effort, manual budget, invalid budget, and thinking-stream structure |

Modules retain separate applicability and execution states. An applicable skipped module materializes visible `SKIP`; an executed module that omits a declared probe becomes an execution `ERROR`, never an implicit pass. Optional modules execute independently of the default Messages module when explicitly selected.

Thinking requires all three conditions:

1. a thinking capability from metadata or an explicit override;
2. `--module=anthropic.thinking`;
3. `--allow-high-token-probes`.

Thinking text and signatures are never retained. Reports keep only status, bounded metadata, usage, and structural validation results.

## Streaming and failure semantics

The Anthropic stream validator consumes named SSE events. It accepts pings and unknown future event names, accumulates text and partial tool JSON by block index, and requires legal message and content-block ordering through `message_stop`.

`[DONE]` is diagnosed as an OpenAI-style sentinel and is never used to terminate an Anthropic stream. Invalid known ordering is `FAIL`. EOF before `message_stop`, a mid-stream provider error, transport failure, HTTP 408 or 429, and all 5xx responses are `ERROR` because compatibility remains inconclusive.

For an applicable optional capability, a stable 400, 404, or 422 response is `UNSUP`. The same rejection cannot turn a required requirement into a pass or optional gap: required unsupported behavior is a core failure.

## SDK check

`official-sdk.anthropic` is compiled into AIQA and pins `anthropic-sdk-go v1.68.0`. Run it with `test=sdk` or combine it with native probes using `test=compat,sdk`.

The extension receives the selected target and subject, disables SDK retries, uses AIQA's bounded executor and adaptive HTTP transport, and emits each case through the shared extension validator. Current suites cover:

```text
models.list
models.get
messages
messages.stream
messages.system
messages.multi-turn
tools
tools.stream
count-tokens
structured-output
error.responses
```

Suite durations measure SDK request and parse work. Native and SDK evidence remain independent profile-bound requirements. A disagreement creates an evidence conflict and does not overwrite the native result. The general executable protocol and security model are documented in [`extension-suites.md`](extension-suites.md).

## Reports and exit policy

Anthropic-only and combined runs use `aiqa.report.v3`. Each target owns its profile, discovery inventory, selected models, requirements, assessment, guidance, usage, rate-limit telemetry, and grouped extension results. Subjects, evidence IDs, artifact references, recommendation targets, and verification targets carry target identity.

The root run verdict reflects the hardest target outcome and the configured `--fail-on` policy. It has no cross-protocol percentage.

The final write boundary redacts every configured key from typed fields, provider metadata, artifacts, and map keys. Extension runner diagnostics are additionally bounded, sanitized, deduplicated, and referenced rather than repeated.

## Verification and maintenance

Run the local gates:

```bash
make check
make race
make build
go run ./script/check-profile-upstream.go \
  --profile profile/anthropic-compat-2026-09.json
```

`make check` validates report v3, profile v3, `aiqa.ext.v1`, both built-in SDKs, the complete dual-facade mock, formatting, vet, and unit/integration tests.

Optional live CI jobs are disabled by default and non-blocking. They require explicit repository variables, target secrets, and one selected model. They omit `ext-suite`, billable artifact modules, and high-token thinking; use fixed profile request caps, a 30-second per-request timeout, and adaptive 250 ms initial pacing; and upload only redacted JSON reports with short retention.

Unimplemented surfaces are listed in [`tasks/backlog.md`](../tasks/backlog.md), including Anthropic model-parameter/context tests, prompt caching, document input, Message Batches, Files, a Messages-only profile, and multi-target HTML or SARIF output.
