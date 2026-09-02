# Architecture

## Design goals

`aiqa` separates protocol conformance, observable behavior, capability discovery, vendor extensions, and benchmarking. Accepting a request is not enough to prove that a feature works, and a provider or transport error is not evidence that a capability is incompatible.

The main module pins the official OpenAI and Anthropic Go SDKs for built-in extension suites. Compatibility profiles, extension manifests, guidance catalogs, and report schemas remain separate from probe implementation so each can evolve without hiding changes in test meaning. Guidance is downstream of conformance assessment: it can explain a result, but it cannot change the result or the process exit code.

## Runtime flow

```text
CLI and environment
        |
        v
configuration validation
        |
        v
ordered target configs: OpenAI, Anthropic
        |
        +----------------------+----------------------+
        |                                             |
        v                                             v
OpenAI profile and client                 Anthropic profile and client
Bearer + base includes /v1                x-api-key + version + /v1 paths
        |                                             |
        v                                             v
discovery, routing, native probes, and explicitly selected extension suites
        |                                             |
        v                                             v
target-local requirements, verdict, guidance, usage, and telemetry
        |                                             |
        +----------------------+----------------------+
                               |
                               v
                 hardest target determines run verdict
                               |
                               v
                     atomic v3 JSON/Markdown writers
```

`internal/app` owns this flow. It activates a target only from a base URL and its matching key, loads a protocol-matched profile, and keeps OpenAI and Anthropic credentials and telemetry separate. Each target discovers its own model inventory and derives its own authoritative verdict. The v3 root records no cross-protocol score; it selects the hardest target verdict for exit policy. Reports are written before that policy is applied.

## Packages and assets

```text
cmd/aiqa             CLI, usage text, build information, and signal handling
internal/config      environment and flag parsing and validation
internal/api         HTTP, JSON, SSE, artifact, pacing, and telemetry client
internal/protocol/anthropic
                     Anthropic Models, Messages, tools, token counting, and named-event validation
internal/profile     embedded profile loading and validation
internal/capability  capability evidence, classification, and module routing
internal/suites      native compatibility and model-parameter probes
internal/extsuite    extension registry, planning, validation, process protocol, and built-ins
internal/result      stable in-memory and JSON report data model
internal/assessment  canonical requirements, verdict, and comparison policy
internal/guidance    findings, recommendations, fingerprints, and advice packs
internal/report      raw-result counters and atomic JSON/Markdown writers
internal/app         discovery, orchestration, concurrency, and exit policy
profile/             embedded compatibility-profile assets
guidance/            embedded generic catalog and non-normative advice packs
schema/              machine-readable report, profile, and extension protocol contracts
script/              repository maintenance programs, not shipped CLI features
```

`profile/embed.go` embeds every JSON profile in `profile/`. The current profiles use `aiqa.profile.v3`. `internal/profile` validates provenance, protocol, API version, qualified Anthropic IDs, unique probe IDs, native module ownership, extension requirements, and explicit suite bindings. OpenAI defaults to `openai-compat-2026-09.json`. Anthropic uses `anthropic-compat-2026-09.json` and pins the stable API version `2023-06-01`.

## Protocol isolation

OpenAI and Anthropic are separate protocols, even when one gateway exposes both. The OpenAI target uses Bearer authentication, endpoints below its configured `/v1`, OpenAI response envelopes, `[DONE]` streaming termination, and prompt/completion usage names. The Anthropic target uses `x-api-key`, appends `/v1` to a configured prefix, sends `anthropic-version: 2023-06-01`, parses content blocks and named SSE events, and preserves input/output/cache usage names.

Ambient Anthropic auth-token, beta, and custom-header variables do not affect native probes or the built-in Official SDK extension. The SDK receives explicit target configuration and disables automatic retries. Third-party executables receive credentials only through an `aiqa.ext.v1` stdin request. A malformed JSONL line becomes runner evidence without discarding valid suite entries parsed before or after it.

Target IDs qualify requirement subjects and evidence IDs in report v3. The same model ID may therefore appear under both facades without merging findings or SDK evidence.

The complete Anthropic target contract, including module gates and failure semantics, is documented in [`anthropic-compatibility.md`](anthropic-compatibility.md).

## Profiles, capabilities, and modules

A profile defines canonical probes and groups each probe into a provider-level or model-level module. A module also declares its required capabilities, whether it is enabled by default, and whether it can create billable artifacts.

Model routing has two distinct phases:

1. Classify the model from evidence.
2. Compare the resulting capabilities with the profile's module requirements.

OpenAI classification collects evidence from these sources in order. Only an explicit override short-circuits the remaining sources; otherwise, compatible evidence can accumulate and conflicting evidence remains visible.

1. `--model-capability` and `--capability-file` overrides. A CLI override for the same model wins because it is applied last.
2. Structured vendor metadata, including capability, task, type, and output-modality fields. Nested `architecture.output_modalities` is supported.
3. Provider-specific adapters, such as Vibecode's `is_image` and token-pricing metadata.
4. Conservative positive rules based on the model ID.
5. The unknown-model policy.

