#!/usr/bin/env bash
# deploy.sh — continuous deployment to TESTNET after a person merges.
#
# When origin/main has moved past the last deployed commit:
#   1. refuses unless the bots run on testnet and this checkout is on main
#      with no local changes (it never deploys someone's work in progress);
#   2. fast-forwards, rebuilds and restarts the containers (the bots
#      reconcile with Deribit on every start, so a restart is safe);
#   3. waits for every bot's decision loop to report again (health check);
#   4. moves the board's "In review" cards whose issue is now closed (the PR
#      said "Closes #n") to Done, with a comment naming the commit.
# Live is never deployed automatically.
#
# Env: DEPLOY_ENABLED (1), DEPLOY_HEALTH_TIMEOUT_SEC (300), AGENT_NOTIFY.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
[[ "${DEPLOY_ENABLED:-1}" == "1" ]] || { log "deploy: disabled"; exit 0; }
take_lock deploy || exit 0

state="$AGENT_STATE_DIR/deployed_commit"
failed="$AGENT_STATE_DIR/failed_commit"
git fetch -q origin main
target="$(git rev-parse origin/main)"
deployed="$(cat "$state" 2>/dev/null || true)"
[[ "$target" != "$deployed" ]] || { log "deploy: up to date (${target:0:7})"; exit 0; }
[[ "$target" != "$(cat "$failed" 2>/dev/null || true)" ]] || { log "deploy: ${target:0:7} failed before; waiting for a new commit"; exit 0; }

mkdir -p "$AGENT_STATE_DIR/deploy"
exec > >(tee -a "$AGENT_STATE_DIR/deploy/$(date +%Y%m%d-%H%M%S)-${target:0:7}.log") 2>&1
log "deploy: ${deployed:0:7} → ${target:0:7}"

env="$(bots_environment)"
if [[ "$env" != "testnet" ]]; then
  log "deploy: bots are '$env' — automatic deploy is testnet-only"
  notify "Deploy skipped" "bots are $env: deploy ${target:0:7} by hand"
  exit 0
fi
branch="$(git rev-parse --abbrev-ref HEAD)"
if [[ "$branch" != "main" || -n "$(git status --porcelain --untracked-files=no)" ]]; then
  log "deploy: checkout is on '$branch' or has local changes — not touching it"
  notify "Deploy waiting" "switch $AGENT_REPO_DIR to a clean main to deploy ${target:0:7}"
  exit 0
fi

git merge -q --ff-only origin/main
log "deploy: building and restarting containers"
if ! docker compose up -d --build; then
  echo "$target" > "$failed"
  notify "Deploy FAILED" "docker compose up failed for ${target:0:7}"
  exit 1
fi

# Health: every bot's decision loop must report a fresh loop_at.
timeout="${DEPLOY_HEALTH_TIMEOUT_SEC:-300}"
start="$(date +%s)"
healthy=0
while (( $(date +%s) - start < timeout )); do
  sleep 15
  if py 'import json,re,sys,urllib.request,datetime as d
base = sys.argv[1]
bots = json.load(urllib.request.urlopen(base + "/api/bots", timeout=10))["bots"]
assert bots
for b in bots:
    st = json.load(urllib.request.urlopen(base + "/api/bots/" + b["name"] + "/status", timeout=10))
    at = d.datetime.fromisoformat(re.sub(r"(\.\d{6})\d+", r"\1", st["loop_at"]))  # Go: nanoseconds
    age = (d.datetime.now(d.timezone.utc) - at).total_seconds()
    assert age < 120, (b["name"], age)  # unchanged images are not restarted: freshness, not uptime' "$MONITOR_URL" 2>/dev/null; then
    healthy=1; break
  fi
done
if [[ $healthy -ne 1 ]]; then
  echo "$target" > "$failed"
  log "deploy: health check failed after ${timeout}s"
  notify "Deploy FAILED health check" "${target:0:7}: check docker compose logs"
  exit 1
fi
echo "$target" > "$state"
log "deploy: ${target:0:7} healthy"

slug="$(repo_slug)"
items_in_status "In review" | while read -r item issue; do
  [[ "$(gh issue view "$issue" -R "$slug" --json state -q .state)" == "CLOSED" ]] || continue
  set_status "$item" "Done"
  gh issue comment "$issue" -R "$slug" --body "🚀 Deployed to **testnet** in $(git log -1 --format='%h (%s)' "$target"). The SME agent verifies it in its next daily review." >/dev/null
  log "deploy: #$issue → Done"
done
notify "Deployed to testnet" "${target:0:7}: $(git log -1 --format=%s "$target")"
