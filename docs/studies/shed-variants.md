# Study: shed less at the bottom (#637)

**Question.** In a negative gamma regime the GEX shed buys back the whole put (or call) side once spot is past the flip by `gamma_flip_buffer_sd` daily sd on two snapshots (`GammaDecision.Shed`).
On 2026-10-29 21:11 UTC−3 both BTC bots shed every put after BTC fell from ~81.1k to ~77.7k, at −71 % to −184 % of the premium; BTC's low over the next six days stayed above the strikes.
Would a smaller, later or hedged cut lose less without adding tail risk?

**The rule is unchanged.** This is a study.
A rule change is core GEX policy and the product owner's call.

## Method

`cmd/stress -replay` runs the bot's pure rules on the recorded mainnet books (`internal/replay`, see `internal/backtest/CLAUDE.md`).
`-sheds all` replays the same books with the same settings once per variant and once per `-costs` multiple, and prints one table per coin (`replay/shed.go`):
- `today` is the bot: every leg of the shed side bought back at the ask, anchored, re-sold at the anchored release.
- `none`: nothing is bought back; the delta exit (0.30), the stop-loss (2×) and the MM cut guard the leg.
- `partial:K` (K = 0.5, 0): the side is cut to its slot target × K, the target sized as `rebalancePositions` does (slot share of the current limit ÷ the strangle's margin per lot on its own).
  `partial:0` is the bot's shed; the test pins that.
  A leg left in part is not anchored (it is not missing, so nothing repairs it).
- `delta:D` (D = 0.20, 0.25): only legs with |Δ| ≥ D are bought back; each later decision while the shed holds checks again.
- `hedge`: the legs stay; the perpetual is traded against the side's delta (a short put is long the coin, so the hedge is short), re-hedged when it drifts more than ±0.05 Δ per option of the side, unwound at the release a shed leg would wait for (`ShedRepairBlockReason`, or `BiasRepairBlockReason` under `bias`).

Under every variant, entries and repairs still skip the side the live signal sheds (`GammaDecision.Action`), and the stop-loss, delta exit and MM cut run as in the bot.

```bash
go run ./cmd/stress -currency BTC -replay data/btc/data/market -from 2026-10-09 -dir data/stress/btc -direction bias -sheds all
go run ./cmd/stress -currency BTC -replay data/btc/data/market -from 2026-10-23 -dir data/stress/btc -direction bias -journal data/btc/orders.log -start journal -sheds all
go run ./cmd/stress -currency ETH -replay data/eth/data/market -from 2026-10-09 -dir data/stress/eth -sheds all
```

`-shed hedge` (or any one variant) runs one replay; with `-trace`, every action, the perp orders included.
`-costs 1,2` is the default; `-perpfunding 10` is the yearly funding the hedge pays (% of its notional).

### What is modelled

- **Costs ×2:** every recorded spread is widened around its mid (the bid never below bid ÷ 2) and every fee doubled; decisions still read the recorded book.
- **Perp hedge (MODEL):** priced at the snapshot's spot (the nearest future), Deribit's taker fee 0.05 % of the notional, funding paid at `-perpfunding` whichever way the hedge faces (funding is not recorded; paying it is the conservative side); the USD 10 contract step is not modelled.
  The perp is in the margin scan, so it lowers the modelled IM as portfolio margin would.
- **−20 % gap:** at every decision, the loss of the book held then if spot fell 20 % at once with IV × 1.3 (the scan's worst volatility shock), as % of the margin balance; the table shows the worst.
  It is what the recorded weeks cannot show: a crash with no exit filled inside it.

### The columns

- **Net:** the change of the margin balance over the window, realised and unrealised, net of fees; USD = each day's change at that day's spot.
- **Shed closes:** realised P&L of the shed buy-backs as % of the starting balance, then per negative break (a decision where a shed starts after one without) × the number of breaks: the issue's −1.56 % metric.
- **Hedge P&L:** the perp's moves less its fees and funding.
- **9–17 Oct, 29 Oct–4 Nov:** the change of the balance over the replayed days in each named window, with how many were replayed; "not replayed" when the recording does not cover it.
- **Worst day, worst week:** % of the starting balance; the worst week is 7 consecutive replayed days.
- **Max MM:** the highest modelled MM as % of the margin balance.
- **Net ÷ avg IM:** the net per unit of average modelled IM (coin over coin).
- **Verdict** against `today` at the same cost multiple (`ShedVerdict`):
  never recommended when its worst week or its max MM is worse than today's, or its MM reached `max_mm_pct`; a candidate only when it also nets more.
  The table ends with the variants that are candidates at every cost multiple.

## Results

The recorded books are not in the repository; a person runs the commands above on stable's recordings and pastes the tables here (percentages and counts only: no amounts of a live bot in this file).
Check before reading them:
- both named windows are replayed (else the table is indicative: the 9–17 Oct sheds predate the 1.5 sd buffer and the two-snapshot confirmation, #84);
- the −20 % gap column: a variant that holds the side through a break carries more of it, which the recorded weeks did not price.

## Decision

The rule change, if the numbers support one, is a separate decision for the product owner: core GEX policy, merged by a person, with `cmd/stress` stress numbers on the standard shocks next to these.
