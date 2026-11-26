#!/usr/bin/env bash
# lab.sh — the lab step of the agents' tick (#876, epic E9). tick.sh starts it
# in the background every tick, so a long comparison never delays the tick:
#   - the queue is the lab queue the monitor writes, <LAB_ENV clone>/data/compare/requests/
#     (LAB_ENV, default mvp); a clone without that folder → exit at once, silently;
#   - a stalled request — queued over 2 h, or left running while the runner's
#     lock is free (the runner died) — files one bug `[E9] Lab queue stalled: …`
#     in Triage, as metrics.sh files the agent-health bugs (#755): ids and
#     times only (lab_queue.py issue), never while one is open;
#   - no queued request → nothing starts (no run, no model call);
#   - the runner's lock (.runner.lock) held → logs that it skipped, starts nothing;
#   - else one runner pass (scripts/lab_runner.sh from that clone, cmd/labrun:
#     each queued request goes running, then done with a run id or failed),
#     a run in the ledger (agent lab), and one line per request it took in
#     data/agents/lab/started.jsonl (the weekly usage report's "Lab usage"
#     counts them, lib.sh lab_section).
# The runner keeps its rules: its lock prevents two runs together, a run
# applies nothing, the AI analysis is one model call per done run with the
# ANTHROPIC_API_KEY of this environment (none → the run stays done with a note).
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
LAB_PY="$AGENT_REPO_DIR/scripts/agents/lab_queue.py"
clone="$(env_dir "${LAB_ENV:-mvp}")"
queue="$clone/data/compare/requests"
[[ -d "$queue" ]] || exit 0
require_tools python3 || exit 0
take_lock lab || exit 0
state="$AGENT_STATE_DIR/lab"
mkdir -p "$state"

# A stalled queue: one bug in Triage while none is open (checked before the
# runner, which marks a stopped run failed on its pass).
if python3 "$LAB_PY" issue "$queue" "$state/stalled.title" "$state/stalled.body"; then
  title="$(cat "$state/stalled.title")"
  log "lab: queue stalled: $title"
  if ! command -v gh >/dev/null; then
    log "lab: no gh on PATH, the stalled queue is not filed"
  elif ! slug="$(repo_slug)" || ! open="$(gh issue list -R "$slug" --state open -L 50 \
    --search '"Lab queue stalled" in:title' --json title -q '.[].title')"; then
    log "lab: could not list issues, the stalled queue is retried on a later tick"
  elif grep -qF "[E9] Lab queue stalled" <<< "$open"; then
    log "lab: a stalled-queue issue is already open, none filed"
  elif url="$(gh issue create -R "$slug" --label bug --label E9 --title "$title" --body-file "$state/stalled.body")"; then
    log "lab: filed $url"
    notify "Lab queue stalled" "$title"
    add_to_board "$url" "Triage" >/dev/null || log "lab: $url filed but not put on the board"
  else
    log "lab: could not file \"$title\", retried on a later tick"
  fi
fi

case "$(python3 "$LAB_PY" action "$queue")" in
  none) exit 0 ;;
  busy) log "lab: the runner's lock is held (a run is in progress), skipped"; exit 0 ;;
esac

run_dir="$state/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$run_dir"
python3 "$LAB_PY" queued "$queue" > "$run_dir/queued.txt"
run_begin lab "" "" "$run_dir" # the run ledger (lib.sh): every exit below ends it
log "lab: $(wc -l < "$run_dir/queued.txt" | tr -d ' ') queued request(s), runner started (log $run_dir/runner.log)"
rc=0
if [[ -n "${LAB_RUNNER:-}" ]]; then runner=("$LAB_RUNNER"); else runner=(bash "$clone/scripts/lab_runner.sh"); fi
(cd "$clone" && "${runner[@]}") > "$run_dir/runner.log" 2>&1 < /dev/null || rc=$?
took="$(python3 "$LAB_PY" started "$run_dir/queued.txt" "$queue" "$state/started.jsonl")"
if grep -q "another run is in progress" "$run_dir/runner.log"; then
  log "lab: another runner took the lock first, skipped"
  run_end skipped "runner busy"
elif [[ $rc -eq 0 ]]; then
  log "lab: runner pass done, $took request(s) taken"
  run_end ran "$took request(s) taken"
else
  log "lab: the runner failed (exit $rc, log $run_dir/runner.log), $took request(s) taken"
  run_end failed "runner exit $rc; $took request(s) taken"
fi
