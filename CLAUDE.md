# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.
It holds what applies everywhere; each folder's own `CLAUDE.md` (index under "Architecture") holds that folder's notes.
One sentence per line; a long one continues as sub-bullets.

## Build, run, test

```bash
make build                     # go build -o bot ./cmd/bot
make check                     # gofmt + go vet + shell scripts + race tests + coverage floor (what CI runs)
scripts/check_cover.sh --update   # coverage floor (#330): fails when ./internal/... falls > 0.2 pt under scripts/cover_baseline.txt (lists the packages that fell vs cover_baseline_packages.txt); --update raises it — only by a person or a PR that says so
bash scripts/check_shell.sh    # bash -n + shellcheck -x --severity=warning on scripts/**/*.sh and .githooks/* (shellcheck skipped if not installed; CI requires it); every disable needs "disable=SCxxxx # why"
make test                      # scripts/go_tests.sh all: core tests under -race, then the report/research tests (single goroutine) without it (#429)
scripts/go_tests.sh core|reports [PROFILE]   # one group; CI runs both as parallel jobs, merges their coverage profiles and on a PR runs only the jobs its files need (`scripts/agents/checkfail.py suites`, #472); a new slow single-goroutine test goes in REPORT_TESTS
make cover                     # coverage across ./internal/... (tests live in ./tests)
bash scripts/regen.sh [--resolve]   # rewrite the generated files (.gitattributes linguist-generated); --resolve settles a merge conflict in them (#465) — never by hand
make diagrams                  # re-render docs/*.mmd → 4× PNG (needs mmdc)
scripts/envctl.sh stable|mvp up|status|logs -f bot-btc  # the two local environments (never plain docker compose)
scripts/promote.sh v0.3.0      # stable → a release: a new tag goes on the newest mvp deploy (deploys.tsv, never a rollback line) 3 days old (`--commit <sha>` = another build that ran in mvp); no open sme:not-fixed; rollback = an older tag; refuses inside stable's clone (#360)
scripts/promote.sh v0.3.1 --commit <sha> --escalate "<reason>"   # admin escalation (#471): no soak, the reason audited; weekly/hotfix releases: scripts/agents/release.sh
scripts/prune_branches.sh      # delete local branches already in main (GitHub deletes merged PR branches itself)
make hooks                     # once per clone: pre-commit refuses .env files and any .env secret value (public repo)

./bot --config config_btc.yaml                     # trade on testnet (default)
DERIBIT_ENV=live ./bot --config config_btc.yaml    # live — real capital, explicit opt-in
./bot --mode=backtest --config config_btc.yaml --from=2026-01-01 --to=2026-12-31
./bot --mode=backtest --config config_btc.yaml --sweep=true --from=… --to=…
./bot --mode=backtest --config config_btc.yaml --sweep-ladders=true --from=… --to=…   # DTE ladders: slot occupancy, return on margin, worst week
./bot --debug                                      # per-cycle diagnostics
go test ./tests/... -run TestStrategy_ -v          # one group
```

The research tools' commands (`cmd/stress`, `cmd/evidence`, `cmd/compare`, `cmd/dora`, …) are in the folder files of the index below.

