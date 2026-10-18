# Operations guide

How to run, watch and stop the bot. For architecture and design see the [main README](../README.md); for the trading logic in plain language see [OptionStrategy.md](../OptionStrategy.md).

> Testnet is the default. Live trading only happens with `DERIBIT_ENV=live` in `.env` — an explicit opt-in.

## Contents
- [Run locally](#run-locally)
- [Environments: stable and mvp](#environments-stable-and-mvp)
- [Live monitor](#live-monitor)
- [Watch the logs](#watch-the-logs)
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

## Environments: stable and mvp

Two local environments run side by side and **share nothing at runtime**. Your development checkout runs neither.

| | **stable** (pseudo-production) | **mvp** (staging) |
|---|---|---|
| Where | `~/dev/go/optionsBot-env/stable` (its own clone) | `~/dev/go/optionsBot-env/mvp` (its own clone) |
| Code | a **release tag**, changed only by `scripts/promote.sh` (a person runs it) | `main`: `scripts/agents/deploy.sh` moves it after every merge |
| Deribit | its own testnet account (`.env` in the clone) | **another** testnet subaccount (`.env` in the clone) |
| Monitor | <http://localhost:3000> | <http://localhost:3100> |
| Data | its own `data/`: the track record, never reset | its own `data/` |
| Market recorder | on | off (`BOT_RECORD_MARKET=0`): one dataset, no duplicate mainnet calls |
| BTC direction source (`gex_direction`) | `bias` (`config_btc.yaml`) | `sma` (`config_btc.mvp.yaml`, `BOT_CONFIG_BTC` in `deploy.env`, #168): the two react to the same BTC moves with different shed rules |
| Agents | the SME agent judges performance here | the agents run from this clone; the SME agent verifies fixes here |

The environments folder is `optionsBot-env` beside your checkout (`~/dev/go/optionsBot` → `~/dev/go/optionsBot-env`), holding `stable/`, `mvp/`, `history/` (deploys and promotions) and `agent-worktrees/`; set `OPTIONSBOT_ENVS` to put it elsewhere.

**Run `scripts/envctl.sh` and `scripts/promote.sh` from your development checkout** (`~/dev/go/optionsBot`), on an up-to-date `main`. Each environment's clone keeps the scripts of the code it runs: stable's are its release tag's, which may predate today's layout or checks. Only the scheduled agents run from the mvp clone, which `deploy.sh` keeps on `main`.

**Why separate accounts:** at startup a bot cancels every order of its currency on the account, and every cycle it adopts any short position it finds there. Two environments on one account would cancel each other's orders and manage each other's positions. `envctl.sh` refuses to start an environment whose `DERIBIT_CLIENT_ID` (or monitor port) another one uses.

```bash
scripts/envctl.sh mvp init              # clone into ~/dev/go/optionsBot-env/mvp + write deploy.env; then add its .env
scripts/envctl.sh mvp check             # set up, and sharing nothing with stable?
scripts/envctl.sh stable up             # build and start
scripts/envctl.sh stable status         # version, containers, monitor URL
scripts/envctl.sh stable logs -f bot-btc
scripts/envctl.sh stable stop bot-eth   # any scripts/envctl.sh stable command, on that stack only
scripts/envctl.sh stable down
```

Each clone's `deploy.env` holds `OPTIONSBOT_ENV`, `MONITOR_PORT` and `BOT_RECORD_MARKET`, plus, in mvp, `BOT_CONFIG_BTC=./config_btc.mvp.yaml`. That file is `config_btc.yaml` with only `gex_direction` changed, and `TestConfig_MVPBTCDiffersOnlyInDirection` fails on any other difference: when you change `config_btc.yaml`, copy the change there. `envctl.sh` also sets `BOT_VERSION` (`git describe`), and the bots report `deployment` and `version` in `/api/status`, which the monitor header shows (**STABLE · v0.3.0**, **MVP · v0.3.0-4-gabc123**). A plain `docker compose up` refuses to run (no `OPTIONSBOT_ENV`): always go through `envctl.sh`.

### Releasing to stable

```bash
scripts/promote.sh v0.3.0          # tags the commit mvp runs (if the tag is new), checks, asks, promotes
scripts/promote.sh v0.2.0          # rollback: a tag stable ran before skips the soak
```

A new release is promoted only when the tagged commit (or a later one) has run in mvp for **3 days** (`PROMOTE_SOAK_DAYS`), and no open issue carries `sme:not-fixed` (the SME agent found that a merged fix did not work). Then stable checks out the tag and rebuilds, and every bot's decision loop must report again within 5 minutes. **If not, stable goes back to its previous release.** History: `~/dev/go/optionsBot-env/history/deploys.tsv` (mvp) and `promotions.tsv` (stable). Promotion is testnet-only: a live stable is promoted by hand.

### Inside an environment

`docker-compose.yml` defines one service per underlying (`bot-btc`, `bot-eth`) plus the monitor. Each bot mounts its own config as `/app/config.yaml` and **runs inside its own host folder** of the clone (`data/btc/`, `data/eth/`, mounted at `/app/run`), so everything it keeps persists across rebuilds and the two never share a file:

```
data/btc/
  bot.log             structured log
  orders.log          decision journal (every order, fill, close, skip, P&L, margin-policy change)
  hedge_report.json   delta-hedge suggestion, when written
  data/pnl_history.jsonl     P&L history for the monitor chart
  data/regime_history.jsonl  gamma regime per daily close (margin policy)
  data/market/        recorded mainnet books (stable only), read by the backtest, stress and replay tools
```

`restart: unless-stopped` revives a crashed container, and the bot's decision-loop watchdog exits a stuck process so it is restarted. The bot reconciles its positions with Deribit on every start, so a restart is always safe. Research tools in the dev checkout read stable's recorded books through `data/btc` → `~/dev/go/optionsBot-env/stable/data/btc` (a symlink). Running a bot by hand (`./bot`, above) needs a Deribit account no environment uses.

## Live monitor

Each environment also starts its **monitor** (stable <http://localhost:3000>, mvp <http://localhost:3100>, this machine only): open positions per strategy and slot with strikes, DTE and Greeks, working orders, a live activity feed with reasons and market context, and P&L — refreshed every second. It is read-only. The P&L chart's longer ranges (1h to all) read each bot's P&L history (`data/<underlying>/data/pnl_history.jsonl`), which survives restarts and rebuilds; delete it to start the history over. Details and local/demo runs: [frontend/README.md](../frontend/README.md).

## Watch the logs

**Market history.** Each bot records the option market every 60 minutes to `data/<coin>/data/market/YYYY-MM-DD.csv.gz` — one gzip CSV per UTC day, one block appended per snapshot (`zcat` reads it whole). Columns: `date` (snapshot time, RFC 3339 UTC), `instrument, underlying, underlying_price` (the expiry's forward), `strike, expiry, option_type, bid, ask, mid, mark` (coin), `delta, gamma, theta, vega` (Black–Scholes from the mark IV), `iv, open_interest, dvol_index`. The quotes are Deribit **mainnet** (the GEX poll's book summary), even on testnet, because testnet books are thin. About 0.5 MB a day per coin. `--reset-history` does not touch it. Log lines: `market snapshot recorded` / `market snapshot not recorded`.

The same record for the coin's **USDC-settled** options (`BTC_USDC-…`, prices in USDC; `underlying` = `BTC_USDC`) goes to `data/<coin>/data/market_usdc/`, one extra mainnet `public/get_book_summary_by_currency` (USDC) call per snapshot. It exists to compare their liquidity with the coin-settled options (2026-10-13: as tight or tighter on a calm day, but under 0.5 % of the open interest), especially on volatile hours; trading never reads it. The log lines carry `dir`.

**GEX every minute** (#122). Each GEX snapshot the bot computes (one a minute) is also written as one JSON line to `data/<coin>/data/market_gex/YYYY-MM-DD.jsonl.gz` (gzip member per line; `zcat` reads it): `computed_at, spot, regime, flip, flip_found, median_flip` (the bias's window median, `StableBiasDir`), `gex_sum` (Σ weighted signed GEX), `dvol, flip_buffer_pct`. No Deribit call, a few hundred KB a day, never read by trading, not touched by `--reset-history`; recorded whenever the market history is. `cmd/stress -replay` reads it to decide the GEX side at the bot's pace. Log lines: `gex recorder started` / `gex snapshot not recorded`.

**Activity feed.** The bot keeps its last 500 decisions in memory for the monitor (`/api/events`); periodic `pnl` lines are counted but not kept there, so they never push decisions out — the feed shows the latest orders, fills, closes, skips, margin changes and SMA trend changes (`trend_changed`, journaled once per change with the reason and the levels, whichever `gex_direction` the bot uses) however long ago they happened, also right after a restart.

**History across restarts.** Each bot replays its journal (`orders.log`) at startup, so realized P&L, close and order counts, the activity feed and the trade history continue where they stopped (`history restored from the journal` in the log gives the totals). To start a fresh history — e.g. after changing the strategy — archive it; nothing is deleted:

```bash
scripts/envctl.sh stable stop bot-btc
scripts/envctl.sh stable run --rm --no-deps bot-btc --config /app/config.yaml --env-file /app/.env --reset-history
scripts/envctl.sh stable start bot-btc
```

The journal and `data/pnl_history.jsonl` move to `data/btc/data/archive/<UTC time>/`; move them back to restore that history.

`bot.log` (structured `slog` JSON) and `orders.log` (the decision journal) are both mirrored to stdout, so `scripts/envctl.sh stable logs` shows one stream; the files themselves are in `data/<underlying>/`. Each `orders.log` line has an `event` (`submitted`, `amended`, `cancelled`, `filled`, `closed`, `reconciled`, `skipped`, `pnl`), the `strategy_id` and `slot`, a `market` object with the conditions at that moment and a `portfolio` object with the book's Greeks. Closes also carry `detail`: the rule that fired, with its numbers — e.g. `stop-loss: loss 2.31× the premium ≥ 2.00×`, `delta exit: |Δ| 0.312 ≥ 0.30`, `take-profit: 55% of the premium earned ≥ 50%`, `time roll: 15 days to expiry ≤ 15`, `leg balance: call … 262 vs put … 162 — buying back the 100 excess … (not a stop)`, `GEX shed: …`, `MM limit: …`, `downsize: …` — and the option's greeks at that moment (`delta`, `gamma`, `theta`, `vega`, per contract as Deribit quotes them). Fills carry `fee` (what Deribit charged, coin, from the order's trades) and, for entries, repairs and top-ups, how much of the order filled: `qty` of `target_qty`, `fill_pct` = qty ÷ target_qty × 100; a cancelled sell carries `filled_qty` of its `qty` (absent = nothing filled) — together they measure how much of each target size the book absorbed. Closes carry `fees` (the opening share + the closing fee) and a **net** `pnl` = premium − buy-back − fees. Every P&L the bot books, journals, serves or charts is net; closes journaled before 2026-10-06 are gross. Sells carry a `trigger_reason` saying why they were sent: `entry` (a vacant slot), `repair` (re-selling the missing leg of a one-legged strangle) or `rebalance_upsize` (a complement bringing a slot up to the IM limit), and `close_long` for a long found on a traded instrument (sold at the bid; the strategy only holds shorts). A `churn_paused` skip means the churn breaker paused a slot; ERROR lines `buy back capped`, `long position on a traded instrument` and `churn breaker` mean the book and the exchange disagreed — check `position drift` lines around them.

```bash
scripts/envctl.sh stable logs -f bot-btc                                    # everything, one underlying
scripts/envctl.sh stable logs -f                                            # both, prefixed by service
scripts/envctl.sh stable logs -f bot-btc | grep '"msg":"heartbeat"'         # equity, margin, positions every 60 s
scripts/envctl.sh stable logs -f bot-btc | grep '"order_id"'                # all order events
scripts/envctl.sh stable logs -f bot-btc | grep '"close_reason":"stop_loss"'
scripts/envctl.sh stable logs bot-btc | grep '"close_reason"' \
  | jq '{instrument, close_reason, pnl, pnl_usd_fmt, roi_pct_fmt, hold_days}'

# P&L per strategy slot and total (every 60 s)
grep '"event":"pnl"' orders.log | jq '{slot, realised, unrealised, total_usd, open_legs}'

# Market conditions at each entry: DVOL, moneyness, open interest
grep '"event":"filled"' orders.log \
  | jq '{instrument, slot, dvol: .market.dvol, iv_pct: .market.iv_percentile,
         moneyness: .market.moneyness, dist_pct: .market.distance_to_strike_pct,
         strike_oi: .market.strike_oi, oi_rank: .market.strike_oi_rank, gex: .market.gex_regime}'

# Why slots were not entered
grep '"event":"skipped"' orders.log | jq '{slot, skip_reason, dvol: .market.dvol}'
scripts/envctl.sh stable logs -f bot-btc | grep '"msg":"rate_limit_metrics"' # gateway health every 60 s
```

Useful events to know: `strangle filled and active`, `rollout triggered`, `stop loss triggered`, `partial close: remainder stays open and tracked`, `gex_regime_trigger`, `reconnecting` / `reconnected successfully`, `gateway giving up`, `hedge_report`.

## Agents (SME review, PM, Dev agent, mvp deploy)

The repo is developed with AI agents and a person approving: the **SME agent** reviews the bots daily and files GitHub issues, the **Dev agent** implements issues moved to *Ready* as pull requests, and merging to `main` redeploys **mvp** (stable moves only with `scripts/promote.sh`). How it works, permissions and guardrails: [AGENTIC.md](../AGENTIC.md).

```bash
gh auth refresh -s project              # once
scripts/agents/setup_github.sh          # once: labels, project board, ruleset on main
scripts/agents/install.sh               # launchd: SME daily at 09:00; deploy + Dev every 15 min
launchctl kickstart gui/$(id -u)/com.optionsbot.sme-review   # run a review now
scripts/agents/install.sh --uninstall
```

Run them on the machine that runs the bots, logged in to Claude Code and `gh`. Reports and the data each review read: `data/sme/<date>/`; Dev runs, deploys and launchd logs: `data/agents/`. The agents run from the **mvp clone** (`~/dev/go/optionsBot-env/mvp/scripts/agents/install.sh`), so each deploy also updates their scripts; automatic deploys need that clone on a clean `main` and its bots on testnet; set `DEPLOY_ENABLED=0` to pause them, `SME_PUBLISH=0` to keep the SME's findings local.

## Kill switch

Flattens every position at market and halts trading:

```bash
docker kill -s USR1 <container>     # Docker
kill -USR1 <pid>                    # local process
```

What happens: resting orders for the underlying are cancelled, whatever a working entry filled before the cancel is booked, the book is matched to the exchange's short positions, every position is bought back at market (partial fills retried), anything still open is logged as an `ERROR` for manual handling, and the bot then **stays idle** — it does not exit, so Docker's restart policy cannot put it straight back into the market. To resume trading, restart the container deliberately (`scripts/envctl.sh stable restart bot-btc`).

## BTC and ETH side by side

Both services share one Deribit account (same `.env`) but each process has its own rate limiter, unaware of the other. At normal load this is fine; if both roll many legs at once, combined requests can approach the account limit. Watch `rate_limit_metrics` and any `too_many_requests` errors; if they appear, **increase** `eval_interval_ms` or stagger restarts. Startup clean-up cancels only the bot's own currency (`private/cancel_all_by_currency`), so restarting one never touches the other's orders.

## macOS sleep caveat

Docker Desktop suspends containers while the Mac sleeps. Use `caffeinate -i`, disable sleep, or — most reliable — run on a small always-on VPS.

## Troubleshooting

| Symptom | Likely cause | What to do |
|---|---|---|
| `auth failed: no access_token` | wrong key/secret or missing scopes | check `.env`; key needs `account:read` and `trade:read_write` |
| `invalid config: …` at startup | a value failed validation | the message names the field; see the configuration table in the README |
| `forbidden` then a 60 s pause | API key scope revoked | fix the key; the bot backs off instead of spamming |
| `circuit breaker opened` | exchange overload/maintenance responses | wait; stop-losses still go through (high priority bypasses the breaker) |
| `gateway giving up` then exit code 1 | reconnect attempts exhausted | the supervisor restarts it and reconcile restores state |
| `margin_unknown` skips, `websocket read error` / `reconnected successfully` | the connection to Deribit dropped or a request timed out | `curl -s $BOT_API_ADDR/api/status \| jq .connections` shows drops, reconnects and timeouts today per gateway with the last drop's time and error; the same time on both bots (and on `trading` and `mainnet-public`) points at the host or network, not the bot |
| `decision loop stalled` then exit code 1 | no decision cycle for 10+ minutes (stop-losses were not being checked) | the supervisor restarts it; report it — something blocked the loop |
| monitor shows **LOOP STALLED** | the bot's API answers but its loop has not cycled for 3 intervals (≥ 2 min) | if it persists past the watchdog limit the bot restarts itself; otherwise `scripts/envctl.sh stable restart bot-<coin>` |
| `skip slot: no suitable expiry` | no listed expiry inside the slot's DTE window | change the slot DTE (the ±10-day window is fixed in code) |
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
- **Rate limit.** `private/simulate_portfolio` is limited by Deribit to one call per second; the executor spaces calls, so sizing every slot takes a few seconds of the decision cycle.
- **Two bots, one account.** Under cross collateral both bots measure the same account-wide IM and MM, each against its own limit (BTC and ETH DVOL can sit in different bands). The more permissive limit is effectively the account's ceiling for new entries; the MM limit protects the whole account.

### Manual rebalance (after a deposit or withdrawal)

The bot resizes its book by itself only when the confirmed IM limit changes, or at startup. A deposit or withdrawal moves the margin balance, not the limit, so existing strangles keep their old size until then; new entries size on the new balance at once (it is read from Deribit every cycle), and the MM limit still cuts at any time. The bot never moves funds and journals no transfer: the change shows only in `/api/account`, so note a hand transfer where the P&L is reviewed (2026-10-17 ETH: −127 ETH moved by hand, #124). To resize now without a restart, use the **Rebalance** button in the monitor's *Margin policy per bot* table: it first shows a dry run (what it would sell or buy back, nothing sent), then runs on confirmation. Under Docker Compose only the monitor's port is published, so use the button; a bot run locally (`BOT_API_ADDR`, `:8081` by default) can also be called directly:

```bash
# preview: same margin simulations, no orders, nothing journaled
curl -X POST -H "Authorization: Bearer $BOT_ADMIN_TOKEN" "http://localhost:8081/api/rebalance?dry_run=true"
# run it
curl -X POST -H "Authorization: Bearer $BOT_ADMIN_TOKEN" "http://localhost:8081/api/rebalance"
```

It applies the same gates as the automatic rebalance — **409** with the reason when new risk is frozen, margin data is missing, the MM limit is reached, GEX is stale or a complement or an entry is still working; **429** (`Retry-After`) within 5 minutes of the last real pass (30 s between dry runs); **504** if the decision loop did not take it. It keeps every hold (stops, hand closes, regime side) and at least one lot per strangle, and never cancels working orders. A real pass is journaled as `risk_limit` `rebalance_manual`.

## Two connections on testnet

On testnet the bot keeps **two** WebSocket connections: `gateway=trading` (testnet, authenticated: orders, positions, margin, tickers) and `gateway=mainnet-public` (mainnet, never authenticated: the book summary for GEX and open interest). Logs carry the `gateway` field; the public one starts with `connected (public data only, not authenticated)`. Each has its own rate limiter. If either gives up reconnecting the bot exits non-zero so the supervisor restarts it. `gex_snapshot` lines show `method`, `regime`, `gamma_flip` and `first_expiry` — compare them with GestaoCarteira's `runOptIndicator.sh`, which uses the same rules.
