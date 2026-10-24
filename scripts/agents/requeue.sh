#!/usr/bin/env bash
# requeue.sh — run by tick.sh before the Dev agents start: a Blocked card whose
# Dev stop was a usage limit (an older LIMIT_PATTERN did not know its wording,
# #237) loses agent:needs-human, goes back to Ready and gets one comment. The
# rules are in requeue.py (tested offline); this script only reads the board
# and the issues and applies them. No Claude call.
#
# Nothing moves inside a limit pause (lib.sh parallel_max 0), so a card is not
# re-queued into a run that would hit the same limit.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
require_tools gh python3 || exit 1
take_lock requeue || exit 0
[[ "$(parallel_max 1)" -gt 0 ]] || { log "requeue: inside a limit pause, nothing moved"; exit 0; }

slug="$(repo_slug)"
rq="$AGENT_REPO_DIR/scripts/agents/requeue.py"
runs="$AGENT_STATE_DIR/dev"
tmp="$(mktemp -d)"; on_exit_remove "$tmp"
board_json > "$tmp/board.json"

# Comments only of the cards whose newest run log shows a limit.
nums="$(python3 "$rq" candidates "$tmp/board.json" "$runs")"
[[ -n "$nums" ]] || exit 0
echo "{" > "$tmp/comments.json"; sep=""
for n in $nums; do
  printf '%s"%s": %s\n' "$sep" "$n" "$(gh issue view "$n" -R "$slug" --json comments -q .comments)" >> "$tmp/comments.json"
  sep=","
done
echo "}" >> "$tmp/comments.json"

python3 "$rq" select "$tmp/board.json" "$tmp/comments.json" "$runs" | while read -r item n; do
  [[ -n "$n" ]] || continue
  gh issue edit "$n" -R "$slug" --remove-label agent:needs-human >/dev/null
  set_status "$item" "Ready"
  gh issue comment "$n" -R "$slug" --body "🤖 Re-queued: the earlier stop was a usage limit, not a question." >/dev/null
  log "requeue: #$n → Ready (its Dev stop was a usage limit)"
done
