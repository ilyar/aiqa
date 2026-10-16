#!/usr/bin/env bash
set -euo pipefail

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
installer="$repo_root/site/install.sh"
test_root=$(mktemp -d "${TMPDIR:-/tmp}/aiqa install test.XXXXXXXX")
trap 'rm -rf "$test_root"' EXIT

fake_bin="$test_root/fake bin"
downloads="$test_root/download fixtures"
case_tmp="$test_root/installer tmp"
mkdir -p "$fake_bin" "$downloads" "$case_tmp"

cat >"$fake_bin/uname" <<'EOF'
#!/bin/sh
case "${1:-}" in
    -s) printf '%s\n' "${FAKE_UNAME_S:?}" ;;
    -m) printf '%s\n' "${FAKE_UNAME_M:?}" ;;
    *) exit 2 ;;
esac
EOF

cat >"$fake_bin/curl" <<'EOF'
#!/bin/sh
set -eu
output=
url=
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o) output=$2; shift 2 ;;
        http://*|https://*) url=$1; shift ;;
        *) shift ;;
    esac
done
[ -n "$output" ] && [ -n "$url" ] || exit 2
name=${url##*/}
case "${FAKE_CURL_FAIL:-}" in
    all|"$name") exit 22 ;;
esac
cp "$FAKE_DOWNLOAD_DIR/$name" "$output"
EOF
chmod 755 "$fake_bin/uname" "$fake_bin/curl"

failures=0
checks=0

record_failure() {
    printf 'not ok %s - %s\n' "$checks" "$1" >&2
    failures=$((failures + 1))
}

assert_eq() {
    local want=$1 got=$2 label=$3
    checks=$((checks + 1))
    if [[ "$got" == "$want" ]]; then
        printf 'ok %s - %s\n' "$checks" "$label"
    else
        record_failure "$label (got unexpected value)"
    fi
}

assert_contains() {
    local file=$1 text=$2 label=$3
    checks=$((checks + 1))
    if grep -Fq -- "$text" "$file"; then
        printf 'ok %s - %s\n' "$checks" "$label"
    else
        record_failure "$label"
    fi
}

assert_absent() {
    local path=$1 label=$2
    checks=$((checks + 1))
    if [[ ! -e "$path" ]]; then
        printf 'ok %s - %s\n' "$checks" "$label"
    else
        record_failure "$label"
    fi
}

assert_executable() {
    local path=$1 label=$2
    checks=$((checks + 1))
    if [[ -f "$path" && -x "$path" ]]; then
        printf 'ok %s - %s\n' "$checks" "$label"
    else
        record_failure "$label"
    fi
}

assert_no_install_temps() {
    local destination=$1 label=$2 found
    found=$(find "$case_tmp" -mindepth 1 -maxdepth 1 -name 'aiqa-install.*' -print -quit)
    if [[ -d "$destination" ]]; then
        found+=$(find "$destination" -mindepth 1 -maxdepth 1 -name '.aiqa-install.*' -print -quit)
    fi
    assert_eq '' "$found" "$label"
}

reset_case() {
    rm -rf "$downloads" "$case_tmp"
    mkdir -p "$downloads" "$case_tmp"
}

write_fixture() {
    local asset=$1 payload=$2 digest
    printf '%s' "$payload" >"$downloads/$asset"
    digest=$(sha256sum "$downloads/$asset")
    digest=${digest%% *}
    printf '%s  %s\n' "$digest" "$asset" >"$downloads/SHA256SUMS"
}

run_installer() {
    local os=$1 arch=$2 destination=$3 stdout=$4 stderr=$5
    shift 5
    env -i \
        PATH="$fake_bin:/usr/bin:/bin" \
        HOME="$test_root/home" \
        TMPDIR="$case_tmp" \
        FAKE_UNAME_S="$os" \
        FAKE_UNAME_M="$arch" \
        FAKE_DOWNLOAD_DIR="$downloads" \
        AIQA_VERSION="${TEST_VERSION:-1.0.0-beta.2}" \
        AIQA_INSTALL_DIR="$destination" \
        FAKE_CURL_FAIL="${TEST_CURL_FAIL:-}" \
        /bin/sh "$installer" "$@" >"$stdout" 2>"$stderr"
}

