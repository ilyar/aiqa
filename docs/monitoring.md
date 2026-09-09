# Monitoring execution progress

AIQA always reports execution progress to `stderr`. Progress is an
operator-facing interface and is independent of diagnostic verbosity: a normal
run is observable without `--verbose`.

## Terminal behaviour

When `stderr` is a TTY, AIQA renders an indeterminate `Planning` state while
it discovers every target inventory, selects models, and freezes one global
logical-job plan. Display labels may be shortened, but job identifiers are
never shortened for state lookup. It has no dependency on a third-party TTY
framework.

When `stderr` is redirected, `TERM=dumb`, or CI is non-interactive, AIQA emits
plain durable milestones and no terminal control sequences. Progress remains
on `stderr`; `--stdout=json|markdown` emits only the selected final public
report on stdout.

Progress is deliberately written to `stderr`; `stdout` is not used for
progress, diagnostics, or report serialization. Markdown and JSON reports are
the requested output files.

## Counting and ETA

The progress unit is one host-owned logical lifecycle operation, not an HTTP
attempt. Retries, HTTP transport attempts and rate-limit backoff do not make a
run look more complete.

Before provider inventory is received, AIQA shows an indeterminate planning or
discovery state. Once selection and capability classification make the work
known, AIQA freezes one global `done/total`; that total never changes during
execution. A unit has a stable host-owned ID. Conditional work is reserved at
planning time and ends exactly once as executed, skipped, blocked, or failed.
Adaptive model-parameter work is one composite unit, and opaque executable
extension activity is one host-owned unit per declared suite; neither is guessed
from external HTTP activity.

Rate and ETA use completed lifecycle units. ETA is a remaining-work simulation
and remains `unknown` rather than inventing a value when there is no valid
estimate. Pacing state is separate from HTTP in-flight accounting: a queued
cooldown does not occupy a global request slot.

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

Progress is driven by a two-phase `Prepare(all targets) → Freeze(RunPlan) →
Execute(plan)` contract. Preparation performs target inventory and model
selection exactly once; execution consumes that frozen binding. A run-owned
admission broker limits physical attempts across native and SDK transports;
leases are held through body/stream close and are not held while pacing waits.
The optional `execution` block in `aiqa.report.v3` contains only safe aggregate
counters and durations, so existing report readers remain compatible.

`aiqa log [FILE|-]` reads private JSONL locally, supports safe filters and
follow mode, and never contacts providers. The current release plan is
[`tasks/next.md`](../tasks/next.md); the former backlog has been retired.
