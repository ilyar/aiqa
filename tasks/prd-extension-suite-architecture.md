# PRD: Extension Suite Architecture

## Introduction

Replace AIQA's Docker- and checker-specific Official SDK execution paths with one extension-suite architecture. Official OpenAI and Anthropic SDK checks will be compiled into the main `aiqa` binary. Independently developed suites may be installed as trusted executables such as `aiqa-openai-ext-suite` and run through the same planning, validation, assessment, and reporting contracts.

The feature introduces `test=ext-suite`. It does not turn extensions into a second compatibility system: an extension produces evidence, while the selected compatibility profile remains the only authority that decides whether that evidence affects conformance.

### Decision

Proceed with the architecture, subject to these constraints:

- Built-in OpenAI and Anthropic Official SDK suites are registered explicitly at compile time.
- External executables use a versioned JSONL protocol over standard input and standard output and communicate completion through their exit code.
- `test=ext-suite` runs every applicable enabled extension; `--ext-suite=<id>` restricts the run.
- Executables are discovered from explicit paths and then from `PATH`.
- Docker is not an AIQA extension transport. The current Docker modes, image settings, and checker-specific Docker assets are removed.
- Built-in and executable extensions use the same manifest, request, result, validation, and profile-binding types.
- An extension cannot assign its own conformance weight or requirement level.

This is an intentional trade-off. The main binary will no longer use only the Go standard library: it will contain pinned official OpenAI and Anthropic SDK dependencies. In return, Official SDK checks work against loopback endpoints without container networking, have no container startup dependency, and share one stable AIQA-owned test contract.

## Goals

- Run Official OpenAI and Anthropic SDK compatibility checks without Docker or separately installed checker binaries.
- Provide a stable executable protocol for third-party extension suites without using Go's runtime plugin ABI.
- Keep extension discovery, selection, applicability, pacing, errors, duration, evidence, and reports deterministic.
- Preserve suite-level evidence: every extension suite case contributes independently rather than as one checker result.
- Ensure only profile-declared extension evidence can affect the conformance verdict.
- Keep credentials out of process arguments, environment variables, reports, stdout diagnostics, and error messages.
- Preserve valid suite results when another suite result is malformed or the executable terminates after producing partial output.
- Make an extension run reproducible by recording extension identity, version, source, protocol version, and executable digest where applicable.

## User Stories

### US-001: Register built-in Official SDK extensions

**Description:** As an AIQA user, I want Official OpenAI and Anthropic SDK checks included in the `aiqa` binary so that I can run them without Docker or installing helper programs.

**Acceptance Criteria:**

- [ ] The main module directly pins the official OpenAI and Anthropic Go SDK dependencies.
- [ ] The application composition root explicitly registers `official-sdk.openai` and `official-sdk.anthropic`; package `init` side effects are not used for registration.
- [ ] Both built-ins implement the shared extension manifest and runner interfaces.
- [ ] Built-ins receive only the selected target, subject, profile bindings, limits, and credentials needed for the invocation.
- [ ] SDK automatic retries are disabled so AIQA owns retry and rate-limit policy.
- [ ] Reports contain the exact embedded SDK module version for every Official SDK extension.
- [ ] The built-ins cover every non-obsolete suite currently provided by `internal/external` and `tools/anthropic-sdk-checker`, with parity tests documenting any deliberate removal or semantic change.
- [ ] `go test ./...` and repository validation pass.

### US-002: Discover extension executables

**Description:** As an extension author, I want AIQA to discover my installed executable so that I can add suites without modifying the AIQA repository.

**Acceptance Criteria:**

