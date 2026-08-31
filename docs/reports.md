# Reports and actionable guidance

An AIQA report states which parts of an OpenAI-compatible API were demonstrated, which evidence remains incomplete, and what to do next. It measures observed behavior against a selected profile; it is not an OpenAI certification, security audit, or model-quality benchmark.

## Result model

AIQA keeps three layers separate:

1. **Conformance** derives canonical requirement outcomes and the authoritative verdict.
2. **Findings** group non-passing evidence into stable, deduplicated observations.
3. **Guidance** turns findings into prioritized actions with owners, confidence, verification, and references.

Guidance never changes a requirement status, verdict, legacy score, or exit code. If guidance generation fails, JSON records `guidance.status=error`, Markdown asks the operator to investigate AIQA, and the conformance result remains intact.

## Canonical requirements

A probe is an experiment; a requirement is the contract unit being assessed. Several probes may support one requirement without giving that requirement extra weight. Native and Official SDK observations retain separate evidence IDs.

Official SDK parsing is normally an explicit optional interoperability requirement linked to the related native wire requirement. Conflicting native and SDK observations therefore create an evidence diagnostic without rewriting a passing native core result. A suite that checks the same response object, such as the Models list schema, may contribute evidence directly to that requirement.

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

`INCONCLUSIVE` is not rankable. Other reports compare by verdict first, then core conformance, optional coverage, and evidence completeness. Never rank reports by the legacy `summary.score` field.

The v1 JSON contract retains the legacy `summary.status` and `summary.score` fields for existing consumers. The authoritative result is `summary.assessment.verdict`; consumers must not infer it from the legacy fields. The score is marked with:

```json
{
  "score_method": "legacy-pass-rate-v1",
  "score_deprecated": true
}
```

Markdown hides that value under **Legacy scoring**.

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

## Markdown layout

Markdown is rendered from the same report object serialized as JSON. Its order is:

1. run metadata and executive summary;
2. **Fix first**, compatibility improvements, investigations, and capability gaps;
3. model and module details;
4. collapsed passing checks;
5. deduplicated raw evidence;
6. methodology and limitations.

External checker logs are redacted, truncated, stored once in `guidance.artifacts`, and linked by content-addressed `artifact_ref` values. API keys, bearer tokens, cookies, signed query strings, tool arguments, and long base64 payloads are removed before rendering.

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

The additive `aiqa.report.v1` migration preserves existing fields while adding canonical requirements, assessment, and `aiqa.guidance.v1`. Profiles still store flat probe definitions; AIQA derives the requirement layer from stable requirement IDs. A future v2 profile may represent nested requirements and probes directly.

Baseline comparison, offline `aiqa explain`, targeted `aiqa verify`, Russian guidance, HTML, SARIF, and generated explanations are not implemented. They remain separate future work and must not be inferred from the v1 guidance contract.