expect_failure() {
    if "$@"; then
        return 1
    fi
}

test_success_matrix() {
    local os arch platform mapped_arch suffix_marker suffix asset destination installed payload stdout stderr
    while read -r os arch platform mapped_arch suffix_marker; do
        reset_case
        suffix=
        [[ "$suffix_marker" == exe ]] && suffix=.exe
        asset="aiqa-$platform-$mapped_arch$suffix"
        payload="fixture:$asset"
        write_fixture "$asset" "$payload"
        destination="$test_root/install $platform $mapped_arch/bin"
        installed="$destination/aiqa$suffix"
        stdout="$test_root/$platform-$mapped_arch.out"
        stderr="$test_root/$platform-$mapped_arch.err"
        if ! run_installer "$os" "$arch" "$destination" "$stdout" "$stderr"; then
            checks=$((checks + 1))
            record_failure "install succeeds for $platform/$mapped_arch"
            continue
        fi
        assert_eq "$payload" "$(<"$installed")" "correct asset installed for $platform/$mapped_arch"
        assert_executable "$installed" "installed binary executable for $platform/$mapped_arch"
        assert_no_install_temps "$destination" "temporary files cleaned for $platform/$mapped_arch"
    done <<'EOF'
Linux x86_64 linux amd64 none
Linux aarch64 linux arm64 none
Darwin x86_64 darwin amd64 none
Darwin arm64 darwin arm64 none
FreeBSD amd64 freebsd amd64 none
FreeBSD aarch64 freebsd arm64 none
MINGW64_NT-10.0 x86_64 windows amd64 exe
MSYS_NT-10.0 arm64 windows arm64 exe
EOF
}

test_checksum_failures() {
    local destination="$test_root/checksum destination/bin" stdout="$test_root/checksum.out" stderr="$test_root/checksum.err" digest
    reset_case
    write_fixture aiqa-linux-amd64 original
    mkdir -p "$destination"
    printf '%s' existing >"$destination/aiqa"
    printf '%s' tampered >"$downloads/aiqa-linux-amd64"
    if ! expect_failure run_installer Linux x86_64 "$destination" "$stdout" "$stderr"; then
        checks=$((checks + 1)); record_failure 'checksum mismatch is rejected'
    else
        checks=$((checks + 1)); printf 'ok %s - checksum mismatch is rejected\n' "$checks"
    fi
    assert_eq existing "$(<"$destination/aiqa")" 'checksum mismatch preserves existing binary'
    assert_contains "$stderr" 'SHA-256 mismatch' 'checksum mismatch reports reason'
    assert_no_install_temps "$destination" 'checksum mismatch cleans temporary files'

    reset_case
    write_fixture aiqa-linux-amd64 payload
    printf '%s  %s\n' "$(printf '0%.0s' {1..64})" another-asset >"$downloads/SHA256SUMS"
    expect_failure run_installer Linux x86_64 "$destination" "$stdout" "$stderr" || {
        checks=$((checks + 1)); record_failure 'missing checksum is rejected'
    }
    assert_contains "$stderr" 'missing or ambiguous checksum' 'missing checksum reports reason'

    reset_case
    write_fixture aiqa-linux-amd64 payload
    digest=$(sha256sum "$downloads/aiqa-linux-amd64"); digest=${digest%% *}
    printf '%s  %s\n%s  %s\n' "$digest" aiqa-linux-amd64 "$digest" aiqa-linux-amd64 >"$downloads/SHA256SUMS"
    expect_failure run_installer Linux x86_64 "$destination" "$stdout" "$stderr" || {
        checks=$((checks + 1)); record_failure 'duplicate checksum is rejected'
    }
    assert_contains "$stderr" 'missing or ambiguous checksum' 'duplicate checksum reports reason'
}

