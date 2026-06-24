# Options Bot — Deribit Short Strangle

A production-grade Go implementation of a short strangle options trading bot on Deribit.
Runs exclusively on **testnet by default**. Live mode requires `DERIBIT_ENV=live`.

---

## Risk Disclaimer

Options trading involves significant financial risk, including total loss of capital.
This software is provided for educational purposes only. The author is not a licensed
financial advisor. Always validate on testnet before risking live capital.

---

## Quick Start

```bash
cp .env.example .env
# Edit .env with your Deribit testnet credentials

go build -o bot ./cmd/bot
./bot --config config.yaml
```

## Running with Docker (recommended)

Docker keeps the bot running as a background server process, isolated from your
terminal session. The `restart: unless-stopped` policy in `docker-compose.yml`
automatically revives the container if it crashes.

### First-time setup

```bash
cp .env.example .env
# Edit .env with your Deribit credentials
```

### Start / stop

```bash
# Start bot in the background (builds image on first run)
docker compose up -d

# Or use the monitor script — starts the bot and checks health every 5 minutes:
./bot.sh

# Stop the bot
docker compose down
# or
./bot.sh --stop
```

### Check if it's alive

```bash
# Container status (Running / Exited / Restarting)
docker compose ps

# Live log stream — includes both bot.log AND orders.log entries (Ctrl+C exits, bot keeps running)
docker compose logs -f bot

# Last 100 lines
docker compose logs --tail=100 bot
```

### Filtering the log stream

Both `bot.log` and `orders.log` are written to stdout, so `docker compose logs`
shows everything in one stream. Filter with `grep` or `jq`:

```bash
# Bot heartbeat only (equity, margin, positions — every 60 s)
docker compose logs -f bot | grep '"msg":"heartbeat"'

# All order events (entries + closes)
docker compose logs -f bot | grep '"order_id"'

# Closed positions only (buy = close for a short)
docker compose logs -f bot | grep '"direction":"buy"'

# Gamma-regime closes with formatted P&L and ROI
docker compose logs -f bot | grep '"close_reason":"gamma_regime"' \
  | jq '{instrument, close_reason, pnl_usd_fmt, roi_pct_fmt, hold_days}'

# Stop-loss events
docker compose logs -f bot | grep '"close_reason":"stop_loss"'

# Pretty-print the last close
docker compose logs bot | grep '"direction":"buy"' | tail -1 | jq .
```

### Connect into the container

```bash
# Open a shell inside the running container
docker compose exec bot sh

# From inside you can also tail the raw log files directly:
#   tail -f bot.log | jq .
#   tail -f orders.log | jq '{instrument, close_reason, pnl_usd_fmt, roi_pct_fmt}'
```

### Rebuild after a code change

```bash
docker compose up -d --build
```

### Monitor script (`bot.sh`)

**Run this on your local machine, not inside the container.**

`bot.sh` goes beyond checking whether the Docker container is running — it
checks whether the **bot process itself is alive** by grepping for heartbeat
log messages (the bot emits one every 60 s). A container can be `running`
in Docker while the bot inside is silently hung due to rate-limit issues or
a Deribit environment problem. In that case a container-status check alone
would report "OK" while the bot is actually dead.

Health rule: if no heartbeat appears in the last 3 minutes → restart the
entire container (SIGTERM → graceful shutdown → fresh start). The bot's own
WebSocket reconnect logic handles transient drops; this script handles the
deeper "bot is stuck" case that the reconnect loop cannot fix.

```bash
./bot.sh                          # start + monitor every 5 min
./bot.sh --once                   # start once and exit (no loop)
./bot.sh --stop                   # stop the container
./bot.sh --service bot-btc        # monitor a specific service (multi-underlying)
caffeinate -i ./bot.sh            # macOS: also prevent sleep
```

Run the monitor in a `tmux` / `screen` session so it survives terminal
disconnects. Ctrl+C exits the monitor but leaves the bot running in Docker.

### macOS sleep caveat

Docker Desktop on macOS **suspends all containers when the Mac sleeps**.
To keep the bot alive through sleep, choose one of:

| Option | Command |
|--------|---------|
| Prevent sleep while monitor runs | `caffeinate -i ./bot.sh` |
| Run permanently on a remote VPS | `ssh user@vps` then `./bot.sh` |
| Enable "Keep running in background" | Docker Desktop → Settings → General → "Start Docker Desktop on login" + disable sleep in macOS System Settings → Battery |

For a production setup, the VPS option is the most reliable.

---

## Running Multiple Underlyings

Each bot instance is a single-underlying process. To trade BTC and ETH options
simultaneously, run two instances — one per config file — as separate Docker
services backed by the same image.

### 1. Create per-underlying config files

```bash
cp config.yaml config_btc.yaml
cp config.yaml config_eth.yaml
```

Edit each file so `underlying` matches the instrument:

```yaml
# config_btc.yaml
underlying: BTC
# ... rest of BTC-specific strategy params
```

