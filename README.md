# optionsBot — automated short-volatility options bot for Deribit (Go)

[![CI](https://github.com/rchau73/optionsBot/actions/workflows/ci.yml/badge.svg)](https://github.com/rchau73/optionsBot/actions/workflows/ci.yml)
![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go)
![Coverage](https://img.shields.io/badge/coverage-87%25-brightgreen)

> **🤖 This is also an agentic-development project.** AI agents watch the running bots, file issues, and implement approved issues as tested pull requests, with a person approving what gets built and what gets merged. How it works and its guardrails: **[AGENTIC.md](AGENTIC.md)**.

A concurrent Go service that sells BTC/ETH option strangles on [Deribit](https://www.deribit.com), manages them through their life (fill tracking, take-profit, rolls, stop-loss, regime-based leg shedding, kill switch) and backtests the same rules on historical data.

> **⚠️ Disclaimer — read first.** This is a personal **software-engineering study**: a Go concurrency and system-design exercise that runs against a real derivatives exchange. It is **not** a professional trading tool and **not investment advice**. Selling options can lose far more than the premium collected, and crypto markets are extremely volatile. The bot runs on Deribit **testnet by default**; live trading requires an explicit `DERIBIT_ENV=live`. Use at your own risk.

**How the strategy works, in plain language, with examples:** [OptionStrategy.md](OptionStrategy.md).

---

## Contents
- [Highlights](#highlights)
- [Architecture](#architecture)
- [Data model](#data-model)
- [Key flows](#key-flows)
- [Concurrency model](#concurrency-model)
- [Getting started](#getting-started)
- [Configuration](#configuration)
- [Backtesting](#backtesting)
- [Stress test](#stress-test)
- [Idle-margin attribution](#idle-margin-attribution)
- [Recorded-history coverage](#recorded-history-coverage)
- [Evidence report](#evidence-report)
- [Testing and CI](#testing-and-ci)
- [Project layout](#project-layout)
- [Design decisions](#design-decisions)
- [Known limitations and roadmap](#known-limitations-and-roadmap)
- [Further documentation](#further-documentation)

## Highlights

- **Exchange gateway built for safety:** one WebSocket, priority lanes (stop-loss and kill switch first), Deribit's two rate-limit pools, a circuit breaker that never blocks risk-reducing orders, retries only for idempotent reads, reply routing by request ID (no head-of-line blocking), automatic reconnect with re-subscription.
- **Fill-safe order handling:** partial fills are booked as filled, timed-out entries keep their filled legs, closes use market or immediate-or-cancel orders so the result is always known, rollouts only close and a single fill-tracked path reopens.
- **Stateless by design:** the in-memory book is rebuilt from the exchange on every start (reconcile), so crashes and restarts are recoverable.
- **SOLID Go:** the strategy depends on small, consumer-defined interfaces; decision rules are pure functions (time passed in) shared by the live loop and the backtest.
- **Market-structure aware:** dealer gamma exposure (GEX) from **mainnet** open interest even when trading on testnet (testnet positioning isn't real), matching GestaoCarteira's `deribit_tc_export_v3.py` rules; DVOL-based IV percentile.
- **Decision journal with P&L:** every submit, amend, cancel, fill, close, reconcile and skipped entry in `orders.log` carries a market snapshot (spot, DVOL, IV percentile and skew, ITM/ATM/OTM and distance to strike, open interest of the strike/expiry with rank and max pain, GEX regime, spread), and P&L per strategy slot (realised, unrealised, coin and USD) is journaled periodically. **All P&L is net of Deribit fees** — each fill's fee is read from Deribit and booked (opening fees ride on the position, a close books its share plus the closing fee); the backtest charges the same fee (`commission_per_contract`, 0.03 % of the underlying, capped at 12.5 % of the option price).
- **Tested:** 87 % statement coverage, end-to-end strategy tests against a fake exchange, gateway integration tests against a mock Deribit WebSocket server, all under the race detector in CI, plus `govulncheck` and a Docker build.

## Architecture

![Architecture overview](docs/arch_overview.png)

<sub>Source: [docs/arch_overview.mmd](docs/arch_overview.mmd) · all diagrams: [docs/README.md](docs/README.md)</sub>

| Layer | Package | Role |
|---|---|---|
| Composition | `cmd/bot` | Flags, logger, config, wiring; `SIGUSR1` → kill switch; live or backtest mode |
| Platform | `internal/gateway` | The only Deribit connection: queue → rate limiter → circuit breaker → socket; replies routed by ID; reconnect |
| Market | `internal/marketdata` | Option chain, ticker/index/DVOL subscriptions, IV percentile, shared expiry-window rule |
| Market | `internal/gex` | Gamma exposure regime from open interest, refreshed every 60 s |
| Decision | `internal/strategy` | Entry, fill tracking, exits, repair, reconcile, rebalance, kill switch; pure rule functions |
| Execution | `internal/orders` | Deribit order/account calls, in-memory book (snapshots), `orders.log` decision journal (market snapshot + P&L) |
| Monitoring | `internal/api` | Read-only JSON API for the live monitor, built from in-memory state (no exchange calls); UI in [`frontend/`](frontend/README.md) |
| Reporting | `internal/hedge` | `hedge_report.json` — suggestion only, never trades |
| Research | `internal/backtest` | CSV replay, simulated fills, metrics, parameter sweep, walk-forward |
| Support | `internal/config`, `internal/logger` | `config.yaml` + `.env` with validation; `slog` JSON logging |

Full design notes: [docs/architecture.md](docs/architecture.md).

## Data model

![Data model](docs/data_model.png)

`StateManager` owns positions and strangles (a strangle links a call and a put leg by position ID). Entry orders live in a `pendingStrangle` until every leg is done; pure functions turn positions into `RolloutDecision`s and the GEX snapshot into a `GammaDecision`.

## Key flows

| Flow | Diagram |
|---|---|
| Startup and reconcile with the exchange | [seq_startup](docs/seq_startup.png) |
| Entry: sizing, gating, submit, fill tracking, amend, timeout | [seq_entry](docs/seq_entry.png) |
| Exit: stop-loss and rollouts, partial fills, who reopens | [seq_exit](docs/seq_exit.png) |
| Gateway request, reply routing, reconnect | [seq_gateway](docs/seq_gateway.png) |
| GEX refresh and leg gating | [seq_gex](docs/seq_gex.png) |
| Kill switch | [seq_kill_switch](docs/seq_kill_switch.png) |

![Exit sequence](docs/seq_exit.png)

## Concurrency model

| Goroutine | Purpose | Stops on |
|---|---|---|
| gateway `readLoop` / `dispatchLoop` | single reader / single writer per connection | connection context |
| gateway `heartbeatLoop`, `metricsLoop` | keep-alive, rate-limit metrics | connection context |
| gateway `reconnect` | at most one; restores the session | root context |
| marketdata consumer | applies ticker/index/DVOL pushes | root context |
| GEX refresher | recomputes the regime every 60 s | root context |
| `strategy.Run` | **all trading decisions** (never concurrent with each other) | root context / kill switch |
| strategy heartbeat | read-only status log every 60 s | root context |

Shared state is guarded by mutexes held only for in-memory work, never across a network call; readers receive copies. The test suite runs with `-race`.

## Getting started

**Prerequisites:** Go 1.25+, a Deribit **testnet** account with an API key (`account:read`, `trade:read_write`); Docker optional; [`mmdc`](https://github.com/mermaid-js/mermaid-cli) only to re-render diagrams.

```bash
git clone https://github.com/rchau73/optionsBot && cd optionsBot
cp .env.example .env              # add your testnet key; DERIBIT_ENV=testnet
make check                        # gofmt + vet + race tests
make hooks                        # once per clone: blocks committing .env files or any .env secret (the repo is public)
make build                        # → ./bot
./bot --config config_btc.yaml    # trade on testnet
./bot --config config_btc.yaml --debug
```

With Docker, as one of the two local environments, **stable** (a promoted release) and **mvp** (`main`). Each has its own clone and its own Deribit account, and they share nothing ([operations guide](docs/operations.md#environments-stable-and-mvp)). Both live in `optionsBot-env/` beside this checkout. Run these commands **from this checkout**, never from inside an environment's clone:

```bash
scripts/envctl.sh stable init && scripts/envctl.sh stable up   # then http://localhost:3000
scripts/envctl.sh stable logs -f bot-btc
scripts/promote.sh v0.3.0                                      # move stable to a release that ran 3 days in mvp
```

| Command | What it does |
|---|---|
| `./bot --config config_btc.yaml` | trade (testnet unless `DERIBIT_ENV=live`) |
| `./bot --mode=backtest --config config_btc.yaml --from=2023-01-01 --to=2023-12-31` | backtest + walk-forward |
| `./bot --mode=backtest --config config_btc.yaml --sweep=true --from=… --to=…` | parameter sweep |
| `kill -USR1 <pid>` / `docker kill -s USR1 <container>` | kill switch: flatten and halt |

Day-to-day operation (logs, health monitor, kill switch, BTC + ETH, troubleshooting): [docs/operations.md](docs/operations.md).

## Configuration

Two files with a strict split: **`config.yaml`** (or `config_btc.yaml` / `config_eth.yaml`) holds strategy logic; **`.env`** holds platform settings. Config is validated on startup and a bad value stops the bot with a clear message.

### Strategy — `config_*.yaml`

Only the settings that are tuned live in the YAML files (2026-10-14: 47 keys became 14). Everything else is fixed in code (`internal/config/fixed.go`, table below); a file that still sets a fixed key fails at startup and names it, so nobody believes a changed value is in force. To make one tunable again, give its field back its `yaml` tag and drop it from `ApplyFixed`. `tests/config_effective_test.go` pins every setting both shipped configs produce, so moving a key between the file and the code cannot change a value unnoticed.

| Key | Default | Meaning |
|---|---|---|
| `underlying` | — | `BTC` or `ETH` |
| `strategy_id` | `short-strangle` | names the strategy in `orders.log` and in Deribit order labels (`<id>:<dte>d:<delta>`) |
| `dte_delta_matrix` | — | slots: each `dte` with one or more `deltas`; one strangle per (DTE, delta) |
| `min_premium` | 0 | skip legs whose price is below this, in the underlying's coin (0 = off; was `min_premium_btc`) |
| `rollout_dte` | — | roll a leg when days to expiry ≤ this; every slot DTE must be above it |
| `delta_drift_threshold` | — | roll a leg when \|delta\| falls below this (little premium left). At most 75% of every entry delta, or a new leg would be rolled by the first small move, over and over |
| `roi_take_profit` | — | roll a leg once this share of the premium is captured (0.5 = 50 %) |
| `stop_loss_multiplier` | — | close at market when the loss reaches this × premium received |
| `delta_exit_threshold` | 0.30 | close a short leg (IOC at the ask) when its \|delta\| reaches this — before the stop; the leg is then held like a stopped one. Must be above every entry delta |
| `repair_cooldown_hours` | 72 | a stopped-out leg is re-sold no sooner than this, and only when entries are not frozen and the confirmed gamma regime is not negative (rolled legs reopen at once) |
| `gamma_flip_buffer_sd` | 1 (shipped 1.5) | GEX sheds a leg only when spot is this many daily standard deviations (DVOL ÷ √365) beyond the gamma flip — at 1.5, about 3.0 % for BTC and 4.0 % for ETH at today's DVOL (1 until 2026-10-14: 1.5 shed less and did better in today's market, about 1 % worse in the general setup); scales with each asset's volatility (0–3) |
| `gex_direction` | `sma` | the side to avoid in a negative regime: `sma` (SMA trend) or `bias` (the script's acceleration bias, spot vs flip ± buffer); BTC trials `bias` since 2026-10-12 |
| `iv_margin_bands` | ≥70 → 50, ≥30 → 35, ≥0 → 20 | initial-margin limit (% of Deribit's margin balance) by DVOL IV-percentile band; the lowest band must start at 0. A confirmed negative gamma regime forces the lowest band and, in a trend, blocks new shorts on the side the trend runs toward |
| `max_mm_pct` | 35 | maintenance-margin limit (% of margin balance), enforced at once by reducing positions; below 100 (Deribit liquidates at 100) |
| `max_leg_size_multiple` | 2 | a new leg is at most this × its slot's normal size (slot share ÷ the strangle's margin per lot on its own) — book offsets can make a leg look almost free |
| `backtest.*` | — | fill model, slippage, limit-fill rule, commission (backtest only) |

**Fixed in code** (`internal/config/fixed.go`; the former key in brackets):

| Value | Rule |
|---|---|
| GEX method `script` (`gex_method`), strikes ±15 % (`gex_strike_range_pct`) | GestaoCarteira's rules (regime = sign of weighted GEX, flip = lowest crossing), checked by `tests/gex_parity_test.go`. The `nearest_flip` method and its 0.025 band (`gamma_regime_band_pct`) remain in code, unused |
| 2 snapshots (`gex_shed_confirm_snapshots`, `gex_repair_confirm_snapshots`) | a leg is shed only once the signal has held on 2 consecutive GEX snapshots (the flip can jump for a minute); a shed leg is re-sold once spot is back above the flip **it was shed at** plus the buffer on 2 snapshots… |
| 24 h (`gex_repair_release_hours`) | …or once the live regime has been non-negative this long |
| 24 h (`gex_bias_repair_cooldown_hours`), 60 min (`gex_bias_flip_window_minutes`) | under `bias`: a shed leg waits 24 h; the bias must agree against the live flip and the last hour's median flip, and sheds nothing until that hour of flips is recorded (after a restart: `bias_warmup`) |
| 120 days, 5 bars (`gamma_trend_lookback_days`, `swing_pivot_n`) | SMA trend from daily closes |
| 252 days (`iv_percentile_window`), 2 closes (`iv_band_confirm_days`) | DVOL IV percentile; a band or regime change must hold on 2 UTC daily closes (until then new risk is frozen when the change would lower the limit) |
| 20 %, 5 min (`stop_spread_guard_pct`, `stop_spread_max_wait_minutes`) | a triggered stop waits while the ask is more than 20 % above the mid (an emptied book), at most 5 minutes, then at market |
| 3 in 60 min (`churn_max_round_trips`, `churn_window_minutes`) | churn breaker: a slot that buys back and re-sells 3 times within an hour is paused for the hour (exits still run) |
| 15 min (`rebalance_retry_minutes`) | a rebalance complement that times out short re-runs the rebalance after this |
| ±1.5 pp (`rebalance_startup_band_pct`) | the first rebalance after a startup does nothing while IM is within 1.5 percentage points of the limit (journaled `rebalance_in_band`); confirmed limit changes and the MM cut ignore it |
| 10 % (`liquidity_cap_oi_pct`) | an entry or top-up order is at most 10 % of each sold strike's mainnet open interest, both legs at the smaller size; journaled once as `liquidity_cap`; under one lot nothing is sent (#103). Repairs, exits and the MM cut are not capped. `cmd/stress -replay … -liqcap N` |
| 3, 60 min (`rebalance_backoff_after`, `rebalance_retry_max_minutes`) | from the 3rd rebalance pass in a row whose complements all timed out unfilled (the bot's own cancels don't count) the retry wait doubles (30, then 60 min at most); a fill or a limit change resets it |
| 90 s, ask → mid → floor (`order_fill_timeout_sec`, `entry_price_floor` mid, `repair_price_floor` bid) | a resting sell offers at the ask for the first third of 90 s, then mid (rounded up to a tick), then the floor — mid for entries and top-ups, bid for repairs; the premium floor is checked at that price. Without a bid it stays at the ask. The same 90 s is the window of a take-profit, delta drift or DTE roll buy-back resting at the mid (rounded down a tick) before the rest crosses with an IOC at the ask (#239; journaled `exit_step` mid/ask); stop-loss, MM cut and delta exit never rest, and each cancels a resting close first |
| 5 %, 3 times (`order_slippage_pct`, `order_max_adjustments`) | follow the ask up when it drifts more than 5 %, at most 3 times (moves down are not capped) |
| ±10 days, × 1.5 (`max_dte_deviation`, `expiry_stretch`) | an expiry may differ from the slot DTE by 10 days; when another slot holds it, the nearest free one up to DTE × 1.5, otherwise wait |
| 0.03 (`delta_slippage`) | max distance from the target delta. An entry picks its call and put **as a pair** within this band; a repair takes the strike nearest the target |
| 0.1 (`min_trade_amount`) | lot size only when Deribit gives none (it is read per option) |
| 40 s, 60 s (`eval_interval_ms`, `report_interval_sec`) | decision-loop period; heartbeat and P&L journal line |
| 60 min (`market_record_minutes`) | market history for backtests and stress tests: the coin's options as Deribit **mainnet** quotes them (bid/ask/mid/mark, mark IV, forward, open interest, Black–Scholes greeks, DVOL) to `data/market/YYYY-MM-DD.csv.gz`, reusing the GEX poll; the USDC-settled options (prices in USDC) to `data/market_usdc/`, one extra mainnet call, for a liquidity comparison |
| 5 % (`spread_alert_threshold`), 0.05 (`hedge_report_threshold`) | journal warning on wide fill spreads; `hedge_report.json` from this \|net delta\| |

### Platform — `.env`

| Key | Default | Meaning |
|---|---|---|
| `DERIBIT_CLIENT_ID`, `DERIBIT_CLIENT_SECRET` | — | API credentials (needed to trade, not to backtest); never logged |
| `DERIBIT_ENV` | `testnet` | `testnet` or `live` (anything else is rejected) |
| `DERIBIT_RATE_WS_NONMATCH_RPS` / `_WS_MATCH_RPS` | 20 / 8 | Deribit credit pools: general requests / order operations |
| `DERIBIT_RATE_SAFETY_FACTOR` | 0.80 | use at most this share of each limit |
| `DERIBIT_RATE_MAX_SUBSCRIPTIONS` | 1000 | subscription cap |
| `DERIBIT_BACKOFF_*` | 500 ms × 2, max 30 s, 6 retries | retry policy for read-only calls |
| `DERIBIT_CIRCUIT_BREAKER_THRESHOLD` / `_OPEN_SEC` | 5 / 60 | failures that open the breaker / how long it stays open |
| `DERIBIT_HEARTBEAT_INTERVAL_SEC` | 15 | server heartbeat interval |
| `DERIBIT_RECONNECT_MAX_ATTEMPTS` / `_BACKOFF_BASE_MS` | 10 / 1000 | reconnect policy before giving up |
| `BOT_ACCOUNT_POLL_SEC` | 10 | how often the account/collateral summary is polled for the monitor (one read-only call; minimum 2) |
| `BOT_API_ADDR` | empty (off) | address of the read-only monitor API, e.g. `127.0.0.1:8081`; Docker Compose sets it per service |
| `BOT_ADMIN_TOKEN` | empty (off) | secret for the monitor's state-changing actions, bearer token; Docker Compose passes it to the monitor. **Manual close** (`POST /api/positions/close`): testnet only, and only for a leg under the regime-side block. **Manual rebalance** (`POST /api/rebalance[?dry_run=true]`): live and testnet, same gates as the automatic rebalance |

## Backtesting

```bash
go run ./cmd/gendata                                       # synthetic data → data/historical/options.csv
./bot --mode=backtest --config config_btc.yaml --from=2026-01-01 --to=2026-12-31
./bot --mode=backtest --config config_btc.yaml --sweep=true --from=2026-01-01 --to=2026-12-31
./bot --mode=backtest --config config_btc.yaml --sweep-ladders=true --from=2025-01-01 --to=2026-10-15
```

The engine replays the CSV day by day and applies the same pure decision functions as the live bot **at the simulated date**. Output in `data/results/`: `summary.json` (Sharpe, Sortino, Calmar, drawdown, win rate, exits by reason), `trades.csv`, `equity_curve.csv`, `drawdown.csv`, `scenario_comparison.csv` (sweep, ranked by Sharpe), `ladder_comparison.csv` (ladder sweep, below) and `walk_forward/` (train/validate windows, overfit flag at > 30 % Sharpe degradation).

As live, rollouts only close: a DTE roll frees the slot once both legs are gone and the slot re-enters through `SelectSlotExpiry`; any other exit is repaired at once on the same strangle and expiry (the live repair hold after a stop is not modelled).

**Ladder sweep** (`--sweep-ladders`, issue #89): runs the DTE ladders 25/50, 30/50, 30/60 and 35/60 (each slot at the config's entry deltas), each with and without weeklies (monthlies = the last Friday of a month), and reports per ladder the share of days each slot held a live leg (`slot_occupancy`, `occupancy_pct`, `min_slot_occupancy_pct`), net P&L, `return_on_margin_pct` (net P&L ÷ mean premium held — the backtest's margin stand-in, comparable between ladders only), the worst 7-day equity change (`worst_week_usd`/`_pct`), max drawdown, trades and `shared_expiry_days` (days two slots held one expiry; the one-expiry-per-slot rule keeps it 0).

CSV columns: `date, instrument, underlying, underlying_price, strike, expiry, option_type, bid, ask, mid, delta, gamma, theta, vega, iv, dvol_index`. The generator produces USD prices with monthly expiries — good for testing mechanics, not for judging profitability. Real Deribit history is available from the API (candles, DVOL, expired instruments) or vendors such as Tardis.dev.

## Stress test

How the **live book** would fare through a violent move, under the bot's real rules. `cmd/stress` starts from a snapshot of the running bot and replays scripted paths, one decision every 4 hours, applying the same pure functions as the bot — the exit rules with the delta exit (`EvaluateLeg`), GEX shedding with the flip buffer, the margin policy (DVOL bands, MM reduction), repair holds, expiry and strike selection, sizing and the 2× cap. Prices (Black–Scholes on a DVOL smile), spreads, margin and fills are modelled, with Deribit's fee (0.03 % per contract); results are net. It never connects to Deribit and places no orders.

```bash
scripts/stress_inputs.sh btc                                # snapshot → data/stress/btc/ (monitor must be up)
go run ./cmd/stress -dir data/stress/btc -shock crash -calm 14             # day-by-day table + summary
go run ./cmd/stress -dir data/stress/btc -shock rally40 -calm 14 -quiet    # summary only
scripts/stress_inputs.sh eth && go run ./cmd/stress -dir data/stress/eth -currency ETH -volscale 1.4 -slots 30,50 -shock chop -calm 14
```

| `-shock` | path (after `-calm N` quiet days with DVOL sliding to its lows) |
|---|---|
| `crash` / `rally` | two weeks, about −25 % / +25 %, DVOL spikes, volume surges |
| `crash40` / `rally40` | the same, about −40 % / +40 % |
| `chop` | two weeks of violent back-and-forth with no trend |
| `quiet` | the squeeze was a false alarm: two more quiet weeks |
| `normalN` / `longN` | ordinary random days (seed N), realised vol = `-vrp` × DVOL |

Inputs (`scripts/stress_inputs.sh`): `positions.json` and `account.json` from the bot (via the monitor), `market.json` with a year of DVOL and perpetual daily closes from Deribit's public mainnet API. The summary line leads with the result **versus simply holding the coin**, in coin (equity − starting equity) and in USD (that coin difference at the final price), with its worst day; then equity in coin and in USD next to holding, the coin's move, realised P&L net of fees, stops, GEX sheds, rolls, peak IM and MM. The day-by-day table has the same "vs holding" column. A short strangle on coin collateral loses a little coin in a rally (the calls cost more coin to buy back) while the USD value still rises with the coin — the comparison shows how much of the move the strategy gave up or added. Flags default to today's bot (BTC's slot matrix: pass `-slots 30,50` for ETH); `-bands low,mid,high` varies the IM limits of the DVOL bands (default 20,35,50); exit rules can be varied for comparison (`-tp 0.65` take-profit, `-drift 0` no delta-drift roll, `-breach`, `-pairstrikes`) and so can GEX shedding (`-shedrepair live|cross|anchored`, `-shedpersist`, `-flipjumps`/`-jumpsize` to model the flip jumping when open interest shifts); `-condor` and its sub-flags re-test the squeeze-protection condor proposal (rejected — kept for comparison). `-shock flash` replays a 2025-10-10-style flash crash: a quiet evening, then a V-shaped wick of `-wick` % (default 40 BTC / 80 ETH, as on the worst single venues; Deribit's index fell roughly 12–18 % from just before the cascade) that recovers within `-wickmin` minutes (`-wickkeep 1`: it does not recover), with DVOL ×`-wickdvol` and a `-wickspread` half-spread at the bottom, then a slide at normal speed — run it with `-steps 1440` (a decision a minute, like the live loop). `-stopslip 0.20`/`-stopslipwait 5` is the bot's stop spread guard (on by default; `-stopslip 0` = stops at market); `-stopslipmm` also defers the MM cut (rejected: margin ran past liquidation). `-livetrend` (default, as the bot: the live price against the daily SMAs; `false` = the last daily close) and `-intranoise` (an intraday wiggle of that many daily σ that still closes each day on the path) model the trend flickering within a day; `-sidehold` (keep a side blocked by the trend for N hours after the trend leaves) was tested on 2026-10-09 and rejected — within ±2.5 % at 4–8 h, and 12–24 h cost BTC up to 16 % in persistent regimes. `-stopcap` (a buy-back limit at a multiple of the premium) and `-velguard`/`-velpause`/`-shockfreeze` (defer market exits, freeze new risk after a fast move) test rejected alternatives; off by default. Limits: one path per scenario, decisions every 4 hours (no intraday gaps), modelled prices and fills; day 0 can show a jump where the model's marks differ from the live ones. Use it to compare rule changes on the same paths, not to forecast P&L.

The trend on the modelled paths is the bot's own (`-bottrend`, default): `strategy.SMALevelsOf`, the function `detectTrend` runs, over the path's daily closes and the live spot — bull on a break above the last confirmed swing high (5 closes each side) or SMA9 > SMA21 with price above SMA9, bear on the mirror. Before #120 the stress model had no swing breakouts and on a rally stayed neutral where the bot turned bull; `-bottrend=false` keeps that old model (SMA9 vs SMA21 with the close beyond SMA21) for comparison.

### Replay of recorded books

`-replay` runs the bot's rules on **real history** instead of a modelled path: the mainnet option books the bot records every hour (`data/market/YYYY-MM-DD.csv.gz`, `internal/recorder`; BTC and ETH since 2026-10-07). At each recorded hour it builds GEX with `gex.Build` from the recorded open interest, feeds the bot's own `GammaMonitor` (SMA trend with swings, `StableBiasDir` with the median flip, shed confirmation), evaluates the margin policy (`risk.Evaluate` on the DVOL and regime daily closes), and runs the cycle in the bot's order: GEX shed → exit rules (`EvaluateLeg`, stop spread guard) → MM cut → repairs (the `repairGate` chain: shed side, rollout window, regime side, shed anchor or bias cooldown, stop hold) → entries (`SelectSlotExpiry`, `SelectStrikePair`, the sides GEX and the regime-side rule allow, `EntryShare` sizing, the 2× cap, post-trade fit). Prices are the recording's: sells at the bot's price floor (entries and top-ups the mid, repairs the bid; no bid → no fill), buy-backs at the ask (no ask → no fill, retried next hour), Deribit's fee; marks are the live mid as the bot reads a ticker.

```bash
# the bot's settings (config_btc.yaml, i.e. bias), then the same history on sma
go run ./cmd/stress -replay data/btc/data/market -from 2026-10-12 -dir data/stress/btc -journal data/btc/orders.log
go run ./cmd/stress -replay data/btc/data/market -from 2026-10-12 -dir data/stress/btc -direction sma
go run ./cmd/stress -replay data/btc/data/market -from 2026-10-12 -dir data/stress/btc -start journal -journal data/btc/orders.log -quiet
```

The settings are the bot's config (`-config`, default `config_<currency>.yaml`); a stress flag given on the command line changes the rule it names (`-direction sma|bias`, `-flipbufsd`, `-breach`, `-drift`, `-tp`, `-cap`, `-bands`, `-shedpersist`, `-shedcross`, `-shedrelease`, `-biascooldown`, `-biasflipwindow`, `-stopslip`, `-stopslipwait`); a flag of a proposal the replay does not model (`-condor`, `-velguard`, …) is refused. `-from`/`-to` bound the traded days — everything recorded before `-from` only warms up GEX, DVOL and the regime (the regime needs 2 recorded daily closes before anything is confirmed, so start ≥ 2 days after the first recording). `-dir` gives `market.json` (daily closes and DVOL before the recording: the SMA trend needs 21 closes, the IV percentile 20 days) and `account.json` (the starting margin balance; `-equity` overrides). `-start journal` starts from the journal's open book at `-from`; `-journal` also prints the bot's **actual** P&L over the same window — its realised closes plus the change of its open book's unrealised P&L, both ends at the recorded mids — and the gap to the replay. `-trace` prints every action. Output: one line per day (spot, DVOL, live/confirmed regime, direction, limit, IM %, equity, day P&L, fees, trades, sheds, repairs, actions), a summary (net, realised, fees, trades, entries, repairs, sheds, stops, delta exits, rolls, MM cuts, unfilled orders, worst drawdown, IM max/avg) and a tab-separated `REPLAY` line for scripts.

**GEX at the bot's pace** (#122). Hourly books cannot see a flip jump that lasts minutes and is confirmed within two snapshots, as the live bot does. Since #122 the bot also records every GEX snapshot (one a minute, `data/market_gex/`, see `docs/operations.md`). Where those records exist, the replay decides at each one: regime, flip, bias (median window), shed confirmation and repair anchors at the minute, spot and DVOL from the record. Option prices, greeks and fills stay those of the **last hourly book** (held, not interpolated): an exit fires on that book's mark and buys back at its ask until the next book arrives, and a book with a minute record within 2 minutes does not decide on its own. An hour without records falls back to its book's rebuilt GEX; a record more than 2 h after the last book is skipped (no prices). `-gex <dir>` names the records' folder (default `market_gex` next to the `-replay` folder); `-gex none` runs hourly, to compare the two gaps to the journal on the same window. The header says how many decisions ran on minute records. Recording started with #122, so the minute replay covers only the days since.

**Limits — read before trusting a number.** One decision per recorded hour (live: every 40 s): the confirmation counts that are "2 snapshots" live (≈2 minutes) take 2 hours here, a stop's 5-minute spread wait ends at the next hour, and the bias's 60-minute median-flip window is widened to two snapshot intervals (else it would never hold two flips). Spot is the nearest expiry's future (the index is not recorded). Margin is MODEL (worst loss over spot ±16 % × IV −20/+30 %, MM = 80 % of IM), not Deribit's simulator; fills are assumed at the floor whenever that side of the book is quoted — mainnet prices, while the bots fill on testnet. Not modelled: the rebalance top-ups, leg balancing, the churn breaker, stop/shed records restored from the journal, and the bias's restart warm-up. So the replay compares rule sets on the same real books; it does not reproduce the journal to the satoshi. The gap to the journal (`-journal`) is printed on every run and should be recorded with any verdict drawn from it.

**Direction trial scorecard** (#245): `go run ./cmd/evidence -compare-direction -week 2026-W43` puts the two GEX direction sources side by side for one week — live (stable's BTC on `bias`, mvp's on `sma`: GEX sheds, repairs of shed sides, stop-losses, hours each source spent shedding calls and puts from `data/direction_history.jsonl`, with the unused source's shadow, realised net P&L in total and per slot, and the book marked at the recorded mids) and replayed twice on stable's recorded books from stable's journal book at the week start, once per source (only `gex_direction` differs, so the market path is identical), with the live − replay gap of each source. Live rows are labelled "different accounts and books: not a controlled test", replay rows "model"; every row names its files, days without recorded books are listed (never filled in), and every figure is a count, hours or a percentage of each environment's equity (`data/stress/btc/account.json`) — no amounts. No recommendation: it lists facts. Output: `<stable>/data/evidence/direction/<week>.md|.json` (`-stable-root`, `-mvp-root` override the clones).

## Idle-margin attribution

`cmd/idlemargin` (#218, epic E1) says **why** a bot used less than its approved margin. `utilisation_7d`/`utilisation_30d` (`history.Utilisation`, #174) give the number; this splits it into causes, from the bot's own files only (no Deribit call, nothing written):

```bash
go run ./cmd/idlemargin -journal data/btc/orders.log -history data/btc/pnl_history.jsonl -config config_btc.yaml -days 7
go run ./cmd/idlemargin -journal data/eth/orders.log -history data/eth/pnl_history.jsonl -from 2026-10-12 -to 2026-10-19
```

- **Per slot** (`internal/idlemargin`, pure `Attribute`): hours and share of measured time `occupied` (an open strangle, even one-legged, or a working entry order), `occupied_liquidity_cap` (occupied after a `liquidity_cap` skip: its entry or a top-up was cut), `frozen` (vacant while `entries_allowed` was false, whatever the skip), vacant by the last skip reason journaled since the slot became vacant (`no_free_expiry`, `margin_limit`, `gex_wait`, … — an unknown reason is its own row), `no_skip_logged` (vacant, no skip yet), and `not_measured` (the bot down or no margin data: the time `history.Utilisation` does not count, a point standing for at most `MaxSampleGap` 15 min). The total column sums slot-hours. `-config` reports that config's slots only; without it, every slot the journal names.
- **Per bot**: the utilisation `history.Utilisation` gives for the same window (the same number as `/api/status`), and the allowed time by book state — `slot_empty` (some slot vacant), `rebalance_waiting` (every slot occupied, a short top-up waiting for its `rebalance_retry`), `sized_small_liquidity_cap` (every slot occupied, one capped), `all_full` — each with its hours, mean IM ÷ limit and its share of the unused margin (Σ (1 − IM ÷ limit) × time). The states' means, weighted by hours, give back the utilisation.

Output holds hours, shares and percentages of the IM limit only — no amounts. Approximations: occupation follows the journal (opens, closes, startup `reconciled` lines; a leg held to settlement ends at 08:00 UTC of its expiry; a working entry order the journal never settled counts for at most 15 min); `occupied_liquidity_cap` cannot tell an order sent at the cap from a later uncapped one after a blocking `liquidity_cap` skip.

## Recorded-history coverage

`cmd/datacoverage` (#233, epic E2) says how much recorded history stands behind a replay or a backtest, and where it has holes. It reads one bot's files only (no Deribit call, nothing written) and prints counts and dates, never an amount:

```bash
go run ./cmd/datacoverage -dir data/btc                                   # first → last recorded day
go run ./cmd/datacoverage -dir data/btc -from 2026-10-12 -to 2026-10-18   # a replay's window (-to day included)
go run ./cmd/datacoverage -dir data/eth -json
```

`-dir` is the bot's folder (`data/btc`, files in `data/btc/data/`) or its data directory. For each dataset — `market` (hourly books), `market_gex` (minute GEX), `market_usdc` (hourly USDC books), `pnl_history.jsonl` (a point a minute) — it reports the first and last record, the bins of the window (an hour or a minute, the dataset's recording period) covered and their share, the hours missing, the days with records, complete and without any, the hours missing per day and the gaps (the 10 longest in text, all in `-json`). An empty bin between two records at most 1.5 periods apart is a ticker's jitter, not a gap. Then the **days a replay decides hourly**: books but no minute GEX record (`cmd/stress -replay` falls back to the books there). Without `-from`/`-to` the window runs from the first to the end of the last recorded day, never past the current hour; days of a window outside the data are reported missing, never filled in. Pure core: `coverage.Coverage(times, from, to, step)` in `internal/coverage`.

## Evidence report

`cmd/evidence` (#176, epic E2) writes the weekly evidence a sceptical investor asks for — does the bot earn its risk? — per bot of an environment, to `data/evidence/<week>/<bot>.md` and `.json` in that environment's clone (amounts stay in `data/`, gitignored). It reads the environment's files only: no Deribit call, no order.

```bash
scripts/stress_inputs.sh btc data/stress/btc                    # the week-end book for the stress section (monitor up)
go run ./cmd/evidence -env stable -week 2026-W42                # both bots of stable, ISO week (Monday–Sunday UTC)
go run ./cmd/evidence -env mvp -from 2026-10-12 -to 2026-10-18 -books-root ~/dev/go/optionsBot-env/stable
go run ./cmd/evidence -env stable -bot eth -stress-cmd none     # no week: the last complete one; no stress section
```

Sections, each naming its source:

- **Missing inputs** — the UTC days of the week without a P&L history point, a recorded book or a minute GEX record. The figures use the other days; nothing is filled in.
- **Live** — the journal's opens and closes in the week: realised P&L net of fees, the fees (opening share + closing fee, as `buyToClose` books them), closes by reason; the P&L history's `internal/perf` figures (net, % of equity, max drawdown, worst 7 days) and the unrealised P&L at the week's end.
- **Replay** — the bot's rules (`internal/replay`, the environment's config: `BOT_CONFIG_<BOT>` of its `deploy.env`, else `config_<bot>.yaml`) on the recorded mainnet books of the week, starting from the journal's book at Monday 00:00, deciding on minute GEX records where they exist; 7 days of earlier recording warm it up. Books are mainnet data: mvp records none, so pass stable's clone as `-books-root`. Needs the starting margin balance (`-equity`, one bot only, else `account.json` in `data/stress/<bot>/` — the balance when captured).
- **Gap** — replay − live over the replay's window, the live side being the journal marked at the recorded mids (`replay.ActualPnL`, as `cmd/stress -replay -journal`), with the list of what the replay does not model.
- **Stress** — the standard shocks (`crash`, `rally`, `crash40`, `rally40`, `chop`, no calm days) of `cmd/stress` on the book in `data/stress/<bot>/` with the config's slots, exits, cap, flip buffer, direction and bands (`-stress-cmd`, default `go run ./cmd/stress`, run from the repository; `none` skips). The book's capture time is shown and flagged when it is more than a day from the week's end: run `scripts/stress_inputs.sh` at the week's end first.
- **Benchmark** — holding the coin over the week (spot at the first and last history point), the strategy's USD return and its excess over holding.
- **Margin utilisation** — `history.Utilisation` over the week (the figure of `/api/status`).

A weekly schedule is not installed yet: `install.sh` will run it on Mondays once the report has been reviewed.

## Testing and CI

```bash
make test      # go test ./tests/... -race
make cover     # coverage across ./internal/... (tests live in ./tests)
make check     # what CI runs: gofmt, vet, race tests
```

| Package | Coverage | Highlights |
|---|---|---|
| strategy | 85 % | black-box tests of `Strategy.Run` against a fake exchange: entry pricing, premium floor, partial fills, timeouts, amend on drift, rollouts, stop-loss, GEX gating, repair, reconcile, rebalance, kill switch |
| gateway | 87 % | real `Gateway` against a scriptable mock Deribit WebSocket: no head-of-line blocking, breaker policy, retry policy, fail-fast on disconnect, reconnect with re-subscription |
| marketdata, gex, orders, config, hedge | 92–97 % | push handling, DVOL history, expiry window, order journal, validation |
| backtest | 81 % | historical-date regression, writers, sweep, walk-forward |

Regression tests for critical fixes were mutation-checked: they fail when the fix is reverted. CI (`.github/workflows/ci.yml`) runs formatting, vet, race tests with a coverage summary, `govulncheck` and a Docker build on every push and pull request.

## Project layout

```
cmd/bot/              main: wiring, flags, signals
cmd/gendata/          synthetic historical data generator
cmd/stress/           stress test of the live book through scripted shocks, and -replay of recorded books (see Stress test)
cmd/idlemargin/       why approved margin went unused, per slot and book state (see Idle-margin attribution)
cmd/datacoverage/     days, gaps and spans of a bot's recorded history (see Recorded-history coverage)
internal/coverage/    that report (pure Coverage) and its readers
cmd/evidence/         the weekly evidence report: live, replay, gap, stress, hold (see Evidence report)
internal/evidence/    builds that report (pure Build) and reads its inputs
scripts/              stress_inputs.sh — captures the stress test's inputs
internal/recorder/    hourly mainnet book recording (data/market/) and its reader
internal/replay/      the bot's rules over recorded books (cmd/stress -replay)
internal/perf/        pure performance summary of the P&L history: net return (coin, USD), hold-the-coin benchmark, max drawdown, worst 7 days, missing days
internal/config/      config.yaml + .env loading and validation
internal/gateway/     Deribit WebSocket client (queue, limits, breaker, retry, reconnect)
internal/marketdata/  option chain, subscriptions, DVOL / IV percentile, expiry window
internal/gex/         gamma exposure and regime
internal/strategy/    decision loop (open, pending, close, repair, reconcile, rebalance, killswitch) + pure rules
internal/orders/      executor, in-memory book, order journal, price/lot rounding
internal/hedge/       hedge report
internal/backtest/    feed, simulated executor, engine, metrics, results, scenarios
tests/                all tests (unit, integration, end-to-end)
docs/                 architecture, operations, Mermaid sources + PNGs
frontend/             live monitor (Next.js) — see frontend/README.md
cmd/monitor-demo/     monitor API with simulated data, for UI development
```

## Design decisions

| Decision | Why |
|---|---|
| One WebSocket per process, all calls through the gateway | Deribit limits are per account; one place enforces them, prioritises risk orders and observes health |
| Writer never waits for replies | a slow reply must not delay a stop-loss queued behind it |
| Never retry orders | a "failed" order may already be on the book; retrying could double the position |
| Exchange as source of truth, state in memory | no database to drift out of sync; reconcile on start is simpler and safer |
| Rollouts only close | one fill-tracked path opens legs, which removed a duplicate-exposure bug |
| Consumer-defined interfaces + pure rule functions | testable without the exchange; rules shared with the backtest |
| Time passed into rule functions | the backtest replays the past correctly (it once silently used the wall clock) |
| Kill switch stays idle instead of exiting | an exit would let Docker restart the bot straight back into trading |
| Hedge is report-only | automated hedging is a product decision, not a refactor |

## Known limitations and roadmap

- The backtest engine runs the shared pure rules in its own day loop rather than the live `Strategy`; order-lifecycle behaviour is covered by end-to-end tests and testnet. **Next:** run `Strategy` itself against the simulated executor.
- Synthetic backtest data only; add a real Deribit dataset and an open-interest collector.
- Multiple strategies per process with per-strategy order labels, versioning and an audit trail.
- Live monitor: phase 1 (read-only, in-session) is in `frontend/`; next are persisted history, change markers and authenticated controls per the dashboard spec.

## Further documentation

| Document | Content |
|---|---|
| [OptionStrategy.md](OptionStrategy.md) | the strategy in plain language: rules, worked examples, risks |
| [docs/architecture.md](docs/architecture.md) | detailed design, invariants, limitations |
| [docs/operations.md](docs/operations.md) | running, monitoring, kill switch, troubleshooting |
| [docs/README.md](docs/README.md) | diagram index (Mermaid sources + 4× PNGs) |
| [CLAUDE.md](CLAUDE.md) | guidance for AI-assisted development in this repo |

---

<sub>Educational study of an automated trading system — not investment advice.</sub>