Conflicting positive evidence produces `unknown` and prevents capability probes. Positive non-text evidence routes the model only to matching modules; it never triggers the text fallback. `input_modalities: ["text"]` alone does not prove Chat Completions support because embedding models can accept the same input modality.

If no OpenAI capability evidence exists and nothing identifies the model as non-text, the default `--unknown-model=chat` policy assigns `chat.completions.text` with low confidence. `--unknown-model=inventory` keeps such models in the inventory without running model capability probes. Anthropic has a separate classifier and capability namespace. Its default `--anthropic-unknown-model=messages` policy permits Messages text plus safe tools and structured-output probes; `inventory` keeps unclassified Anthropic models inventory-only.

Module assessment records applicability separately from execution:

- `applicable` means the model has a required capability;
- `not_applicable` means positive classification points elsewhere;
- `unknown` means the evidence conflicts or strict inventory mode has no classification;
- `executed` and `skipped` record what happened after applicability and cost policy were applied.

The native implementation executes OpenAI Chat Completions and image generation plus Anthropic Messages, tools, Count Tokens, structured output, vision, and opt-in thinking. Other module definitions remain visible to routing and can be added without changing the report model. OpenAI image generation requires explicit billable-artifact authorization and an artifact limit; Anthropic thinking requires explicit module selection and high-token authorization.

## Probe levels

1. **Transport** – HTTP status, content type, timeout behavior, and SSE framing.
2. **Shape** – request and response fields follow the selected protocol contract.
3. **SDK** – an official SDK can issue and parse a call through a built-in extension suite.
4. **Semantic** – requested behavior is observed; for example, `n=2` yields two choices.
5. **Validation canary** – deliberately invalid input is rejected instead of silently ignored.
6. **Statistical evidence** – repeated generations compare observed distributions without presenting a small sample as certainty.

Raw native and Official SDK evidence distinguishes `pass`, `warn`, `unsupported`, `fail`, `error`, and `skip`. Optional capabilities rejected with an appropriate 400, 404, or 422 response are `unsupported`. Timeouts, exhausted 429 responses, 5xx responses, and runner failures are `error` because the compatibility conclusion is incomplete. The requirement layer additionally uses `not_applicable`; this is distinct from a skipped applicable check.

## Native probes and extension suites

Native probes test wire behavior, response shape, validation, and semantic effects. Built-in Official SDK extensions check whether the official OpenAI and Anthropic Go clients can send requests and parse responses through the target.

Extension execution requires `test=ext-suite`. The application registers built-ins explicitly and may discover trusted `aiqa-*-ext-suite` executables. Built-ins and executables share manifest, planning, event validation, profile binding, status derivation, and report types. Each suite contributes independently. AIQA normalizes known optional-capability rejections against the active profile and converts provider-side 408, 429, and 5xx failures to `error` rather than compatibility failures.

Executable extensions remain a trusted process boundary, not a sandbox. Suite duration comes from the extension's reported request and parse work rather than process startup and is never mixed into native request latency. A startup, protocol, timeout, or termination failure is a runner error rather than a failed compatibility check. [`extension-suites.md`](extension-suites.md) defines this contract.

## Canonical requirements and evidence

Raw probes are evidence sources, not independently weighted result units. Each native result carries a stable requirement ID and probe ID. An `aiqa.profile.v3` binding maps an extension ID and suite ID to its canonical requirement and related native requirements. `internal/assessment` joins observations that test the same contract, so adding another probe does not increase that requirement's weight. Unbound extension results remain supplementary. A native/SDK disagreement becomes a linked evidence diagnostic without rewriting the native requirement.

The assessment preserves every evidence reference and resolves the canonical requirement status deterministically. Conflicting positive and negative observations become `warn` with `evidence_conflict`; a required capability reported as unsupported becomes a core failure; provider or runner errors remain inconclusive evidence. Model parameters and context-boundary observations are optional requirements unless a future profile declares otherwise.

The conformance verdict is derived only from applicable canonical requirements:

- `fail` means at least one core requirement failed;
- `inconclusive` means core evidence is missing, uncertain, skipped, or errored;
- `partial` means core requirements passed but an optional requirement failed;
- `pass-with-gaps` means core requirements passed and optional evidence contains only unsupported, warning, skipped, or errored outcomes;
- `pass` means every applicable requirement passed.

`not_applicable` requirements do not enter the core, optional, or evidence denominators. An inconclusive report is deliberately not rankable. Other reports expose an ordinal verdict rank, followed by core coverage, optional coverage, and evidence completeness; no single percentage can override that ordering.

## Findings and actionable guidance

`internal/guidance` consumes canonical requirements and typed evidence signals. It does not parse prose summaries to decide what happened. The engine groups repeated observations by finding code and subject, assigns stable fingerprints, records evidence and artifact references, and emits deterministic recommendations with priority, target component, confidence, verification targets, references, and conditional impact. Verification targets are requirement or Official SDK suite IDs; the current engine does not emit broad shell commands.

