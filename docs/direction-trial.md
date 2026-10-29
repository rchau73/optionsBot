# Direction trial: decision brief (bias vs sma)

Written before the result, so the trial ends with a decision and not an opinion (#324, epic E2: "a decision on each running trial"; seed #88). **This brief only proposes.** `gex_direction` is a core strategy policy: at the review date the decision goes to the product owner in a ticket marked `agent:needs-human`. The agents do not change either config, the default or the direction rules in `internal/strategy/gamma.go`.

Every number below carries its source, or the label **assumption** with how to check it.

## Hypothesis

In a confirmed negative gamma regime the bot avoids selling one side. The side comes from either the SMA trend of daily closes (`sma`) or the reference script's GEX acceleration bias (`bias`, `StableBiasDir`; CLAUDE.md "Direction source"). The hypothesis under test: **`bias` earns more per unit of margin than `sma`, with no worse a tail.** The stress test of 2026-10-12 says the opposite (bias shed 3–5× as often and returned 16–139 % less than sma; source: `OptionStrategy.md` §9 "Direction"). The live trial checks this on real books.

## Setups

| Setup | Env | Config | Since | Source |
|---|---|---|---|---|
| `bias` | stable BTC | `config_btc.yaml` (`gex_direction: bias`) | 2026-10-12 (BTC on bias); alone on bias since 2026-10-18 | CLAUDE.md, #168 |
| `sma` | mvp BTC | `config_btc.mvp.yaml` (only `gex_direction: sma` differs, `TestConfig_MVPBTCDiffersOnlyInDirection`) | 2026-10-18 | CLAUDE.md, #168 |

The two environments use different Deribit testnet accounts and different books, so the live rows are **not a controlled test**. The controlled comparison is the replay: both sources on stable's recorded mainnet books, from the same starting book.

## The evidence: the weekly scorecard

`go run ./cmd/evidence -compare-direction -week <YYYY-Www>` → `<stable>/data/evidence/direction/<week>.md|.json` (#245; README "Replay of recorded books"). It runs weekly, from the agents' Monday evidence job (`scripts/agents/evidence.sh`, #440). Percentages of equity, counts and hours only. The **rows that decide** are:

| Metric | Scorecard row and column | Weight |
|---|---|---|
| Net return | **Replay — model**: `Net %` of `bias` vs `sma` (`replay[].net_pct`) | decides |
| Worst week | the lowest weekly `Net %` of each replay row across the weeks reviewed | decides (tail guard) |
| Drawdown | **Replay — model**: `Max drawdown %` (`replay[].max_drawdown_pct`) | decides (tail guard) |
| Sheds | **Replay — model**: `Sheds` (`replay[].sheds`), and live `GEX sheds` | context: more sheds cost spread and fees |
| Noise | **Live**: `Live − replay pp` of each source (`live[].live_minus_replay_pp`) | sets the noise threshold below |
| Live direction | **Live**: `Realised net %`, `Marked net %` | context only (not a controlled test) |

Return on margin is not in the scorecard yet. The weekly evidence report of each env gives it (`return_on_margin`, #278), and the trend table holds it week by week (`cmd/evidence -trend`, #323). Read it next to the replay rows. If the two disagree in sign, say so in the decision ticket.

## Minimum span

**6 recorded ISO weeks** of scorecards, counted from the first complete week with both setups live: 2026-W43 (Monday 2026-10-19) to 2026-W48. **Assumption.** One week holds one or two regime episodes, too few to separate the two sources. Check it as the weeks arrive: if the sign of `bias − sma` (replay `Net %`) flips from week to week with no trend, the span is too short, so extend it by 4 weeks. Weeks whose recorded books miss more than 2 days (the scorecard's "Missing inputs") do not count toward the 6.

## Outcomes

Let **d** = the mean over the weeks reviewed of (replay `Net %` of `bias` − replay `Net %` of `sma`), in percentage points. Let **noise** = the mean over the same weeks of |`Live − replay pp`| of the two live rows (how far the replay is from what the bot actually does).

- **No difference:** |d| ≤ noise. A result inside the replay's own error decides nothing. **Assumption** for the threshold: the measured gap stands for the model's error. If the gap is unknown for a week (no replay of that source), that week counts toward neither noise nor d.
- **bias better:** d > noise, **and** the worst week and the max drawdown of `bias` are no worse than `sma`'s by more than noise.
- **sma better:** d < −noise, or `bias`'s worst week or max drawdown is worse than `sma`'s by more than noise (the tail guard alone is enough).

## Kill rule

Stop the trial early and file the decision ticket at once if, over any 3 consecutive reviewed weeks, `bias`'s replay `Net %` is below `sma`'s every week by more than noise, **or** stable BTC's live max drawdown in a week exceeds 2× mvp BTC's. **Assumption:** 3 weeks, the same streak length the track record counts (`longest_losing_streak`, #279), and 2× drawdown. The trial runs on testnet accounts, so the cost of waiting is evidence, not capital.

## Review date

**2026-12-07** (the Monday after 2026-W48, the 6th recorded week). **Assumption**, tied to the minimum span. If the span is extended, the date moves with it, and the extension is written on the decision ticket.

## What happens at the review

1. The scorecard of each week of the span is already there: `scripts/agents/evidence.sh` runs it every Monday for the week before (and back-fills the weeks since 2026-W43), posts its counters on the E2 epic and files the `agent:needs-human` ticket for the first GEX shed, the kill rule and the review (#440, docs/agents/evidence.md "Weekly evidence report"). By hand: `-compare-direction -week`.
2. Fill the outcome above from the decisive rows, with d, noise and each week listed.
3. File one ticket marked `agent:needs-human` with the outcome and its numbers. The product owner decides whether to keep `bias` on stable, move it to `sma`, or extend the trial. A change of `gex_direction` is a config PR a person merges. It is never made by an agent.
