#!/usr/bin/env bash
# sweep_runs.sh — run by tick.sh before it starts new runs (#291). Every open
# run in the ledger (lib.sh run_begin with no run_end) whose process is gone —
# a hard kill, a crash the EXIT trap never saw — gets an `end` line with
# outcome `abandoned`, so the ledger tells the truth. A live run past its
# agent's time limit (runs.py TIME_LIMIT_MIN) is reported once (a marker in
# data/agents/runs/flagged/<run_id>) by a notification naming the agent, the
# card and the log. Nothing is ever killed. Rules in runs.py (tested offline);
# the sweep holds the agent-runs mutex, so no run_end interleaves. No Claude
# call, no GitHub call.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
[[ -f "$AGENT_RUNS_FILE" ]] || exit 0
mutex agent-runs || { log "run sweep: the agent-runs mutex is busy, next tick"; exit 0; }
out="$(python3 "$AGENT_REPO_DIR/scripts/agents/runs.py" --sweep --file "$AGENT_RUNS_FILE" \
  --flagged "$AGENT_STATE_DIR/runs/flagged" < /dev/null)" || { unmutex agent-runs; exit 1; }
unmutex agent-runs
[[ -n "$out" ]] || exit 0
while IFS=$'\t' read -r kind agent issue minutes logp run_id; do
  case "$kind" in
    over_limit)
      log "run sweep: $agent run on $issue still going after $minutes min (limit passed; not killed) — log $logp, run $run_id"
      notify "optionsBot agents: a run is taking long" "$agent on $issue for $minutes min — log $logp" ;;
    *) log "run sweep: $kind" ;; # "abandoned <run_id>": its process is gone
  esac
done <<< "$out"
