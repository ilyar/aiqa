# Google GenAI compatibility

AIQA treats the Gemini Developer API as an independent `google-genai` target.
It does not translate Google traffic into OpenAI or Anthropic requests, and it
does not treat a Google error, rate limit, or missing SDK method as evidence
that another protocol is incompatible.

## Activation and configuration

Google is activated only when a base URL and a usable key are both available.
There is no implicit Google endpoint, so a key in the environment is never
enough to create an active target.

```bash
GOOGLE_GENAI_BASE_URL="https://generativelanguage.googleapis.com" \
GOOGLE_GENAI_API_KEY="$GEMINI_API_KEY" \
aiqa test=compat,sdk \
  --target=google-genai \
  --model=gemini-2.5-flash \
  --format=markdown,json \
  --name=google-genai-compat
```

The accepted controls are:

| Purpose | Canonical | Compatibility aliases and precedence |
|---|---|---|
| Base URL | `--google-genai-base-url`, `GOOGLE_GENAI_BASE_URL` | `GOOGLE_GEMINI_BASE_URL` |
| API key | `--google-genai-api-key`, `GOOGLE_GENAI_API_KEY` | `GOOGLE_API_KEY`, then `GEMINI_API_KEY` |
| API version | `--google-genai-api-version`, `GOOGLE_GENAI_API_VERSION` | default `v1beta` |
| Profile | `--google-genai-profile`, `GOOGLE_GENAI_PROFILE` | default `google-genai-compat-2026-09.json` |
| Models | global `--model` | otherwise discovered from `/models` |

An explicit flag wins over its environment counterpart. A canonical key wins
over aliases; conflicting `GOOGLE_API_KEY` and `GEMINI_API_KEY` values fail
configuration unless `GOOGLE_GENAI_API_KEY` is set. Aliases are read only
after Google has been activated. With `--target=auto`, all complete configured
OpenAI, Anthropic, and Google pairs are selected in deterministic order. The
global `--model` list is intersected independently with each target inventory;
an ID missing from one target is silently omitted there. `GOOGLE_GENAI_MODEL`
is no longer accepted and must be migrated to `--model`.

The configured Google URL is the prefix immediately before the API version.
For example, a base of `http://127.0.0.1:8080/google/v1beta/` and version
`v1beta` produces `/google/v1beta/models` exactly once. Existing path prefixes
are preserved; a trailing version segment and slashes are normalized. Query
strings, fragments, URL credentials, and non-TLS non-loopback endpoints are
rejected. Native authentication is only the `x-goog-api-key` header; the key
is never put in a URL.

## Profiles and routing

Two embedded `aiqa.profile.v3` profiles make the tested claim explicit:

| Profile | Core claim | Optional surfaces |
|---|---|---|
| [`google-genai-compat-2026-09.json`](../profile/google-genai-compat-2026-09.json) | Models plus current Interactions | legacy `generateContent`, streams, counting, embeddings, tools, structured output, vision, and thinking |
| [`google-genai-generate-content-compat-2026-09.json`](../profile/google-genai-generate-content-compat-2026-09.json) | Models plus legacy `generateContent` and its stream | Interactions and auxiliary surfaces |

The modern profile is the default. Its Interactions core is not inferred from
`supportedGenerationMethods`: that metadata describes model methods such as
`generateContent`, `countTokens`, and `embedContent`, not universal
Interactions support. A model is eligible for Interactions only when it was
explicitly selected or appears in the versioned profile allowlist. If no
eligible model is present, the modern core is reported as `INCONCLUSIVE` and
the report asks for an explicit model rather than claiming a pass.

Classification is target-local and metadata-first:

1. `--google-genai-model-capability` or `--google-genai-capability-file`.
2. `supportedGenerationMethods` for generation, counting, and embeddings.
3. explicit vision and thinking metadata.
4. the modern profile's Interactions selection policy.
5. the configured unknown-model policy (`inventory` by default; `text` is an
   explicit low-confidence fallback).