- [ ] Repeatable `--ext-suite-path=<absolute-path>` values and the OS path-list environment setting `AIQA_EXT_SUITE_PATHS` provide explicit candidates.
- [ ] AIQA also discovers executable files named `aiqa-*-ext-suite` using normal `PATH` precedence.
- [ ] Discovery occurs only when `ext-suite` is selected; a `compat`-only run does not scan or execute `PATH` candidates.
- [ ] Every candidate receives a manifest request before it can be planned.
- [ ] A duplicate extension ID, invalid manifest, unsupported protocol version, non-executable path, or ambiguous explicit configuration is a configuration error.
- [ ] Built-in IDs cannot be replaced or shadowed by executable extensions.
- [ ] The resolved absolute path and SHA-256 digest of each executable are recorded in JSON provenance.
- [ ] Discovery order does not affect result or report order.
- [ ] Unit tests cover explicit paths, `PATH` precedence, duplicate IDs, invalid executables, and no-discovery runs.

### US-003: Negotiate an extension manifest

**Description:** As AIQA, I want every extension to describe its identity and suites through a versioned manifest so that applicability and output can be validated before credentials are supplied.

**Acceptance Criteria:**

- [ ] AIQA starts the executable with a clean environment and writes one `manifest_request` JSON line to stdin without credentials.
- [ ] The executable emits exactly one `manifest` JSON line on stdout and exits with code zero.
- [ ] The manifest includes protocol version, extension ID, display name, extension version, supported AIQA report/profile contract versions, supported target protocols, and suite definitions.
- [ ] Each suite definition includes a stable suite ID, subject scope (`provider` or `model`), required model capabilities, billable-artifact class, and declared internal concurrency.
- [ ] Extension and suite IDs match a documented lowercase stable-ID grammar.
- [ ] Human diagnostics are allowed only on stderr and are bounded and sanitized.
- [ ] The manifest has no authority to define requirement priority, requiredness, score, or verdict policy.
- [ ] Malformed, oversized, duplicated, or trailing stdout records reject the candidate.

### US-004: Plan applicable extension suites

**Description:** As an AIQA user, I want extension suites to run only for compatible targets and models so that unrelated or billable probes are not triggered.

**Acceptance Criteria:**

- [ ] `test=ext-suite` runs all applicable built-in and discovered extensions.
- [ ] Repeatable or comma-separated `--ext-suite=<extension-id>` filters execution to exact extension IDs.
- [ ] An explicitly selected but unavailable extension is a configuration error rather than a compatibility failure.
- [ ] `test=compat` does not run extension suites implicitly.
- [ ] `test=compat,ext-suite` produces one combined report from native and extension evidence.
- [ ] Provider-scoped suites run once per target; model-scoped suites run only for models whose module and capability assessments make them applicable.
- [ ] Discovery and capability classification may run as prerequisites for `test=ext-suite`, but unrelated native compatibility probes do not run.
- [ ] Billable suites retain the existing explicit artifact authorization and per-model budget gates.
- [ ] The plan is sorted by target ID, extension ID, subject scope, subject ID, and suite ID before execution.
- [ ] Tests prove that unknown, non-text, image, and multi-protocol models are routed without invoking unrelated suites.

### US-005: Run an external executable through JSONL

**Description:** As an extension author, I want a small language-neutral process protocol so that my suite can be implemented outside the AIQA codebase.

**Acceptance Criteria:**

- [ ] AIQA starts an executable directly without a shell and writes one `run_request` JSON line to stdin.
- [ ] A run request contains a unique run ID, selected target protocol and base URL, subject, exact suite IDs, profile identity, timeouts, concurrency/pacing limits, and target credentials.
- [ ] Target credentials are sent only in the stdin request, are never placed in argv or the child environment, and are never persisted by AIQA.
- [ ] AIQA closes stdin after the request and reads versioned JSONL result events from stdout.
- [ ] One process invocation handles one extension, target, and provider/model subject group, allowing AIQA to schedule and isolate groups.
- [ ] Each suite emits exactly one terminal observation with `pass`, `assertion_failed`, `request_rejected`, `execution_error`, or `skip`, a non-empty summary, suite work duration, and optional structured HTTP, signal, and usage fields.
- [ ] AIQA derives the report status from the observation, HTTP evidence, and active profile; the executable does not assign `unsupported` or other conformance semantics.
- [ ] A zero exit code means the protocol run completed; suite assertion failures remain result observations and do not require a non-zero exit code.
- [ ] A non-zero exit, timeout, signal termination, missing planned result, duplicate result, or malformed line creates runner `error` evidence while preserving all valid results already received.
- [ ] stdout is reserved for protocol records; bounded stderr is retained only as a sanitized runner artifact when the invocation errors.
- [ ] The host enforces line, total-output, stderr, deadline, and process-tree limits.
- [ ] Contract tests are published as reusable fixtures so non-Go implementations can prove conformance.

