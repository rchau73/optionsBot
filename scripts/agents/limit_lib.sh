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

# ── Budget: how much of the person's Claude plan the agents may use ─────────
# The agents share the plan with the person's own Claude use (Desktop, chat):
# product owner 2026-10-23, "slow down … I can't even use Claude Desktop".
# <envs>/agents.env (outside both clones, so a deploy never resets it; read by
# tick.sh and lib.sh, example: scripts/agents/agents.env.example) sets:
#   AGENTS_PAUSED=1            no new Claude run at all
#   AGENT_HOURS=20-8           local hours a run may start in (from-to, wraps
#                              midnight; empty or 0-24 = any hour)
#   AGENT_MAX_RUNS_PER_DAY=20  Claude runs started per local day, all agents
#   DEV_PARALLEL, REVIEW_PARALLEL, PM_ANSWER_PARALLEL (1 each by default)
#   DEV_MODEL, REVIEW_MODEL, PM_MODEL, SME_MODEL (e.g. sonnet: cheaper runs)
#   DEV_LIGHT_MODEL (sonnet), DEV_LIGHT_EPICS (E5,E11): the Dev model for
#                              documents/research cards when DEV_MODEL is unset
# A variable already set in the environment wins over the file.
AGENTS_ENV_FILE="${AGENTS_ENV_FILE:-${ENVS_DIR:-}/agents.env}"
load_budget() {
  [[ -f "$AGENTS_ENV_FILE" ]] || return 0
  local line key
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%%#*}"; line="${line//[[:space:]]/}"
    [[ "$line" == *=* ]] || continue
    key="${line%%=*}"
    case "$key" in
      AGENTS_PAUSED|AGENT_HOURS|AGENT_MAX_RUNS_PER_DAY|DEV_PARALLEL|REVIEW_PARALLEL|PM_ANSWER_PARALLEL|DEV_MODEL|DEV_LIGHT_MODEL|DEV_LIGHT_EPICS|REVIEW_MODEL|PM_MODEL|SME_MODEL)
        [[ -n "${!key+x}" ]] || export "$key=${line#*=}" ;;
    esac
  done < "$AGENTS_ENV_FILE"
}
load_budget

# The Claude runs started, one "<epoch> <agent>" line each (budget_take).
BUDGET_FILE="${BUDGET_FILE:-$(dirname "$LIMIT_FILE")/claude_runs}"

# runs_today → Claude runs started since local midnight.
runs_today() {
  local midnight
  midnight="$(python3 -c 'import datetime as d; print(int(d.datetime.combine(d.date.today(), d.time()).timestamp()))')"
  awk -v m="$midnight" '$1 >= m { n++ } END { print n + 0 }' "$BUDGET_FILE" 2>/dev/null || echo 0
}

# budget_reason → why no Claude run may start now (empty: it may).
budget_reason() {
  [[ "${AGENTS_PAUSED:-0}" != "1" ]] || { echo "paused (AGENTS_PAUSED=1)"; return; }
  local hours="${AGENT_HOURS:-}" from to h
  if [[ -n "$hours" && "$hours" == *-* ]]; then
    from=$((10#${hours%-*})); to=$((10#${hours#*-})); h=$((10#$(date +%H)))
    if [[ $from -lt $to ]]; then
      [[ $h -ge $from && $h -lt $to ]] || { echo "outside agent hours $hours"; return; }
    elif [[ $from -gt $to ]]; then
      [[ $h -ge $from || $h -lt $to ]] || { echo "outside agent hours $hours"; return; }
    fi
  fi
  local max="${AGENT_MAX_RUNS_PER_DAY:-30}" n
  n="$(runs_today)"
  [[ "$n" -lt "$max" ]] || echo "daily budget used ($n of $max Claude runs)"
}

# budget_take <agent> — records a Claude run, just before `claude -p`.
budget_take() {
  mkdir -p "$(dirname "$BUDGET_FILE")" 2>/dev/null || true
  echo "$(date -u +%s) $1" >> "$BUDGET_FILE"
}

# parallel_max <configured> → how many instances may run now: 0 outside the
# budget (budget_reason), 0 within 30 min of a limit hit, 1 within 90 min,
# else <configured>.
parallel_max() {
  local t age why
  why="$(budget_reason)"
  if [[ -n "$why" ]]; then log "agents: no new Claude run — $why" >&2; echo 0; return; fi
  t="$(cat "$LIMIT_FILE" 2>/dev/null || echo 0)"; age=$(( $(date -u +%s) - t ))
  if [[ $age -lt 1800 ]]; then echo 0; elif [[ $age -lt 5400 ]]; then echo 1; else echo "$1"; fi
}
