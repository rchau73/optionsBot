# Agent metrics

Part of [Agentic development](../../AGENTIC.md).
The delivery numbers (DORA) are in [dora.md](dora.md).

E6's outcome is "no human toil except approvals", so the loop measures itself (#180).
On the first tick of each Monday (UTC), [`metrics.sh`](../../scripts/agents/metrics.sh) writes last week's report to `data/agents/metrics/<YYYY-Www>.md` and posts it as a comment on the E6 epic.
It makes no Claude call.
The numbers come from [`metrics.py`](../../scripts/agents/metrics.py), which is standard library only and tested offline on fixture JSON (`test_metrics.py`).
Its inputs are the issues and agent PRs updated in the week, the repository's label events, `main`'s git log, the run ledger and the stuck-card log.

| Section | What it counts |
|---|---|
| Stages | Events in the week: SME filed (an `agent:sme` issue created), PM triaged (an issue's first 🧭 PM comment), → Ready (a PM comment that moves a card to Ready), Dev picked up, PR opened, merged, deployed to mvp (🚀 comment), SME verified fixed or reopened (`not_fixed`). Only `agent/issue-N` PRs count. |
| Cycle time | Median and p90 (nearest rank) of Ready → PR, PR → merge and merge → deploy, for spans that ended in the week. A card a person moved to Ready leaves no comment, so its Ready time is the first Dev pickup. |
| Quality | Reviewer rounds per PR merged or closed in the week (each `agent-review` marker counts, blocking ones shown), PRs closed unmerged, `Revert …` commits on `main`, and Dev agent blocks with their first line. |
| Waiting for a person | Hours in the week that issues carried `agent:needs-human` or `prototype:review`, the waits that ended in the week (median, p90) and the ones still open. |
| Runs | From the [run ledger](run-ledger.md) (#290), the runs that **started** in the week, per agent: count, each outcome's count, failed % (`failed` + `abandoned`), limit % (`skipped_limit`), median and p90 duration (nearest rank) of the runs that ended, and the runs still open at the week's end (no duration). Only the fixed outcome names are shown, never a detail. A missing or unreadable ledger says "ledger unavailable" and leaves every other section as it is. |
| Overruled by a person | (#292) **SME findings rejected:** `agent:sme` issues closed in the week as not planned, or closed after a person's comment with no merged agent PR, out of the SME findings closed in the week. **PM decisions reversed:** an `E#`, `P#` or `pm:waiting` label removed in the week with no PM comment and no epic retag (another `E#` added) on that issue within 2 min, out of the week's PM comments. Board moves and Priority changes leave no trace on an issue and are not counted. **Clean reviews overruled:** agent PRs whose last review marker was `verdict=clean` on their final head, then closed unmerged in the week or reverted on `main` in the week (a `Revert …` subject naming the PR or its branch), out of those PRs merged or closed in the week. Each item links to its issue or PR. |
| Stuck cards | The cards [`stuck.sh`](stuck-cards.md) found in the week, with their longest idle time and reason (#329). |

Each stage is read from what the agents leave on GitHub, because a move on the board leaves no trace on the issue.
If an agent's comment wording changes, update the markers at the top of `metrics.py` to match.
