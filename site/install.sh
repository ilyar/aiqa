#!/bin/sh
# AIQA installer. Design reference: Astral uv's standalone installer.
# Downloads a versioned binary, verifies SHA-256, then installs without sudo.
set -eu

main() {
    [ "$#" -le 1 ] || fail 'too many arguments'
    case "${1:-}" in
        --help|-h)
            printf '%s\n' 'Install AIQA for Linux, macOS, FreeBSD, or Windows (Git Bash).' \
                'Options: --help' \
                'Environment: AIQA_VERSION (default 1.0.0-beta.2), AIQA_INSTALL_DIR.' \
                'Default directory: XDG_BIN_HOME, or HOME/.local/bin. Shell profiles are not modified.'
            return ;;
        '') ;;
        *) fail "unknown option: $1" ;;
    esac
    version=${AIQA_VERSION:-1.0.0-beta.2}
    version=${version#v}
    case "$version" in ''|*[!0-9A-Za-z.-]*|.*|-*) fail 'invalid AIQA_VERSION' ;; esac
    case "$(uname -s)" in
        Linux) platform=linux ;;
        Darwin) platform=darwin ;;
        FreeBSD) platform=freebsd ;;
        MINGW*|MSYS*|CYGWIN*) platform=windows ;;
        *) fail 'unsupported operating system' ;;
    esac
    case "$(uname -m)" in
        x86_64|amd64) arch=amd64 ;;
        aarch64|arm64) arch=arm64 ;;
        *) fail 'unsupported architecture (requires amd64 or arm64)' ;;
    esac
    binary=aiqa
    [ "$platform" != windows ] || binary=aiqa.exe
    asset="aiqa-$platform-$arch"
    [ "$platform" != windows ] || asset="$asset.exe"
    destination=${AIQA_INSTALL_DIR:-${XDG_BIN_HOME:-${HOME:?HOME or AIQA_INSTALL_DIR must be set}/.local/bin}}
    case "$destination" in /*) ;; *) fail 'install directory must be absolute' ;; esac
    command -v curl >/dev/null 2>&1 || fail 'curl is required'
    if command -v sha256sum >/dev/null 2>&1; then
        hash_tool=sha256sum
    elif command -v shasum >/dev/null 2>&1; then
        hash_tool=shasum
    elif command -v sha256 >/dev/null 2>&1; then
        hash_tool=sha256
    else
        fail 'a SHA-256 utility is required (sha256sum, shasum, or sha256)'
    fi
    temporary=$(mktemp -d "${TMPDIR:-/tmp}/aiqa-install.XXXXXXXX")
    pending=
    trap 'cleanup' 0
    trap 'exit 1' HUP INT TERM
    base="https://github.com/ilyar/aiqa/releases/download/v$version"
    printf 'Downloading AIQA %s (%s/%s)\n' "$version" "$platform" "$arch"
    curl --proto '=https' --tlsv1.2 -LsSf "$base/$asset" -o "$temporary/$asset"
    curl --proto '=https' --tlsv1.2 -LsSf "$base/SHA256SUMS" -o "$temporary/SHA256SUMS"
    expected=$(awk -v name="$asset" '$2 == name {print $1}' "$temporary/SHA256SUMS")
    [ "${#expected}" -eq 64 ] || fail 'missing or ambiguous checksum'
    case "$expected" in *[!0-9a-f]*) fail 'invalid checksum' ;; esac
    case "$hash_tool" in
        sha256sum) actual=$(sha256sum "$temporary/$asset") ;;
        shasum) actual=$(shasum -a 256 "$temporary/$asset") ;;
        sha256) actual=$(sha256 -q "$temporary/$asset") ;;
    esac
    actual=${actual%% *}
    [ "$actual" = "$expected" ] || fail 'SHA-256 mismatch; installation aborted'
    mkdir -p "$destination"
    pending=$(mktemp "$destination/.aiqa-install.XXXXXXXX")
    cp "$temporary/$asset" "$pending"
    chmod 755 "$pending"
    [ ! -d "$destination/$binary" ] || fail 'destination is a directory'
    mv -f "$pending" "$destination/$binary"
    pending=
    printf 'Installed %s\n' "$destination/$binary"
    printf '%s\n' 'Add this directory to PATH if needed, then run: aiqa --version'
}

fail() { printf 'aiqa installer: %s\n' "$*" >&2; exit 1; }
cleanup() {
    [ -z "$pending" ] || rm -f "$pending"
    # Only the unique directory created by mktemp above is removed.
    rm -rf "$temporary"
}

# Keep the invocation last so a truncated download cannot run a partial installer.
main "$@"
