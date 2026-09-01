# Extension suites

Extension suites add client-library and third-party evidence to an AIQA run. They do not define compatibility policy. The active `aiqa.profile.v3` profile decides whether a suite result affects conformance, which requirement it supports, and whether that requirement is core or optional.

## Running extensions

Extension execution is always explicit:

```bash
aiqa test=ext-suite
aiqa test=compat,ext-suite
aiqa test=ext-suite --ext-suite=official-sdk.openai
aiqa test=ext-suite \
  --ext-suite=acme.gateway \
  --ext-suite-path=/opt/aiqa/aiqa-acme-ext-suite
```

`test=ext-suite` runs every applicable built-in and discovered extension. Repeat `--ext-suite` or pass comma-separated IDs to select exact extensions. Selecting an unavailable ID is a configuration error. A `compat`-only run neither scans `PATH` nor executes extensions.

AIQA includes two built-ins:

| Extension ID | Implementation | Embedded version | License |
|---|---|---:|---|
| `official-sdk.openai` | [Official OpenAI Go SDK](https://github.com/openai/openai-go) | `v3.54.0` | [Apache-2.0](https://github.com/openai/openai-go/blob/v3.54.0/LICENSE) |
| `official-sdk.anthropic` | [Official Anthropic Go SDK](https://github.com/anthropics/anthropic-sdk-go) | `v1.68.0` | [MIT](https://github.com/anthropics/anthropic-sdk-go/blob/v1.68.0/LICENSE) |

Both SDKs disable their automatic retries. Requests use AIQA's bounded executor, adaptive target pacer, retry ceiling, and `Retry-After` handling.

The embedded suites retain every case that the previous runners could reach through current profiles: Models list/retrieve, chat or Messages, streaming, streaming usage, JSON or structured output, tools, tool streaming, multi-turn/system behavior, error parsing, token counting, and OpenAI image generation. Legacy OpenAI checker labels for Embeddings, Responses, and Audio were removed because AIQA had no selectable native modules or profile bindings for them. They can return as extension suites only with capability routing, tests, and explicit profile requirements.

## Applicability and cost

An extension manifest declares each suite's target protocol, provider or model subject, required model capabilities, cost class, and internal concurrency. AIQA combines those declarations with model classification and the active profile's module assessment.

Provider suites run once per target. Model suites run only when their capability and module are applicable. Unclassified models follow the configured unknown-model policy. Positive non-text evidence never falls back to text suites.

Suites marked `billable_artifact` require `--allow-billable-artifacts`. An extension-only run needs one available artifact slot per model. A combined native and extension run needs two because the native and SDK suites each create an artifact.

## Profile bindings

An `aiqa.profile.v3` binding connects an extension result to compatibility policy:

```json
{
  "extension_id": "official-sdk.openai",
  "suite_id": "chat.stream",
  "requirement_id": "official-sdk.openai.chat.stream",
  "related_requirement_ids": ["chat.stream.sse"],
  "subject": "model",
  "module": "chat-completions"
}
```

The referenced requirement is a normal profile probe with `"executor": "extension"`. Its `required` value determines whether the requirement is core or optional. An extension cannot set requiredness, weight, score, or verdict policy.

A valid unbound result remains in `targets[].extensions[]` as supplementary evidence. It does not enter requirement counts or change the verdict. Several suites may bind to one canonical requirement; their observations remain separate evidence records, while the requirement counts once.

## Outcome and status

Extensions report raw outcomes. AIQA derives report statuses:

| Extension outcome | Report status |
|---|---|
| `pass` | `PASS` |
| `assertion_failed` | `FAIL` |
| `execution_error` | `ERROR` |
| `skip` | `SKIP` |
| `request_rejected` | Derived from HTTP evidence and the profile |

An exhausted HTTP 408, 429, 5xx response, timeout, or transport failure is `ERROR`, not proof of incompatibility. A recognized 400, 404, or 422 capability rejection becomes `UNSUP` only for an optional profile-bound capability. Other rejected requests are `FAIL`.

Runner failures are separate from suite outcomes. A non-zero exit, timeout, signal, malformed record, duplicate result, or missing planned result sets the invocation's runner status to `ERROR`. Valid suite results emitted before or after a malformed record remain in the report. For a bound suite, the runner failure is separate `ERROR` evidence: a valid `PASS` remains visible but the combined requirement becomes an evidence-gap warning. A missing bound result creates direct `ERROR` suite evidence.

## Executable discovery and trust

AIQA discovers trusted executables only during `test=ext-suite`:

1. repeatable absolute `--ext-suite-path` values;
2. the OS path list in `AIQA_EXT_SUITE_PATHS`;
3. executable names matching `aiqa-*-ext-suite` in normal `PATH` precedence.

AIQA resolves each path once, validates its manifest, and records the absolute path and SHA-256 digest in JSON provenance. Markdown omits executable paths. Duplicate IDs, attempts to shadow built-ins, invalid manifests, unsupported protocol versions, and non-executable paths are configuration errors.

Extension executables are trusted local programs. The JSONL boundary is not a sandbox: an extension runs with the same OS user privileges as AIQA. AIQA does not download, install, update, or verify signatures for extensions.

## Process protocol

External executables implement `aiqa.ext.v1` as one JSON object per line. The strict control-record schema is [`schema/ext-v1.schema.json`](../schema/ext-v1.schema.json).

For manifest negotiation, AIQA starts the executable directly, with no shell and a clean environment, then writes one credential-free request:

```json
{"schema_version":"aiqa.ext.v1","type":"manifest_request","host":{"name":"aiqa","version":"1.0.0-alpha.2"}}
```

The executable writes exactly one `manifest` record and exits zero. stdout is reserved for protocol records; human diagnostics belong on stderr.

For execution, one process handles one extension, target, and provider/model subject group. AIQA writes one `run_request`, including the exact suite IDs, target, subject, profile revision, timeouts, allowed concurrency, initial request interval, the current observed safe request rate and concurrency, a retry limit of two retries (three attempts), and the credential. The credential is sent only through stdin. AIQA never adds it to argv or the child environment.

Each planned suite writes one terminal `suite_result` record:

```json
{"schema_version":"aiqa.ext.v1","type":"suite_result","run_id":"opaque","extension_id":"acme.gateway","suite_id":"chat.stream","subject":{"target_id":"openai","protocol":"openai","kind":"model","id":"model-a"},"outcome":"pass","duration_ms":136,"summary":"SDK sent and parsed a streamed response"}
```

Exit zero means the protocol run completed. Assertion failures remain suite results and do not require a non-zero exit. Suite `duration_ms` measures request and parse work only. JSON records separately expose process startup and total wall time.

AIQA limits each manifest to 1 MiB, each JSONL line to 1 MiB, total stdout to 16 MiB, stderr to 64 KiB, and execution to the planned deadline. Cancellation terminates the process group on Unix. Captured error diagnostics are sanitized, deduplicated, and stored as bounded guidance artifacts.

External executables must disable vendor SDK retries and honor the supplied pacing and concurrency limits. AIQA reserves the declared number of global slots, but it cannot enforce an external process's internal HTTP behavior. The JSON runner record therefore reports `host_enforced` for built-ins and `extension_required_unverified` for executables. An extension that declares concurrency zero is treated as unsafe and serialized across subjects.

## Writing an extension

Use this sequence:

1. Choose a lowercase stable ID containing letters, digits, dots, or hyphens.
2. Implement credential-free manifest negotiation.
3. Declare suite subjects, capabilities, cost, and internal concurrency accurately.
4. Accept only the suite IDs in each `run_request`.
5. Disable SDK retries and obey the supplied deadline, pacing, and concurrency limits.
6. Emit one terminal result per planned suite, even when another suite fails.
7. Keep stdout protocol-only and send bounded diagnostics to stderr.
8. Test malformed input, cancellation, partial output, duplicate results, and secret handling.

[`test/fixtures/ext-suite`](../test/fixtures/ext-suite) is a small conforming executable used by AIQA's contract tests. Its deliberate failure cases prove that valid results before and after a malformed record survive alongside a non-zero exit, and that an earlier result also survives a later timeout.

## Reports

`aiqa.report.v3` groups extension data under each target:

- `provenance` identifies the extension, version, source, protocol, and executable digest;
- `runner` records startup, wall time, exit code, and actual execution failures;
- `suites` records each suite's raw outcome, derived status, subject, duration, HTTP evidence, usage, signals, and profile binding.

Markdown lists enabled checks by display name and version, then renders `Suite`, `Subject`, `Status`, `Duration`, and `Summary`. It shows a runner issue only when process startup, protocol handling, timeout, or termination actually failed.

## Validation gates

The extension contract is protected at five boundaries:

| Boundary | Repository evidence |
|---|---|
| SDK parity | Each built-in has an exact suite-list test, an explicit-binding test for every suite, and a build-information check that the reported SDK version matches the pinned module. |
| End-to-end behavior | Deterministic OpenAI and Anthropic loopback providers run both embedded SDKs directly, validate report v3, and verify combined native-plus-extension assessment. |
| Process protocol | Contract tests and the published fixture cover credential-free manifest negotiation, stdin-only run credentials, executable provenance, malformed and duplicate records, bounded output, non-zero exit, partial output, and timeout recovery. |
| Routing and load | Planner tests cover protocol and capability isolation, unknown/text and non-text models, billable budgets, built-in concurrency, and executable slot reservations. Race tests protect immutable manifests and concurrent result ordering. |
| Supply chain | `make dependency-check` verifies checksums and exact pins; `make vuln-check` runs pinned `govulncheck`; `make sbom-check` validates a temporary license-bearing CycloneDX SBOM. |

Run `make check`, `make race`, and `make security-check` before changing the protocol, a built-in suite, or an SDK version.

## Embedded SDK build cost

The first implementation was measured against pre-extension commit `ef98a503f882` on Linux amd64 with Go 1.27.0 and 16 reported CPUs. Each cold build used a separate empty `GOCACHE`, `-buildvcs=false`, and `-trimpath`; release-size builds also used `-ldflags '-s -w'`.

| Metric | Before | With embedded SDKs | Change |
|---|---:|---:|---:|
| Stripped binary | 8,749,319 bytes | 19,636,487 bytes | +10,887,168 bytes (+124.4%) |
| Cold build wall time | 8.31s | 24.47s | +16.16s (+194.5%) |
| Cold build peak RSS | 295,136 KiB | 1,549,320 KiB | +1,254,184 KiB (+425.0%) |
| `--version` peak RSS | 8,680 KiB | 13,056 KiB | +4,376 KiB (+50.4%) |

Both `--version` runs completed below the timer's 0.01-second resolution. These measurements document the dependency trade-off; they are not release budgets and will vary by toolchain and host.

`make dependency-check` verifies module checksums and exact SDK pins. `make vuln-check` runs the pinned official Go vulnerability scanner. `make sbom-check` creates a temporary CycloneDX module SBOM with detected licenses and asserts that both embedded SDK versions are present. CI runs the combined `make security-check`; the temporary SBOM is deleted after validation.
