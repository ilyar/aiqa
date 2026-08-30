# Architecture

## Goals

`aiqa` separates protocol conformance, observable semantics, vendor extensions, and benchmarking. A server accepting an unknown JSON field must not be reported as supporting it.

## Packages

```text
cmd/aiqa          CLI and signal handling
internal/config   environment and flag parsing
internal/api      dependency-free HTTP, JSON, and SSE client
internal/profile  embedded versioned compatibility profile
internal/suites   native behavioral probes
internal/external optional adapters to third-party/official-SDK checkers
internal/result   stable report data model
internal/report   aggregation and atomic JSON/Markdown writers
internal/app      orchestration, model discovery, concurrency, exit policy
profiles          editable source profile
schemas           machine-readable report contract
```

## Probe levels

1. **Transport** – status, content type, timeout, SSE framing.
2. **Shape** – request/response fields follow the OpenAI contract.
3. **SDK** – an official SDK can issue and parse calls through the external checker.
4. **Semantic** – requested behavior is observed, for example `n=2` yields two choices.
5. **Validation canary** – a deliberately invalid value is rejected, detecting silent field dropping.
6. **Statistical evidence** – repeated generations compare distributions; never presented as certainty.

## Dependency policy

The native binary uses only the Go standard library so it remains easy to audit and deploy. Existing tools are process adapters rather than linked dependencies. This lets their official-SDK coverage evolve independently and prevents their release cycle from controlling the core binary.

## Profile policy

The JSON profile records source, exact upstream commit, revision, scope, required probes, optional probes, standard request parameters, and vendor extensions. The copy in `internal/profile` is embedded into the binary. `make profiles-check` rejects drift between the editable and embedded copies.

## Context probing

Context capacity is a boundary search, not a single giant request:

- exponential growth establishes a bracket;
- binary search narrows it;
- token budget caps total expense;
- a byte ceiling caps local memory/request size;
- exact provider-reported prompt tokens are preferred;
- estimates and confidence remain explicit in the report.

A 2xx request only establishes an accepted lower bound. It does not establish long-context reasoning quality.
