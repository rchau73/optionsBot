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
#   4. moves the board's "In review" cards whose issue is now closed (the PR
#      said "Closes #n") to Done, with a comment naming the commit.
# Stable is never touched here: a person promotes it (scripts/promote.sh).
# The agents run from the mvp clone, so this also keeps their scripts current.
#
# Env: DEPLOY_ENABLED (1), DEPLOY_HEALTH_TIMEOUT_SEC (300), AGENT_NOTIFY.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
[[ "${DEPLOY_ENABLED:-1}" == "1" ]] || { log "deploy: disabled"; exit 0; }
take_lock deploy || exit 0

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
[[ "$target" != "$deployed" ]] || { log "deploy: mvp up to date (${target:0:7})"; exit 0; }
[[ "$target" != "$(cat "$failed" 2>/dev/null || true)" ]] || { log "deploy: ${target:0:7} failed before; waiting for a new commit"; exit 0; }

mkdir -p "$AGENT_STATE_DIR/deploy"
exec > >(tee -a "$AGENT_STATE_DIR/deploy/$(date +%Y%m%d-%H%M%S)-${target:0:7}.log") 2>&1
log "deploy: mvp ${deployed:0:7} → ${target:0:7}"

url="$(env_monitor_url mvp)"
env="$(bots_environment "$url")"
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
if ! wait_healthy "$url" "${DEPLOY_HEALTH_TIMEOUT_SEC:-300}"; then
  echo "$target" > "$failed"
  log "deploy: mvp health check failed"
  notify "Deploy FAILED health check" "mvp ${target:0:7}: scripts/envctl.sh mvp logs"
  exit 1
fi
echo "$target" > "$state"
env_record deploys.tsv mvp "$target"
log "deploy: mvp ${target:0:7} healthy"

slug="$(repo_slug)"
items_in_status "In review" | while read -r item issue; do
  [[ "$(gh issue view "$issue" -R "$slug" --json state -q .state)" == "CLOSED" ]] || continue
  set_status "$item" "Done"
  gh issue comment "$issue" -R "$slug" --body "🚀 Deployed to **mvp** (testnet) in $(git -C "$dir" log -1 --format='%h (%s)' "$target"). The SME agent verifies it there in its next daily review; it reaches **stable** when a person promotes a release containing it (scripts/promote.sh)." >/dev/null
  log "deploy: #$issue → Done"
done || true
notify "Deployed to mvp" "${target:0:7}: $(git -C "$dir" log -1 --format=%s "$target")"
