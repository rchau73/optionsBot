#!/usr/bin/env bash
# go_no_go.sh — the weekly go / no-go reading (#738). tick.sh starts it every
# tick; it does nothing once this week's done-marker exists, so the first tick
# of a Monday runs it — after the weekly evidence report (its E2 row reads it),
# or from Wednesday on whatever happened to that report:
#   python3 scripts/product/go_no_go_week.py runs scripts/product/go_no_go.py -json
#   against stable's clone (--evidence-root, --bot btc) and the newest tally and
#   fake-door files under data/product/, and appends one dated line to the
#   gitignored data/product/go_no_go_history.jsonl (percentages and ratios only);
#   when a row changed status or the overall line became "kill criterion met" /
#   "no kill criterion met", one agent:pm + agent:needs-human issue goes to
#   Triage with the old and new reading and the source file.
# Reads files only: no Claude call, no Deribit call, no order. A missing input
# stays "missing"; a fresh checkout with no data/product/ writes a missing reading.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
GNG_PY="$AGENT_REPO_DIR/scripts/product/go_no_go_week.py"
week="$(python3 "$AGENT_REPO_DIR/scripts/agents/evidence_week.py" week)"
state="$AGENT_STATE_DIR/go_no_go"
done_marker="$state/$week.done"
[[ -f "$done_marker" ]] && exit 0
if [[ ! -f "$AGENT_STATE_DIR/evidence/$week.done" && "$(date -u +%u)" -lt 3 ]]; then exit 0; fi
require_tools python3 gh || exit 0
take_lock go_no_go || exit 0
dir="$state/$week"
mkdir -p "$dir"
run_begin go_no_go "" "" "$dir" # the run ledger (lib.sh): every exit below ends it
# An alert whose issue could not be filed stays in $dir/alert and is retried before
# the week is done: the next reading's baseline is the new one, so the change
# would never alert again, and the scorecard must not run (and append) twice.
if [[ ! -f "$dir/alert/title.txt" ]]; then
  rm -rf "$dir/alert"
  if ! python3 "$GNG_PY" --evidence-root "$(env_dir stable)" --bot btc --alert-dir "$dir/alert" > "$dir/reading.log" 2>&1; then
    log "go_no_go: the reading failed (log $dir/reading.log)"
    run_end failed "scorecard failed"; exit 1
  fi
fi
if [[ ! -f "$dir/alert/title.txt" ]]; then
  date -u +%Y-%m-%dT%H:%M:%SZ > "$done_marker"
  log "go_no_go: $week read, nothing changed that needs a person"
  run_end written "$week read, no change"; exit 0
fi
if url="$(gh issue create -R "$(repo_slug)" --label agent:pm --label agent:needs-human \
    --title "$(cat "$dir/alert/title.txt")" --body-file "$dir/alert/body.md")"; then
  date -u +%Y-%m-%dT%H:%M:%SZ > "$done_marker"
  add_to_board "$url" "Triage" >/dev/null || log "go_no_go: $url filed but not put on the board"
  log "go_no_go: filed $url"
  notify "Go / no-go reading ($week)" "$(cat "$dir/alert/title.txt")"
  run_end posted "$week read, issue filed"
else
  log "go_no_go: could not file the issue for $week; retrying next tick (alert kept in $dir/alert)"
  notify "Go / no-go reading: issue not filed" "$(cat "$dir/alert/title.txt")"
  run_end failed "$week read, issue not filed (retried next tick)"
fi
