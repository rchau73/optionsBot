# Multi-strategy monitor — single-page dashboard spec

This is the specialist's spec for the developer skill to build (Next.js App Router + Tailwind + Recharts, read-only Go `internal/api` by default — see the developer skill). It is one page, dense like a trading-desk blotter, dark mode first. Every number shows its unit (BTC/ETH/USD/%) and its age; stale data is visibly stale, never a silent zero.

## Contents
1. Core idea: strategy as a first-class dimension
2. Data sources and modes (backtest / testnet / live)
3. Page layout
4. Drill-down / drill-up hierarchy
5. Filters and grouping
6. Panels in detail
7. Timeline, history and change audit
8. Controls (setup & tuning)
9. API shape the page needs
10. Build order

---

## 1. Core idea: strategy as a first-class dimension

Multiple strategies run in parallel (e.g. `strangle-16d`, `condor-oi-pin`, `call-spread-gex`). Each has its own monitor, but they share one page so the book is always visible as a whole.

- Every strategy has a stable **`strategy_id`**, a **version** (hash of its parameters) and a **mode**: `backtest`, `testnet` or `live`.
- Every order carries the strategy in its Deribit order **`label`** (e.g. `condor-oi-pin:v3:st-42`), so fills, P&L and Greeks can be attributed per strategy straight from the exchange — this also lets restart reconciliation reassign positions to the right strategy.
- Everything on the page — tables, charts, KPIs — can be cut by `strategy_id`, and a strategy's backtest runs and its testnet run appear side by side, so "does testnet behave like the backtest?" is one glance.

## 2. Data sources and modes

| Mode | Source | Refresh |
|---|---|---|
| Live / testnet | Go API reads bot state (`StateManager`, pending orders, GEX snapshot, market data) and `orders.log` | poll every 2–5 s (SSE later only if needed) |
| Backtest | `data/results/<run_id>/` (summary.json, trades.csv, equity_curve.csv, drawdown.csv, walk_forward/) | on demand |

The UI never calls Deribit directly; the bot is the single source of truth and the API never adds load to the trading gateway's high-priority queue.

## 3. Page layout (single page, top to bottom)

```
┌───────────────────────────────────────────────────────────────────────────────┐
│ HEADER  env badge [TESTNET]  BTC 101,234 ▲  DVOL 54 (p62)  GEX +PIN flip 97k │
│         last update 2s ago ● · kill-switch state · filters bar (§5)          │
├───────────────────────────────────────────────────────────────────────────────┤
│ KPI STRIP (respects filters):  P&L today | P&L total | Max DD | Margin used/cap│
│                                Net Δ | Γ | Vega | Θ/day | open structures     │
├──────────────────────────────────────────┬────────────────────────────────────┤
│ STRATEGY BOARD (one row per strategy)    │ RISK MAP                           │
│ id · mode · status · P&L · DD · Sharpe · │ spot vs short strikes per expiry,   │
│ margin · Δ Γ V Θ · open · last action    │ gamma walls, flip, OI bars          │
│ ▸ click row = drill down                 │ (hover = strike detail)             │
├──────────────────────────────────────────┴────────────────────────────────────┤
│ CHARTS (tabs): Equity & drawdown │ P&L attribution │ Greeks over time │        │
│                Backtest vs testnet overlay │ Exit reasons │ IV/regime         │
├───────────────────────────────────────────────────────────────────────────────┤
│ DRILL TABLE (changes with level, §4): breadcrumb  Book › condor-oi-pin › st-42 │
│ grouped, sortable, filterable rows; expand ▸ to drill, breadcrumb to drill up │
├───────────────────────────────────────────────────────────────────────────────┤
│ EVENT TAPE: fills, rolls, stops, GEX actions, errors (filterable, newest top) │
└───────────────────────────────────────────────────────────────────────────────┘
```

Mobile: KPI strip and strategy board stack; charts become swipeable; the drill table keeps the first two columns pinned.

## 4. Drill-down / drill-up hierarchy

```
Book (all strategies, one underlying or both)
 └─ Strategy (strategy_id, mode)
     └─ Slot / expiry group (e.g. 45 DTE · 0.16Δ · 27DEC26)
         └─ Structure (strangle / condor / spread instance, st-42)
             └─ Leg (BTC-27DEC26-110000-C, short 0.3)
                 └─ Orders & fills (submit → amends → fill/cancel, with prices vs mid)
```

