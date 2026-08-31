# Actionable reports delivery record

Status: Milestones 0 and 1 completed on 2026-08-31.

The original implementation PRD has been reduced after delivery. Durable behavior now belongs to:

- [`docs/reports.md`](../docs/reports.md) for report, verdict, guidance, CLI, and migration semantics;
- [`docs/architecture.md`](../docs/architecture.md) for package boundaries and runtime flow;
- [`docs/adding-tests.md`](../docs/adding-tests.md) for probe, requirement, signal, catalog, pack, and Official SDK extension rules;
- [`schema/report.schema.json`](../schema/report.schema.json) for the additive `aiqa.report.v1` contract.

This file records the completed scope, acceptance audit, evidence, and deferred milestones. It is no longer a second normative specification.

## Delivered scope

Milestone 0 established the requirement and scoring foundation:

- canonical requirements are assessed once per subject;
- native probes and Official SDK suites remain separate evidence sources;
- distinct SDK parser interoperability promises use explicit optional `official-sdk.*` requirements linked to native requirements;
- `not_applicable` requirements are materialized and excluded from all measures;
- the authoritative verdict is `pass`, `pass-with-gaps`, `partial`, `fail`, or `inconclusive`;
- core, optional, execution, and conclusive-evidence measures are independent;
- legacy `summary.status` and `summary.score` are retained only for additive v1 compatibility; the score is marked deprecated and `summary.assessment.verdict` is authoritative;
- exit policy uses the canonical verdict.

Milestone 1 delivered actionable Markdown and JSON:

- probes emit stable requirement IDs, probe IDs, and typed evidence signals;
- findings are grouped by code, subject, and normalized root-cause parameters;
- findings carry stable fingerprints, observed scope, attribution, confidence, requirements, evidence, and artifact references;
- recommendations carry P0–P3 priority, action kind, guidance level, owner, steps, verification, caveats, references, and conditional impact;
- execution failures produce rerun or investigation guidance;
- native/SDK conflicts preserve both observations and produce low-attribution diagnostics;
- blocked dependent checks link to the parent finding without creating duplicate actions;
- the generic catalog and advice pack are embedded and versioned;
- strict pack decoding prevents normative overrides;
- external logs are redacted, truncated, content-addressed, and rendered once;
- Markdown leads with verdict and actions, collapses passing checks, and separates `UNSUP`, `SKIP`, `ERROR`, and `N/A`;
- `--guidance=full|summary|off`, `--guidance-pack`, and English locale controls are available;
- guidance uses no provider request, network lookup, or LLM call.

## Functional requirement audit

| IDs | Result | Evidence |
|---|---|---|
| AR-001–AR-004 | Pass | `result.Report` is the single renderer input; assessment and guidance consume canonical requirements and typed signals, never human summaries as rule input. |
| AR-005–AR-006 | Pass | Root-cause grouping and stable fingerprints are tested; P0/P1 catalog entries and generic fallbacks include steps and verification. |
| AR-007–AR-010 | Pass | Guidance is downstream and non-normative; N/A is ignored; errors and evidence conflicts have dedicated tests and action policy. |
| AR-011–AR-013 | Pass | Target, attribution, confidence, and profile-based impact are explicit; recommendation ranking never reads the legacy score. |
| AR-014–AR-016 | Pass | External artifacts are deduplicated; catalog and pack revisions are recorded; strict JSON rejects forbidden pack fields. |
| AR-017–AR-021 | Pass | Tuple ordering, collapsed passing checks, distinct labels, methodology, deterministic IDs, and golden snapshots are covered. |
| AR-022–AR-025 | Pass | Guidance failure is contained; diagnostics and commands are redacted; observed scope is distinct from attribution; model findings default to unknown ownership. |
| AR-026–AR-030 | Pass | Blocked requirements link to the parent; coverage uses canonical requirements; guidance is offline; versions and complete reference objects are serialized. |

## Acceptance fixture audit

### Vibecode

The semantic golden `internal/guidance/testdata/vibecode-actions.golden.txt` proves:

- 30 missing `created` fields become one P0 action;
- native and Official SDK Models evidence belongs to the same finding;
- invalid request types reaching 502 become a P1 gateway-validation fix;
- accepted-but-missing logprobs become a separate P1 action;
- native/SDK streaming disagreement becomes one investigation with both evidence records;
- unsupported Models retrieval remains a P2 capability gap;
- fixing `created` projects `FAIL` to `PARTIAL`, not `PASS`;
- repeated external output becomes one artifact.

### DashScope/Qwen

The semantic golden `internal/guidance/testdata/qwen-actions.golden.txt` proves:

- required, forced, and streaming tool-choice constraints become one recommendation;
- tool continuation is linked as blocked and creates no separate action;
- developer role remains a separate P2 capability gap;
- native/SDK streaming disagreement remains a diagnostic;
- all eight native core requirements remain visible as `8/8` while optional and SDK evidence is assessed separately;
- model-scoped evidence does not automatically blame the provider gateway.

### Infrastructure and applicability

Unit tests prove:

- timeout, rate-limit, provider, and runner failures remain inconclusive execution or runner findings with no projected verdict;
- an unavailable auto-mode checker creates no compatibility requirement;
- a non-applicable chat module on an image-only model emits explicit N/A requirements, no findings, and no coverage or verdict effect.

## Quality evidence

The delivery gate ran successfully on 2026-08-31:

```text
GOCACHE=/tmp/aiqa-go-cache make check
GOCACHE=/tmp/aiqa-go-cache go test -race ./...
```

The checks include:

- all package and mock-provider integration tests;
- `go vet` and formatting checks;
- deterministic Vibecode and Qwen semantic goldens;
- old v1 and additive actionable v1 Draft 2020-12 schema fixtures;
- schema validation of a marshaled actionable fixture and generated mock report;
- catalog and pack lint behavior;
- redaction and artifact deduplication;
- canonical assessment and exit-policy regression tests.

The `BenchmarkGuidance1000Requirements` result on the final delivery run was approximately 22.4 ms per 1,000 requirements, below the 100 ms target. Guidance performs no API calls and adds no token usage.

Human usability targets from the original product study—time to first action, evidence discovery time, and interpretation by five maintainers—were not measured by automated delivery tests. They remain a validation activity, not an implemented software capability.

## Deferred milestones

The following items were explicitly future milestones in the original PRD and are not implemented by this delivery:

### Milestone 2

- baseline comparison and `new`, `resolved`, `regressed`, and related states;
- offline `aiqa explain` rendering;
- targeted `aiqa verify` execution;
- provider-specific pack detection and non-generic packs;
- Russian guidance locale;
- nested requirement/probe profile representation for a future v2 contract.

### Milestone 3

- self-contained HTML reports;
- interactive filtering and comparison views;
- SARIF or CI issue export where a meaningful source mapping exists.

### Milestone 4

- opt-in generated explanation from sanitized findings.

Any future generated layer must remain non-normative, visibly marked, and unable to change IDs, evidence, status, priority, impact, verification commands, verdict, or exit policy.
