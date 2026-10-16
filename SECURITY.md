# Security

AIQA sends requests to configured API providers. Use test credentials with the
minimum necessary permissions and review the selected checks before running
them. Some checks consume tokens or create provider resources; their explicit
permission gates are not a substitute for account spending limits.

Provider-controlled inventories and streams have local safety limits. The
default inventory allowance is 4,096 catalog entries, 16 MiB of response bodies,
and 64 selected models per target. Adjust these deliberately with
`--max-catalog-models`, `--inventory-byte-budget`, and `--max-selected-models`;
use `--model` to narrow selection. A run also has a 10,000 physical API-attempt
ceiling; persistent-resource cleanup receives a separate small allowance.
Context probing has per-model request and cumulative payload limits, and its
token accounting cannot be reduced below a local estimate by provider telemetry.
These are work limits, not a guarantee of monetary spend or tokenizer accuracy.

Untrusted `Retry-After` delays are capped at 30 seconds. The overall run timeout
remains opt-in: set `--run-timeout` when a hard wall-clock bound is required.
Per-request `--timeout` and local work limits do not replace that bound.

Treat executable extension suites as trusted local programs. They receive the
credentials necessary to test their target. Do not run extensions from an
untrusted source. Keep the executable, its resolved symlink target, and every
ancestor directory writable only by trusted administrators or your own trusted
account. This requirement also applies to scripts' interpreters and dependencies.
Do not run extensions from shared writable directories or replace/update them
while a run is in progress.

The extension SHA-256 recorded in provenance describes the file read during
discovery; it is not an execution-time integrity guarantee. Execution later uses
the resolved pathname. An actor able to replace that file or an ancestor between
discovery and execution can change what runs. Extension execution is therefore
not a sandbox or a security boundary against local file writers.

Logs, forensic traces, provider inventories, and reports may contain sensitive
provider responses. Review artifacts before sharing, even when redaction is
enabled. Never commit credentials or private environment files.

## Reporting a vulnerability

Use GitHub's **Security → Report a vulnerability** for this repository if private
reporting is enabled. If it is unavailable, ask the maintainer for a private
reporting channel without posting exploit details, credentials, or private
reports in a public issue. Include the affected version, reproduction steps,
impact, and a minimal sanitized example in the private report.

The project is in beta. Security fixes target the latest published beta; there
is no commitment to backport fixes to older alpha builds.

## Release checks

Run `make check`, `make race`, and `make security-check` before releasing.
`security-check` verifies dependency integrity, runs Go vulnerability analysis,
and validates an SBOM. These checks complement source review and history secret
scanning; none alone establishes that a release is free of vulnerabilities.

Published binaries have SHA-256 checksums in `SHA256SUMS`. Checksums detect
corruption or a mismatched download; they do not provide an independent
signature if the release account itself is compromised.
