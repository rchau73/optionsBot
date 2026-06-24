# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
# Build
go build -o bot ./cmd/bot

# Run (testnet by default)
./bot --config config.yaml

# Run live (real capital)
DERIBIT_ENV=live ./bot

# Backtest
./bot --mode=backtest --from=2026-01-01 --to=2026-12-31

# Parameter sweep
./bot --mode=backtest --sweep=true --from=2026-01-01 --to=2026-12-31

# Debug (verbose per-tick logging to stdout + bot.log)
./bot --debug
```

## Tests

```bash
# All tests
go test ./tests/... -v

# Single test
go test ./tests/... -v -run TestCircuitBreaker
```

All tests live under `tests/` (not alongside the source files). There are no unit tests colocated with `internal/`.

Test files and what they cover:
- `strategy_test.go` — slot expansion, rollout priority, GEX/repair/entry guards, premium floor, resolveQty uniformity
- `entry_test.go` — `SelectExpiry` and `SelectExpiryFallback` rolloutDTE lower bound enforcement
- `gex_test.go` — BSGamma, ExpiryWeight, FindGammaFlip, BuildSnapshot (flip-based regime)
- `circuit_test.go`, `ratelimit_test.go`, `retry_test.go`, `priority_test.go` — gateway infrastructure
- `backtest_*.go` — feed, executor, loop, metrics

## Diagrams

Source files are `.mmd` (Mermaid) in `docs/`. Re-render all PNGs at high resolution:

```bash
cd docs && for f in *.mmd; do mmdc -i "$f" -o "${f%.mmd}.png" -s 3 -w 1600; done
```

## Environment & Config

Strict separation — **do not mix**:
- `.env` — Deribit platform only: credentials, rate limits, retry/backoff, circuit breaker, heartbeat/reconnect
- `config.yaml` — all strategy and execution logic: DTE matrix, rollout rules, order sizing, gamma monitor params

The old pattern of env var overrides for logic params (`DERIBIT_EVAL_INTERVAL_MS` etc.) has been removed. Config.yaml is the sole authority for logic params; `.env` is the sole authority for platform params. `DERIBIT_ENV` (testnet/live) lives in `.env`.

## Architecture

### Component wiring (`cmd/bot/main.go`)

```
Gateway → MarketData → Strategy
               ↑           ↑
             GEX Manager ──┘
