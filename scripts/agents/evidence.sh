#!/usr/bin/env bash
# evidence.sh — the weekly evidence report, by itself (#244). tick.sh starts it
# every tick; it does nothing once last week's done-marker exists, so the first
# tick of a Monday (00:00 UTC, or the first after the Mac wakes) runs it:
#   for each environment (stable, mvp) and bot (EVIDENCE_BOTS, "btc eth"):
#     scripts/stress_inputs.sh <bot> → <env clone>/data/stress/<bot>/ (that
#       environment's monitor and read-only MONITOR_API_TOKEN), then
#     go run ./cmd/evidence -env <env> -root <env clone> -bot <bot> -week <week>
#       (mvp records no books: -books-root = stable's clone)
#     → <env clone>/data/evidence/<week>/<bot>.md and .json;
#   one comment on the E2 epic: a row per env × bot and every failed step
#   (evidence_week.py, offline and tested; percentages only for a live env),
#   with one line per recorded dataset that missed more than 3 h in the week
#   (the report's coverage section, #362), also sent as a notify;
#   the direction trial's scorecard (#440): go run ./cmd/evidence
#     -compare-direction for last week and every week since 2026-W43 without
#     one → <stable>/data/evidence/direction/<week>.md|json, a "Direction
#     trial" block in the comment and, once each, an agent:needs-human ticket
#     for the first GEX shed, the brief's early stop and its review date.
# A failed step is listed and the rest goes on. The week is retried once (the
# comment could not be posted); after the second attempt it is done whatever
# happened: data/agents/evidence/<week>.done. Reads the bots' files and
# monitors only: no Deribit private call, no order. No Claude call.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
EV_PY="$AGENT_REPO_DIR/scripts/agents/evidence_week.py"
BOTS="${EVIDENCE_BOTS:-btc eth}"
week="$(python3 "$EV_PY" week)"
state="$AGENT_STATE_DIR/evidence"
done_marker="$state/$week.done"
[[ -f "$done_marker" ]] && exit 0
require_tools go gh curl python3 || exit 0
take_lock evidence || exit 0
mkdir -p "$state/$week"
attempts="$(cat "$state/$week.attempts" 2>/dev/null || echo 0)"
if [[ $attempts -ge 2 ]]; then # the second attempt died before its end
  log "evidence: $week already tried twice, giving up"; date -u +%Y-%m-%dT%H:%M:%SZ > "$done_marker"; exit 0
fi
attempt=$((attempts + 1))
echo "$attempt" > "$state/$week.attempts"
log "evidence: week $week, attempt $attempt"

failures="$state/$week/failures.tsv"
: > "$failures"
fail() { # env, bot, step, log file
  local detail; detail="$(grep -v '^[[:space:]]*$' "$4" 2>/dev/null | tail -1 | tr '\t' ' ' || true)"
  printf '%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "$detail" >> "$failures"
  log "evidence: $1 $2 $3 failed (log $4)"
}

for env in $ENV_NAMES; do
  root="$(env_dir "$env")"
  if [[ ! -d "$root" ]]; then
    echo "no clone at $root" > "$state/$week/$env.log"
    fail "$env" - "clone" "$state/$week/$env.log"; continue
  fi
  books=()
  [[ "$env" == "mvp" ]] && books=(-books-root "$(env_dir stable)")
  for bot in $BOTS; do
    out="$state/$week/$env-$bot"
    # The book at the week's end for the stress section; a failure leaves the
    # last captured book (the report flags it stale) and the rest still runs.
    MONITOR_URL="$(env_monitor_url "$env")" MONITOR_TOKEN="$(env_monitor_token "$env")" \
      bash "$AGENT_REPO_DIR/scripts/stress_inputs.sh" "$bot" "$root/data/stress/$bot" > "$out-stress-inputs.log" 2>&1 ||
      fail "$env" "$bot" "stress inputs" "$out-stress-inputs.log"
    go run ./cmd/evidence -env "$env" -root "$root" ${books[@]+"${books[@]}"} -bot "$bot" -week "$week" \
      > "$out-evidence.log" 2>&1 ||
      fail "$env" "$bot" "cmd/evidence" "$out-evidence.log"
  done
