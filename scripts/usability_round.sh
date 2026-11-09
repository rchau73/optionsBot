#!/usr/bin/env bash
# usability_round.sh — one command for an E3 usability round's setup (#727):
# docs/usability/e3-investor-monitor-test.md section 3.
#
#   scripts/usability_round.sh [default|mixed|drawdown]
#
# Starts the demo bot API and the monitor on a throw-away user database, and
# prints the URLs, the scenario and the moderator's password. Between
# participants press Enter to restart the demo on the same scenario, or type
# the next participant's scenario. Ctrl-C stops everything and deletes the
# user database.
#
# frontend/.env.local is never written. Next.js reads it, but a variable already
# set wins, so the sign-in variables below and the blank tokens take precedence:
# the monitor cannot reach a real bot or use the real monitor tokens.
# USABILITY_DEMO_PORT and USABILITY_MONITOR_PORT move the ports (tests).
# Bash 3.2-safe: macOS /bin/bash runs it.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

demo_port="${USABILITY_DEMO_PORT:-8081}"
monitor_port="${USABILITY_MONITOR_PORT:-3000}"
demo_url="http://127.0.0.1:${demo_port}"
monitor_url="http://localhost:${monitor_port}"

valid_scenario() {
  case "$1" in
    default|mixed|drawdown) return 0 ;;
    *) return 1 ;;
  esac
}

scenario="${1:-default}"
if ! valid_scenario "$scenario"; then
  echo "usability_round: unknown scenario '$scenario': want default, mixed or drawdown" >&2
  exit 2
fi

port_in_use() {
  (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null
}

# A real bot's API on the demo port would be shown to the participant.
for port in "$demo_port" "$monitor_port"; do
  if port_in_use "$port"; then
    echo "usability_round: port $port is in use: stop what runs there first" >&2
    exit 1
  fi
done

work="$(mktemp -d)"
demo_pid=""
monitor_pid=""

# Each background job runs in its own process group (set -m), so stopping the
# group also stops the children it started (next dev's workers).
stop_group() {
  [[ -n "$1" ]] || return 0
  kill -- "-$1" 2>/dev/null || true
  wait "$1" 2>/dev/null || true
}

cleanup() {
  stop_group "$demo_pid"
  stop_group "$monitor_pid"
  rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 130' INT TERM
set -m

# wait_ready URL PID SECONDS: true once URL answers; false if PID died or time ran out.
wait_ready() {
  local i
  for ((i = 0; i < $3; i++)); do
    kill -0 "$2" 2>/dev/null || return 1
    curl -fsS -o /dev/null --max-time 5 "$1" 2>/dev/null && return 0
    sleep 1
  done
  return 1
}

start_demo() {
  if [[ "$1" == "default" ]]; then
    "$work/monitor-demo" -addr "127.0.0.1:${demo_port}" </dev/null &
  else
    "$work/monitor-demo" -addr "127.0.0.1:${demo_port}" -scenario "$1" </dev/null &
  fi
  demo_pid=$!
  if ! wait_ready "${demo_url}/api/health" "$demo_pid" 30; then
    echo "usability_round: the demo API did not start" >&2
    exit 1
  fi
}

print_round() {
  cat <<EOF

Scenario:  $scenario
Demo API:  $demo_url
Monitor:   $monitor_url
Sign in:   $monitor_url/login   (user moderator, password $ADMIN_PASSWORD)

Between participants: Enter restarts the demo on this scenario, or type default, mixed or drawdown.
Ctrl-C ends the round and deletes its user database.
EOF
}

echo "usability_round: building the demo and installing the monitor"
go build -o "$work/monitor-demo" ./cmd/monitor-demo
(cd frontend && npm ci)

# The first admin is created only when the user database is empty, so each
# round gets its own: the printed password always works.
export AUTH_DB_PATH="$work/auth.db"
AUTH_SECRET="$(openssl rand -base64 48)"
ADMIN_PASSWORD="$(openssl rand -base64 18)"
export AUTH_SECRET ADMIN_PASSWORD
export ADMIN_USERNAME=moderator
export BOT_APIS="btc=${demo_url}"
export MONITOR_API_TOKEN="" BOT_ADMIN_TOKEN="" ANTHROPIC_API_KEY=""

start_demo "$scenario"
(cd frontend && exec npx next dev -p "$monitor_port") </dev/null &
monitor_pid=$!
if ! wait_ready "${monitor_url}/login" "$monitor_pid" 120; then
  echo "usability_round: the monitor did not start" >&2
  exit 1
fi
print_round

while IFS= read -r next; do
  next="${next:-$scenario}"
  if ! valid_scenario "$next"; then
    echo "usability_round: unknown scenario '$next': want default, mixed or drawdown" >&2
    continue
  fi
  scenario="$next"
  stop_group "$demo_pid"
  demo_pid=""
  start_demo "$scenario"
  print_round
done