```

1. `gateway.Gateway` — single WebSocket connection to Deribit. All API calls go through it. Embeds a token-bucket `RateLimiter` (4 scopes: nonMatch, match, orderOps, rest), `CircuitBreaker`, `RetryHandler`, and a `PriorityQueue` (high for stop-loss/kill, low for market data).
2. `marketdata.Manager` — subscribes to `ticker.<instrument>.100ms` channels for only the relevant expiries (DTE slots ± `max_dte_deviation`, plus 3 extra for rollout). Also subscribes to `deribit_price_index.<underlying>_usd` for a reliable spot price independent of options market activity. Uses `mark_price` as fallback for `Mid` when `best_bid = best_ask = 0` (e.g. testnet).
3. `strategy.Strategy` — runs a ticker loop every `eval_interval_ms`. On each tick: calls `EvaluateLeg` per open position (rollout rules), evaluates the `GammaMonitor`, and calls `maybeOpenStrangles` to fill vacant slots.
4. `gex.Manager` — background goroutine polling `public/get_book_summary_by_currency` every 60s. Computes market-wide GEX regime across the nearest 5 expiries. Regime is classified by `spot vs GammaFlip` (not raw score sum), because the aggregate score is structurally biased negative by put-heavy open interest across the full chain.
5. `orders.Executor` — submits limit/market orders via gateway; also exposes `GetMargins` (`private/get_margins`) for PM margin introspection.
6. `orders.StateManager` — in-memory position/strangle state.
7. `hedge.Reporter` — generates `hedge_report.json` when net delta exceeds threshold. Never auto-executes.

### Strategy slots (`dte_delta_matrix`)

Each `(DTE, delta)` pair is an independent strangle slot. Every slot uses the exchange's actual instrument minimum (`max(call.MinTradeAmount, put.MinTradeAmount)`) as the fixed position size — no scaling from margin budget, which caused unbalanced strangles. The slot key is `(DTE, delta)`; a slot is skipped if already open or pending.

**Expiry selection constraint**: `SelectExpiry` enforces `DTE > rollout_dte` as a hard lower bound, overriding the deviation window. Opening a position at or below `rollout_dte` would trigger an immediate rollout on the next cycle.

### Entry order lifecycle

1. **Pre-flight checks** (in `openStrangle`):
   - Exchange qty quantization: `max(call.MinTradeAmount, put.MinTradeAmount)`
   - GEX action gate: only skip a leg when `GammaDecision.Action == GammaActionClosePuts/Calls` (not raw trend)
   - Premium floor: both legs must have `max(mid, ask) >= min_premium_btc`
2. **Submit**: limit sell at `max(mid, ask)` — uses ask when bid=0 to avoid `price_too_low` rejection
3. **Amend** (every eval cycle, up to `order_max_adjustments`): tracks **ask** drift for sell orders (not bid — wide bid/ask spreads produce spurious drift when bid≈0)
4. **Timeout** (`order_fill_timeout_sec = 90s`): cancel and resubmit at fresh market price on the next cycle

### Rollout priority (highest → lowest)

1. **Stop-loss** (`stop_loss_multiplier × premium`) — market close immediately
2. **DTE expiry** (`rollout_dte`) — roll whole strangle to next monthly expiry
3. **Delta drift** (`delta_drift_threshold`) — roll only the drifted leg, reopen at `old.Qty`
4. **ROI take-profit** (`roi_take_profit`) — roll only that leg, limit orders only

Rollout reopens always use `old.Qty` (original position size), never budget-derived sizing.

### GEX regime and trading guards

The regime decision flows through a single `GammaDecision` struct:

```
gex.BuildSnapshot → snap.Regime (flip-based) → GammaMonitor.Evaluate() → GammaDecision.Action
```

Three places use `GammaDecision.Action` to gate leg decisions — all three must be consistent:
- **Entry** (`openStrangle`): skip a leg only when `Action == GammaActionClosePuts/Calls`
- **Repair** (`repairIncompleteStrangles`): same condition
- **Rollout reopen**: not gated (rollouts execute regardless of GEX state)

Raw `s.gamma.Trend()` is **never** used for trading decisions — only `GammaDecision.Action`. The raw trend check was the source of the "repair skipping put leg" and "single-leg entry" bugs.

### Portfolio Margin (PM) margin tracking

The bot runs under a Segregated Portfolio Margin account. `TotalMarginUsed()` (sum of BTC qty) is not a valid proxy for PM — actual margin per position is tiny (~0.28% per strangle). Two correct sources:
- **`fetchMarginState()`** — calls `private/get_account_summary` to get `InitialMargin` (real PM margin used) and `AvailableFunds` as equity. Used in `openStrangles` and `maybeOpenStrangles`.
- **`GetMargins()`** on Executor — wraps `private/get_margins` for per-order margin introspection. The margin budget is a safety gate (`budget ≤ 0 → skip`), not a sizing driver.

### GammaMonitor / swing levels

`GammaMonitor` maintains `dailyCloses` (one entry per completed UTC day, committed at midnight in `PushPrice`). Swing high/low are confirmed pivots requiring `swingPivotN` strictly lower/higher days on each side — always at least `swingPivotN+1` days old. Trend detection uses swing levels and SMA9/SMA21 to confirm direction; negative GEX + confirmed trend → shed the at-risk leg.

### Ticker subscription constraints

- Valid Deribit ticker intervals: `100ms` and `raw` only. `1000ms` is **not** a valid channel name — subscriptions are silently accepted but never deliver data.
- The `deribit_price_index.<underlying>_usd` channel must be subscribed alongside instrument tickers to ensure `UnderlyingPrice` is populated even when options have no quotes (testnet).
- On startup, `waitForTickerData()` blocks until `UnderlyingPrice > 0` before the first open attempt (up to 30s, then logs a warning and proceeds).

### Backtest

`backtest.HistoricalFeed` replays `data/historical/options.csv`. `SimExecutor` fills market orders immediately and limit orders at the next tick. `Engine.Run` drives a day-by-day loop. `RunScenarioSweep` iterates a parameter grid ranked by Sharpe; `RunWalkForward` splits into 4 train/validate folds.

### Kill switch

Send `SIGUSR1` or set `KILL_SWITCH=1` to flatten all positions at market price immediately.
