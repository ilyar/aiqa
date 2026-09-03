# Adding tests

## Add a compatibility probe

1. Add a unique entry to `profile/openai-compat-2026-09.json` and reference it from the applicable module's `probe_ids`.
2. Implement the probe in `internal/suites/compat.go`.
3. Set its stable `requirement_id` and `probe_id`; `testBase` handles the one-probe-per-requirement case.
4. Return `pass`, `warn`, `unsupported`, `fail`, `error`, or `skip` deliberately.
5. Emit a typed `EvidenceSignal` for outcomes that need curated guidance. Do not make guidance rules parse `summary`, `observation`, or `error` prose.
6. Add deterministic behavior to the integration mock in `internal/app/app_test.go`.
7. Add assessment and guidance tests, then run `make check` and `make race`.

A probe should record:

- expected behavior from the profile;
- observed HTTP metadata;
- token usage where available;
- a short sanitized observation;
- metrics needed for later machine analysis.

When several probes test the same normative promise, map them to one canonical requirement. When they test distinct promises, such as valid SSE framing and Official SDK parser interoperability, give them separate requirement IDs and link the extension suite through `related_requirement_ids`.

For Anthropic, add the probe to `profile/anthropic-compat-2026-09.json` and use an `anthropic.*` requirement ID. Implement request construction and validation under `internal/protocol/anthropic`; do not translate the probe into an OpenAI Chat Completions request. Stable probes must send `x-api-key` and `anthropic-version: 2023-06-01`, must not inherit beta headers, and must preserve Anthropic content blocks, error envelopes, named SSE events, and usage fields.

Keep new behavior consistent with the target contract in [`anthropic-compatibility.md`](anthropic-compatibility.md).

For Google GenAI, add protocol-qualified native probes to the selected current
or legacy profile and implement them under
[`internal/protocol/googlegenai`](../internal/protocol/googlegenai). Keep
Interactions and legacy `generateContent` request, response, usage, and stream
contracts separate. Route generation, token counting, and embeddings from
`supportedGenerationMethods`; never infer Interactions support from
`generateContent`, and never send generation calls to an embedding-only model.
Use `x-goog-api-key`, preserve the configured path prefix before the selected
API version, and add a deterministic fixture test. Stateless Interactions
must send `store:false`; stateful probes require an explicit persistent-resource
gate and must verify cleanup. The complete contract, including the pinned
Official Google Gen AI Go SDK extension, is in
[`google-genai-compatibility.md`](google-genai-compatibility.md).

## Add actionable guidance

Add a typed signal and a generic catalog entry under `guidance/catalog/` when AIQA can provide reviewed, cross-provider advice. Every P0/P1 entry needs ordered steps and verification targets. Guidance level 3 always needs verification targets.

A verification target is a stable requirement ID or Official SDK suite ID. Do not embed shell commands in catalogs or reports. AIQA will render commands only after the targeted verification runner in [`tasks/backlog.md`](../tasks/backlog.md) exists and only when the user explicitly enables them.

Catalog entries may define normative priority and action policy. Advice packs under `guidance/packs/` are deliberately narrower: they may add steps, workarounds, caveats, and references, but strict decoding rejects status, requirement-level, priority, verdict, or impact overrides.

Use a deterministic heuristic diagnostic when a safe fix is not known. Execution failures must recommend rerun or investigation; they must never become unsupported-capability findings. Conflicting evidence must preserve both evidence records and keep attribution confidence low until the mismatch is localized.

## Add a model parameter

Parameter tests should, where possible, contain:

- one or more valid variants;
- one invalid canary;
- an observable-effect test;
- a confidence level;
- aggregated token usage.

Use `unsupported` when a valid optional parameter is explicitly rejected with a client error. Use `warn` when it is accepted but its effect cannot be demonstrated. Use `fail` when it is accepted and returns structurally incompatible behavior.

## Add a native suite

1. Define the suite name in `internal/config` validation.
2. Add a runner under `internal/suites`.
3. Extend orchestration in `internal/app`.
4. Update the current report schema when the public contract changes.
5. Document expected cost and side effects.

## Add an extension suite

Built-in and executable extensions use the same manifest, run request, event sink, validator, profile bindings, and report model. Read [`extension-suites.md`](extension-suites.md) before changing the contract.

For a built-in suite:

1. Add its manifest entry under `internal/extsuite/builtin/openai` or `internal/extsuite/builtin/anthropic`.
2. Add an `executor: extension` requirement and an `extension_bindings` entry to the matching profile.
3. Keep SDK retries disabled and issue requests through `RunRequest.Runtime.HTTPClient` and `RunRequest.Runtime.Executor`.
4. Emit a raw outcome through `EventSink`; do not assign conformance status in the suite.
5. Add deterministic mock-provider coverage and a parity note for any removed or changed legacy case.

For an executable extension, implement `aiqa.ext.v1` and validate records against [`schema/ext-v1.schema.json`](../schema/ext-v1.schema.json). Start from [`test/fixtures/ext-suite`](../test/fixtures/ext-suite). Tests must cover clean manifest negotiation, stdin-only credentials, partial output, malformed and duplicate records, cancellation, bounded output, and redaction.

When adding a protocol or report target:

1. Add a protocol-qualified profile and validate it through the embedded profile registry.
2. Keep authentication, base-URL normalization, request types, streaming state, and usage parsing in a protocol package.
3. Add a target-local runner and preserve deterministic target order.
4. Extend report v3 without adding a cross-target percentage.
5. Test duplicate model IDs across targets, credential isolation, redaction, and the exact CLI environment contract.

Suites that execute code, browse the web, persist objects, or create billable assets must be opt-in and clearly marked.

`make schemas-check` validates report v3, profile v3, all embedded current profiles, and the extension protocol with JSON Schema Draft 2020-12. The development environment needs Python 3 and a Draft 2020-12-capable `jsonschema` package in addition to Go. The main module intentionally pins the OpenAI, Anthropic, and Google Gen AI official SDK dependencies.
