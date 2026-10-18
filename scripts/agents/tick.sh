#!/usr/bin/env bash
# tick.sh — run every 15 minutes by launchd: deploy what a person merged,
# review at most one Dev agent PR (a blocking review moves its card back to
# Ready), let the PM agent triage new Triage cards into the approved epics and
# rank Ready, then let the Dev agent take the top Ready issue — so a review's
# findings and the PM's order are picked up in the same tick. Each step has its own lock, so a
# long implementation never blocks the next deploy check.
set -uo pipefail
dir="$(dirname "${BASH_SOURCE[0]}")"
"$dir/deploy.sh" || echo "deploy step failed (exit $?)"
"$dir/review_agent.sh" || echo "review step failed (exit $?)"
"$dir/pm_answer.sh" || echo "pm answer step failed (exit $?)"
"$dir/pm_agent.sh" || echo "pm step failed (exit $?)"
"$dir/dev_agent.sh" || echo "dev step failed (exit $?)"
