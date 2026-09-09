# Verified project context

Status: adapted 2026-09-05 from the checked-in CI and manifests. Keep this
document short (target: under 150 lines). Revalidate after relevant CI,
manifest, or architecture changes.

## Purpose and architecture

- Purpose: test OpenAI-, Anthropic-, and Google GenAI-compatible provider
  behavior and produce a versioned JSON/Markdown compatibility report.
- Important modules: `internal/protocol/*` executes target-local probes;
  `internal/capability` selects model modules; `internal/assessment` derives
  canonical conformance; `internal/report` serializes and renders it.
- Critical invariants: exact inventory intersection is target-local; report
  verdict comes from canonical assessment, not guidance; rate-limit telemetry
  is transport-owned; live credentials and billable/persistent probes require
  explicit gates.

## Commands

Record the working directory, exact command, source (CI/manifest), required
environment, and whether execution has actually been observed. A discovered
command is a candidate until verified. Never invent a passing test result.

| Scope | Command | Source | Observed result |
| --- | --- | --- | --- |
| Full local check | `make check` | `.github/workflows/ci.yml` | executed in this task; uses the Go-only Draft 2020-12 checker; repeat on final integrated HEAD |
| Race suite | `make race` | `.github/workflows/ci.yml` | executed in this task; repeat on final integrated HEAD |
| Security gate | `make security-check` | `.github/workflows/ci.yml` | executed in this task; repeat on final integrated HEAD |
| Build | `make build VERSION=ci COMMIT=... DATE=ci` then `./bin/aiqa --version` | `.github/workflows/ci.yml` | CI-defined; pending final integrated HEAD |

## Skills and tools

Record only actually available, relevant skills. For each, record its exact
path/name, trigger and intended role. Load a skill only when its trigger applies.
Do not install MCP servers or assume credentials because a manifest mentions them.

## Decisions and pitfalls

Link to evidence or ADRs. Keep local task notes in `.orchestration/state/`.
Revalidate this file when the relevant manifests, CI or architecture change.
