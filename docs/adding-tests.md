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

When several probes test the same normative promise, map them to one canonical requirement. When they test distinct promises, such as valid SSE framing and Official SDK parser interoperability, give them separate requirement IDs and link the external suite through `related_requirement_ids`.

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

## Add a suite

1. Define the suite name in `internal/config` validation.
2. Add a runner under `internal/suites`.
3. Extend orchestration in `internal/app`.
4. Keep report schema backward compatible or increment `schema_version`.
5. Document expected cost and side effects.

An Official SDK suite must declare its canonical interoperability requirement and any related native requirements in `internal/external/runner.go`. Suite parsing may use structured suite/status fields, but provider-specific remediation must not be inferred from free-form checker output.

Suites that execute code, browse the web, persist objects, or create billable assets must be opt-in and clearly marked.

`make schemas-check` validates the generated example and the actionable v1 fixture with JSON Schema Draft 2020-12. The development environment therefore needs Python 3 and a Draft 2020-12-capable `jsonschema` package in addition to Go; CI pins version 4.25.1 before `make check`. The shipped `aiqa` binary still has no external Go dependencies.
