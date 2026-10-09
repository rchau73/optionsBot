# Operations guide

How to run, watch and stop the bot. For architecture and design see the [main README](../README.md); for the trading logic in plain language see [OptionStrategy.md](../OptionStrategy.md).

> Testnet is the default. Live trading only happens with `DERIBIT_ENV=live` in `.env` — an explicit opt-in.

## Contents
- [Run locally](#run-locally)
- [Run with Docker](#run-with-docker)
- [Live monitor](#live-monitor)
- [Watch the logs](#watch-the-logs)
- [Health monitor (`bot.sh`)](#health-monitor-botsh)
- [Kill switch](#kill-switch)
- [BTC and ETH side by side](#btc-and-eth-side-by-side)
- [macOS sleep caveat](#macos-sleep-caveat)
- [Troubleshooting](#troubleshooting)

## Run locally

```bash
cp .env.example .env          # add your Deribit testnet API key (account:read, trade:read_write)
make build                    # → ./bot
./bot --config config_btc.yaml          # testnet
./bot --config config_btc.yaml --debug  # per-cycle diagnostics
```

## Run with Docker

`docker-compose.yml` defines one service per underlying (`bot-btc`, `bot-eth`) plus the monitor. Each bot mounts its own config as `/app/config.yaml` and **runs inside its own host folder** (`data/btc/`, `data/eth/`, mounted at `/app/run`), so everything it keeps persists across rebuilds and the two never share a file:

```
data/btc/
  bot.log             structured log
  orders.log          decision journal (every order, fill, close, skip, P&L, margin-policy change)
  hedge_report.json   delta-hedge suggestion, when written
  data/pnl_history.jsonl     P&L history for the monitor chart
  data/regime_history.jsonl  gamma regime per daily close (margin policy)
```

`restart: unless-stopped` revives a crashed container; the bot reconciles its positions with Deribit on every start, so a restart is always safe. Run bots **either** with Compose **or** as local processes, never both for the same underlying: two bots would manage the same positions. Running two local bots needs one working folder each (or the same files collide) and `BOT_API_ADDR=127.0.0.1:8082` for the second — variables set in the environment override `.env`.

```bash
docker compose up -d                 # start both (builds the image on first run)
docker compose up -d bot-btc         # start one
docker compose ps                    # Running / Exited / Restarting
docker compose stop bot-eth          # stop one
docker compose down                  # stop everything
docker compose up -d --build         # rebuild after a code change
docker compose exec bot-btc sh       # shell inside the container (runs as a non-root user)
```

## Live monitor

`docker compose up -d` also starts the **monitor** at <http://localhost:3000> (this machine only): open positions per strategy and slot with strikes, DTE and Greeks, working orders, a live activity feed with reasons and market context, and P&L — refreshed every second. It is read-only. The P&L chart's longer ranges (1h to all) read each bot's P&L history (`data/<underlying>/data/pnl_history.jsonl`), which survives restarts and rebuilds; delete it to start the history over. Details and local/demo runs: [frontend/README.md](../frontend/README.md).

## Watch the logs

**Market history.** Each bot records the option market every `market_record_minutes` (60) to `data/<coin>/data/market/YYYY-MM-DD.csv.gz` — one gzip CSV per UTC day, one block appended per snapshot (`zcat` reads it whole). Columns: `date` (snapshot time, RFC 3339 UTC), `instrument, underlying, underlying_price` (the expiry's forward), `strike, expiry, option_type, bid, ask, mid, mark` (coin), `delta, gamma, theta, vega` (Black–Scholes from the mark IV), `iv, open_interest, dvol_index`. The quotes are Deribit **mainnet** (the GEX poll's book summary), even on testnet, because testnet books are thin. About 0.5 MB a day per coin. `--reset-history` does not touch it. Log lines: `market snapshot recorded` / `market snapshot not recorded`.

**Activity feed.** The bot keeps its last 500 decisions in memory for the monitor (`/api/events`); periodic `pnl` lines are counted but not kept there, so they never push decisions out — the feed shows the latest orders, fills, closes, skips and margin changes however long ago they happened, also right after a restart.

**History across restarts.** Each bot replays its journal (`orders.log`) at startup, so realized P&L, close and order counts, the activity feed and the trade history continue where they stopped (`history restored from the journal` in the log gives the totals). To start a fresh history — e.g. after changing the strategy — archive it; nothing is deleted:

```bash
docker compose stop bot-btc
docker compose run --rm --no-deps bot-btc --config /app/config.yaml --env-file /app/.env --reset-history
docker compose start bot-btc
```

The journal and `data/pnl_history.jsonl` move to `data/btc/data/archive/<UTC time>/`; move them back to restore that history.

`bot.log` (structured `slog` JSON) and `orders.log` (the decision journal) are both mirrored to stdout, so `docker compose logs` shows one stream; the files themselves are in `data/<underlying>/`. Each `orders.log` line has an `event` (`submitted`, `amended`, `cancelled`, `filled`, `closed`, `reconciled`, `skipped`, `pnl`), the `strategy_id` and `slot`, a `market` object with the conditions at that moment and a `portfolio` object with the book's Greeks. Closes also carry `detail`: the rule that fired, with its numbers — e.g. `stop-loss: loss 2.31× the premium ≥ 2.00×`, `delta exit: |Δ| 0.312 ≥ 0.30`, `take-profit: 55% of the premium earned ≥ 50%`, `time roll: 15 days to expiry ≤ 15`, `leg balance: call … 262 vs put … 162 — buying back the 100 excess … (not a stop)`, `GEX shed: …`, `MM limit: …`, `downsize: …` — and the option's greeks at that moment (`delta`, `gamma`, `theta`, `vega`, per contract as Deribit quotes them). Fills carry `fee` (what Deribit charged, coin, from the order's trades); closes carry `fees` (the opening share + the closing fee) and a **net** `pnl` = premium − buy-back − fees. Every P&L the bot books, journals, serves or charts is net; closes journaled before 2026-10-06 are gross. Sells carry a `trigger_reason` saying why they were sent: `entry` (a vacant slot), `repair` (re-selling the missing leg of a one-legged strangle) or `rebalance_upsize` (a complement bringing a slot up to the IM limit), and `close_long` for a long found on a traded instrument (sold at the bid; the strategy only holds shorts). A `churn_paused` skip means the churn breaker paused a slot; ERROR lines `buy back capped`, `long position on a traded instrument` and `churn breaker` mean the book and the exchange disagreed — check `position drift` lines around them.

```bash
docker compose logs -f bot-btc                                    # everything, one underlying
docker compose logs -f                                            # both, prefixed by service
docker compose logs -f bot-btc | grep '"msg":"heartbeat"'         # equity, margin, positions every 60 s
docker compose logs -f bot-btc | grep '"order_id"'                # all order events
docker compose logs -f bot-btc | grep '"close_reason":"stop_loss"'
docker compose logs bot-btc | grep '"close_reason"' \
  | jq '{instrument, close_reason, pnl, pnl_usd_fmt, roi_pct_fmt, hold_days}'

# P&L per strategy slot and total (every report_interval_sec)
grep '"event":"pnl"' orders.log | jq '{slot, realised, unrealised, total_usd, open_legs}'

# Market conditions at each entry: DVOL, moneyness, open interest
grep '"event":"filled"' orders.log \
  | jq '{instrument, slot, dvol: .market.dvol, iv_pct: .market.iv_percentile,
         moneyness: .market.moneyness, dist_pct: .market.distance_to_strike_pct,
         strike_oi: .market.strike_oi, oi_rank: .market.strike_oi_rank, gex: .market.gex_regime}'

# Why slots were not entered
grep '"event":"skipped"' orders.log | jq '{slot, skip_reason, dvol: .market.dvol}'
docker compose logs -f bot-btc | grep '"msg":"rate_limit_metrics"' # gateway health every 60 s
```

Useful events to know: `strangle filled and active`, `rollout triggered`, `stop loss triggered`, `partial close: remainder stays open and tracked`, `gex_regime_trigger`, `reconnecting` / `reconnected successfully`, `gateway giving up`, `hedge_report`.

## Health monitor (`bot.sh`)

Run on the host, not inside the container. A container can be "running" while the bot is stuck; `bot.sh` checks for a `heartbeat` log line in the last 3 minutes and restarts the container if there is none.

```bash
./bot.sh                        # start bot-btc + check every 5 min
./bot.sh --service bot-btc      # one service
./bot.sh --once                 # start once, no loop
./bot.sh --stop                 # stop
caffeinate -i ./bot.sh          # macOS: prevent sleep while monitoring
```

Run it in `tmux`/`screen` so it survives terminal disconnects.

## Kill switch

Flattens every position at market and halts trading:

```bash
docker kill -s USR1 <container>     # Docker
kill -USR1 <pid>                    # local process
```

What happens: resting orders for the underlying are cancelled, whatever a working entry filled before the cancel is booked, the book is matched to the exchange's short positions, every position is bought back at market (partial fills retried), anything still open is logged as an `ERROR` for manual handling, and the bot then **stays idle** — it does not exit, so Docker's restart policy cannot put it straight back into the market. To resume trading, restart the container deliberately (`docker compose restart bot-btc`).

## BTC and ETH side by side

Both services share one Deribit account (same `.env`) but each process has its own rate limiter, unaware of the other. At normal load this is fine; if both roll many legs at once, combined requests can approach the account limit. Watch `rate_limit_metrics` and any `too_many_requests` errors; if they appear, **increase** `eval_interval_ms` or stagger restarts. Startup clean-up cancels only the bot's own currency (`private/cancel_all_by_currency`), so restarting one never touches the other's orders.

## macOS sleep caveat

Docker Desktop suspends containers while the Mac sleeps. Use `caffeinate -i ./bot.sh`, disable sleep, or — most reliable — run on a small always-on VPS.

## Troubleshooting

| Symptom | Likely cause | What to do |
|---|---|---|
| `auth failed: no access_token` | wrong key/secret or missing scopes | check `.env`; key needs `account:read` and `trade:read_write` |
| `invalid config: …` at startup | a value failed validation | the message names the field; see the configuration table in the README |
| `forbidden` then a 60 s pause | API key scope revoked | fix the key; the bot backs off instead of spamming |
| `circuit breaker opened` | exchange overload/maintenance responses | wait; stop-losses still go through (high priority bypasses the breaker) |
| `gateway giving up` then exit code 1 | reconnect attempts exhausted | the supervisor restarts it and reconcile restores state |
| `decision loop stalled` then exit code 1 | no decision cycle for 10+ minutes (stop-losses were not being checked) | the supervisor restarts it; report it — something blocked the loop |
| monitor shows **LOOP STALLED** | the bot's API answers but its loop has not cycled for 3 intervals (≥ 2 min) | if it persists past the watchdog limit the bot restarts itself; otherwise `docker compose restart bot-<coin>` |
| `skip slot: no suitable expiry` | no listed expiry inside the slot's DTE window | widen `max_dte_deviation` or change the slot DTE |
| testnet fills look odd | testnet quotes are thin or synthetic | use testnet to prove mechanics, not profitability |

## Book vs exchange

Two log lines mean the bot corrected itself; both are `ERROR` so they stand out:

- `order submit outcome unknown` — an order went out as the connection dropped. The bot cancels it by its label (`unconfirmed order resolved` when done) and sends nothing else for that slot or instrument meanwhile.
- `position drift` — the exchange's position differs from the bot's book (an unknown order filled before it could be cancelled, a manual trade, a close whose reply was lost). The bot adopts the exchange's size (journaled as `reconciled`), so the position is managed by the stop-loss and rolls.

They mostly appear after network outages. On a laptop, prevent sleep while the bots run (`caffeinate -s`, or disable sleep on power): on 2026-10-04 both connections dropped six times overnight.

## Margin policy

The bot sizes and limits itself on Deribit's own margin figures (see *Margin policy and sizing* in [architecture.md](architecture.md)). What to know when running it:

- **Files.** `data/regime_history.jsonl` keeps the gamma regime at each daily close (Deribit has no history of it). Like `data/pnl_history.jsonl`, keep `data/` on a persistent volume; deleting it restarts the regime confirmation.
- **First days after deploying.** With no recorded regime closes yet, new entries are **frozen** until the regime has been confirmed (2 daily closes by default). Exits, rolls and repairs run normally. The monitor shows the countdown.
- **Journal.** Every policy change is a `risk_limit` line in `orders.log` (`change`: `frozen`, `unfrozen`, `limit_changed`, `rebalance`, `rebalance_retry`, `mm_breach`) with IM/MM %, limits, DVOL, IV percentile and regime.
- **Reading a skip.** `risk_frozen` = a band/regime change awaits confirmation; `margin_limit` = no headroom, or no size fits after simulation; `margin_unknown` = Deribit margin data unavailable (fail safe); `repair_held` = a stopped-out leg waits for a calm market (the reason says which condition: frozen, negative gamma, or cooldown until a time).
- **Rate limit.** `private/simulate_portfolio` is limited by Deribit to one call per second; the executor spaces calls, so sizing three slots takes a few seconds of the decision cycle.
- **Two bots, one account.** Under cross collateral both bots measure the same account-wide IM and MM, each against its own limit (BTC and ETH DVOL can sit in different bands). The more permissive limit is effectively the account's ceiling for new entries; the MM limit protects the whole account.

## Two connections on testnet

On testnet the bot keeps **two** WebSocket connections: `gateway=trading` (testnet, authenticated: orders, positions, margin, tickers) and `gateway=mainnet-public` (mainnet, never authenticated: the book summary for GEX and open interest). Logs carry the `gateway` field; the public one starts with `connected (public data only, not authenticated)`. Each has its own rate limiter. If either gives up reconnecting the bot exits non-zero so the supervisor restarts it. `gex_snapshot` lines show `method`, `regime`, `gamma_flip` and `first_expiry` — compare them with GestaoCarteira's `runOptIndicator.sh`, which uses the same rules.