```yaml
# config_eth.yaml
underlying: ETH
# ... rest of ETH-specific strategy params (DTE matrix, margins, etc.)
```

### 2. Add a second service to `docker-compose.yml`

```yaml
services:
  bot-btc:
    build: .
    restart: unless-stopped
    volumes:
      - ./.env:/app/.env:ro
      - ./config_btc.yaml:/app/config.yaml:ro
      - ./data/btc:/app/data
    environment:
      - TZ=UTC

  bot-eth:
    build: .
    restart: unless-stopped
    volumes:
      - ./.env:/app/.env:ro
      - ./config_eth.yaml:/app/config.yaml:ro
      - ./data/eth:/app/data
    environment:
      - TZ=UTC
```

Both services share the same `.env` (same Deribit account credentials). The
`data/` volume is split per underlying so log files, position state, and
backtest output don't collide.

### 3. Start / stop

```bash
# Start both
docker compose up -d

# Start only one underlying
docker compose up -d bot-btc

# Stop one without affecting the other
docker compose stop bot-eth

# Rebuild after a code change (applies to both)
docker compose up -d --build
```

### 4. Monitor with `bot.sh`

`bot.sh` accepts `--service <name>` to target a specific bot instance. Run one
monitor per underlying, each in its own `tmux` pane or `screen` window:

```bash
# Monitor BTC bot (start + health-check loop)
./bot.sh --service bot-btc

# Monitor ETH bot in a second pane
./bot.sh --service bot-eth

# Start once without the monitor loop
./bot.sh --once --service bot-btc
./bot.sh --once --service bot-eth

# Stop a specific service
./bot.sh --stop --service bot-btc

# Stop everything
./bot.sh --stop
```

Each log line is prefixed with the service name so you can tell them apart when
tailing both at once:

```
[2026-06-23 12:00:00] [bot-btc] OK — bot is alive (heartbeat seen within 3m)
[2026-06-23 12:00:01] [bot-eth] OK — bot is alive (heartbeat seen within 3m)
```

### 5. Monitor logs per underlying

```bash
# Follow both in the same stream (service name is prefixed on each line)
docker compose logs -f

# Follow only one underlying
docker compose logs -f bot-btc
docker compose logs -f bot-eth

# Heartbeat for each (equity, margin, positions — emitted every 60 s)
docker compose logs -f bot-btc | grep '"msg":"heartbeat"'
docker compose logs -f bot-eth | grep '"msg":"heartbeat"'

# All fills across both underlyings
docker compose logs | grep '"direction":"buy"' | jq '{service, instrument, pnl_usd_fmt}'
```

### Rate-limit note

Both instances share the same Deribit account and its API rate limits, but each
has its own in-process token bucket — they are unaware of each other. Under
normal operating load (a handful of slots per underlying) this is fine. During
simultaneous rollout storms (both bots rolling at the same time), combined
request rates could approach the Deribit limits. Watch for `rate_limit_exceeded`
errors in the logs; reduce `eval_interval_ms` or stagger restarts if they appear.

---

## Modes

| Mode | Command |
|------|---------|
| Live (testnet) | `./bot` |
| Live (mainnet) | `DERIBIT_ENV=live ./bot` |
| Backtest | `./bot --mode=backtest --from=2026-01-01 --to=2026-12-31` |
| Parameter sweep | `./bot --mode=backtest --sweep=true --from=... --to=...` |

### Debug mode

Add `--debug` to any command to enable verbose logging:

```bash
./bot --debug
./bot --mode=backtest --from=2026-01-01 --to=2026-12-31 --debug
```

Without `--debug` the default `INFO` level logs key lifecycle events: startup config,
instruments loaded, strangles opened/closed, margin limit skips, rollout decisions,
and a heartbeat every 60 seconds showing equity, margin used, and open positions.

With `--debug` you additionally get per-tick decision traces:

| Message | What it shows |
|---|---|
| `opening initial strangles` | Equity, IV percentile, margin allowed vs used |
| `entry margin check` | Call/put candidates with delta, mid, and margin numbers before each entry attempt |
| `skip entry: strangle already open` | DTE bucket already filled |
| `skip entry: no suitable expiry` | No expiry found near target DTE |
| `skip entry: call/put strike selection failed` | No OTM strike at target delta |
| `skip entry: margin limit` | Exact margin needed vs used vs allowed |
| `position status` *(heartbeat)* | Per-position DTE, delta, IV, entry price, current mid, unrealised P&L |
| `processing day` *(backtest)* | Date, tick count, open positions, equity |
| `position hold` *(backtest)* | Per-position DTE/delta/ROI each day |

Logs are written to both stdout and `bot.log` in JSON format.

---

## Architecture

```
cmd/bot/main.go           Entry point — wires all components
internal/
  config/                 YAML + env config loader
  logger/                 slog JSON logger (bot.log)
  gateway/                WebSocket, auth, heartbeat, rate limiter,
                          retry, circuit breaker, priority queue,
                          subscription registry
  marketdata/             Real-time feed manager, DVOL/IV percentile tracker
  strategy/               Short strangle entry, rollout rules (4.1–4.5),
                          gamma monitor, margin guard
  orders/                 Order struct, live executor, state manager,
                          structured order log (orders.log)
  hedge/                  Delta hedge report generator (hedge_report.json)
  backtest/               Historical CSV feed, simulated executor,
                          backtest engine, metrics (Sharpe/Sortino/Calmar),
                          scenario sweep, walk-forward validation
```

