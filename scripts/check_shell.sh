#!/usr/bin/env bash
# check_shell.sh — catches shell mistakes before merge (#205): the agents' tick,
# the deploy and the pre-commit secret guard are all bash.
#
#   scripts/check_shell.sh            every scripts/**/*.sh and .githooks/*
#   scripts/check_shell.sh <file>...  only these
#
# For each file: bash -n (syntax), then shellcheck -x at warning level (sources
# found beside each script: .shellcheckrc), and every disable directive must
# carry its reason on the same line ("disable=SC2034 # why"). CI and make check
# run it. Without shellcheck on PATH that step is skipped with a message —
# unless REQUIRE_SHELLCHECK=1 (CI), which fails instead. SHELLCHECK names the
# binary (tests). Bash 3.2-safe: macOS /bin/bash runs make check.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

files=()
if [[ $# -gt 0 ]]; then
  files=("$@")
else
  while IFS= read -r f; do files+=("$f"); done < <({ find scripts -type f -name '*.sh'; find .githooks -type f; } | sort)
fi
[[ ${#files[@]} -gt 0 ]] || { echo "check_shell: no script to check"; exit 1; }

status=0
for f in "${files[@]}"; do
  bash -n "$f" || { echo "check_shell: $f: syntax error (bash -n)"; status=1; }
done

# Every disable says why. /dev/null keeps grep printing the file name for one file.
bare="$(grep -nE '#[[:space:]]*shellcheck[[:space:]]+disable=' "${files[@]}" /dev/null |
  grep -vE 'disable=SC[0-9]+(,SC[0-9]+)*[[:space:]]+#[[:space:]]*[^[:space:]]' || true)"
if [[ -n "$bare" ]]; then
  echo "check_shell: a disable without its reason on the same line (disable=SCxxxx # why):"
  echo "$bare"
  status=1
fi

sc="${SHELLCHECK:-shellcheck}"
if command -v "$sc" >/dev/null 2>&1; then
  "$sc" -x --severity=warning --format=gcc "${files[@]}" || status=1
elif [[ "${REQUIRE_SHELLCHECK:-0}" == "1" ]]; then
  echo "check_shell: $sc is not installed and REQUIRE_SHELLCHECK=1"
  status=1
else
  echo "check_shell: $sc not installed — skipped (bash -n and disable reasons checked; CI runs shellcheck)"
fi

[[ $status -eq 0 ]] && echo "check_shell: ${#files[@]} script(s) ok"
exit $status
