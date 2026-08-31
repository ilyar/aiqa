# Changelog

## 1.0.0-alpha.0 – 2026-08-30

- Initial `compat` suite for OpenAI Models and Chat Completions APIs.
- SSE chunk validation, streaming usage, tool-choice modes, streamed tools, tool continuation, structured outputs, logprobs, and error-shape probes.
- Initial `model-param` suite with context-boundary search, reasoning controls, sampling controls, output limits, penalties, verbosity, and parallel tools.
- Markdown and versioned JSON reports with atomic writes.
- Optional integration with `beranekio/openai-compatibility-tester` and the official OpenAI Go SDK.
- Canonical requirement assessment with authoritative `PASS`, `PASS-WITH-GAPS`, `PARTIAL`, `FAIL`, and `INCONCLUSIVE` verdicts; Official SDK results are evidence rather than duplicate score votes.
- Deterministic actionable guidance with typed findings, P0–P3 recommendations, conditional impact, verification steps, versioned catalogs and packs, and deduplicated redacted artifacts.
- Action-first Markdown reports, additive `aiqa.guidance.v1` JSON, deprecated legacy-score metadata, and Draft 2020-12 schema fixtures.
- Deterministic mock-provider integration tests, race checks, CI across the minimum and current stable Go versions, and static cross-platform builds.
