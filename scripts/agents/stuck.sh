#!/usr/bin/env bash
# stuck.sh — run by tick.sh each tick; does nothing until 6 h have passed since
# its last check (#329). Lists the cards that sit In progress (> 6 h) or In
# review (> 24 h) with no open run in the ledger and no label that makes them
# wait (rules in stuck.py, tested offline), and posts one 🤖 comment per
# stalled card — deduplicated by a marker per (card, column, last run). A pause
# or budget in agents.env, or a recent usage limit, explains the idle time:
# those cards are only logged (and listed in the weekly metrics). It moves
# nothing and changes no label. No Claude call.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
STUCK_PY="$AGENT_REPO_DIR/scripts/agents/stuck.py"
state="$AGENT_STATE_DIR/stuck"
mkdir -p "$state/marks"
python3 "$STUCK_PY" due "$state" || exit 0
require_tools gh python3 || exit 1
take_lock stuck || exit 0
slug="$(repo_slug)"
tmp="$(mktemp -d)"; on_exit_remove "$tmp"
board_json > "$tmp/board.json"
python3 "$STUCK_PY" check "$tmp/board.json" "$AGENT_RUNS_FILE" "$state" \
  --budget "$(budget_reason)" --limit-at "$(cat "$LIMIT_FILE" 2>/dev/null || echo 0)" > "$tmp/faults.jsonl"
n=0
while IFS= read -r line; do
  [[ -n "$line" ]] || continue
  n=$((n + 1))
  printf '%s' "$line" | py 'import json,sys; print(json.load(sys.stdin)["body"])' > "$tmp/body-$n.md"
  read -r issue key < <(printf '%s' "$line" | py 'import json,sys; d = json.load(sys.stdin); print(d["issue"], d["key"])')
  if gh issue comment "$issue" -R "$slug" --body-file "$tmp/body-$n.md" >/dev/null < /dev/null; then
    : > "$state/marks/$key"
    log "stuck: #$issue reported ($key)"
  else
    log "stuck: could not comment on #$issue, next check"
  fi
done < "$tmp/faults.jsonl"
log "stuck: check done, $n card(s) to report (all found: $state/reports.jsonl)"
