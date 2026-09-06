# Forensic trace traceability

This table tracks implementation evidence for
[PRD forensic trace verification](../tasks/prd-forensic-trace-verification.md).
`Pending` is intentionally not evidence of completion.

| Requirement | Source | Positive evidence | Negative evidence | Status |
| --- | --- | --- | --- | --- |
| FR-1 versioned trace/verdict contracts | `internal/logging/logging.go`, `schema/trace-v1.schema.json`, `schema/trace-verdict-v1.schema.json` | `internal/traceverify/verify_test.go` valid graph | invalid schema fixture | Complete |
| FR-2 mandatory correlation | `internal/logging/logging.go`, `internal/telemetry/telemetry.go`, `internal/traceverify/verify.go` | verbose-4 app loopbacks; lifecycle records require `operation_id` while unscoped application messages retain empty IDs | missing mandatory fields fixture | Complete |
| FR-3/4 span cardinality and nesting | `internal/traceverify/verify.go` | valid native graph and two-target interleaving fixture | duplicate end, missing end, parent-order and cross-target-parentage fixtures | Complete |
| FR-5 required run-to-attempt chain | `internal/traceverify/verify.go` | native/SDK/executable verifier-backed graphs | incomplete subject chain fixture | Complete |
| FR-7 validator lifecycle and derived linkage | `internal/traceverify/verify.go`, `internal/suites/common.go`, protocol composite runners | validators are emitted before owning `probe.end`; shared chat/image and Anthropic/Google composite results carry safe `derived_from` linkage | late-validator fixture; `TestRunTestsProbeRecordsDerivedResultLinkage` | Complete |
| FR-17 stable violation codes | `internal/traceverify/verify.go`, `docs/trace-contract.md` | typed verdict fixtures and documented violation catalogue/migration rule | unsafe `derived_from` yields `derived_reference_invalid` | Complete |
| FR-18 verdict privacy | `internal/traceverify/verify.go`, `schema/trace-verdict-v1.schema.json` | verdict serialization rejects copying absolute path and credential-like trace values | payload/path/credential fixture | Complete |
| FR-19 report versus trace verdict distinction | `docs/trace-contract.md`, `internal/report/multi.go`, `cmd/aiqa/main.go` | report Markdown/JSON forensic reference tests; `aiqa --help` states level-4 diagnostics do not alter compatibility verdict/exit policy | — | Complete |
| FR-20 alpha format migration discipline | schemas, fixtures, `docs/trace-contract.md` | schema/version checks and explicit pre-v1 migration policy | — | Complete |
| FR-21 traceability table | `docs/forensic-trace-traceability.md` | this table and the local verification record below | completion audit | Partial: the required final commit/PR evidence cannot exist until this working tree is committed |
| FR-8 composite outcome aggregation | `internal/protocol/trace.go` | `trace_outcome_test.go` | derived fail/error beats first pass | Complete |
| FR-9 native/SDK HTTP ownership | `internal/traceverify/verify.go`, `internal/api/client_test.go`, `internal/app/app_test.go` | verifier-backed native retry and SDK close-error loopbacks; verbose-4 built-in SDK verdict | orphan attempt and double-probe ownership fixtures | Complete for native and built-in SDK; executable is tracked separately |
| FR-10/11 privacy boundary | `internal/logging/logging.go`, `internal/traceverify/verify.go` | table-driven logger coverage for credentials, headers, bodies, prompts, output, tool arguments, media, URLs/query; verifier-backed SSE read-error loopback | sensitive-field and nested/unknown-field fixtures | Complete |
| FR-14 OTel propagation allowlist | `internal/config/config.go`, `internal/protocol/*/runner.go`, `internal/api/client.go` | exact configured-origin validation; exact-origin and cross-origin redirect tests | blocked redirect origin fixture | Complete |
| FR-15 offline verdict | `internal/traceverify/verify.go`, `internal/app/app.go` | dual-target app loopback byte-for-byte compares the run verdict with a second offline `Verify` of saved JSONL; levels 0–3 create no forensic verdict | malformed JSON fixture | Complete |
| FR-16 selected/not-selected executor coverage | `internal/app/app.go`, `internal/traceverify/verify.go` | OpenAI/Anthropic and Google verbose-4 app loopbacks produce target/protocol coverage; built-ins classify as SDK | selected executor without lifecycle yields `selected_executor_unobserved` | Complete |
| FR-13/16 executable extension coverage | `internal/extsuite/*`, `test/fixtures/ext-suite`, `internal/app/app_test.go` | v2 cooperative host-mapped graph and end-to-end PATH-discovered executable sidecar verdict | missing/unclosed/unsafe trace event fixtures; non-cooperative output is inconclusive | Complete for cooperative `aiqa.ext.v2` |
| FR-6 discovery/preflight hierarchy | `internal/protocol/*/runner.go`, `internal/app/anthropic_test.go`, `internal/app/google_test.go` | verbose-4 OpenAI/Anthropic multi-target and Google native+SDK loopbacks | incomplete hierarchy fixture yields `execution_hierarchy_invalid` | Complete for loopback protocol matrix |
| FR-12 SDK body-close lifecycle | `internal/api/client.go`, `internal/api/client_test.go` | verifier-backed SDK close-error graph | close-error fixture | Complete |
| FR-3 transport terminal paths | `internal/api/client.go`, `internal/api/client_test.go` | verifier-backed retry, cancellation, SDK close-error and SSE read-error loopbacks | malformed graph/late-validator fixtures in `traceverify` | Complete for covered native/SDK terminal paths; redirect remains under FR-14 |
| FR-22 sidecar write failure | `internal/app/app.go` | `TestForensicSidecarsExistWithPrivateModesBeforeFirstHTTPRequest` proves both 0600 files exist before first HTTP; injected streaming-write failure yields run error and `inconclusive/trace_write_failed` verdict | `TestRunMakesSidecarWriteFailureInconclusiveAndFailsRun` | Complete |
| Final gates | repository root | local verification record below | — | Passed on the uncommitted integrated working tree; final-commit/CI evidence pending |

## Local verification record

- Base revision: `22253b65fe70`; the forensic implementation remains an
  uncommitted working tree, so this is not final-commit or PR evidence.
- `make qa` — required local pre-commit gate: schema/profile validation, `go vet`, all Go tests and
  example-script tests.
- It also runs module/SBOM verification, `govulncheck`, build/version, race
  tests, and `git diff --check`; the last recorded integrated invocation passed.

The commands attached to implementation evidence must name the exact revision in
the PR/CI record. Passing a narrower package test does not close a Pending item.
