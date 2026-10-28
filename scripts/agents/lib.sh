# shellcheck shell=bash
# lib.sh — shared helpers for the agents (sourced, not run). Bash 3.2-safe:
# launchd runs macOS /bin/bash.
#
# GitHub is the control plane: issues are the work queue, the Project board's
# Status column is the workflow, PRs are the audit trail. Helpers read the
# board through `gh` (needs the `project` token scope).

AGENT_REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
AGENT_STATE_DIR="${AGENT_STATE_DIR:-$AGENT_REPO_DIR/data/agents}" # gitignored; tests point it elsewhere
# shellcheck source=../env_lib.sh
source "$AGENT_REPO_DIR/scripts/env_lib.sh"
AGENT_WORK_DIR="${AGENT_WORK_DIR:-$ENVS_DIR/agent-worktrees}" # worktrees, outside the repo
# The SME agent judges performance on stable and verifies fixes on mvp, where
# merged work runs first (see scripts/env_lib.sh).
MONITOR_URL="${MONITOR_URL:-http://127.0.0.1:$(env_port stable || true)}"
[[ "$MONITOR_URL" != "http://127.0.0.1:" ]] || MONITOR_URL="http://127.0.0.1:3000"
# shellcheck disable=SC2034 # read by the scripts that source lib.sh (sme_review.sh)
MVP_MONITOR_URL="${MVP_MONITOR_URL:-http://127.0.0.1:$(env_port mvp || true)}"
# shellcheck disable=SC2034 # read by the scripts that source lib.sh (sme_review.sh)
[[ "$MVP_MONITOR_URL" != "http://127.0.0.1:" ]] || MVP_MONITOR_URL="http://127.0.0.1:3100"
# The monitors require a session; scripts read them with each environment's
# read-only service token (MONITOR_API_TOKEN in its .env).
MONITOR_TOKEN="${MONITOR_TOKEN:-$(env_monitor_token stable)}"
# shellcheck disable=SC2034 # read by the scripts that source lib.sh (sme_review.sh)
MVP_MONITOR_TOKEN="${MVP_MONITOR_TOKEN:-$(env_monitor_token mvp)}"
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

# ready_next [claim kind] → "ITEM_ID ISSUE_NUMBER" of the Ready card to build
# next: highest Priority first (P0…P3, the PM agent's order; none last), then
# the oldest issue. Epic issues and agent:pm tickets (PRD changes and
# decisions: the PM agent's, pm_answer.sh) are never built, nor a card that
# must wait (pm_board.py wait: "Depends on #N" while #N is open, "Waits until
# <date>"), nor one another instance of <claim kind> is already building.
ready_next() {
  local cands item num why
  # Cards of an epic another instance is building wait (pm_board.py ready).
  local busy="" d k f
  if [[ -n "${1:-}" ]]; then
    for d in "$CLAIMS_DIR/$1"-*; do
      [[ -d "$d" ]] || continue
      k="${d##*/}"; k="${k#"$1"-}"; claim_alive "$1" "$k" && busy="$busy $k"
    done
  fi
  f="$(mktemp)"; board_json > "$f"
  # shellcheck disable=SC2086 # busy is a list of issue numbers
  cands="$(python3 "$AGENT_REPO_DIR/scripts/agents/pm_board.py" ready "$f" $busy)"
  rm -f "$f"
  while read -r item num; do
    [[ -n "$num" ]] || continue
    [[ -n "${1:-}" ]] && claim_alive "$1" "$num" && continue
    why="$(waiting_reason "$num")"
    if [[ -n "$why" ]]; then log "ready: #$num $why" >&2; continue; fi
    echo "$item $num"; return 0
  done <<< "$cands"
}

# waiting_reason <issue> → why the issue must wait (pm_board.py wait), empty
# when it is free. "human: …" means a dependency was closed as not planned.
waiting_reason() {
  local body deps n states="{" sep=""
  body="$(gh issue view "$1" -R "$(repo_slug)" --json body -q .body)"
  deps="$(printf '%s' "$body" | python3 "$AGENT_REPO_DIR/scripts/agents/pm_board.py" deps)"
  for n in $deps; do
    states="$states$sep\"$n\": $(gh issue view "$n" -R "$(repo_slug)" --json state,stateReason -q '{state, reason: .stateReason}' 2>/dev/null || echo '{"state": "OPEN"}')"
    sep=", "
  done
  states="$states}"
  local f; f="$(mktemp)"; printf '%s' "$states" > "$f"
  printf '%s' "$body" | python3 "$AGENT_REPO_DIR/scripts/agents/pm_board.py" wait "$(date -u +%Y-%m-%d)" "$f"
  rm -f "$f"
}

