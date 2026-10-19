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

dir="$(env_dir mvp)"
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
[[ "$target" != "$(cat "$failed" 2>/dev/null || true)" ]] || { log "deploy: ${target:0:7} failed before; waiting for a new commit"; exit 0; }

mkdir -p "$AGENT_STATE_DIR/deploy"
exec > >(tee -a "$AGENT_STATE_DIR/deploy/$(date +%Y%m%d-%H%M%S)-${target:0:7}.log") 2>&1
log "deploy: mvp ${deployed:0:7} → ${target:0:7}"

url="$(env_monitor_url mvp)"
token="$(env_monitor_token mvp)"
env="$(bots_environment "$url" "$token")"
if [[ "$env" == "live" ]]; then
  log "deploy: mvp bots are live — automatic deploy is testnet-only"
  notify "Deploy skipped" "mvp bots are live: deploy ${target:0:7} by hand"
  exit 0
fi
branch="$(git -C "$dir" rev-parse --abbrev-ref HEAD)"
if [[ "$branch" != "main" || -n "$(git -C "$dir" status --porcelain --untracked-files=no)" ]]; then
  log "deploy: $dir is on '$branch' or has local changes — not touching it"
  notify "Deploy waiting" "the mvp clone must be a clean main to deploy ${target:0:7}"
  exit 0
fi

git -C "$dir" merge -q --ff-only origin/main
log "deploy: building and restarting mvp ($(env_version mvp))"
if ! env_compose mvp up -d --build; then
  echo "$target" > "$failed"
  notify "Deploy FAILED" "mvp: docker compose up failed for ${target:0:7}"
  exit 1
fi
if ! wait_healthy "$url" "${DEPLOY_HEALTH_TIMEOUT_SEC:-300}" "$token"; then
  echo "$target" > "$failed"
  log "deploy: mvp health check failed"
  notify "Deploy FAILED health check" "mvp ${target:0:7}: scripts/envctl.sh mvp logs"
  exit 1
fi
echo "$target" > "$state"
env_record deploys.tsv mvp "$target"
log "deploy: mvp ${target:0:7} healthy"

close_out "$target"
"$AGENT_REPO_DIR/scripts/prune_branches.sh" "$dir" | tail -1 || true # merged agent branches
notify "Deployed to mvp" "${target:0:7}: $(git -C "$dir" log -1 --format=%s "$target")"
