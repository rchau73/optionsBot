# optionsBot monitor (frontend)

A **read-only, single-page live monitor** for the bots: what each strategy holds, what it just did and how it is doing, refreshed every second. All UI documentation lives here; the backend docs only describe the API.

> Educational study of an automated trading system — not investment advice. The monitor is meant for **Deribit testnet** first.

## What you see

| Area | Shows |
|---|---|
| Header | one chip per bot: TESTNET / LIVE, data freshness (turns STALE / OFFLINE), **LOOP STALLED** when the API answers but the decision loop has not cycled for 3 intervals (no exits or stop-losses are being checked), HALTED after a kill switch, spot, DVOL, IV percentile, GEX regime, trend, the **SMA trend's levels** (`/api/status` → `sma`: SMA9, SMA21, the last confirmed swing high and low with their day, each with its distance from spot in %; hover for the trend and the condition that set it), and the **gamma flip** with spot's distance to it and its **±1σ band** (σ = the bot's own shed buffer, `flip_buffer_pct` = `gamma_flip_buffer_sd` × DVOL ÷ √365: in a negative regime a leg is shed below the band and repaired above it), flagged **below flip −1σ** when spot is under it |
| KPI strip | bots online, open legs, pending orders, orders sent / filled, closes, skipped entries, realised / open / total P&L (USD at spot) — **since the journal began** (the date is shown), restored when a bot restarts |
| Account & collateral | collapsible (remembered per browser; folded, it keeps IM vs limit per bot, worst MM and warning badges in one line) — the shared Deribit account, as Deribit reports it: margin model (cross/segregated · portfolio/standard), cross collateral on/off, USD totals (equity, margin balance, IM, MM) with **IM % / MM % bars** (amber from 50 %, red from 80 %; Deribit liquidates at MM 100 %), and per asset: balance, equity, margin balance, available, withdrawable, IM and MM (and projected, without the nearest expiry), IM % / MM %, reserved. Flags STALE data, a failed poll and high margin. **Margin policy per bot**: IM used vs the active limit and *why* (DVOL band, or negative gamma), MM used vs its limit, whether new entries are allowed or FROZEN (with the reason), and any pending change with its countdown (e.g. *DVOL → band ≥70: 1 of 2 daily closes*), and beside the Rebalance button an amber *IM over limit since HH:MM:SS — review with a dry run* badge while the bot reports `risk.im_over_limit` (IM above the limit with no rebalance due; the bot resizes nothing by itself). The limits are drawn as ticks on the IM / MM bars |
| Open positions | one row per bot + slot + strike — positions on the same strike (partial fills, rebalance top-ups) are merged, marked **×n**, with summed qty and P&L, qty-weighted entry and the first stop to fire — instrument, slot (DTE · Δ), call/put, **side** (SELL = short, premium collected; BUY = long, premium paid — e.g. an iron-condor wing), strike, DTE, qty, entry, mark, unrealized P&L (coin and USD), **P&L %** (unrealized P&L as % of the premium: +50% = half the premium is profit, −100% = loss equal to the premium; the stop-loss fires at −200% with `stop_loss_multiplier: 2`), stop-loss mark, ITM/ATM/OTM and distance, Δ Γ Θ Vega. Filter by bot, strategy, type, moneyness; group by strategy, slot, expiry or type; each group header totals unrealized P&L (coin and USD), **P&L %** (group P&L ÷ group premium, in USD) and the **position Greeks** (each leg's per-option Greek × qty, negative for shorts — so the header is on a different scale than the per-option Greeks in the rows; Δ and Γ blank when a group mixes BTC and ETH). **Close** button per row (manual buy-back at market): active only while the bot's regime-side block covers that leg's side — a confirmed negative gamma regime with puts in a bear trend or calls in a bull trend; otherwise gray, with the bot's reason on hover. A confirmation window shows the cost at the ask, the confirmed and live gamma regime, the flip ±1σ and the trend, and what the bot does next — **red** when the close would be rebuilt (the strangle's last leg: its slot is re-entered). Needs `BOT_ADMIN_TOKEN` on the monitor and the bots; the proxy adds it server-side |
| Missing legs | collapsible — one row per one-legged strangle (`/api/status` → `held_legs`, built by the bot from the same gate chain repair runs): the rule holding the repair now (regime side, stop hold, GEX anchor, GEX shedding, MM limit, …) and its reason; the confirmed regime, the bot's SMA trend and the reference script's bias (spot vs flip, negative regime); the **live flip + buffer** next to the **anchor flip + buffer** a GEX-shed leg waits on (snapshots counted, release hours), each with spot's distance to it; and the stop cooldown's end |
| Not managed by the bot | every other position on the account, of any kind (a hand-bought hedge, a future, …): side, size, average price, mark, P&L, Δ and its IM / MM, as Deribit reports them. No bot trades or sizes these, but they use margin, so they are never hidden. Collapsible; says "unavailable" (not "none") when the list could not be read |
| Trade history & profitability | every open and close since the journal began: realized P&L (USD at each close, and per coin), closes, win rate, average win vs average loss and their ratio, P&L by exit reason and by slot (worst first), and the list itself (time, bot, instrument, slot, qty, price, reason **with the rule and numbers that fired it** (e.g. "leg balance: call 262 vs put 162 … not a stop"), the option's **Δ (position Δ = −Δ × qty) · Θ · Vega at that moment**, P&L coin/USD, P&L %, days held, spot · DVOL at that moment). The activity feed shows the same reason and Δ on each close with filters by bot, open/close and exit reason. Collapsible |
| Activity | live feed of orders sent, re-priced, cancelled, opened, closed (take-profit, rolls, stop-loss, GEX shed, kill switch) skipped entries with the reason, and margin-policy changes (entries frozen/unfrozen, IM limit changed, rebalance, MM breach — with DVOL, IV percentile and regime), and **SMA trend changes** (`trend_changed`: old → new trend, the condition that fired, spot, SMA9, SMA21 and the swings — once per change, under either direction source), each with its market context (spot, DVOL, moneyness, open interest, regime) |
| P&L chart | total and realized P&L across bots (USD), plus one dashed line per bot (BTC, ETH) when several run, so each coin's contribution is visible. **Live** = the last 15 minutes of stored history, then this session every refresh (a reload never starts empty); **1h · 6h · 1d · 1w · 1m · All** = the bots' stored history (survives restarts), about 300 points per range |
| P&L in coin | each bot's realized and total P&L **in its own coin** (BTC, ETH), net of fees, from the same stored history: the coin's own price moves are left out, so it shows whether the strategy is growing the coin stack. One chart per bot (coins cannot be summed); **1h · 6h · 1d · 1w · 1m · All** |
| Working orders | entry/repair orders still on the book: fill progress, limit, time left before the fill timeout, age |

**The panels follow the window** (CSS only, no resize listeners): on wide screens (`lg` and up) the Activity panel is as tall as its row — the P&L charts and working orders beside it, and never less than 20rem or 70 % of the window — and its list scrolls inside it; the P&L charts scale with the window height between a minimum and a maximum (`clamp()` on `vh`), and so does the rebalance dialog's action list. On narrow screens the panels stack and the Activity list keeps a bounded height (60 % of the window, at least 20rem) with its own scroll bar.

**Every P&L on the page is net of Deribit fees** (opening fees on open legs; opening share + closing fee on closes). Closes journaled before fees were recorded (2026-10-06) are gross. Counts, realized P&L, the activity feed and the trade history are **restored from each bot's journal** (`orders.log`) when it starts, so a restart of a bot or of the monitor loses nothing. The P&L chart's ranges come from each bot's `data/pnl_history.jsonl` (one point every 60 s, kept for a year); its realized line is the journal's, so the chart, the KPIs and the trade history agree. To start a fresh history, see `--reset-history` in `docs/operations.md`. Other history (positions, events, change markers) is a later phase of the [dashboard spec](../.claude/skills/option-bot-specialist/references/dashboard.md).

## How it works

```
browser ──1 s polls──▶ Next.js server ──read-only proxy──▶ bot API (Go, internal/api)
                       /api/bots/<bot>/<endpoint>           /api/status · positions · orders · pnl · pnl/history · events · account
```

- Each bot exposes a read-only JSON API when `BOT_API_ADDR` is set. It is built from the bot's in-memory state — **no Deribit calls, no order endpoints**.
- The browser talks only to the Next.js server, which forwards an allow-list of GET endpoints (`lib/bots.js`). Bot URLs are configured with `BOT_APIS` and never reach the browser.
- Events are fetched incrementally (`/events?since=<seq>`); a bot restart is detected and the feed resumes.
- **Every page and every `/api` route needs a signed-in session** (below). The bots' APIs know nothing about it: they stay unpublished on the compose network and the monitor reaches them as before (with `BOT_ADMIN_TOKEN` for close and rebalance).

## Sign-in

People sign in at `/login` with a username and password; `proxy.js` sends every page without a session to `/login?next=…` and answers 401 on every `/api` route except `/api/auth/login` and `/api/auth/refresh`. The close and rebalance routes check the session again themselves.

| Part | How |
|---|---|
| Users | SQLite (`sql.js`, WebAssembly — no native build in the image) in `data/monitor/auth.db` (`AUTH_DB_PATH`), a `monitor-auth` Docker volume per environment: stable and mvp have separate users. Table `users`: username (3–64 of `a-z 0-9 . _ -`, case-insensitive), argon2id hash, role `admin`/`manager`/`user`, email. Roles are stored now and enforced in #171 |
| Passwords | argon2id (`hash-wasm`; 19 MiB, 2 passes), at least 12 characters. An unknown user is checked against a dummy hash, so it takes as long as a wrong password and gets the same 401 `invalid username or password` |
| Access token | JWT HS256 (`jose`), 15 min, cookie `access_token` (HttpOnly, SameSite=Lax, path `/`), signed with `AUTH_SECRET` |
| Refresh token | 32 random bytes, only its SHA-256 stored, 14 days, cookie `refresh_token` on path `/api/auth`; reusable until logout (several tabs). A password change, a role change or a deletion ends every session of that user |
| Silent refresh | `lib/api.js` `apiFetch`: the monitor's own 401 (header `x-monitor-session: required`) → one refresh shared by every waiting request → one retry; when that fails the page goes to `/login` and comes back afterwards. A bot's 401 passed through a proxy route is shown as an error, never a sign-out |
| Lockout | 5 failed sign-ins for a username within 15 min lock it until 15 min after the last one (same 401); 20 sign-ins a minute per IP, then 429 with `Retry-After`. In memory: a restart of the monitor forgets it |
| Secure cookies | `Secure` except on plain http to `localhost`/`127.0.0.1` (the compose port), where a browser would drop them. Opened from another machine over plain http, sign-in cannot work: put TLS in front first |
| Service token | `MONITOR_API_TOKEN` (≥ 32 chars) as `Authorization: Bearer …` reads `GET /api/bots/*` without a session — the deploy health check, `promote.sh`, the SME agent and `stress_inputs.sh` use it (`env_monitor_token` in `scripts/env_lib.sh`). Never a page, a close or a rebalance |
| Logs | one JSON line per sign-in, failure (reason, `locked`), lockout, rate limit, sign-out and first admin, with username and IP — never a password, hash, token or `AUTH_SECRET` |

The first start with no users creates an admin from `ADMIN_USERNAME` / `ADMIN_PASSWORD` / `ADMIN_EMAIL`; later starts never touch existing users. The server exits at start (`instrumentation-node.js`) when `AUTH_SECRET` is missing or shorter than 32 bytes, or the database cannot be opened.

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

Local runs need the sign-in variables too, e.g. in `frontend/.env.local` (git-ignored): `AUTH_SECRET=$(openssl rand -base64 48)`, `ADMIN_USERNAME=admin`, `ADMIN_PASSWORD=<12+ characters>`; the user database is then `frontend/data/monitor/auth.db` (git-ignored).

**Against locally running bots:** start each bot with `BOT_API_ADDR=127.0.0.1:8081` (BTC) / `127.0.0.1:8082` (ETH), then `npm run dev`.

| Variable | Default | Meaning |
|---|---|---|
| `BOT_APIS` | `btc=http://127.0.0.1:8081,eth=http://127.0.0.1:8082` | comma-separated `name=url` of each bot API (server side only) |
| `MONITOR_POLL_MS` | `1000` | how often the page refreshes (500–60000 ms). Polling reads the bots' memory only — it never uses Deribit rate limits |
| `AUTH_SECRET` | — (required) | signs the access tokens; ≥ 32 bytes, from the environment's `.env`, never in git |
| `ADMIN_USERNAME`, `ADMIN_PASSWORD`, `ADMIN_EMAIL` | — | the first admin, created only while there are no users |
| `MONITOR_API_TOKEN` | — (off) | read-only service token for scripts (≥ 32 chars) |
| `AUTH_DB_PATH` | `data/monitor/auth.db` | the user database |

## Develop

```bash
npm run dev     # dev server with hot reload
npm test        # Jest + Testing Library
npm run lint    # ESLint (next/core-web-vitals)
npm run build   # production build — catches Server/Client Component mistakes
```

| Folder | Contents |
|---|---|
| `app/` | `layout.jsx`, `page.jsx` (composition only), `login/page.jsx`, `api/bots/…` read-only proxy routes, `api/auth/{login,refresh,logout}` |
| `proxy.js`, `instrumentation*.js` | the session guard on every route; the start-up check (`AUTH_SECRET`, first admin) |
| `lib/auth/` | server-side sign-in: `store.js` (SQLite), `password.js` (argon2id), `tokens.js` (JWT, cookies), `limits.js` (lockout, per-IP), `service.js` (login/refresh/logout), `server.js` (the shared instance) |
| `components/` | one panel per file: `Header`, `KpiStrip`, `Filters`, `PositionsTable`, `PendingOrders`, `ActivityFeed`, `PnlChart`, `AccountPanel`, `Monitor` (+ `Panel`, `Badge`, `StatTile`, `MarginBar`) |
| `hooks/` | `useMonitor` — polling, incremental events, staleness, session history |
| `lib/` | `api.js` (the only fetching module), `monitor.js` (pure grouping/summaries/event text), `format.js` (units), `bots.js` (server config) |
| `__tests__/`, `test/` | tests and shared fixtures |

Stack: Next.js (App Router) · Tailwind CSS v4 with design tokens in `app/globals.css` (`profit`, `loss`, `call`, `put`, `live`, `testnet`…) · Recharts · clsx.

## Rules of the road

- **Read-only.** Anything that would change trading state (pause, kill switch, parameter edits) needs authentication, an explicit confirmation step and an audit entry, and must reuse the bot's existing code paths — it is a planned phase, not part of this page.
- **Never expose it publicly.** It shows positions and P&L; Docker binds it to `127.0.0.1`. Sign-in protects it from anyone who reaches the port, but it is no substitute for TLS and a closed port.
- Every number carries its unit (BTC/ETH, USD, %); missing data shows "—", never 0; stale data is flagged.
- No business logic in Next.js routes — the Go bot is the single backend. The one exception is the monitor's own users and sessions (`lib/auth/`).
- **Times: stored in UTC, shown in BRT.** The bot, its journal, history files and API are UTC; the page converts only for display, in one place (`lib/time.js`, `America/Sao_Paulo`, UTC−3). Change the zone there, never in the data.
