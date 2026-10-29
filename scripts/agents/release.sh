#!/usr/bin/env bash
# release.sh — the automatic release of stable (#471). tick.sh starts it after
# a healthy deploy step, every tick:
#   - weekly: the first tick of an ISO week (Monday UTC) with no
#     data/agents/release/<week>.done releases the newest mvp build that has
#     soaked PROMOTE_SOAK_DAYS (3) and that stable does not contain, with the
#     next patch version; nothing to release → a one-line note on the E6 epic
#     saying why. Either way the marker is written: once a week.
#   - hotfix: any tick, a merged PR labelled (or closing an issue labelled)
#     `bug` + `release:hotfix` goes once its first mvp deploy has been live
#     PROMOTE_HOTFIX_HOURS (1).
#   Rules in release.py (pure, test_release.py); an open sme:not-fixed issue
#   blocks both. The release itself is promote.sh --commit <sha> --yes
#   (--weekly | --hotfix "<PRs>"): its own gates, the health check and the
#   rollback, and the `stable … release <tag> <trigger> <who> <why>` audit line
#   in deploys.tsv. Every release, good or failed, notifies and comments on
#   E6; a commit whose promotion failed is not retried
#   (data/agents/release/failed-<sha>).
# A person escalates by hand: promote.sh --commit <sha> --escalate "<reason>".
# Env: AUTO_RELEASE (1; 0 in agents.env turns it off), RELEASE_DRY_RUN (1 =
# decide and print, change nothing), PROMOTE_SOAK_DAYS, PROMOTE_HOTFIX_HOURS.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
[[ "${AUTO_RELEASE:-1}" == "1" ]] || exit 0
RELEASE_PY="$AGENT_REPO_DIR/scripts/agents/release.py"
dry="${RELEASE_DRY_RUN:-0}"
state="$AGENT_STATE_DIR/release"
mkdir -p "$state"
week="$(python3 "$RELEASE_PY" week)"
weekly=0; [[ -f "$state/$week.done" ]] || weekly=1
require_tools gh git python3 || exit 0
take_lock release || exit 0
stable="$(env_dir stable)"
[[ -d "$stable/.git" ]] || { log "release: no stable clone at $stable"; exit 0; }
git -C "$stable" fetch -q --tags origin || { log "release: could not fetch into $stable, next tick"; exit 0; }
slug="$(repo_slug)" || { log "release: GitHub unreachable, next tick"; exit 0; }
tmp="$(mktemp -d)"; on_exit_remove "$tmp"

git -C "$stable" tag -l 'v*' > "$tmp/tags"
# Never release blind: no answer about sme:not-fixed → nothing this tick.
gh issue list -R "$slug" --label sme:not-fixed --state open --json number,title \
  -q '.[] | "#\(.number) \(.title)"' > "$tmp/not_fixed" ||
  { log "release: could not read the sme:not-fixed issues, next tick"; exit 0; }
since="$(python3 -c 'import datetime as d; print((d.datetime.now(d.timezone.utc) - d.timedelta(days=14)).strftime("%Y-%m-%d"))')"
if ! gh issue list -R "$slug" --label bug --label release:hotfix --state all -L 100 --json number,labels > "$tmp/hotfix_issues.json" ||
  ! gh pr list -R "$slug" --state merged -L 200 --search "merged:>=$since" \
    --json number,title,body,labels,mergeCommit > "$tmp/prs.json"; then
  log "release: could not list the hotfixes: weekly check only this tick"
  echo '[]' > "$tmp/hotfix_issues.json"; echo '[]' > "$tmp/prs.json"
fi
due=(); [[ $weekly -eq 0 ]] || due=(--weekly)
python3 "$RELEASE_PY" decide --deploys "$ENV_HISTORY/deploys.tsv" --promotions "$ENV_HISTORY/promotions.tsv" \
  --tags "$tmp/tags" --prs "$tmp/prs.json" --hotfix-issues "$tmp/hotfix_issues.json" --not-fixed "$tmp/not_fixed" \
  --git "$stable" "${due[@]+"${due[@]}"}" \
  --soak-days "${PROMOTE_SOAK_DAYS:-3}" --hotfix-hours "${PROMOTE_HOTFIX_HOURS:-1}" > "$tmp/decision.json"