### US-006: Use one result validator for built-in and executable suites

**Description:** As an AIQA maintainer, I want all extension results validated identically so that built-ins cannot bypass rules imposed on third-party extensions.

**Acceptance Criteria:**

- [ ] Built-ins emit results through an `EventSink` backed by the same validator used for executable JSONL records.
- [ ] The validator checks planned extension, target, subject, suite identity, terminal-result uniqueness, outcome, duration, HTTP observations, signals, usage, and secret redaction.
- [ ] Unknown suites, subjects, outcomes, requirement IDs, or protocol fields cannot enter the report as valid evidence.
- [ ] Suite duration measures only suite request/parse work; built-in setup and executable startup durations are recorded separately as runner telemetry.
- [ ] Results are emitted in deterministic plan order even when execution is concurrent.
- [ ] Fuzz tests cover JSONL decoding, oversized records, duplicate records, invalid UTF-8, partial output, and secret-bearing messages.

### US-007: Bind extension evidence through the compatibility profile

**Description:** As a report reader, I want extension findings assessed by the same declared compatibility policy as native probes so that an extension cannot inflate or distort conformance.

**Acceptance Criteria:**

- [ ] A new profile contract explicitly binds an extension ID and suite ID to canonical requirement IDs and related native requirement IDs.
- [ ] Requirement level and requiredness come only from the active profile.
- [ ] Every current Official SDK suite that is intended to affect conformance has an explicit profile binding; synthesized `official-sdk.<suite>` requirements are removed.
- [ ] A valid but unbound third-party suite is retained as supplementary evidence and is excluded from conformance measures and verdicts.
- [ ] Multiple suite cases mapped to the same requirement remain multiple evidence records but count as one canonical requirement.
- [ ] Each separately bound suite can affect its own requirement; no checker-level aggregate result contributes to conformance.
- [ ] Runner failures and 408, exhausted 429, and 5xx responses are `error` or inconclusive evidence, never compatibility `fail`.
- [ ] Optional 400, 404, and 422 capability rejections may become `unsupported` only when the selected profile marks that capability optional and the error is recognized as a capability rejection.
- [ ] Profile schema validation rejects incomplete or duplicate bindings, unknown requirements, incompatible subject scopes, and bindings to billable suites without cost metadata; the profile may still describe an optional third-party extension that is not installed.
- [ ] The extension binding contract is introduced as `aiqa.profile.v3`; updated OpenAI and Anthropic profiles are delivered with the feature.
- [ ] Updating the semantic meaning or coverage of an Official SDK suite requires a profile revision and a changelog entry.

### US-008: Report extension execution clearly

**Description:** As a technical or non-technical report reader, I want Official SDK and third-party extension results separated from runner failures so that I can distinguish incompatibility from an execution problem.

**Acceptance Criteria:**

