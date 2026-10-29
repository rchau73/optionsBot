#!/usr/bin/env bash
# dora.sh — the delivery report: DORA metrics and each PR's time live (#470).
# tick.sh starts it every tick; it does nothing once today's (UTC) done-marker
# exists, so the first tick of a day runs it:
#   dumps main's first-parent history (with each merge's first commit), the
#   release tags, the merged PRs and the issues (gh) to data/agents/dora/<date>/;
#   go run ./cmd/dora on them with <envs>/history/deploys.tsv and promotions.tsv
#     → <envs>/history/delivery/dora.json and <week>.md (every week from
#       DORA_FROM, 2026-10-02);
#   once a week is over (the first run of a Monday, retried on later days until
#   it is posted): that week's four numbers per environment as one comment on
#   the E6 epic (data/agents/dora/<week>.posted).
# Reads git and GitHub; writes only the report and that one comment. No Claude
# call, no bot state, no order.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
FROM="${DORA_FROM:-2026-10-02}"
SINCE="${DORA_SINCE:-2026-09-21}" # git and gh look back to here (the backfill's weeks start on FROM's Monday)
day="$(date -u +%Y-%m-%d)"
state="$AGENT_STATE_DIR/dora"
done_marker="$state/$day.done"
[[ -f "$done_marker" ]] && exit 0
require_tools go gh git python3 || exit 0
take_lock dora || exit 0
dir="$state/$day"
mkdir -p "$dir"
run_begin dora "" "" "$dir" # the run ledger (lib.sh): every exit below ends it
slug="$(repo_slug)"
out="$ENV_HISTORY/delivery"

git_locked fetch -q --tags origin || log "dora: git fetch failed, using the local origin/main"
# Main's first-parent line, newest first; for each merge the author time of the
# earliest commit it brought in (merge^1..merge^2). cmd/dora's main.tsv format.
: > "$dir/main.tsv"
git log --first-parent --since="$SINCE" origin/main --format='%H%x09%P%x09%ct%x09%s' > "$dir/log.tsv"
while IFS=$'\t' read -r sha parents ct subject; do
  first=""
  read -r p1 p2 _ <<< "$parents"
  if [[ -n "${p2:-}" ]]; then
    first="$(git log --format=%at "$p1..$p2" | sort -n | awk 'NR == 1')"
  fi
  printf '%s\t%s\t%s\t%s\t%s\n' "$sha" "$parents" "$ct" "$first" "$subject" >> "$dir/main.tsv"
done < "$dir/log.tsv"
git for-each-ref 'refs/tags/v*' --format='%(refname:short)%09%(*objectname)%09%(objectname)' |
  awk -F'\t' '{ print $1 "\t" ($2 != "" ? $2 : $3) }' > "$dir/tags.tsv"
# A file gh could not write is removed: cmd/dora then lists it as not read.
if ! gh pr list -R "$slug" --state merged -L 2000 --search "merged:>=$SINCE" \
  --json number,title,headRefName,body,createdAt,mergedAt,labels > "$dir/prs.json"; then
  log "dora: could not list the merged PRs: no titles or epics, no reverts this run"; rm -f "$dir/prs.json"
fi
if ! gh issue list -R "$slug" --state all -L 2000 --search "created:>=$SINCE" \
  --json number,title,body,labels,createdAt,closedAt > "$dir/issues.json"; then
  log "dora: could not list the issues: no deploy-failed or regression failures this run"; rm -f "$dir/issues.json"
fi

# Last week's four numbers go on the E6 epic once, so -summary names it.
week="$(python3 -c 'import datetime as d; y, w, _ = (d.datetime.now(d.timezone.utc).date() - d.timedelta(days=7)).isocalendar(); print(f"{y}-W{w:02d}")')"
if ! go run ./cmd/dora -main "$dir/main.tsv" -tags "$dir/tags.tsv" -prs "$dir/prs.json" -issues "$dir/issues.json" \
  -deploys "$ENV_HISTORY/deploys.tsv" -promotions "$ENV_HISTORY/promotions.tsv" \
  -from "$FROM" -out "$out" -summary "$week" > "$dir/dora.out" 2> "$dir/dora.err"; then
  log "dora: cmd/dora failed (log $dir/dora.err)"
  run_end failed "cmd/dora failed"; exit 1
fi
log "$(head -1 "$dir/dora.out")"
skipped="$(grep -c '^skipped:' "$dir/dora.err" || true)"
[[ "$skipped" == "0" ]] || log "dora: $skipped input line(s) reported and skipped (see $dir/dora.err)"
date -u +%Y-%m-%dT%H:%M:%SZ > "$done_marker"

posted_marker="$state/$week.posted"
if [[ -f "$posted_marker" ]]; then
  run_end written "report in $out; $week already posted"; exit 0
fi
{ echo "🤖 **Delivery (DORA)** — week $week (\`scripts/agents/dora.sh\`, #470)"; echo
  sed 1d "$dir/dora.out"
  echo; echo "Every PR with its merge and live times: \`<envs>/history/delivery/dora.json\` and \`$week.md\`."; } > "$dir/comment.md"
epic="$(gh issue list -R "$slug" --label epic --state open -L 200 --json number,title \
  -q '.[] | select(.title | startswith("Epic E6 ")) | .number' | head -1)" || epic=""
if [[ -z "$epic" ]]; then
  log "dora: no open E6 epic, $week not posted"
  run_end written "report in $out; no E6 epic"
elif gh issue comment "$epic" -R "$slug" --body-file "$dir/comment.md" >/dev/null; then
  date -u +%Y-%m-%dT%H:%M:%SZ > "$posted_marker"
  log "dora: posted $week on #$epic"
  run_end posted "report in $out; $week on #$epic"
else
  log "dora: could not comment on #$epic, retried tomorrow"
  run_end written "report in $out; comment not posted"
fi
