#!/usr/bin/env bash
# pr_gate.sh — every tick, before the agents: makes sure a person only picks up
# Dev agent PRs that are ready (product owner, 2026-10-19). For each open
# agent:dev PR (pr_gate.py decide):
#   conflict → only in generated files: main merged in and the generators
#              rerun here (scripts/regen.sh, #465), the Reviewer checks the
#              new commit; any other: the card goes back to Ready with
#              needs-rebase, the Dev agent merges main and resolves (dev_agent.sh);
#   update   → behind main without conflict: gh pr update-branch, and the
#              Reviewer checks the new commit;
#   ready    → reviewed clean on its current commit, CI green, CLEAN, no open
#              dependency: label ready-to-merge (the person's queue) — a PR
#              that changes a business rule or policy, or any PR with
#              PR_AUTO_MERGE=0 (agents.env);
#   merge    → the same, policy-free and up to date with main: merged here
#              (#468, product owner 2026-10-28), at most one per run so the
#              next one is updated and re-tested on the new main first;
#   wait     → the label comes off until it is ready again — always while the
#              card is In progress: a Dev agent is working on it (#478).
# A clean review carries over GitHub's own update-with-main merges (no
# conflict is possible in them: pr_gate.py equiv), so updating a PR before it
# merges costs no new review.
# No Claude call; one comment per PR commit at most.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
take_lock pr-gate || exit 0
slug="$(repo_slug)"
GATE_PY="$AGENT_REPO_DIR/scripts/agents/pr_gate.py"
BOARD_PY="$AGENT_REPO_DIR/scripts/agents/pm_board.py"
STATE="$AGENT_STATE_DIR/pr_gate.json"
tmp="$AGENT_STATE_DIR/pr-gate-$$"; mkdir -p "$tmp"; on_exit_remove "$tmp"

