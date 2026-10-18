#!/usr/bin/env bash
# prune_branches.sh — delete local branches whose work is already on main.
#
#   scripts/prune_branches.sh            this checkout
#   scripts/prune_branches.sh <dir>      another clone (deploy.sh: the mvp clone)
#   scripts/prune_branches.sh -n [dir]   only list what would go
#
# GitHub deletes a PR's branch when it is merged (repo setting
# delete_branch_on_merge, set by scripts/agents/setup_github.sh); this does
# the local side: fetch --prune drops the gone remote refs, then every local
# branch contained in origin/main is deleted. Never main, never a branch
# checked out in a worktree (an agent or a person may be on it), never one
# with a commit main lacks — unpushed or unmerged work stays.
set -euo pipefail
dry=0
[[ "${1:-}" == "-n" ]] && { dry=1; shift; }
dir="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
g() { git -C "$dir" "$@"; }

g fetch -q --prune origin
in_use="$(g worktree list --porcelain | sed -n 's#^branch refs/heads/##p')"
n=0
for b in $(g for-each-ref --format='%(refname:short)' refs/heads/); do
  [[ "$b" == "main" ]] && continue
  printf '%s\n' "$in_use" | grep -qxF "$b" && continue
  g merge-base --is-ancestor "$b" origin/main || continue
  if [[ $dry == 1 ]]; then echo "would delete $b"; else g branch -q -D "$b" && echo "deleted $b"; fi
  n=$((n + 1))
done
g worktree prune
echo "prune_branches: $n branch(es) $([[ $dry == 1 ]] && echo 'to delete' || echo deleted) in $dir"
