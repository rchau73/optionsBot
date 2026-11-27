# internal/compare

Folder notes for `CLAUDE.md` (the root file's trading rules apply here too).

```bash
go run ./cmd/compare -base config_btc.yaml -proposed my.yaml -from 2026-10-12 -to 2026-10-18 -replay data/btc/data/market -stress data/stress/btc   # two configs side by side → data/compare/<time>/report.md|json: key-by-key diff, backtest, replay, stress; `-windows week|day` adds backtest and replay per window and how often the proposal wins (#301); `-neighbours <key>` re-runs the proposed value ±10/20 % and flags cliffs (#495) (README "Settings comparison")
go run ./cmd/compare -base config_btc.yaml -set gamma_flip_buffer_sd=1.2 -from … -to …   # -set key=value (repeatable, #641): the proposed config = -proposed (or -base) with these values, a temporary copy; "[]" in a key = every element
bash scripts/lab_runner.sh     # the lab's runner (#641, cmd/labrun): the monitor's queued run requests (data/compare/requests/) one at a time through cmd/compare -set … -neighbours → done with run_id | failed with a reason | expired after a day; a done run then gets its AI analysis (#792, below), best effort
go run ./cmd/labapply -id 20261104T230000Z -compare data/compare   # a recorded lab change (#806, COMPARE_DIR/applied/<id>.json) → COMPARE_DIR/candidates/<id>/: config.candidate.yaml (ready only), diff.md, candidate.json (ready | refused, reasons); exit 0 ready, 2 refused, 1 error
go run ./cmd/labpr -id 20261104T230000Z -compare data/compare [-dry-run]   # a ready candidate → branch lab/<id> from origin/main (the recorded values in the config and its BTC twin, regen.sh's files) and one draft PR through gh (#862); by hand only; exit 0 opened or already open, 2 refused, 1 error; never merges or pushes main
go run ./cmd/labstats -compare data/compare [-days 28] [-json]   # the lab's usage report (#824): requests, done/failed/expired %, median minutes to done, per manager per week (a hash), recorded changes, analyses written — counts only, read-only
go run ./cmd/labfollow -id 20261104T230000Z -live-from 2026-11-10 [-days 7] [-record-live-from] [-json]   # the lab follow-up (#855): the run's backtested figures beside the realised ones after it went live, the gap in points; -all = every applied/<id>.live; read-only, exit 1 on bad input
go run ./cmd/analyze -report data/compare/<time>/report.json   # AI analysis of a comparison (#426): one model call (ANTHROPIC_API_KEY; -fake <answer.md> sends nothing), checked by aianalysis.Check (#257) → analysis.md|json beside the report, Disclaimer first; a failing answer writes nothing and exits 1; no monitor route (README "AI analysis of a comparison")
```

- **`internal/compare`** + `cmd/compare` — two configs through the same backtest, replay (each from a flat book) and `cmd/stress` standard shocks (`evidence.RunStress` once per config), side by side (#186):
  - `DiffConfigs` lists the differing YAML keys first (flattened leaves; fixed values and `.env` are never compared), every report is headed `Disclaimer` ("Backtested/replayed, not a promise");
  - differing keys `cmd/stress` does not take are listed as `Stress.NotModelled` (`evidence.StressModels` — a new `StressArgs` flag needs its key in `stressKeys`).
  Reads files only; writes `data/compare/<time>/report.md|json`.
  The monitor reads them back, read-only: `/api/compare` (#302) and the lab's `/api/lab/runs` (#539, `frontend/lib/lab.js`: headline in percentages, analysis withheld until legal question L10)
  — a renamed or removed `report.json` field used there (`diffs`, `return_pct`, `worst_week_pct`, `max_drawdown*`, `fees*`, `equity*`, `windows.summary`, `neighbours.cliffs`) updates `lib/lab.js` too.
  A new figure in `backtest.Summary` or `replay.Result` worth comparing needs a line in `BacktestRun`/`ReplayRun` and `markdown.go`.
  `RunWindows` (`windows.go`, #301, `-windows week|day`) re-runs `Run` per ISO week or day (stress not windowed) into `Report.Windows`;
  - pure `Windows` and `SummarizeWindows` (wins, median, spread, worst window, replay/backtest order flips; a window that did not run is listed, counted nowhere).
  `-neighbours <key>` (`neighbours.go`, #495): pure `NeighbourValues` (±10/20 %, rounded to the catalogue's step, clipped to its range, duplicates skipped) and `FindCliffs` (`CliffPP`, its reason beside it);
  - `RunNeighbours` loads each value through `config.Load` on a temporary copy of the proposed file (a `Validate` refusal is a skipped row), runs backtest and replay only, into `Report.Neighbours`; the analysis's grounding check reads its numbers like any other.
  The lab's run requests (`requests.go`, #641): `POST /api/lab/requests` (`frontend/lib/labrequests.js`) writes `data/compare/requests/<id>.json` (the shape of `tests/testdata/lab_request.json`, read by both test suites);
  - `RunLabQueue` (lock folder `.runner.lock`, taken over after `RequestMaxAge`) marks orphaned `running` as failed and day-old `queued` as expired, then runs each queued request through `CompareArgs` and an injected `CompareFunc` (cmd/labrun: `go run ./cmd/compare`);
  - after a done run, one call to an injected `AnalyzeFunc` (#792; cmd/labrun: `aianalysis.LabAnalyzer`, the same `aianalysis.Run` as cmd/analyze) writes `analysis.md|json` beside the report;
    no key, a model error or a failed check leaves the run done with `note: "analysis not written: <reason>"` (not served by the monitor), failed, expired and orphaned requests get no call;
  - `set.go`'s `WriteWith` builds `-set`'s temporary proposed file (also `RunNeighbours`' copies); nothing here writes a shipped config.
  Lab candidates (`candidate.go`, #806, `cmd/labapply`): `BuildCandidate` re-checks a manager's record (the shape of `tests/testdata/lab_applied_record.json`, written by `frontend/lib/labapply.js`) on the bot side, because the file alone is not trusted:
  - a shipped config only (`CandidateConfigs`), the key in the catalogue (`TunableFor`) or fixed (then `config.Load` refuses it), `RangeReasons` (labvalidate.js's rules), the record's `old` still the shipped value (`StaleReason`),
    a core policy only with `product_owner_approval` in the record (a person adds it; else `needs_product_owner`), the run's `report.json` readable, then the whole candidate through `config.Load` and `DiffConfigs` (only the recorded keys differ);
  - `EditConfigText` changes the values in the file's text in place (comments and alignment kept, a missing top-level key appended);
  - writes only `COMPARE_DIR/candidates/<id>/`, each file only when its bytes change (a second run rewrites nothing); a refusal removes an older `config.candidate.yaml`; it never edits the record or a shipped config and calls no bot or exchange (`tests/lab_candidate_test.go` checks the imports).
  Lab pull requests (`labpr.go`, #862, `cmd/labpr`; decision `docs/product/decisions/2026-11-20-lab-change-reaches-bot-by-pr.md`): `PlanLabPR` (files only) acts on a `ready` candidate alone, else a `*LabPRRefusal` with the reasons:
  - a core policy again needs `product_owner_approval`; `config.candidate.yaml` must equal `EditConfigText` on today's shipped file (else run labapply again), and the twin (`ConfigTwins`, all but `TwinOnlyKey`) must still hold the old value, each through `loadCandidate`;
  - `LabPRBody`: diff.md's Settings table, percentages under `LabLabel`, the cliffs, the risks, `ManagerHash` of the person, `LabPRMergeLine`; `LabPRPrivacyReasons` refuses a name or an amount;
  - `RunLabPR` sends git and gh through an injected `Runner` (cmd: os/exec; `tests/lab_pr_test.go`: a fake): one PR per branch (open/merged reported, closed refused), a worktree of origin/main, `scripts/regen.sh`, only those files committed, a push of `lab/<id>` only, `gh pr create --draft`.
  The lab's usage report (`labstats.go`, #824, `cmd/labstats`): `ReadLabUsage` reads the queue, `applied/`, `dropped/` (#890), `candidates/<id>/candidate.json` and `<run id>/analysis.json` (an unreadable request file is counted, never skipped);
  - pure `ComputeLabStats` (`now` passed in) gives counts, shares, the median minutes to done, requests per manager per ISO week under `ManagerHash` (8 hex of SHA-256) and the targets; `LabStatsMarkdown` prints it;
  - a drop (#890, `POST /api/lab/runs/<id>/drop`) counts beside the recorded changes: `Decided` / `DecidedPctOfDone` are the done runs ending in a decision, a run with both files counted once, as recorded;
  - no setting value, name, amount or run id in the output, and it writes nothing (`tests/lab_stats_test.go`); `usage.sh` appends it weekly (`lib.sh lab_section`).
  The lab follow-up (`labfollow.go`, #855, `cmd/labfollow`): `ReadLabFollow` reads the record, the run's `report.json`, the bot's journal and P&L history under `-root` (`evidence.BotPaths`) and the equity (the run's replay equity, else `account.json`);
  - `CheckLiveFrom` refuses a day in the future or before the record (exit 1, nothing written); pure `BuildLabFollow` (`now` passed in) puts the proposed backtest (return and fees scaled to the window) beside `perf.Summarize` over `[live-from, +days)` and the close fees, the gap in points;
  - states `too early to judge` / `no data` (no gap), a flag when the realised worst week or drawdown is deeper; `applied/<id>.live` holds the day (`RecordLiveFrom`, `-record-live-from`), `-all` follows them all for `usage.sh` (`lib.sh lab_follow_section`);
  - percentages and counts only, keys without values, no name; `tests/lab_follow_test.go` checks the imports.
  Never name a Go file `*_windows.go`/`*_windows_test.go`: Go builds it on Windows only.