An embedding-only model never receives generation or Interactions calls. An
unknown model does not automatically receive every suite. Raw model metadata
is retained in JSON while normalized IDs remove only the `models/` prefix.

## Native REST evidence

Native probes are implemented in [`internal/protocol/googlegenai`](../internal/protocol/googlegenai/).
The client owns URL construction, `x-goog-api-key` authentication, adaptive
pacing, bounded responses, and target-local telemetry. URL-addressed model
operations explicitly carry the model identity into the pacing layer, so
per-model rate-limit and latency statistics include generation, counting,
embedding, retrieval, and cleanup requests. It covers:

- paginated Models list, unique names, model retrieval, limits, methods,
  thinking metadata, and authentication;
- Interactions requests with `store:false` by default, response schema,
  status, output extraction, usage, tools, structured output, vision, errors,
  and an opt-in stateful continuation;
- legacy `generateContent`, its own stream parser, tools, structured output,
  vision, thinking, errors, token counting, and embeddings;
- separate stream validators for Interactions SSE and legacy
  `GenerateContentResponse` chunks.

The Interactions state machine accepts unknown future event names but enforces
the known lifecycle, step identifiers, event-name/type agreement, valid tool
argument accumulation, `interaction.completed`, `event: done`, and
`data: [DONE]`. Legacy streaming terminates at EOF and is not required to
emit Interactions events or an OpenAI sentinel. Provider in-band errors and
transient HTTP failures are recorded as `ERROR`, not as capability failures.

Usage normalization maps both Google naming families (`promptTokenCount` and
`total_input_tokens`, for example) to common input, output, cache-read,
thinking, and total fields. Provider-specific tool counters remain under
`provider_fields`. Arithmetic differences are diagnostic; AIQA does not invent
an exact total or double-count thought tokens.

The default suite sends no retained resource, media artifact, server-side tool,
agent, or generated media. Thinking requires `--allow-high-token-probes`.
Stateful testing additionally requires both an explicit stateful module and
`--allow-persistent-resources`; it creates `store:true` state, verifies
`previous_interaction_id` continuation, deletes every created resource, and
reports cleanup separately. A cleanup failure carries a P0 catalog finding.
Resource IDs are represented only by short hashes in evidence.

Google model-parameter analysis is not silently omitted. When
`test=model-param` is requested for an applicable generation model, the report
contains an explicit optional `UNSUP` notice stating that the Google
parameter suite is not implemented in this release and no parameter calls
were made.

## SDK check

