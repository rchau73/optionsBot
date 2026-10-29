# Study: the liquidity cap and the 60 % margin-use target (#487)

**Question.** What does `liquidity_cap_oi_pct` (fixed at 10, `internal/config/fixed.go`, #103) cost in margin use, and what does it buy in fill quality?
E1's outcome is margin use at 60 % of the IM limit or more whenever entries are allowed; a cap nobody has measured could be what holds it back.

**The rule is unchanged.** This is a study.
The cap stays 10 in `fixed.go`, and both effective-config golden files stay as they are.
Changing it is a separate story, and `OptionStrategy.md` changes only if a new value is adopted.

## Method

`cmd/stress -replay` runs the bot's pure rules on the recorded mainnet books (`internal/replay`, see `internal/backtest/CLAUDE.md`).
`-liqcaps 5,10,20` replays the same books with the same settings once per cap and prints one table per coin:

```bash
go run ./cmd/stress -replay data/btc/data/market -from <first recorded day> -dir data/stress/btc -currency BTC -liqcaps 5,10,20
go run ./cmd/stress -replay data/eth/data/market -from <first recorded day> -dir data/stress/eth -currency ETH -liqcaps 5,10,20
```

Run them from a checkout that can read the environment's recorder output (`data/btc` in the dev checkout links to stable's).
Add `-start journal -journal data/<coin>/orders.log` to start from the bot's own book, and `-to` to end the window.

The columns:
- **Avg IM (% of limit):** IM ÷ the IM limit in force, averaged over every decision (one per hourly book, or per minute GEX record where those exist).
- **Cycles under 60 % of limit, entries allowed:** of the decisions where an entry could go out (not frozen, no MM breach, no deferred stop, fresh GEX), the share whose IM stayed under 60 % of the limit (`strategy.UnderUseTargetPct`).
  This is the same test as the `im_under_use` alert (#369).
- **Entries cut or blocked:** entries whose margin size the cap cut (or blocked: under one lot), and the entries sent.
- **Lots cut:** the margin size minus the capped size, summed over the legs sold.
- **Legs capped:** legs whose own open interest set the cap.
- **Net, worst week:** the change of the margin balance over the window and over the worst 7 consecutive replayed days, as % of the starting balance.

Percentages and lots only: no amounts of a live bot appear in this file.

## Limits

- **Entries only.** The replay does not model rebalance top-ups, so a top-up the cap would have cut is not counted.
  The live bot journals each one as a `liquidity_cap: top-up` skip; `cmd/idlemargin` counts slots sized small by the cap.
- **Fill quality is not measured by the replay.** It fills at the recorded bid, ask or mid by the bot's price floors, whatever the size.
  The cap's benefit (#103: 1,454 contracts sent on a strike whose open interest was 164, 152 filled; mvp's 2–12 % of OI filled whole) is the evidence for the fill side.
  A wider cap can only look better in this replay on margin use; the cost it hides is a large order that does not fill, or moves the price.
- **Margin is modelled** (scan ±16 % spot, IV −20/+30 %, MM a fixed share of IM), not Deribit's.
- **Open interest is mainnet's,** as the bot reads it (the GEX manager's `OISnapshot`).
- **Short history is reported, not extrapolated.** When the window has days with no recording, or fewer than 7 replayed days, the table is marked **Indicative** and lists the missing days.

## Results

Not yet filled in.
The Dev agent that built the tool (#487) runs in an isolated worktree with no access to the recorded books, so it could not run the study.
A person (or an agent run from the environment) runs the two commands above and pastes each coin's table here, with the **Indicative** line the tool prints.

### BTC

_Pending: paste the `-liqcaps 5,10,20` table._

### ETH

_Pending: paste the `-liqcaps 5,10,20` table._

## Recommendation

Pending the results. How to read them:
- If the 5 → 10 → 20 rows show **Avg IM** rising and **cycles under 60 %** falling with **Lots cut** large, the cap is what holds margin use back.
  Then a wider cap is worth a separate story, checked against fill rates on testnet: the replay cannot show the fill cost.
- If the rows barely differ (**Lots cut** small at 10), something else holds margin use back, and `cmd/idlemargin`'s causes are where to look.
- A result marked **Indicative** is not a basis for changing the rule.