done

# The direction trial's scorecard (#440, docs/direction-trial.md): BTC, bias on
# stable vs sma on mvp, replayed both ways on stable's books → <stable>/data/
# evidence/direction/<week>.md|json. Last week, plus every trial week since
# 2026-W43 still without one (the back-fill). A failure is listed like the rest.
stable_root="$(env_dir stable)"
if [[ -d "$stable_root" ]]; then
  for w in $(python3 "$EV_PY" direction-weeks "$week" "$stable_root"); do
    go run ./cmd/evidence -compare-direction -bot btc -week "$w" -stable-root "$stable_root" -mvp-root "$(env_dir mvp)" \
      > "$state/$week/direction-$w.log" 2>&1 ||
      fail stable btc "direction scorecard $w" "$state/$week/direction-$w.log"
  done
fi

comment="$state/$week/comment.md"
python3 "$EV_PY" comment "$week" "$ENVS_DIR" "$failures" "$(echo $BOTS | tr ' ' ',')" "$(echo $ENV_NAMES | tr ' ' ',')" > "$comment"
log "evidence: wrote $comment"
# A recorder that left a gap over 3 h (#362) is named in the comment and
# notified now, so it can be fixed before the next week's report.
gaps="$(python3 "$EV_PY" gaps "$week" "$ENVS_DIR" "$(echo $BOTS | tr ' ' ',')" "$(echo $ENV_NAMES | tr ' ' ',')" || true)"
if [[ -n "$gaps" ]]; then
  n="$(wc -l <<< "$gaps" | tr -d ' ')"
  log "evidence: recorder gaps: ${gaps//$'\n'/; }"
  notify "Recorder gap ($week)" "$(head -1 <<< "$gaps")$([[ $n -gt 1 ]] && echo " (+$((n - 1)) more)")"
fi

posted=0
slug="$(repo_slug)"
epic="$(gh issue list -R "$slug" --label epic --state open -L 200 --json number,title \
  -q '.[] | select(.title | startswith("Epic E2 ")) | .number' | head -1)" || epic=""
if [[ -z "$epic" ]]; then
  log "evidence: no open E2 epic, report not posted"
elif gh issue comment "$epic" -R "$slug" --body-file "$comment" >/dev/null; then
  log "evidence: posted $week on #$epic"; posted=1
else
  log "evidence: could not comment on #$epic"
fi
# The trial's decision tickets (first GEX shed, early stop, review due): each
# kind filed once, ever, for a person (agent:needs-human, Triage). Never filed
# when the issue list could not be read.
if [[ -d "$stable_root" ]]; then
  tickets="$state/$week/tickets"
  rm -rf "$tickets"
  if gh issue list -R "$slug" --state all -L 300 --search "Direction trial in:title" \
      --json number,title,state > "$state/$week/trial_issues.json" &&
    python3 "$EV_PY" tickets "$week" "$stable_root" "$state/$week/trial_issues.json" "$tickets" > "$state/$week/tickets.log"; then
    for t in "$tickets"/*.title; do
      [[ -f "$t" ]] || continue
      if url="$(gh issue create -R "$slug" --label agent:needs-human --title "$(cat "$t")" --body-file "${t%.title}.body")"; then
        add_to_board "$url" "Triage" >/dev/null || log "evidence: $url filed but not put on the board"
        log "evidence: filed $url"
        notify "Direction trial ($week)" "$(cat "$t")"
      else
        log "evidence: could not file \"$(cat "$t")\""
      fi
    done
  else
    log "evidence: direction trial issues not read, no ticket filed"
  fi
fi

if [[ $posted -eq 1 || $attempt -ge 2 ]]; then
  date -u +%Y-%m-%dT%H:%M:%SZ > "$done_marker"
else
  log "evidence: $week will be retried once on a later tick"
fi
