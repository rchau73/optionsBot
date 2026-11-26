# shellcheck shell=bash
# env_lib.sh — the local environments (sourced, not run). Bash 3.2-safe.
#
#   <envs>/stable   a clone checked out at a release tag; changes only through
#                   scripts/promote.sh (a person runs it)
#   <envs>/mvp      a clone on main; deploy.sh moves it after each merge, and
#                   the agents run from it
#
# <envs> is OPTIONSBOT_ENVS, else optionsBot-env beside the checkout running
# the script (~/dev/go/optionsBot → ~/dev/go/optionsBot-env); an environment
# clone (it has a deploy.env) uses its own parent folder.
#
# Each clone holds its own .env (its own Deribit account: two environments on
# one account would cancel each other's orders at startup and adopt each
# other's positions) and a deploy.env:
#
#   OPTIONSBOT_ENV=stable     the environment's name (stable | mvp)
#   MONITOR_PORT=3000         its monitor on 127.0.0.1 (stable 3000, mvp 3100)
#   BOT_RECORD_MARKET=1       market recorders: on in stable only
#   BOT_LINEAR=1              the SOL and XRP bots (#837, mvp): the compose
#                             profile "linear" and their monitor entries; each
#                             trades on its own account, SOL_/XRP_DERIBIT_CLIENT_ID
#                             and _SECRET in the clone's .env
#
# History (who ran what, when) lives outside both clones, in
# <envs>/history/: deploys.tsv (mvp) and promotions.tsv (stable).

_env_repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ -f "$_env_repo/deploy.env" ]]; then
  ENVS_DIR="${OPTIONSBOT_ENVS:-$(dirname "$_env_repo")}"
else
  ENVS_DIR="${OPTIONSBOT_ENVS:-$(dirname "$_env_repo")/optionsBot-env}"
fi
ENV_HISTORY="$ENVS_DIR/history"
ENV_NAMES="stable mvp"

env_dir() { echo "$ENVS_DIR/$1"; }

# env_not_in_clone — fails, printing why, when the script runs from inside an
# environment's clone (it has a deploy.env). promote.sh and `envctl.sh … init`
# call it: 2026-10-24 stable's own v0.1.0 copy looked in ~/optionsbot-env and
# init created a stray clone there (#360). The agents (deploy.sh) do run from
# the mvp clone and do not call it. env_not_in_clone mvp also accepts the mvp
# clone (it runs main, so its scripts are current): the weekly release
# (release.sh, #471) runs promote.sh from there.
env_not_in_clone() {
  [[ ! -f "$_env_repo/deploy.env" ]] && return 0
  [[ -n "${1:-}" && "$(env_file_var "$_env_repo/deploy.env" OPTIONSBOT_ENV)" == "$1" ]] && return 0
  echo "$_env_repo is an environment's clone: run it from the dev checkout"
  return 1
}

# env_file_var <file> <KEY> → value of KEY in a dotenv file (quotes stripped).
env_file_var() {
  [[ -f "$1" ]] || return 0
  sed -n "s/^[[:space:]]*$2[[:space:]]*=[[:space:]]*//p" "$1" | tail -1 | sed -e 's/[[:space:]]*$//' -e 's/^["'\'']//' -e 's/["'\'']$//'
}

env_port() { env_file_var "$(env_dir "$1")/deploy.env" MONITOR_PORT; }
env_monitor_url() { echo "http://127.0.0.1:$(env_port "$1")"; }
# env_monitor_token <env> → MONITOR_API_TOKEN of that environment's .env: the
# monitor's read-only service token (GET /api/bots/* without a session).
env_monitor_token() { env_file_var "$(env_dir "$1")/.env" MONITOR_API_TOKEN; }

