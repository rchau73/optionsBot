# Weekly evidence report

Part of [Agentic development](../../AGENTIC.md).

The bots' track record builds itself too (#244, epic E2).
On the first tick of each Monday (UTC), [`evidence.sh`](../../scripts/agents/evidence.sh) runs the [evidence report](../../README.md#evidence-report) for the last complete ISO week on **stable** and on **mvp**, for every bot (`EVIDENCE_BOTS`, default `btc eth`).
It makes no Claude call and places no order.

1. For each environment and bot, `scripts/stress_inputs.sh <bot>` captures the week-end book into that clone's `data/stress/<bot>/`, through that environment's monitor with its read-only `MONITOR_API_TOKEN`.
2. `go run ./cmd/evidence -env <env> -root <clone> -bot <bot> -week <week>` writes `data/evidence/<week>/<bot>.md` and `.json` in the clone.
   mvp records no books, so it gets `-books-root` = stable's clone.
   Each report carries the **twelve-week scoreboard** (#537, `scoreboard` in the JSON, a section in the Markdown): PRD §6's kill test as of that week
   — the engine's weekly Sharpe ratio against holding the coin's over the last 12 weekly reports with both returns known, read from the clone's earlier reports and this week's
   — with the weeks counted, the weeks still needed, both ratios, the status (`meets`, `fails`, `not enough weeks`, `no spread`) and the ISO weeks missing since the first counted week.
   Ratios, counts and weeks only, labelled "past performance, net of fees" and "a measured window, not a promise of returns"; the calculation is `docs/product/go-no-go.md` §3's.
3. [`evidence_week.py`](../../scripts/agents/evidence_week.py) builds one comment from the `.json` reports, posted on the E2 epic: a row per env × bot with net P&L (% of equity), excess return vs holding the coin, worst 7 days, replay gap, the worst stress shock,
   E1 reliability (#361: mean IM ÷ limit against the 60 % target and whether it was met, the largest cause of idle slot time, startups and how many were restart-made — with their lots on testnet only — or `not measured` and why)
   and the missing days with the market books' share of the week's hours (`(replay partial)` under 90 %, #362),
   then one line per env × bot × dataset with a **recorder gap** over 3 h
   (the dataset, how many such gaps, the longest with its start — times and hours only, so for live accounts too;
   `evidence_week.py gaps` prints the same lines and `evidence.sh` sends the first as a notify, so the recorder can be fixed before the next week),
   then every failed step.
   The script posts on E2 only; the E1 figures ride in that comment.
   **Live accounts:** an environment whose `.env` has `DERIBIT_ENV=live` gets percentages and the report's path only — no coin or USD amount, no failure detail (the rule of `sme_review.sh`).

**Direction trial** (#440, [`docs/direction-trial.md`](../direction-trial.md)): after the per-bot reports, `go run ./cmd/evidence -compare-direction -bot btc -week <week>` writes the bias-vs-sma scorecard to `<stable>/data/evidence/direction/<week>.md|json` — for last week and every week since 2026-W43 still without one (the back-fill, `evidence_week.py direction-weeks`).
The comment gets a "Direction trial" block: each source's replay net %, max drawdown and sheds, the live sheds, live − replay, and the brief's counters (weeks counted toward 6, weeks excluded for more than 2 days of missing books, mean bias − sma, the noise, the early-stop streak, the first GEX shed, the review).
Percentages and counts only.
The first week with a GEX shed (a replay or live row with sheds > 0) is named on the comment's first line, and three decision tickets go to **Triage** labelled `agent:needs-human`,
each kind once ever (deduplicated by its title's start against every issue, open or closed; none when the issue list cannot be read):
`[E2] Direction trial: first GEX shed in <week>, review bias vs sma`,
`… early stop in <week>, decide bias vs sma` (bias's replay net under sma's by more than the noise for 3 counted weeks in a row) and `… review due in <week>, decide bias vs sma` (6 counted weeks, or the report made on or after 2026-12-07).
A ticket only asks: a change of `gex_direction` is a person's config PR.
The kill rule's live drawdown is not in the scorecard; the ticket points to the weekly reports.

A failed step (stress inputs, a report, a scorecard) is listed in the comment and the rest goes on.
`data/agents/evidence/<week>.done` marks the week done once the comment is posted; a week whose comment could not be posted is tried once more on a later tick and then marked done whatever happened.
Logs of each step: `data/agents/evidence/<week>/`.
Tests: `test_evidence.py` (the comment, and `evidence.sh` end to end on throwaway clones with stub `go`/`gh`/`curl`).