- **Drill down:** click a row (or a chart series/bar) to filter everything on the page to that node.
- **Drill up:** breadcrumb at the top of the drill table; `Esc` goes up one level.
- KPIs and charts always reflect the current node, so the same page answers "how is the book?" and "why did leg X stop out?".
- The current node, filters and grouping live in the URL query string, so a view can be bookmarked or shared.

## 5. Filters and grouping

Filters bar (chips, combinable): underlying · strategy · mode (backtest/testnet/live) · backtest run · status (open/closed/pending) · structure type · option type (call/put) · expiry / DTE range · delta range · exit reason · date range · GEX regime · IV-percentile band.

Group-by (drill table and attribution chart): strategy · expiry · DTE bucket (0–7, 8–14, 15–30, 31–60, 60+) · delta bucket · option type · exit reason · regime · week/month.

Each group row shows count, qty, P&L (coin and USD), win rate, avg win / avg loss, worst trade, and net Greeks, with sparklines where useful.

## 6. Panels in detail

**Strategy board** — status dot (running / paused / halted by kill switch / error), mode badge, version, P&L today/total, max DD, Sharpe (rolling 30d), margin used, net Greeks, open structures, last action time. Red/green is never the only signal: use signs and icons.

**Risk map** — x-axis strike, one row per active expiry: spot line, short strikes (markers sized by qty), long wings, OI bars (calls up, puts down), gamma flip, call/put walls. Shows at a glance how close each short leg is to being tested.

**Charts**
- Equity curve with drawdown underneath (shared x-axis); overlay backtest vs testnet for the same strategy version.
- P&L attribution: stacked bars by group-by dimension.
- Greeks over time: net delta (with hedge band), gamma, vega, theta.
- Exit reasons: counts and P&L per reason (take-profit, roll, stop, GEX close, kill switch).
- IV / regime: DVOL and IV percentile with GEX regime shading; entries/exits as markers.

**Drill table** — columns adapt per level (leg level shows strike, type, qty, entry, mark, P&L, delta, DTE, distance to strike %, stop level, next rule likely to fire). Sortable, sticky header, pagination or virtualisation beyond a few hundred rows.

**Event tape** — every fill, amend, roll, stop, GEX action, reconnect, rejection; filter by severity; clicking an event drills to its leg.

## 7. Timeline, history and change audit

The monitor is **timeline-first**: every number can be read *as of a moment* and every chart shares one time axis, so behaviour can be compared across time, across modes and across strategy versions.

### 7.1 One time axis for everything
- A global **time-range selector** (presets: 24h, 7d, 30d, 90d, YTD, all, custom) plus a **brush** on the main equity chart; every chart and table follows it.
- A **synced time cursor**: hovering any chart shows the same timestamp on all charts, and the drill table can be switched to "as of cursor" (positions, Greeks and margin at that moment).
- Times in UTC by default (Deribit settles at 08:00 UTC), with a local-time toggle.

### 7.2 Building history (backtest side)
- Backtests produce the longest history: each run is stored with its strategy version, parameter set, data source and period, so any past window can be replayed and compared.
- **Backfill** what Deribit serves historically: underlying candles (`public/get_tradingview_chart_data`), DVOL (`public/get_volatility_index_data`), trades and settlement prices for expired instruments where available.
- Some inputs have **no historical API** (notably open interest per strike and full order-book depth). Start a **collector** now that snapshots them on a schedule (e.g. OI every 15 min, top-of-book every minute for traded strikes), so pinning/spread strategies gain a real history over time. Label any dataset that is synthetic or partially backfilled.

### 7.3 Storing live/testnet data for future comparison
- The bot persists an **append-only time series** of its own state, independent of logs: per strategy, every N seconds (e.g. 60 s) — equity/P&L (coin and USD), margin, net Greeks, open structures, spot, DVOL/IV percentile, GEX regime/flip; plus every event (submit, amend, fill, cancel, roll, stop, GEX action, reconnect, error).
- Storage is the developer's choice under the developer skill's rules (no extra infrastructure): an embedded store such as SQLite or day-partitioned JSONL under `data/`, with retention and a size budget. Restarts must not lose history; reconcile gaps are shown as gaps, not interpolated.
- The same schema serves backtest and live rows (`mode`, `run_id`, `strategy_id`, `version`, `ts`), so backtest vs testnet vs live overlay is a query, not a special case.