- [ ] The JSON report groups extension provenance, runner telemetry, and suite results instead of flattening all data into legacy `external` items.
- [ ] Grouped extension data is introduced as `aiqa.report.v3`; no dual writing of legacy external-checker fields is retained.
- [ ] Each group records extension ID, display name, version, source kind (`builtin` or `executable`), protocol version, and executable provenance when applicable.
- [ ] Markdown lists configured external checks by display name and version and renders suite-level findings under the extension display name.
- [ ] A runner failure appears only when startup, protocol, timeout, or process termination actually failed.
- [ ] Suite `duration_ms` excludes manifest lookup, process startup, and AIQA scheduling overhead.
- [ ] Valid suite results remain visible beside a runner error from the same invocation.
- [ ] Extension results contribute to canonical requirement summaries only through profile bindings.
- [ ] The report contains no Docker image, Docker mode, or container-networking concepts.
- [ ] Report schema, Markdown golden tests, and deterministic mock-provider fixtures pass.

### US-009: Integrate extension load with global pacing and concurrency

**Description:** As a provider operator, I want extension checks to respect the same run budget as native probes so that enabling suites does not create an uncontrolled request burst.

**Acceptance Criteria:**

- [ ] Every built-in suite request acquires a slot from AIQA's global executor and uses the target's adaptive pacer.
- [ ] An external process invocation reserves the number of global slots declared in its manifest, defaulting to one and never exceeding the CLI concurrency ceiling.
- [ ] The external run request contains the allowed internal concurrency, initial request interval, retry ceiling, and current target pacing constraints.
- [ ] External extensions are required by protocol to disable vendor SDK automatic retries and honor the supplied limits; this compliance is reported but cannot be technically guaranteed for trusted native executables.
- [ ] AIQA schedules only one invocation at a time for an extension that declares internal concurrency unknown or unsafe.
- [ ] 429 retry behavior honors `Retry-After`; an exhausted 429 remains an error and is included in rate-limit telemetry.
- [ ] Extension latency is reported separately from native API latency unless a suite supplies validated per-request latency samples.
- [ ] Load tests prove the configured global concurrency ceiling is not exceeded by built-in suites or declared executable reservations.

### US-010: Remove legacy checker transports

**Description:** As an AIQA maintainer, I want one extension path so that Docker, legacy text parsing, and provider-specific runner branches do not continue to diverge.

**Acceptance Criteria:**

- [ ] Remove `--external`, `--external-image`, protocol-specific external mode/image flags, and their environment aliases.
- [ ] Remove Docker discovery, image inspection, host-network logic, and checker Dockerfiles from the Official SDK feature.
- [ ] Remove the legacy parser for `beranekio/openai-compatibility-tester` human-readable output.
- [ ] Move Anthropic suite implementation from the separate checker module into the built-in Anthropic extension package.
- [ ] Remove `internal/external` after all callers, mappings, tests, reports, and documentation use the new extension packages.
- [ ] No backward-compatibility adapter or dual report writing is retained for legacy checker modes.
- [ ] README, architecture, adding-tests guidance, Anthropic documentation, schemas, examples, and changelog describe only the extension-suite architecture.
- [ ] Repository-wide search confirms there are no stale user-facing Docker checker instructions or old mode flags.

## Functional Requirements

