#!/usr/bin/env bash
# usage.sh — the weekly usage and cohort report, by itself (#340, #341, #252).
# tick.sh starts it every tick; it does nothing once last week's done-marker
# exists, so the first tick of a Monday (UTC) runs it:
#   for each environment (stable, mvp) whose clone has data/monitor/events.jsonl:
#     go run ./cmd/usage -events <clone>/data/monitor/events.jsonl -week <week>
#       -out <clone>/data/monitor/reports -alert
#     → <clone>/data/monitor/reports/<week>.md and .json, and the drop check
#       (its DROPS line → <state>/<week>/<env>.drops.json);
#   one `bug` issue "[E13] Engagement drop: <metrics> <week>" in Triage when a
#   key number fell (usage_week.py issue: one per week, deduplicated by title
#   against open issues and those closed in 14 days);
#   one comment on the E13 epic (usage_week.py comment: counts and percentages
#   only, percentages only for a live env).
# A missing or empty events file, or one with no event in the week, is logged
# and skipped; when no environment has events in the week nothing is posted
# and no marker is written, so a later tick retries (at most hourly). A week whose comment
# could not be posted is tried once more and then marked done:
# data/agents/usage/<week>.done. Reads the monitors' event files only: no bot
# state, no order. No Claude call.
# First it runs assistant_review.sh, the assistant's weekly quality review
# (#438: its own marker, ledger agent assistant_review, the E10 epic), then
# assistant_golden.sh, the live golden-set run (#536: its own marker, ledger
# agent assistant_golden, the E10 epic).
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
# The assistant's weekly quality review (#438) is a step of this job with its
# own marker, run first because this report's checks below exit early; it is
# a no-op without an assistant log, and its failure never stops this report.
bash "$AGENT_REPO_DIR/scripts/agents/assistant_review.sh" || log "usage: assistant_review.sh failed (exit $?)"
bash "$AGENT_REPO_DIR/scripts/agents/assistant_golden.sh" || log "usage: assistant_golden.sh failed (exit $?)"
US_PY="$AGENT_REPO_DIR/scripts/agents/usage_week.py"
week="$(python3 "$US_PY" week)"
state="$AGENT_STATE_DIR/usage"
done_marker="$state/$week.done"
[[ -f "$done_marker" ]] && exit 0
# A week found without events is looked at again at most hourly (not every
# tick): files appear slowly and each look is a run in the ledger.
no_events="$state/$week.no-events"
[[ -n "$(find "$no_events" -mmin -60 2>/dev/null)" ]] && exit 0
has_events=0
for env in $ENV_NAMES; do
  [[ -s "$(env_dir "$env")/data/monitor/events.jsonl" ]] && has_events=1
done
if [[ $has_events -eq 0 ]]; then # every tick until one exists: no run, no ledger line
  log "usage: $week: no environment has a non-empty data/monitor/events.jsonl, nothing to report"; exit 0
fi
require_tools go gh python3 || exit 0
take_lock usage || exit 0
dir="$state/$week"
mkdir -p "$dir"
run_begin usage "" "" "$dir" # the run ledger (lib.sh): every exit below ends it

envs_csv="" # the environments with events in the week (a string: bash 3.2 and set -u)
for env in $ENV_NAMES; do
  root="$(env_dir "$env")"
  events="$root/data/monitor/events.jsonl"
  rm -f "$dir/$env.drops.json"
  if [[ ! -s "$events" ]]; then
    log "usage: $env: $([[ -e "$events" ]] && echo "events file empty" || echo "no events file") ($events), skipped"; continue
  fi
  if ! go run ./cmd/usage -events "$events" -week "$week" -out "$root/data/monitor/reports" -alert > "$dir/$env.log" 2>&1; then
    log "usage: $env: cmd/usage failed (log $dir/$env.log), skipped"; continue
  fi
  { grep '^DROPS ' "$dir/$env.log" || true; } | tail -1 | sed 's/^DROPS //' > "$dir/$env.drops.json"
  [[ -s "$dir/$env.drops.json" ]] || { rm -f "$dir/$env.drops.json"; log "usage: $env: no drop check in $dir/$env.log"; }
  n="$(python3 "$US_PY" events "$root/data/monitor/reports/$week.json")"
  if [[ "$n" == "0" ]]; then
    log "usage: $env: no event in $week ($events), skipped"; continue
  fi
  log "usage: $env: $n events in $week, report $root/data/monitor/reports/$week.md"
  envs_csv="${envs_csv:+$envs_csv,}$env"
done
if [[ -z "$envs_csv" ]]; then
  log "usage: $week: no environment has events in the week — nothing posted, no marker (retried in an hour)"
  date -u +%Y-%m-%dT%H:%M:%SZ > "$no_events"
  run_end skipped "no events in the week"; exit 0
fi

attempts="$(cat "$state/$week.attempts" 2>/dev/null || echo 0)"
attempt=$((attempts + 1))
echo "$attempt" > "$state/$week.attempts"
log "usage: week $week, attempt $attempt ($envs_csv)"
slug="$(repo_slug)"

# The drop issue first (deduplicated by title, so a retry never files twice).
filed=0
rm -rf "$dir/issue"
if gh issue list -R "$slug" --state all -L 300 --search "Engagement drop in:title" \
  --json number,title,state,closedAt > "$dir/issues.json"; then
  python3 "$US_PY" issue "$week" "$ENVS_DIR" "$dir" "$dir/issues.json" "$dir/issue" "$envs_csv" > "$dir/issue.log"
  while IFS= read -r line; do log "usage: $line"; done < "$dir/issue.log"
  if [[ -f "$dir/issue/drop.title" ]]; then
    if url="$(gh issue create -R "$slug" --label bug --title "$(cat "$dir/issue/drop.title")" --body-file "$dir/issue/drop.body")"; then
      filed=1
      add_to_board "$url" "Triage" >/dev/null || log "usage: $url filed but not put on the board"
      log "usage: filed $url"
    else
      log "usage: could not file \"$(cat "$dir/issue/drop.title")\""
    fi
  fi
else
  log "usage: could not list issues, the drop check files nothing this tick"
fi

comment="$dir/comment.md"
python3 "$US_PY" comment "$week" "$ENVS_DIR" "$dir" "$envs_csv" > "$comment"
posted=0
epic="$(gh issue list -R "$slug" --label epic --state open -L 200 --json number,title \
  -q '.[] | select(.title | startswith("Epic E13 ")) | .number' | head -1)" || epic=""
if [[ -z "$epic" ]]; then
  log "usage: no open E13 epic, report not posted"
elif gh issue comment "$epic" -R "$slug" --body-file "$comment" >/dev/null; then
  log "usage: posted $week on #$epic"; posted=1
else
  log "usage: could not comment on #$epic"
fi
if [[ $posted -eq 1 || $attempt -ge 2 ]]; then
  date -u +%Y-%m-%dT%H:%M:%SZ > "$done_marker"
  run_end "$([[ $posted -eq 1 ]] && echo posted || echo failed)" "$envs_csv; $filed drop issue(s) filed"
else
  log "usage: $week will be retried once on a later tick"
  run_end failed "comment not posted, retried once"
fi