test_download_failure() {
    local destination="$test_root/download failure/bin" stdout="$test_root/download.out" stderr="$test_root/download.err"
    reset_case
    write_fixture aiqa-linux-amd64 payload
    TEST_CURL_FAIL=aiqa-linux-amd64
    export TEST_CURL_FAIL
    if ! expect_failure run_installer Linux x86_64 "$destination" "$stdout" "$stderr"; then
        checks=$((checks + 1)); record_failure 'download failure is rejected'
    else
        checks=$((checks + 1)); printf 'ok %s - download failure is rejected\n' "$checks"
    fi
    unset TEST_CURL_FAIL
    assert_absent "$destination/aiqa" 'download failure does not install binary'
    assert_no_install_temps "$destination" 'download failure cleans temporary files'
}

test_platform_rejections() {
    local destination="$test_root/unsupported/bin" stdout="$test_root/unsupported.out" stderr="$test_root/unsupported.err"
    reset_case
    if ! expect_failure run_installer Plan9 x86_64 "$destination" "$stdout" "$stderr"; then
        checks=$((checks + 1)); record_failure 'unsupported OS is rejected'
    else
        checks=$((checks + 1)); printf 'ok %s - unsupported OS is rejected\n' "$checks"
    fi
    assert_contains "$stderr" 'unsupported operating system' 'unsupported OS reports reason'
    if ! expect_failure run_installer Linux riscv64 "$destination" "$stdout" "$stderr"; then
        checks=$((checks + 1)); record_failure 'unsupported architecture is rejected'
    else
        checks=$((checks + 1)); printf 'ok %s - unsupported architecture is rejected\n' "$checks"
    fi
    assert_contains "$stderr" 'unsupported architecture' 'unsupported architecture reports reason'
}

test_options() {
    local stdout="$test_root/options.out" stderr="$test_root/options.err" destination="$test_root/options install/bin"
    if ! env -i PATH=/usr/bin:/bin /bin/sh "$installer" --help >"$stdout" 2>"$stderr"; then
        checks=$((checks + 1)); record_failure '--help succeeds without probing the host'
    else
        checks=$((checks + 1)); printf 'ok %s - --help succeeds without probing the host\n' "$checks"
    fi
    assert_contains "$stdout" 'AIQA_VERSION' '--help documents environment options'
    expect_failure env -i PATH=/usr/bin:/bin /bin/sh "$installer" --unknown >"$stdout" 2>"$stderr" || {
        checks=$((checks + 1)); record_failure 'unknown option is rejected'
    }
    assert_contains "$stderr" 'unknown option' 'unknown option reports reason'
    expect_failure env -i PATH=/usr/bin:/bin /bin/sh "$installer" --help extra >"$stdout" 2>"$stderr" || {
        checks=$((checks + 1)); record_failure 'extra arguments are rejected'
    }

    reset_case
    write_fixture aiqa-linux-amd64 payload
    TEST_VERSION='bad/version'
    export TEST_VERSION
    expect_failure run_installer Linux x86_64 "$destination" "$stdout" "$stderr" || {
        checks=$((checks + 1)); record_failure 'invalid version is rejected'
    }
    unset TEST_VERSION
    assert_contains "$stderr" 'invalid AIQA_VERSION' 'invalid version reports reason'

    expect_failure run_installer Linux x86_64 relative/path "$stdout" "$stderr" || {
        checks=$((checks + 1)); record_failure 'relative install directory is rejected'
    }
    assert_contains "$stderr" 'install directory must be absolute' 'relative install directory reports reason'

    reset_case
    write_fixture aiqa-linux-amd64 version-prefix
    TEST_VERSION='v1.0.0-beta.1'
    export TEST_VERSION
    if ! run_installer Linux x86_64 "$destination" "$stdout" "$stderr"; then
        checks=$((checks + 1)); record_failure 'leading v in AIQA_VERSION is accepted'
    else
        checks=$((checks + 1)); printf 'ok %s - leading v in AIQA_VERSION is accepted\n' "$checks"
    fi
    unset TEST_VERSION
    assert_eq version-prefix "$(<"$destination/aiqa")" 'version option selects expected release asset'
}

test_success_matrix
test_checksum_failures
test_download_failure
test_platform_rejections
test_options

if ((failures != 0)); then
    printf '%s of %s checks failed\n' "$failures" "$checks" >&2
    exit 1
fi
printf 'all %s installer checks passed\n' "$checks"
