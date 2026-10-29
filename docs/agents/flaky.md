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
Past the time limit everything is stopped, nothing is filed and the log says it timed out.
`data/agents/flaky/<date>.done` is written when the hunt starts, so a crash is not retried every tick.
Results and logs: `data/agents/flaky/<date>/`.
Run ledger agent `flaky`, outcomes `filed`, `none` or `timed_out`.
Tests: `test_flaky.py` (the parsing, dedup and issue body, and `flaky.sh` end to end on a throwaway origin with stub `go`/`npm`/`gh`).
