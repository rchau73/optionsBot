# Architecture

A detailed look at how the bot is built: packages, goroutines, data flow and the invariants that keep it safe. The [README](../README.md) has the overview; [diagrams](README.md) are listed in the docs index.

## Contents
1. [Design goals](#1-design-goals)
2. [Package map](#2-package-map)
3. [Concurrency model](#3-concurrency-model)
4. [Gateway](#4-gateway)
5. [Market data](#5-market-data)
6. [Strategy](#6-strategy)
7. [Orders and state](#7-orders-and-state)
8. [GEX regime](#8-gex-regime)
9. [Hedge reporting](#9-hedge-reporting)
10. [Backtest](#10-backtest)
11. [Configuration](#11-configuration)
12. [Observability](#12-observability)
13. [Invariants (do not break)](#13-invariants-do-not-break)
14. [Known limitations](#14-known-limitations)

## 1. Design goals

- **Safety first.** The bot can trade real capital, so every path that places or closes an order is explicit, tested end to end and logged. Testnet is the default.
- **The exchange is the source of truth.** State lives in memory and is rebuilt from Deribit on every start (reconcile), so a crash or restart is always recoverable.
- **Small, consumer-defined interfaces.** Packages depend on the few methods they use, which keeps them testable with fakes and lets the backtest reuse the pure decision functions.
- **Simple over clever.** One process per underlying, one WebSocket, plain goroutines and mutexes, standard library first.

## 2. Package map

| Package | Responsibility | Depends on |
|---|---|---|
| `cmd/bot` | Composition root: flags, logger, config, wiring, `SIGUSR1` → kill switch, live vs backtest | everything below |
| `cmd/gendata` | Synthetic historical CSV for backtests | — |
| `internal/config` | `config.yaml` (strategy) + `.env` (platform), defaults, `Validate`, `RequireCredentials` | — |
| `internal/gateway` | The only Deribit connection: priority queue, rate limiter, circuit breaker, retries, reply routing, reconnect | config |
| `internal/marketdata` | Option chain, ticker/index/DVOL subscriptions, IV percentile, shared expiry-window rule | gateway (interface) |
| `internal/gex` | Market-wide gamma exposure regime from **mainnet** open interest; pure `Build` + 60 s refresher | gateway (interface), marketdata (name parsing) |
| `internal/risk` | Margin policy, pure: IM limit by DVOL IV-percentile band, negative-gamma override, changes confirmed on daily closes, fixed MM limit | — |
| `internal/strategy` | Decision loop: entry, fill tracking, exits, margin policy, repair, reconcile, rebalance, kill switch; pure rule functions | orders, marketdata, gex, risk (interfaces) |
| `internal/orders` | `Executor` (Deribit order/account calls), `StateManager` (in-memory book), order journal | gateway (interface) |
| `internal/hedge` | Writes `hedge_report.json`; never trades | — |
| `internal/history` | P&L history (`data/pnl_history.jsonl`, bucketed range queries) and gamma regime per UTC daily close (`data/regime_history.jsonl`, which Deribit does not keep); append-only, reloaded on start, corrupt lines skipped | — |
| `internal/account` | Polls `private/get_account_summaries` (fallback: per-currency `get_account_summary`) every `BOT_ACCOUNT_POLL_SEC` and caches collateral per asset, margin model and IM/MM — Deribit's figures, plus IM %/MM % of margin balance | gateway (interface) |
| `internal/api` | Read-only monitor API (`BOT_API_ADDR`) from `Strategy.View()` and the journal's recent events; no exchange calls | strategy, orders (interfaces) |
| `internal/backtest` | CSV feed, simulated executor, day-loop engine, metrics, sweep, walk-forward | strategy (pure functions), orders |
| `internal/logger` | `slog` JSON to stdout + `bot.log` | — |

`internal/strategy/deps.go` lists every interface the strategy consumes (`OrderPlacer`, `OrderTracker`, `AccountReader`, `MarketData`, `TradeJournal`, `HedgeReporter`, `GEXSource`) and the `Deps` struct `main` fills in.

## 3. Concurrency model

| Goroutine | Owner / lifetime | Shares |
|---|---|---|
| `gateway.readLoop` | one per connection; stops when the connection context is cancelled | `pending` map (mutex), notification channel |
| `gateway.dispatchLoop` | one per connection — the **only writer** to the socket | write mutex |
| `gateway.heartbeatLoop`, `metricsLoop` | one per connection | — |
| `gateway.reconnect` | at most one at a time (atomic flag); drops during it re-arm it (`reconnectWanted`); runs on the root context | — |
| watchdog (`main`) | one; reads `Strategy.Progress()` every 30 s | root context |
| `marketdata.processNotifications` | one, until the root context ends | instrument map (RWMutex) |
| `gex` background refresh | one, every 60 s | published snapshot (immutable, RWMutex) |
| `strategy.Run` | one — **every trading decision runs here**, so decisions never race | book (StateManager), pending map |
| `strategy.heartbeat` | one, read-only logging and P&L lines every `report_interval_sec` | reads snapshots only |
| monitor API handlers | `net/http`, one per request | `View()`: snapshots + state the loop publishes after each cycle |
| `main` signal watcher | one; `SIGUSR1` → `KillSwitch()` | — |

Rules: every goroutine stops on a context; no lock is held across a network call; readers of shared state get copies (`StateManager` and `marketdata` return snapshots). The suite runs under `-race`.

## 4. Gateway

See [seq_gateway](seq_gateway.png), [rate limiter](gateway_ratelimiter.png), [circuit breaker](gateway_circuitbreaker.png).

- **Call path:** `Call(ctx, method, params, priority)` → priority queue (high lane drained first) → rate limiter (matching-engine pool for buy/sell/edit/cancel, non-matching pool for everything else) → circuit breaker (skipped for high priority) → write. The writer never waits for the reply; `readLoop` routes each reply to its caller by request ID.
- **Every request is answered exactly once:** reply, RPC error, timeout (30 s from the `Call`, queue time included), `ErrConnectionLost`, or `ErrCircuitOpen`. Requests that expire in the queue are dropped, never sent late. The caller also holds its own deadline, so even a request that is never sent (no connection, no dispatcher) times out instead of waiting forever.
- **Circuit breaker** counts transport failures, timeouts and exchange-health errors (10028, 10040, 10041, 11051, 13888). Business rejections (bad price, no funds) prove the exchange is up and reset it.
- **Retries** (full-jitter exponential backoff) only for idempotent reads (`public/*`, `private/get_*`); orders are never retried, because a "failed" order may already be on the book.
- **Reconnect** runs on the root context: backoff, dial, auth, restore subscriptions. Only one runs at a time, but a drop during a reconnect is never lost (`reconnectWanted`): the running reconnect goes round again. A failed subscription restore counts as a failed attempt, and channels still owed from an interrupted restore are carried over. When it gives up, `Fatal()` tells `main` to shut down so the supervisor restarts the process and reconcile rebuilds state.
- **Decision-loop watchdog** (`strategy.WatchProgress`, in `main`): if no decision cycle completes for max(10 × `eval_interval_ms`, 10 min), the bot logs `decision loop stalled` and exits non-zero for a supervised restart — a stalled loop checks no stop-loss. A halted bot (kill switch) is idle on purpose and never trips it.
- **Heartbeats:** Deribit `test_request`s are answered directly on the socket, bypassing the queue.

## 5. Market data

See [marketdata_flow](marketdata_flow.png).

- Loads the option chain once, seeds a year of daily DVOL, and subscribes to the index price, DVOL and the tickers of **tradable** expiries only: per slot, `NearestExpiry` in `ExpiryWindow(target, deviation, rollout_dte)` — the exact rule the strategy uses — plus the next three expiries for rollouts.
- Ticker pushes update bid/ask/mid (mark price when there are no quotes, common on testnet), greeks and IV. The spot comes from `deribit_price_index`, not the per-option underlying price.
- `DVOLTracker` keeps **one value per UTC day**; the IV percentile ranks today against the configured number of days.

## 6. Strategy

See [event loop](strategy_eventloop.png), [startup](seq_startup.png), [entry](seq_entry.png), [exit](seq_exit.png), [rollout rules](strategy_rollout.png), [position states](strategy_position_states.png), [kill switch](seq_kill_switch.png).

**Startup:** seed price history → log account → **reconcile** (cancel this currency's stale orders, load open shorts, regroup into strangles by expiry, match each to its slot) → wait for the index price → **rebalance** to the current budget → fill vacant slots.

**Each cycle (`eval_interval_ms`):** refresh marks → evaluate the GEX regime → poll pending orders → GEX sheds at-risk legs → rollout rules per position → repair one-legged strangles → hedge report → open vacant slots.

| File | What it owns |
|---|---|
| `open.go` | slot occupancy, expiry choice (with fallback), strike choice, PM-aware sizing, premium floor, GEX leg gate, submitting entry legs |
| `pending.go` | fill tracking: partial fills, amend on ask drift, timeout → cancel + read back final fill → book filled legs |
| `close.go` | `buyToClose` (market, or IOC limit at the ask) with partial-fill handling; stop-loss, rollouts, GEX closes |
| `repair.go` | reopen a missing leg at the strangle's expiry, entry delta and size; GEX-gated; skipped inside the rollout window |
| `reconcile.go` | rebuild the book from the exchange; startup account log |
| `limits.go` | margin policy each cycle: evaluate `internal/risk`, journal changes, reduce at market on an MM breach, size entries with `private/simulate_portfolio` |
| `rebalance.go` | resize strangles toward a newly confirmed IM limit (downsize at market only while IM is above it, upsize via a complement entry) |
| `killswitch.go` | cancel all → flatten at market with retries → stay idle |
| `entry.go`, `rollout.go`, `gamma.go`, `margin.go` | pure decision functions shared with the backtest |

**Marks.** Every cycle the strategy subscribes to the ticker of any held instrument that has none (`marketdata.Track`: positions loaded at startup can sit in expiries the bot would not open today), then copies live quotes onto positions. An instrument that has never had a quote is skipped, so the position keeps its last known mark (Deribit's, from reconcile) instead of a zero, and is flagged not live until a quote arrives.

**Rollout priority** (`EvaluateLeg`, pure): stop-loss → DTE roll → delta drift → ROI take-profit. Delta drift and take-profit need a live mark; stop-loss and the DTE roll act on the last known one, so a missing quote can neither trigger a close nor blind the stop-loss. Rollouts only *close*; replacement legs are opened by repair (single leg) or entry (whole strangle), so there is exactly one fill-tracked way to open a leg.

**Margin policy and sizing.** All margin figures are Deribit's: the account-wide USD totals when cross collateral is on, otherwise the currency's own (`AccountSummary.MarginUsage`, used for both the live summary and simulations so they compare).

| Rule | Behaviour |
|---|---|
| IM limit | `iv_margin_bands` by the confirmed DVOL IV-percentile band (default ≥70 → 50 %, ≥30 → 35 %, else 20 % of margin balance) |
| Negative gamma | a confirmed negative GEX regime forces the lowest band |
| Confirmation | a band or regime change must hold for `iv_band_confirm_days` (2) consecutive UTC daily closes; a missing day resets the count. Until then: **frozen**, no new entries or upsizes; exits, rolls and repairs continue. A change that reverts before confirmation unfreezes with nothing else changed |
| Rebalance | on a confirmed limit change (and at startup): buy back whole lots above each strangle's share while IM is above the limit, at least one lot kept; open complements up to the headroom when below |
| MM limit | MM ≥ `max_mm_pct` (35 %): reduce every position at market each cycle until under, regardless of any freeze; repairs and entries blocked |
| Fail safe | margin unknown, simulation failed or units disagree → no new risk; unknown DVOL → lowest band, no rebalance; no confirmed regime yet (first days after deploying) → frozen |

Each vacant slot gets its **slot share** of the limit (limit × margin balance ÷ all slots — the same target the rebalance uses), capped by an equal part of the remaining headroom (limit × margin balance − IM in use, ÷ vacant slots). A single vacant slot therefore never takes all the headroom. `private/simulate_portfolio` — Deribit allows one call per second, so the executor spaces them — prices one strangle lot added to the real portfolio (legs offsetting), the size is scaled to the share, and a second simulation confirms post-trade IM and MM fit; otherwise the size shrinks or the slot is skipped (`margin_limit`). Every freeze, unfreeze, limit change, rebalance and MM breach is journaled as a `risk_limit` event.

## 7. Orders and state

- `Executor` wraps every Deribit order/account method behind a small `rpcCaller` interface; one generic `call[T]` decodes results; `forbidden` errors become `orders.ErrForbidden`; risk-reducing orders go through the high-priority lane.
- `StateManager` is the in-memory book (positions + strangles, strangle legs linked by ID). Readers get snapshots; all changes go through methods. Portfolio greeks are short-signed (Deribit greeks are long-perspective).
- The order journal (`orders.log`) is the decision record: one JSON line per submit, amend, cancel, fill, close, reconcile and skipped entry, each tagged with `strategy_id` and slot and carrying a **market snapshot** built by the pure `strategy.BuildMarketSnapshot`: spot, DVOL, IV percentile, option IV, ATM IV and skew, ITM/ATM/OTM with distance to strike (in % and in standard deviations), bid/ask/spread, intrinsic and extrinsic value in coin, open interest of the instrument, strike and expiry with the strike's rank and the expiry's max-pain strike (from the GEX manager's 60 s book-summary poll — no extra API calls), and GEX regime/flip. Closes add P&L in coin (`pnl`) and USD at the close-time spot (`pnl_usd`). Skips are written once per reason until it changes.
- **P&L:** realised P&L is booked per slot on every close; every `report_interval_sec` the journal gets one `pnl` line per slot plus the strategy total (realised since start + unrealised marked to mid, coin and USD). `Strategy.PnLReport()` exposes the same numbers for the future API. Orders carry a Deribit `label` (`<strategy_id>:<dte>d:<delta>`) so exchange-side fills can be attributed too.

See the [data model](data_model.png).

## 8. GEX regime

See [seq_gex](seq_gex.png). **Data source:** open interest always comes from **mainnet**. Testnet mirrors mainnet's prices (index, DVOL, option marks and IVs match), but its open interest belongs to test accounts — 4–5× mainnet's on a typical day — and its "dealers" hedge nothing in the real market, so a testnet GEX says nothing about the moves the bot's positions face. On testnet the bot therefore opens a second, **public-only** gateway to mainnet: it never authenticates, refuses every `private/*` method, and has its own rate limiter; orders, positions and margin stay on the trading gateway. On live both are the same connection.

Every 60 s the GEX manager pulls `public/get_book_summary_by_currency` (open interest, mark IV, each expiry's future price), reads strike/expiry/type from the instrument names, computes Black-Scholes gamma × OI × spot² per strike over the nearest five expiries, weights each expiry by its open-interest share × a weekday/month-end weight, consolidates them and finds the **gamma flip**. `gex_method` picks the rules:

| `gex_method` | Expiries | Strike window centre | Flip | Regime | Hysteresis |
|---|---|---|---|---|---|
| `script` (default) | 5 nearest of the chain | each expiry's own future | lowest zero crossing | sign of the summed weighted GEX | none |
| `nearest_flip` | 5 nearest with OI and IV | latest future price | crossing nearest spot | spot vs flip | `gamma_regime_band_pct` |

`script` reproduces GestaoCarteira's `deribit_tc_export_v3.py`; `tests/gex_parity_test.go` feeds captured mainnet data to `gex.Build` and requires the script's own flip, regime, score and strikes (computed by its functions on the same data with the clock frozen). `GammaMonitor` combines the regime with a trend from daily closes (swing pivots + SMA9/21). Trading uses only `GammaDecision.Action`: shed puts in a confirmed negative regime with a bear trend, shed calls with a bull trend, otherwise trade both legs. Entry and repair apply the same gate.

## 9. Hedge reporting

See [hedge_flow](hedge_flow.png). When |net delta| ≥ `hedge_report_threshold` and has moved by at least that much since the last report, `hedge_report.json` is rewritten with the side (buy/sell perpetual), size and five staged tranches. It never places orders.

## 10. Backtest

See [backtest_flow](backtest_flow.png) and [SimExecutor](backtest_simexec.png).

`HistoricalFeed` replays `data/historical/options.csv` (prices in USD in the synthetic data from `cmd/gendata`) grouped by day; `Engine` runs the pure strategy functions (`EvaluateLeg`, `SelectExpiry`, `SelectStrike`, `GammaMonitor`) **at the simulated date**; margin is an approximation (`ApproxIMLimitPct`: premium as margin, capped by the DVOL band's IM limit, no confirmation or gamma rule) because Deribit's simulator is not available offline; `SimExecutor` fills market orders with slippage and limits per the configured rule; results are written to `data/results/`. `--sweep` runs five scenarios in parallel (each applied as a slot matrix); walk-forward splits the period into train/validate windows and flags > 30 % Sharpe degradation as overfit.

## 11. Configuration

`config.yaml` holds strategy logic, `.env` holds platform settings (credentials, `DERIBIT_ENV`, rate limits, retry, circuit breaker, heartbeat/reconnect) — never mixed. `config.Load` fills defaults and runs `Validate`: underlying set, at least one slot, deltas in (0, 0.5), every slot above `rollout_dte`, margin bands covering 0–100 with limits in (0, 100], `max_mm_pct` below 100 and the removed `leverage` / `max_margin_pct` keys rejected, positive stop-loss, `DERIBIT_ENV` ∈ {testnet, live}. Credentials are checked only for trading, so backtests need no API key. The full table is in the README.

## 12. Observability

- `bot.log`: `slog` JSON with key/value context (instrument, slot, order ID, reason). Levels: `Error` needs attention, `Warn` degraded but handled, `Info` lifecycle and trades, `Debug` per-cycle detail (`--debug`).
- `orders.log`: the audit trail of every order event.
- Every 60 s: `heartbeat` (equity, margin, positions) and `rate_limit_metrics` (tokens, breaker state, retries, in-flight requests, dropped notifications).

## 13. Invariants (do not break)

1. All exchange I/O goes through `gateway.Gateway`.
2. Trading decisions use `GammaDecision.Action`, never raw trend; entry and repair gate legs identically.
3. Every short leg the exchange holds is tracked: partial fills are booked, timeouts keep filled legs, failed closes keep the position.
4. Rollouts only close; opening a leg has one path.
5. Stop-loss and kill-switch orders use the high-priority lane and are never blocked by the circuit breaker.
6. Orders are never retried automatically.
7. Testnet is the default; live requires `DERIBIT_ENV=live`.
8. `hedge` never places orders.
9. Pure decision functions take time as a parameter (`now`), so the backtest replays the past correctly.

## 14. Known limitations

- **Backtest ≠ live loop.** The backtest engine re-implements the day loop around the shared pure functions instead of running `strategy.Strategy` itself, so order-lifecycle behaviour (pending fills, amends, partial fills) is only tested live/testnet and in the end-to-end tests. Running the real strategy against `SimExecutor` is the planned next step.
- **Synthetic data.** `cmd/gendata` produces Black-Scholes prices in USD with monthly expiries only; results on it validate mechanics, not profitability.
- **In-memory state.** Pending orders are not persisted; a restart cancels them and reconciles positions. The only persisted state is the P&L history for the monitor chart (`data/pnl_history.jsonl`); trading never reads it.
- **One account, two processes.** BTC and ETH bots share the account's rate limit without coordinating.