- **FR-1:** AIQA must expose `ext-suite` as a valid test selector.
- **FR-2:** `test=compat` must run native compatibility probes only; extension probes require explicit `ext-suite` selection.
- **FR-3:** `test=ext-suite` must run every applicable enabled built-in and discovered extension.
- **FR-4:** `--ext-suite=<id>` must restrict execution to exact extension IDs and must reject unknown IDs.
- **FR-5:** The built-in registry must explicitly contain `official-sdk.openai` and `official-sdk.anthropic`.
- **FR-6:** Both built-in Official SDK extensions must use pinned official Go SDK modules and report their exact versions.
- **FR-7:** AIQA must discover explicit executable paths before scanning `PATH` for `aiqa-*-ext-suite`.
- **FR-8:** Extension IDs must be globally unique within a run, and executable extensions must not shadow built-ins.
- **FR-9:** External executables must implement `aiqa.ext.v1` request and event envelopes encoded as one JSON object per line.
- **FR-10:** Manifest negotiation must occur without target credentials.
- **FR-11:** Run credentials must be sent through stdin only; the child environment must be constructed from a documented non-secret allowlist.
- **FR-12:** AIQA must execute extension binaries directly by resolved absolute path and must not invoke a shell.
- **FR-13:** A suite `assertion_failed` outcome must be carried in a result record while the extension process exits zero after a complete protocol run.
- **FR-14:** A non-zero process exit must represent runner failure and must not overwrite valid suite results.
- **FR-15:** AIQA must preserve valid results around malformed JSONL records and add separate protocol-error evidence.
- **FR-16:** Missing or duplicate terminal results for planned suites must produce runner-error evidence.
- **FR-17:** The result validator must be shared by built-in and executable sources.
- **FR-18:** Suite duration must exclude extension discovery, manifest negotiation, process startup, scheduling, and report generation.
- **FR-19:** Startup and total runner wall time must be recorded separately in JSON telemetry.
- **FR-20:** Extension manifests may declare suite applicability and cost but must not declare requirement level, score, or verdict policy; extension observations must not directly assign the final report status.
- **FR-21:** Only active-profile bindings may make extension evidence part of canonical conformance assessment.
- **FR-22:** Unbound extension results must remain visible as non-scored supplementary evidence.
- **FR-23:** Each profile-bound suite must contribute independently through its bound canonical requirement; no extension-level aggregate contributes.
- **FR-24:** Provider/rate-limit/runner errors must remain inconclusive evidence and must not be normalized to compatibility failures.
- **FR-25:** Extension execution must use the global concurrency budget and target pacing policy.
- **FR-26:** Reports must identify extension source, version, and protocol version and distinguish suite status from runner status.
- **FR-27:** JSON reports for executable extensions must include resolved path and SHA-256 digest; Markdown must omit the path unless verbose provenance output is explicitly requested.
- **FR-28:** AIQA must sanitize credentials and bound stdout, stderr, line size, total result count, and execution time.
- **FR-29:** The implementation must remove all Official SDK Docker transport and legacy human-output parsing paths.
- **FR-30:** The extension protocol must have fixture-based conformance tests and a written author guide.

## Non-Goals

- Loading Go shared objects or using Go's `plugin` package.
- Treating external executables as sandboxed or safe for untrusted code.
- Downloading, installing, updating, or signing third-party extensions.
- Providing a Docker transport, Docker image lifecycle, or container-network configuration in AIQA.
- Running extension suites implicitly during `test=compat`.
- Allowing extensions to redefine profile requirements, requiredness, scoring, verdict ordering, or exit policy.
- Supporting remote extension services over HTTP, RPC, or MCP in the first version.
- Letting an external executable call back into AIQA's HTTP client.
- Guaranteeing that a trusted external executable actually obeys declared pacing or refrains from reading host resources.
- Maintaining backward compatibility with legacy external-checker CLI flags, report fields, or text output.
- Adding an extension package registry or marketplace.
- Implementing targeted `verify` commands beyond the selectors required here; the existing targeted-verification backlog remains separate.

## Design Considerations

### Terminology

Use **extension suite** for this feature. The repository already uses **module** for capability-routed groups such as `chat-completions` and `anthropic.messages`; reusing that word for executable plugins would make profiles and reports ambiguous.

Use these stable built-in IDs:

```text
official-sdk.openai
official-sdk.anthropic
```

The report display names remain user-facing, for example `Official SDK — OpenAI Go v3.x.y`.

### CLI examples

```text
aiqa test=ext-suite
aiqa test=compat,ext-suite
aiqa test=ext-suite --ext-suite=official-sdk.openai
aiqa test=ext-suite --ext-suite=acme.gateway --ext-suite-path=/opt/aiqa/aiqa-acme-ext-suite
```

`test=ext-suite` is the explicit authorization boundary for executing matching binaries discovered from `PATH`. Documentation must warn that such executables run with the same OS user privileges as AIQA.

### Report presentation

