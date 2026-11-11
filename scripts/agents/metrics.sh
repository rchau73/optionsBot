#!/usr/bin/env bash
# metrics.sh — the weekly agent metrics report (#180). tick.sh starts it every
# tick; it does nothing until last week's report is missing, so the first tick
# of a Monday (00:00 UTC, or the first after the Mac wakes) writes it:
#   data/agents/metrics/<YYYY-Www>.md, from GitHub (issues, agent PRs, label
#   events), main's git log, the run ledger and the stuck-card log
#   (metrics.py, offline and tested);
#   the same report as one comment on the E6 epic;
#   one `bug` issue "[E6] Agent health: <agent> <metric> <week>" in Triage per
#   agent whose failed % or limit % crossed the threshold (#755, metrics.py
#   health: at most 3, else one combined issue; deduplicated by title).
# No Claude call. Read-only on GitHub except that comment and those issues.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
METRICS_PY="$AGENT_REPO_DIR/scripts/agents/metrics.py"
read -r week _ since < <(python3 "$METRICS_PY" week)
out_dir="$AGENT_STATE_DIR/metrics"
report="$out_dir/$week.md"
[[ -f "$report" ]] && exit 0
take_lock metrics || exit 0
run_begin metrics "" "" "$out_dir" # the run ledger (lib.sh): every exit below ends it
slug="$(repo_slug)"
tmp="$AGENT_STATE_DIR/metrics-$$"; mkdir -p "$tmp" "$out_dir"; on_exit_remove "$tmp"

# Everything touched since three weeks before the week began, for the Trend
# section, or 30 days before it ended, for the flaky repeat offenders (#747),
# whichever is earlier: an issue or PR with an event in a week was updated in
# it or later (each carries all its comments and reviews).
gh issue list -R "$slug" --state all -L 1000 --search "updated:>=$since" \
  --json number,title,labels,createdAt,closedAt,state,stateReason,comments > "$tmp/issues.json"
gh pr list -R "$slug" --state all -L 500 --search "updated:>=$since" \
  --json number,title,headRefName,headRefOid,labels,createdAt,mergedAt,closedAt,state,reviews > "$tmp/prs.json"
# Label events (the repository keeps 90 days): waits for a person.
gh api "repos/$slug/issues/events" --paginate \
  -q '.[] | select(.event == "labeled" or .event == "unlabeled") | {event, label: .label.name, created_at, issue: .issue.number}' \
  > "$tmp/events.json"
git_locked fetch -q origin
git log origin/main --max-count=3000 --format='%cI%x09%s' > "$tmp/gitlog.txt" # metrics.py keeps the weeks

# The run ledger (#290) and the stuck-card log (#329): a missing file only
# says so in its section.
python3 "$METRICS_PY" report "$week" "$tmp" --runs "$AGENT_RUNS_FILE" \
  --stuck "$AGENT_STATE_DIR/stuck/reports.jsonl" --repo "$slug" > "$tmp/report.md"
mv "$tmp/report.md" "$report"
log "metrics: wrote $report"

epic="$(gh issue list -R "$slug" --label epic --state open -L 200 --json number,title \
  -q '.[] | select(.title | startswith("Epic E6 ")) | .number' | head -1)" || epic=""
outcome=written detail="report in $report" link=""
if [[ -z "$epic" ]]; then
  log "metrics: no open E6 epic, report not posted"; detail="$detail; no E6 epic"
else
  { echo "🤖 **Agent metrics** — week $week (\`scripts/agents/metrics.sh\`, #180)"; echo; sed 1d "$report"; } > "$tmp/comment.md"
  if link="$(gh issue comment "$epic" -R "$slug" --body-file "$tmp/comment.md")"; then
    log "metrics: posted $week on #$epic"; outcome=posted detail="$detail; $week on #$epic"
  else
    log "metrics: could not comment on #$epic"; detail="$detail; comment not posted"; link=""
  fi
fi

# Agent health (#755): one bug in Triage per agent whose failed % or limit % crossed
# the threshold (metrics.py health: at most 3, else one combined issue), deduplicated
# by title against every issue already filed, so a re-run for the week files nothing twice.
filed=0
if gh issue list -R "$slug" --state all -L 300 --search "\"Agent health\" in:title" \
  --json number,title > "$tmp/health_issues.json"; then
  python3 "$METRICS_PY" health "$week" "$tmp/health" --runs "$AGENT_RUNS_FILE" \
    --issues "$tmp/health_issues.json" --link "$link" > "$tmp/health.log"
  while IFS= read -r line; do log "metrics: $line"; done < "$tmp/health.log"
  for title_file in "$tmp"/health/*.title; do
    [[ -f "$title_file" ]] || continue
    title="$(cat "$title_file")"
    if url="$(gh issue create -R "$slug" --label bug --title "$title" --body-file "${title_file%.title}.body")"; then
      filed=$((filed + 1))
      add_to_board "$url" "Triage" >/dev/null || log "metrics: $url filed but not put on the board"
      log "metrics: filed $url"
    else
      log "metrics: could not file \"$title\""
    fi
  done
else
  log "metrics: could not list issues, the agent-health check files nothing"
fi
run_end "$outcome" "$detail; $filed agent-health issue(s) filed"