Normative facts remain in the compatibility profile and requirement assessment. The generic catalog provides reviewed cross-provider guidance for known signal codes. Advice packs may add operational steps, workarounds, caveats, and references, but they cannot redefine requirement level, status, verdict, or priority policy. Unknown signals receive conservative generic guidance instead of a fabricated provider-specific fix.

Recommendations are ordered by priority and action type. A projected verdict is shown only for fix or configuration actions and is explicitly conditional on the affected requirements passing without regressions elsewhere. Execution failures and conflicting evidence produce investigate or rerun guidance rather than unsupported-capability claims.

Failed extension-runner diagnostics are sanitized, truncated, hashed, and stored once in `guidance.artifacts`. Evidence records refer to them by `artifact_ref`; Markdown renders each artifact once instead of copying the same log into every suite section.

## Request pacing and telemetry

Independent native probes and built-in extension requests are submitted to one bounded executor shared by all model workers. `--concurrency` is therefore a run-wide ceiling, not merely the number of models tested at once. Executable invocations reserve their declared capacity from the same executor. Results are collected in canonical order, so scheduling does not make reports nondeterministic. Probes with a real data dependency remain sequential, including tool continuation after a forced tool call and parameter checks that require a baseline.

All native HTTP calls also share an adaptive pacer. It starts with up to `--concurrency` request lanes; `--request-interval` is the initial interval within each lane. After quiet success windows, it reduces the interval. A 429 response resets active concurrency to one, increases the interval, honors `Retry-After` when present, and retries the request at most twice (three attempts total). After the service remains stable, concurrency can recover up to the configured ceiling.

The report keeps rate-limit observations separate from compatibility results:

- successful requests;
- rate-limited responses;
- recovered retries;
- total backoff time;
- observed safe request rate and concurrency;
- mean, P50, and P95 latency for successful native compatibility requests.

Latency measures the HTTP operation after pacing, not queueing or backoff time. Global statistics include discovery and native compatibility requests. Per-model rate-limit and latency sections are emitted when more than one model is tested. Extension suite duration, process time, model-parameter probes, and context probes are not mixed into native latency percentiles.

## Context probing

Context capacity is an observed boundary, not a single giant request and not a reasoning-quality benchmark.

The probe:

1. Reads declared limits from model metadata when available.
2. Sends deterministic, incompressible payloads and grows them exponentially until it finds an accepted/rejected bracket.
3. Narrows the bracket with binary search.
4. Prefers provider-reported `usage.prompt_tokens` and otherwise calibrates a byte-to-token estimate.
5. Parses an explicit server limit from recognized overflow errors when the provider supplies one.
6. Stops at configured token, request-byte, iteration, and timeout limits.

The report separates declared metadata, a provider-reported server limit, the largest accepted observation, and the first estimated rejected boundary. A 2xx response proves only an accepted lower bound. It does not show that the model can use information effectively across the entire context; that requires a separate needle-in-a-haystack or multi-hop benchmark.

## Reports, assessment, and exit policy

`internal/result` is the shared contract between native probes, extension suites, assessment, guidance, and both writers. Report v3 contains an ordered `targets[]` array, and every target owns its profile, discovery, models, requirements, summary, guidance, telemetry, and grouped extension runs. Markdown leads with the target table and per-target rate-limit and latency data. Secondary token usage remains at the end. Writers create a temporary file, sync it, and atomically replace the destination. [`docs/reports.md`](reports.md) defines the user-facing status, verdict, and guidance semantics.

The raw summary keeps status counts and a raw-result status for diagnostics. The authoritative result is `summary.assessment.verdict`. AIQA emits no overall numeric compatibility score because one percentage cannot preserve core gates, optional coverage, and inconclusive evidence at the same time. Comparisons use the canonical verdict first, followed by the separate core, optional, and evidence measures. The design rationale is retained in [`research/scoring.md`](../research/scoring.md).

Exit codes are applied after the report is written:

- `0` – the selected `--fail-on` policy accepted the canonical verdict;
- `1` – the canonical verdict violated that policy;
- `2` – configuration, orchestration, or report writing failed.

With `--fail-on=core`, `fail` and `inconclusive` return `1`; optional-only gaps do not. With `--fail-on=any`, every verdict except `pass` returns `1`. `--fail-on=never` always returns `0` after a report is written. Guidance status never changes these decisions.

## Safety boundaries

The API key is used for requests but is not written to the public configuration in the report. Extension credentials are absent from argv and the child environment; captured diagnostics redact secrets and are size-limited. Native responses are bounded before decoding. Artifact reports store format, media type, size, dimensions, and SHA-256, not image payloads or signed URL query strings. Remote artifact fetching rejects unsafe URL schemes and network targets.

Suites that execute code, create persistent objects, browse external resources, or generate billable media must remain explicit opt-ins. See [`docs/adding-tests.md`](adding-tests.md) for the extension checklist.
