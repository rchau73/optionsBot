#!/usr/bin/env bash
# pr_gate.sh — every tick, before the agents: makes sure a person only picks up
# Dev agent PRs that are ready (product owner, 2026-10-19). For each open
# agent:dev PR (pr_gate.py decide):
#   conflict → the card goes back to Ready with needs-rebase; the Dev agent
#              merges main into the branch and resolves (dev_agent.sh);
#   update   → behind main without conflict: gh pr update-branch, and the
#              Reviewer checks the new commit;
#   ready    → reviewed clean on its current commit, CI green, CLEAN, no open
#              dependency: label ready-to-merge (the person's queue);
#   wait     → the label comes off until it is ready again.
# Merge order follows priority: a person merges ready PRs top-down (the PM
# orders In review), and whatever then conflicts is rebased by the Dev agent.
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

gh pr list -R "$slug" --label agent:dev --state open --json number,headRefName -q '.[] | "\(.number) \(.headRefName)"' > "$tmp/prs.txt"
while read -r pr ref; do
  [[ -n "$pr" && "$ref" == agent/issue-* ]] || continue
  issue="${ref#agent/issue-}"
  gh pr view "$pr" -R "$slug" --json number,state,isDraft,labels,headRefOid,mergeable,mergeStateStatus,statusCheckRollup > "$tmp/pr.json" < /dev/null
  gh api "repos/$slug/pulls/$pr/reviews" --paginate -q '.[].body' < /dev/null |
    grep -o "<!-- agent-review sha=[0-9a-f]* verdict=[a-z]* -->" | sed "s/<!-- agent-review //; s/ -->//" > "$tmp/markers.txt" || true
  : > "$tmp/deps.txt"
  for d in $(gh issue view "$issue" -R "$slug" --json body -q .body < /dev/null | python3 "$BOARD_PY" deps); do
    [[ "$(gh issue view "$d" -R "$slug" --json state -q .state < /dev/null)" == "OPEN" ]] && echo "$d" >> "$tmp/deps.txt"
  done
  read -r action why < <(python3 "$GATE_PY" decide "$tmp/pr.json" "$tmp/markers.txt" "$tmp/deps.txt")
  head="$(py 'import json,sys; print(json.load(open(sys.argv[1]))["headRefOid"])' "$tmp/pr.json")"
  has_ready="$(py 'import json,sys; print(any(l["name"] == "ready-to-merge" for l in json.load(open(sys.argv[1]))["labels"]))' "$tmp/pr.json")"
  issue_ready="$(gh issue view "$issue" -R "$slug" --json labels -q '[.labels[].name] | index("ready-to-merge") != null' < /dev/null)"
  if [[ "$action" != "ready" ]]; then
    [[ "$has_ready" != "True" ]] || gh pr edit "$pr" -R "$slug" --remove-label ready-to-merge >/dev/null < /dev/null || true
    [[ "$issue_ready" != "true" ]] || gh issue edit "$issue" -R "$slug" --remove-label ready-to-merge >/dev/null < /dev/null || true
  elif [[ "$issue_ready" != "true" ]]; then
    # On the card too: the board shows issue labels, and the PM puts these first in In review.
    gh issue edit "$issue" -R "$slug" --add-label ready-to-merge >/dev/null < /dev/null || true
  fi
  case "$action" in
    conflict)
      [[ "$(seen "$pr" "conflict:$head")" == "1" ]] && continue
      item="$(item_for_issue "$issue")"
      gh issue edit "$issue" -R "$slug" --add-label needs-rebase >/dev/null < /dev/null || true
      [[ -n "$item" ]] && set_status "$item" "Ready"
      gh issue comment "$issue" -R "$slug" --body "🤖 **PR gate** — #$pr conflicts with \`main\` (another PR merged first). Back to **Ready** with \`needs-rebase\`: the Dev agent merges \`main\` into the branch, resolves the conflict keeping both changes' intent, re-runs every check, and the Reviewer checks the result before it comes back to you." >/dev/null < /dev/null
      remember "$pr" "conflict:$head"; log "pr-gate: #$pr conflicts → #$issue Ready (needs-rebase)"
      gate_run sent_back "conflicts with main: Ready with needs-rebase" ;;
    update)
      if gh pr update-branch "$pr" -R "$slug" >/dev/null 2>&1 < /dev/null; then
        log "pr-gate: #$pr updated with main"; gate_run branch_updated "behind main: updated"
      else log "pr-gate: #$pr could not be updated with main"; gate_run failed "could not update with main"; fi ;;
    ready)
      if [[ "$has_ready" != "True" ]]; then
        gh pr edit "$pr" -R "$slug" --add-label ready-to-merge >/dev/null < /dev/null
        if [[ "$(seen "$pr" "ready:$head")" != "1" ]]; then
          gh pr comment "$pr" -R "$slug" --body "🤖 **PR gate** — ready for you: reviewed clean on \`${head:0:7}\`, CI green, no conflict with \`main\`, nothing it depends on is open." >/dev/null < /dev/null
          notify "PR #$pr ready to merge" "issue #$issue"
          remember "$pr" "ready:$head"
        fi
        log "pr-gate: #$pr ready-to-merge"
        gate_run ready_to_merge "reviewed clean, CI green, no conflict"
      fi ;;
    *) log "pr-gate: #$pr waits — $why"
      [[ "$has_ready" != "True" ]] || gate_run ready_removed "waits: $why" ;;
  esac
done < "$tmp/prs.txt"