# open_issues_among <n>... — true when any of these issues is still open.
open_issues_among() {
  local n
  for n in "$@"; do
    [[ "$(gh issue view "$n" -R "$(repo_slug)" --json state -q .state 2>/dev/null)" == "OPEN" ]] && return 0
  done
  return 1
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

# monitor_curl <token> <curl args...> — curl with the monitor's service token,
# sent as a header read from stdin so it never shows in the process list.
monitor_curl() {
  local tok="$1"; shift
  if [[ -n "$tok" ]]; then
    printf 'Authorization: Bearer %s\n' "$tok" | curl -H @- "$@"
  else
    curl "$@"
  fi
}

# bots_environment [monitor url] [token] → "testnet" | "live" | "unknown" (live
# if any bot is live). Default: MONITOR_URL (stable) with MONITOR_TOKEN.
bots_environment() {
  local MONITOR_URL="${1:-$MONITOR_URL}" tok="${2-$MONITOR_TOKEN}" bots env any=""
  bots="$(monitor_curl "$tok" -fsS -m 10 "$MONITOR_URL/api/bots" 2>/dev/null |
    py 'import json,sys; print(" ".join(b["name"] for b in json.load(sys.stdin).get("bots", [])))' 2>/dev/null || true)"
  [[ -n "$bots" ]] || { echo unknown; return; }
  for b in $bots; do
    env="$(monitor_curl "$tok" -fsS -m 10 "$MONITOR_URL/api/bots/$b/status" 2>/dev/null |
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

# ── Parallel runs ───────────────────────────────────────────────────────────
# Several Dev, Reviewer and PM-answer instances run at once (tick.sh), each on
# its own card. A claim is a directory holding the owner's pid: alive while
# that process runs, free as soon as it exits or dies (no age-based expiry, so
# a long implementation keeps its card).
CLAIMS_DIR="$AGENT_STATE_DIR/claims"
_exit_paths=""
# _on_exit [exit code] — the EXIT trap: ends an open ledger run (below), then
# removes claims, mutexes and temp dirs. A script with its own EXIT trap passes
# the exit code it saw first (review_agent.sh).
_on_exit() {
  local rc="${1:-$?}" p
  run_end_on_exit "$rc"
  for p in $_exit_paths; do rm -rf "$p" 2>/dev/null; done
}
trap _on_exit EXIT
on_exit_remove() { _exit_paths="$_exit_paths $1"; }

# claim_alive <kind> <key> — true while a live process holds that claim.
claim_alive() {
  local dir="$CLAIMS_DIR/$1-$2" pid
  [[ -d "$dir" ]] || return 1
  pid="$(cat "$dir/pid" 2>/dev/null || true)"
  if [[ -z "$pid" ]]; then # being created right now, or left half-made
    [[ -n "$(find "$dir" -maxdepth 0 -mmin -1 2>/dev/null)" ]]; return
  fi
  kill -0 "$pid" 2>/dev/null
}

# claim <kind> <key> — take a card for this process; fails when it is taken.
claim() {
  local dir="$CLAIMS_DIR/$1-$2"
  mkdir -p "$CLAIMS_DIR"
  claim_alive "$1" "$2" && return 1
  rm -rf "$dir"
  mkdir "$dir" 2>/dev/null || return 1
  echo $$ > "$dir/pid"
  on_exit_remove "$dir"
}

# claims_alive <kind> → how many instances of <kind> are working now.
claims_alive() {
  local d k n=0
  for d in "$CLAIMS_DIR/$1"-*; do
    [[ -d "$d" ]] || continue
    k="${d##*/}"; claim_alive "$1" "${k#"$1"-}" && n=$((n + 1))
  done
  echo "$n"
}

# mutex <name> — wait (up to 2 min) for a short critical section; unmutex <name>
# ends it. A mutex older than 10 min is stale. Used to pick cards one instance
# at a time and to serialise git fetch/worktree/push in the shared clone.
mutex() {
  local dir="$AGENT_STATE_DIR/$1.mutex" i=0
  while ! mkdir "$dir" 2>/dev/null; do
    if [[ -n "$(find "$dir" -maxdepth 0 -mmin +10 2>/dev/null)" ]]; then rm -rf "$dir"; continue; fi
    i=$((i + 1)); [[ $i -gt 240 ]] && { log "mutex $1: gave up waiting"; return 1; }
    perl -e 'select(undef,undef,undef,0.5)'
  done
  on_exit_remove "$dir"
}
# unmutex also forgets the dir for the EXIT trap: by then another process may
# hold that mutex, and removing it at our exit would let both in at once.
unmutex() {
  local d="$AGENT_STATE_DIR/$1.mutex"
  rmdir "$d" 2>/dev/null || true
  _exit_paths="${_exit_paths//" $d"/}"
}

# Usage and rate limits (LIMIT_PATTERN, note_limit, note_stop, parallel_max):
# limit_lib.sh.
LIMIT_FILE="$AGENT_STATE_DIR/limit_hit"
# shellcheck source=limit_lib.sh
source "$AGENT_REPO_DIR/scripts/agents/limit_lib.sh"

# ── Run ledger (#206) ───────────────────────────────────────────────────────
# One file indexes every agent run: <envs>/history/agent_runs.jsonl, a `start`
# and an `end` JSON line per run (format and queries: runs.py). Usage:
#   run_begin <agent> [issue] [pr] [log path]   agent: sme pm pm_answer dev review pr_gate deploy flaky backup usage requests assistant_review
#   RUN_PR=… RUN_COMMIT=…                        what the run learnt on the way (optional)
#   run_end <outcome> [detail]                   e.g. pr_opened, blocked, skipped_limit
# Every exit after run_begin ends the run: the EXIT trap writes `failed` for a
# non-zero exit (a crash under set -e) and `done` otherwise. The commit is the
# agents' own code (this checkout's HEAD) unless the script names the commit it
# worked on (the PR head, the deployed commit). Paths and ids only: the detail
# is a short fixed phrase, never prompt text, a secret or an amount. The ledger
# never stops an agent: a line it cannot write is logged and skipped.
AGENT_RUNS_FILE="${AGENT_RUNS_FILE:-$ENV_HISTORY/agent_runs.jsonl}"
RUN_ID="" RUN_AGENT="" RUN_ISSUE="" RUN_PR="" RUN_LOG="" RUN_COMMIT="" _RUN_SEQ=0

# _run_write <event> [outcome] [detail] — one line, one printf, under a mutex,
# so parallel runs never interleave.
_run_write() {
  local line locked=0
  line="$(python3 "$AGENT_REPO_DIR/scripts/agents/runs.py" line --agent="$RUN_AGENT" --run-id="$RUN_ID" \
    --event="$1" --issue="$RUN_ISSUE" --pr="$RUN_PR" --outcome="${2:-}" --detail="${3:-}" \
    --log="${RUN_LOG#"$AGENT_REPO_DIR"/}" --commit="$RUN_COMMIT" < /dev/null 2>/dev/null)" ||
    { log "run ledger: line refused ($RUN_AGENT $1, run $RUN_ID)"; return 0; }
  mkdir -p "$(dirname "$AGENT_RUNS_FILE")" 2>/dev/null || true
  mutex agent-runs && locked=1
  printf '%s\n' "$line" >> "$AGENT_RUNS_FILE" || log "run ledger: could not write $AGENT_RUNS_FILE"
  [[ $locked -eq 0 ]] || unmutex agent-runs
  return 0
}

# run_begin <agent> [issue] [pr] [log path] — a no-op while a run is open.
run_begin() {
  [[ -z "$RUN_ID" ]] || return 0
  _RUN_SEQ=$((_RUN_SEQ + 1))
  RUN_AGENT="$1"; RUN_ISSUE="${2:-}"; RUN_PR="${3:-}"; RUN_LOG="${4:-}"
  RUN_COMMIT="$(git -C "$AGENT_REPO_DIR" rev-parse --short HEAD 2>/dev/null < /dev/null || true)"
  RUN_ID="$1-$(date -u +%Y%m%dT%H%M%SZ)-$$-$_RUN_SEQ"
  _run_write start
}

# run_end <outcome> [detail] — a no-op when no run is open.
run_end() {
  [[ -n "$RUN_ID" ]] || return 0
  _run_write end "$1" "${2:-}"
  RUN_ID="" RUN_AGENT="" RUN_ISSUE="" RUN_PR="" RUN_LOG="" RUN_COMMIT=""
}

# run_end_on_exit <exit code> — the EXIT trap's part: a run nobody ended.
run_end_on_exit() {
  [[ -n "$RUN_ID" ]] || return 0
  if [[ "$1" -eq 0 ]]; then run_end "done" "exit 0 with no outcome recorded"
  else run_end failed "exit $1"; fi
}

# git_locked <git args…> — git in the shared clone, one instance at a time.
git_locked() { mutex git || return 1; local rc=0; git "$@" || rc=$?; unmutex git; return $rc; }

# take_lock <name> — one run at a time; a lock older than 3 h is stale.
take_lock() {
  local dir="$AGENT_STATE_DIR/$1.lock"
  if [[ -d "$dir" ]] && [[ -n "$(find "$dir" -maxdepth 0 -mmin +180 2>/dev/null)" ]]; then
    log "removing stale lock $dir"; rmdir "$dir" 2>/dev/null || rm -rf "$dir"
  fi
  mkdir "$dir" 2>/dev/null || { log "$1 already running"; return 1; }
  on_exit_remove "$dir"
}

# set_position <item id> [after item id] — place a card in its column (first
# when no "after"). The board's manual order is what people see.
set_position() {
  local pid; pid="$(project_info | cut -d' ' -f2)" || return 1
  if [[ -n "${2:-}" ]]; then
    gh api graphql -f query='mutation($p: ID!, $i: ID!, $a: ID!) { updateProjectV2ItemPosition(input: {projectId: $p, itemId: $i, afterId: $a}) { clientMutationId } }' \
      -f p="$pid" -f i="$1" -f a="$2" >/dev/null
  else
    gh api graphql -f query='mutation($p: ID!, $i: ID!) { updateProjectV2ItemPosition(input: {projectId: $p, itemId: $i}) { clientMutationId } }' \
      -f p="$pid" -f i="$1" >/dev/null
  fi
}
