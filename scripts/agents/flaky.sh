#!/usr/bin/env bash
# flaky.sh — the nightly flaky-test hunt (#248). tick.sh starts it every tick;
# it does nothing before FLAKY_HOUR_UTC (3) or once today's done-marker exists,
# so the first tick of each UTC night at or after that hour runs it:
#   1. a clean detached worktree of origin/main (<work dir>/flaky-<date>);
#   2. in parallel, under one hard time limit (FLAKY_LIMIT_MIN, 30):
#        go test ./tests/... -race -count=5 -json
#        npm ci, then npm test -- --ci (jest --json) FLAKY_JEST_RUNS (3) times;
#   3. flaky.py plan: a test that both passed and failed tonight is flaky; one
#      `bug` issue "[E6] Flaky test: <name>" per flaky test in Triage (counts,
#      commit, the first failure's output with .env secrets redacted), unless
#      an open issue or one closed in 14 days already names it. A test that
#      failed every run is logged, nothing filed (CI catches it).
# Past the time limit everything is stopped and nothing is filed: the log says
# it timed out. The night is marked done when it starts, so a crash is not
# retried every tick. No Claude call, no order; GitHub: the issues only.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$AGENT_REPO_DIR"
FLAKY_PY="$AGENT_REPO_DIR/scripts/agents/flaky.py"
hour="$(date -u +%H)"
[[ $((10#$hour)) -ge "${FLAKY_HOUR_UTC:-3}" ]] || exit 0 # 10#: "08" is not octal
night="$(date -u +%Y-%m-%d)"
state="$AGENT_STATE_DIR/flaky"
done_marker="$state/$night.done"
[[ -f "$done_marker" ]] && exit 0
require_tools go gh python3 || exit 0
take_lock flaky || exit 0
dir="$state/$night"
mkdir -p "$dir/results" "$dir/issues"
date -u +%Y-%m-%dT%H:%M:%SZ > "$done_marker"
run_begin flaky "" "" "$dir" # the run ledger (lib.sh): every exit below ends it
log "flaky: night $night"

wt="$AGENT_WORK_DIR/flaky-$night"
git_locked worktree prune || true
[[ -d "$wt" ]] && { git_locked worktree remove --force "$wt" 2>/dev/null || rm -rf "$wt"; }
git_locked fetch -q origin main
git_locked worktree add -q --detach "$wt" origin/main
on_exit_remove "$wt" # git worktree prune forgets it on the next night
commit="$(git -C "$wt" rev-parse HEAD)"
RUN_COMMIT="${commit:0:7}"
log "flaky: testing ${commit:0:7} in $wt"

limit_sec="${FLAKY_LIMIT_SEC:-$(( ${FLAKY_LIMIT_MIN:-30} * 60 ))}" # FLAKY_LIMIT_SEC: tests
deadline=$(( $(date +%s) + limit_sec ))
limited() { # out, err, cmd... — the command under the deadline; marks a time-out
  local out="$1" err="$2" rc=0; shift 2
  python3 "$FLAKY_PY" run "$deadline" "$out" "$err" -- "$@" || rc=$?
  [[ $rc -ne 124 ]] || touch "$dir/timed_out"
  return $rc
}

# Go: its non-zero exit only means a test failed; the JSON says which.
( cd "$wt" && limited "$dir/results/go.json" "$dir/go.log" go test ./tests/... -race -count=5 -json ) || true &
go_pid=$!
# Frontend: install, then the suite FLAKY_JEST_RUNS times, each to its own report.
(
  cd "$wt/frontend" || { log "flaky: no frontend/ on ${commit:0:7} — frontend not tested"; exit 0; }
  rc=0; limited "$dir/npm-ci.out" "$dir/npm-ci.log" npm ci --no-audit --no-fund || rc=$?
  if [[ $rc -ne 0 ]]; then
    [[ $rc -eq 124 ]] || log "flaky: npm ci failed (log $dir/npm-ci.log) — frontend not tested"
    exit 0
  fi
  i=1
  while [[ $i -le ${FLAKY_JEST_RUNS:-3} && ! -f "$dir/timed_out" ]]; do
    limited "$dir/jest-$i.out" "$dir/jest-$i.log" npm test -- --ci --json --outputFile="$dir/results/jest-$i.json" || true
    i=$((i + 1))
  done
) || true
wait "$go_pid" || true

if [[ -f "$dir/timed_out" ]]; then
  log "flaky: timed out after ${limit_sec}s — stopped, nothing filed (logs $dir)"
  run_end timed_out "time limit ${limit_sec}s"; exit 0
fi

slug="$(repo_slug)"
gh issue list -R "$slug" --state all -L 300 --search "Flaky test in:title" \
  --json number,title,state,closedAt > "$dir/issues.json"
envs=()
for env in $ENV_NAMES; do envs+=("$(env_dir "$env")/.env" "$(env_dir "$env")/deploy.env"); done
python3 "$FLAKY_PY" plan "$dir/results" "$dir/issues.json" "$dir/issues" "$commit" "${envs[@]}" > "$dir/plan.log"
while IFS= read -r line; do log "flaky: $line"; done < "$dir/plan.log"

filed=0
for t in "$dir"/issues/*.title; do
  [[ -f "$t" ]] || continue
  if url="$(gh issue create -R "$slug" --label bug --title "$(cat "$t")" --body-file "${t%.title}.body")"; then
    filed=$((filed + 1))
    add_to_board "$url" "Triage" >/dev/null || log "flaky: $url filed but not put on the board"
    log "flaky: filed $url"
  else
    log "flaky: could not file \"$(cat "$t")\""
  fi
done
run_end "$([[ $filed -gt 0 ]] && echo filed || echo none)" "$filed issue(s) filed"
