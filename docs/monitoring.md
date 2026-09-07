# Monitoring execution progress

AIQA always reports execution progress to `stderr`. Progress is an
operator-facing interface and is independent of diagnostic verbosity: a normal
run is observable without `--verbose`.

## Terminal behaviour

When `stderr` is a TTY, AIQA redraws one ASCII progress line for the active
target. It identifies the target, current model/probe when available,
completed and planned logical units, percentage, measured rate, ETA, and
provider pacing waits. The line is terminated before a target or run finishes
or is interrupted.

When `stderr` is redirected or CI is non-interactive, AIQA emits durable plain
text milestones instead: run/discovery start, a target plan, 10-percent
checkpoints, a 30-second heartbeat, plan revisions, pacing waits, target end,
and final report paths. It emits neither ANSI escapes nor carriage-return
redraws in this mode.

Progress is deliberately written to `stderr`; `stdout` is not used for
progress, diagnostics, or report serialization. Markdown and JSON reports are
the requested output files.

## Counting and ETA

The progress unit is one host-owned logical lifecycle operation, not an HTTP
attempt. Retries, HTTP transport attempts and rate-limit backoff do not make a
run look more complete.

Before provider inventory is received, AIQA shows an indeterminate planning or
discovery state. Once selection and capability classification make the native
work known, it publishes `done/total`. Conditional work may extend that plan;
the terminal explicitly reports `plan revised` and never permits `done >
total`. Adaptive model-parameter work and opaque executable extension activity
are represented by host-owned units, never guessed external HTTP activity.

Rate and ETA use completed lifecycle units. ETA is `estimating` until enough
terminal events exist, and remains `unknown` rather than inventing a value when
there is no valid estimate. Pacing state is shown separately, so a provider
cooldown is not indistinguishable from a hung process.

## Diagnostics and privacy

`--verbose` controls only private JSONL sidecars:

- level 0 (default): no `.log` sidecar;
- levels 1 through 4: `<name>.log` with increasing WARN through TRACE detail;
- level 4: additionally writes `<name>.trace-verdict.json`.

The terminal never receives raw JSONL. The sidecar format, redaction rules,
correlation fields and structural verdict are specified in
[the forensic trace contract](trace-contract.md). Progress accepts only safe
target, model, suite and probe identifiers; it never renders credentials,
provider URLs, prompts, request/response bodies, completions, tool arguments
or media.

## Implementation boundaries

Progress is driven by local telemetry lifecycle events and explicit runner
plans. It does not change provider requests, scheduling policy, retry policy,
rate-limit classification, public report schemas, compatibility verdicts, or
exit-code policy. Extension executables retain their own execution boundary:
AIQA tracks declared host suite units but does not infer their HTTP attempts.
