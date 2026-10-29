# internal/api

Folder notes for `CLAUDE.md` (the root file's trading rules apply here too).

- **Routes per domain** (#466): `api.go` holds the server, its options and one `s.routes<Domain>(mux)` call per domain; each domain's routes and thin handlers live in `routes_<domain>.go` (status, book, trades, summary, journal, actions), the pure builders in their own files.
  A new route goes in its domain's file (`TestAPI_RoutesRegisteredPerDomain`); never name a file with a GOOS/GOARCH suffix such as `_windows.go`.
- **`internal/api`** — read-only monitor API (`BOT_API_ADDR`): `/api/status|positions|orders|pnl|events`, built from `Strategy.View()`
  - (`held_legs`: each missing leg and what holds its repair — `holds.go`, `repairGate` is the one gate chain repair and the view share;
  - `flip_buffer_pct` is the live shed buffer, `GammaMonitor.FlipBuffer`, so the monitor's flip ±1σ band is the one the shed uses;
  - `sma` is the SMA trend's levels, `GammaMonitor.SMALevels` — SMA9, SMA21, swings with their days, the trend and the `SMATrendOf` reason —
    plus `candles` (#394, `candles.go`: the last 60 daily OHLC candles and today's live one, built from the startup history fetch and the pushed prices, no new Deribit call; only `Close` drives the trend),
    with `median_flip` and `gex_as_of` beside it for the monitor's regime chart;
  - a change of the SMA trend is journaled once as `trend_changed` (`trend.go`, #118); loop-published snapshots, never calls Deribit) and the journal's in-memory recent events;
  - `connections.<gateway>` in `/api/status` is each gateway's `ConnStats` (drops, reconnects, timeouts today; last drop time and error — `api.WithConnection`, #87).
  The state-changing routes are `POST /api/positions/close` (manual close, below: testnet only) and `POST /api/rebalance` (manual rebalance, below: live and testnet):
  - bearer `BOT_ADMIN_TOKEN`, audited in the bot log and journaled; the monitor's server-side proxies add the token and refuse cross-origin requests.
  Both take an optional `"actor": {"user", "role"}` (#171: the monitor's signed-in person; `api.ActorOf` checks only its form — username 3–64 of `a-z 0-9 . _ -`, role admin|manager|user — none → `monitor`),
  - logged as `by` and journaled as `orders.Actor` (the close's `actor` via `buyToCloseBy`, `rebalance_manual`'s `actor`, `Trade.Actor` in `/api/trades`);
  - who may act is the monitor's check (roles, re-read from its user database), never the bot's.
  Never add other endpoints that change trading state without auth + confirmation + audit.
  `cmd/monitor-demo` serves it with simulated data.
  `/api/account` serves `internal/account` (cached `private/get_account_summaries` plus `private/get_positions` for each held currency, all kinds
  - — so positions no bot manages, which still use margin, are visible; polled every `BOT_ACCOUNT_POLL_SEC`; margin figures are Deribit's, never recomputed).
  The strategy itself reads only short options.
  `/api/pnl/history?range=` serves `internal/history` (append-only `data/pnl_history.jsonl`, written by `strategy/pnl.go` each report interval; trading never reads it).
  Each line also carries the margin view the policy used
  - (`im_pct`, `im_limit_pct`, `mm_pct`, `entries_allowed` — false while frozen, MM-blocked, GEX-stale, stop-deferred or margin unknown; `strategy.HistoryMargin` from `RiskView`, `marginState.entriesAllowed`;
  - and, #277, the account's `margin_balance`, `equity` and `unit` from `AccountSummary.MarginUsage` — Deribit's figures, USD under cross collateral, absent while margin is unknown —
    read by the evidence report's equity bridge, `evidence.BuildBridge`; old lines without them still load);
  - pure `history.Utilisation` is the time-weighted mean of IM ÷ limit while entries were allowed plus the allowed share (a line stands for ≤ `MaxSampleGap` 15 min),
    served as `utilisation_7d`/`utilisation_30d` in `/api/status` (#174, E1's metric).
  `/api/summary` (`summary.go`, pure `BuildSummary`, #177) is the investor's four answers from the view and the journal's trades:
  - `holding`, `income` (realised net P&L per UTC month, last 6, from the journal's closes — its one source — plus unrealised),
  - `worst_case` (a labelled model: Black–Scholes on each leg's IV, a one-day ±3 daily σ move from DVOL, loss in coin, USD and % of equity;
    `scenarios` −3, −2, 0, +2, +3 σ with signed results for the monitor's worst-case page, #318)
  - and `last_action` (latest open/close in plain words, `PlainReason`);
  - plus `strangle` for the How it works page (#322, `howitworks.go`, pure `HowItWorksOf`:
    the nearest two-legged strangle's strikes, premiums, coin-settled break-evens and each leg's exit line —
    the price where its Black–Scholes |Δ| reaches `delta_exit_threshold`, closed form `ExitLinePrice`, a model — null when none is held)
    with `delta_exit_threshold` and `repair_cooldown_hours`; fields in `frontend/README.md`.
  `/api/performance?range=7d|30d|all` (`performance.go`, pure `BuildPerformance`, #232) is `perf.Summarize` over the history's raw points (`history.Store.Points`; `all` from the first point; another range → 400):
  - equity at the window start is the first line's Deribit `margin_balance` (#277's field; USD converted at that line's spot, `EquityAtStart`), never P&L
    — none → every % of equity null, `equity_known` false;
  - `missing_days` listed, never filled; `label` live|testnet from `DERIBIT_ENV`.
  `/api/income` (`income.go`, pure `IncomeWithHistory`, #319) is the summary's income completed for the income page:
  - each month's `premium_coin`/`buyback_coin`/`fees_coin` (from the journal's closes, `CloseParts`: buy-back = premium − fees − P&L, so `premium − buyback − fees = realised` always;
    USD at each close's spot, `CloseSpot`; these are in `/api/summary`'s months too),
  - the month's recorded spot and `hold_usd` (equity at the month's start × spot change),
  - and `summary` (total, average of months with a close, up/down months, worst month, `perf`'s worst week, missing days)
  - — its own route because it reads months of raw history points and `/api/summary` is polled every second.
  `/api/strangles/payoff` (`payoff.go`, pure `PayoffOf(view, now)`, #391) is each strangle's payoff chart for the monitor's Positions page, overview cards and modal:
  - ~121 points of `expiry_pnl` (coin-settled, as `BreakEvens`)
    and `today_pnl` (the book's unrealised P&L moved by Black–Scholes on each leg's IV, so it equals `unrealised_coin` at spot; null with `today_reason` when an IV is missing),
  - break-evens, each leg's exit line (`ExitLinePrice`) and stop level (modelled mark = `stop_loss_mark`, bisection), earned share, net delta (coin, USD per 1 %) and the missing side from `held_legs`.
  `/api/explain?seq=|trade=` (`explanation.go`, pure `BuildExplanation(event, window)`, #314) is the facts behind one journal event for "why did the bot do that":
  - the event with `plain`, `rule`, `detail`, the journaled `market` (spot, DVOL, regime, flip, trend) and `related` (same instrument or slot within ±30 min), `source: "journal"`,
    read from the in-memory recent events only — an event that left them is 404 `not in recent history`, never reconstructed.
  `/api/journal?days=1–7&held=0|1` (`journal.go`, #417, default 7; the assistant's week of facts) is the one route that reads `orders.log`
  - (#427: `held=1` adds every older event of each instrument held now, however old — `ReadHeldJournal` reads the whole file back and decodes only lines naming one, same collapse and bound, `held` lists them;
    a fill or a close carries `market` {spot, dvol, iv_percentile, gex_regime} from its line):
  - `Logger.JournalBack` reads it from its end (seqs as the ring and a restart's replay give them — `writeJSON` numbers and writes under one lock — so `journal:<bot>/<seq>` stays citable), `pnl` lines skipped;
  - the same event + reason (close/skip word/trigger/risk change) + instrument/slot + side + `plain` without its numbers collapses into one item
    with `count`, `first`/`first_seq`, `last` and the last one's seq and numbers;
  - fills, closes, a person's action and a cancel with a fill are never collapsed;
  - ≤ `MaxJournalItems` (2,000) newest items, else `truncated`; a writer-only logger → 503.
  **Plain words** (`explain.go`, #201): one catalogue (`ExplainEvent`, `ExplainTrade`; facts only, never a forecast or advice) gives every item of `/api/events` and `/api/trades` a `plain` sentence
  - — what happened, why, what follows — by event, trigger, close reason, skip reason (the word before the first `:` or space; `repair_held` by the hold its detail names — stopped, GEX shed, regime side, top-up), risk change and trend;
  - an unknown value gets the generic sentence (`GenericPlain`) and one WARN per value per process.
  `tests/api_explain_test.go` reads the `Event*`/`Trigger*`/`Risk*` constants of `internal/orders` and the `Skip*` constants of `internal/strategy` from the source:
  - a new journal value needs its sentence there (and a skip is journaled from a `Skip*` constant, never a literal).
