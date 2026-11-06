# Study: always-full strangles under the futures hedge (#677)

**Question.** With the futures hedge holding the book's delta (#657), can the bot keep both legs of every strangle sold at all times?
A missing leg earns nothing and makes the book directional: on 2026-11-05 mvp ETH held calls only.

**No rule changes here.** This is a spike: the review, a replay and a data plan.
The product owner decides which relaxations to build, not before 2026-11-12 and after the shed-vs-hedge benchmark has at least one negative-regime episode, one policy PR per rule area.

**Reading so far: the replay does not support building the relaxations yet.**
On the bots' real book over 23 Oct–5 Nov, missing legs fall a lot, but neither coin meets both targets: ETH nets less than today at both cost levels, and BTC nets more but fails the gap test at 2× costs.
It is one fortnight and one episode, so this is a reason to keep measuring, not a verdict.

## The rules that leave a leg missing

"Holding" below means the hedge is ON and `hedge.Health.NotHolding` is empty (`not_holding` false on `GET /api/hedge`, #664).
The hedge covers **delta** only, up to one band (5 % of the margin balance) and at most 12 orders/h.
It does not cover **gamma** (a short strangle loses on a large move either way; the perpetual is re-traded after the move, so the loss is realised in steps), **vega** (a volatility spike marks both legs up), a **gap** (nothing trades inside it), or the **re-hedge lag** of one band.

| # | Rule | Leaves a leg missing when | What it protects against | What the hedge covers | What it does not | Proposal while holding | Fallback when the hedge cannot trade |
|---|---|---|---|---|---|---|---|
| 1 | Regime side (`regimeside.go`, `RegimeSideBlockReason`) | Confirmed negative regime: no puts in a bear trend, no calls in a bull trend | Selling into the side dealers' hedging accelerates (2026-10-09 ETH puts topped up in a bear trend) | The directional loss of the leg, down to one band | The leg's gamma and vega at the worst moment: it is sold where a move is most likely | **Lift**: sell both sides, entries, repairs and top-ups | Today's rule, at once; `cancelRegimeBlockedSides` withdraws working sells of the side, so the release needs no new path |
| 2 | GEX action skip (`GammaDecision.Action`, `Sheds`) | The live shed signal, entry and repair | The same, on the live signal, before any confirmation | As rule 1 | As rule 1, and the live signal flaps more than the confirmed one (#243) | **Lift**, with rule 1: one gate reads both (`avoidsSide`) | Today's rule, and `cancelShedSide` as now |
| 3 | No re-selling into the move (`stopped.go`, `RepairBlockReason`) | After a stop-loss, delta exit or MM cut: frozen, confirmed negative regime, or 72 h cooldown | Re-selling the leg the market just took out, at a peak of IV and with the trend still running | The re-sold leg's direction | A second gamma loss on the same move, and the churn of stop → re-sell → stop | **Shorten to 24 h** and drop its freeze and regime gates; see the results for 0 h and 72 h | Today's gates and the full 72 h, counted from the original stop |
| 4 | Freeze (`risk.Evaluate`) | A band or regime change is pending: no new risk; a stopped leg's repair waits, as rule 3 | Adding size while the limit may be about to fall | Nothing about margin: the perpetual moves delta, not the limit | The limit itself | **A repair of a hedged book is not new risk**; entries and top-ups stay frozen, because they add size | Today's freeze |
| 5 | GEX shed (`close.go`) | Already `shed_hedged` while ON (#657) | Holding the side through a break | Delta, by #657's design | Gamma, as above | Keep #657 | The shed comes back (`hedge_unavailable`, #664); legs sold under rules 1–4 are then shed too, a double cost to count |
| 6 | Strike gate, premium floor, liquidity cap (`strikegate.go`, `liquidity.go`) | No valid strike | Selling a bad quote, a mark off the smile or more than the market holds | Nothing: not a direction rule | All of it | **Keep**: market-quality rules | Same |
| 7 | Fresh-GEX wait (`gexready.go`), stop deferral (`stopguard.go`) | Short waits | Trading blind; stopping into an empty book | Nothing | All of it | **Keep** | Same |
| 8 | Partial fills, rollouts | Transient: repair completes, or both legs are freed | Nothing to protect: the normal path | — | — | **Keep** | Same |
| 9 | MM cut, size cap, margin limit | Margin | The account | The hedge lowers modelled IM, it does not replace the limits | Liquidation in a gap | **Keep**: capital rules | Same |

Two findings from the review:
- **"Re-sell at a further strike" needs no new rule.** A repair picks the strike nearest the entry delta at today's spot (`SelectStrike`), so after a move it is already further from the old strike; matching the open leg was rejected in stress (−25–30 %).
- **The size a relaxed repair sells is its open leg's** (`present.Qty`), so repairs add no size, and top-ups stay gated by the limit.

## The proposed rule set

While the hedge is ON and holding, and only then:
1. Rules 1 and 2 do not hold a side: entries, repairs and top-ups sell both.
2. A stopped leg (rule 3) waits 24 h, and neither the freeze (rule 4) nor the regime holds it.
3. Everything else stands: stops, delta exit, DTE roll, MM cut, limits, entry freeze, strike gate, premium floor, liquidity cap, fresh-GEX wait.
4. The moment the hedge stops holding, today's rules apply, and working sells of a blocked side are cancelled by the paths that already do it.

### Kill criteria

**Do not build a relaxation if**, on recorded books at 2× costs against today's rules:
- net is not above today's on either coin;
- the −20 % gap loss is worse;
- the worst week is worse, or max MM reaches `max_mm_pct`.

`FullStrangleVerdict` applies these; it says "not comparable" when a variant's average IM is under half of today's, since a much smaller book looks safer by its size alone.

**Withdraw a relaxation already live if**, over any week with a negative-regime episode:
- the hedge's result (`Ledger`: its P&L with fees and funding) is a bigger loss than the extra legs earned (`GET /api/shadow-legs` before the switch, the legs' real P&L after);
- the hedge stopped holding during a break and the shed bought back legs the relaxation had sold;
- a relaxed slot trips the churn breaker.

## Replay

`-fullstrangle today,hedged` replays the same recorded books once per rule set and once per `-costs` multiple (`replay/fullstrangle.go`, one table per coin).
`hedged:H` sets the cooldown of rule 3 to H hours (`hedged` = 24).
`today` is the plain replay, with no perpetual.

```bash
go run ./cmd/stress -currency BTC -replay data/btc/data/market -from 2026-10-23 -dir data/stress/btc -journal data/btc/orders.log -start journal -limits journal -rebalance prorata -fullstrangle today,hedged,hedged:0,hedged:72
go run ./cmd/stress -currency ETH -replay data/eth/data/market -from 2026-10-23 -dir data/stress/eth -journal data/eth/orders.log -start journal -limits journal -rebalance prorata -fullstrangle today,hedged,hedged:0,hedged:72
go run ./cmd/stress -currency BTC -replay data/btc/data/market -from 2026-10-09 -dir data/stress/btc -rebalance prorata -fullstrangle today,hedged,hedged:0,hedged:72
```

Add `-trace` to print each row's actions, and read them before trusting a large number.

### What is modelled

- **The hedge** is the bot's (`hedge.HedgeOrder`, `hedge.Band`, `hedge.RateLimited`): the perpetual trades by the book's coin delta once it is outside 5 % of the balance, at most 12 orders/h, in whole contracts.
- **Costs:** priced at spot, taker fee 0.05 % (×2 at 2× costs), funding at `-perpfunding` (10 %/year) paid whichever way it faces: funding is not recorded, and paying it is the conservative side.
- **The hedge is assumed to hold throughout.** The fallback is not replayed: nothing in the recording says when the hedge could not trade.
- **Option deltas are the last hourly book's**, so a move inside the hour is not re-hedged until the next book: the re-hedge lag is understated, and with it the gamma cost.
- **The −20 % gap** is spot −20 % and IV ×1.3 at once on the book and perpetual held, the worst decision shown.
- **Legs missing** counts one-legged strangles × hours; a vacant slot misses no leg.
- Not modelled, as in the other studies: leg balancing, churn breaker, Deribit's own margin. The perpetual is in the margin scan, so it lowers modelled IM as portfolio margin would.

## Results

Run on 2026-11-06 on stable's recordings; the BTC recording lost the hour after 2026-11-04 14:00 UTC (#661), and balances start from the first `risk_limit` line, 26 Oct.
Percentages are of the starting balance; no amounts of a live bot in this file.

### The real book, 23 Oct–5 Nov (the window that decides)

| Coin | Rules | Costs | Net (%) | Missing leg-hours/week | Avg IM (%) | Net ÷ avg IM | Worst week (%) | Max MM (%) | Gap loss (%) | Stops + delta exits | MM cuts | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| BTC | today | 1× | −3.50 | 49 | 17.0 | −0.207 | −2.81 | 26.6 | 45.8 | 1 | 0 | baseline |
| BTC | hedged | 1× | −2.79 | 29 | 14.3 | −0.196 | −2.01 | 20.3 | 36.5 | 3 | 0 | candidate |
| BTC | hedged:0 | 1× | −2.72 | 0 | 17.5 | −0.157 | −1.94 | 25.9 | 47.4 | 4 | 0 | gap worse |
| BTC | hedged:72 | 1× | −2.85 | 82 | 13.2 | −0.217 | −2.06 | 27.6 | 50.8 | 6 | 0 | gap worse |
| BTC | today | 2× | −5.32 | 42 | 16.8 | −0.320 | −3.88 | 19.8 | 35.0 | 2 | 0 | baseline |
| BTC | hedged | 2× | −4.26 | 29 | 14.5 | −0.297 | −2.51 | 20.4 | 36.6 | 3 | 0 | gap worse |
| ETH | today | 1× | −1.94 | 137 | 7.5 | −0.258 | −1.23 | 17.6 | 30.9 | 16 | 0 | baseline |
| ETH | hedged | 1× | −2.74 | 12 | 16.1 | −0.170 | −2.01 | 21.4 | 37.7 | 1 | 0 | gap worse |
| ETH | hedged:0 | 1× | −2.78 | 1 | 16.4 | −0.171 | −2.01 | 21.4 | 37.6 | 1 | 0 | gap worse |
| ETH | hedged:72 | 1× | −2.85 | 36 | 15.8 | −0.181 | −2.01 | 22.0 | 38.6 | 1 | 0 | gap worse |
| ETH | today | 2× | −3.55 | 133 | 7.7 | −0.466 | −2.57 | 19.8 | 34.0 | 15 | 0 | baseline |
| ETH | hedged | 2× | −3.72 | 12 | 16.2 | −0.232 | −2.74 | 21.4 | 37.7 | 1 | 0 | gap worse |

The perpetual at 1×, as a share of the balance: BTC lost 1.6 % on it (fees 0.23 %, funding 0.05 %, 40 orders), ETH 2.8 % (fees 0.28 %, funding 0.06 %, 59 orders).
Fees and funding are the small part; the rest is the perpetual's own price P&L, which offsets moves in the options and is not a cost alone, so read Net, not this line.
Turnover was about 5× the balance on each coin in the fortnight, and the largest position about a third (BTC) to a half (ETH) of the balance.

What the table says:
- **Missing legs fall**, 137 → 12 leg-hours a week on ETH and 49 → 29 on BTC; what remains is the cooldown of rule 3, and only `hedged:0` reaches the ≈ 0 target.
- **Net: not met.** BTC is ahead at both cost levels; ETH is behind at both, narrowly at 2×.
- **Gap: not met.** BTC at 1× is better, but at 2× it is 36.6 % against 35.0 %, and ETH is worse at both.
- **Full strangles use more margin on ETH** (16 % average IM against 7.5 %): today's ETH is one-sided and under-used, so "more legs" here also means "a bigger book", and that is what the gap column prices. Per unit of margin ETH improves (−0.170 against −0.258 at 1×, −0.232 against −0.466 at 2×).
- **The cooldown is not the lever.** 0, 24 and 72 h differ by about a tenth of a percent of the balance on both coins at 1×, and the order is not the same across coins and costs, which is what one path and a few events look like.

### From an empty book, 9 Oct–5 Nov (for the 9–17 Oct window)

| Coin | Rules | Costs | Net (%) | 9–17 Oct (%) | 29 Oct–4 Nov (%) | Missing leg-hours/week | Avg IM (%) | Worst week (%) | Gap loss (%) | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|
| BTC | today | 1× | −0.46 | +1.06 | −0.71 | 20 | 18.9 | −1.29 | 53.3 | baseline |
| BTC | hedged | 1× | −2.17 | +0.52 | −1.21 | 15 | 17.8 | −1.66 | 52.5 | worst week worse |
| BTC | today | 2× | −2.78 | +0.79 | −1.83 | 20 | 18.9 | −2.47 | 53.7 | baseline |
| BTC | hedged | 2× | −4.46 | +0.19 | −2.17 | 15 | 18.0 | −2.73 | 53.5 | worst week worse |

**ETH from an empty book is not comparable and is left out.** The replay shows today at −36 % and hedged at +0.2 %, and neither number means anything:
- today's −36 % is one fill: a 718-lot call-only leg stopped at an empty book's ask, 93 % above the mid. Prices are held between hourly books, so the 5-minute spread guard expires on the same stale book; the bot would have seen new quotes.
- hedged's +0.2 % is a book a seventh of the size (1.3 % average IM against 9.2 %): repair keeps a leg's size and the liquidity cap shrinks fresh entries, and no limit change triggers a top-up pass.

That first row still shows a real risk the study does not price: a large one-sided leg stopped in a thin book is exactly the tail a full strangle would reduce.

## Data plan

**#676's shadow legs are enough for what they are for:** per held rule (`rule`, `held_by`), what the missing leg would have earned at the bid, followed at its mark through the real exit rules, with the extra perpetual it needs.
That answers "what does rule N cost", which this replay cannot: it re-trades a whole alternative book, while the shadow leg measures the leg alone on the live path.
Two limits to read it with: a leg an exit rule ended is not re-opened while the hold lasts, so it understates `hedged:0`'s churn, and its fill is modelled at the bid.

**Worth adding now, because it cannot be recovered later:**
- **The mainnet perpetual's best bid and ask with each hourly book.** The replay fills at spot and the testnet perpetual trades ~3.5 % away (#664), so the hedge's spread cost is unmeasured on both. One `public/ticker` call an hour on the public mainnet gateway; adding it is a recorder ticket.

**Not needed now:**
- **Perpetual funding history:** Deribit's public funding-rate history can be read back when the study is re-run, instead of recording it; confirm the look-back when that is built. It would replace the 10 %/year assumption, which paid funding in both directions, whereas a short perpetual usually receives it.
- **Intra-minute spot:** the minute GEX records already give spot each minute, and ≤ 12 orders/h makes finer data moot. The larger gap is option delta held for the hour; live `hedge_fill` journal lines (options and perpetual delta at each fill) measure the real lag without new recording.

**Missing from the baseline:** the issue's "missing-leg hours per week, both bots, from the journal" is not computed here; the table's `today` rows are the replay's figure.

## Decision

For the product owner, not before 2026-11-12: which relaxations to build, if any, each a policy PR with `cmd/stress` numbers on the standard shocks next to these.
The numbers above suggest re-running after the benchmark has its episode, with the perpetual's real spread and funding, before building any of rules 1–4.
