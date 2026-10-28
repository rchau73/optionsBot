#!/usr/bin/env bash
# assistant_review.sh — the assistant's weekly quality review (#438), the step
# usage.sh runs on every tick before its own report (so the first tick of a
# Monday UTC runs both); it does nothing once last week's done-marker exists:
#   for each environment (stable, mvp) whose clone has a non-empty
#   data/monitor/assistant.jsonl:
#     go run ./cmd/assistantreview -log <that file> -week <week>
#       -out <clone>/data/monitor/assistant_reviews
#     → <week>.md|json (private: questions and answers, never posted),
#       <week>.summary.json (counts and percentages only) and
#       <week>.golden_candidates.jsonl;
#   one comment on the E10 epic (usage_week.py assistant-comment: the
#   summaries' named count and percentage fields, percentages only for a
#   live env).
# An env with no answer in the week is skipped; when none has one nothing is
# posted and no marker is written (looked at again at most hourly). A comment
# that could not be posted is tried once more, then the week is marked done:
# data/agents/assistant_review/<week>.done. Reads the monitors' review logs
# only, never writes them. No Claude call, no model, no Deribit call.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
US_PY="$AGENT_REPO_DIR/scripts/agents/usage_week.py"
week="$(python3 "$US_PY" week)"
state="$AGENT_STATE_DIR/assistant_review"
done_marker="$state/$week.done"
[[ -f "$done_marker" ]] && exit 0
quiet="$state/$week.no-answers"
[[ -n "$(find "$quiet" -mmin -60 2>/dev/null)" ]] && exit 0
has_log=0
for env in $ENV_NAMES; do
  [[ -s "$(env_dir "$env")/data/monitor/assistant.jsonl" ]] && has_log=1
done
[[ $has_log -eq 1 ]] || exit 0 # no assistant log anywhere: no run, no ledger line
require_tools go gh python3 || exit 0
take_lock assistant_review || exit 0
dir="$state/$week"
mkdir -p "$dir"
run_begin assistant_review "" "" "$dir"

envs_csv=""
for env in $ENV_NAMES; do
  root="$(env_dir "$env")"
  alog="$root/data/monitor/assistant.jsonl"
  [[ -s "$alog" ]] || continue
  if ! go run ./cmd/assistantreview -log "$alog" -week "$week" -out "$root/data/monitor/assistant_reviews" > "$dir/$env.log" 2>&1; then
    log "assistant review: $env: cmd/assistantreview failed (log $dir/$env.log), skipped"; continue
  fi
  n="$(python3 "$US_PY" assistant-answers "$ENVS_DIR" "$env" "$week")"
  if [[ "$n" == "0" ]]; then
    log "assistant review: $env: no answer in $week, skipped"; continue
  fi
  log "assistant review: $env: $n answers in $week, review $root/data/monitor/assistant_reviews/$week.md"
  envs_csv="${envs_csv:+$envs_csv,}$env"
done
if [[ -z "$envs_csv" ]]; then
  log "assistant review: $week: no answer in any environment — nothing posted, no marker (retried in an hour)"
  date -u +%Y-%m-%dT%H:%M:%SZ > "$quiet"
  run_end skipped "no answers in the week"; exit 0
fi

attempts="$(cat "$state/$week.attempts" 2>/dev/null || echo 0)"
attempt=$((attempts + 1))
echo "$attempt" > "$state/$week.attempts"
comment="$dir/comment.md"
python3 "$US_PY" assistant-comment "$week" "$ENVS_DIR" "$envs_csv" > "$comment"
slug="$(repo_slug)"
posted=0
epic="$(gh issue list -R "$slug" --label epic --state open -L 200 --json number,title \
  -q '.[] | select(.title | startswith("Epic E10 ")) | .number' | head -1)" || epic=""
if [[ -z "$epic" ]]; then
  log "assistant review: no open E10 epic, summary not posted"
elif gh issue comment "$epic" -R "$slug" --body-file "$comment" >/dev/null; then
  log "assistant review: posted $week on #$epic"; posted=1
else
  log "assistant review: could not comment on #$epic"
fi
if [[ $posted -eq 1 || $attempt -ge 2 ]]; then
  date -u +%Y-%m-%dT%H:%M:%SZ > "$done_marker"
  run_end "$([[ $posted -eq 1 ]] && echo posted || echo failed)" "$envs_csv"
else
  log "assistant review: $week will be retried once on a later tick"
  run_end failed "comment not posted, retried once"
fi
