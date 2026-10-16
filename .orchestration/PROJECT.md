# Verified project context

Status: revalidated 2026-10-16 against CI and the beta release preparation. Keep this
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
- Security invariants: inventory limits apply before reading/decoding, including
  SDK paths; zero target-local evaluated model scope is non-rankable; local
  `api.ErrWorkLimit` is incomplete execution evidence, never provider failure.
  Single-attempt operation metadata controls persistent/image replay safety.
  Google pagination accounts raw entries cumulatively and validates raw names
  before normalization; unsuccessful retrieval never supplies partial models.

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
| Installer | `bash script/install_test.sh` | CI/release workflows | 44 offline checks passed 2026-10-16 |
| Secret screen | `bash script/secret-screen.sh` | CI/release workflows | 214 reachable commit trees + candidate worktree passed; limited signatures, not a Codex Security audit |

The local Go toolchain is `/usr/local/go/bin/go` (1.27.2). In restricted
execution use `PATH=/usr/local/go/bin:$PATH GOCACHE=/tmp/aiqa-release-go-cache`.
Tests require local HTTP sockets; dependency security checks require network.
`make check race security-check` passed on the beta preparation with gRPC 1.83.2.
Release builds use Linux/Bash/GNU coreutils and preserve existing dist versions.
Final post-remediation `make check`, `make race`, and `make security-check`
passed. Eight beta.1 cross-builds and their checksums passed; Linux amd64 version
smoke passed (`securitycheck` metadata, not a release tag). Other platforms have
build evidence, not local native runtime evidence.
Codex Security's eleven initial findings were remediated and revalidated;
final focused inventory source assessment confirmed closure on snapshot
`9c62e620`. See `docs/release-audit-beta1.md` for scope and remaining limitations.

## Skills and tools

Record only actually available, relevant skills. For each, record its exact
path/name, trigger and intended role. Load a skill only when its trigger applies.
Do not install MCP servers or assume credentials because a manifest mentions them.

## Decisions and pitfalls

Link to evidence or ADRs. Keep local task notes in `.orchestration/state/`.
Revalidate this file when the relevant manifests, CI or architecture change.
