# shellcheck shell=bash
# limit_lib.sh — usage and rate limits of the agents (sourced by lib.sh, and by
# test_requeue.py on its own: no side effect when sourced). Bash 3.2-safe.
#
# A run that hits the Claude usage limit or GitHub's rate limit records it; for
# 30 min no new Claude run starts, then one at a time for an hour, then the
# configured parallelism again (tick.sh). The numbers adapt instead of being
# tuned by hand (product owner, 2026-10-19: "find the optimum value, like 3
# parallel, 5 or 2").
#
# Needs LIMIT_FILE and a log function (lib.sh sets both).

# The wordings seen so far. Claude's weekly limit reads "You've hit your weekly
# limit · resets 11pm (America/Sao_Paulo)" (#237: unrecognised, it blocked 28
# cards); the generic forms catch the next variant. requeue.py reads this line.
LIMIT_PATTERN="session limit|usage limit|weekly limit|hit your .*limit|limit · resets|rate limit|secondary rate|HTTP 429|overloaded"

# note_limit <output file> — true (and recorded) when the output shows a limit.
note_limit() {
  grep -qiE "$LIMIT_PATTERN" "$1" 2>/dev/null || return 1
  date -u +%s > "$LIMIT_FILE"
  log "a usage/rate limit was hit: new agent runs pause 30 min, then run one at a time for an hour"
}

# FAST_FAILURE_SEC: a run that fails sooner never did any work.
FAST_FAILURE_SEC=60

# note_stop <exit code> <seconds run> <output file> — true (and recorded like a
# limit) when the run stopped for the infrastructure, not for the card: the
# output shows a limit, or the run exited non-zero in under FAST_FAILURE_SEC
# with no answer block (RESULT:, REVIEW-JSON, PM-*) — a limit worded in a way
# LIMIT_PATTERN does not know yet, a CLI that cannot start. Such a stop never
# labels a card agent:needs-human.
note_stop() {
  note_limit "$3" && return 0
  [[ "$1" -ne 0 && "$2" -lt "$FAST_FAILURE_SEC" ]] || return 1
  grep -qE '^RESULT:|<!-- (REVIEW-JSON|PM-[A-Z]+)' "$3" 2>/dev/null && return 1
  date -u +%s > "$LIMIT_FILE"
  log "a run failed in $2 s (exit $1) with no answer: treated as a limit — new agent runs pause 30 min"
}

# parallel_max <configured> → how many instances may run now: 0 within 30 min
# of a limit hit, 1 within 90 min, else <configured>.
parallel_max() {
  local t age
  t="$(cat "$LIMIT_FILE" 2>/dev/null || echo 0)"; age=$(( $(date -u +%s) - t ))
  if [[ $age -lt 1800 ]]; then echo 0; elif [[ $age -lt 5400 ]]; then echo 1; else echo "$1"; fi
}
