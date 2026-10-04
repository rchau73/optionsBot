# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build, run, test

```bash
make build                     # go build -o bot ./cmd/bot
make check                     # gofmt + go vet + race tests (what CI runs)
make test                      # go test ./tests/... -race -count=1
make cover                     # coverage across ./internal/... (tests live in ./tests)
make diagrams                  # re-render docs/*.mmd → 4× PNG (needs mmdc)

./bot --config config_btc.yaml                     # trade on testnet (default)
DERIBIT_ENV=live ./bot --config config_btc.yaml    # live — real capital, explicit opt-in
./bot --mode=backtest --config config_btc.yaml --from=2026-01-01 --to=2026-12-31
./bot --mode=backtest --config config_btc.yaml --sweep=true --from=… --to=…
./bot --debug                                      # per-cycle diagnostics
go test ./tests/... -run TestStrategy_ -v          # one group
```

All tests live in `tests/` (package `tests`), not next to the source. Notable files: `strategy_run_test.go` (end-to-end `Strategy.Run` against a fake exchange), `margin_policy_run_test.go` + `risk_test.go` (margin policy end to end and its pure rules), `gateway_test.go` (real `Gateway` against a mock Deribit WebSocket), `marketdata_test.go`, `gex_manager_test.go`, `orderlog_test.go`, `state_test.go`, `config_test.go`, `backtest_*_test.go`.

## Environment & config

Strict separation — do not mix:
- `.env` — Deribit platform only: credentials, `DERIBIT_ENV` (testnet|live), rate limits (two pools), retry/backoff, circuit breaker, heartbeat/reconnect.
- `config_*.yaml` — all strategy and execution logic. `config.Load` fills defaults and runs `Validate()`; `RequireCredentials()` is called only for trading, so backtests need no API key.

Never reintroduce env-var overrides for logic parameters.

## Architecture

`cmd/bot/main.go` is the composition root: it wires `Gateway → MarketData → GEX Manager → Strategy` (plus, on testnet, a public-only mainnet gateway for GEX — see `internal/gex`), handles `SIGUSR1` (kill switch) and watches `Gateway.Fatal()`. Details: `docs/architecture.md`; diagrams: `docs/README.md`.

- **`internal/gateway`** — every Deribit connection goes through it. `PublicOnly()` gateways never authenticate and refuse `private/*` (`ErrPrivateOnPublic`); `WithName` tags their logs (`gateway=trading|mainnet-public`). `Call` → priority queue (high lane first) → rate limiter (matching-engine pool for buy/sell/edit/cancel, non-matching pool otherwise) → circuit breaker (skipped for high priority) → socket. One writer goroutine never waits for replies; `readLoop` routes replies by request ID. Every request gets exactly one answer (reply, RPC error, timeout, `ErrConnectionLost`, `ErrCircuitOpen`). Retries only for idempotent methods (`public/*`, `private/get_*`) — never for orders. Breaker counts transport failures and exchange-health errors only. Callers also hold a hard deadline, so a request that is never sent still times out. Reconnect runs on the root context; a drop during a reconnect re-arms it (never ignored) and a failed subscription restore is a failed attempt; giving up is reported via `Fatal()`. `main` also runs a decision-loop watchdog (`strategy.WatchProgress`): no cycle for max(10 × eval interval, 10 min) → exit for a supervised restart (never when halted by the kill switch).
- **`internal/marketdata`** — option chain, subscriptions to the index, DVOL (`deribit_volatility_index.btc_usd`) and tickers of tradable expiries only, chosen with `ExpiryWindow`/`NearestExpiry` — the same rule `strategy.SelectExpiry` uses. Valid ticker intervals are `100ms` and `raw` only. `DVOLTracker` keeps one value per UTC day. Readers get copies. `Track(ctx, names)` subscribes to the tickers of held instruments outside those expiries (positions loaded at startup); the strategy calls it every cycle. `Instrument.HasQuote()` is false until the first ticker: never use such an instrument's zero prices as a mark.
- **`internal/gex`** — every 60 s computes dealer gamma exposure from **mainnet** open interest (always: testnet prices mirror mainnet but its open interest belongs to test accounts, so testnet GEX describes nothing real). `gex.Build(rows, now, params)` is pure; `gex_method: script` (default) reproduces GestaoCarteira's `deribit_tc_export_v3.py` exactly (5 nearest expiries of the chain, strikes ±`gex_strike_range_pct` of each expiry's future, regime = sign of the weighted sum, flip = lowest crossing, no hysteresis) — `tests/gex_parity_test.go` checks it against the script's own output on captured mainnet data; `nearest_flip` is the original rule (crossing nearest spot, regime = spot vs flip, hysteresis `gamma_regime_band_pct`). Option names are parsed with `marketdata.ParseOptionName` (daily expiries have one-digit days, e.g. `4OCT26`).
- **`internal/strategy`** — the decision loop; depends only on the interfaces in `deps.go`.
  - `strategy.go` Run/evaluate/heartbeat · `limits.go` margin policy (status, MM reduction, simulation sizing) · `open.go` entry · `pending.go` fill tracking · `close.go` `buyToClose`, rollouts, GEX closes · `repair.go` · `reconcile.go` · `rebalance.go` · `killswitch.go`.
  - Pure rules shared with the backtest: `entry.go` (`SelectExpiry(…, now, …)`, `SelectStrike`), `rollout.go` (`EvaluateLeg(pos, now, …)`), `gamma.go`, `margin.go`.
