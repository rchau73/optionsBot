#!/usr/bin/env bash
# frontend_deps.sh <worktree> <log> <reason.md> — install the frontend's
# dependencies in a Dev agent's fresh worktree before the agent starts (#900).
#
# `git worktree add` leaves out frontend/node_modules (gitignored), so #878's
# and #887's agents could run no jest, build or Playwright command and stopped.
# dev_agent.sh runs this when `checkfail.py frontend-needed` says the card
# needs it.
#
# Exit 0: installed; prints frontend/package-lock.json's git hash, so the
#         script's own checks skip a second npm ci while the lock is unchanged.
# Exit 1: npm ci failed (or npm is missing); <reason.md> says so with the
#         log's last lines, and dev_agent.sh blocks the card without starting
#         the agent — never a silent continue.
set -euo pipefail
wt="$1" log="$2" reason="$3"
if (cd "$wt/frontend" && npm ci --no-audit --no-fund) > "$log" 2>&1 < /dev/null; then
  git -C "$wt" hash-object frontend/package-lock.json
  exit 0
fi
{ echo "Installing the frontend's dependencies (\`npm ci --no-audit --no-fund\` in \`frontend/\`) failed before the agent started, so the agent was not started."
  echo; echo "Last lines of \`$(basename "$log")\`:"; echo
  tail -30 "$log" | sed 's/^/    /'; } > "$reason"
exit 1
