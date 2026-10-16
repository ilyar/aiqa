# Beta.1 preparation audit — 2026-10-16

Status: local preparation complete; **not an approval to publish**. The requested
Codex Security audit completed; its initial scan confirmed nine medium- and two
low-severity findings. All eleven were remediated and independently reviewed.
Codex Security revalidation confirmed closure, including focused follow-ups
that exposed and then confirmed repairs to Anthropic pagination and Google
inventory-error propagation. This is scoped static assurance, not a guarantee
that the software has no vulnerabilities.

## Scope and findings

- History: `bash script/secret-screen.sh` screened file contents in all 214
  commit trees reachable from locally available refs and the tracked/non-ignored
  candidate working files. No configured high-confidence credential signature
  matched. This excludes unfetched refs, reflogs, unreachable objects, ignored
  private files, and secrets outside the configured patterns.
- Local private provider files were present outside tracked history. Ignore
  rules now cover alternate private-environment filenames, credentials,
  execution logs, traces, and local reports. No credential values are included
  in this report. Ignoring a file does not remove an already tracked secret.
- The initial dependency scan found seven symbol-level vulnerabilities in
  `golang.org/x/net`, OpenTelemetry SDK, and gRPC. Updates to x/net 0.60.0,
  OpenTelemetry 1.45.0, gRPC 1.83.2, and compatible transitive versions resolved
  the reported symbol/package findings.
- The remaining module-level advisory, GO-2026-5932, concerns the unmaintained
  `golang.org/x/crypto/openpgp` package. `go mod why` confirms this application
  does not need that package. The final scan reports zero affected symbols and
  zero vulnerable imported packages; this is a static-analysis result, not a
  general claim of absence of vulnerabilities.
- The Windows build blocker in log following was removed by replacing
  Unix-specific file identity with `os.SameFile` and a per-session generation.
  Regression tests distinguish appends from replacement.
- The insecure-log test fixture now explicitly sets its intended permissions,
  so a restrictive process umask cannot accidentally turn it into a secure file.
- All official GitHub Actions in CI, mutation, release, and Pages workflows
  are pinned to commit SHAs resolved from their upstream repositories on
  2026-10-16. Version comments are retained for maintainability. This addresses
  the workflow review's mutable-action-reference finding.

## Security remediation

- API redirects are origin-bound and hop-limited; image and persistent creates
  are single-attempt operations, independent of URL prefixes.
- Artifact downloads reject non-global address ranges at validation and dialing
  and do not inherit ambient proxy settings.
- Native and SDK inventories have pre-read byte limits and bounded decoding.
  Selected models, pagination, concurrent workers, and physical API attempts are
  bounded. Zero evaluated target-local model scopes are not rankable successes.
  Google discovery is fail-closed: unsuccessful pagination supplies no model
  entries to planning; cumulative raw-entry limits include malformed/duplicate
  entries, and raw names are checked before normalization.
- SSE parsing bounds structure and retained data; incremental text and tool
  arguments use linear accumulation. Local work limits produce incomplete
  evidence instead of provider-failure assertions.
- Context accounting has a conservative local floor, separate request/byte
  allowances, and overflow-safe arithmetic. Retry-After is capped at 30 seconds.
- Model IDs reject oversized/control/bidi values; reports render provider text
  inertly. Configured secrets are redacted longest-first before trace truncation
  and serialization, including arbitrary structured log fields.
- Persistent-create ambiguity is reported as cleanup-unproven. Confirmed IDs
  get detached, finite cleanup with a separate small request allowance.
- The deferred overlapping-secret issue is fixed. Extension pathname replacement
  is **not technically eliminated**: the trusted-filesystem operating boundary
  and provenance limitation are explicit in [SECURITY.md](../SECURITY.md).

Regression tests include actual malformed HTTP/SSE responses, allocation bounds,
SDK oversized catalogs, remaining pagination budgets, response-loss on persistent
create, empty per-target scope, and structured/overlapping secret values.

## Local verification

- `make check`, `make race`, and `make security-check`: passed after the final
  Google fail-closed, Anthropic pagination, configurable SDK inventory-limit,
  and Unicode-output integration, with Go 1.27.2 and gRPC 1.83.2.
  Additional compact pagination and Unicode-output regressions passed with
  the race detector.
- Real HTTP Google regressions cover unsafe raw IDs, cumulative duplicate and
  malformed-entry counts, successful bounded pagination, and a failed later
  page. Failure retains only bounded inventory diagnostics: no model candidates,
  selected IDs, report model entries, or downstream model requests.
- `bash script/install_test.sh`: 44 offline checks passed, covering eight
  platform selections, checksums, failed downloads, preservation of an existing
  binary, paths with spaces, cleanup, and rejected inputs.
- ShellCheck and shell syntax checks passed for the release, installer, and
  secret-screen scripts. YAML parsing and `git diff --check` passed.
- The static page was rendered and visually inspected in Chrome at desktop
  and mobile widths. Public hosting and public download URLs are not verified.
- All eight binaries were built in `dist/1.0.0-beta.1`; SHA256SUMS verification
  passed for every file. Binary formats match each target, and build metadata
  confirms gRPC 1.83.2 in all eight. The Linux amd64 binary starts and prints
  beta.1. These final post-remediation local validation builds use commit label
  `securitycheck`, not a reviewed release tag. Earlier builds are preserved in
  sibling `-pre-security`, `-pre-sdk-bound`, `-pre-pagination-bound`, and
  `-pre-google-failclosed` directories. The final Google repair rebuild and all
  eight checksum/format/metadata checks passed.

## Separate follow-ups

- Some early discovery-error returns can leave telemetry lifecycle spans open;
  this does not allow rejected inventory into planning.
- Google model-list conformance diagnostics currently validate the first page's
  schema. Safety checks and cumulative limits cover subsequent pages, but this
  is not a claim of complete per-page schema-conformance coverage.

## Codex Security evidence

The user authorized execution of
  `@openai/codex-security@0.3.0`; CLI help and scan preflight succeeded. Preflight
  found stored credentials. The user subsequently authorized sending the
  256-file candidate snapshot to OpenAI Codex Security. The standard repository
  scan completed with stored ChatGPT credentials. The 256-file snapshot excluded
  ignored local data. Findings concern redirect credentials, artifact SSRF,
  streaming complexity, inventory work amplification, empty-inventory verdicts,
  image and persistent-create retries, context accounting, Markdown injection,
  Retry-After bounds, and exact-secret trace redaction. Full private results are
  retained in ignored local audit storage. Revalidation used sanitized immutable
  snapshots. The all-findings recheck confirmed the initial fixes; subsequent
  inventory rechecks identified residual paths that were repaired and tested.
  The final inventory assessment suppressed the finding with high static
  confidence against snapshot `9c62e620db8757507027e49c98ac5c96aa375ad0`.
  Final application sources match that snapshot; subsequent changes are tests
  and documentation. Revalidation inspected source without running code or
  tests. Runtime test evidence above was produced separately in the working tree.

## Outstanding publication gates

- Run native qualification on the other target systems. Cross-build success
  must be distinguished from native execution on every target. Rebuild official
  artifacts from the reviewed release tag using the workflow.
- Review the candidate commit, including pre-existing application changes.
- Decide repository publication, configure Pages, publish the reviewed release,
  and verify unauthenticated URLs and the exact public installation command.

The repository was private and had no Pages site when checked through GitHub's
API. No tag, commit, push, visibility change, or deployment was made by this
preparation work. See [releasing.md](releasing.md) for the publication procedure.
