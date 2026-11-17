# Study: the 0.30 delta exit against the 2× stop alone (#769)

**Question.** On 2026-11-08 and 11-10 stable ETH's delta exit closed five 25DEC puts at −100 % to −160 % of their premium, and each held its side for `repair_cooldown_hours` (72).
On a 16-delta leg the delta exit (`delta_exit_threshold` 0.30) and the stop (`stop_loss_multiplier` 2×, about −100 % of premium) sit close.
Does the delta exit add anything over the stop alone, and would another threshold do better?

**The rule is unchanged.** This is a study.
A change to the exits is a core policy and the product owner's call.

## Method

`cmd/stress -replay` runs the bot's pure rules on the recorded mainnet books (`internal/replay`, see `internal/backtest/CLAUDE.md`).
`-deltaexits 0.25,0.30,0.35,off` (or `all`) replays the same books with the same settings once per threshold and prints one table per coin (`replay/deltaexit.go`):
- `0.30` is the bot; `off` disables the delta exit, so only the stop (and the MM cut) closes a leg that moves against it.
- A threshold at or under an entry delta is refused, as `config.Validate` does.

```bash
go run ./cmd/stress -currency ETH -replay data/eth/data/market -from 2026-10-09 -to 2026-11-12 -dir data/stress/eth -journal data/eth/orders.log -deltaexits all
go run ./cmd/stress -currency ETH -replay data/eth/data/market -from 2026-10-09 -to 2026-11-12 -dir data/stress/eth -journal data/eth/orders.log -gex none -deltaexits all
go run ./cmd/stress -currency ETH -replay data/eth/data/market -from 2026-10-27 -to 2026-11-12 -dir data/stress/eth -journal data/eth/orders.log -start journal -limits journal -rebalance prorata -deltaexits all
go run ./cmd/stress -currency BTC -replay data/btc/data/market -from 2026-10-09 -to 2026-11-12 -dir data/stress/btc -journal data/btc/orders.log -gex none -deltaexits all
```