field() { py 'import json,sys; print(json.load(open(sys.argv[1]))[sys.argv[2]])' "$tmp/decision.json" "$1"; }
action="$(field action)"; trigger="$(field trigger)"; commit="$(field commit)"
tag="$(field tag)"; ref="$(field ref)"; note="$(field note)"

# e6 <body> — one comment on the open E6 epic (logged, never fatal).
e6() {
  local epic
  epic="$(gh issue list -R "$slug" --label epic --state open -L 200 --json number,title \
    -q '.[] | select(.title | startswith("Epic E6 ")) | .number' | head -1)" || epic=""
  if [[ -z "$epic" ]]; then log "release: no open E6 epic, note not posted"; return 0; fi
  gh issue comment "$epic" -R "$slug" --body "$1" >/dev/null || log "release: could not comment on #$epic"
}
mark_week() { [[ $weekly -eq 0 ]] || date -u +%Y-%m-%dT%H:%M:%SZ > "$state/$week.done"; }

if [[ "$action" != "release" ]]; then
  if [[ "$dry" == "1" ]]; then echo "dry run: no release — ${note:-nothing due}"; exit 0; fi
  [[ -z "$note" ]] || log "release: no release — $note"
  if [[ $weekly -eq 1 ]]; then
    e6 "🤖 **Weekly release** $week: no release — $note (\`scripts/agents/release.sh\`, #471)"
    mark_week
  fi
  exit 0
fi
if [[ -f "$state/failed-$commit" ]]; then
  log "release: ${commit:0:7} failed its promotion before ($(cat "$state/failed-$commit")): not retried"
  [[ "$dry" == "1" ]] || mark_week
  exit 0
fi
args=(--commit "$commit" --yes)
if [[ "$trigger" == "hotfix" ]]; then args+=(--hotfix "$ref"); else args+=(--weekly); fi
if [[ "$dry" == "1" ]]; then echo "dry run: would run promote.sh $tag ${args[*]} — $note"; exit 0; fi

# Only from a healthy mvp: a rollback that failed leaves mvp down while
# deploy.sh waits for a new commit.
if ! wait_healthy "$(env_monitor_url mvp)" "${RELEASE_MVP_HEALTH_SEC:-60}" "$(env_monitor_token mvp)"; then
  log "release: mvp is not healthy now — no release this tick"; exit 0
fi
run="$state/$(date -u +%Y%m%d-%H%M%S)-$tag"
mkdir -p "$run"
run_begin release "" "" "$run" # the run ledger (lib.sh): every exit below ends it
RUN_COMMIT="${commit:0:7}"
log "release: $trigger → $tag (${commit:0:7}) — $note"
if bash "$AGENT_REPO_DIR/scripts/promote.sh" "$tag" "${args[@]}" > "$run/promote.log" 2>&1; then
  log "release: stable is on $tag"
  notify "Released $tag to stable" "$trigger: ${commit:0:7}"
  e6 "🤖 **Release $tag** ($trigger) — stable is on \`${commit:0:7}\` and healthy. $note. Audit: the \`stable … release $tag $trigger\` line in \`deploys.tsv\` (\`scripts/agents/release.sh\`, #471)"
  mark_week
  run_end released "$tag $trigger"
  exit 0
fi
why="$(grep -E '^promote:' "$run/promote.log" | tail -1)"; why="${why:-see $run/promote.log}"
echo "$tag $(date -u +%Y-%m-%dT%H:%M:%SZ) $why" > "$state/failed-$commit"
log "release: $tag (${commit:0:7}) FAILED — $why"
notify "Release $tag FAILED" "$why"
e6 "🤖 **Release $tag FAILED** ($trigger, \`${commit:0:7}\`) — $why. Not retried for this commit; log on the runner: \`$run/promote.log\` (\`scripts/agents/release.sh\`, #471)"
mark_week
run_end failed "$tag: $why"
exit 1
