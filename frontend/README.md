# optionsBot monitor (frontend)

A **read-only, single-page live monitor** for the bots: what each strategy holds, what it just did and how it is doing, refreshed every second. All UI documentation lives here; the backend docs only describe the API.

> Educational study of an automated trading system — not investment advice. The monitor is meant for **Deribit testnet** first.

## What you see

| Area | Shows |
|---|---|
| Header | one chip per bot: TESTNET / LIVE, data freshness (turns STALE / OFFLINE), **LOOP STALLED** when the API answers but the decision loop has not cycled for 3 intervals (no exits or stop-losses are being checked), HALTED after a kill switch, spot, DVOL, IV percentile, GEX regime, trend |
| KPI strip | bots online, open legs, pending orders, orders sent / filled, closes, skipped entries, realised / open / total P&L (USD at spot) — **since the journal began** (the date is shown), restored when a bot restarts |
| Account & collateral | collapsible (remembered per browser; folded, it keeps IM vs limit per bot, worst MM and warning badges in one line) — the shared Deribit account, as Deribit reports it: margin model (cross/segregated · portfolio/standard), cross collateral on/off, USD totals (equity, margin balance, IM, MM) with **IM % / MM % bars** (amber from 50 %, red from 80 %; Deribit liquidates at MM 100 %), and per asset: balance, equity, margin balance, available, withdrawable, IM and MM (and projected, without the nearest expiry), IM % / MM %, reserved. Flags STALE data, a failed poll and high margin. **Margin policy per bot**: IM used vs the active limit and *why* (DVOL band, or negative gamma), MM used vs its limit, whether new entries are allowed or FROZEN (with the reason), and any pending change with its countdown (e.g. *DVOL → band ≥70: 1 of 2 daily closes*). The limits are drawn as ticks on the IM / MM bars |
| Open positions | one row per leg — instrument, slot (DTE · Δ), call/put, **side** (SELL = short, premium collected; BUY = long, premium paid — e.g. an iron-condor wing), strike, DTE, qty, entry, mark, unrealized P&L (coin and USD), **P&L %** (unrealized P&L as % of the premium: +50% = half the premium is profit, −100% = loss equal to the premium; the stop-loss fires at −200% with `stop_loss_multiplier: 2`), stop-loss mark, ITM/ATM/OTM and distance, Δ Γ Θ Vega. Filter by bot, strategy, type, moneyness; group by strategy, slot, expiry or type with group totals |
| Not managed by the bot | every other position on the account, of any kind (a hand-bought hedge, a future, …): side, size, average price, mark, P&L, Δ and its IM / MM, as Deribit reports them. No bot trades or sizes these, but they use margin, so they are never hidden. Collapsible; says "unavailable" (not "none") when the list could not be read |
| Trade history & profitability | every open and close since the journal began: realized P&L (USD at each close, and per coin), closes, win rate, average win vs average loss and their ratio, P&L by exit reason and by slot (worst first), and the list itself (time, bot, instrument, slot, qty, price, reason, P&L coin/USD, P&L %, days held, spot · DVOL at that moment) with filters by bot, open/close and exit reason. Collapsible |
| Activity | live feed of orders sent, re-priced, cancelled, opened, closed (take-profit, rolls, stop-loss, GEX shed, kill switch) skipped entries with the reason, and margin-policy changes (entries frozen/unfrozen, IM limit changed, rebalance, MM breach — with DVOL, IV percentile and regime), each with its market context (spot, DVOL, moneyness, open interest, regime) |
| P&L chart | total and realized P&L across bots (USD), plus one dashed line per bot (BTC, ETH) when several run, so each coin's contribution is visible. **Live** = the last 15 minutes of stored history, then this session every refresh (a reload never starts empty); **1h · 6h · 1d · 1w · 1m · All** = the bots' stored history (survives restarts), about 300 points per range |
| Working orders | entry/repair orders still on the book: fill progress, limit, time left before the fill timeout, age |

