#!/usr/bin/env bash
# assistant_run.sh — the live golden-set run (#536): ask the real assistant
# every question of docs/product/assistant/golden.jsonl, grade the answers and
# compare the score per category with the baseline.
#
#   bash scripts/product/assistant_run.sh [--env stable|mvp] [--week 2026-W44] [--accept] [--pause-ms N]
#
#   1. node frontend/scripts/assistant-run.mjs → <data>/assistant_runs/<time>/answers.jsonl
#      and its own log run.jsonl (every line test_run: never assistant.jsonl,
#      never the request queue; outside the per-person limit, paced);
#   2. python3 scripts/product/assistant_golden.py score → <data>/assistant_reviews/
#      <week>.golden.json (private), <week>.golden.summary.json (counts and %),
#      golden_baseline.json (set by the first run; --accept replaces it).
# <data> is the environment's data/monitor with --env, else this checkout's.
# The key: ANTHROPIC_API_KEY from the shell, else (with --env) that
# environment's .env — passed through the environment, never printed.
# Cost: one model call per question (≈ 115), at most two more for a rejected
# answer's one more try or a failed call; nothing for a question no passage answers.
# Exit 0 = no regression, 1 = a category fell more than the threshold, 2 = no
# key or bad input (nothing written), 3 = the run's log could not be written.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../env_lib.sh
source "$ROOT/scripts/env_lib.sh"

env_name="" week="" accept=() pause=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --env) env_name="${2:?--env needs stable or mvp}"; shift ;;
    --week) week="${2:?--week needs an ISO week}"; shift ;;
    --accept) accept=(--accept) ;;
    --pause-ms) pause=(--pause-ms "${2:?--pause-ms needs a number}"); shift ;;
    *) echo "assistant_run: unknown argument $1" >&2; exit 2 ;;
  esac
  shift
done

data="$ROOT/data/monitor"
key="${ANTHROPIC_API_KEY:-}"
if [[ -n "$env_name" ]]; then
  case " $ENV_NAMES " in *" $env_name "*) ;; *) echo "assistant_run: unknown environment '$env_name' (stable | mvp)" >&2; exit 2 ;; esac
  data="$(env_dir "$env_name")/data/monitor"
  [[ -n "$key" ]] || key="$(env_file_var "$(env_dir "$env_name")/.env" ANTHROPIC_API_KEY)"
fi
if [[ -z "$key" ]]; then
  echo "assistant_run: ANTHROPIC_API_KEY is not set${env_name:+ (in the shell or $(env_dir "$env_name")/.env)}: nothing was asked and no score was written" >&2
  exit 2
fi
[[ -n "$week" ]] || week="$(date -u +%G-W%V)"

run="$data/assistant_runs/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$run"
chmod 700 "$data/assistant_runs" "$run"
ANTHROPIC_API_KEY="$key" "${NODE:-node}" "$ROOT/frontend/scripts/assistant-run.mjs" \
  --out "$run/answers.jsonl" --log "$run/run.jsonl" ${pause[@]+"${pause[@]}"}
python3 "$ROOT/scripts/product/assistant_golden.py" score "$run/answers.jsonl" \
  --out-dir "$data/assistant_reviews" --week "$week" ${accept[@]+"${accept[@]}"}
