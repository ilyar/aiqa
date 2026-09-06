# Reports and actionable guidance

An AIQA report states which parts of an OpenAI, Anthropic, or Google GenAI API facade were demonstrated, which evidence remains incomplete, and what to do next. It measures observed behavior against a selected profile; it is not a provider certification, security audit, or model-quality benchmark.

## Report contracts

`aiqa.report.v3` represents one or more OpenAI, Anthropic, or Google GenAI targets. It is the only public report contract.

Each target owns these fields:

- target ID and protocol;
- profile and source revision;
- discovery inventory and selected models;
- canonical requirements and target verdict;
- findings, recommendations, usage, rate-limit telemetry, and grouped extension runs.
- optional notes, such as a pinned SDK surface limitation (Google currently has no public Interactions methods in v1.71.0).

The original model request is stored once as `config.requested_models`. Target
rows contain only models selected from that target's successfully retrieved
inventory; `targets[].models` is not a copy of the requested list. An ID that
is absent from one successful inventory is silently omitted there, and an ID
absent from every successful inventory creates no model row, requirement,
finding, or verdict effect. Raw inventory and discovery validation remain in
the affected target, so consumers can distinguish silent absence from a
discovery failure. The full routing contract is in
[`unified-model-routing.md`](unified-model-routing.md).

Model verdicts are derived from that model's canonical requirements. A missing planned native or profile-bound extension result cannot leave a model labelled `PASS`.

Requirement subjects and evidence IDs include the target ID. The same model name on OpenAI and Anthropic targets remains two separate subjects. The root contains a run verdict and target counts, but no weighted score or cross-protocol percentage. The hardest target outcome wins in this order: `FAIL`, `INCONCLUSIVE`, `PARTIAL`, `PASS-WITH-GAPS`, and `PASS`.

API keys are absent from the contract. The root `/summary` is a run-level verdict and must not replace `/targets/0/summary` in a consumer that needs target conformance details.

## Result model

AIQA keeps three layers separate:

1. **Conformance** derives canonical requirement outcomes and the authoritative verdict.
2. **Findings** group non-passing evidence into stable, deduplicated observations.
3. **Guidance** turns findings into prioritized actions with owners, confidence, verification, and references.

Guidance never changes a requirement status, conformance assessment, or exit code. If guidance generation fails, JSON records `guidance.status=error`, Markdown asks the operator to investigate AIQA, and the conformance result remains intact.

## Canonical requirements

A probe is an experiment; a requirement is the contract unit being assessed. Several probes may support one requirement without giving that requirement extra weight. Native and SDK observations retain separate evidence IDs.

SDK parsing is normally an explicit optional interoperability requirement linked to the related native wire requirement. `aiqa.profile.v3` supplies the binding; an extension cannot define its own requiredness or weight. Conflicting native and SDK observations create an evidence diagnostic without rewriting a passing native core result. Unbound extension results remain visible but do not enter conformance measures.

Each requirement has a level, applicability, status, subject, and evidence:

| Field | Values |
|---|---|
| Level | `core`, `optional` |
| Applicability | `applicable`, `not_applicable`, `unknown` |
| Status | `pass`, `fail`, `warn`, `unsupported`, `skip`, `error`, `not_applicable` |
| Subject | Provider endpoint or model ID |

The statuses have distinct meanings:

| JSON | Markdown | Meaning |
|---|---|---|
| `pass` | `PASS` | Expected behavior was demonstrated. |
| `fail` | `FAIL` | Executed behavior violated the applicable contract. |
| `warn` | `WARN` | The check ran, but correctness or semantic effect remains uncertain. |
| `unsupported` | `UNSUP` | An optional capability was explicitly rejected with a suitable client error. |
| `skip` | `SKIP` | An applicable check did not run. |
| `error` | `ERROR` | Provider, transport, rate-limit, or runner failure prevented a compatibility conclusion. |
| `not_applicable` | `N/A` | The requirement does not apply to this subject. |

`N/A` is excluded from all denominators. `ERROR` is inconclusive and never becomes an unsupported-capability claim. A required unsupported capability is normalized to a core failure.

## Verdict and measures

The authoritative verdict follows hard gates:

| Verdict | Meaning |
|---|---|
| `FAIL` | At least one applicable core requirement failed. |
| `INCONCLUSIVE` | Core evidence is uncertain, skipped, or errored. |
| `PARTIAL` | Core passed, but at least one optional requirement failed. |
| `PASS-WITH-GAPS` | Core passed; optional evidence contains only gaps, unsupported capabilities, or inconclusive results. |
| `PASS` | Every applicable core and optional requirement passed. |

Reports show three independent measures:

- **Core conformance**: passed core requirements divided by applicable core requirements.
- **Optional coverage**: passed optional requirements divided by applicable optional requirements.
- **Evidence completeness**: conclusive requirements divided by all applicable requirements; execution coverage is also preserved in JSON.

