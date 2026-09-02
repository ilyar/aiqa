# Changelog

## Unreleased

## 1.0.0-alpha.3 – 2026-09-02
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
