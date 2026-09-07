# Monitoring execution progress

AIQA always reports execution progress to `stderr`. Progress is an
operator-facing interface and is independent of diagnostic verbosity: a normal
run is observable without `--verbose`.

## Terminal behaviour

When `stderr` is a TTY, AIQA first renders one indeterminate `Planning` line.
During that phase it discovers every target inventory, selects each target's
models, and freezes one global logical-job plan. It then renders one ASCII
progress line for the whole run. The line identifies the current target and
model/probe when available, completed and planned logical units, percentage,
measured rate, ETA, and provider pacing waits. The line is terminated before a
run finishes or is interrupted.

The interactive renderer is `github.com/schollz/progressbar/v3`; it owns the
single terminal line. AIQA owns planning and logical-job accounting, rather
than letting terminal redraws determine execution state.

When `stderr` is redirected or CI is non-interactive, AIQA emits durable plain
text milestones instead: planning start, target discovery, frozen global plan,
10-percent checkpoints, a 30-second heartbeat, pacing waits, target end, and
final report paths. It emits neither ANSI escapes nor carriage-return redraws
in this mode.

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

Progress is driven by a two-phase `Prepare(all targets) → Freeze(plan) →
Execute(plan)` contract. Preparation performs target inventory and model
selection; execution reuses that evidence and does not repeat model discovery.
A terminal renderer consumes frozen host-owned jobs; raw telemetry spans remain
diagnostic evidence and do not themselves increment the operator counter. This
does not change provider requests, scheduling policy, retry policy, rate-limit
classification, public report schemas, compatibility verdicts, or exit-code
policy. Extension executables retain their own execution boundary: AIQA tracks
declared host suite units but does not infer their HTTP attempts.
