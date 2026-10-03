# Operations guide

How to run, watch and stop the bot. For architecture and design see the [main README](../README.md); for the trading logic in plain language see [OptionStrategy.md](../OptionStrategy.md).

> Testnet is the default. Live trading only happens with `DERIBIT_ENV=live` in `.env` — an explicit opt-in.

## Contents
- [Run locally](#run-locally)
- [Run with Docker](#run-with-docker)
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

`docker-compose.yml` defines one service per underlying (`bot-btc`, `bot-eth`), each mounting its own config as `/app/config.yaml`. `restart: unless-stopped` revives a crashed container; the bot reconciles its positions with Deribit on every start, so a restart is always safe.

```bash
docker compose up -d                 # start both (builds the image on first run)
docker compose up -d bot-btc         # start one
docker compose ps                    # Running / Exited / Restarting
docker compose stop bot-eth          # stop one
docker compose down                  # stop everything
docker compose up -d --build         # rebuild after a code change
docker compose exec bot-btc sh       # shell inside the container (runs as a non-root user)
```

## Watch the logs

`bot.log` (structured `slog` JSON) and `orders.log` (the decision journal) are both mirrored to stdout, so `docker compose logs` shows one stream. Each `orders.log` line has an `event` (`submitted`, `amended`, `cancelled`, `filled`, `closed`, `reconciled`, `skipped`, `pnl`), the `strategy_id` and `slot`, a `market` object with the conditions at that moment and a `portfolio` object with the book's Greeks.

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

What happens: resting orders for the underlying are cancelled, every position is bought back at market (partial fills retried), anything still open is logged as an `ERROR` for manual handling, and the bot then **stays idle** — it does not exit, so Docker's restart policy cannot put it straight back into the market. To resume trading, restart the container deliberately (`docker compose restart bot-btc`).

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
| `skip slot: no suitable expiry` | no listed expiry inside the slot's DTE window | widen `max_dte_deviation` or change the slot DTE |
| testnet fills look odd | testnet quotes are thin or synthetic | use testnet to prove mechanics, not profitability |
