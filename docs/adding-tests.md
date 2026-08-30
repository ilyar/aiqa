# Adding tests

## Add a compatibility probe

1. Add a unique entry to `profile/openai-compat-2026-08.json`.
3. Implement the probe in `internal/suites/compat.go`.
4. Return `pass`, `warn`, `unsupported`, `fail`, `error`, or `skip` deliberately.
5. Add deterministic behavior to the integration mock in `internal/app/app_test.go`.
6. Run `make check` and `make race`.

A probe should record:

- expected behavior from the profile;
- observed HTTP metadata;
- token usage where available;
- a short sanitized observation;
- metrics needed for later machine analysis.

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

Suites that execute code, browse the web, persist objects, or create billable assets must be opt-in and clearly marked.
