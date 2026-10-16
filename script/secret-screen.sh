#!/usr/bin/env bash
# Baseline secret screening; prints paths and revisions, never matching values.
# This complements (and does not replace) a dedicated secret scanner and review.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

pattern='AKIA[0-9A-Z]{16}|ASIA[0-9A-Z]{16}|sk-[A-Za-z0-9_-]{20,}|sk-ant-[A-Za-z0-9_-]{20,}|AIza[0-9A-Za-z_-]{30,}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|xox[baprs]-[A-Za-z0-9-]{20,}|-----BEGIN (RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----'
found=0
count=0
while IFS= read -r revision; do
    count=$((count + 1))
    if git grep -I -l -E "$pattern" "$revision" -- .; then
        found=1
    else
        status=$?
        if [[ "$status" -ne 1 ]]; then exit "$status"; fi
    fi
done < <(git rev-list --all)

printf 'Screened file contents in %s reachable commit trees.\n' "$count"
while IFS= read -r -d '' file; do
    [[ -f "$file" ]] || continue
    if grep -I -l -E "$pattern" -- "$file"; then
        found=1
    else
        status=$?
        if [[ "$status" -ne 1 ]]; then exit "$status"; fi
    fi
done < <(git ls-files -z --cached --others --exclude-standard)
printf '%s\n' 'Screened tracked and non-ignored untracked working files.'
printf '%s\n' 'Excluded: ignored local files, unfetched refs, reflogs, unreachable objects, unknown credential formats.'
if [[ "$found" -ne 0 ]]; then
    printf '%s\n' 'Potential secrets found; review the listed paths privately.' >&2
    exit 1
fi
printf '%s\n' 'No configured high-confidence secret signature matched.'
