#!/usr/bin/env bash
# tick.sh — run every 6 minutes by launchd. Deploys what a person merged, runs
# the PR gate (only ready PRs reach a person: pr_gate.sh), then
# starts the agents in the background and returns, so a long implementation
# never delays the next tick:
#   - REVIEW_PARALLEL (3) Reviewer agents, PM_ANSWER_PARALLEL (2) PM answers and
#     DEV_PARALLEL (3) Dev agents, each claiming its own card (lib.sh claim), so
#     independent cards are worked on at once; an instance that finds nothing
#     free exits at once;
#   - one PM agent (triage, planning, dependencies, board order; its own lock).
# After a usage or rate limit no new Claude run starts for 30 min, then one at a
# time for an hour (lib.sh parallel_max). install.sh sets AbandonProcessGroup so
# launchd leaves the background agents running when this script exits.
set -uo pipefail
dir="$(dirname "${BASH_SOURCE[0]}")"
logs="$dir/../../data/agents"
mkdir -p "$logs"
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
start review_agent.sh "${REVIEW_PARALLEL:-3}"
start pm_answer.sh "${PM_ANSWER_PARALLEL:-2}"
start pm_agent.sh 1
start dev_agent.sh "${DEV_PARALLEL:-3}"
echo "[$(date '+%Y-%m-%d %H:%M:%S')] tick: agents started (logs in data/agents/tick-*.log)"
