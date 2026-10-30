# Study: holding the trend's side on a live negative regime (#550)

**Question.** Stable ETH lost most of a week's premium on a 25DEC 2000-P put sold in a bear trend while the gamma regime was negative.
The bot holds the trend's side (no puts in a bear trend, no calls in a bull trend) only once the regime is **confirmed** negative (`strategy.RegimeSideBlockReason`, `iv_band_confirm_days`).
Would also holding it while the **live** regime is negative, before confirmation, have helped?

**The rule is unchanged.** This is a study.
A rule change is a core policy and the product owner's call.

## Method

`cmd/stress -replay` runs the bot's pure rules on the recorded mainnet books (`internal/replay`, see `internal/backtest/CLAUDE.md`).
`-regimeholds confirmed,live` replays the same books with the same settings once per rule and prints one table per coin (`replay/regimehold.go`):
- `confirmed` is the bot.
- `live` also holds the trend's side while the live regime is negative.

```bash
go run ./cmd/stress -currency ETH -replay data/eth/data/market -from 2026-10-12 -dir data/stress/eth -regimeholds confirmed,live
go run ./cmd/stress -currency ETH -replay data/eth/data/market -from 2026-10-23 -dir data/stress/eth -journal data/eth/orders.log -start journal -regimeholds confirmed,live
go run ./cmd/stress -currency BTC -replay data/btc/data/market -from 2026-10-12 -dir data/stress/btc -regimeholds confirmed,live
```

`-regimehold live` runs one replay under the live rule (with `-trace`, every action).

The columns:
- **Net (% of balance):** the change of the margin balance over the window, realised and unrealised, net of fees.
- **Entries opened without the held side:** entries that opened one leg because the rule held the other.
- **Side holds (decisions):** entry or repair sides the rule held, once per decision (one per minute GEX record), so a side held for a day counts about 1,440 times.
- **Legs sold into a live negative regime on the trend's side:** puts in a bear trend and calls in a bull trend sold (entries, repairs) while the live regime was negative.
  This is the issue's metric; the live rule makes it 0 by construction.
- **Worst week:** the worst 7 consecutive replayed days, as % of the starting balance.

## Why the two rules barely differ

The margin policy already freezes new entries when the regime turns negative and is not yet confirmed: "the regime turning negative always freezes" (`risk.Evaluate`).
So a live negative regime before confirmation blocks entries anyway, under either rule.
The live rule changes only **repairs**, which continue during a freeze, and a strangle that is open while the regime flaps.

## Results (stable's recordings, 2026-10-12 → 2026-10-30)

Percentages and counts only: no amounts of a live bot appear in this file.

| Coin, start | Hold | Net (% of balance) | Entries | Entries opened without the held side | Side holds | Legs sold into a live negative regime on the trend's side | Delta exits | Worst week (%) |
|---|---|---|---|---|---|---|---|---|
| ETH sma, flat from 10-12 | confirmed | +0.12 | 8 | 8 | 28,807 | 0 | 1 | −1.31 |
| ETH sma, flat from 10-12 | live | +0.12 | 8 | 8 | 28,807 | 0 | 1 | −1.31 |
| ETH sma, journal book from 10-23 | confirmed | −0.74 | 0 | 0 | 1,178 | 2 | 1 | −0.71 |
| ETH sma, journal book from 10-23 | live | −0.76 | 2 | 2 | 18,162 | 0 | 1 | −0.64 |
| BTC bias, flat from 10-12 | confirmed | −0.20 | 5 | 0 | 0 | 0 | 1 | −0.95 |
| BTC bias, flat from 10-12 | live | −0.20 | 5 | 0 | 0 | 0 | 1 | −0.95 |
| BTC sma, flat from 10-12 | confirmed | −1.05 | 6 | 3 | 2,328 | 0 | 1 | −1.60 |
| BTC sma, flat from 10-12 | live | −1.05 | 6 | 3 | 2,386 | 0 | 1 | −1.60 |
| BTC bias, journal book from 10-23 | confirmed | −1.23 | 3 | 0 | 0 | 0 | 1 | −1.33 |
| BTC bias, journal book from 10-23 | live | −1.23 | 3 | 0 | 0 | 0 | 1 | −1.33 |

**Indicative:** under three weeks of recording, one fast fall.
On ETH the live rule held two repaired puts and opened two call-only entries instead, and ended 0.02 pt lower; everywhere else it changed nothing.

## What the journal shows about the 25DEC 2000-P

The replay could not reproduce the loss, so the journal was read (stable ETH `orders.log`, `data/regime_history.jsonl`):
- The **confirmed** regime was already negative: every daily close from 10-25 to 10-30 is `NEGATIVE/ACCELERATION`, and the risk lines say "negative gamma regime (confirmed)".
  The issue's premise (confirmation still 1 of 2 days) does not hold; the live rule would not have blocked anything the bot's rule did not.
- The put was a **repair**, not an entry: 558 lots of `ETH-25DEC26-2000-P`, sent 2026-10-28 08:36:37 UTC.
  The issue's "10-29 12:35 -03" is a `reconciled` line (a restart re-reading the book), not the sale.
- It was sent during a **40-second neutral trend**: the SMA trend went bear → neutral at 08:36:37 UTC with spot 2378.82 a hair above the swing low 2378.30, and back to bear at 08:37:17 ("price < swing low").
  The swing-low test has no band, unlike SMA9's (`sma9_band_sd`, #243).
- The order then **kept working in the bear trend**: amended at 08:37:57 with the journal's trend `bear`, filled whole at 08:38:37.
  `cancelRegimeBlockedSides` (#243) should have cancelled it on the first bear cycle; no `regime_side` cancel appears anywhere in stable ETH's journal.

## Recommendation

- **No change to the regime-side rule.** Holding on the live regime adds nothing for entries (the freeze already holds them) and, on this history, nothing for the P&L.
- **The loss came from a trend flicker under a confirmed regime, and a working repair that was not withdrawn.** Two follow-ups for a person:
  1. Why `cancelRegimeBlockedSides` did not cancel the working put repair on 2026-10-28 08:37–08:38 UTC (a bug ticket: reproduce with the strategy fixture).
  2. Whether the swing-low/high breakdown test needs the same band as SMA9 (#243), studied with `cmd/stress -sma9band`-style flags.
- The comment's candidate for BTC (lift a regime freeze only after `gex_shed_confirm_snapshots` non-negative snapshots in a row) is a change to the freeze, not to the regime-side rule; it needs its own study.