Counts, realized P&L, the activity feed and the trade history are **restored from each bot's journal** (`orders.log`) when it starts, so a restart of a bot or of the monitor loses nothing. The P&L chart's ranges come from each bot's `data/pnl_history.jsonl` (one point per `report_interval_sec`, kept for a year); its realized line is the journal's, so the chart, the KPIs and the trade history agree. To start a fresh history, see `--reset-history` in `docs/operations.md`. Other history (positions, events, change markers) is a later phase of the [dashboard spec](../.claude/skills/option-bot-specialist/references/dashboard.md).

## How it works

```
browser ──1 s polls──▶ Next.js server ──read-only proxy──▶ bot API (Go, internal/api)
                       /api/bots/<bot>/<endpoint>           /api/status · positions · orders · pnl · pnl/history · events · account
```

- Each bot exposes a read-only JSON API when `BOT_API_ADDR` is set. It is built from the bot's in-memory state — **no Deribit calls, no order endpoints**.
- The browser talks only to the Next.js server, which forwards an allow-list of GET endpoints (`lib/bots.js`). Bot URLs are configured with `BOT_APIS` and never reach the browser.
- Events are fetched incrementally (`/events?since=<seq>`); a bot restart is detected and the feed resumes.

## Run it

**With Docker (recommended)** — from the repository root:

```bash
docker compose up -d          # bot-btc, bot-eth and the monitor
open http://localhost:3000    # bound to 127.0.0.1 only
```

**Without a bot (demo data)** — the real API with a simulated strategy, no exchange connection:

```bash
go run ./cmd/monitor-demo                                       # BTC on 127.0.0.1:8081
go run ./cmd/monitor-demo -underlying ETH -addr 127.0.0.1:8082  # ETH
cd frontend && npm install && npm run dev                       # http://localhost:3000
```

**Against locally running bots:** start each bot with `BOT_API_ADDR=127.0.0.1:8081` (BTC) / `127.0.0.1:8082` (ETH), then `npm run dev`.

| Variable | Default | Meaning |
|---|---|---|
| `BOT_APIS` | `btc=http://127.0.0.1:8081,eth=http://127.0.0.1:8082` | comma-separated `name=url` of each bot API (server side only) |
| `MONITOR_POLL_MS` | `1000` | how often the page refreshes (500–60000 ms). Polling reads the bots' memory only — it never uses Deribit rate limits |

## Develop

```bash
npm run dev     # dev server with hot reload
npm test        # Jest + Testing Library
npm run lint    # ESLint (next/core-web-vitals)
npm run build   # production build — catches Server/Client Component mistakes
```

| Folder | Contents |
|---|---|
| `app/` | `layout.jsx`, `page.jsx` (composition only), `api/bots/…` read-only proxy routes |
| `components/` | one panel per file: `Header`, `KpiStrip`, `Filters`, `PositionsTable`, `PendingOrders`, `ActivityFeed`, `PnlChart`, `AccountPanel`, `Monitor` (+ `Panel`, `Badge`, `StatTile`, `MarginBar`) |
| `hooks/` | `useMonitor` — polling, incremental events, staleness, session history |
| `lib/` | `api.js` (the only fetching module), `monitor.js` (pure grouping/summaries/event text), `format.js` (units), `bots.js` (server config) |
| `__tests__/`, `test/` | tests and shared fixtures |

Stack: Next.js (App Router) · Tailwind CSS v4 with design tokens in `app/globals.css` (`profit`, `loss`, `call`, `put`, `live`, `testnet`…) · Recharts · clsx.

## Rules of the road

- **Read-only.** Anything that would change trading state (pause, kill switch, parameter edits) needs authentication, an explicit confirmation step and an audit entry, and must reuse the bot's existing code paths — it is a planned phase, not part of this page.
- **Never expose it publicly.** It shows positions and P&L; Docker binds it to `127.0.0.1`.
- Every number carries its unit (BTC/ETH, USD, %); missing data shows "—", never 0; stale data is flagged.
- No business logic in Next.js routes — the Go bot is the single backend.
- **Times: stored in UTC, shown in BRT.** The bot, its journal, history files and API are UTC; the page converts only for display, in one place (`lib/time.js`, `America/Sao_Paulo`, UTC−3). Change the zone there, never in the data.
