# Nightly flaky-test hunt

Part of [Agentic development](../../AGENTIC.md).

A flaky test turns a PR's CI red by chance and costs a review round (#113 and #165 were found that way, under load).
Since #248, the first tick of each UTC night at or after `FLAKY_HOUR_UTC` (03:00) runs [`flaky.sh`](../../scripts/agents/flaky.sh).
It makes no Claude call, so the budget does not hold it, and it runs beside the other agents, whose schedule and parallel limits are unchanged.

1. A clean detached worktree of `origin/main` (`<work dir>/flaky-<date>`, removed at the end).
2. In parallel, under one hard time limit (`FLAKY_LIMIT_MIN`, 30): `go test ./tests/... -race -count=5 -json`, and `npm ci` followed by `npm test -- --ci --json` run `FLAKY_JEST_RUNS` (3) times.
3. [`flaky.py`](../../scripts/agents/flaky.py) reads the results.
   A test is **flaky** when it both passed and failed that night (a flaky Go subtest is reported, not its parent).
   For each flaky test it files one `bug` issue in **Triage**, titled `[E6] Flaky test: <name>`, with the commit, the pass and fail counts and the first failure's output (last 80 lines, `.env` secrets redacted the way `deploy_issue.py` does).
   Nothing is filed when an open issue, or one closed in the last 14 days, has a title ending in `Flaky test: <name>`.

A test that failed every run is not flaky: it is logged and nothing is filed, because CI catches it.
A jest test whose timeout fired more than a minute after it was due (`STALL_LATE_MS`) is not counted either: the process was frozen, not the test slow, and the log names it `stalled` (#587).
On 2026-11-02 the runner slept mid-run: tests passed after 590 s and 740 s with 5–30 s timeouts, and tests that timed out in that window were filed as flaky (#587, #589).
CPU load delays a timer by seconds, not a minute, so a test that is really slow under load is still filed.
On macOS the hunt runs `caffeinate -is` for as long as `flaky.sh` lives, so the Mac does not idle-sleep mid-run.
Past the time limit everything is stopped, nothing is filed and the log says it timed out.
`data/agents/flaky/<date>.done` is written when the hunt starts, so a crash is not retried every tick.
Results and logs: `data/agents/flaky/<date>/`.
Run ledger agent `flaky`, outcomes `filed`, `none` or `timed_out`.
Tests: `test_flaky.py` (the parsing, dedup and issue body, and `flaky.sh` end to end on a throwaway origin with stub `go`/`npm`/`gh`).

## Repeat offenders

One issue per test hides a file that keeps producing flakes (#747: `reset-audit-ui.test.jsx` #731 and #732, `answers-card.test.jsx` #587 and #589), and each issue costs a Dev run and a review round.
`flaky.py`'s pure `repeat_offenders(issues, now)` groups the `Flaky test:` issues, open or closed in the last 30 days (`REPEAT_DAYS`), by test file (`file_key`):
for jest the part of the name before ` › `, for Go the top-level `Test…` name (a subtest counts for its parent), so a Go test and a jest file never share a group.
- The weekly [agent metrics](metrics.md) report lists every file with 2 or more in its "Flaky repeat offenders (30 days)" section, from the issue list it already reads.
- A new issue whose file reaches 3 or more (`REPEAT_HINT_MIN`, itself and that night's earlier filings included) carries one more line in its body:
  "This file has N flaky issues in 30 days: fix the shared cause in the file (timers, shared state, ordering), not only this test."
  The title, the 14-day dedup and the Triage column are unchanged.
