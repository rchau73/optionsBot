# Study: the futures hedge on a one-sided book (#812)

**Question.** Since the futures hedge went on (2026-11-05 21:43) the mvp books held calls only (confirmed negative regime, bear trend) and lost money although every call closed at a profit.
Which change to the hedge would have kept the bear book's result?

**No rule changes here.** `cmd/stress -replay -hedgemodes` replays the same recorded books with the bot's hedge and three changes; the product owner decides.

**Reading: hedging a one-sided book threw away its edge.**
On both coins and both windows, the hedge standing down on a one-sided book (`onesided`) or hedging only the delta that opposes the trend (`trend`) nets more than today's hedge (`bot`).
Trading the hedge after repairs and entries (`+after`) changes little at the replay's hourly pace.
Both windows are falling markets: a rally, where a calls-only book is the one at risk, is not in the recordings yet.

## Live (mvp, 2026-11-05 → 2026-11-16)

| | ETH | BTC |
|---|---|---|
| Option closes | +3.88 ETH (6 calls, all profitable) | +0.00002 BTC (two puts delta-exited on 11-08, calls profitable) |
| Futures hedge (8 fills each, net of fees and funding) | −5.18 ETH | −0.0027 BTC |
| Spot | 2,245 → 1,815 (−19 %) | 75,600 → 64,900 (−14 %) |

The calls-only book is short delta, a bet on the fall; the hedge bought a long perpetual against it (64,603 USD at 2,245 on 11-05) and lost through the fall.
It lost more than the calls gained because it trails: the calls' delta shrinks as the price falls while the perpetual is cut only at the band (5 % of the balance, ~15 ETH), and each drift roll re-sells calls nearer the price, so the hedge buys long again lower down.
Each roll also sold and bought back the perpetual minutes apart (11-12 11:21/11:31, 11-14 11:04/11:08, 19:08/23:13).

## Variants

| Mode | What the perpetual does |
|---|---|
| `off` | Nothing: the bot with the hedge off (today's GEX shed). |
| `bot` | The bot with the hedge on (#657): holds the options' coin delta at zero within 5 % of the balance, at most 12 orders/h; the shed closes nothing, entries still skip the shed side, the regime side holds. |
| `onesided` | (1) As `bot`, but stands down (perpetual closed, the shed back) while the book holds one option type only. |
| `trend` | (2) Hedges only the delta that opposes the trend: in a bear trend a long book delta, in a bull trend a short one, neutral all of it. |
| `+after` | (3) The hedge trades after repairs and entries in the decision, so a roll's close and its re-sell net out before it trades. |

## Results

Stable's recorded mainnet books, mvp's configs, 1× the recorded costs (2× in the tables of the PR).
A lot is priced without the perpetual in every mode (`replay.engine.size`): next to the hedge a short leg lowers the scenario IM, the per-lot IM came out ≤ 0 and `EntryLots` sold one lot, so the modes compared book sizes, not the hedge.

**The live window, 2026-11-05 → 2026-11-16**, from mvp's journal book and its balance (300.7 ETH, 0.5045 BTC):

| Mode | ETH net | Hedge P&L | Worst week | BTC net | Hedge P&L | Worst week |
|---|---|---|---|---|---|---|
| off | +4.14 (+1.38 %) | — | −0.24 % | +0.0105 (+2.08 %) | — | −0.12 % |
| **bot** | **−6.96 (−2.32 %)** | −11.05 | −1.47 % | **−0.0096 (−1.90 %)** | −0.0195 | −1.54 % |
| onesided | +4.14 (+1.38 %) | 0 | −0.24 % | +0.0105 (+2.09 %) | +0.0006 | −0.12 % |
| trend | +6.28 (+2.09 %) | +2.11 | +0.16 % | +0.0113 (+2.24 %) | +0.0014 | +0.04 % |
| bot+after | −6.99 (−2.32 %) | −11.08 | −1.44 % | −0.0096 (−1.89 %) | −0.0194 | −1.51 % |

**The whole recording, 2026-10-10 → 2026-11-16**, from an empty book:

| Mode | ETH net | Hedge P&L | BTC net | Hedge P&L |
|---|---|---|---|---|
| off | −123.41 | — | +0.0019 | — |
| **bot** | **−126.00** | −3.78 | **−0.0260** | −0.0242 |
| onesided | −120.94 | +3.90 | −0.0099 | −0.0081 |
| trend | −120.45 | +3.37 | −0.0075 | −0.0057 |
| bot+after | −127.08 | −3.95 | −0.0240 | −0.0212 |

ETH's long window is dominated by one replay stop on 11-03 08:06: a 2900 call bought back at an ask of 0.2665 ETH with spot near 2,400 (−124 ETH, every mode alike) — a broken recorded quote the replay's stop did not defer, not a hedge effect.

## Caveats

- Both windows fall (ETH −27 %, BTC −21 % from 10-10); the case the hedge is for on a calls-only book, a rally, is not tested.
- The replay sells about four times the live book's lots (~500 against ~100 ETH lots), so its hedge losses are larger than live; the signs agree.
- `trend`'s lead over `onesided` in the live window comes from the hours on 11-05 when the replay held puts too (its start repaired them at once), a long book delta it hedged into the fall.
- The verdict column of the tables reads "max MM above today's": the modes without a perpetual run 1–2 pp more maintenance margin than `bot`, as the bot with the hedge off does, far under `max_mm_pct`.
- `+after` is measured hourly; live, the roll round trips cost about the spread and fees (ETH: three pairs of ~35,000 USD, ~0.07 ETH), and under `onesided` a calls-only book has no hedge to round-trip.
