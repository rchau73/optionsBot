# Agent metrics

Part of [Agentic development](../../AGENTIC.md).
The delivery numbers (DORA) are in [dora.md](dora.md).

E6's outcome is "no human toil except approvals", so the loop measures itself (#180).
On the first tick of each Monday (UTC), [`metrics.sh`](../../scripts/agents/metrics.sh) writes last week's report to `data/agents/metrics/<YYYY-Www>.md` and posts it as a comment on the E6 epic.
It makes no Claude call.
The numbers come from [`metrics.py`](../../scripts/agents/metrics.py), which is standard library only and tested offline on fixture JSON (`test_metrics.py`).
Its inputs are the issues and agent PRs updated since three weeks before the week (the Trend section needs them), the repository's label events, `main`'s git log, the run ledger and the stuck-card log.

| Section | What it counts |
|---|---|
| Stages | Events in the week: SME filed (an `agent:sme` issue created), PM triaged (an issue's first 🧭 PM comment), → Ready (a PM comment that moves a card to Ready), Dev picked up, PR opened, merged, deployed to mvp (🚀 comment), SME verified fixed or reopened (`not_fixed`). Only `agent/issue-N` PRs count. |
| Cycle time | Median and p90 (nearest rank) of Ready → PR, PR → merge and merge → deploy, for spans that ended in the week. A card a person moved to Ready leaves no comment, so its Ready time is the first Dev pickup. |
| Quality | Reviewer rounds per PR merged or closed in the week (each `agent-review` marker counts, blocking ones shown), PRs closed unmerged, `Revert …` commits on `main`, and Dev agent blocks with their first line. |
| Waiting for a person | Hours in the week that issues carried `agent:needs-human` or `prototype:review`, the waits that ended in the week (median, p90) and the ones still open. |
| Trend (last 4 weeks) | (#707) The week beside the three before it, from the same dumps: every stage count, the three cycle-time medians, the reviewer-rounds median, PRs closed unmerged and reverts. See "Reading the Trend" below. |
| Funnel (issues filed in the week) | (#707) The issues created in the week that carry an `E#` label (not the epics themselves), followed as far as the dumps go: how many reached each stage, the median time from filing, and the numbers of those still waiting. See "Reading the Funnel" below. |
| Runs | From the [run ledger](run-ledger.md) (#290), the runs that **started** in the week, per agent: count, each outcome's count, failed % (`failed` + `abandoned`), limit % (`skipped_limit`), median and p90 duration (nearest rank) of the runs that ended, and the runs still open at the week's end (no duration). Only the fixed outcome names are shown, never a detail. A missing or unreadable ledger says "ledger unavailable" and leaves every other section as it is. |
| Overruled by a person | (#292) **SME findings rejected:** `agent:sme` issues closed in the week as not planned, or closed after a person's comment with no merged agent PR, out of the SME findings closed in the week. **PM decisions reversed:** an `E#`, `P#` or `pm:waiting` label removed in the week with no PM comment and no epic retag (another `E#` added) on that issue within 2 min, out of the week's PM comments. Board moves and Priority changes leave no trace on an issue and are not counted. **Clean reviews overruled:** agent PRs whose last review marker was `verdict=clean` on their final head, then closed unmerged in the week or reverted on `main` in the week (a `Revert …` subject naming the PR or its branch), out of those PRs merged or closed in the week. Each item links to its issue or PR. |
| Stuck cards | The cards [`stuck.sh`](stuck-cards.md) found in the week, with their longest idle time and reason (#329). |

## Reading the Trend

The last column compares the week with the mean of the earlier weeks.

- `↓ worse than mean 5.0`: a stage count fell. `↑ worse than mean 5.0`: a cycle time, reviewer rounds, unmerged PRs, reverts or SME reopened rose.
- `mean 5.0` alone: no worse. SME filed never gets an arrow: it is the loop's input, so neither direction is worse.
- `too few to judge`: the earlier weeks hold fewer than 3 in total (of that count, or of the samples behind a median), the same kind of cut `internal/usage/drops.go` makes on a small base. It never carries an arrow.
- `–` in a week: no data, never 0. A whole column is `–` when neither `issues.json` nor `prs.json` has anything in that week, and that week is left out of the mean. One median is `–` when it has no sample.

## Reading the Funnel

A later stage proves the ones before it: a card moved on without a comment (by a person, or by the PM's own planning) still counts as triaged and Ready, at the time of the next stage.
Verified fixed is for `agent:sme` issues only, so its cell reads `1 of 2`; for the others the loop ends at deployed.
The waiting lines group the issue numbers by what each is waiting for next.
Closed-as-not-planned issues are counted on their own line and left out of the table.
An issue closed as done with no merged agent PR stays in the table up to the stage it reached and is not listed as waiting.
The section prints only numbers and counts, never a title or a body, because the repository is public.

## Where the stages come from

Each stage is read from what the agents leave on GitHub, because a move on the board leaves no trace on the issue.
If an agent's comment wording changes, update the markers at the top of `metrics.py` to match.
