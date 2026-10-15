# lib.sh — shared helpers for the agents (sourced, not run). Bash 3.2-safe:
# launchd runs macOS /bin/bash.
#
# GitHub is the control plane: issues are the work queue, the Project board's
# Status column is the workflow, PRs are the audit trail. Helpers read the
# board through `gh` (needs the `project` token scope).

AGENT_REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
AGENT_STATE_DIR="$AGENT_REPO_DIR/data/agents"          # gitignored
AGENT_WORK_DIR="${AGENT_WORK_DIR:-$HOME/.optionsbot-agents}" # worktrees, outside the repo
MONITOR_URL="${MONITOR_URL:-http://127.0.0.1:3000}"
PROJECT_TITLE="${PROJECT_TITLE:-optionsBot · agentic workflow}"
mkdir -p "$AGENT_STATE_DIR" "$AGENT_WORK_DIR"


log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"; }

notify() { # title, message
  [[ "${AGENT_NOTIFY:-1}" == "1" ]] || return 0
  command -v osascript >/dev/null || return 0
  local t="${1//\"/\'}" m="${2//\"/\'}"
  osascript -e "display notification \"$m\" with title \"$t\"" >/dev/null 2>&1 || true
}

# py <code> [args...] — run a small Python 3 snippet (JSON handling).
py() { local code="$1"; shift; python3 -c "$code" "$@"; }

repo_slug() { gh repo view --json nameWithOwner -q .nameWithOwner; }
repo_owner() { repo_slug | cut -d/ -f1; }

# project_info → "NUMBER PROJECT_ID STATUS_FIELD_ID" of the board, or fails.
project_info() {
  local owner num pid fid
  owner="$(repo_owner)"
  num="$(gh project list --owner "$owner" --format json -L 100 |
    py 'import json,sys; t=sys.argv[1]; print(next((str(p["number"]) for p in json.load(sys.stdin)["projects"] if p["title"]==t), ""))' "$PROJECT_TITLE")"
  [[ -n "$num" ]] || { log "project '$PROJECT_TITLE' not found: run scripts/agents/setup_github.sh"; return 1; }
  pid="$(gh project view "$num" --owner "$owner" --format json -q .id)"
  fid="$(gh project field-list "$num" --owner "$owner" --format json |
    py 'import json,sys; print(next(f["id"] for f in json.load(sys.stdin)["fields"] if f["name"]=="Status"))')"
  echo "$num $pid $fid"
}

# status_option_id <status name> — option id of a Status column ("In review").
status_option_id() {
  local owner num
  owner="$(repo_owner)"
  num="$(project_info | cut -d' ' -f1)"
  gh project field-list "$num" --owner "$owner" --format json |
    py 'import json,sys; want=sys.argv[1]
f=next(f for f in json.load(sys.stdin)["fields"] if f["name"]=="Status")
print(next(o["id"] for o in f["options"] if o["name"]==want))' "$1"
}

# items_in_status <status> → lines "ITEM_ID ISSUE_NUMBER" for this repo's issues.
items_in_status() {
  local owner num slug
  owner="$(repo_owner)"; slug="$(repo_slug)"
  num="$(project_info | cut -d' ' -f1)"
  gh project item-list "$num" --owner "$owner" --format json -L 500 |
    py 'import json,sys; want, slug = sys.argv[1], sys.argv[2]
for it in json.load(sys.stdin)["items"]:
    c = it.get("content") or {}
    if it.get("status") == want and c.get("type") == "Issue" and c.get("repository") == slug:
        print(it["id"], c["number"])' "$1" "$slug" | sort -k2 -n
}

# item_for_issue <issue number> → project item id (empty when not on the board).
item_for_issue() {
  local owner num slug
  owner="$(repo_owner)"; slug="$(repo_slug)"
  num="$(project_info | cut -d' ' -f1)"
  gh project item-list "$num" --owner "$owner" --format json -L 500 |
    py 'import json,sys; n, slug = int(sys.argv[1]), sys.argv[2]
for it in json.load(sys.stdin)["items"]:
    c = it.get("content") or {}
    if c.get("type") == "Issue" and c.get("number") == n and c.get("repository") == slug:
        print(it["id"]); break' "$1" "$slug"
}

# set_status <item id> <status name>
set_status() {
  local info pid fid oid
  info="$(project_info)" || return 1
  pid="$(echo "$info" | cut -d' ' -f2)"; fid="$(echo "$info" | cut -d' ' -f3)"
  oid="$(status_option_id "$2")"
  gh project item-edit --id "$1" --project-id "$pid" --field-id "$fid" --single-select-option-id "$oid" >/dev/null
}

# add_to_board <issue url> <status> → item id
add_to_board() {
  local owner num id
  owner="$(repo_owner)"
  num="$(project_info | cut -d' ' -f1)" || return 1
  id="$(gh project item-add "$num" --owner "$owner" --url "$1" --format json -q .id)"
  set_status "$id" "$2"
  echo "$id"
}

# bots_environment → "testnet" | "live" | "unknown" (live if any bot is live).
bots_environment() {
  local bots env any=""
  bots="$(curl -fsS -m 10 "$MONITOR_URL/api/bots" 2>/dev/null |
    py 'import json,sys; print(" ".join(b["name"] for b in json.load(sys.stdin).get("bots", [])))' 2>/dev/null || true)"
  [[ -n "$bots" ]] || { echo unknown; return; }
  for b in $bots; do
    env="$(curl -fsS -m 10 "$MONITOR_URL/api/bots/$b/status" 2>/dev/null |
      py 'import json,sys; print(json.load(sys.stdin).get("environment",""))' 2>/dev/null || true)"
    [[ "$env" == "live" ]] && { echo live; return; }
    [[ "$env" == "testnet" ]] && any=testnet
  done
  echo "${any:-unknown}"
}

# take_lock <name> — one run at a time; a lock older than 3 h is stale.
take_lock() {
  local dir="$AGENT_STATE_DIR/$1.lock"
  if [[ -d "$dir" ]] && [[ -n "$(find "$dir" -maxdepth 0 -mmin +180 2>/dev/null)" ]]; then
    log "removing stale lock $dir"; rmdir "$dir" 2>/dev/null || rm -rf "$dir"
  fi
  mkdir "$dir" 2>/dev/null || { log "$1 already running"; return 1; }
  # shellcheck disable=SC2064
  trap "rmdir '$dir' 2>/dev/null" EXIT
}