They also show requested module-route coverage (`expected`, `executed`, `unresolved`, and `excluded`). Unknown capability evidence in a requested core route is `INCONCLUSIVE`; an optional-only unknown route is `PASS-WITH-GAPS`. Inventory-only discovery may avoid model requests, but cannot claim generation coverage.

`INCONCLUSIVE` is not rankable. Other reports compare by verdict first, then core conformance, optional coverage, and evidence completeness. AIQA emits no overall numeric compatibility score. Raw status counts remain diagnostic, and percentages describe only their named core, optional, or evidence dimension.

## Findings and recommendations

A finding identifies what was observed, its scope, the likely component, the attribution basis, confidence, affected requirements, and evidence references. Stable fingerprints exclude timestamps, latency, request IDs, raw error prose, and volatile counts.

Recommendations use these priorities:

| Priority | Use |
|---|---|
| `P0` | A core failure or core evidence gap blocks the authoritative verdict. |
| `P1` | A confirmed optional defect or important evidence conflict needs prompt attention. |
| `P2` | An optional capability gap, configuration constraint, or documentation task. |
| `P3` | A low-confidence improvement, skipped optional check, or manual review. |

`action_kind` distinguishes `fix`, `configure`, `workaround`, `document`, `investigate`, `rerun`, and `manual_review`. An execution error produces `rerun` or `investigate`; conflicting evidence does not produce an automatic provider fix.

Guidance levels describe specificity:

| Level | Meaning |
|---:|---|
| 3 | The change location, desired change, and automated verification are known. |
| 2 | The target and desired behavior are known, but implementation depends on the provider architecture. |
| 1 | Only an investigation or manual-review direction is safe. |

Finding, attribution, and recommendation confidence are recorded separately as `high`, `medium`, or `low`. A projected verdict is conditional: it assumes only the affected requirements change to `pass` and nothing else regresses. Conflicting or inconclusive findings always use `projected_verdict: null`.

Verification entries identify the affected target, protocol, requirement IDs, and SDK suite IDs. AIQA does not emit shell commands: a full `test=compat` rerun does not verify one finding in isolation and would misrepresent its cost and scope. Targeted execution and opt-in command rendering are tracked in the [backlog](../tasks/backlog.md).

## Markdown layout

Markdown is rendered from the same report object serialized as JSON. Its order is:

Markdown lists every configured protocol service in the `Service` field. Each entry is host-only and linked to its service root; API paths, query parameters, credentials, and API keys are not included. The report then shows a target comparison table, run verdict, per-target telemetry, separate target sections, model routing, grouped extension suites, and normalized provider-reported usage. OpenAI `prompt_tokens`/`completion_tokens`, Anthropic `input_tokens`/`output_tokens`, and Google `promptTokenCount`/`candidatesTokenCount` share common Input and Output columns. Cache creation and cache read remain separate input categories; thinking/reasoning is shown as an output subset and is not added twice. The Total column and the multi-target total row show the observed aggregate. When a target evaluates more than one model, its model section also shows separate rate-limit and latency observations for each model. Percentages remain target-local.

Rate-limit telemetry is transport-owned. `logical_requests`, physical attempts,
retry attempts, recovered and exhausted retries, rate-limited responses,
backoff, and unique successful-response latency samples are not reconstructed
from the number of result rows. Safe request rate and concurrency are client
pacer observations and are omitted from JSON, or rendered as `N/S`, when no
request succeeded.

Failed extension-runner diagnostics are redacted, truncated, stored once in `guidance.artifacts`, and linked by content-addressed `artifact_ref` values. API keys, bearer tokens, cookies, signed query strings, tool arguments, and long base64 payloads are removed before rendering.

## CLI controls

Guidance is local and deterministic; it sends no additional provider requests and uses no LLM.

```text
--guidance=full|summary|off
--guidance-pack=auto|generic|<id>
--guidance-locale=en
```

Defaults are `full`, `auto`, and `en`. `summary` omits recommendation detail from Markdown but leaves structured JSON guidance intact. `off` disables findings and recommendations without changing probes or conformance.

The generic catalog contains reviewed cross-provider advice. Packs may add operational steps, workarounds, caveats, and references. Strict decoding prevents packs from redefining normative fields such as requirement level, status, priority policy, verdict, or impact.

## Exit policy

Reports are written before the policy is applied:

| `--fail-on` | Exit 1 when |
|---|---|
| `core` | Verdict is `FAIL` or `INCONCLUSIVE`. |
| `any` | Verdict is not `PASS`. |
| `never` | Never because of the conformance verdict. |

Exit `2` remains reserved for configuration, orchestration, or report-writing failures. Guidance status does not affect these codes.

## Current scope and future work

Report v3 contains canonical requirements and authoritative target assessments without an overall score. `aiqa.profile.v3` adds explicit extension requirements and bindings while keeping the profile a normative manifest rather than an HTTP request DSL.

Baseline comparison, offline `aiqa explain`, targeted `aiqa verify`, opt-in verification commands, Russian guidance, HTML, SARIF, and generated explanations are not implemented. They remain separate future work and must not be inferred from the guidance contract.
