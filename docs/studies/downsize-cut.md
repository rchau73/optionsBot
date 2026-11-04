# Study: which strangle a downsize cuts (#638)

**Question.** When IM must come down to the limit, the bot cuts every strangle to its slot share, both legs evenly (`rebalancePositions`, "pro-rata").
That buys the tested put back at its worst price (2026-10-22 stable BTC: the 27 Nov 75k put cut 3.1 → 1.3 lots at −65 %).
Would cutting whole strangles in another order free the same margin for less?

**The rule is unchanged.** This is a study.
A change to which strangle is cut is a margin-policy decision and the product owner's call.

## Method

`cmd/stress -replay` runs the bot's pure rules on the recorded mainnet books (`internal/replay`, see `internal/backtest/CLAUDE.md`).
It had no rebalance; `-rebalance` adds one (`replay/rebalance.go`), and `-rebalances` replays the same books once per downsize rule:
- `prorata` is the bot: each strangle to its slot target (slot share ÷ the IM one lot adds, capped at 2 × its normal size), at least one lot kept.
- `cheapest` cuts whole strangles, both legs evenly, in order of realised cost (buy-back at the ask − premium received + fees) per unit of IM freed, until IM is at the limit.
- `winners` cuts whole strangles, highest captured premium % first, the same way.