seen() { py 'import json,sys
try: s = json.load(open(sys.argv[1]))
except FileNotFoundError: s = {}
print("1" if s.get(sys.argv[2]) == sys.argv[3] else "0")' "$STATE" "$1" "$2"; }
remember() { py 'import json,sys
try: s = json.load(open(sys.argv[1]))
except FileNotFoundError: s = {}
s[sys.argv[2]] = sys.argv[3]; json.dump(s, open(sys.argv[1], "w"), indent=1)' "$STATE" "$1" "$2"; }

# gate_run <outcome> <detail> — one run in the ledger (lib.sh) for each PR the
# gate changed this tick; a tick that changes nothing records nothing.
gate_run() {
  run_begin pr_gate "$issue" "$pr"
  RUN_COMMIT="${head:0:7}"
  run_end "$1" "$2"
}

# merge_regenerating <branch> — a PR that conflicts with main only in generated
# files (.gitattributes linguist-generated: the help index, docs/parameters.md,
# the effective-config goldens) needs no agent (#465): merge main into the
# branch in a scratch worktree, rerun the generators (scripts/regen.sh), push.
# Any other conflict, or a failure, pushes nothing and returns 1.
merge_regenerating() {
  local branch="$1" wt="$tmp/merge"
  git_locked fetch -q origin || return 1
  git_locked worktree add -q --detach "$wt" "origin/$branch" 2>/dev/null || return 1
  local rc=1
  if ! git -C "$wt" merge --no-edit -q origin/main >/dev/null 2>&1 &&
    (cd "$wt" && bash scripts/regen.sh --resolve) >> "$tmp/regen.log" 2>&1 &&
    git -C "$wt" commit -q --no-edit &&
    git_locked -C "$wt" push -q origin "HEAD:$branch"; then
    rc=0
  fi
  git_locked worktree remove --force "$wt" 2>/dev/null || true
  return $rc
}

merged=0
gh pr list -R "$slug" --label agent:dev --state open --json number,headRefName -q '.[] | "\(.number) \(.headRefName)"' > "$tmp/prs.txt"
# Each card's column, read once: a card In progress waits (#478).
board_cards > "$tmp/cards.txt" || : > "$tmp/cards.txt"
while read -r pr ref; do
  [[ -n "$pr" && "$ref" == agent/issue-* ]] || continue
  # agent/issue-<n>, or agent/issue-<n>-r<k>: review fixes moved to a new PR
  # after the first merged (dev_agent.sh, #478).
  issue="${ref#agent/issue-}"; issue="${issue%%-r*}"
  [[ "$issue" =~ ^[0-9]+$ ]] || continue
  card="$(awk -v n="$issue" '$2 == n { $1 = $2 = ""; sub(/^  /, ""); print; exit }' "$tmp/cards.txt")"
  gh pr view "$pr" -R "$slug" --json number,state,isDraft,labels,headRefOid,mergeable,mergeStateStatus,statusCheckRollup > "$tmp/pr.json" < /dev/null
  gh api "repos/$slug/pulls/$pr/reviews" --paginate -q '.[].body' < /dev/null |
    grep -oE "<!-- agent-review sha=[0-9a-f]* verdict=[a-z]*( policy=[a-z]*)? -->" | sed "s/<!-- agent-review //; s/ -->//" > "$tmp/markers.txt" || true
  head="$(py 'import json,sys; print(json.load(open(sys.argv[1]))["headRefOid"])' "$tmp/pr.json")"
  # Every changed path (the files API pages past gh pr view's 100), how far
  # behind main the head is, and the review the head inherits, if any.
  gh api "repos/$slug/pulls/$pr/files" --paginate -q '.[].filename' > "$tmp/files.txt" < /dev/null
  behind="$(gh api "repos/$slug/compare/main...$head" -q .behind_by < /dev/null || echo 0)"
  : > "$tmp/equiv.txt"
  last="$(tail -1 "$tmp/markers.txt" | sed -n 's/^sha=\([0-9a-f]*\) .*/\1/p')"
  if [[ -n "$last" && "$last" != "$head" ]] && gh api "repos/$slug/compare/$last...$head" > "$tmp/compare.json" < /dev/null; then
    python3 "$GATE_PY" equiv "$tmp/compare.json" > "$tmp/equiv.txt" || true
  fi
  : > "$tmp/deps.txt"
  for d in $(gh issue view "$issue" -R "$slug" --json body -q .body < /dev/null | python3 "$BOARD_PY" deps); do
    [[ "$(gh issue view "$d" -R "$slug" --json state -q .state < /dev/null)" == "OPEN" ]] && echo "$d" >> "$tmp/deps.txt"
  done
  read -r action why < <(python3 "$GATE_PY" decide "$tmp/pr.json" "$tmp/markers.txt" "$tmp/deps.txt" \
    "$tmp/equiv.txt" "$tmp/files.txt" "$behind" "${PR_AUTO_MERGE:-1}" "$card")
  has_ready="$(py 'import json,sys; print(any(l["name"] == "ready-to-merge" for l in json.load(open(sys.argv[1]))["labels"]))' "$tmp/pr.json")"
  issue_ready="$(gh issue view "$issue" -R "$slug" --json labels -q '[.labels[].name] | index("ready-to-merge") != null' < /dev/null)"
  if [[ "$action" != "ready" && "$action" != "merge" ]]; then
    [[ "$has_ready" != "True" ]] || gh pr edit "$pr" -R "$slug" --remove-label ready-to-merge >/dev/null < /dev/null || true
    [[ "$issue_ready" != "true" ]] || gh issue edit "$issue" -R "$slug" --remove-label ready-to-merge >/dev/null < /dev/null || true
  elif [[ "$issue_ready" != "true" ]]; then
    # On the card too: the board shows issue labels, and the PM puts these first in In review.
    gh issue edit "$issue" -R "$slug" --add-label ready-to-merge >/dev/null < /dev/null || true
  fi
  case "$action" in
    conflict)
      [[ "$(seen "$pr" "conflict:$head")" == "1" ]] && continue
      remember "$pr" "conflict:$head"
      if merge_regenerating "$ref"; then
        log "pr-gate: #$pr conflicted only in generated files: merged main, regenerated, pushed"
        gate_run branch_updated "conflict only in generated files: regenerated"
        continue
      fi
      item="$(item_for_issue "$issue")"
      gh issue edit "$issue" -R "$slug" --add-label needs-rebase >/dev/null < /dev/null || true
      [[ -n "$item" ]] && set_status "$item" "Ready"
      gh issue comment "$issue" -R "$slug" --body "🤖 **PR gate** — #$pr conflicts with \`main\` (another PR merged first). Back to **Ready** with \`needs-rebase\`: the Dev agent merges \`main\` into the branch, resolves the conflict keeping both changes' intent, re-runs every check, and the Reviewer checks the result before it comes back to you." >/dev/null < /dev/null
      log "pr-gate: #$pr conflicts → #$issue Ready (needs-rebase)"
      gate_run sent_back "conflicts with main: Ready with needs-rebase" ;;
    update)
      if gh pr update-branch "$pr" -R "$slug" >/dev/null 2>&1 < /dev/null; then
        log "pr-gate: #$pr updated with main"; gate_run branch_updated "behind main: updated"
      else log "pr-gate: #$pr could not be updated with main"; gate_run failed "could not update with main"; fi ;;
    ready)
      if [[ "$has_ready" != "True" ]]; then
        gh pr edit "$pr" -R "$slug" --add-label ready-to-merge >/dev/null < /dev/null
        if [[ "$(seen "$pr" "ready:$head")" != "1" ]]; then
          gh pr comment "$pr" -R "$slug" --body "🤖 **PR gate** — ready for you to merge (\`${head:0:7}\`): $why. Nothing it depends on is open." >/dev/null < /dev/null
          notify "PR #$pr ready to merge" "issue #$issue"
          remember "$pr" "ready:$head"
        fi
        log "pr-gate: #$pr ready-to-merge"
        gate_run ready_to_merge "reviewed clean, CI green, no conflict"
      fi ;;
    merge)
      if [[ "$merged" -eq 1 ]]; then log "pr-gate: #$pr merges on a later run (one merge per run)"; continue; fi
      # --match-head-commit: a push after this decision refuses the merge.
      if gh pr merge "$pr" -R "$slug" --merge --match-head-commit "$head" >/dev/null 2>"$tmp/merge.err" < /dev/null; then
        merged=1
        gh pr comment "$pr" -R "$slug" --body "🤖 **PR gate** — merged \`${head:0:7}\`: $why (#468). mvp redeploys it on its next tick; stable moves only by a release." >/dev/null < /dev/null || true
        notify "PR #$pr merged" "issue #$issue"
        log "pr-gate: #$pr merged"
        gate_run merged "reviewed clean, CI green, up to date, no policy change"
      else
        log "pr-gate: #$pr merge refused: $(head -c 200 "$tmp/merge.err")"
        gate_run failed "merge refused: $(head -c 120 "$tmp/merge.err" | tr '\n' ' ')"
      fi ;;
    *) log "pr-gate: #$pr waits — $why"
      [[ "$has_ready" != "True" ]] || gate_run ready_removed "waits: $why" ;;
  esac
done < "$tmp/prs.txt"
