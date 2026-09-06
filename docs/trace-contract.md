# Forensic trace contract

`--verbose=4` creates two private, `0600` artifacts before AIQA makes a
provider request:

- `<name>.log` — streamed JSONL records conforming to `aiqa.trace.v1`;
- `<name>.trace-verdict.json` — an offline structural assessment conforming to
  `aiqa.trace-verdict.v1`.

They are diagnostic artifacts, not part of `aiqa.report.v3`. A compatibility
PASS and a trace-verdict PASS make different claims: the first evaluates
provider evidence, while the second says the locally observed execution graph
is structurally reconstructable.

## Record safety and correlation

Each record has a schema version, UTC time, strictly increasing `seq`, level,
event name, `trace_id`, `span_id`, and `parent_span_id`. Lifecycle records also
have an `operation_id`. The logger uses an allowlist and drops unreviewed field
names as well as their values. It never records credentials, headers, URLs or
query strings, request/response bodies, prompts, generated output, tool
arguments, or media data.

The expected execution relationship is:

```text
run → target → provider/model → suite → named probe → logical request → attempt
```

Every span has one start and one end. A parent starts before and ends after its
children. Each HTTP attempt must be below both a logical request and a named
probe. Validator evidence belongs to the probe and must be emitted before its
end. The verifier reports stable violation codes such as
`http_attempt_without_probe`, `child_outlives_parent`,
`validator_after_probe_end`, and `sensitive_field_detected` when applicable.

## Offline verification

`internal/traceverify.Verify(io.Reader)` validates a saved JSONL stream without
network access, credentials, report payload, or provider response. The app runs
the same verifier after closing the sidecar and serializes its result in the
verdict file. Its statuses are:

| Status | Meaning |
| --- | --- |
| `pass` | Complete observed graph with no structural violation. |
| `fail` | A complete trace was read and violates the contract. |
| `inconclusive` | The trace was unreadable/incomplete or an executor could not provide required lifecycle evidence. |
| `not_requested` | Forensic tracing was not requested. |

`aiqa.ext.v2` executable extensions emit cooperative, safe `trace_event`
records while they execute. The host validates their local IDs, maps them to
host-owned spans, and requires `suite → probe → logical_request → attempt`
before executable HTTP coverage can receive `pass`. A process that emits only
terminal suite results remains `inconclusive` with `extension_trace_unavailable`.

OpenTelemetry propagation remains off by default. If configured, AIQA injects
context only for exact allowlisted origins and removes it on cross-origin
redirects.

## Alpha migration policy

This document describes `aiqa.trace.v1` and `aiqa.trace-verdict.v1`. A breaking
alpha change is permitted only when it changes the affected schema version,
updates this contract and all fixtures/verdict expectations, and adds a
migration note stating whether an adapter exists. AIQA does not interpret an
older trace as `aiqa.trace.v1` without an explicit compatibility adapter; no
adapter exists for pre-v1 diagnostic JSONL.

## Stable violation catalogue

The following codes are part of `aiqa.trace-verdict.v1`. New codes require a
fixture and an entry in the traceability table; removing or redefining one is a
trace-format migration.

| Code | Meaning |
| --- | --- |
| `malformed_json`, `trace_read_failed` | The saved sidecar cannot be fully read. |
| `trace_schema_version_invalid`, `mandatory_field_missing`, `seq_not_monotonic` | A record violates the base JSONL contract. |
| `run_root_invalid`, `span_id_missing`, `duplicate_span_start`, `duplicate_span_end`, `span_end_without_start`, `missing_span_end`, `span_operation_mismatch` | Lifecycle cardinality or identity is invalid. |
| `parent_missing`, `parent_order_invalid`, `child_outlives_parent`, `trace_mismatch`, `execution_hierarchy_invalid`, `target_scope_mismatch` | Parentage, ordering, trace identity, target isolation, or required execution hierarchy is invalid. |
| `http_attempt_without_logical_request`, `http_attempt_without_probe`, `http_attempt_multiple_probe_owners` | HTTP ownership is absent or ambiguous. |
| `validator_without_probe`, `validator_after_probe_end`, `derived_reference_invalid` | Validator evidence has no active owner or its derived-result reference is unsafe. |
| `selected_executor_unobserved`, `extension_trace_unavailable` | A requested executor did not produce the required lifecycle evidence. |
| `sensitive_field_detected` | An unallowlisted, credential-like, URL-like, or nested sensitive value reached the trace. |
| `trace_write_failed` | The forensic sidecar could not be written completely. |
