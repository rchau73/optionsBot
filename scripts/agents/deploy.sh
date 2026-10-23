#!/usr/bin/env bash
# deploy.sh — continuous deployment of main to the MVP environment.
#
# When origin/main has moved past the commit mvp runs:
#   1. refuses unless mvp is set up (scripts/envctl.sh mvp check: its own
#      Deribit account and port), its bots run on testnet, and its clone is
#      on main with no local changes;
#   2. fast-forwards the mvp clone, rebuilds and restarts its stack (the bots
#      reconcile with Deribit on every start, so a restart is safe);
#   3. waits for every bot's decision loop to report again (health check) and
#      records the deploy in <envs>/history/deploys.tsv — the soak
#      clock scripts/promote.sh reads;
#      on a failed compose or health check (#204): resets the clone to the
#      commit that last passed (mvp.deployed), rebuilds and health-checks it,
#      records it as a `rollback` line in deploys.tsv, keeps the bad commit in
#      mvp.failed (not retried until main moves) and files one `bug` issue on
#      the board's Triage — the failed step, the last 80 log lines (secrets
#      redacted, deploy_issue.py) and the commit range; deduplicated by the
#      commit in its title, re-tried each tick until filed. A rollback that
#      fails too is reported loudly and stops: nothing retries it;
#   4. moves every card whose issue is now closed (the PR said "Closes #n",
#      or a person closed it) to Done; a card that was In review or In
#      progress gets a comment naming the deployed commit.
#   5. deletes the mvp clone's local branches already in main (the Dev agent's
#      agent/issue-<n>; GitHub deletes the remote one at merge).
# With mvp up to date, a tick still moves closed cards to Done (no comment):
# the board's "Item closed" workflow is off, and an issue closed by hand
# would otherwise stay in its column (2026-10-17: #131, #133 In progress).
# Stable is never touched here: a person promotes it (scripts/promote.sh).
# The agents run from the mvp clone, so this also keeps their scripts current.
#
# Env: DEPLOY_ENABLED (1), DEPLOY_HEALTH_TIMEOUT_SEC (300), AGENT_NOTIFY.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
[[ "${DEPLOY_ENABLED:-1}" == "1" ]] || { log "deploy: disabled"; exit 0; }
take_lock deploy || exit 0

# close_out [commit] — every closed issue whose card is not in Done moves to
# Done. With a commit (just deployed), cards that were In review or In
# progress are told where it runs. Epics carry no Status and are left alone.
close_out() {
  local commit="${1:-}" slug item issue status
  slug="$(repo_slug)"
  board_json | py 'import json,sys
for c in json.load(sys.stdin):
    if c["state"] == "CLOSED" and c["status"] not in ("Done", "-"):
        print(c["item"], c["number"], c["status"].replace(" ", "_"))' |
    while read -r item issue status; do
      set_status "$item" "Done" || continue
      if [[ -n "$commit" && ( "$status" == "In_review" || "$status" == "In_progress" ) ]]; then
        gh issue comment "$issue" -R "$slug" --body "🚀 Deployed to **mvp** (testnet) in $(git -C "$dir" log -1 --format='%h (%s)' "$commit"). The SME agent verifies it there in its next daily review; it reaches **stable** when a person promotes a release containing it (scripts/promote.sh)." >/dev/null
      fi
      log "deploy: #$issue (${status//_/ }, closed) → Done"
    done || true
}

# file_issue <sha> — the bug issue for a failed deploy of <sha>, once: the
# step, rollback and log tail saved by fail() are in $faildir/<sha>.*; an
# issue whose title already names <sha> (open or closed) is reused.
file_issue() {
  local sha="$1" slug found url subject f="$faildir/$1"
  [[ -f "$f.meta" && ! -f "$f.issue" ]] || return 0
  slug="$(repo_slug)" || { log "deploy: GitHub unreachable — the issue for ${sha:0:7} is filed next tick"; return 0; }
  found="$(gh issue list -R "$slug" --state all --search "${sha:0:7} in:title" --json number,title -L 20 |
    python3 "$AGENT_REPO_DIR/scripts/agents/deploy_issue.py" find "$sha")" ||
    { log "deploy: could not search the issues — the issue for ${sha:0:7} is filed next tick"; return 0; }
  if [[ -n "$found" ]]; then
    echo "$found" > "$f.issue"; log "deploy: #$found already reports ${sha:0:7} — nothing filed"; return 0
  fi
  subject="$(git -C "$dir" log -1 --format=%s "$sha" 2>/dev/null || echo unknown)"
  python3 "$AGENT_REPO_DIR/scripts/agents/deploy_issue.py" body "$sha" "$f.meta" "$f.tail" "$f.range" "$dir/.env" "$dir/deploy.env" > "$f.body"
  url="$(gh issue create -R "$slug" --label bug --body-file "$f.body" \
    --title "$(python3 "$AGENT_REPO_DIR/scripts/agents/deploy_issue.py" title "$sha" "$subject")")" ||
    { log "deploy: could not file the issue for ${sha:0:7} — retried next tick"; return 0; }
  echo "${url##*/}" > "$f.issue"
  add_to_board "$url" "Triage" >/dev/null || log "deploy: $url filed but not put on the board"
  log "deploy: filed $url"
}

