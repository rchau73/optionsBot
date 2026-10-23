#!/usr/bin/env bash
# tick.sh — run every 6 minutes by launchd. Deploys what a person merged, runs
# the PR gate (only ready PRs reach a person: pr_gate.sh), re-queues the cards
# a usage limit blocked (requeue.sh), then starts the agents in the background and returns, so a long implementation
# never delays the next tick:
#   - REVIEW_PARALLEL (1) Reviewer agents, PM_ANSWER_PARALLEL (1) PM answers and
#     DEV_PARALLEL (1) Dev agents, each claiming its own card (lib.sh claim), so
#     independent cards are worked on at once; an instance that finds nothing
#     free exits at once;
#   - one PM agent (triage, planning, dependencies, board order; its own lock);
#   - metrics.sh, which writes last week's agent metrics on the first tick of a
#     Monday (UTC) and posts them on the E6 epic (#180), else exits at once;
#   - evidence.sh, which writes last week's evidence report of every env × bot
#     on the first tick of a Monday (UTC) and posts its summary on the E2 epic
#     (#244), else exits at once;
#   - backup_data.sh, which copies every env × bot's journal and histories to
#     <envs>/history/backups/<date>/ and checks the copy replays, on the first
#     tick after 00:00 UTC (#328), else exits at once.
# After a usage or rate limit no new Claude run starts for 30 min, then one at a
# time for an hour (lib.sh parallel_max); the budget in <envs>/agents.env
# (pause, agent hours, Claude runs per day, parallelism, models: limit_lib.sh)
# holds every Claude run, so deploys and the PR gate keep running alone.
# install.sh sets AbandonProcessGroup so
# launchd leaves the background agents running when this script exits.
set -uo pipefail
dir="$(dirname "${BASH_SOURCE[0]}")"
logs="$dir/../../data/agents"
mkdir -p "$logs"
log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"; }
# shellcheck source=../env_lib.sh
source "$dir/../env_lib.sh"
LIMIT_FILE="$logs/limit_hit"
# shellcheck source=limit_lib.sh
source "$dir/limit_lib.sh" # loads agents.env: the parallelism below comes from it
"$dir/deploy.sh" || echo "deploy step failed (exit $?)"
"$dir/pr_gate.sh" || echo "pr gate step failed (exit $?)"

start() { # script, instances
  local i=0
  while [[ $i -lt $2 ]]; do
    # Own session (setsid via python3: macOS has no setsid(1)), so nothing that
    # ends the tick's process group — launchd included — stops a running agent.
    python3 -c 'import os, sys; os.setsid(); os.execv("/bin/bash", ["bash"] + sys.argv[1:])' "$dir/$1" \
      >> "$logs/tick-${1%.sh}.log" 2>&1 < /dev/null &
    i=$((i + 1))
  done
}
# Cards a usage limit wrongly blocked go back to Ready before the Dev agents
# pick (#237; nothing moves inside a limit pause).
bash "$dir/requeue.sh" >> "$logs/tick-requeue.log" 2>&1 || echo "requeue step failed (exit $?)"
why="$(budget_reason)"
if [[ -n "$why" ]]; then
  log "tick: no Claude agent started — $why"
else
  start review_agent.sh "${REVIEW_PARALLEL:-1}"
  start pm_answer.sh "${PM_ANSWER_PARALLEL:-1}"
  start dev_agent.sh "${DEV_PARALLEL:-1}"
fi
start pm_agent.sh 1 # its board passes need no Claude; triage and planning check the budget
start metrics.sh 1 # weekly: exits at once unless last week's report is missing
start evidence.sh 1 # weekly: exits at once once last week's done-marker exists
start backup_data.sh 1 # nightly: exits at once once today's (UTC) done-marker exists
echo "[$(date '+%Y-%m-%d %H:%M:%S')] tick: agents started (logs in data/agents/tick-*.log)"
