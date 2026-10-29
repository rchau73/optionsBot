#!/usr/bin/env bash
# tick.sh — run every 6 minutes by launchd. Deploys what a person merged, runs
# the PR gate (only ready PRs reach a person: pr_gate.sh), re-queues the cards
# a usage limit blocked (requeue.sh), closes the ledger's dead runs and flags
# long ones (sweep_runs.sh), reports stalled cards (stuck.sh, every 6 h),
# then starts the agents in the background and returns, so a long implementation
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
#   - usage.sh, which writes last week's monitor usage report of every env
#     with analytics events, files one issue on an engagement drop and posts
#     its summary on the E13 epic (#340), else exits at once;
#   - flaky.sh, which repeats the Go and frontend suites on origin/main once a
#     UTC night and files one bug per new flaky test (#248), else exits at once;
#   - backup_data.sh, which copies every env × bot's journal and histories to
#     <envs>/history/backups/<date>/ and checks the copy replays, on the first
#     tick after 00:00 UTC (#328), else exits at once;
#   - dora.sh, which writes the delivery report (DORA metrics, each PR's time
#     live on mvp and stable) once a UTC day and posts last week's four
#     numbers on the E6 epic once (#470), else exits at once;
#   - requests.sh, which judges the assistant's private request queue with one
#     PM agent run and files the accepted ones as redacted issues (#410), else
#     exits at once;
#   - release.sh, only after a healthy deploy step: the weekly release of the
#     newest mvp build soaked 3 days (first tick of a Monday UTC), or a
#     `release:hotfix` bug fix live 1 h on mvp (#471; AUTO_RELEASE=0 in
#     agents.env turns it off), else exits at once.
# After a usage or rate limit no new Claude run starts for 30 min, then one at a
# time for an hour (lib.sh parallel_max); the budget in <envs>/agents.env
# (pause, agent hours, Claude runs per day, parallelism, models: limit_lib.sh)
# holds every Claude run, so deploys and the PR gate keep running alone.
# The PR gate runs before the Reviewers start, so a Dev draft whose CI turned
# green is marked ready and reviewed on the same tick, and a red one goes back
# to Ready before the Dev agents pick (#472).
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
deploy_ok=1
"$dir/deploy.sh" || { echo "deploy step failed (exit $?)"; deploy_ok=0; }
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
# Runs whose process is gone are closed as abandoned; runs past their time
# limit are notified once, never killed (#291).
bash "$dir/sweep_runs.sh" >> "$logs/tick-sweep.log" 2>&1 || echo "run sweep step failed (exit $?)"
# Cards In progress / In review with no run behind them get one comment; at
# most once every 6 h (#329).
bash "$dir/stuck.sh" >> "$logs/tick-stuck.log" 2>&1 || echo "stuck-card step failed (exit $?)"
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
start usage.sh 1 # weekly: exits at once once last week's done-marker exists (or no events file)
start flaky.sh 1 # nightly: exits at once before 03 UTC or once tonight's done-marker exists
start backup_data.sh 1 # nightly: exits at once once today's (UTC) done-marker exists
start dora.sh 1 # daily: exits at once once today's (UTC) done-marker exists
start requests.sh 1 # exits at once unless an assistant request waits or a filed one is open (its Claude run checks the budget)
# Only after a healthy deploy step (#471): weekly, or a hotfix live 1 h on mvp.
if [[ $deploy_ok -eq 1 ]]; then start release.sh 1; else echo "release step skipped: the deploy step failed"; fi
echo "[$(date '+%Y-%m-%d %H:%M:%S')] tick: agents started (logs in data/agents/tick-*.log)"