The top-level report summary should identify selected extension checks without exposing implementation transport. Detailed Markdown should group results by extension and list suite, subject, status, duration, and concise summary. Runner failures belong in a separate row or subsection and must not look like a failed API compatibility suite.

## Technical Considerations

### Package layout

The intended layout is:

```text
internal/extsuite
    shared manifest, request, event, result, registry, planner, and validator
internal/extsuite/builtin/openai
    Official OpenAI Go SDK implementation
internal/extsuite/builtin/anthropic
    Official Anthropic Go SDK implementation
internal/extsuite/process
    executable discovery, clean process environment, JSONL transport, and limits
internal/extsuite/reporting
    extension provenance and result conversion into the shared result model
```

`internal/app` constructs the registry explicitly and supplies the shared scheduler, pacer, target configuration, profile, and report sink.

### Built-in interface

The exact Go names may change, but the boundary must provide these responsibilities:

```go
type Extension interface {
    Manifest() Manifest
    Run(context.Context, RunRequest, EventSink) error
}

type EventSink interface {
    Emit(ResultEvent) error
}
```

The sink validates each event before retaining it. Built-ins do not write directly to `result.Report`.

### Executable protocol

Manifest request:

```json
{"schema_version":"aiqa.ext.v1","type":"manifest_request","host":{"name":"aiqa","version":"1.0.0-alpha.x"}}
```

Manifest response:

```json
{"schema_version":"aiqa.ext.v1","type":"manifest","extension":{"id":"acme.gateway","name":"Acme Gateway Suite","version":"1.2.0"},"protocols":["openai"],"suites":[{"id":"chat.stream","subject":"model","capabilities":["chat.completions.text"],"cost":"text","internal_concurrency":1}]}
```

Run request:

```json
{"schema_version":"aiqa.ext.v1","type":"run_request","run_id":"opaque-id","target":{"id":"openai","protocol":"openai","base_url":"https://example.test/v1"},"subject":{"kind":"model","id":"model-a"},"suite_ids":["chat.stream"],"profile":{"id":"openai-compat","revision":"2026-09-01"},"limits":{"request_timeout_ms":120000,"deadline_ms":240000,"max_concurrency":1,"request_interval_ms":100},"credential":{"type":"api_key","value":"secret"}}
```

Suite result:

```json
{"schema_version":"aiqa.ext.v1","type":"suite_result","run_id":"opaque-id","extension_id":"acme.gateway","suite_id":"chat.stream","subject":{"kind":"model","id":"model-a"},"outcome":"pass","duration_ms":136,"summary":"SDK sent and parsed a streamed response"}
```

The schemas must use `additionalProperties: false` for protocol control records. A future protocol version may add fields through explicit version negotiation rather than silently changing v1 interpretation.

### Profiles and assessment

Extension applicability belongs in the manifest, but normative meaning belongs in the compatibility profile. The profile contract therefore needs explicit extension bindings. A conceptual binding is:

```json
{
  "extension_id": "official-sdk.openai",
  "suite_id": "chat.stream",
  "requirement_id": "official-sdk.openai.chat.stream",
  "related_requirement_ids": ["chat.stream"],
  "subject": "model"
}
```

The referenced requirement must exist in the profile and carry its normal core/optional level there. This replaces the current behavior that synthesizes optional `official-sdk.*` requirements from runner output. The change introduces `aiqa.profile.v3`, new OpenAI and Anthropic profile revisions, and corresponding schema validation before the old runner is removed.

The host, not the extension, derives the report status. For example, an `assertion_failed` observation becomes `fail`; a recognized optional-capability `request_rejected` observation with HTTP 400, 404, or 422 may become `unsupported`; and a 408, exhausted 429, 5xx, timeout, or transport failure becomes `error`.

### Dependency policy