# fail <step> <log of the step> — the target failed: keep it in mvp.failed,
# roll mvp back to the last healthy commit, file the bug issue, stop.
fail() {
  local step="$1" f="$faildir/$target" rollback outcome
  echo "$target" > "$failed"
  tail -n 200 "$2" > "$f.tail" 2>/dev/null || true
  git -C "$dir" log --oneline "${deployed:+$deployed..}$target" 2>/dev/null | head -30 > "$f.range" || true
  log "deploy: mvp ${target:0:7} failed ($step)"
  if [[ -z "$deployed" ]]; then
    rollback="none: mvp has no earlier healthy deploy"
    outcome=failed
    notify "Deploy FAILED" "mvp ${target:0:7}: $step; nothing to roll back to"
  elif log "deploy: rolling mvp back to ${deployed:0:7}" && git -C "$dir" reset -q --hard "$deployed" &&
    env_compose mvp up -d --build && wait_healthy "$url" "${DEPLOY_HEALTH_TIMEOUT_SEC:-300}" "$token"; then
    rollback="mvp rolled back to \`${deployed:0:7}\`, healthy"
    outcome=rolled_back
    env_record deploys.tsv mvp "$deployed" rollback
    log "deploy: mvp rolled back to ${deployed:0:7}, healthy"
    notify "Deploy FAILED, rolled back to ${deployed:0:7}" "mvp ${target:0:7}: $step"
  else
    rollback="**FAILED**: the rollback to \`${deployed:0:7}\` did not come back healthy, mvp is down (scripts/envctl.sh mvp logs)"
    outcome=failed
    log "deploy: ROLLBACK to ${deployed:0:7} FAILED — mvp is down; scripts/envctl.sh mvp logs"
    notify "Deploy FAILED, ROLLBACK FAILED" "mvp is down: ${target:0:7} and ${deployed:0:7} both unhealthy"
  fi
  printf '%s\n%s\n' "$step" "$rollback" > "$f.meta"
  if [[ "$outcome" == "rolled_back" ]]; then run_end rolled_back "$step failed: back to ${deployed:0:7}"
  else run_end failed "$step failed; rollback: ${deployed:+failed}${deployed:-none}"; fi
  file_issue "$target"
  exit 1
}

dir="$(env_dir mvp)"
faildir="$AGENT_STATE_DIR/deploy/failed"
if ! why="$(env_check mvp)"; then
  log "deploy: mvp is not ready ($why) — nothing deployed"; exit 0
fi
mkdir -p "$ENV_HISTORY"
state="$ENV_HISTORY/mvp.deployed"
failed="$ENV_HISTORY/mvp.failed"
git -C "$dir" fetch -q origin main
target="$(git -C "$dir" rev-parse origin/main)"
deployed="$(cat "$state" 2>/dev/null || true)"
if [[ "$target" == "$deployed" ]]; then
  log "deploy: mvp up to date (${target:0:7})"; close_out; exit 0
fi
if [[ "$target" == "$(cat "$failed" 2>/dev/null || true)" ]]; then
  log "deploy: ${target:0:7} failed before; waiting for a new commit"
  file_issue "$target" # when GitHub was unreachable at the failure
  exit 0
fi

mkdir -p "$AGENT_STATE_DIR/deploy" "$faildir"
deploy_log="$AGENT_STATE_DIR/deploy/$(date +%Y%m%d-%H%M%S)-${target:0:7}.log"
exec > >(tee -a "$deploy_log") 2>&1
# The run ledger (lib.sh): only a tick that tries a new commit is a run.
run_begin deploy "" "" "$deploy_log"
RUN_COMMIT="${target:0:7}"
log "deploy: mvp ${deployed:0:7} → ${target:0:7}"

url="$(env_monitor_url mvp)"
token="$(env_monitor_token mvp)"
env="$(bots_environment "$url" "$token")"
if [[ "$env" == "live" ]]; then
  log "deploy: mvp bots are live — automatic deploy is testnet-only"
  notify "Deploy skipped" "mvp bots are live: deploy ${target:0:7} by hand"
  run_end skipped "mvp bots are live: deploy by hand"
  exit 0
fi
branch="$(git -C "$dir" rev-parse --abbrev-ref HEAD)"
if [[ "$branch" != "main" || -n "$(git -C "$dir" status --porcelain --untracked-files=no)" ]]; then
  log "deploy: $dir is on '$branch' or has local changes — not touching it"
  notify "Deploy waiting" "the mvp clone must be a clean main to deploy ${target:0:7}"
  run_end skipped "mvp clone not a clean main"
  exit 0
fi

git -C "$dir" merge -q --ff-only origin/main
log "deploy: building and restarting mvp ($(env_version mvp))"
step_log="$faildir/$target.step.log"
rc=0; env_compose mvp up -d --build > "$step_log" 2>&1 || rc=$?
cat "$step_log"
[[ $rc -eq 0 ]] || fail "docker compose up --build" "$step_log"
if ! wait_healthy "$url" "${DEPLOY_HEALTH_TIMEOUT_SEC:-300}" "$token"; then
  env_compose mvp logs --no-color --tail 80 > "$step_log" 2>&1 || true
  fail "health check (no fresh decision loop within ${DEPLOY_HEALTH_TIMEOUT_SEC:-300} s)" "$step_log"
fi
rm -f "$step_log"
echo "$target" > "$state"
env_record deploys.tsv mvp "$target"
log "deploy: mvp ${target:0:7} healthy"
run_end deployed "healthy on mvp"

close_out "$target"
"$AGENT_REPO_DIR/scripts/prune_branches.sh" "$dir" | tail -1 || true # merged agent branches
notify "Deployed to mvp" "${target:0:7}: $(git -C "$dir" log -1 --format=%s "$target")"
