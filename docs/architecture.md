# Architecture

## Design goals

`aiqa` separates protocol conformance, observable behavior, capability discovery, vendor extensions, and benchmarking. Accepting a request is not enough to prove that a feature works, and a provider or transport error is not evidence that a capability is incompatible.

The native binary uses the Go standard library only. Compatibility profiles, external checkers, and report schemas remain separate from probe implementation so each can evolve without hiding changes in test meaning.

## Runtime flow

```text
CLI and environment
        |
        v
configuration validation -----> embedded compatibility profile
        |                                  |
        v                                  v
OpenAI-compatible client -----> GET /models and retain metadata
                                           |
                                           v
                              capability classification
                                           |
                                           v
                                    module plan
                                /          |          \
                               v           v           v
                    provider probes   model probes   Official SDK suites
                                \          |          /
                                           v
                              normalized report model
                                           |
                                           v
                         aggregation, verdict, and score
                                           |
                                           v
                           atomic JSON/Markdown writers
```

`internal/app` owns this flow. It discovers models once, preserves their raw metadata in JSON, keeps user-selected model order, executes model work with bounded concurrency, restores deterministic report order, finalizes summaries, writes reports, and applies the exit policy.

## Packages and assets

```text
cmd/aiqa             CLI, usage text, build information, and signal handling
internal/config      environment and flag parsing and validation
internal/api         HTTP, JSON, SSE, artifact, pacing, and telemetry client
internal/profile     embedded profile loading and validation
internal/capability  capability evidence, classification, and module routing
internal/suites      native compatibility and model-parameter probes
internal/external    Official SDK checker process adapter
internal/result      stable in-memory and JSON report data model
internal/report      aggregation and atomic JSON/Markdown writers
internal/app         discovery, orchestration, concurrency, and exit policy
profile/             embedded compatibility-profile assets
schema/              machine-readable report contract
script/              repository maintenance programs, not shipped CLI features
```

`profile/embed.go` embeds every JSON profile in `profile/`. `internal/profile` validates profile provenance, unique probe IDs, module ownership, capability names, and required fields. The default profile is `openai-compat-2026-09.json`; the immutable 2026-08 profile remains embedded for reproducibility.

## Profiles, capabilities, and modules

A profile defines canonical probes and groups each probe into a provider-level or model-level module. A module also declares its required capabilities, whether it is enabled by default, and whether it can create billable artifacts.

Model routing has two distinct phases:

1. Classify the model from evidence.
2. Compare the resulting capabilities with the profile's module requirements.

Classification collects evidence from these sources in order. Only an explicit override short-circuits the remaining sources; otherwise, compatible evidence can accumulate and conflicting evidence remains visible.

1. `--model-capability` and `--capability-file` overrides. A CLI override for the same model wins because it is applied last.
2. Structured vendor metadata, including capability, task, type, and output-modality fields. Nested `architecture.output_modalities` is supported.
3. Provider-specific adapters, such as Vibecode's `is_image` and token-pricing metadata.
4. Conservative positive rules based on the model ID.
5. The unknown-model policy.

Conflicting positive evidence produces `unknown` and prevents capability probes. Positive non-text evidence routes the model only to matching modules; it never triggers the text fallback. `input_modalities: ["text"]` alone does not prove Chat Completions support because embedding models can accept the same input modality.

If no capability evidence exists and nothing identifies the model as non-text, the default `--unknown-model=chat` policy assigns `chat.completions.text` with low confidence. `--unknown-model=inventory` keeps such models in the inventory without running model capability probes.

Module assessment records applicability separately from execution:

- `applicable` means the model has a required capability;
- `not_applicable` means positive classification points elsewhere;
- `unknown` means the evidence conflicts or strict inventory mode has no classification;
- `executed` and `skipped` record what happened after applicability and cost policy were applied.

The native implementation currently executes Chat Completions and image-generation modules. Other module definitions remain visible to routing and can be added without changing the report model. Image generation requires explicit billable-artifact authorization and an artifact limit.

## Probe levels