Top-ups are the bot's under every rule.
When a pass runs follows the bot (`enforceMarginPolicy`): a confirmed limit change cuts to the exact limit; a retry of a held or short top-up and a startup pass cut only above the limit + 1.5 pp (`DownsizeDue`, #555); a startup pass within ±1.5 pp does nothing (#123).
IM above the limit with no pass due is alert only, as live (#166).

The replay rebuilds the IM limit from the recorded DVOL and its own regime closes, which need not match what the bot held.
`-limits journal` takes the limit, the freeze and the bot's starts (each one a startup pass) from the journal's `risk_limit` lines.

The starting balance is the bot's own at `-from` whenever `-journal` is given (`-equity` overrides it):
- the last margin balance at or before `-from` in the journal's `risk_limit` lines or in `data/pnl_history.jsonl` beside it (`-pnlhistory` for another path), else the first one after `-from`;
- a balance in USD (cross collateral) is converted at the spot of its P&L point;
- without a journal, `account.json` in `-dir` (today's balance, said so on the first line);
- with none of them the replay stops with an error. It used to fall back to 1 coin, which priced every percentage of ETH's ~489 ETH book on 1 ETH.

The first line printed names the balance and where it came from; with `-journal` the report also prints the bot's own balance change over the window, beside the replay's net.

```bash
go run ./cmd/stress -currency BTC -replay data/btc/data/market -from 2026-10-20 -to 2026-11-03 -dir data/stress/btc -journal data/btc/orders.log -start journal -limits journal -rebalances prorata,cheapest,winners
go run ./cmd/stress -currency BTC -replay data/btc/data/market -from 2026-10-12 -to 2026-11-03 -dir data/stress/btc -journal data/btc/orders.log -start journal -limits journal -rebalances prorata,cheapest,winners
go run ./cmd/stress -currency ETH -replay data/eth/data/market -from 2026-10-20 -to 2026-11-03 -dir data/stress/eth -journal data/eth/orders.log -start journal -limits journal -rebalances prorata,cheapest,winners
```

`-rebalance prorata -limits journal -trace` runs one replay and prints every cut; with `-journal` a table sets the bot's downsizes beside the replay's, per UTC day and side.

The columns:
- **Realised P&L of downsizes:** the closes' P&L net of fees, as % of the starting balance (negative = a cost).
- **IM freed (pp):** IM before − after each cut pass, summed, in points of the margin balance.
- **Cost per 1 pp of IM freed:** the two above divided, as % of balance; the issue's target is 30 % lower than pro-rata.
- **Cuts left above the limit / strangles uneven after a cut:** the guards; both must be 0.
- **Largest slot ÷ its share:** after a cut, the heaviest slot's IM (its legs on their own) ÷ its slot share of the limit; the guard is 1.5.

## Results (stable's recordings and journal)

Percentages and counts only: no amounts of a live bot appear in this file.

**To be re-run.** These tables and the journal check below were made before the starting-balance fix.
On the PM's run the balance had fallen back to 1 coin, which made BTC's percentages ~15 % too large and ETH's ~490× too large, and sized every slot target on that 1 coin.
Re-run the three commands above on stable's data and replace both tables; the first line printed must name a balance from the bot (`risk_limit` or `pnl_history`).

| Coin, window | Rule | Net (% of balance) | Cut passes | Legs cut | Downsize P&L (% of balance) | IM freed (pp) | Cost per pp (% of balance) | Above limit after | Largest slot ÷ share | Top-ups | Entries | Worst week (%) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| BTC bias, 10-20 → 11-03 | prorata | −1.18 | 3 | 12 | −0.53 | 14.4 | 0.037 | 0 | 0.96 | 6 | 2 | −1.91 |
| BTC bias, 10-20 → 11-03 | cheapest | −1.17 | 3 | 10 | −0.33 | 13.3 | 0.025 | 0 | 1.08 | 6 | 3 | −1.91 |
| BTC bias, 10-20 → 11-03 | winners | −1.17 | 3 | 10 | −0.33 | 13.3 | 0.025 | 0 | 1.08 | 6 | 3 | −1.91 |
| BTC bias, 10-12 → 11-03 | prorata | +0.16 | 4 | 22 | −0.32 | 21.7 | 0.015 | 0 | 1.01 | 20 | 4 | −1.65 |
| BTC bias, 10-12 → 11-03 | cheapest | +0.13 | 4 | 14 | −0.31 | 13.6 | 0.023 | 0 | 0.90 | 9 | 6 | −1.89 |
| BTC bias, 10-12 → 11-03 | winners | +0.23 | 4 | 14 | −0.31 | 13.2 | 0.023 | 0 | 0.70 | 7 | 6 | −1.78 |

ETH (10-20 → 11-03): no downsize in the journal nor in any replayed rule, so the three rules end the same (+1.93 %).

**Indicative:** three weeks of recording, three or four cut passes per run.

## Reading

- From 10-20 (the issue's window, with the 22 Oct and 26 Oct cuts): cheapest first freed IM for **33 % less per point** (0.025 vs 0.037 % of balance), at the same net and worst week, every slot within 1.08 × its share.
- From 10-12 the order reverses: pro-rata freed more IM in total (21.7 vs 13.6 pp) at about the same realised cost, so **cheapest first costs 55 % more per point**; its net is 0.03 pt lower, winners' 0.07 pt higher.
- The guards held under every rule: IM at or under the limit after each cut, call and put even, no slot above 1.5 × its share.
- Winners first and cheapest first picked the same strangles on the shorter window: a strangle far in profit is also cheap to close.
- On two windows the result does not have one sign; three or four cut passes per run are too few to recommend a rule change.

## Check against the journal (the Gherkin's "within one lot step")

With `-limits journal` the replay cuts on the bot's days (22, 26, 29 Oct; the bot also cut on 28 Oct after a short top-up, which the replay's whole fills do not reproduce).
Per day and side, BTC 10-20 → 11-03:

| Day | Side | Bot | Replay | Within a lot |
|---|---|---|---|---|
| 22 Oct | call / put | 2.1 / 2.1 | 1.5 / 1.5 | no |
| 26 Oct | call / put | 0.5 / 0.5 | 0.4 / 0.4 | yes |
| 28 Oct | call / put | 0.4 / 0.4 | 0 / 0 | no |
| 29 Oct | call / put | 0.4 / 0.4 | 0.6 / 0.6 | no |

22 Oct, the largest cut, comes at the same minute (the 19:48 restart) but smaller: the replay's margin is a scenario scan, not Deribit's simulator (it read IM 27.3 % where Deribit read 24.6 %, and sized the slot target on its own IM per lot),
and by then its book had drifted from the bot's (its own rolls and repairs chose other strikes and sizes).
The replay's sizes are therefore indicative; its timing follows the bot.

**Acceptance status:** "the replayed downsizes on 22 Oct and 26 Oct match the journal's cuts within one lot step" is met for 26 Oct and **not yet met for 22 Oct** on this table.
The timing of the 22 Oct startup pass is pinned on a synthetic book (`TestReplayRebalance_StartupPassBand`: one cut at the restart above limit + 1.5 pp, none within the band, none without a restart).
If the 22 Oct size still differs after the re-run, the follow-up is to calibrate the replay's margin scan to the IM % in the journal's `risk_limit` lines (Deribit's figure).
