#!/usr/bin/env bash
# Build the signed-off release payload without replacing an existing release.
# This is intentionally run on the Linux CI builder: it requires GNU sha256sum
# and GNU mv's -T option while producing binaries for all supported targets.
set -euo pipefail

: "${VERSION:?VERSION is required}"
: "${COMMIT:?COMMIT is required}"
: "${DATE:?DATE is required}"

case "$VERSION" in
  *[!0-9A-Za-z.+-]* | '')
    printf 'invalid VERSION: %s\n' "$VERSION" >&2
    exit 2
    ;;
esac

root_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root_dir"
dist_dir="$root_dir/dist"
release_dir="$dist_dir/$VERSION"

if [[ -e "$release_dir" ]]; then
  printf 'release directory already exists: %s\n' "$release_dir" >&2
  exit 1
fi

mkdir -p "$dist_dir"
staging_dir=$(mktemp -d "$dist_dir/.${VERSION}.XXXXXX")
trap 'rm -rf "$staging_dir"' EXIT

ldflags="-s -w -X main.version=$VERSION -X main.commit=$COMMIT -X main.date=$DATE"
platforms=(
  linux/amd64
  linux/arm64
  darwin/amd64
  darwin/arm64
  windows/amd64
  windows/arm64
  freebsd/amd64
  freebsd/arm64
)

for platform in "${platforms[@]}"; do
  IFS=/ read -r goos goarch <<<"$platform"
  extension=''
  if [[ "$goos" == windows ]]; then
    extension='.exe'
  fi
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath \
    -ldflags "$ldflags" -o "$staging_dir/aiqa-$goos-$goarch$extension" ./cmd/aiqa
done

(
  cd "$staging_dir"
  sha256sum aiqa-* > SHA256SUMS
)
cp "$root_dir/LICENSE" "$staging_dir/LICENSE"

# -T refuses an occupied destination rather than moving the staging directory
# beneath it if another release process wins the race after the first check.
mv -T "$staging_dir" "$release_dir"
trap - EXIT

printf 'release artifacts written to %s\n' "$release_dir"
