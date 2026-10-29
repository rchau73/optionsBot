#!/usr/bin/env bash
# branch_work.sh — the Dev agent's work on its branch (#517: #466's run was
# blocked with its whole split committed, and the branch lived only in the
# worktree; the next run would have restarted it from main).
#
#   branch_work.sh count <worktree> <base> [<open PR>]
#     prints the commits there are to deliver: with an open PR, the run's own
#     since <base> (the head it started from); without one, every commit ahead
#     of origin/main — a branch a blocked run pushed and the next run resumed
#     goes on to the checks and the PR even when this run added nothing.
#   branch_work.sh keep <worktree> <branch>
#     pushes <branch> (no PR) when it holds a commit origin/main lacks, so the
#     next run on the card starts from it; nothing ahead exits 3, pushes nothing.
#
# Run `git fetch origin` first. No gh: dev_agent.sh comments and moves the card.
set -euo pipefail
usage() { echo "usage: branch_work.sh count <worktree> <base> [<open PR>] | keep <worktree> <branch>" >&2; exit 2; }
[[ $# -ge 3 ]] || usage
mode="$1"; wt="$2"
g() { git -C "$wt" "$@"; }
ahead() { g rev-list --count origin/main..HEAD; }

case "$mode" in
  count)
    [[ $# -le 4 ]] || usage
    if [[ -n "${4:-}" ]]; then g rev-list --count "$3"..HEAD; else ahead; fi ;;
  keep)
    [[ $# -eq 3 ]] || usage
    n="$(ahead)"
    [[ "$n" -gt 0 ]] || { echo "keep: no commit ahead of main, nothing pushed" >&2; exit 3; }
    g push -q origin "HEAD:refs/heads/$3"
    echo "keep: $n commit(s) pushed to $3" >&2 ;;
  *) usage ;;
esac
