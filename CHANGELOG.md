# Changelog

## 1.0.0-beta.2 – 2026-10-16

- Distinguish credential/access prerequisites from provider conformance. Keep
  authentication failures visible without recommending protocol implementation
  changes; preserve explicit invalid-auth contract checks and independent failures.
- Separate Google GenerateContent protocol validation from exact-text and tool
  obedience. Use a bounded generation budget shared by native and SDK checks,
  and retain safe finish-reason diagnostics for incomplete generation.
- Represent selected but prerequisite-blocked or empty executors explicitly in
  forensic traces, without treating absent execution as successful coverage.
- Preserve discovery request statistics when execution stops before
  any model is selected, including rejected credentials.
- Update public binaries, download links, and installer defaults to beta.2.
  Operator skill 1.0.1 accepts both beta.1 and beta.2; the original pinned skill
  1.0.0 release remains unchanged.

## 1.0.0-beta.1 – 2026-10-16

- Publish the first public AIQA distribution, with static binaries for Linux,
  macOS, Windows, and FreeBSD on amd64 and arm64, SHA-256 checksums, and the
  project license.
- Update vulnerable transitive dependencies, make log-follow file identity
  portable across supported operating systems, and provide a checksum-verifying
  installer for the public release assets.
- Add the GitHub Pages introduction, platform downloads, and installation guide.
- Harden provider-controlled redirects, artifact destinations, inventory and
  streaming work limits, retry cooldowns, and context-probe accounting. Prevent
  replay of image generation and persistent-resource creation requests.
- Redact configured secrets before trace serialization, handle overlapping
  credentials safely, render hostile report text literally, and keep untested
  model scopes inconclusive instead of reporting a rankable success.
- Add the `test=sdk` selector for built-in SDK checks, keep `test=ext-suite` for generic extensions, and shorten report labels to SDK names, versions, and links.
- Replace protocol-specific model selectors with one global `--model` list. Each active target intersects it with its validated inventory, keeps absent IDs silent, and records the requested list once in report v3. Removed selectors and `GOOGLE_GENAI_MODEL` return migration errors.
- Add explicit ambiguous-model preflight and route/transient classification, capability-aware module materialization, dependency blocking, provider-root guidance, exact transport retry telemetry, and the pinned Anthropic Models validator shape.
- Add first-class Google GenAI (`google-genai`) support with modern Interactions and strict legacy GenerateContent profiles, target-scoped configuration, metadata-aware routing, native REST/SSE probes, usage/error validation, and explicit persistent-resource safeguards.
- Embed the pinned Google Gen AI Go SDK `v1.71.0` as `official-sdk.google-genai`; aggregate `--ext-suite=official-sdk` now includes it only when the Google target is configured. The report records that this SDK has no public Interactions API methods.
- Add Google guidance, report-v3 target notes, service/version rendering, deterministic profile/schema/dependency/SBOM gates, and documentation for current/legacy surfaces and capability selection.

## 1.0.0-alpha.14 – 2026-09-16

- Give the Anthropic strict-tool probe a 128-token output budget and report truncation as incomplete execution evidence. Validate extra properties in completed strict tool inputs.
- Send only supported contents fields to Gemini countTokens instead of a generation request body.
- Treat a narrowly identified image-model rejection during speculative capability discovery as an unavailable route, preserving raw evidence and conflicting results. Declared image support remains subject to normal conformance checks.
- Explain exhausted daily quotas separately from temporary throttling in report recommendations.
- Probe individually unknown Anthropic capabilities without discarding declared metadata; recognize nested thinking support and preserve explicit exclusions and cost gates.
- Preserve the documented Sonnet 5 prefill rejection as raw evidence while marking the optional positive-prefill requirement not applicable.
- Report explicit Google model-access restrictions as incomplete execution evidence in native and SDK checks. Avoid retrying the same generation request after a confirmed structured daily-quota rejection.
- Defer model-parameter changes to `model-param-backlog.md`; this correction targets `test=compat,sdk`.

## 1.0.0-alpha.13 – 2026-09-16

- Keep execution progress live across rate-limit cooldowns, including expired deadlines. Show an explicit monitoring-unavailable message if accounting fails instead of displaying stale counters and ETA.
- Recover adaptive request pacing by halving the interval after five successful attempts, down to the existing 200 ms floor, while preserving `Retry-After` cooldowns.
- Default missing-capability handling to `probe`: run eligible selected profile checks and bound built-in SDK suites instead of skipping models solely because capability metadata is absent.
- Report missing metadata as an advisory, without inventing supported capabilities or lowering the compatibility verdict. Keep declared capabilities, explicit negative metadata, and cost/resource permission gates authoritative.

