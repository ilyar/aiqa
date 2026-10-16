# Publishing 1.0.0-beta.1

The release consists of eight standalone binaries (Linux, macOS, Windows, and
FreeBSD, each on amd64 and arm64), `SHA256SUMS`, and the license. Profiles and
guidance are embedded. No working-directory assets are required for normal use.
Build the release on Linux with Bash and GNU coreutils, as the release workflow
does; the output binaries target all four operating systems.

## Before tagging

1. Review the full candidate diff, including any pre-existing application work.
   Do not stage local provider credentials, reports, or trace files.
2. Scan all reachable Git history and the candidate working tree for secrets.
   If a real credential is found in history, revoke it before considering a
   history rewrite. Rewriting published history requires explicit coordination.
3. Run the requested Codex Security audit and resolve or document every finding.
   Dependency checks and regex secret screening do not replace that audit.
4. Run `make check`, `make race`, `make security-check`, and
   `bash script/install_test.sh`. Run `make qa` before committing, as required
   by the repository's local gate.
5. Confirm `Makefile`, `cmd/aiqa/main.go`, `CHANGELOG.md`, and `site/` all identify
   the same release. Build with `make dist VERSION=1.0.0-beta.1`; existing output
   must be preserved or moved before rebuilding into the same destination.
6. Verify the eight checksums and run native smoke tests on supported systems.
   Cross-compilation proves build support, not native runtime correctness.

## GitHub release

The repository is currently private. Public distribution also requires an
explicit decision to change its visibility after the history/security review.
Verify the resulting access without an authenticated session; a private draft
release is not a public download source.

The release workflow builds the tagged commit and prepares a draft prerelease.
Review its results and asset inventory before making it public. Do not replace
assets in an already published release; issue a new version for corrections.

Use tag `v1.0.0-beta.1` only after the candidate passes review. This preparation
does not itself create a tag, push changes, or publish a release.

## GitHub Pages

Select **GitHub Actions** as the repository's Pages source. The Pages workflow
deploys only `site/` to `https://ilyar.github.io/aiqa/`; private working-tree
files and the rest of the repository are excluded from the deployment artifact.

Publish the release before announcing the website's download links. Confirm
the page and `https://ilyar.github.io/aiqa/install.sh` return successfully, then
test the exact public command in a clean environment:

```sh
curl -LsSf https://ilyar.github.io/aiqa/install.sh | sh
```

The installer pins beta.1 because GitHub's latest-release endpoint does not
select prereleases. Future versions require updating the default installer
version and direct download links together.

The release workflow smoke-tests its Linux, macOS, and Windows runner hosts.
The remaining architectures and FreeBSD are cross-compiled; native runtime
qualification must be recorded separately before claiming that coverage.
