# Weekly usage report

Part of [Agentic development](../../AGENTIC.md).

Engagement is measured by itself too (#340, #341, #252, epic E13).
On the first tick of each Monday (UTC), [`usage.sh`](../../scripts/agents/usage.sh) runs the [usage report](../../README.md#usage-report) for the last complete ISO week in every environment whose clone has a non-empty `data/monitor/events.jsonl`.
It makes no Claude call, never reads bot state and places no order.

1. `go run ./cmd/usage -events <clone>/data/monitor/events.jsonl -week <week> -out <clone>/data/monitor/reports -alert` (from the agents' clone, so stable needs no newer code) writes `<week>.md` and `.json` in the clone and prints the engagement-drop check on its `DROPS` line.
2. When a key number fell, one `bug` issue `[E13] Engagement drop: <metrics> <week>` goes to **Triage**: every falling metric of every environment, counts and percentages only.
   Nothing is filed when an open issue, or one closed in the last 14 days, has an `[E13] Engagement drop:` title ending with that week.
3. [`usage_week.py`](../../scripts/agents/usage_week.py) builds one comment, posted on the E13 epic: per environment, active users, the change against the week before, week-1 retention of the latest cohort, first answers found, help used and help answers, the drops and the weeks with no events.
   It reads named figures only, so no user, session or amount field can reach GitHub.
   **Live accounts:** an environment whose `.env` has `DERIBIT_ENV=live` gets percentages only.

**Drop rules** (`internal/usage/drops.go`, fixed; `cmd/usage -alert-*` flags study them): weekly active users or help used (tips opened + videos played) at least 30 % under the mean of the 3 weeks before; the first-answers, manual-close or rebalance funnel's completion share at least 20 points under those weeks'.
A base under 10 users (or 10 funnel starts, in the base and in the week) is `too few to judge`.
A week with no events is noted and left out of the base, never counted as zero.

A missing or empty events file, or one with no event in the week, is logged and skipped.
When no environment has events in the week nothing is posted and no marker is written; the week is looked at again at most hourly.
`data/agents/usage/<week>.done` marks the week done once the comment is posted; a comment that could not be posted is tried once more and then the week is done whatever happened.
Logs: `data/agents/usage/<week>/`.
Run ledger agent `usage`, outcomes `posted`, `skipped` or `failed`.
Tests: `test_usage.py` (the comment, the privacy rule, the dedup, and `usage.sh` end to end on throwaway clones with stub `go`/`gh`) and `tests/usage_test.go` (the drop rules on a six-week fixture).

**Assistant quality review** (#438): before its own report, `usage.sh` runs [`assistant_review.sh`](../../scripts/agents/assistant_review.sh).
In each environment with a non-empty `data/monitor/assistant.jsonl` it runs `go run ./cmd/assistantreview` (the private review files go to `<clone>/data/monitor/assistant_reviews/`) and posts `usage_week.py assistant-comment` on the E10 epic.
The comment holds the `<week>.summary.json` named counts and percentages only, percentages only for a live env.
A week with no answers posts nothing and writes no marker (looked at again hourly).
A comment that fails to post is retried once.
Done marker: `data/agents/assistant_review/<week>.done`.
Ledger agent: `assistant_review`.
Metrics and the review rule: [docs/product/assistant/README.md](../product/assistant/README.md#weekly-quality-review).
