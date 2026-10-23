# optionsBot monitor (frontend)

A **read-only, single-page live monitor** for the bots: what each strategy holds, what it just did and how it is doing, refreshed every second. All UI documentation lives here; the backend docs only describe the API.

> Educational study of an automated trading system — not investment advice. The monitor is meant for **Deribit testnet** first.

## What you see

| Area | Shows |
|---|---|
| Investor summary (first block, one card per bot; #246) | the holder's four answers from `/api/summary` (`components/InvestorSummary.jsx`, helpers in `lib/summary.js`), fetched with each poll: **what it holds** (strangles, legs, calls/puts, coin sold and its USD value, nearest expiry), **income per month** (last 6 UTC months, newest first, realised net of fees in coin and USD with its sign, the number of closes; a month without closes says "no closes", never zeros; plus the open book's unrealised P&L), **worst case now** (one-day move each way in coin, USD and % of equity, signed; "% of equity unknown" without the equity; "Unknown: reason" when spot or DVOL is missing) with the API's model label beside it in the same type size, and the **last action** in the journal's plain words with its time, size, instrument and, for a close, its net result. Losses and gains share size and weight; only the colour and the sign differ. Empty states: nothing held, no closes yet (says what appears and when), no trade yet; a failed `/api/summary` or a bot that stopped answering → "Summary unavailable: …", no numbers. Everything below is the detail on demand |
| Header | one chip per bot: TESTNET / LIVE, data freshness (turns STALE / OFFLINE), **LOOP STALLED** when the API answers but the decision loop has not cycled for 3 intervals (no exits or stop-losses are being checked), HALTED after a kill switch, spot, DVOL, IV percentile, GEX regime, trend, the **SMA trend's levels** (`/api/status` → `sma`: SMA9, SMA21, the last confirmed swing high and low with their day, each with its distance from spot in %; hover for the trend and the condition that set it), and the **gamma flip** with spot's distance to it and its **±1σ band** (σ = the bot's own shed buffer, `flip_buffer_pct` = `gamma_flip_buffer_sd` × DVOL ÷ √365: in a negative regime a leg is shed below the band and repaired above it), flagged **below flip −1σ** when spot is under it |
| KPI strip | bots online, open legs, pending orders, orders sent / filled, closes, skipped entries, realised / open / total P&L (USD at spot) — **since the journal began** (the date is shown), restored when a bot restarts |
| Account & collateral | collapsible (remembered per browser; folded, it keeps IM vs limit per bot, worst MM and warning badges in one line) — the shared Deribit account, as Deribit reports it: margin model (cross/segregated · portfolio/standard), cross collateral on/off, USD totals (equity, margin balance, IM, MM) with **IM % / MM % bars** (amber from 50 %, red from 80 %; Deribit liquidates at MM 100 %), and per asset: balance, equity, margin balance, available, withdrawable, IM and MM (and projected, without the nearest expiry), IM % / MM %, reserved. Flags STALE data, a failed poll and high margin. **Margin policy per bot**: IM used vs the active limit and *why* (DVOL band, or negative gamma), MM used vs its limit, whether new entries are allowed or FROZEN (with the reason), and any pending change with its countdown (e.g. *DVOL → band ≥70: 1 of 2 daily closes*), and beside the Rebalance button an amber *IM over limit since HH:MM:SS — review with a dry run* badge while the bot reports `risk.im_over_limit` (IM above the limit with no rebalance due; the bot resizes nothing by itself). The limits are drawn as ticks on the IM / MM bars |
| Open positions | one row per bot + slot + strike — positions on the same strike (partial fills, rebalance top-ups) are merged, marked **×n**, with summed qty and P&L, qty-weighted entry and the first stop to fire — instrument, slot (DTE · Δ), call/put, **side** (SELL = short, premium collected; BUY = long, premium paid — e.g. an iron-condor wing), strike, DTE, qty, entry, mark, unrealized P&L (coin and USD), **P&L %** (unrealized P&L as % of the premium: +50% = half the premium is profit, −100% = loss equal to the premium; the stop-loss fires at −200% with `stop_loss_multiplier: 2`), stop-loss mark, ITM/ATM/OTM and distance, Δ Γ Θ Vega. Filter by bot, strategy, type, moneyness; group by strategy, slot, expiry or type; each group header totals unrealized P&L (coin and USD), **P&L %** (group P&L ÷ group premium, in USD) and the **position Greeks** (each leg's per-option Greek × qty, negative for shorts — so the header is on a different scale than the per-option Greeks in the rows; Δ and Γ blank when a group mixes BTC and ETH). **Close** button per row (manual buy-back at market): active only while the bot's regime-side block covers that leg's side — a confirmed negative gamma regime with puts in a bear trend or calls in a bull trend; otherwise gray, with the bot's reason on hover. A confirmation window shows the cost at the ask, the confirmed and live gamma regime, the flip ±1σ and the trend, and what the bot does next — **red** when the close would be rebuilt (the strangle's last leg: its slot is re-entered). Needs `BOT_ADMIN_TOKEN` on the monitor and the bots; the proxy adds it server-side. A manager or an admin only (Roles, below): a user gets 403 |
| Missing legs | collapsible — one row per one-legged strangle (`/api/status` → `held_legs`, built by the bot from the same gate chain repair runs): the rule holding the repair now (regime side, stop hold, GEX anchor, GEX shedding, MM limit, …) and its reason; the confirmed regime, the bot's SMA trend and the reference script's bias (spot vs flip, negative regime); the **live flip + buffer** next to the **anchor flip + buffer** a GEX-shed leg waits on (snapshots counted, release hours), each with spot's distance to it; and the stop cooldown's end |
| Not managed by the bot | every other position on the account, of any kind (a hand-bought hedge, a future, …): side, size, average price, mark, P&L, Δ and its IM / MM, as Deribit reports them. No bot trades or sizes these, but they use margin, so they are never hidden. Collapsible; says "unavailable" (not "none") when the list could not be read |
| Trade history & profitability | every open and close since the journal began: realized P&L (USD at each close, and per coin), closes, win rate, average win vs average loss and their ratio, P&L by exit reason and by slot (worst first), and the list itself (time, bot, instrument, slot, qty, price, reason **with the rule and numbers that fired it** (e.g. "leg balance: call 262 vs put 162 … not a stop"), the option's **Δ (position Δ = −Δ × qty) · Θ · Vega at that moment**, P&L coin/USD, P&L %, days held, spot · DVOL at that moment). The activity feed shows the same reason and Δ on each close with filters by bot, open/close and exit reason. Collapsible |
| Activity | live feed of orders sent, re-priced, cancelled, opened, closed (take-profit, rolls, stop-loss, GEX shed, kill switch) skipped entries with the reason, and margin-policy changes (entries frozen/unfrozen, IM limit changed, rebalance, MM breach — with DVOL, IV percentile and regime), and **SMA trend changes** (`trend_changed`: old → new trend, the condition that fired, spot, SMA9, SMA21 and the swings — once per change, under either direction source), each with its market context (spot, DVOL, moneyness, open interest, regime) |
| P&L chart | total and realized P&L across bots (USD), plus one dashed line per bot (BTC, ETH) when several run, so each coin's contribution is visible. **Live** = the last 15 minutes of stored history, then this session every refresh (a reload never starts empty); **1h · 6h · 1d · 1w · 1m · All** = the bots' stored history (survives restarts), about 300 points per range |
| P&L in coin | each bot's realized and total P&L **in its own coin** (BTC, ETH), net of fees, from the same stored history: the coin's own price moves are left out, so it shows whether the strategy is growing the coin stack. One chart per bot (coins cannot be summed); **1h · 6h · 1d · 1w · 1m · All** |
| Working orders | entry/repair orders still on the book: fill progress, limit, time left before the fill timeout, age |

**The panels follow the window** (CSS only, no resize listeners): on wide screens (`lg` and up) the Activity panel is as tall as its row — the P&L charts and working orders beside it, and never less than 20rem or 70 % of the window — and its list scrolls inside it; the P&L charts scale with the window height between a minimum and a maximum (`clamp()` on `vh`), and so does the rebalance dialog's action list. On narrow screens the panels stack and the Activity list keeps a bounded height (60 % of the window, at least 20rem) with its own scroll bar. What breaks at phone and tablet widths (360, 390, 768 px), touch-target sizes and the contrast of every token pair are in the [responsive audit](docs/responsive-audit.md) (#207), the input of E12's prototype.

**Every P&L on the page is net of Deribit fees** (opening fees on open legs; opening share + closing fee on closes). Closes journaled before fees were recorded (2026-10-06) are gross. Counts, realized P&L, the activity feed and the trade history are **restored from each bot's journal** (`orders.log`) when it starts, so a restart of a bot or of the monitor loses nothing. The P&L chart's ranges come from each bot's `data/pnl_history.jsonl` (one point every 60 s, kept for a year); its realized line is the journal's, so the chart, the KPIs and the trade history agree. To start a fresh history, see `--reset-history` in `docs/operations.md`. Other history (positions, events, change markers) is a later phase of the [dashboard spec](../.claude/skills/option-bot-specialist/references/dashboard.md).

## How it works

```
browser ──1 s polls──▶ Next.js server ──read-only proxy──▶ bot API (Go, internal/api)
                       /api/bots/<bot>/<endpoint>           /api/status · positions · orders · pnl · pnl/history · events · account · trades · summary
```

- Each bot exposes a read-only JSON API when `BOT_API_ADDR` is set. It is built from the bot's in-memory state — **no Deribit calls, no order endpoints**.
- The browser talks only to the Next.js server, which forwards an allow-list of GET endpoints (`lib/bots.js`). Bot URLs are configured with `BOT_APIS` and never reach the browser.
- Events are fetched incrementally (`/events?since=<seq>`); a bot restart is detected and the feed resumes.
- Every item of `/api/events` and `/api/trades` carries `plain` (#201): one sentence in everyday words — what happened, why, what follows — from the bot's catalogue (`internal/api/explain.go`; the same words as `last_action.reason`). A journal value the catalogue does not know yet gets a generic sentence beginning "The bot recorded a step that has no plain-words explanation yet". Not shown on screen yet (a later UI story); every other field is unchanged.
- `GET /api/summary` (#177; in the proxy allow-list since #246, readable by any signed-in role, shown as the investor summary) answers a holder's four questions in one call. Coin amounts are in `underlying` (BTC or ETH), USD at today's `spot` unless said otherwise:
  - `holding` — `strangles`, `legs`, `calls`, `puts`, `notional_coin`, `notional_usd`, `nearest_expiry` and `nearest_dte` (null with no legs), `coin`.
  - `income` — `months`: the last 6 UTC calendar months, oldest first, the current one last, each `{month: "YYYY-MM", realised_coin, realised_usd, closes}` (journal closes net of fees; USD at each close's spot; a month without closes is zeros); `this_month` (= the last of `months`); `unrealised_coin`, `unrealised_usd` (open book at its marks); `coin`; `basis`.
  - `worst_case` — **a model, label it so**: `model` (the text to show), `available` (false with `reason` when spot or DVOL is missing — show "unknown", never 0), `sigmas` (3), `dvol`, `move_pct`, `coin`, and `down` / `up` each `{spot, loss_coin, loss_usd, pct_of_equity}` (positive = money lost; `pct_of_equity` null without the equity). Black–Scholes on each leg's current IV, one day passed, spot moved ±3 daily σ (σ = DVOL ÷ √365); `loss_coin` is the USD loss at the moved spot.
  - `last_action` — null before the first trade, else the journal's latest open or close: `at`, `kind` (open | close), `instrument`, `option_type`, `qty`, `rule` (the journal's reason, e.g. `stop_loss`), `reason` in plain words (e.g. "Bought back the put: it lost twice its premium (stop-loss)"), `detail` (the rule's numbers), and for a close `pnl_coin`, `pnl_usd`.
- **Every page and every `/api` route needs a signed-in session** (below). The bots' APIs know nothing about it: they stay unpublished on the compose network and the monitor reaches them as before (with `BOT_ADMIN_TOKEN` for close and rebalance).

## Sign-in

People sign in at `/login` with a username and password; `proxy.js` sends every page without a session to `/login?next=…` and answers 401 on every `/api` route except `/api/auth/login` and `/api/auth/refresh`. The close and rebalance routes check the person again themselves (Roles, below).

### Roles (#171, PRD §3)

| Action | admin | manager | user |
|---|---|---|---|
| See the monitor | ✓ | ✓ | ✓ |
| Manual close, manual rebalance (and its dry run) | ✓ | ✓ | — (403) |
| Create, edit and delete users; reset a password | ✓ | — | — |

`admin` satisfies `manager` satisfies `user` (`lib/auth/roles.js`). Checked on the server, in the route handlers — not by hiding buttons (a user still sees them; the route answers 403 `manager role required` and the bot receives nothing). **State-changing routes re-read the person from the database** (`requireRole` in `lib/auth/server.js` → `AuthService.actingUser`): the user must still exist, their **current** role must allow the action (else 403), and the access token's session (`sid`, its refresh token's SHA-256) must still be live (else 401). So a demoted manager is refused at once, and a deleted user or one signed out everywhere too — not 15 minutes later. Read routes trust the token. An access token from before #171 has no `sid`: its first close answers 401, the page refreshes into a new token and retries.

The route sends the person to the bot with the request — `"actor": {"user": "bob", "role": "manager"}` beside `BOT_ADMIN_TOKEN` — and the bot logs and journals it (the close's `actor` and `detail` "manual close from the monitor by bob (manager)", `rebalance_manual`'s `actor`). User management is in `AuthService` (`changeRole`, `deleteUser`, `signOutEverywhere`: never zero admins, never delete yourself, every change ends that user's sessions); its Users page and `/api/admin/users` routes come with the page's own story.

| Part | How |
|---|---|
| Users | SQLite (`sql.js`, WebAssembly — no native build in the image) in `data/monitor/auth.db` (`AUTH_DB_PATH`), a `monitor-auth` Docker volume per environment: stable and mvp have separate users. Table `users`: username (3–64 of `a-z 0-9 . _ -`, case-insensitive), argon2id hash, role `admin`/`manager`/`user` (enforced: Roles, above), email |
| Passwords | argon2id (`hash-wasm`; 19 MiB, 2 passes), at least 12 characters. An unknown user is checked against a dummy hash, so it takes as long as a wrong password and gets the same 401 `invalid username or password` |
| Access token | JWT HS256 (`jose`), 15 min, cookie `access_token` (HttpOnly, SameSite=Lax, path `/`), signed with `AUTH_SECRET`; carries the user id, name, role and `sid` (its session) |
| Refresh token | 32 random bytes, only its SHA-256 stored, 14 days, cookie `refresh_token` on path `/api/auth`; reusable until logout (several tabs). A password change, a role change or a deletion ends every session of that user |
| Silent refresh | `lib/api.js` `apiFetch`: the monitor's own 401 (header `x-monitor-session: required`) → one refresh shared by every waiting request → one retry; when that fails the page goes to `/login` and comes back afterwards. A bot's 401 passed through a proxy route is shown as an error, never a sign-out |
| Lockout | 5 failed sign-ins for a username within 15 min lock it until 15 min after the last one (same 401); 20 sign-ins a minute per IP, then 429 with `Retry-After`. In memory: a restart of the monitor forgets it |
| Secure cookies | `Secure` except on plain http to `localhost`/`127.0.0.1` (the compose port), where a browser would drop them. Opened from another machine over plain http, sign-in cannot work: put TLS in front first |
| Service token | `MONITOR_API_TOKEN` (≥ 32 chars) as `Authorization: Bearer …` reads `GET /api/bots/*` without a session — the deploy health check, `promote.sh`, the SME agent and `stress_inputs.sh` use it (`env_monitor_token` in `scripts/env_lib.sh`). Never a page, a close or a rebalance |
| Logs | one JSON line per sign-in, failure (reason, `locked`), lockout, rate limit, sign-out, first admin, user change and refused action, with username and IP — never a password, hash, token or `AUTH_SECRET` |
| Audit | `data/monitor/audit.jsonl` beside the user database (`AUDIT_PATH`; off for an in-memory database), append-only, owner-only: one JSON line `{time, action, username, role, …}` per `signed_in`, `sign_in_failed` (reason), `locked`, `signed_out`, `user_created`, `role_changed`, `user_deleted`, `signed_out_everywhere` (with `target`) and `manual_close` / `manual_rebalance` (bot, position ids or dry run, the bot's status and, for a close, what it filled). Never a password, hash or token |

The first start with no users creates an admin from `ADMIN_USERNAME` / `ADMIN_PASSWORD` / `ADMIN_EMAIL`; later starts never touch existing users. The server exits at start (`instrumentation-node.js`) when `AUTH_SECRET` is missing or shorter than 32 bytes, or the database cannot be opened.

## Analytics (#193)

Usage events from the event plan (`docs/product/analytics/event-plan.md`) — what a person looked at or did, **never an amount, size, price, instrument, secret or email**. Nothing leaves the machine: no third-party tracker, SDK or script.

- **Sender:** `lib/track.js` `track(event, props)` posts to the monitor's own `POST /api/events`, without waiting and without ever throwing. It sends nothing until `setAnalyticsConsent(true)`; "no" or not asked yet (the default) means nothing at all is sent. Events outside the plan are dropped in the page too. The session id is random (32 hex), new after 30 min without an event. No component calls it yet, and nothing sets the consent: the consent notice waits for E13's prototype.
- **Allow-list:** `lib/analytics/plan.js` — each event and the only properties it may carry (enums, flags, capped millisecond counts, help catalogue keys of letters and `_` only, so no number fits). `__tests__/events-plan.test.js` keeps it equal to the plan's tables: change the plan first.
- **Collector:** `app/api/events/route.js` stores an event only for a signed-in person (re-read from the database), same-origin JSON ≤ 2 KB, whose `users.analytics_consent` is yes (403 otherwise) and when `ANALYTICS_SALT` (≥ 16 chars) is set (503 otherwise). An unknown event, a property not in the plan or a value outside its rule is refused whole: 400, nothing written, a WARN log naming the field, never its value. 204 when stored.
- **Stored:** one JSON line per event in `data/monitor/events.jsonl` beside `auth.db` (`EVENTS_PATH`; off for an in-memory database), append-only, owner-only: `event`, `ts` (UTC, to the second), `user_id` (HMAC-SHA256 of the user's row id with `ANALYTICS_SALT`, 16 hex), `session_id`, `role`, `environment` (`stable`|`mvp`, from `MONITOR_DEPLOYMENT`), `app_version` (`MONITOR_VERSION`) and the event's properties. Never the username, email, IP, user agent or URL. Not read by the bot; retention (13 months) and deletion on request are follow-ups.

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

**Usability test (E3, #202):** [`docs/usability/e3-investor-monitor-test.md`](../docs/usability/e3-investor-monitor-test.md) — the 5-user, 4-question, 5-minute test of the investor view on this demo data (baseline, prototype, release), with its answer key, scoring and anonymised templates.

**Against locally running bots:** start each bot with `BOT_API_ADDR=127.0.0.1:8081` (BTC) / `127.0.0.1:8082` (ETH), then `npm run dev`.

| Variable | Default | Meaning |
|---|---|---|
| `BOT_APIS` | `btc=http://127.0.0.1:8081,eth=http://127.0.0.1:8082` | comma-separated `name=url` of each bot API (server side only) |
| `MONITOR_POLL_MS` | `1000` | how often the page refreshes (500–60000 ms). Polling reads the bots' memory only — it never uses Deribit rate limits |
| `AUTH_SECRET` | — (required) | signs the access tokens; ≥ 32 bytes, from the environment's `.env`, never in git |
| `ADMIN_USERNAME`, `ADMIN_PASSWORD`, `ADMIN_EMAIL` | — | the first admin, created only while there are no users |
| `MONITOR_API_TOKEN` | — (off) | read-only service token for scripts (≥ 32 chars) |
| `AUTH_DB_PATH` | `data/monitor/auth.db` | the user database |
| `AUDIT_PATH` | `audit.jsonl` beside `AUTH_DB_PATH` | the audit trail |
| `ANALYTICS_SALT` | — (off) | salt of the analytics `user_id` (≥ 16 chars), from `.env`, never in git or an event |
| `EVENTS_PATH` | `events.jsonl` beside `AUTH_DB_PATH` | the analytics events |
| `MONITOR_DEPLOYMENT`, `MONITOR_VERSION` | — | the environment (`stable`/`mvp`) and release tagged on each event (compose sets them) |

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
| `lib/auth/` | server-side sign-in: `store.js` (SQLite), `password.js` (argon2id), `tokens.js` (JWT, cookies), `limits.js` (lockout, per-IP), `roles.js` (role order), `audit.js` (audit.jsonl), `service.js` (login/refresh/logout, the acting user, user changes), `server.js` (the shared instance, `requireRole`) |
| `components/` | one panel per file: `Header`, `KpiStrip`, `Filters`, `PositionsTable`, `PendingOrders`, `ActivityFeed`, `PnlChart`, `AccountPanel`, `Monitor` (+ `Panel`, `Badge`, `StatTile`, `MarginBar`) |
| `hooks/` | `useMonitor` — polling, incremental events, staleness, session history |
| `lib/` | `api.js` (the only fetching module), `monitor.js` (pure grouping/summaries/event text), `format.js` (units), `bots.js` (server config) |
| `content/` | `glossary.json` — the plain-words tips (below) |
| `__tests__/`, `test/` | tests and shared fixtures |

### Glossary (#188)

`content/glossary.json` holds one short tip for every term the monitor shows and every config setting: `{ "<term>": { "short" (≤ 140 chars), "long" (≤ 600), "risk_income" (how it moves risk and income), "source" ("OptionStrategy.md#<anchor>" or "frontend/README.md#<anchor>"), "labels" (optional: the exact monitor texts that mean this term) } }`. Config keys are entries under their YAML name, nested ones as `parent.child` (`backtest.fill_model`). `lib/glossary.js` `findTerm(label)` finds an entry by term, config key or label. It is content only: the tips UI (E11) and the assistant (E10) will read it.

Tests keep it complete: `__tests__/glossary.test.js` fails when a literal column header (`<th>`), `<dt>`, `label="…"` prop, hold-rule, feed-event or exit-reason label in the monitor, or a key of `config_btc.yaml`/`config_eth.yaml`/`config_btc.mvp.yaml`, has no entry, when a source anchor does not exist, and on return promises; `tests/glossary_test.go` checks every key `config.Load` accepts. **A new column or setting needs its entry in the same PR.** Writing rules: short sentences (8th-grade reading), no jargon without its own entry, no return promises.

Stack: Next.js (App Router) · Tailwind CSS v4 with design tokens in `app/globals.css` (`profit`, `loss`, `call`, `put`, `live`, `testnet`…) · Recharts · clsx.

## Rules of the road

- **Read-only.** Anything that would change trading state (pause, kill switch, parameter edits) needs authentication, an explicit confirmation step and an audit entry, and must reuse the bot's existing code paths — it is a planned phase, not part of this page.
- **Never expose it publicly.** It shows positions and P&L; Docker binds it to `127.0.0.1`. Sign-in protects it from anyone who reaches the port, but it is no substitute for TLS and a closed port.
- Every number carries its unit (BTC/ETH, USD, %); missing data shows "—", never 0; stale data is flagged.
- No business logic in Next.js routes — the Go bot is the single backend. The one exception is the monitor's own users and sessions (`lib/auth/`).
- **Times: stored in UTC, shown in BRT.** The bot, its journal, history files and API are UTC; the page converts only for display, in one place (`lib/time.js`, `America/Sao_Paulo`, UTC−3). Change the zone there, never in the data.
