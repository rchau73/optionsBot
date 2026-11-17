#!/usr/bin/env bash
# recording_check.sh — the daily recording check (#716). tick.sh starts it
# every tick; it does nothing once yesterday's (UTC) done-marker exists, so the
# first tick of a UTC day runs it:
#   for each environment that records the market (deploy.env without
#   BOT_RECORD_MARKET=0: stable) and bot (RECORDING_BOTS, "btc eth") whose
#   folder <clone>/data/<bot> exists,
#     go run ./cmd/datacoverage -dir <clone>/data/<bot> -yesterday -json
#     → data/agents/recording/<day>/<env>-<bot>.json: yesterday's covered hours
#       and share per market recording (books, minute GEX) against the weekly
#       report's floor (evidence.ReplayFullCoveragePct, 90 %) and each gap over
#       evidence.LongGapHours (3 h); a folder without the day's file is 0 %;
#   every dataset at the floor → a log line, nothing sent, nothing posted;
#   otherwise one notification (the bot, dataset, day, covered share and the
#   longest gap, "+n more") and one comment on the E2 epic for the day
#   (recording_day.py, offline and tested; hours and percentages only).
# A comment that could not be posted is retried on a later tick of the same
# day (the notification is sent once). Reads the recorded files only: no
# Deribit call, no write to a clone, no Claude call.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
REC_PY="$AGENT_REPO_DIR/scripts/agents/recording_day.py"
BOTS="${RECORDING_BOTS:-btc eth}"
day="$(python3 "$REC_PY" yesterday)"
state="$AGENT_STATE_DIR/recording"
done_marker="$state/$day.done"
[[ -f "$done_marker" ]] && exit 0
check="${RECORDING_CHECK_BIN:-}" # tests pass a prebuilt cmd/datacoverage
if [[ -n "$check" ]]; then require_tools gh python3 || exit 0; else require_tools go gh python3 || exit 0; fi
take_lock recording || exit 0
dir="$state/$day"
mkdir -p "$dir"

# What there is to check: "env bot" lines. Nothing → no run.
sources=""
while IFS= read -r env; do
  for bot in $BOTS; do
    [[ -d "$(env_dir "$env")/data/$bot" ]] && sources="$sources$env $bot"$'\n'
  done
done < <(python3 "$REC_PY" recorders "$ENVS_DIR" "$(echo $ENV_NAMES | tr ' ' ',')")
if [[ -z "$sources" ]]; then
  log "recording: no environment records the market, nothing to check"
  date -u +%Y-%m-%dT%H:%M:%SZ > "$done_marker"; exit 0
fi
run_begin recording "" "" "$dir" # the run ledger (lib.sh): every exit below ends it

failures="$dir/failures.tsv"
: > "$failures"
while read -r env bot; do
  [[ -n "$env" ]] || continue
  out="$dir/$env-$bot"
  if [[ -n "$check" ]]; then cmd=("$check"); else cmd=(go run ./cmd/datacoverage); fi
  if ! "${cmd[@]}" -dir "$(env_dir "$env")/data/$bot" -yesterday -json > "$out.json" 2> "$out.err"; then
    rm -f "$out.json"
    printf '%s\t%s\n' "$env" "$bot" >> "$failures"
    log "recording: $env $bot not checked (log $out.err)"
  fi
done <<< "$sources"

short="$(python3 "$REC_PY" short "$dir")"
if [[ -z "$short" && ! -s "$failures" ]]; then
  log "recording: $day every market recording meets the floor"
  date -u +%Y-%m-%dT%H:%M:%SZ > "$done_marker"
  run_end complete "$day at the floor"; exit 0
fi
[[ -z "$short" ]] || log "recording: short: ${short//$'\n'/; }"
if [[ ! -f "$dir/notified" ]]; then
  notify "Recording short ($day)" "$(python3 "$REC_PY" notify "$day" "$dir")"
  date -u +%Y-%m-%dT%H:%M:%SZ > "$dir/notified"
fi

python3 "$REC_PY" comment "$day" "$dir" > "$dir/comment.md"
if ! slug="$(repo_slug)" || ! epic="$(gh issue list -R "$slug" --label epic --state open -L 200 --json number,title \
  -q '.[] | select(.title | startswith("Epic E2 ")) | .number')"; then
  log "recording: the epics could not be read, $day retried on a later tick"
  run_end short "$day short; epics not read"; exit 0
fi
epic="${epic%%$'\n'*}"
if [[ -z "$epic" ]]; then
  log "recording: no open E2 epic, $day not posted"
  date -u +%Y-%m-%dT%H:%M:%SZ > "$done_marker"
  run_end short "$day short; no E2 epic"
elif gh issue comment "$epic" -R "$slug" --body-file "$dir/comment.md" >/dev/null; then
  log "recording: posted $day on #$epic"
  date -u +%Y-%m-%dT%H:%M:%SZ > "$done_marker"
  run_end posted "$day short; on #$epic"
else
  log "recording: could not comment on #$epic, retried on a later tick"
  run_end short "$day short; comment not posted"
fi
