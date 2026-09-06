# AIQA backlog

This file contains only work that is not implemented. Completed PRDs and their
acceptance ledgers are removed; current behavior is documented in `docs/` and
enforced by `make qa`.

## 1. Lost relevance

These former ideas have no accepted product requirement. Do not implement them
from this list; they require a new decision and a new PRD if revived.

- Standalone baseline comparison, `aiqa explain`, `aiqa verify`, and generated
  opt-in shell commands from guidance. Current reports identify stable
  verification targets but deliberately do not generate commands.
- Additional report presentation/export formats: multi-target HTML, SARIF,
  generated explanations, and Russian guidance. They do not change canonical
  compatibility assessment and have no current consumer or acceptance contract.

## 2. Worth doing

### [Forensic baseline diagnostics](prd-forensic-baseline-diagnostics.md)

Make `--verbose 4` sufficient for safe, evidence-based interpretation of
baseline failures; repair the confirmed Google Interactions SSE `index`
validator defect; distinguish provider nonconformance from checker defects,
behavioral observations and execution gaps; and close trace lifecycle gaps.

### Honest verdict for unresolved model capability coverage

When selected inventory models have unknown capabilities and therefore receive
no model probes, AIQA must not report overall `PASS` as though generation was
evaluated. Preserve the inventory-only no-request policy, but expose the
unresolved selected scope in JSON and Markdown; make core unknown coverage
`INCONCLUSIVE`, set `rankable=false`, and retain target/model-local provenance.

Acceptance must use deterministic fixtures for unknown IDs, mixed PASS plus
unknown coverage, direct FAIL plus unknown coverage, explicit preflight,
transient and route-rejection outcomes, zero-match selection, and `--fail-on`.
No real Dahl/provider run is part of this work.

### Anthropic coverage expansion

Add only separately specified, cost-bounded surfaces: model parameters/context,
prompt caching, document input, Message Batches, Files, and a Messages-only
profile. Each surface needs an explicit profile contract, loopback fixture,
safety/cost gate, canonical assessment mapping, and cleanup proof where it
creates remote state.
