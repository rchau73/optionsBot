# lib.sh — shared helpers for the agents (sourced, not run). Bash 3.2-safe:
# launchd runs macOS /bin/bash.
#
# GitHub is the control plane: issues are the work queue, the Project board's
# Status column is the workflow, PRs are the audit trail. Helpers read the
# board through `gh` (needs the `project` token scope).

AGENT_REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
AGENT_STATE_DIR="$AGENT_REPO_DIR/data/agents"          # gitignored
AGENT_WORK_DIR="${AGENT_WORK_DIR:-$HOME/.optionsbot-agents}" # worktrees, outside the repo
# shellcheck source=../env_lib.sh
source "$AGENT_REPO_DIR/scripts/env_lib.sh"
# The SME agent judges performance on stable and verifies fixes on mvp, where
# merged work runs first (see scripts/env_lib.sh).
MONITOR_URL="${MONITOR_URL:-http://127.0.0.1:$(env_port stable || true)}"
[[ "$MONITOR_URL" != "http://127.0.0.1:" ]] || MONITOR_URL="http://127.0.0.1:3000"
MVP_MONITOR_URL="${MVP_MONITOR_URL:-http://127.0.0.1:$(env_port mvp || true)}"
[[ "$MVP_MONITOR_URL" != "http://127.0.0.1:" ]] || MVP_MONITOR_URL="http://127.0.0.1:3100"
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

# The board's ids rarely change, and Project queries are expensive in GitHub's
# GraphQL budget (a burst of them trips the secondary rate limit), so they are
# cached in data/agents/project.json for a day. setup_github.sh clears it.
PROJECT_CACHE="$AGENT_STATE_DIR/project.json"

load_project_cache() {
  if [[ -f "$PROJECT_CACHE" ]] && [[ -z "$(find "$PROJECT_CACHE" -mmin +1440 2>/dev/null)" ]] &&
    grep -q '"priority_field"' "$PROJECT_CACHE"; then return 0; fi
  local owner num pid
  owner="$(repo_owner)"
  num="$(gh project list --owner "$owner" --format json -L 100 |
    py 'import json,sys; t=sys.argv[1]; print(next((str(p["number"]) for p in json.load(sys.stdin)["projects"] if p["title"]==t), ""))' "$PROJECT_TITLE")"
  [[ -n "$num" ]] || { log "project '$PROJECT_TITLE' not found: run scripts/agents/setup_github.sh"; return 1; }
  pid="$(gh project view "$num" --owner "$owner" --format json -q .id)"
  gh project field-list "$num" --owner "$owner" --format json |
    py 'import json,sys
fields = json.load(sys.stdin)["fields"]
f = next(f for f in fields if f["name"] == "Status")
p = next((f for f in fields if f["name"] == "Priority"), None)  # setup_github.sh creates it
json.dump({"number": int(sys.argv[1]), "id": sys.argv[2], "status_field": f["id"],
           "options": {o["name"]: o["id"] for o in f["options"]},
           "priority_field": p["id"] if p else None,
           "priority_options": {o["name"]: o["id"] for o in p["options"]} if p else {}}, open(sys.argv[3], "w"))' "$num" "$pid" "$PROJECT_CACHE"
}

# project_info → "NUMBER PROJECT_ID STATUS_FIELD_ID" of the board, or fails.
project_info() {
  load_project_cache || return 1
  py 'import json,sys; c=json.load(open(sys.argv[1])); print(c["number"], c["id"], c["status_field"])' "$PROJECT_CACHE"
}

# status_option_id <status name> — option id of a Status column ("In review").
status_option_id() {
  load_project_cache || return 1
  py 'import json,sys; print(json.load(open(sys.argv[1]))["options"][sys.argv[2]])' "$PROJECT_CACHE" "$1"
}

# board_cards → lines "ITEM_ID ISSUE_NUMBER STATUS" for this repo's issues on the
# board. Read from the issues' side: the board's own item listing is a search
# index that lags (a new project listed 0 items for minutes while its issues
# were already on it), and an agent must not miss a card moved to Ready.
board_cards() {
  local slug num
  slug="$(repo_slug)"
  num="$(project_info | cut -d' ' -f1)" || return 1
  gh api graphql -f query='
query($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) {
    issues(first: 100, states: [OPEN, CLOSED], orderBy: {field: UPDATED_AT, direction: DESC}) {
      nodes { number projectItems(first: 10) { nodes { id project { number }
        fieldValueByName(name: "Status") { ... on ProjectV2ItemFieldSingleSelectValue { name } } } } }
    }
  }
}' -f owner="${slug%/*}" -f name="${slug#*/}" |
    py 'import json,sys; num = int(sys.argv[1])
for i in json.load(sys.stdin)["data"]["repository"]["issues"]["nodes"]:
    for it in i["projectItems"]["nodes"]:
        if it["project"]["number"] == num:
            st = (it.get("fieldValueByName") or {}).get("name") or "-"
            print(it["id"], i["number"], st)' "$num"
}

