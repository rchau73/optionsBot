#!/usr/bin/env bash
# assistant_golden.sh — the assistant's weekly live golden-set run (#536), the
# step usage.sh runs on every tick after the quality review (assistant_review.sh);
# it does nothing once last week's done-marker exists:
#   in ASSISTANT_GOLDEN_ENV (default mvp: main, where a prompt, model or help
#   change lands first) when its .env has ANTHROPIC_API_KEY:
#     bash scripts/product/assistant_run.sh --env <env> --week <week>
#     → every golden question asked to the real assistant (a test run: its own
#       log, outside the per-person limit, never counted by the quality review),
#       graded and compared with the baseline in <clone>/data/monitor/assistant_reviews/;
#   one comment on the E10 epic (assistant_golden.py comment: counts and
#   percentages per category, the regressions named; no question or answer).
# A regression (exit 1) is reported, not a failure. No key → nothing runs, no
# ledger line (the person adds it, docs/product/assistant/README.md). A run
# that failed is tried once more on a later tick, then the week is marked done;
# a run whose comment could not be posted is posted once more without asking
# the model again: data/agents/assistant_golden/<week>.done.
# Cost: one run a week, ≈ 115 model calls (README "Live golden-set run").
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
GOLDEN_PY="$AGENT_REPO_DIR/scripts/product/assistant_golden.py"
week="$(python3 "$AGENT_REPO_DIR/scripts/agents/usage_week.py" week)"
state="$AGENT_STATE_DIR/assistant_golden"
done_marker="$state/$week.done"
[[ -f "$done_marker" ]] && exit 0
env="${ASSISTANT_GOLDEN_ENV:-mvp}"
root="$(env_dir "$env")"
[[ -n "$(env_file_var "$root/.env" ANTHROPIC_API_KEY)" ]] || exit 0 # the assistant is off there: no run, no ledger line
require_tools node python3 gh || exit 0
take_lock assistant_golden || exit 0
dir="$state/$week"
mkdir -p "$dir"
run_begin assistant_golden "" "" "$dir"

attempts="$(cat "$state/$week.attempts" 2>/dev/null || echo 0)"
attempt=$((attempts + 1))
echo "$attempt" > "$state/$week.attempts"
summary="$root/data/monitor/assistant_reviews/$week.golden.summary.json"
if [[ ! -f "$summary" ]]; then
  rc=0
  bash "$AGENT_REPO_DIR/scripts/product/assistant_run.sh" --env "$env" --week "$week" > "$dir/run.log" 2>&1 || rc=$?
  if [[ $rc -ne 0 && $rc -ne 1 ]] || [[ ! -f "$summary" ]]; then
    log "assistant golden run: $env: the run failed (exit $rc, log $dir/run.log)"
    if [[ $attempt -ge 2 ]]; then
      date -u +%Y-%m-%dT%H:%M:%SZ > "$done_marker"
      run_end failed "run failed twice, week marked done"
    else
      run_end failed "run failed, retried once"
    fi
    exit 0
  fi
  [[ $rc -eq 1 ]] && log "assistant golden run: $env: regression in $week, see $summary"
fi

comment="$dir/comment.md"
python3 "$GOLDEN_PY" comment "$summary" > "$comment"
slug="$(repo_slug)"
posted=0
epic="$(gh issue list -R "$slug" --label epic --state open -L 200 --json number,title \
  -q '.[] | select(.title | startswith("Epic E10 ")) | .number' | head -1)" || epic=""
if [[ -z "$epic" ]]; then
  log "assistant golden run: no open E10 epic, summary not posted"
elif gh issue comment "$epic" -R "$slug" --body-file "$comment" >/dev/null; then
  log "assistant golden run: posted $week on #$epic"; posted=1
else
  log "assistant golden run: could not comment on #$epic"
fi
if [[ $posted -eq 1 || $attempt -ge 2 ]]; then
  date -u +%Y-%m-%dT%H:%M:%SZ > "$done_marker"
  run_end "$([[ $posted -eq 1 ]] && echo posted || echo failed)" "$env"
else
  log "assistant golden run: $week will be posted once more on a later tick"
  run_end failed "comment not posted, retried once"
fi