The OpenAI documentation currently describes `github.com/openai/openai-go/v3` as the Go API helper and marks it beta. The implementation must pin an exact version, isolate it behind the built-in extension package, record the version in reports, and require compatibility fixtures before dependency upgrades. See [OpenAI SDKs and CLI](https://developers.openai.com/api/docs/libraries).

The Anthropic dependency currently isolated under `tools/anthropic-sdk-checker` must move into the main module and remain pinned. Both SDKs must be included in dependency, license, vulnerability, and SBOM checks. The expected increases in binary size and build time must be measured and recorded, not treated as regressions without an agreed budget.

### Security model

Executable extensions are trusted local programs, not sandboxed plugins. JSONL limits data exchange but does not limit what the process can access as the current OS user. AIQA must:

- execute only explicit candidates or names matching `aiqa-*-ext-suite` during an explicit extension run;
- resolve the path once and execute the resolved absolute file;
- avoid a shell;
- clear vendor credential and SDK-control environment variables;
- pass credentials only in the run request;
- redact known credentials from every captured field;
- cap stdout, stderr, line size, result count, and runtime;
- terminate the complete process group on cancellation where the OS supports it;
- record executable provenance in JSON.

Cryptographic trust, signatures, sandboxing, and package installation are deferred.

### Concurrency and pacing limitation

Built-ins can directly use AIQA scheduler and pacing services. External executables cannot be forced to obey request pacing because their SDK owns the HTTP calls. AIQA can reserve global slots, pass limits, serialize untrusted concurrency declarations, and classify observed 429 responses correctly, but protocol compliance remains part of the executable trust contract. Reports and documentation must state this limitation plainly.

### Migration sequence

1. Add `aiqa.profile.v3` profile-bound extension requirements and the grouped `aiqa.report.v3` result/provenance schema without changing execution.
2. Add the shared registry, planner, validator, and built-in interface.
3. Port Anthropic Official SDK suites into the built-in package and prove parity.
4. Implement OpenAI suites directly with the pinned official OpenAI Go SDK and prove parity with the current useful coverage.
5. Add executable discovery and the `aiqa.ext.v1` conformance fixture executable.
6. Add `test=ext-suite`, filtering, combined-run behavior, reports, and documentation.
7. Remove legacy Docker/binary modes, external text parser, separate checker module, images, flags, report fields, and documentation.

The migration may use short-lived internal adapters on a development branch, but the delivered public contract must not retain dual legacy modes.

## Success Metrics

- Official OpenAI and Anthropic SDK suites run successfully against deterministic loopback providers on Linux without Docker, host networking, or helper binaries.
- One hundred percent of retained current Official SDK suite cases have parity fixtures and explicit profile bindings.
- Built-in and executable fixtures produce identical validated result objects for the same manifest and suite events.
- An assertion failure, failed runner, rate limit, unsupported capability, and malformed protocol record each produce distinct expected report states.
- Valid partial results survive every tested timeout, malformed-line, and non-zero-exit scenario.
- No API key appears in argv inspection, child environment inspection, JSON/Markdown reports, captured artifacts, or failing test output.
- Extension result and Markdown order are byte-for-byte deterministic across repeated concurrent runs.
- The global concurrency ceiling is not exceeded in built-in load tests or declared executable reservations.
- The repository contains no Official SDK Docker runtime path, checker image flag, or legacy human-output parser after migration.
- Binary size, cold startup time, and build time changes from embedding both SDKs are measured and documented before release.

## Open Questions

- What maximum binary-size increase is acceptable after embedding both generated SDKs?
- Should a later release add an `aiqa ext list` inspection command, or is verbose planning output sufficient for v1?
- Should explicit executable paths be supported only by flags/environment in v1, or should AIQA first introduce a persistent configuration file?
- Which exact OpenAI Official SDK suites are retained when replacing `beranekio/openai-compatibility-tester`, especially for legacy Chat Completions versus the current Responses API?
- What line, total-output, stderr, process-duration, and maximum-result limits should be the v1 protocol defaults?