- **`internal/orders`** — `Executor` (Deribit calls behind `rpcCaller`; `ErrForbidden`; order `label`), `StateManager` (in-memory book; readers get snapshots; greeks short-signed), decision journal `orders.log` (`journal.go` types: `EventContext`, `MarketSnapshot`, `OrderLog`, `PnLRecord`), price/lot helpers.
- **Journal rule:** every decision (submit, amend, cancel, fill, close, reconcile, skip) is journaled through `s.eventContext`/`s.instrumentContext`, which build the market snapshot (`strategy/snapshot.go`, pure `BuildMarketSnapshot`). Realised P&L is booked per slot in `buyToClose`; `strategy/pnl.go` writes periodic P&L lines. New decision points must journal too.
- **`internal/api`** — read-only monitor API (`BOT_API_ADDR`): `/api/status|positions|orders|pnl|events`, built from `Strategy.View()` (loop-published snapshots, never calls Deribit) and the journal's in-memory recent events. Never add endpoints that change trading state without auth + confirmation + audit. `cmd/monitor-demo` serves it with simulated data. `/api/account` serves `internal/account` (cached `private/get_account_summaries`, polled every `BOT_ACCOUNT_POLL_SEC`; margin figures are Deribit's, never recomputed). `/api/pnl/history?range=` serves `internal/history` (append-only `data/pnl_history.jsonl`, written by `strategy/pnl.go` each report interval; trading never reads it).
- **`internal/hedge`** — writes `hedge_report.json` when |net delta| ≥ threshold. **Never places orders.**
- **`internal/backtest`** — CSV feed, `SimExecutor`, day-loop `Engine` using the pure rules at the simulated date, sweep (scenarios applied as slot matrices), walk-forward.

### Trading rules (keep these true)

- **Slots:** each `(DTE, delta)` in `dte_delta_matrix` is one strangle slot; occupied by an open strangle (even one-legged) or a pending entry.
- **Margin policy** (`internal/risk`, pure; applied in `strategy/limits.go`): every figure is Deribit's — account-wide USD totals under cross collateral, else the currency's own (`AccountSummary.MarginUsage`). IM limit (% of margin balance) = band of the DVOL IV percentile (`iv_margin_bands`), or the lowest band under a confirmed negative gamma regime. Band/regime changes are confirmed on `iv_band_confirm_days` consecutive UTC daily closes (DVOL closes from `DVOLTracker.Daily`, regime closes from `data/regime_history.jsonl`); until then new risk is frozen (exits, rolls and repairs continue). A confirmed limit change (and startup) runs `rebalancePositions` toward it; MM ≥ `max_mm_pct` reduces positions at market at once and blocks repairs and entries. Missing/stale margin data or a failed simulation → no new risk. Never reintroduce a margin cap computed from equity.
- **Sizing:** each vacant slot gets an equal share of the IM headroom; `private/simulate_portfolio` (≤ 1 call/s, spaced in the executor) prices one lot, then confirms the final size keeps post-trade IM and MM within the limits. The backtest has no simulator: `backtest.ApproxIMLimitPct` (premium as margin, DVOL band only) is a labelled approximation.
- **Entry:** limit sell at `max(mid, ask)`; premium floor checked for every leg before any order; amend on **ask** drift up to `order_max_adjustments`; after `order_fill_timeout_sec` cancel, read back the final fill and **book filled legs** (one-legged strangles are completed by repair).
- **Marks:** `refreshMarks` copies a quote onto a position only when the instrument `HasQuote()`, which sets `Position.MarkLive`; otherwise the last known mark stays (from the exchange at reconcile). Reconcile converts Deribit's size-weighted position greeks to per-option greeks (`PerOptionGreeks`).
- **Exit priority** (`EvaluateLeg`): stop-loss → DTE roll → delta drift → ROI take-profit. Delta drift and take-profit act only on a `MarkLive` position; stop-loss and the DTE roll always act (on the last known mark). Stop-loss = market buy, high priority. Rolls = IOC limit at the ask. `buyToClose` books partial fills and keeps the remainder tracked.
- **Rollouts only close.** Repair reopens a single rolled leg (same expiry, entry delta, size); a strangle with both legs gone frees its slot for a fresh entry. Never add a second reopen path.
- **GEX gating:** trading decisions use `GammaDecision.Action`, never raw `gamma.Trend()`. Entry and repair skip a leg only when the action sheds that leg type.
- **Kill switch:** cancel all orders for the currency → flatten at market with retries on a detached context → stay idle (don't exit; Docker would restart into trading).
- **Startup:** reconcile from the exchange (cancel this currency's orders via `private/cancel_all_by_currency`, load positions, regroup, match slots) → margin policy (rebalance only when the limit rests on confirmed data and nothing is frozen) → entries.
- **Time:** pure functions take `now`; never call `time.Now()` inside them (the backtest replays the past).

## Documentation

- `README.md` technical overview · `OptionStrategy.md` business explanation · `docs/architecture.md` · `docs/operations.md` · `docs/README.md` (diagram index).
- Diagrams are Mermaid `.mmd` in `docs/` rendered to PNG with `make diagrams`; update both when a flow changes.
- The planned dashboard lives in `frontend/` with its own `frontend/README.md`; keep UI/UX material out of the backend docs.