# ENV_LINEAR_COINS — the bots BOT_LINEAR=1 adds (#837), each on its own
# account: <COIN>_DERIBIT_CLIENT_ID and <COIN>_DERIBIT_CLIENT_SECRET in .env.
ENV_LINEAR_COINS="SOL XRP"
# ENV_ACCOUNT_VARS — every .env variable naming a bot's Deribit account.
ENV_ACCOUNT_VARS="DERIBIT_CLIENT_ID SOL_DERIBIT_CLIENT_ID XRP_DERIBIT_CLIENT_ID"

# env_accounts_distinct <dir>... — no two bots of these environment
# directories share a Deribit account: no client id (DERIBIT_CLIENT_ID, and the
# SOL and XRP bots' own, ENV_ACCOUNT_VARS) appears twice, in one .env or across
# two (#538: a bot cancels every order of its currency at startup and adopts
# every short it finds, so two bots on one account fight; #837: two USDC bots
# would also share one USDC margin). Prints both and fails otherwise; a
# directory without a .env or a client id is skipped.
env_accounts_distinct() {
  local i j d v id
  local ids=() wdir=() wvar=()
  for d in "$@"; do
    for v in $ENV_ACCOUNT_VARS; do
      id="$(env_file_var "$d/.env" "$v")"
      [[ -n "$id" ]] || continue
      ids+=("$id"); wdir+=("$d"); wvar+=("$v")
    done
  done
  for ((i = 0; i < ${#ids[@]}; i++)); do
    for ((j = i + 1; j < ${#ids[@]}; j++)); do
      [[ "${ids[i]}" == "${ids[j]}" ]] || continue
      if [[ "${wvar[i]}" == DERIBIT_CLIENT_ID && "${wvar[j]}" == DERIBIT_CLIENT_ID ]]; then
        echo "${wdir[i]} and ${wdir[j]} use the same Deribit account (DERIBIT_CLIENT_ID): each environment needs its own subaccount"
      else
        echo "${wdir[i]} (${wvar[i]}) and ${wdir[j]} (${wvar[j]}) use the same Deribit account: each bot needs its own subaccount"
      fi
      return 1
    done
  done
}

# env_linear <env> — the environment runs the SOL and XRP bots (BOT_LINEAR=1
# in its deploy.env, #837).
env_linear() { [[ "$(env_file_var "$(env_dir "$1")/deploy.env" BOT_LINEAR)" == "1" ]]; }

# env_linear_check <env> — with BOT_LINEAR=1, each linear bot has its own
# credentials in .env. Prints what is missing and fails otherwise.
env_linear_check() {
  local dir c
  env_linear "$1" || return 0
  dir="$(env_dir "$1")"
  for c in $ENV_LINEAR_COINS; do
    if [[ -z "$(env_file_var "$dir/.env" "${c}_DERIBIT_CLIENT_ID")" || -z "$(env_file_var "$dir/.env" "${c}_DERIBIT_CLIENT_SECRET")" ]]; then
      echo "$dir/deploy.env has BOT_LINEAR=1 but .env has no ${c}_DERIBIT_CLIENT_ID and ${c}_DERIBIT_CLIENT_SECRET: the $c bot's own testnet account (README \"SOL and XRP bots\")"
      return 1
    fi
  done
}

# env_compose_vars <env> — the variables env_compose exports for the stack:
# with BOT_LINEAR=1 the compose profile of the SOL and XRP bots and their
# monitor entries (appended to BOT_APIS), else none of either.
env_compose_vars() {
  if env_linear "$1"; then
    echo "COMPOSE_PROFILES=linear"
    echo "MONITOR_LINEAR_APIS=,sol=http://bot-sol:8083,xrp=http://bot-xrp:8084"
  else
    echo "COMPOSE_PROFILES="
    echo "MONITOR_LINEAR_APIS="
  fi
}

# env_platform_dirs — every environment directory of the platform: each
# folder of <envs> with a .env (stable, mvp and any other bot's clone).
env_platform_dirs() {
  local d
  for d in "$ENVS_DIR"/*/; do
    [[ -f "$d.env" ]] && echo "${d%/}"
  done
  return 0
}

# env_registry_check <env dir> <platform dir>... — the account registry
# (#800, data/accounts.json in the environment's clone) lets it start: no rule
# broken, each bot's account key ok, no account shared with another folder.
# Without a registry file it does nothing. Read-only; scripts/env_registry.py.
env_registry_check() { python3 "$_env_repo/scripts/env_registry.py" "$@"; }

# env_check <env> — the environment is set up and shares nothing with the
# other one, nor its Deribit account with any environment of the platform,
# and its account registry (when it has one) allows it. Prints the reason and
# fails otherwise.
env_check() {
  local name="$1" dir other odir id d
  local dirs=()
  dir="$(env_dir "$name")"
  case " $ENV_NAMES " in *" $name "*) ;; *) echo "unknown environment '$name' (stable | mvp)"; return 1 ;; esac
  [[ -d "$dir/.git" ]] || { echo "$dir is not a clone: run scripts/envctl.sh $name init"; return 1; }
  [[ "$(env_file_var "$dir/deploy.env" OPTIONSBOT_ENV)" == "$name" ]] || { echo "$dir/deploy.env must say OPTIONSBOT_ENV=$name"; return 1; }
  [[ -n "$(env_port "$name")" ]] || { echo "$dir/deploy.env has no MONITOR_PORT"; return 1; }
  [[ -f "$dir/.env" ]] || { echo "$dir/.env is missing: this environment's own Deribit credentials"; return 1; }
  id="$(env_file_var "$dir/.env" DERIBIT_CLIENT_ID)"
  [[ -n "$id" ]] || { echo "$dir/.env has no DERIBIT_CLIENT_ID"; return 1; }
  env_linear_check "$name" || return 1
  while IFS= read -r d; do dirs+=("$d"); done < <(env_platform_dirs)
  env_accounts_distinct "${dirs[@]}" || return 1
  for other in $ENV_NAMES; do
    [[ "$other" == "$name" ]] && continue
    odir="$(env_dir "$other")"
    [[ -f "$odir/.env" ]] || continue
    if [[ -f "$odir/deploy.env" && "$(env_port "$other")" == "$(env_port "$name")" ]]; then
      echo "$name and $other use the same MONITOR_PORT"; return 1
    fi
  done
  env_registry_check "$dir" "${dirs[@]}" || return 1
}

# env_version <env> → the release the clone is on (tag, or tag-N-gSHA on main).
env_version() { git -C "$(env_dir "$1")" describe --tags --always 2>/dev/null; }

# env_compose <env> <docker compose args...> — the only way a stack is run.
# The SOL and XRP bots run only with BOT_LINEAR=1 (env_compose_vars).
env_compose() {
  local name="$1"; shift
  local dir line; dir="$(env_dir "$name")"
  (cd "$dir" &&
    while IFS= read -r line; do export "${line?}"; done < <(env_compose_vars "$name") &&
    BOT_VERSION="$(env_version "$name")" docker compose --env-file .env --env-file deploy.env "$@")
}

# wait_healthy <monitor url> <timeout sec> [token] — every bot's decision loop
# reports a fresh loop_at through that monitor (freshness, not uptime: an
# unchanged image is not restarted). token: the environment's MONITOR_API_TOKEN
# (env_monitor_token), passed through the environment, never on the command line.
wait_healthy() {
  local url="$1" timeout="${2:-300}" start
  start="$(date +%s)"
  while (( $(date +%s) - start < timeout )); do
    sleep 15
    if MONITOR_TOKEN="${3:-}" python3 -c 'import json,os,re,sys,urllib.request,datetime as d
base = sys.argv[1]
auth = {"Authorization": "Bearer " + os.environ["MONITOR_TOKEN"]} if os.environ.get("MONITOR_TOKEN") else {}
get = lambda p: json.load(urllib.request.urlopen(urllib.request.Request(base + p, headers=auth), timeout=10))
bots = get("/api/bots")["bots"]
assert bots
for b in bots:
    st = get("/api/bots/" + b["name"] + "/status")
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
