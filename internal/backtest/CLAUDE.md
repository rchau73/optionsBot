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
  Not modelled there: rebalance, leg balancing, churn breaker, journal-restored holds.
