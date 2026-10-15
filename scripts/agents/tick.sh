#!/usr/bin/env bash
# tick.sh — run every 15 minutes by launchd: deploy what a person merged, then
# let the Dev agent take at most one Ready issue. Each step has its own lock,
# so a long implementation never blocks the next deploy check.
set -uo pipefail
dir="$(dirname "${BASH_SOURCE[0]}")"
"$dir/deploy.sh" || echo "deploy step failed (exit $?)"
"$dir/dev_agent.sh" || echo "dev step failed (exit $?)"
