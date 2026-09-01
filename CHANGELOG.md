# Changelog

## 1.0.0-alpha.0 – 2026-08-30

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
- `anthropic-messages-compat@2026-09-01`, pinned to `anthropic-version: 2023-06-01` and the official Anthropic Go SDK v1.68.0.
- Native Anthropic Models, Messages, error-envelope, named-event streaming, tool-use, Count Tokens, structured-output, vision, and opt-in thinking probes.
- `aiqa.report.v2` with target-local requirements, verdicts, guidance, usage, per-target/model rate-limit telemetry, strict schema validation, and output-boundary secret redaction; no cross-protocol percentage.
- Separate JSONL Anthropic Official SDK checker with explicit credentials, disabled retries, Docker packaging, and partial-output recovery.
