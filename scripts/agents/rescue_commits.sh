#!/usr/bin/env bash
# rescue_commits.sh <worktree> <base> <new branch> — moves the Dev agent's new
# commits onto a fresh branch from origin/main, for review fixes made on a PR
# that merged while the agent worked (#478: PR #474 merged at 01:48 UTC, the
# fixes were pushed to its branch the same minute and never reached main).
#
# The commits moved are the branch's own since <base> (the head the run
# started from): merges, and everything origin/main already holds (the merged
# PR, main merged in at the start), are left out. Each is cherry-picked in
# order; one that main already holds the change of (empty) is dropped. A
# conflict aborts, puts the worktree back on the old branch and exits 1 — a
# person resolves it; nothing left to move (all in main already) exits 3. Run `git fetch origin` first: origin/main must hold the
# merge. No network, no gh: dev_agent.sh pushes and opens the PR.
set -euo pipefail
[[ $# -eq 3 ]] || { echo "usage: rescue_commits.sh <worktree> <base> <new branch>" >&2; exit 2; }
wt="$1"; base="$2"; new="$3"
g() { git -C "$wt" "$@"; }
g rev-parse -q --verify "$base^{commit}" >/dev/null || { echo "rescue: unknown base $base" >&2; exit 2; }

old="$(g rev-parse HEAD)"
old_branch="$(g symbolic-ref -q --short HEAD || echo "$old")"
commits=()
while read -r c; do [[ -n "$c" ]] && commits+=("$c"); done < <(g rev-list --reverse --no-merges "$old" "^$base" "^origin/main")
[[ ${#commits[@]} -gt 0 ]] || { echo "rescue: no commit of the branch after ${base:0:7} that main lacks" >&2; exit 3; }

g checkout -q -B "$new" origin/main
moved=0
for c in "${commits[@]}"; do
  if g cherry-pick "$c" >/dev/null 2>&1; then moved=$((moved + 1)); continue; fi
  if [[ -z "$(g diff --name-only --diff-filter=U)" ]] && g diff --quiet && g diff --cached --quiet; then
    g cherry-pick --skip >/dev/null 2>&1 # main already holds this change
    echo "rescue: ${c:0:7} already in main, dropped" >&2
    continue
  fi
  echo "rescue: ${c:0:7} conflicts with main in: $(g diff --name-only --diff-filter=U | tr '\n' ' ')" >&2
  g cherry-pick --abort >/dev/null 2>&1 || true
  g checkout -q "$old_branch"
  exit 1
done
[[ $moved -gt 0 ]] || { g checkout -q "$old_branch"; echo "rescue: every commit is already in main" >&2; exit 3; }
echo "rescue: $moved commit(s) on $new from origin/main" >&2