# items_in_status <status> → lines "ITEM_ID ISSUE_NUMBER", oldest issue first.
items_in_status() {
  board_cards | py 'import sys; want = sys.argv[1]
for line in sys.stdin:
    item, num, st = line.rstrip("\n").split(" ", 2)
    if st == want: print(item, num)' "$1" | sort -k2 -n
}

# item_for_issue <issue number> → project item id (empty when not on the board).
item_for_issue() {
  board_cards | awk -v n="$1" '$2 == n { print $1; exit }'
}

# board_json → JSON list of this repo's issues on the board: item, id (node),
# number, title, state, status, priority, labels, updatedAt, parent (epic
# issue number or null). Read from the issues' side, like board_cards.
board_json() {
  local slug num
  slug="$(repo_slug)"
  num="$(project_info | cut -d' ' -f1)" || return 1
  gh api graphql -f query='
query($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) {
    issues(first: 100, states: [OPEN, CLOSED], orderBy: {field: UPDATED_AT, direction: DESC}) {
      nodes { id number title state updatedAt labels(first: 20) { nodes { name } } parent { number }
        projectItems(first: 10) { nodes { id project { number }
          status: fieldValueByName(name: "Status") { ... on ProjectV2ItemFieldSingleSelectValue { name } }
          priority: fieldValueByName(name: "Priority") { ... on ProjectV2ItemFieldSingleSelectValue { name } } } } }
    }
  }
}' -f owner="${slug%/*}" -f name="${slug#*/}" |
    py 'import json,sys; num = int(sys.argv[1]); out = []
for i in json.load(sys.stdin)["data"]["repository"]["issues"]["nodes"]:
    for it in i["projectItems"]["nodes"]:
        if it["project"]["number"] == num:
            out.append({"item": it["id"], "id": i["id"], "number": i["number"], "title": i["title"],
                "state": i["state"], "status": (it.get("status") or {}).get("name") or "-",
                "priority": (it.get("priority") or {}).get("name"),
                "labels": [l["name"] for l in i["labels"]["nodes"]], "updatedAt": i["updatedAt"],
                "parent": (i.get("parent") or {}).get("number")})
json.dump(out, sys.stdout)' "$num"
}

# ready_next → "ITEM_ID ISSUE_NUMBER" of the Ready card to build next: highest
# Priority first (P0…P3, the PM agent's order; none last), then the oldest
# issue. Epic issues are never built.
ready_next() {
  board_json | py 'import json,sys
cards = [c for c in json.load(sys.stdin) if c["status"] == "Ready" and c["state"] == "OPEN" and "epic" not in c["labels"]]
rank = lambda c: ({"P0": 0, "P1": 1, "P2": 2, "P3": 3}.get(c["priority"], 9), c["number"])
for c in sorted(cards, key=rank)[:1]: print(c["item"], c["number"])'
}

# set_priority <item id> <P0|P1|P2|P3>
set_priority() {
  load_project_cache || return 1
  local pid fid oid
  pid="$(project_info | cut -d' ' -f2)"
  fid="$(py 'import json,sys; print(json.load(open(sys.argv[1]))["priority_field"] or "")' "$PROJECT_CACHE")"
  oid="$(py 'import json,sys; print(json.load(open(sys.argv[1]))["priority_options"].get(sys.argv[2], ""))' "$PROJECT_CACHE" "$2")"
  [[ -n "$fid" && -n "$oid" ]] || { log "no Priority field/option $2 on the board: run scripts/agents/setup_github.sh"; return 1; }
  gh project item-edit --id "$1" --project-id "$pid" --field-id "$fid" --single-select-option-id "$oid" >/dev/null
}

# set_status <item id> <status name>
set_status() {
  local info pid fid oid
  info="$(project_info)" || return 1
  pid="$(echo "$info" | cut -d' ' -f2)"; fid="$(echo "$info" | cut -d' ' -f3)"
  oid="$(status_option_id "$2")" || return 1
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

# bots_environment [monitor url] → "testnet" | "live" | "unknown" (live if any
# bot is live). Default: MONITOR_URL (stable).
bots_environment() {
  local MONITOR_URL="${1:-$MONITOR_URL}" bots env any=""
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

# require_tools <tool>... — fails (and notifies) when a tool is not on PATH, so
# an agent stops before it takes a card instead of blocking it for a broken
# environment. Run scripts/agents/install.sh again after installing a tool.
require_tools() {
  local missing="" t
  for t in "$@"; do command -v "$t" >/dev/null || missing="$missing $t"; done
  [[ -z "$missing" ]] && return 0
  log "missing on PATH:$missing (PATH=$PATH) — re-run scripts/agents/install.sh"
  notify "optionsBot agents: missing tools" "$missing"
  return 1
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
