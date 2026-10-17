# env_lib.sh — the local environments (sourced, not run). Bash 3.2-safe.
#
#   ~/optionsbot-env/stable   a clone checked out at a release tag; changes only
#                             through scripts/promote.sh (a person runs it)
#   ~/optionsbot-env/mvp      a clone on main; deploy.sh moves it after each
#                             merge, and the agents run from it
#
# Each clone holds its own .env (its own Deribit account: two environments on
# one account would cancel each other's orders at startup and adopt each
# other's positions) and a deploy.env:
#
#   OPTIONSBOT_ENV=stable     the environment's name (stable | mvp)
#   MONITOR_PORT=3000         its monitor on 127.0.0.1 (stable 3000, mvp 3100)
#   BOT_RECORD_MARKET=1       market recorders: on in stable only
#
# History (who ran what, when) lives outside both clones, in
# ~/optionsbot-env/history/: deploys.tsv (mvp) and promotions.tsv (stable).

ENVS_DIR="${OPTIONSBOT_ENVS:-$HOME/optionsbot-env}"
ENV_HISTORY="$ENVS_DIR/history"
ENV_NAMES="stable mvp"

env_dir() { echo "$ENVS_DIR/$1"; }

# env_file_var <file> <KEY> → value of KEY in a dotenv file (quotes stripped).
env_file_var() {
  [[ -f "$1" ]] || return 0
  sed -n "s/^[[:space:]]*$2[[:space:]]*=[[:space:]]*//p" "$1" | tail -1 | sed -e 's/[[:space:]]*$//' -e 's/^["'\'']//' -e 's/["'\'']$//'
}

env_port() { env_file_var "$(env_dir "$1")/deploy.env" MONITOR_PORT; }
env_monitor_url() { echo "http://127.0.0.1:$(env_port "$1")"; }

# env_check <env> — the environment is set up and shares nothing with the
# other one. Prints the reason and fails otherwise.
env_check() {
  local name="$1" dir other odir id oid
  dir="$(env_dir "$name")"
  case " $ENV_NAMES " in *" $name "*) ;; *) echo "unknown environment '$name' (stable | mvp)"; return 1 ;; esac
  [[ -d "$dir/.git" ]] || { echo "$dir is not a clone: run scripts/envctl.sh $name init"; return 1; }
  [[ "$(env_file_var "$dir/deploy.env" OPTIONSBOT_ENV)" == "$name" ]] || { echo "$dir/deploy.env must say OPTIONSBOT_ENV=$name"; return 1; }
  [[ -n "$(env_port "$name")" ]] || { echo "$dir/deploy.env has no MONITOR_PORT"; return 1; }
  [[ -f "$dir/.env" ]] || { echo "$dir/.env is missing: this environment's own Deribit credentials"; return 1; }
  id="$(env_file_var "$dir/.env" DERIBIT_CLIENT_ID)"
  [[ -n "$id" ]] || { echo "$dir/.env has no DERIBIT_CLIENT_ID"; return 1; }
  for other in $ENV_NAMES; do
    [[ "$other" == "$name" ]] && continue
    odir="$(env_dir "$other")"
    [[ -f "$odir/.env" ]] || continue
    oid="$(env_file_var "$odir/.env" DERIBIT_CLIENT_ID)"
    if [[ "$oid" == "$id" ]]; then
      echo "$name and $other use the same Deribit account (DERIBIT_CLIENT_ID): each environment needs its own subaccount"; return 1
    fi
    if [[ -f "$odir/deploy.env" && "$(env_port "$other")" == "$(env_port "$name")" ]]; then
      echo "$name and $other use the same MONITOR_PORT"; return 1
    fi
  done
}

# env_version <env> → the release the clone is on (tag, or tag-N-gSHA on main).
env_version() { git -C "$(env_dir "$1")" describe --tags --always 2>/dev/null; }

# env_compose <env> <docker compose args...> — the only way a stack is run.
env_compose() {
  local name="$1"; shift
  local dir; dir="$(env_dir "$name")"
  (cd "$dir" && BOT_VERSION="$(env_version "$name")" docker compose --env-file .env --env-file deploy.env "$@")
}

# wait_healthy <monitor url> <timeout sec> — every bot's decision loop reports
# a fresh loop_at through that monitor (freshness, not uptime: an unchanged
# image is not restarted).
wait_healthy() {
  local url="$1" timeout="${2:-300}" start
  start="$(date +%s)"
  while (( $(date +%s) - start < timeout )); do
    sleep 15
    if python3 -c 'import json,re,sys,urllib.request,datetime as d
base = sys.argv[1]
bots = json.load(urllib.request.urlopen(base + "/api/bots", timeout=10))["bots"]
assert bots
for b in bots:
    st = json.load(urllib.request.urlopen(base + "/api/bots/" + b["name"] + "/status", timeout=10))
    at = d.datetime.fromisoformat(re.sub(r"(\.\d{6})\d+", r"\1", st["loop_at"]))  # Go: nanoseconds
    assert (d.datetime.now(d.timezone.utc) - at).total_seconds() < 120, b["name"]' "$url" 2>/dev/null; then
      return 0
    fi
  done
  return 1
}

# env_record <file> <fields...> — append one tab-separated line with the UTC time.
env_record() {
  local f="$ENV_HISTORY/$1"; shift
  mkdir -p "$ENV_HISTORY"
  local IFS=$'\t'
  echo "$(date -u +%Y-%m-%dT%H:%M:%SZ)	$*" >> "$f"
}
