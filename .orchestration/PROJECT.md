# Verified project context

Status: not yet adapted. Keep this document short (target: under 150 lines).
On the first substantive task, validate the observations in
`project-observed.json` against source files and CI, then update this document.
For routine commit/status tasks, do not bootstrap the entire project.

## Purpose and architecture

- Purpose: unknown
- Important modules and ownership: unknown
- Critical invariants and sensitive paths: unknown

## Commands

Record the working directory, exact command, source (CI/manifest), required
environment, and whether execution has actually been observed. A discovered
command is a candidate until verified. Never invent a passing test result.

| Scope | Command | Source | Observed result |
| --- | --- | --- | --- |
| — | — | — | not verified |

## Skills and tools

Record only actually available, relevant skills. For each, record its exact
path/name, trigger and intended role. Load a skill only when its trigger applies.
Do not install MCP servers or assume credentials because a manifest mentions them.

## Decisions and pitfalls

Link to evidence or ADRs. Keep local task notes in `.orchestration/state/`.
Revalidate this file when the relevant manifests, CI or architecture change.