### 7.4 Audit trail of strategy changes
- **Every change is an audit event**: parameter edits from the UI, config-file changes detected at startup (hash of the effective config differs from the last run), code deploys (git commit / build version), kill-switch use, pause/resume, manual interventions.
- Each event records: timestamp, who/what (UI user, config file, deploy), strategy, old → new version, the **diff** of parameters, and a free-text **reason** (required for UI changes).
- Versions are immutable: a strategy at `v4` always means the same parameter set.

### 7.5 Change markers on every chart
- Vertical markers on all time charts at each audit event, coloured by type (parameter change, deploy, config change, manual action, incident) and labelled (`v3 → v4`). Hover shows the diff and reason; click drills into the event.
- Background bands show which **version** was active over each period, and regime bands (GEX, IV percentile) can be toggled on, so "the change" is not confused with "the market changed".

### 7.6 Before/after impact analysis
- Select a change marker → an **impact panel** compares equal-length windows before and after: P&L/day, drawdown, win rate, avg win/loss, exits by reason, slippage, Greeks exposure, margin usage, trade count.
- Show the regime mix of both windows next to the comparison and flag when it differs a lot (the market, not the change, may explain the result).
- Counterfactual: run the backtest of the *old* and *new* versions over the same window and overlay them, so the effect of the change is isolated from the market.
- Small samples are labelled as such (e.g. fewer than 20 trades per window) instead of presenting noise as a finding.

## 8. Controls (setup & tuning)

Phase 1 is read-only. Phase 2 adds controls, behind authentication, explicit confirmation and an audit log, applied to **testnet first**:
- Strategy setup: create from a template (playbook structure), set slots, deltas, DTE, exits, risk limits; validation mirrors `config.Validate`.
- Parameter tuning: edit → see a diff (old vs new) and the backtest result of the new version before applying → apply to testnet.
- Run control: pause/resume a strategy (stops new entries, keeps managing exits), kill switch per strategy and for the book (uses the bot's existing kill-switch path).
- Backtest lab: launch a sweep for a strategy version, compare runs, promote a parameter set.

## 9. API shape the page needs (read-only, phase 1)

```
GET /api/strategies                          → id, mode, version, status, KPIs, Greeks
GET /api/positions?strategy=&group_by=&…     → tree rows for the drill table
GET /api/orders?strategy=&leg=&since=        → order lifecycle and fills
GET /api/metrics/equity?strategy=&run=       → equity + drawdown series
GET /api/metrics/attribution?group_by=&…     → grouped P&L
GET /api/market                              → spot, DVOL, IV pct, GEX snapshot, OI by strike
GET /api/backtests ; /api/backtests/{run_id} → runs and their results
GET /api/events?since=&severity=             → event tape
GET /api/timeseries?strategy=&mode=&run=&from=&to=&metrics=&step=  → time-bucketed metrics
GET /api/audit?strategy=&from=&to=           → change events with diffs and reasons
GET /api/audit/{event_id}/impact?window=     → before/after comparison for one change
GET /api/strategies/{id}/versions            → immutable parameter sets per version
```
Every response carries `as_of` (timestamp) and units; empty states are explicit (`[]` + reason), never fabricated zeros. Time-series endpoints accept `from`/`to`/`step` so the browser never downloads raw history.

## 10. Build order (suggested Research/Dev requests)

1. Tag orders with `strategy_id` labels; version strategies (hash of parameters); record audit events (startup config hash, deploy version, kill switch).
2. Persist the time series and events (§7.3) and start the OI/order-book collector (§7.2) — history only accrues from the day this ships, so it comes early.
3. Expose `/api/strategies`, `/api/positions`, `/api/market`, `/api/timeseries`, `/api/audit` — KPI strip, strategy board, drill table, equity/drawdown with change markers.
4. Attribution, backtest-vs-testnet overlay, impact panel, risk map, event tape.
5. Phase-2 controls (auth, confirmation, audit log with reasons), testnet only.
