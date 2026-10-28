#!/usr/bin/env bash
# go_tests.sh — the Go test suite in two groups (#429), so CI runs them in
# parallel and only the code that runs goroutines pays for the race detector.
#
#   scripts/go_tests.sh core    [PROFILE]   every test except the reports, -race
#   scripts/go_tests.sh reports [PROFILE]   the report and research tests, no -race
#   scripts/go_tests.sh all     [PROFILE]   both, one after the other, profiles merged
#   scripts/go_tests.sh merge OUT IN...     join coverage profiles (same mode)
#
# The reports are single-goroutine calculations over recorded or synthetic
# books (evidence, direction scorecard, replay, compare, the USDC study…):
# -race finds nothing in them and made the whole suite 10–14 min on CI. The
# nightly flaky-test hunt (flaky.sh) still runs every test with -race -count=5.
# The same regex is given to -run (reports) and -skip (core), so every test
# runs in exactly one group. A PROFILE is written with -covermode=atomic over
# ./internal/... (the mode -race forces), so the two merge for check_cover.sh.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

# Top-level test names (prefixes) of the report group. A new slow,
# single-goroutine test belongs here; anything with goroutines stays in core.
REPORT_TESTS='^(TestEvidence|TestDirectionScorecard|TestMarketReplay|TestCompare|TestUsabilityKit|TestUSDCStudy|TestEquityBridge|TestShellScripts|TestExchangeCoupling|TestTrackRecord|TestTrend|TestReplay|TestStress|TestBacktest|TestLadder|TestIdleMargin|TestDeployEffect|TestDataCoverage|TestUsage|TestCheckCover)'

run() { # run <group> <profile|"">
  local group="$1" profile="${2:-}" cover=()
  [[ -z "$profile" ]] || cover=(-covermode=atomic -coverpkg=./internal/... -coverprofile="$profile")
  case "$group" in
    core) go test ./tests/... -race -count=1 -timeout 30m -skip "$REPORT_TESTS" "${cover[@]}" ;;
    reports) go test ./tests/... -count=1 -timeout 30m -run "$REPORT_TESTS" "${cover[@]}" ;;
  esac
}

merge() { # merge <out> <in>...: the first mode line, then every block line
  local out="$1"; shift
  { head -1 "$1"; for f in "$@"; do tail -n +2 "$f"; done; } > "$out"
}

case "${1:-}" in
  core|reports) run "$1" "${2:-}" ;;
  all)
    if [[ -n "${2:-}" ]]; then
      run core "$2.core"; run reports "$2.reports"
      merge "$2" "$2.core" "$2.reports"; rm -f "$2.core" "$2.reports"
    else
      run core ""; run reports ""
    fi ;;
  merge) shift; [[ $# -ge 2 ]] || { echo "usage: go_tests.sh merge OUT IN..." >&2; exit 2; }; merge "$@" ;;
  regex) echo "$REPORT_TESTS" ;;
  *) echo "usage: go_tests.sh core|reports|all [PROFILE] | merge OUT IN... | regex" >&2; exit 2 ;;
esac
