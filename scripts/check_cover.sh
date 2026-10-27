#!/usr/bin/env bash
# check_cover.sh — the coverage floor (#330): CI fails when the total statement
# coverage of ./internal/... falls below the baseline kept in the repo, so a
# change cannot quietly remove the tests of the trading rules.
#
#   scripts/check_cover.sh                    run the coverage as make cover does, then check
#   scripts/check_cover.sh --profile FILE     check an existing profile (make check and CI
#                                             reuse the race run's, so the tests run once)
#   scripts/check_cover.sh --update [...]     rewrite the baseline to today's total — only by
#                                             a person, or a PR that says so
#
# Baseline: scripts/cover_baseline.txt, one number to one decimal. Beside it,
# scripts/cover_baseline_packages.txt ("<package> <pct>" per line, also written
# by --update) is what the failure message compares each package with; it never
# fails the check by itself. The check fails when the total is more than
# TOLERANCE (0.2) points under the baseline, and prints a hint to raise the
# baseline when the total is above it.
#
# Totals are computed from the profile itself (statements of each block counted
# once, covered when any test run hit it) — the figure `go tool cover -func`
# prints on its total line. COVER_BASELINE names another baseline file (tests).
# Bash 3.2-safe: macOS /bin/bash runs make check.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
export LC_ALL=C # awk prints 71.4, never 71,4, whatever the person's locale



TOLERANCE="0.2"
baseline="${COVER_BASELINE:-scripts/cover_baseline.txt}"
pkg_baseline="${baseline%.txt}_packages.txt"
profile=""
update=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --update) update=1; shift ;;
    --profile)
      [[ $# -ge 2 ]] || { echo "check_cover: --profile needs a file"; exit 2; }
      profile="$2"; shift 2 ;;
    *) echo "check_cover: unknown argument $1 (use --profile FILE, --update)"; exit 2 ;;
  esac
done

if [[ -z "$profile" ]]; then
  profile="coverage.out"
  go test ./tests/... -race -count=1 -coverpkg=./internal/... -coverprofile="$profile"
fi
[[ -s "$profile" ]] || { echo "check_cover: no coverage profile at $profile"; exit 2; }

# One line "TOTAL <pct>", then "<package> <pct>" per package, sorted.
summarize() {
  awk '
    $1 == "mode:" { next }
    NF == 3 {
      key = $1
      if (!(key in stmts)) {
        stmts[key] = $2
        file = key; sub(/:[^:]*$/, "", file)
        pkg = file; sub(/\/[^\/]*$/, "", pkg)
        pkgof[key] = pkg
      }
      if ($3 > 0) hit[key] = 1
    }
    END {
      for (k in stmts) {
        p = pkgof[k]; tot[p] += stmts[k]; all += stmts[k]
        if (k in hit) { cov[p] += stmts[k]; allcov += stmts[k] }
      }
      if (all == 0) exit 1
      printf "TOTAL %.1f\n", 100 * allcov / all
      for (p in tot) printf "%s %.1f\n", p, 100 * cov[p] / tot[p] | "sort"
    }
  ' "$1"
}

summary="$(summarize "$profile")" || { echo "check_cover: no statements in $profile"; exit 2; }
total="$(printf '%s\n' "$summary" | awk '$1 == "TOTAL" { print $2 }')"
packages="$(printf '%s\n' "$summary" | grep -v '^TOTAL ')"

if [[ $update -eq 1 ]]; then
  printf '%s\n' "$total" >"$baseline"
  printf '%s\n' "$packages" >"$pkg_baseline"
  echo "check_cover: baseline set to $total% ($baseline, $pkg_baseline)"
  exit 0
fi

[[ -s "$baseline" ]] || { echo "check_cover: no baseline at $baseline (run scripts/check_cover.sh --update)"; exit 2; }
base="$(tr -d '[:space:]' <"$baseline")"
[[ "$base" =~ ^[0-9]+(\.[0-9])?$ ]] || { echo "check_cover: $baseline must hold one number to one decimal, got '$base'"; exit 2; }

# Compare in tenths of a point, as integers: no float rounding at the edge.
verdict="$(awk -v t="$total" -v b="$base" -v tol="$TOLERANCE" 'BEGIN {
  t10 = int(t * 10 + 0.5); b10 = int(b * 10 + 0.5); tol10 = int(tol * 10 + 0.5)
  if (t10 < b10 - tol10) print "fell"; else if (t10 > b10) print "rose"; else print "held"
}')"

case "$verdict" in
  held)
    echo "check_cover: coverage $total% (baseline $base%, tolerance $TOLERANCE) — ok" ;;
  rose)
    echo "check_cover: coverage $total% is above the baseline $base% — ok."
    echo "check_cover: raise the floor with scripts/check_cover.sh --update (and say so in the PR)." ;;
  fell)
    echo "check_cover: FAIL — coverage $total% is under the baseline $base% by more than $TOLERANCE points."
    if [[ -s "$pkg_baseline" ]]; then
      echo "check_cover: packages whose coverage fell (baseline → now):"
      printf '%s\n' "$packages" | awk -v ref="$pkg_baseline" '
        BEGIN { while ((getline line < ref) > 0) { split(line, f, " "); old[f[1]] = f[2] } }
        ($1 in old) && int($2 * 10 + 0.5) < int(old[$1] * 10 + 0.5) {
          printf "  %-50s %5.1f%% → %5.1f%%\n", $1, old[$1], $2; n++
        }
        !($1 in old) { printf "  %-50s   new → %5.1f%%\n", $1, $2 }
        END { if (n == 0) print "  (no package under its own baseline: the fall is spread, or a new package is barely tested)" }'
    else
      echo "check_cover: no $pkg_baseline to compare packages with; coverage per package now:"
      printf '%s\n' "$packages" | awk '{ printf "  %-50s %5.1f%%\n", $1, $2 }'
    fi
    exit 1 ;;
esac
