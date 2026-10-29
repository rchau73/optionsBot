# optionsBot monitor (frontend)

A **read-only, single-page live monitor** for the bots: what each strategy holds, what it just did and how it is doing, refreshed every second.
All UI documentation lives here; the backend docs only describe the API.

> Educational study of an automated trading system — not investment advice.
> The monitor is meant for **Deribit testnet** first.

## Where to read

Each area has its own file under `docs/`, so a change to one area does not touch the others.

- [Menu and pages](docs/menu-and-pages.md): what each page, panel and chart shows
- [API routes](docs/api-routes.md): what the monitor's server reads from the bots
- [Sign-in](docs/sign-in.md): sessions, passwords, lockout, reset links, the service token
- [Users and roles](docs/users-and-roles.md): who may do what, and the Users page
- [Audit](docs/audit.md): the audit trail and its page
- [Analytics](docs/analytics.md): usage events, surveys, retention, and the Analytics page
- [Assistant](docs/assistant.md): the help assistant, its guard, its panel and its routes
- [Glossary and help content](docs/glossary-and-help.md): glossary, risk notes, empty states, videos
- [Testing](docs/testing.md): the viewport check and the accessibility baseline
- [Responsive audit](docs/responsive-audit.md): what breaks at phone and tablet widths

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

**Investor-summary scenarios** (#203) for design and user tests — `-scenario good|mixed|drawdown`, served through the real API code, nothing faked in the frontend: `good` steady monthly income (no stop-loss), `mixed` a losing month (every close two months back a stop-loss, negative in coin and USD) among winning ones, `drawdown` the same losing month, a thin current month, a book five times larger so the worst case now dwarfs the month's income, and the nearest put held at 2.6× its entry, within 20 % of its stop mark.
A scenario reports `deployment: "demo"` and `version: "scenario <name>"` in `/api/status` (the header's badge shows DEMO · scenario <name>) beside the demo's `testnet (demo)` environment, so it is never read as a track record.
Without `-scenario` the demo is unchanged.

```bash
go run ./cmd/monitor-demo -scenario mixed
go run ./cmd/monitor-demo -scenario drawdown -underlying ETH -addr 127.0.0.1:8082
```

Local runs need the sign-in variables too, e.g. in `frontend/.env.local` (git-ignored): `AUTH_SECRET=$(openssl rand -base64 48)`, `ADMIN_USERNAME=admin`, `ADMIN_PASSWORD=<12+ characters>`; the user database is then `frontend/data/monitor/auth.db` (git-ignored).

**Usability test (E3, #202):** [`docs/usability/e3-investor-monitor-test.md`](../docs/usability/e3-investor-monitor-test.md) — the 5-user, 4-question, 5-minute test of the investor view on this demo data (baseline, prototype, release), with its answer key, scoring and anonymised templates.

**Against locally running bots:** start each bot with `BOT_API_ADDR=127.0.0.1:8081` (BTC) / `127.0.0.1:8082` (ETH), then `npm run dev`.

- `BOT_APIS`
  - Default: `btc=http://127.0.0.1:8081,eth=http://127.0.0.1:8082`
  - Meaning: comma-separated `name=url` of each bot API (server side only)
- `MONITOR_POLL_MS`
  - Default: `1000`
  - Meaning: how often the page refreshes (500–60000 ms).
    Polling reads the bots' memory only — it never uses Deribit rate limits
- `AUTH_SECRET`
  - Default: — (required)
  - Meaning: signs the access tokens; ≥ 32 bytes, from the environment's `.env`, never in git
- `ADMIN_USERNAME`, `ADMIN_PASSWORD`, `ADMIN_EMAIL`
  - Default: —
  - Meaning: the first admin, created only while there are no users
- `MONITOR_API_TOKEN`
  - Default: — (off)
  - Meaning: read-only service token for scripts (≥ 32 chars)
- `AUTH_DB_PATH`
  - Default: `data/monitor/auth.db`
  - Meaning: the user database
- `AUDIT_PATH`
  - Default: `audit.jsonl` beside `AUTH_DB_PATH`
  - Meaning: the audit trail
- `ANALYTICS_SALT`
  - Default: — (off)
  - Meaning: salt of the analytics `user_id` (≥ 16 chars), from `.env`, never in git or an event
- `EVENTS_PATH`
  - Default: `events.jsonl` beside `AUTH_DB_PATH`
  - Meaning: the analytics events
- `MONITOR_DEPLOYMENT`, `MONITOR_VERSION`
  - Default: —
  - Meaning: the environment (`stable`/`mvp`) and release tagged on each event (compose sets them)
- `USAGE_REPORTS_DIR`
  - Default: `data/reports`
  - Meaning: the weekly usage reports per environment (`<dir>/<env>/<week>.json`) the Analytics page reads; compose mounts them read-only
- `ANTHROPIC_API_KEY`
  - Default: — (off)
  - Meaning: the assistant's Claude API key, from `.env`, written by the person; never logged or sent to the browser
- `ASSISTANT_MODEL`
  - Default: `claude-haiku-5-5`
  - Meaning: the assistant's model (any Claude model id; Haiku chosen for cost, #412)
- `ASSISTANT_LOG_PATH`
  - Default: `assistant.jsonl` beside `AUTH_DB_PATH`
  - Meaning: the assistant's review log
- `COMPARE_DIR`
  - Default: `data/compare` in the clone
  - Meaning: the comparison reports `cmd/compare` writes (`<UTC time>/report.json`), read-only through `/api/compare`

## Develop

```bash
npm run dev     # dev server with hot reload
npm test        # Jest + Testing Library
npm run lint    # ESLint (next/core-web-vitals)
npm run build   # production build — catches Server/Client Component mistakes
npx playwright install chromium   # once: the browser the phone check uses
npm run test:phone                # viewport check (#309, #425): every menu page + the assistant at 360, 768 and 1280 px on demo data
```

- `app/`
  - Contents: `layout.jsx`, `page.jsx` (composition only), `login/page.jsx`, `reset/page.jsx` (a reset link's form, public), `admin/users/page.jsx` (Users page), `admin/audit/page.jsx` (Audit page), `admin/analytics/page.jsx` (Analytics page), `api/bots/…` read-only proxy routes, `api/auth/{login,refresh,logout,password,reset}`, `api/admin/users/…`, `api/admin/audit` and `api/admin/analytics` (admins only)
- `proxy.js`, `instrumentation*.js`
  - Contents: the session guard on every route; the start-up check (`AUTH_SECRET`, first admin)
- `lib/auth/`
  - Contents: server-side sign-in: `store.js` (SQLite), `password.js` (argon2id), `tokens.js` (JWT, cookies), `limits.js` (lockout, per-IP), `roles.js` (role order), `audit.js` (audit.jsonl), `auditread.js` (reads it backwards for the Audit page), `service.js` (login/refresh/logout, password change, reset links, the acting user, user administration), `server.js` (the shared instance, `requireRole`), `admin.js` (what every admin route checks first), `viewer.js` (who is looking, for display only)
- `components/`
  - Contents: one panel per file: `Header`, `KpiStrip`, `Filters`, `PositionsTable`, `PendingOrders`, `ActivityFeed`, `PnlChart`, `AccountPanel`, `Monitor`, `DetailsPage` (+ `MicroBar`, `DotStrip`, `Panel`, `Badge`, `StatTile`, `MarginBar`, `Details`); the Users page: `UsersAdmin`, `AddUserForm`, `UserActionDialog`, `ResetLinkDialog`; `ResetPasswordForm` (`/reset`); `AuditTrail` (Audit page)
- `hooks/`
  - Contents: `useMonitor` — polling, incremental events, staleness, session history
- `lib/`
  - `api/`: the only fetching modules, one file per domain (#466) — `bots.js`, `actions.js`, `auth.js`, `users.js`, `admin.js`, `assistant.js`, `survey.js`, `notifications.js` — over the shared fetch and errors of `api/http.js`.
    Callers import the domain file; there is no barrel.
  - Contents: `monitor.js` (pure grouping/summaries/event text), `users.js` (Users page form checks and confirmation words), `audit.js` (the audit actions, the Audit page's query rules and detail column), `format.js` (units), `bots.js` (server config)
- `content/`
  - Contents: `glossary.json` — the plain-words tips (below); `videos.json` — the how-to video catalogue (#300): `video.<task>` → title, task, `seconds` (30–90), `captions` path (may not exist while `status` is `planned`), screen, storyboard (`docs/product/education/video-storyboards.md`); the analytics collector accepts a `video_id` only if it is a key here and a `tip_id` only if it is `tip.` + a glossary key (lower case, other characters as `_`); `confirm-notes.json` — the risk notes before a confirmation (below); `empty-states.json` — what an empty, loading, stale or failing panel says (below); `help-index.json` — generated, never hand-edited: the E10 assistant's only corpus, OptionStrategy.md, this glossary and the bot's plain-words catalogue as cited passages (`go run ./cmd/helpindex` after a change to any of them, #312)
- `__tests__/`, `test/`
  - Contents: tests and shared fixtures
- `e2e/`
  - Contents: the viewport check (Playwright, `npm run test:phone`): config, sign-in fixture, the pages from the menu, in-page measuring, baseline comparison

Stack: Next.js (App Router) · Tailwind CSS v4 with design tokens in `app/globals.css` (`profit`, `loss`, `call`, `put`, `live`, `testnet`…) · Recharts · clsx.

## Rules of the road

- **Read-only.** Anything that would change trading state (pause, kill switch, parameter edits) needs authentication, an explicit confirmation step and an audit entry, and must reuse the bot's existing code paths — it is a planned phase, not part of this page.
- **Never expose it publicly.** It shows positions and P&L; Docker binds it to `127.0.0.1`.
  Sign-in protects it from anyone who reaches the port, but it is no substitute for TLS and a closed port.
- Every number carries its unit (BTC/ETH, USD, %); missing data shows "—", never 0; stale data is flagged.
- No business logic in Next.js routes — the Go bot is the single backend.
  The one exception is the monitor's own users and sessions (`lib/auth/`).
- **Times: stored in UTC, shown in BRT.** The bot, its journal, history files and API are UTC; the page converts only for display, in one place (`lib/time.js`, `America/Sao_Paulo`, UTC−3).
  Change the zone there, never in the data.