## 1.0.0-alpha.12 – 2026-09-15

- Compact terminal ETA as `ETA ~1m41s` and show the next wait deadline inline as `Waiting 1/1s`, without a separate waiting detail row.

## 1.0.0-alpha.11 – 2026-09-15

- Recognize declared rerank-only models without routing them through chat checks.
- Respect explicit unsupported model capabilities and accept empty Google method arrays without inventing generation support.
- Report actionable model metadata diagnostics and prevent discovery-only success when no model matches the selected compatibility profile.

## 1.0.0-alpha.7 – 2026-09-06
- Add private `aiqa.trace.v1` forensic sidecars, offline trace verdicts, strict
  lifecycle and HTTP ownership verification, and cooperative executable-suite
  tracing.
- Add `make qa` as the required deterministic local pre-commit gate.

## 1.0.0-alpha.6 – 2026-09-06
- Render every configured protocol service in the Markdown `Service` field, while retaining origin-only links that exclude API paths, query parameters, credentials, and API keys.

## 1.0.0-alpha.4 – 2026-09-02
- Replace Docker and checker-specific Official SDK transports with the explicit `test=ext-suite` architecture.
- Embed OpenAI Go SDK `v3.54.0` and Anthropic Go SDK `v1.68.0`, disable SDK retries, and route built-in requests through AIQA concurrency and pacing controls.
- Preserve the retained Models, chat/Messages, streaming, structured-output, tools, error, token-counting, and image SDK cases as independently bound suites; omit obsolete OpenAI checker labels that had no selectable profile module.
- Add trusted executable discovery, the strict `aiqa.ext.v1` JSONL protocol, bounded partial-output recovery, stdin-only credentials, clean child environments, and executable SHA-256 provenance.
- Share each target's adaptive pacer and global executor across native and extension phases, expose current pacing constraints to executables, and enforce one deterministic per-model artifact budget across native and extension suites.
- Introduce `aiqa.profile.v3` extension bindings and grouped `aiqa.report.v3` provenance, runner, and suite results. Unbound extension evidence is supplementary and cannot affect conformance.
- Remove legacy external-checker flags, Docker assets, human-output parsing, report fields, schemas, and the separate Anthropic checker module.
- Document the extension author contract and publish a reusable conformance fixture under `test/fixtures/ext-suite`.
- Show a host-only linked `Service` in Markdown reports without exposing API paths, query parameters, or credentials.
- Allow `--ext-suite=official-sdk` to select the configured OpenAI and/or Anthropic Official SDK suites automatically.
- Initial `compat` suite for OpenAI Models and Chat Completions APIs.
- SSE chunk validation, streaming usage, tool-choice modes, streamed tools, tool continuation, structured outputs, logprobs, and error-shape probes.
- Initial `model-param` suite with context-boundary search, reasoning controls, sampling controls, output limits, penalties, verbosity, and parallel tools.
- Markdown and versioned JSON reports with atomic writes.
- Optional integration with `beranekio/openai-compatibility-tester` and the official OpenAI Go SDK.
- Canonical requirement assessment with authoritative `PASS`, `PASS-WITH-GAPS`, `PARTIAL`, `FAIL`, and `INCONCLUSIVE` verdicts; Official SDK results are evidence rather than duplicate requirement weight.
- Deterministic actionable guidance with typed findings, P0–P3 recommendations, conditional impact, structured verification targets, versioned catalogs and packs, and deduplicated redacted artifacts.
- Action-first Markdown reports, `aiqa.guidance.v1` JSON without an overall numeric score, and Draft 2020-12 schema fixtures.
- Deterministic mock-provider integration tests, race checks, CI across the minimum and current stable Go versions, and static cross-platform builds.
- Independent OpenAI and Anthropic targets with protocol-specific credentials, profiles, model selection, and deterministic combined execution.
- `anthropic-compat@2026-09-01`, pinned to `anthropic-version: 2023-06-01` and the official Anthropic Go SDK v1.68.0.
- Native Anthropic Models, Messages, error-envelope, named-event streaming, tool-use, Count Tokens, structured-output, vision, and opt-in thinking probes.
- `aiqa.report.v2` with target-local requirements, verdicts, guidance, usage, per-target/model rate-limit telemetry, strict schema validation, and output-boundary secret redaction; no cross-protocol percentage.
- Separate JSONL Anthropic Official SDK checker with explicit credentials, disabled retries, Docker packaging, and partial-output recovery.
- Run independent OpenAI and Anthropic native probes concurrently under one `--concurrency` ceiling, with deterministic result ordering and adaptive reduction after rate limits.
- Normalize multi-target token usage into input/output/cache/total columns and fix missing or duplicated usage from native OpenAI and Anthropic probes.