The columns:
- **Return on margin:** the net change of the margin balance ÷ the average initial margin over the decisions.
- **Worst week:** the worst 7 consecutive replayed days, as % of the starting balance.
- **Delta exit P&L / Stop P&L (% of premium):** what those closes realised, net of fees, as % of the premium their legs received (the issue's metric).
- **Re-entries:** repairs of a leg that a stop, a delta exit or an MM cut closed, once `repair_cooldown_hours` had passed.
  A strangle that lost both legs frees its slot for a fresh entry instead; those count under **Entries**.

## A caveat on minute decisions: stale empty-book stops

With minute GEX records the replay decides each minute on the last **hourly** book's prices (`replay/ticks.go`).
A stop on an empty book is deferred at most `stop_spread_max_wait_minutes` (5); in the replay the 5 minutes run out on the same stale hourly book, so the stop fills at that book's ask.
On 2026-11-03 08:00 UTC this closed a 25DEC 2900-C at −1,426 % of premium (ask 93 % above the mid) in the 0.25 and 0.30 replays, and dominates their rows.
The live bot sees a fresh book within the 5 minutes.
The hourly runs (`-gex none`) re-check the stop on the next hour's book and are the fairer comparison here; both are listed.

## Results (stable's recordings, 2026-10-09 → 2026-11-12)

Percentages and counts only: no amounts of a live bot appear in this file.

| Coin, start, decisions | Delta exit | Net (% of balance) | Return on margin (%) | Worst week (%) | Delta exits | Delta exit P&L (% of premium) | Stops | Stop P&L (% of premium) | MM cuts | Re-entries | Entries |
|---|---|---|---|---|---|---|---|---|---|---|---|
| ETH, flat from 10-09, hourly | 0.25 | −1.92 | −23.6 | −4.58 | 3 | −130 | 0 | — | 0 | 0 | 11 |
| ETH, flat from 10-09, hourly | 0.30 | −4.26 | −50.7 | −6.48 | 3 | −184 | 0 | — | 0 | 0 | 11 |
| ETH, flat from 10-09, hourly | 0.35 | +0.38 | +9.1 | −0.94 | 1 | −155 | 0 | — | 0 | 0 | 8 |
| ETH, flat from 10-09, hourly | off | −0.27 | −6.6 | −1.58 | 0 | — | 1 | −234 | 0 | 0 | 8 |
| ETH, flat from 10-09, minute | 0.25 | −34.33 | −420.6 | −36.74 | 4 | −122 | 1 | −1,426 | 0 | 0 | 13 |
| ETH, flat from 10-09, minute | 0.30 | −35.41 | −423.9 | −37.17 | 3 | −181 | 1 | −1,426 | 0 | 0 | 12 |
| ETH, flat from 10-09, minute | 0.35 | +0.37 | +9.6 | −0.94 | 1 | −155 | 0 | — | 0 | 0 | 8 |
| ETH, flat from 10-09, minute | off | −0.28 | −7.1 | −1.58 | 0 | — | 1 | −234 | 0 | 0 | 8 |
| ETH, journal book from 10-27, hourly | 0.25 | −5.10 | −45.6 | −7.36 | 5 | −119 | 0 | — | 0 | 0 | 7 |
| ETH, journal book from 10-27, hourly | 0.30 | −3.67 | −28.0 | −5.06 | 2 | −192 | 0 | — | 0 | 0 | 3 |
| ETH, journal book from 10-27, hourly | 0.35 | −4.05 | −31.0 | −4.76 | 0 | — | 1 | −220 | 0 | 0 | 3 |
| ETH, journal book from 10-27, hourly | off | −4.05 | −31.0 | −4.76 | 0 | — | 1 | −220 | 0 | 0 | 3 |
| ETH, journal book from 10-27, minute | 0.25 | −16.80 | −160.9 | −18.81 | 5 | −127 | 1 | −707 | 0 | 0 | 9 |
| ETH, journal book from 10-27, minute | 0.30 | −15.30 | −125.0 | −16.50 | 2 | −191 | 1 | −707 | 0 | 0 | 5 |
| ETH, journal book from 10-27, minute | 0.35 | −15.98 | −133.8 | −16.44 | 0 | — | 3 | −497 | 0 | 0 | 5 |
| ETH, journal book from 10-27, minute | off | −15.98 | −133.8 | −16.44 | 0 | — | 3 | −497 | 0 | 0 | 5 |
| BTC, flat from 10-09, hourly | 0.25 | −1.16 | −7.2 | −3.87 | 4 | −75 | 0 | — | 0 | 1 | 9 |
| BTC, flat from 10-09, hourly | 0.30 | −1.72 | −9.4 | −3.47 | 3 | −135 | 0 | — | 4 | 0 | 9 |
| BTC, flat from 10-09, hourly | 0.35 | −2.12 | −10.6 | −2.96 | 3 | −155 | 0 | — | 8 | 0 | 8 |
| BTC, flat from 10-09, hourly | off | +0.91 | +6.8 | −1.45 | 0 | — | 1 | −220 | 0 | 1 | 7 |

The journal-book runs start from the bot's open book and margin balance at 10-27 (its first `risk_limit` line with a balance is 10-26) and use its own limits and rebalance.
The flat runs start from no position on the bot's balance at its first recorded `risk_limit` line.

## What the numbers say

- **The delta exit's closes cost about as much as a stop.** At 0.30 they realised −135 % to −192 % of premium, where the stop alone realised −220 % to −234 % on one leg; the issue's −100 % to −160 % is the same band.
  On a 16-delta leg the exit fires only a little before the stop, and the leg's price at the exit already holds most of the loss.
- **The stop alone was never worse on worst week in the hourly runs.** ETH flat: −1.58 % against −6.48 % at 0.30; ETH journal book: −4.76 % against −5.06 %; BTC: −1.45 % against −3.47 %.
  The paths diverge after the first exit: the exited side is held for 72 h while the slot keeps its other leg, and the 0.35 and off rows closed fewer legs and so sold fewer new ones into the fall.
- **A lower threshold (0.25) exits more legs** (3–5) at a smaller loss each (−75 % to −130 %) but did not reduce the worst week against the stop alone in any run.
- **0.35 behaves like `off`** on ETH (no or one exit); on BTC it ran into more MM cuts (8) than any other row.
- **Re-entries are 0 or 1 everywhere:** the 72 h cooldown, the regime-side hold and the freezes kept nearly every exited side out for the rest of the window.

**Indicative:** five weeks of recording, one fast ETH fall (11-08 to 11-10), a handful of exits per row; the replay's margin is its own scan, not Deribit's simulator, and the minute runs carry the stale-book stop above.
The direction is consistent across the hourly runs, but the sample is too small to set a threshold on.

## Recommendation

- **No change from this study alone.** The evidence leans toward the stop alone (or a higher threshold) for 16-delta legs, but it rests on 0–5 exits per row and one fall.
- For a person, as a separate proposal with this report attached: re-run `-deltaexits all` on a longer recording (at least the 4-month evidence window) and on `cmd/stress`'s scripted shocks, where a gap past the stop is the case the delta exit exists for.
- Follow-up for the replay: with minute GEX records a deferred empty-book stop times out on the held hourly book (the caveat above); the stop's re-check should wait for the next hourly book, or the study should be read on `-gex none`.
