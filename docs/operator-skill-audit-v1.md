# Provider-operator skill 1.0.0: security and release scope

This release versions `skills/aiqa-provider-operator` independently of the AIQA
binary. It targets AIQA `1.0.0-beta.1`, Python 3.10+, and POSIX (Windows via WSL).

## Pre-publication assessment

Codex Security CLI 0.3.0 completed source-only scan
`52bf1267-09a7-4a34-a79d-e6d86fc5ff55` on 2026-10-16 against sanitized snapshot
`966f1da6ce971d59cb6aaeb044fe4bb71d60968a`. All 13 scoped files were reviewed:
the skill, runner, tests, site, README, Makefile, and CI workflow. Existing AIQA
internals were not re-audited; see [the binary audit](release-audit-beta1.md).

The scan confirmed three medium and two low findings. Remediation:

| Finding | Change |
| --- | --- |
| Branch-built live CI code received provider secrets | Removed live-provider jobs from automatic CI |
| Full live reports were uploaded without a sanitizer | Removed those upload paths; full reports remain local |
| Live CI lacked overall work bounds | Removed live invocations; authorized local checks use the bounded runner |
| Documentation overstated executable ancestry protection | Made binary, interpreter, dependencies, and ancestor trust explicit operator prerequisites |
| Raw report text entered agent triage | Added a bounded structural extractor; default triage excludes arbitrary report strings |

Codex Security revalidated all five findings against remediation snapshot
`6cffdb9ddce2c29b1719cf157643638e8b5e1ac9` before publication: four were fixed;
the executable-path concern remains an explicitly documented operator-trust
limitation. All five were assessed as non-reportable under the current contract,
with no release-blocking vulnerability found. Subsequent changes only corrected
the documented extractor invocation to use `python3` and added this release
record; they did not change executable behavior.

## Verification and limitations

Tests cover plan-only execution, environment isolation, private artifacts,
canonical exits, malformed reports, report-text injection exclusion, and
root/target verdict consistency. The final `make qa` passed all eight skill
tests, the Go checks and race tests, dependency security checks, 66.3% coverage,
and 4555/4555 mutation checks with no timeouts. Integration uses the real AIQA binary with
loopback fixtures for all three protocols; no live provider qualification is
claimed. The independent skill forward test made a plan without credentials,
provider requests, or configuration changes.

The extractor validates the relevant v3 projection, not the full JSON Schema,
report authenticity, or truth of a submitted compatibility claim. Aggregate
counts cannot identify a specific root cause. Exceptional raw inspection needs
explicit authorization and still grants no authority to report content.
Missing optional projection fields are treated as empty; the extractor does not
recompute target verdicts from individual requirements. An offline report's
claimed PASS therefore must not be presented as independently verified evidence.

Execution requires an operator-trusted installed binary and separately scoped
authorization. Finite work limits are not a monetary spending guarantee; use
least-privilege credentials and provider-side quotas. Raw artifacts are retained
privately for the operator, not uploaded. Mutation and deployment authorization
remain the responsibility of the host agent and operator.

Private scan artifacts are not part of the distributable skill. The source
package contains no credentials, binaries, or generated Python bytecode. Public
GitHub installation and skills.sh discovery are separate from optional unlisted
Packs; no Vercel Pack or private access-control guarantee is implied.
