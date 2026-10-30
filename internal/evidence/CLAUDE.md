# internal/evidence

Folder notes for `CLAUDE.md` (the root file's trading rules apply here too).

```bash
go run ./cmd/evidence -env stable -week 2026-W42   # weekly evidence per bot → <clone>/data/evidence/<week>/<bot>.md|.json: live, replay, gap, stress, hold (README "Evidence report")
go run ./cmd/evidence -compare-direction -week 2026-W43   # direction trial scorecard: stable BTC (bias) vs mvp BTC (sma), live and replayed both ways on the same books, % only → <stable>/data/evidence/direction/<week>.md|.json (README "Replay of recorded books"; decision rule in docs/direction-trial.md, #324)
go run ./cmd/evidence -env stable -track-record -trend   # the whole P&L history + one row per ISO week from data/evidence/<week>/<bot>.json → data/evidence/track-record/<bot>.md|json (#279); the weeks side by side, % only → data/evidence/trend/<bot>.md|json (#323) (README "Track record and trend")
go run ./cmd/idlemargin -journal data/btc/orders.log -history data/btc/pnl_history.jsonl -days 7   # why approved margin went unused: per slot (occupied, skip reason, frozen, not measured) and book state (README "Idle-margin attribution")
go run ./cmd/deployeffect -journal ~/dev/go/optionsBot-env/mvp/data/btc/orders.log -config config_btc.mvp.yaml -deploys ~/dev/go/optionsBot-env/history/deploys.tsv -days 3   # what each startup sent in its first 30 min, by trigger: neutral | expected | restart-made, counts and lots only (#219, docs/operations.md "Soak check: deploy effect")
```

- **`internal/evidence`** + `cmd/evidence` — the weekly evidence report (#176):
  - pure `Build(Inputs)` over the journal's trades (`Trade.Fees` = a close's fees), the P&L history (`perf`, `history.Utilisation`), `replay.Run` on the week's recorded books from the journal's book at the week start, the gap to `replay.ActualPnL`,
    `cmd/stress`'s standard shocks run as a subprocess on `data/stress/<bot>/` (`StressArgs` from the config; its `RESULT` line parsed by `ParseStressResult` — a change of that line's fields needs it too) and hold-the-coin;
  - every section names its source and missing days are listed, never filled;
  - the E1 reliability section (`reliability.go`, #361) adds `history.Utilisation` against the 60 % target, `idlemargin.Attribute`'s idle time by cause and `deployeffect.Analyze`'s startups (journal + `<envs>/history/deploys.tsv`), counts/hours/lots only;
  - the recorded-history coverage section (`coverage.go`, #362) runs `internal/coverage` over the week per dataset (`Inputs.Recorded`, read by `Load`; absent folder → `not recorded`),
    labels the replay and gap `partial` under `ReplayFullCoveragePct` (90) of market hours and lists gaps over `LongGapHours` (3) in `coverage.long_gaps`, which `evidence.sh` posts and notifies;
  - the "replay vs live" section (`replayvslive.go`, #488, `Report.RVL`) puts the Replay and Gap figures side by side (result in coin, USD and % of equity, fees, filled legs, worst day; gap in coin and % of the live result), no second replay;
    - not comparable — missing hours listed, no gap — under 90 % market coverage, a market gap over 3 h, coverage not measured or no replay;
    - causes only from the journal (frozen slot-hours, startup rebalance orders, closes with an `Actor`);
    - `Inputs.Account` (`DeribitAccount(root)`, set by `cmd/evidence`) = `live` drops every amount from its Markdown and JSON (% only).
  Reads files only; writes under the clone's `data/evidence/`.
  Pure `BuildTrackRecord` (`track.go`, #279:
  - `perf.Summarize`/`ReturnOnMargin`/`Utilisation` from the history's first to last point, equity = the earliest weekly report carrying one,
    one `WeekRow` per ISO week read from `data/evidence/<week>/<bot>.json` — no report → `not reported`, never recomputed;
  - a section an older report lacks (empty `Source`/`Label`) → `not measured`; counts)
  - and `BuildTrend` (#323: the same rows first → last report, amounts dropped, % only);
  - `ReadWeekly` reads only ISO-week folders.
  A new weekly figure worth following needs a `WeekRow` field and its column in `track_markdown.go`.
