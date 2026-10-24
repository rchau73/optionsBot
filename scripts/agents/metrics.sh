#!/usr/bin/env bash
# metrics.sh — the weekly agent metrics report (#180). tick.sh starts it every
# tick; it does nothing until last week's report is missing, so the first tick
# of a Monday (00:00 UTC, or the first after the Mac wakes) writes it:
#   data/agents/metrics/<YYYY-Www>.md, from GitHub (issues, agent PRs, label
#   events), main's git log, the run ledger and the stuck-card log
#   (metrics.py, offline and tested);
#   the same report as one comment on the E6 epic.
# No Claude call. Read-only on GitHub except that one comment.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
METRICS_PY="$AGENT_REPO_DIR/scripts/agents/metrics.py"
read -r week since < <(python3 "$METRICS_PY" week)
out_dir="$AGENT_STATE_DIR/metrics"
report="$out_dir/$week.md"
[[ -f "$report" ]] && exit 0
take_lock metrics || exit 0
slug="$(repo_slug)"
tmp="$AGENT_STATE_DIR/metrics-$$"; mkdir -p "$tmp" "$out_dir"; on_exit_remove "$tmp"

# Everything touched since the week began: an issue or PR with an event in the
# week was updated in it (each carries all its comments and reviews).
gh issue list -R "$slug" --state all -L 1000 --search "updated:>=$since" \
  --json number,title,labels,createdAt,closedAt,state,stateReason,comments > "$tmp/issues.json"
gh pr list -R "$slug" --state all -L 500 --search "updated:>=$since" \
  --json number,title,headRefName,headRefOid,labels,createdAt,mergedAt,closedAt,state,reviews > "$tmp/prs.json"
# Label events (the repository keeps 90 days): waits for a person.
gh api "repos/$slug/issues/events" --paginate \
  -q '.[] | select(.event == "labeled" or .event == "unlabeled") | {event, label: .label.name, created_at, issue: .issue.number}' \
  > "$tmp/events.json"
git_locked fetch -q origin
git log origin/main --max-count=1000 --format='%cI%x09%s' > "$tmp/gitlog.txt" # metrics.py keeps the week

# The run ledger (#290) and the stuck-card log (#329): a missing file only
# says so in its section.
python3 "$METRICS_PY" report "$week" "$tmp" --runs "$AGENT_RUNS_FILE" \
  --stuck "$AGENT_STATE_DIR/stuck/reports.jsonl" --repo "$slug" > "$tmp/report.md"
mv "$tmp/report.md" "$report"
log "metrics: wrote $report"

epic="$(gh issue list -R "$slug" --label epic --state open -L 200 --json number,title \
  -q '.[] | select(.title | startswith("Epic E6 ")) | .number' | head -1)"
if [[ -z "$epic" ]]; then log "metrics: no open E6 epic, report not posted"; exit 0; fi
{ echo "🤖 **Agent metrics** — week $week (\`scripts/agents/metrics.sh\`, #180)"; echo; sed 1d "$report"; } > "$tmp/comment.md"
gh issue comment "$epic" -R "$slug" --body-file "$tmp/comment.md" >/dev/null &&
  log "metrics: posted $week on #$epic" || log "metrics: could not comment on #$epic"