`official-sdk.google-genai` is a built-in extension backed by
[`google.golang.org/genai`](https://github.com/googleapis/go-genai) `v1.71.0`,
reviewed at commit
`bfe8a871478bdec16932d7809e8f8bff122569d3`. It uses
`BackendGeminiAPI`, the target's base URL and API version, the target-owned
pacing HTTP client, and one SDK attempt (AIQA owns retries and rate-limit
telemetry).

The pinned public Go client exposes these suites:

```text
models.list, models.get, generate-content, generate-content-stream,
count-tokens, embeddings, tools, structured-output, multimodal
```

The SDK has no public Interactions service at this pin. AIQA records that as a
target note and does not fabricate an Interactions suite. Native Interactions
evidence therefore remains authoritative for that surface. SDK request
rejections are `UNSUP` only when the active profile marks the capability
optional; transport, timeout, 429, and 5xx failures remain `ERROR`.

The built-in SDK check is selected as follows:

```bash
aiqa test=compat,sdk
```

The selector expands only to the SDK check for active targets. `compat` alone
never starts an extension. Each suite is bound once in the selected profile;
native and SDK observations remain separate evidence and a disagreement is an
evidence conflict, not two independent failure votes.

## Reports and guidance

Google appears as an independent target in `aiqa.report.v3`. The Markdown
header identifies the service origin, target profile, and external SDK. It
shows rate-limit and latency telemetry per target (and per model when there is
more than one), while normalized token usage remains at the end. Current
Interactions and legacy GenerateContent are visible through their profile and
module names; no cross-target percentage is emitted.

The Google advice pack is [`guidance/packs/generic-google-genai-gateway.json`](../guidance/packs/generic-google-genai-gateway.json).
The catalog entry for stateful cleanup is P0. Guidance is generated from typed
requirement evidence, cites stable requirement or suite IDs, and cannot change
status, verdict, score policy, or exit code. Keys, tool arguments, thought
content, media bytes, and raw stateful IDs are redacted or bounded before they
reach JSON, Markdown, stderr, or guidance artifacts.

## Maintenance and verification

The profile source and SDK pin are checked by
[`script/check-profile-upstream.go`](../script/check-profile-upstream.go), a
repository maintenance program rather than a shipped CLI feature. JSON Schema
checks cover both Google profiles. `make dependency-check` and `make sbom-check`
assert the exact SDK version; `make security-check` adds the pinned
vulnerability scan. The vulnerable transitive gRPC release is explicitly pinned
to the fixed `v1.82.1` line. CI keeps direct Google calls behind the
`AIQA_LIVE_GOOGLE_GENAI` secret/variable gate and does not make billable calls
by default.

The deterministic fixture in
[`internal/protocol/googlegenai/runner_test.go`](../internal/protocol/googlegenai/runner_test.go)
exercises discovery, Interactions, malformed JSON, streams, tools, structured
output, token counting, and routing without network access. The SDK
manifest test checks the exact public suite list, profile bindings, and module
version. Run the repository gates with a writable Go cache when the host `/tmp`
filesystem is constrained:

```bash
GOCACHE="$PWD/.tmp-go-cache" GOTMPDIR="$PWD/.tmp-go-tmp" go test ./...
GOCACHE="$PWD/.tmp-go-cache" GOTMPDIR="$PWD/.tmp-go-tmp" go test -race ./...
go mod verify
make schemas-check dependency-check
```

## Implementation audit

The following is the completed acceptance audit for the Google GenAI plan.

| Criteria | Evidence | Result |
|---|---|---|
| 1–12: target activation, aliases, URL/version rules, model flags, and multi-target ambiguity | `internal/config/config.go`, `internal/config/config_test.go` | Implemented and unit-tested |
| 13–18: two v3 profiles, core/optional policy, and denominator behavior | `profile/*.json`, `schema/profile-v3.schema.json`, `internal/profile` | Implemented and schema-checked |
| 19–23: method routing, embed-only isolation, unknown policy, and Interactions selection | `internal/capability/classifier.go`, `internal/protocol/googlegenai/runner.go` | Implemented; missing allowlisted model is inconclusive |
| 24–32: header-only auth, pagination, metadata preservation, stateless default, opt-in stateful cleanup | `internal/protocol/googlegenai/client.go`, `runner.go`, fixture tests | Implemented; stateful path is explicit opt-in |
| 33–48: schemas, streams, errors, usage, counting, embeddings, structured output, vision, and thinking privacy | `validate.go`, `stream.go`, `runner.go` | Implemented for the profiled MVP surfaces |
| 49–60: pinned SDK, target-owned transport, retries, public suite list, no fabricated Interactions, and bindings | `internal/extsuite/builtin/googlegenai`, profiles, `go.mod` | Implemented and manifest-tested |
| 61–74: report v3, independent targets, SDK limitation note, redaction, fingerprints, guidance, and exit policy | `internal/result`, `internal/report`, `internal/assessment`, `internal/guidance` | Implemented; no cross-target score |
| 75–79: format, race, vulnerability, module checksum, and SBOM gates | `Makefile`, CI, dependency pin checks | Passed on 2026-09-03 (`make check`, `make race`, `make security-check`, `go mod verify`) |
| 80–82: secret-gated live tests, billable-call safety, deterministic fixtures | `.github/workflows/ci.yml`, `runner_test.go` | Implemented; live evidence remains opt-in |

Vertex AI, generated media, server-side tools, agents, broad model-quality
benchmarks, and a full Google `test=model-param` suite remain explicitly out
of scope. They require separate profiles or opt-in plans and must not be
inferred from this compatibility report.
