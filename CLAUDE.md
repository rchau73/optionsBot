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

All tests live in `tests/` (package `tests`), not next to the source. Notable files: `strategy_run_test.go` (end-to-end `Strategy.Run` against a fake exchange), `gateway_test.go` (real `Gateway` against a mock Deribit WebSocket), `marketdata_test.go`, `gex_manager_test.go`, `orderlog_test.go`, `state_test.go`, `config_test.go`, `backtest_*_test.go`.

## Environment & config

Strict separation — do not mix:
- `.env` — Deribit platform only: credentials, `DERIBIT_ENV` (testnet|live), rate limits (two pools), retry/backoff, circuit breaker, heartbeat/reconnect.
- `config_*.yaml` — all strategy and execution logic. `config.Load` fills defaults and runs `Validate()`; `RequireCredentials()` is called only for trading, so backtests need no API key.

Never reintroduce env-var overrides for logic parameters.

## Architecture

`cmd/bot/main.go` is the composition root: it wires `Gateway → MarketData → GEX Manager → Strategy`, handles `SIGUSR1` (kill switch) and watches `Gateway.Fatal()`. Details: `docs/architecture.md`; diagrams: `docs/README.md`.

- **`internal/gateway`** — the only Deribit connection. `Call` → priority queue (high lane first) → rate limiter (matching-engine pool for buy/sell/edit/cancel, non-matching pool otherwise) → circuit breaker (skipped for high priority) → socket. One writer goroutine never waits for replies; `readLoop` routes replies by request ID. Every request gets exactly one answer (reply, RPC error, timeout, `ErrConnectionLost`, `ErrCircuitOpen`). Retries only for idempotent methods (`public/*`, `private/get_*`) — never for orders. Breaker counts transport failures and exchange-health errors only. Reconnect runs on the root context; giving up is reported via `Fatal()`.
- **`internal/marketdata`** — option chain, subscriptions to the index, DVOL (`deribit_volatility_index.btc_usd`) and tickers of tradable expiries only, chosen with `ExpiryWindow`/`NearestExpiry` — the same rule `strategy.SelectExpiry` uses. Valid ticker intervals are `100ms` and `raw` only. `DVOLTracker` keeps one value per UTC day. Readers get copies.
- **`internal/gex`** — every 60 s computes dealer gamma exposure from open interest, the gamma flip and a flip-based regime with hysteresis.
- **`internal/strategy`** — the decision loop; depends only on the interfaces in `deps.go`.
  - `strategy.go` Run/evaluate/heartbeat · `open.go` entry · `pending.go` fill tracking · `close.go` `buyToClose`, rollouts, GEX closes · `repair.go` · `reconcile.go` · `rebalance.go` · `killswitch.go`.
  - Pure rules shared with the backtest: `entry.go` (`SelectExpiry(…, now, …)`, `SelectStrike`), `rollout.go` (`EvaluateLeg(pos, now, …)`), `gamma.go`, `margin.go`.
- **`internal/orders`** — `Executor` (Deribit calls behind `rpcCaller`; `ErrForbidden`; order `label`), `StateManager` (in-memory book; readers get snapshots; greeks short-signed), decision journal `orders.log` (`journal.go` types: `EventContext`, `MarketSnapshot`, `OrderLog`, `PnLRecord`), price/lot helpers.
- **Journal rule:** every decision (submit, amend, cancel, fill, close, reconcile, skip) is journaled through `s.eventContext`/`s.instrumentContext`, which build the market snapshot (`strategy/snapshot.go`, pure `BuildMarketSnapshot`). Realised P&L is booked per slot in `buyToClose`; `strategy/pnl.go` writes periodic P&L lines. New decision points must journal too.
- **`internal/hedge`** — writes `hedge_report.json` when |net delta| ≥ threshold. **Never places orders.**
- **`internal/backtest`** — CSV feed, `SimExecutor`, day-loop `Engine` using the pure rules at the simulated date, sweep (scenarios applied as slot matrices), walk-forward.

### Trading rules (keep these true)

- **Slots:** each `(DTE, delta)` in `dte_delta_matrix` is one strangle slot; occupied by an open strangle (even one-legged) or a pending entry.
- **Sizing:** budget = equity × `max_margin_pct` × `leverage` − initial margin used (from `private/get_account_summary`); equal share per vacant slot; `private/get_margins` per leg → `ComputeQtyFromIM` whole lots, minimum one lot.
- **Entry:** limit sell at `max(mid, ask)`; premium floor checked for every leg before any order; amend on **ask** drift up to `order_max_adjustments`; after `order_fill_timeout_sec` cancel, read back the final fill and **book filled legs** (one-legged strangles are completed by repair).
- **Exit priority** (`EvaluateLeg`): stop-loss → DTE roll → delta drift → ROI take-profit. Stop-loss = market buy, high priority. Rolls = IOC limit at the ask. `buyToClose` books partial fills and keeps the remainder tracked.
- **Rollouts only close.** Repair reopens a single rolled leg (same expiry, entry delta, size); a strangle with both legs gone frees its slot for a fresh entry. Never add a second reopen path.
- **GEX gating:** trading decisions use `GammaDecision.Action`, never raw `gamma.Trend()`. Entry and repair skip a leg only when the action sheds that leg type.
- **Kill switch:** cancel all orders for the currency → flatten at market with retries on a detached context → stay idle (don't exit; Docker would restart into trading).
- **Startup:** reconcile from the exchange (cancel this currency's orders via `private/cancel_all_by_currency`, load positions, regroup, match slots) → rebalance → entries.
- **Time:** pure functions take `now`; never call `time.Now()` inside them (the backtest replays the past).

## Documentation

- `README.md` technical overview · `OptionStrategy.md` business explanation · `docs/architecture.md` · `docs/operations.md` · `docs/README.md` (diagram index).
- Diagrams are Mermaid `.mmd` in `docs/` rendered to PNG with `make diagrams`; update both when a flow changes.
- The planned dashboard lives in `frontend/` with its own `frontend/README.md`; keep UI/UX material out of the backend docs.
