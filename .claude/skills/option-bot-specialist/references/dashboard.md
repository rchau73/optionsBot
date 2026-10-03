# Multi-strategy monitor — single-page dashboard spec

This is the specialist's spec for the developer skill to build (Next.js App Router + Tailwind + Recharts, read-only Go `internal/api` by default — see the developer skill). It is one page, dense like a trading-desk blotter, dark mode first. Every number shows its unit (BTC/ETH/USD/%) and its age; stale data is visibly stale, never a silent zero.

## Contents
1. Core idea: strategy as a first-class dimension
2. Data sources and modes (backtest / testnet / live)
3. Page layout
4. Drill-down / drill-up hierarchy
5. Filters and grouping
6. Panels in detail
7. Controls (setup & tuning)
8. API shape the page needs
9. Build order

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

## 7. Controls (setup & tuning)

Phase 1 is read-only. Phase 2 adds controls, behind authentication, explicit confirmation and an audit log, applied to **testnet first**:
- Strategy setup: create from a template (playbook structure), set slots, deltas, DTE, exits, risk limits; validation mirrors `config.Validate`.
- Parameter tuning: edit → see a diff (old vs new) and the backtest result of the new version before applying → apply to testnet.
- Run control: pause/resume a strategy (stops new entries, keeps managing exits), kill switch per strategy and for the book (uses the bot's existing kill-switch path).
- Backtest lab: launch a sweep for a strategy version, compare runs, promote a parameter set.

## 8. API shape the page needs (read-only, phase 1)

```
GET /api/strategies                          → id, mode, version, status, KPIs, Greeks
GET /api/positions?strategy=&group_by=&…     → tree rows for the drill table
GET /api/orders?strategy=&leg=&since=        → order lifecycle and fills
GET /api/metrics/equity?strategy=&run=       → equity + drawdown series
GET /api/metrics/attribution?group_by=&…     → grouped P&L
GET /api/market                              → spot, DVOL, IV pct, GEX snapshot, OI by strike
GET /api/backtests ; /api/backtests/{run_id} → runs and their results
GET /api/events?since=&severity=             → event tape
```
Every response carries `as_of` (timestamp) and units; empty states are explicit (`[]` + reason), never fabricated zeros.

## 9. Build order (suggested Research/Dev requests)

1. Tag orders with `strategy_id` labels and expose `/api/strategies`, `/api/positions`, `/api/market` — KPI strip, strategy board, drill table.
2. Equity/drawdown and attribution from `orders.log` and backtest results; backtest-vs-testnet overlay.
3. Risk map and event tape.
4. Phase-2 controls (auth, confirmation, audit log), testnet only.
