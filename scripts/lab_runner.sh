#!/usr/bin/env bash
# lab_runner.sh — the lab's runner (#641, epic E9): runs the comparison of each
# run request a manager queued in the monitor (POST /api/lab/requests →
# data/compare/requests/), one at a time, through cmd/labrun → cmd/compare.
#
#   scripts/lab_runner.sh                 one pass over the queue, from the repository
#   scripts/lab_runner.sh -days 14 ...    any cmd/labrun flag (go run ./cmd/labrun -h)
#
# Each request goes running, then done (with the report's id, listed by
# GET /api/lab/runs) or failed (with a short reason); a request queued over a
# day ago is expired. A second runner started meanwhile takes nothing and exits
# 0. It writes no config and applies no setting: the proposed values live in
# cmd/compare's temporary copy. Run it from the environment the monitor reads
# (its data/compare is the monitor's COMPARE_DIR), by hand or from cron.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
exec go run ./cmd/labrun "$@"