1. **Transport** — HTTP status, content type, timeout behavior, and SSE framing.
2. **Shape** — request and response fields follow the selected OpenAI contract.
3. **SDK** — an official SDK can issue and parse a call through the external checker.
4. **Semantic** — requested behavior is observed; for example, `n=2` yields two choices.
5. **Validation canary** — deliberately invalid input is rejected instead of silently ignored.
6. **Statistical evidence** — repeated generations compare observed distributions without presenting a small sample as certainty.

Native probes distinguish `pass`, `warn`, `unsupported`, `fail`, `error`, and `skip`. Optional capabilities rejected with an appropriate 400, 404, or 422 response are `unsupported`. Timeouts, exhausted 429 responses, 5xx responses, and runner failures are `error` because the compatibility conclusion is incomplete.

## Native and Official SDK checks

Native probes test wire behavior, response shape, validation, and semantic effects. The Official SDK adapter complements them by checking whether the official OpenAI Go SDK can send requests and parse responses through [`beranekio/openai-compatibility-tester`](https://github.com/beranekio/openai-compatibility-tester).

The checker runs as an installed binary or a Docker container. On Linux, Docker uses host networking for loopback endpoints. Each checker suite is launched and parsed separately, so its result contributes independently to the report. AIQA normalizes known optional-capability rejections against the active profile and converts provider-side 408, 429, and 5xx failures to `error` rather than compatibility failures. Detailed checker output is retained only for failed or errored suites.

External execution remains a process boundary. Suite duration comes from the checker's reported request work rather than Docker startup and is never mixed into native request latency. A failure before any suite result is reported as a runner error rather than a failed compatibility check.

## Request pacing and telemetry

All native HTTP calls share an adaptive pacer. It begins with the configured request interval and one in-flight request. After quiet success windows, it reduces the interval and can raise concurrency. A 429 response resets concurrency to one, increases the interval, honors `Retry-After` when present, and retries the request up to three times.

The report keeps rate-limit observations separate from compatibility results:

- successful requests;
- rate-limited responses;
- recovered retries;
- total backoff time;
- observed safe request rate and concurrency;
- mean, P50, and P95 latency for successful native compatibility requests.

Latency measures the HTTP operation after pacing, not queueing or backoff time. Global statistics include discovery and native compatibility requests. Per-model rate-limit and latency sections are emitted when more than one model is tested. Official SDK process time, model-parameter probes, and context probes are not mixed into these latency percentiles.

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

## Reports, scoring, and exit policy

`internal/result` is the shared contract between native probes, external suites, aggregation, and both writers. JSON preserves complete model metadata and evidence. Markdown is a compact human-readable view and intentionally omits large metadata lists when a count is sufficient. Writers create a temporary file, sync it, and atomically replace the destination.

Each Official SDK suite and each native probe contributes one status to the current summary. The current numeric score is:

```text
100 * pass / (pass + fail + warn + unsupported)
```

`skip` and `error` do not enter that denominator. The score is therefore a descriptive pass ratio, not a severity ordering and not a substitute for the verdict. In particular, reports with different error, required-probe, or applicability patterns must not be ranked by score alone. Verdict semantics and planned requirement-level improvements are documented in [`docs/scoring.md`](scoring.md).

Exit codes are applied after the report is written:

- `0` — the selected `--fail-on` policy passed;
- `1` — probe results violated that policy;
- `2` — configuration, orchestration, or report writing failed.

This separation lets CI choose a strict policy without removing diagnostic evidence from the report.

## Safety boundaries

The API key is used for requests but is not written to the public configuration in the report. External diagnostics redact the configured key and are size-limited. Native responses are bounded before decoding. Artifact reports store format, media type, size, dimensions, and SHA-256, not image payloads or signed URL query strings. Remote artifact fetching rejects unsafe URL schemes and network targets.

Suites that execute code, create persistent objects, browse external resources, or generate billable media must remain explicit opt-ins. See [`docs/adding-tests.md`](adding-tests.md) for the extension checklist.