All tests live in `tests/` (package `tests`), not next to the source.
Lists every PR adds to are kept in alphabetical order, a test failing otherwise (#465): `glossary.json` keys, `golden.jsonl` ids, the analytics plan (`plan.js`, `plan.go`, `event-plan.md`), the responsive audit's panels.
Notable files: `tests/CLAUDE.md`.

## Environment & config

Strict separation — do not mix:
- `.env` — Deribit platform only: credentials, `DERIBIT_ENV` (testnet|live), rate limits (two pools), retry/backoff, circuit breaker, heartbeat/reconnect.
- `config_*.yaml` — all strategy and execution logic.
  `config.Load` fills defaults and runs `Validate()`; `RequireCredentials()` is called only for trading, so backtests need no API key.

Never reintroduce env-var overrides for logic parameters.

**Two local environments, sharing nothing** (2026-10-17, #129; `docs/operations.md` "Environments"):
- `~/dev/go/optionsBot-env/stable` (a release tag, moved only by `scripts/promote.sh` — a person's or the weekly `release.sh`'s — the track record, market recorder on) and `~/dev/go/optionsBot-env/mvp` (main, auto-deployed by `deploy.sh`; the agents run from it).
- Each clone has its own `.env` — **its own Deribit account**: a bot cancels every order of its currency at startup and adopts every short it finds, so two environments on one account fight
  - — and a `deploy.env` (`OPTIONSBOT_ENV`, `MONITOR_PORT` 3000/3100, `BOT_RECORD_MARKET`; mvp also `BOT_CONFIG_BTC=./config_btc.mvp.yaml` — BTC on `gex_direction: sma` while stable stays on `bias`, to compare the two, #168;
  - that file is `config_btc.yaml` with only that line different, enforced by `TestConfig_MVPBTCDiffersOnlyInDirection`: change both).
- `scripts/env_lib.sh` `env_check` refuses a shared port, or a `DERIBIT_CLIENT_ID` shared by any two environment folders (#538); compose refuses without `OPTIONSBOT_ENV`.
- Bots report `deployment`/`version` (`BOT_DEPLOYMENT`/`BOT_VERSION`) in `/api/status`, shown in the monitor header.
- The dev checkout runs nothing; `data/btc` there links to stable's for the research tools.
- The monitor requires a signed-in session on every page and `/api` route (#170, `frontend/README.md` "Sign-in"; users per environment, `AUTH_SECRET` in its `.env` or it won't start);
  - scripts that read it (`wait_healthy`, the agents, `stress_inputs.sh`) send that environment's read-only `MONITOR_API_TOKEN` (`env_monitor_token`) — a new script reading the monitor must too.

**Few settings** (2026-10-14, the user's call: "far complex to manage"):
- the YAML holds only what is tuned — slots, exits, `repair_cooldown_hours`, `gamma_flip_buffer_sd`, `gex_direction`, margin bands/MM, `max_leg_size_multiple`, `min_premium`.
- Every other rule value is fixed in `internal/config/fixed.go` (`ApplyFixed`, run by `Load`); a file that sets a fixed key fails to load (`fixedKeys`).
- The snake_case names below (e.g. `gex_shed_confirm_snapshots` (2)) name those fixed values — the Go fields keep them, so tests and sweeps can still vary them in code.
- Do not add a config key for a new rule: fix its value in `fixed.go` and keep a `cmd/stress` flag to study it.
- `tests/config_effective_test.go` pins every effective setting of both shipped configs (golden `tests/testdata/effective_config_*.json`; regenerate with `UPDATE_GOLDEN=1` only for an intended change).
- **A new tuned key needs a catalogue entry** (`internal/config/catalogue.go`, `config.Tunables()`, #255: plain words, unit, the range `Validate` enforces, cross rules, core-policy area, the tools that vary it)
  and `go run ./cmd/paramdocs` to regenerate `docs/parameters.md`; `tests/config_catalogue_test.go` fails otherwise.

**Secrets never reach GitHub** (the repository is public):
- `.env` and every `.env.*` except `.env.example` are ignored;
  `make hooks` installs `.githooks/pre-commit`, which refuses env/key files and any staged line containing a value of a `*SECRET*`/`*TOKEN*`/`*CLIENT_ID*`/`*KEY*` variable from the local `.env`;
  CI runs gitleaks over the full history.
- Never paste a secret into a commit, PR text, test fixture or log line.

## Architecture

`cmd/bot/main.go` is the composition root: it wires `Gateway → MarketData → GEX Manager → Strategy` (plus, on testnet, a public-only mainnet gateway for GEX — see `internal/gex`),
handles `SIGUSR1` (kill switch) and watches `Gateway.Fatal()`.
Details: `docs/architecture.md`; diagrams: `docs/README.md`.

Each folder's notes are in its own `CLAUDE.md`, loaded when you work there:
- `internal/gateway/CLAUDE.md` — every Deribit connection: priority queue, rate limiter, circuit breaker, retries, reconnect, the watchdog.
- `internal/marketdata/CLAUDE.md` — option chain, index, DVOL, ticker subscriptions, `HasQuote`.
- `internal/gex/CLAUDE.md` — dealer gamma exposure from mainnet open interest, `gex.Build`, the reference script's method.
- `internal/strategy/CLAUDE.md` — the decision loop's files and the details (history, numbers, mechanics) of the trading rules below.
- `internal/orders/CLAUDE.md` — `Executor`, `StateManager`, the decision journal, P&L net of fees, the journal as the history.
- `internal/api/CLAUDE.md` — the monitor API: every route, the two state-changing ones, plain words.
- `internal/exchange/CLAUDE.md` — the exchange boundary (interfaces, Deribit as the only adapter, the coupling test).
- `internal/recorder/CLAUDE.md` — market history for backtests and stress tests.
- `internal/perf/CLAUDE.md` — performance summary and return on margin.
- `internal/evidence/CLAUDE.md` — the weekly evidence report, track record and trend.
- `internal/compare/CLAUDE.md` — two configs side by side, windows.
- `internal/helpkb/CLAUDE.md` — the assistant's corpus and search.
- `internal/assistant/CLAUDE.md` — the assistant's answer guard and the monitor's assistant.
- `internal/backtest/CLAUDE.md` — the backtest and `cmd/stress` (stress test and replay of recorded books).
- `scripts/agents/CLAUDE.md` — the agentic workflow: SME, PM, Dev, Reviewer, PR gate, deploy, ledger and the scheduled reports.
- `frontend/CLAUDE.md` — monitor pages and the menu.
- `tests/CLAUDE.md` — notable test files.
- **`internal/hedge`** — writes `hedge_report.json` when |net delta| ≥ threshold.
  **Never places orders.**

### Trading rules (keep these true)

Each rule's history, numbers and mechanics continue in `internal/strategy/CLAUDE.md` "Trading rules: details".

- **Slots:** each `(DTE, delta)` in `dte_delta_matrix` is one strangle slot; occupied by an open strangle (even one-legged) or a pending entry.
- **Journal rule:** every decision (submit, amend, cancel, fill, close, reconcile, skip) is journaled through `s.eventContext`/`s.instrumentContext`,
  which build the market snapshot (`strategy/snapshot.go`, pure `BuildMarketSnapshot`).
  New decision points must journal too, and every close passes a `detail` (the rule and its numbers, e.g. `RolloutDecision.Detail`) to `buyToClose`.
- **Margin policy** (`internal/risk`, pure; applied in `strategy/limits.go`): every figure is Deribit's — account-wide USD totals under cross collateral, else the currency's own (`AccountSummary.MarginUsage`).
  IM limit (% of margin balance) = band of the DVOL IV percentile (`iv_margin_bands`), or the lowest band under a confirmed negative gamma regime.
  Band/regime changes are confirmed on `iv_band_confirm_days` consecutive UTC daily closes (DVOL closes from `DVOLTracker.Daily`, regime closes from `data/regime_history.jsonl`;
  - a **brand-new** bot — empty regime history — seeds the `iv_band_confirm_days` closes before today with the live regime, marked `seeded`, journaled `regime_seeded`
    (`RegimeStore.Seed`, #147: a fresh mvp sat frozen a day), so it starts from the market as it is; restarts are never seeded);
  - until then new risk is frozen (exits, rolls and repairs continue) — but only when confirming the change would lower the effective limit (`limitFor` in `risk.Evaluate`):
  - a change that can only raise it or leave it (a DVOL band rise, a DVOL change under a confirmed negative regime, the regime turning positive) stays `pending` without freezing
    — the current, stricter limit applies until it confirms;
  - the regime turning negative always freezes (2026-10-15: ETH frozen and flat for days on a DVOL change that could not lift its pinned 20 %; 2026-10-16 ETH frozen ~8 h of 12 by a live regime flapping to positive, #105).
  A confirmed limit change (and startup) runs `rebalancePositions` toward it, sizing each slot as a whole (all its strangles)
  - — except that the first pass after a startup does nothing while IM is within `rebalance_startup_band_pct` (±1.5 pp) of the limit
    (`RebalanceInBand`, journaled `rebalance_in_band`; 2026-10-17 BTC: two deploys 38 min apart cut and re-sold 1.2 lots, #123), a later confirmed change is met exactly;
  - a complement that times out short re-arms it after `rebalance_retry_minutes` (journaled `rebalance_retry`)
    — doubled per unfilled pass from the `rebalance_backoff_after` (3)-th pass in a row whose every complement ran to its fill timeout without filling both legs
    (#98: a put-only fill counts as unfilled; the `rebalance_retry` line names the legs that filled), up to `rebalance_retry_max_minutes` (60),
    reset by a complement filled on both legs or a limit change (`RebalanceRetryWait`, `NextUnfilledPasses`; 2026-10-14 BTC: 12 unfilled top-up orders in 1.5 h)
  - — complements the bot cancels itself (freeze, shed, kill switch) don't count, a held or failed top-up re-arms at the base wait, the kill switch never re-arms (#93)
  - — never while a complement or an entry is still working (#97: one new-risk sizer at a time — a due pass waits for a working entry, journaled once as a `rebalance_wait_entry` skip,
    and entries wait for a working complement, `entry_wait_topup`; 2026-10-15 BTC: an entry and a top-up sized together reached IM 25.7 % of 20 % and were bought back);
  - MM ≥ `max_mm_pct` reduces positions at market at once and blocks repairs and entries.
  Missing/stale margin data or a failed simulation → no new risk.
  Never reintroduce a margin cap computed from equity.
  A freeze or an MM breach also cancels working entries and top-ups (`cancelNewRisk`, journaled `risk_frozen`; repairs keep working) — an order sent just before a freeze otherwise filled into it (2026-10-08).
- **Size cap:** a new leg (entry or complement upsize) is at most `max_leg_size_multiple` (2) × its slot's **normal size**
  = slot share ÷ the strangle's margin per lot **on its own** (`SimulateAlone`, `add_positions=false`) — `CapLots`.
- **Liquidity cap** (`liquidity.go`, pure `LiquidityCapQty`; #103): an entry or top-up order is also at most `liquidity_cap_oi_pct` (10, fixed) % of each sold strike's **mainnet** open interest
  (the GEX manager's `OISnapshot`, no extra call; testnet strikes matched by name), floored to lots; both legs get the smaller cap, so they stay even (a one-sided entry is capped by its own leg).
  Repairs, exits, stops and the MM cut are not capped.
- **Sizing:** each vacant slot gets `EntryShare` = min(its slot share = limit × margin balance ÷ all slots, headroom ÷ vacant slots) — the same slot share the rebalance targets, so a strangle opened into a nearly full book is not oversized;
  `private/simulate_portfolio` (≤ 1 call/s, spaced in the executor) prices one lot, then confirms the final size keeps post-trade IM and MM within the limits.
- **Entry:** limit sell at `max(mid, ask)`; premium floor checked for every leg before any order, at the lowest price the step-down can reach;
  - while resting, `repriceLegs` steps the price down (`StepDownPrice`, pure): ask for the first third of the timeout, mid (rounded up to a tick) for the second, then the floor
    — `entry_price_floor` (mid; also rebalance upsizes) or `repair_price_floor` (bid: a repair restores a one-sided hedge, so it crosses the spread);
  - no bid → stays at the ask; re-pricing up to a higher ask counts toward `order_max_adjustments`;
  - after `order_fill_timeout_sec` cancel, read back the final fill and **book filled legs** (one-legged strangles are completed by repair).
- **Marks:** `refreshMarks` copies a quote onto a position only when the instrument `HasQuote()`, which sets `Position.MarkLive`; otherwise the last known mark stays (from the exchange at reconcile).
  Reconcile converts Deribit's size-weighted position greeks to per-option greeks (`PerOptionGreeks`).
- **Exit priority** (`EvaluateLeg`): stop-loss → DTE roll → **delta exit** → delta drift → ROI take-profit.
  The delta exit closes a short leg whose |Δ| reaches `delta_exit_threshold` (0.30) — the move is real — before the 2× stop.
  Delta exit, drift and take-profit act only on a `MarkLive` position; stop-loss and the DTE roll always act (on the last known mark).
  Stop-loss = market buy, high priority — except while the leg's book is empty:
  - `StopDeferReason` (`stopguard.go`, pure) defers it while the ask is more than `stop_spread_guard_pct` (20) above the mid (or there is no bid),
    re-checked each cycle (fires once the spread is normal if the stop still applies, dropped if the price came back), at most `stop_spread_max_wait_minutes` (5), then at market;
  - journaled once as a `stop_deferred` skip; while any stop waits, no entries, top-ups or repairs.
  The MM cut never waits (stress: deferring it ran margin past liquidation).
  Delta exit = IOC limit at the ask.
  `buyToClose` books partial fills and keeps the remainder tracked.
- **Unknown order outcomes:** a submit that fails without a Deribit answer (`orders.MaybePlaced`: connection lost, timeout) may still be on the book.
  Every order has a unique label (`uniqueOrderLabel`); such an order is cancelled by label (`private/cancel_by_label`) each cycle until that succeeds, and its slot (entries/repairs) and instrument (closes) are blocked meanwhile.
  Every cycle `checkPositions` compares the exchange's short positions with the book (+ fills of tracked entry orders):
  - a difference on two consecutive cycles (or right after resolving an unknown order) is adopted into the book — ERROR log + `reconciled` journal line — so every exchange position is managed.
  See `strategy/orphans.go`.
  Never treat a submit error as "not placed" without this.
- **Position safety** (`safety.go`, after the 2026-10-06 testnet churn): the position check compares the exchange with the **sum** of the book's positions per instrument
  (one instrument can back several positions), and a difference is applied newest position first.
  A buy-back never exceeds the short the exchange holds this cycle (`capToExchange`), so a wrong book cannot flip a position long.
  A long on an instrument the bot trades (book, working orders, past buy-backs) is sold at the bid (`close_long`); other longs are left alone.
- **Even legs:** every cycle `balanceStrangles` (`balance.go`) buys back the excess of the larger leg of any strangle whose call and put differ by ≥ 1 lot (IOC at the ask, `rebalance_legs`),
  outside the rollout window, when no repair is working, and not while an exit rule is closing the smaller leg (a roll filling in pieces: 2026-10-08 ETH shrank 503 → 303).
  Risk-reducing, so it runs while frozen.
  One-legged strangles stay with repair.
- **Rollouts only close.**
  Repair reopens a single rolled leg (same expiry, entry delta, size); a strangle with both legs gone frees its slot for a fresh entry.
  Never add a second reopen path.
- **No re-selling into the move:** a leg closed by a stop-loss or a delta exit (or cut to zero by the MM limit) is recorded (`stopped.go`) and repaired only when `RepairBlockReason` is empty
  — entries not frozen, confirmed gamma regime not negative, and `repair_cooldown_hours` (72) since the stop.
  Rolled (take-profit / delta-drift) legs reopen at once.
  Held repairs are journaled as `repair_held` skips.
- **Confirmed negative regime, side by trend** (`regimeside.go`, pure `RegimeSideBlockReason`):
  under a confirmed negative gamma regime, no new puts in a bear trend and no new calls in a bull trend (`GammaDecision.TrendDir`)
  — entries open the other leg alone, repairs of that side are held (`repair_held`), and a top-up adds both sides or none (held as `regime_side`, `rearmRebalance` retries it).
- **GEX gating:** trading decisions use `GammaDecision.Action`, never raw `gamma.Trend()`.
  Entry and repair skip a leg only when the action sheds that leg type.
  A leg is shed only when spot is more than `gamma_flip_buffer_sd` (1.5 since 2026-10-14; was 1) daily standard deviations — `FlipBufferPct` = sd × DVOL ÷ √365, from the live DVOL —
  below the flip, in a NEGATIVE regime, with a trend (bull → calls, bear → puts).
  `Action` is the live signal (entry and repair skip that leg type);
  the shed itself uses `Shed`, the same action once it has held on `gex_shed_confirm_snapshots` (2) consecutive GEX snapshots (counted by `ComputedAt`, so cycles on one snapshot count once).
- **Direction source** (`gex_direction`, `gamma.go`): the side to avoid in a negative regime comes from the SMA trend (`sma`, default and live) or the reference script's ACCELERATION BIAS
  (`bias`: `BiasDir` = spot above flip × (1 + buffer) → up, below flip × (1 − buffer) → down, else neutral; positive regime = neutral).
- **Manual close** (`manualclose.go`, pure `ManualCloseBlockReason`): the monitor may buy back a short leg at market only while `RegimeSideBlockReason` blocks its side
  (confirmed negative regime; puts in a bear trend, calls in a bull trend) — the leg the bot no longer sells but may still hold (2026-10-09 BTC: puts re-sold during two minutes of neutral trend).
- **Kill switch:** cancel all orders for the currency → read back and book what each working entry filled (`settleLeg` + `finalizePending`)
  → flatten at market with retries on a detached context, the book matched to the exchange's shorts before each pass (`syncBookToExchange`, no drift confirmation) and the exchange checked flat at the end
  → stay idle (don't exit; Docker would restart into trading).
- **Startup** (reconcile, regrouping, slot assignment: `internal/strategy/CLAUDE.md`).
  **No new risk without fresh GEX** (`gexready.go`, pure `GEXWaitReason`): with GEX wired, entries (`gex_wait` skip), top-ups and repairs wait until the first snapshot and whenever it is older than `GEXStaleAfter` (5 min)
  — before it the regime and trend are unknown (2026-10-09: both bots sold puts seconds after a restart into a negative regime with a bear trend).
  Exits, balancing and the MM cut do not wait.
- **Exit thresholds bracket the entry delta:** `delta_drift_threshold` ≤ 75 % of every entry delta (`config.MaxDriftToEntryRatio`) and `delta_exit_threshold` above every entry delta
  — `Validate` rejects anything else, because a leg sold at a threshold is rolled or exited on the next cycle, over and over.
- **Strikes:** an entry or rebalance top-up picks its call and put together (`SelectStrikePair`): each within `delta_slippage` of the entry delta,
  the pair scoring | |Δc| − |Δp| | + ½ (distance of each from the target) — on a coarse grid (ETH) legs picked alone left the book tilted.
  A repair picks the strike nearest the entry delta (`SelectStrike`); matching it to the open leg was stress-tested and rejected (−25–30 % vs holding).
- **One expiry per slot:** a vacant slot enters `SelectSlotExpiry` (`marketdata.PickExpiry`): its window's nearest expiry unless another slot holds it (open strangle, pending entry or unconfirmed order — `heldExpiries`),
  then the nearest free expiry up to target × `expiry_stretch`, otherwise it waits (`no_free_expiry` skip).
  An empty window still waits (`no_expiry`).
  Rebalance complements use their own strangle's expiry.
  The backtest applies the same rule.
- **DTE has one definition:** `marketdata.DaysToExpiry` — whole days left, rounded down.
  Entry (`NearestExpiry`/`SelectExpiry`) and the exit rules (`Position.DTEAt`) both use it, so an expiry the entry accepts (DTE > `rollout_dte`) is never rolled on the next cycle.
  Never compute DTE another way.
- **Time:** pure functions take `now`; never call `time.Now()` inside them (the backtest replays the past).

## Documentation

- `README.md` technical overview · `OptionStrategy.md` business explanation · `docs/architecture.md` · `docs/operations.md` · `docs/README.md` (diagram index).
- **Legal** (`docs/legal/`, the `legal-counsel` skill, #228): preparatory analysis for a licensed lawyer
  — preliminary answers to the legal brief's L1–L24 (`docs/product/legal-brief.md`), studies per jurisdiction (Brazil first), risk register, data-protection programme, audit and records,
  PT-BR drafts of terms, risk statement, privacy policy, API-key authorisation, testnet-trial agreement and disclaimers.
  Not legal advice; nothing goes to an external user before counsel approves it.
  Counsel's names, fees and opinions stay in the gitignored `data/product/legal/`.
- **Agentic workflow:** `scripts/agents/CLAUDE.md` and `AGENTIC.md`.
- `OptionStrategy.md` §9 "Market regimes and what the bot does" is the table of every open/close/hold rule by market state (normal, freeze, confirmed negative regime, GEX shedding, MM limit).
  Update it in the same PR as any rule change.
- Diagrams are Mermaid `.mmd` in `docs/` rendered to PNG with `make diagrams`; update both when a flow changes.
- The planned dashboard lives in `frontend/` with its own `frontend/README.md`; keep UI/UX material out of the backend docs.
- **Monitor pages and the menu:** `frontend/CLAUDE.md` — a page added, renamed or removed updates the menu, the menu test and the Monitor guide in the same PR.
- A folder's `CLAUDE.md` is listed in the index above; `tests/claude_docs_test.go` fails on one that is not, on a root file of 25 KB or more and on a prose line over 300 characters.
