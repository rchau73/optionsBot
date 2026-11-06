# internal/backtest

Folder notes for `CLAUDE.md` (the root file's trading rules apply here too): the backtest and the research tool `cmd/stress`.

```bash
scripts/stress_inputs.sh btc && go run ./cmd/stress -dir data/stress/btc -shock crash -calm 14   # stress test of the live book (README "Stress test")
go run ./cmd/stress -replay data/btc/data/market -from 2026-10-12 -dir data/stress/btc -direction sma -journal data/btc/orders.log   # the bot's rules on recorded books (README "Replay of recorded books")
```

- **`internal/backtest`** — CSV feed, `SimExecutor`, day-loop `Engine` using the pure rules at the simulated date, sweep (scenarios applied as slot matrices),
  ladder sweep (`ladder.go`: DTE ladders with/without weeklies → slot occupancy from `occupancy.go`, return on margin, worst week), walk-forward.
  As live, a DTE roll only closes (the slot re-enters); other exits are repaired on the same strangle and expiry.
- **`cmd/stress`** — research tool, not the bot: replays the live book (snapshot from `scripts/stress_inputs.sh`) through scripted shocks with the bot's pure rules and modelled prices/fills/fees.
  Keep its defaults equal to today's bot when a rule changes, and re-run it to compare rule changes on the same paths.
  Its trend is the bot's own (`strategy.SMALevelsOf`, which `GammaMonitor.SMALevels`/`detectTrend` call — swings included).
  `-replay <data/market dir>` (`internal/replay`, reader `recorder.ReadDir`) runs the bot's pure rules and its `GammaMonitor` (`PushPriceAt`) hour by hour on the recorded mainnet books:
  - GEX rebuilt with `gex.Build`, fills at the recorded bid/ask/mid by the bot's price floors, margin modelled;
  - where minute GEX records exist (`-gex`, default `market_gex` beside the books; `none` = hourly) it decides at each record on the last hourly book's prices (`replay/ticks.go`), hourly where they are absent;
  - `-journal` prints the bot's actual P&L over the window to measure the gap.
  - `-liqcaps 5,10,20` (study, #487, `replay/liqcap.go`): one replay per `liquidity_cap_oi_pct`, one table — IM as % of the limit, decisions under 60 % of it with entries allowed, entries and lots cut, worst week;
    days with no recording are listed and the table marked indicative (`docs/studies/liquidity-cap.md`). The bot's cap stays fixed at 10.
  - `-regimeholds confirmed,live` (study, #550, `replay/regimehold.go`): one replay per regime-side rule — the bot's (confirmed negative regime) and also on a live negative regime — one table:
    net, entries opened without the held side, side holds, legs sold into a live negative regime on the trend's side, worst week (`docs/studies/regime-hold.md`); `-regimehold live` runs one replay so.
  - `-sheds all` (study, #637, `replay/shed.go`): one replay per GEX shed variant (today, none, `partial:K`, `delta:D`, a perp `hedge`) at each `-costs` multiple (1,2), one table:
    net (coin, USD), shed closes per break, the named windows 9–17 Oct and 29 Oct–4 Nov, worst day/week, max MM, a −20 % gap on the held book, stops, MM cuts, a verdict against today's shed (`docs/studies/shed-variants.md`); `-shed hedge` runs one replay so.
  - `-fullstrangle today,hedged` (study, #677, `replay/fullstrangle.go`): today's rules vs the perpetual holding the book's coin delta (the bot's band and order limit) with the rules that leave a leg missing relaxed; `hedged:H` = a stopped leg waits H hours;
    at each `-costs` multiple, one table: net (coin, USD), legs missing, perpetual traded and fees, the named windows, worst day/week, avg IM, max MM, a −20 % gap, stops, MM cuts, a verdict; `-trace` prints each row's actions (`docs/studies/full-strangle-hedge.md`).
    Run it with `-start journal -limits journal -rebalance prorata`: from an empty book a variant can run a far smaller book, which the verdict refuses to compare.
  - `-rebalance prorata` (#638, `replay/rebalance.go`; default `off`, so earlier replays are unchanged): the bot's rebalance — a pass on a confirmed limit change (cuts to the exact limit),
    retries of a held or short top-up (cut only above limit + 1.5 pp, `DownsizeDue`), top-ups as `rebalancePositions`; `-limits journal` takes the limit, freeze and starts from the journal's `risk_limit` lines
    (each start a startup pass with the ±1.5 pp band), and with `-journal` the bot's downsizes are listed beside the replay's per day and side.
  - Starting balance (#638): `-equity`, else with `-journal` the bot's own margin balance at `-from` (its `risk_limit` lines and `data/pnl_history.jsonl`, `-pnlhistory`), else `account.json` (today's);
    none → an error, never a 1-coin default (it priced ETH's ~489 ETH book on 1 ETH).
  - `-rebalances prorata,cheapest,winners` (study, #638): which strangle a downsize cuts — downsize P&L, IM freed, cost per pp, IM and slot balance after the cut, top-ups (`docs/studies/downsize-cut.md`).
  Not modelled there: leg balancing, churn breaker, journal-restored holds; margin is the replay's scan, not Deribit's simulator.