---

## Strategy Summary

### Entry
- Opens one strangle per `(DTE, delta)` slot defined in `dte_delta_matrix`
- Each slot is independent: `(DTE=15, delta=0.10)`, `(DTE=30, delta=0.16)`, `(DTE=30, delta=0.18)`, etc.
- **Quantity per order** = `budget ÷ total_slots` (equal-sized positions across all slots)
- Deduplication: a slot is skipped if it already has an open or pending strangle at that `(DTE, delta)` key
- **Next-DTE fallback**: if a slot's primary expiry is already occupied at that delta, the bot searches for the next available expiry within the deviation window rather than skipping entirely
- Margin gated by IV percentile (DVOL rolling 252-day window):
  - IV ≥ 70th pct → 35% of equity
  - 30–70th pct → 25% of equity
  - < 30th pct → 15% of equity

### Rollout Rules (priority order)
1. **Stop loss 200%** — market close immediately
2. **`rollout_dte`** — roll entire strangle to next monthly expiry (limit first, market fallback after 1 day)
3. **Delta drift < threshold** — roll affected leg, reopens at the slot's original delta ≥25 DTE
4. **ROI ≥ take-profit** — roll affected leg, reopens at the slot's original delta ≥25 DTE (limit orders only)

### Gamma Monitor (Section 5)
- If net portfolio gamma < 0:
  - Bear trend → close all puts at market
  - Bull trend → close all calls at market
- Re-establishes strangles after gamma normalises

### Kill Switch
Send `SIGUSR1` or set `KILL_SWITCH=1` to flatten all positions at market.

---

## Output Files

| File | Contents |
|------|----------|
| `bot.log` | Structured JSON event log (slog) |
| `orders.log` | Per-order JSON records (see `OrderLog` struct) |
| `hedge_report.json` | Delta hedge report — **never auto-executed** |
| `data/results/summary.json` | Backtest summary metrics |
| `data/results/equity_curve.csv` | Daily equity snapshots |
| `data/results/drawdown.csv` | Daily drawdown |
| `data/results/trades.csv` | One row per closed position |
| `data/results/scenario_comparison.csv` | Sweep results ranked by Sharpe |
| `data/results/walk_forward/` | Train/validate JSON + summary CSV |

---

## Historical Data

The backtest expects a CSV at `data/historical/options.csv` with these columns:

```
date, instrument, underlying, underlying_price, strike, expiry, option_type,
bid, ask, mid, delta, gamma, theta, vega, iv, dvol_index
```

Recommended data sources:
- **Deribit REST**: `GET /public/get_tradingview_chart_data`
- **Tardis.dev** (paid, high quality): https://tardis.dev
- **Deribit dataset on Kaggle**: search "deribit options historical"

---

## Configuration

### Multi-delta strangle matrix (`dte_delta_matrix`)

The primary way to configure entry positions. Each entry pairs a DTE target with one or more deltas:

```yaml
dte_delta_matrix:
  - dte: 15
    deltas: [0.10]           # 1 slot
  - dte: 30
    deltas: [0.16, 0.18]     # 2 slots
  - dte: 45
    deltas: [0.16, 0.18, 0.20]  # 3 slots
# Total: 6 slots → qty per order = budget ÷ 6
```

The bot opens one independent strangle per `(DTE, delta)` slot. If a slot's target expiry already has a position at that delta, the next available expiry within `max_dte_deviation` days is used instead.

**Legacy single-delta config** (still supported for backtest scenario sweeps):
```yaml
target_dte:   [30, 45]
entry_delta:  0.16
```
`target_dte` and `entry_delta` are ignored when `dte_delta_matrix` is set.

See `config.yaml` for all strategy parameters and the `backtest:` block.
See `.env.example` for all rate-limit, retry, circuit-breaker, and heartbeat env vars.

---

## Tests

```bash
go test ./tests/... -v
```

Test coverage:
- `strategy_test.go` — `Slots()` expansion, matrix vs legacy fallback, rollout rule priority
- `ws_mock_test.go` — WebSocket connect, subscribe deduplication, retry on rate-limit
- `ratelimit_test.go` — token bucket rate enforcement, safety factor
- `retry_test.go` — backoff intervals, jitter bounds, context cancellation
- `circuit_test.go` — open/half-open/closed transitions
- `priority_test.go` — high-priority always dispatched before low
- `backtest_feed_test.go` — CSV load, date filter, DVOL percentile
- `backtest_executor_test.go` — market fill, commission, cancel
- `backtest_loop_test.go` — full simulation loop runs without error
- `backtest_metrics_test.go` — Sharpe, Sortino, max drawdown, win rate
