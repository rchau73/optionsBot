# internal/compare

Folder notes for `CLAUDE.md` (the root file's trading rules apply here too).

```bash
go run ./cmd/compare -base config_btc.yaml -proposed my.yaml -from 2026-10-12 -to 2026-10-18 -replay data/btc/data/market -stress data/stress/btc   # two configs side by side → data/compare/<time>/report.md|json: key-by-key diff, backtest, replay, stress; `-windows week|day` adds backtest and replay per window and how often the proposal wins (#301); `-neighbours <key>` re-runs the proposed value ±10/20 % and flags cliffs (#495) (README "Settings comparison")
go run ./cmd/compare -base config_btc.yaml -set gamma_flip_buffer_sd=1.2 -from … -to …   # -set key=value (repeatable, #641): the proposed config = -proposed (or -base) with these values, a temporary copy; "[]" in a key = every element
bash scripts/lab_runner.sh     # the lab's runner (#641, cmd/labrun): the monitor's queued run requests (data/compare/requests/) one at a time through cmd/compare -set … -neighbours → done with run_id | failed with a reason | expired after a day
go run ./cmd/analyze -report data/compare/<time>/report.json   # AI analysis of a comparison (#426): one model call (ANTHROPIC_API_KEY; -fake <answer.md> sends nothing), checked by aianalysis.Check (#257) → analysis.md|json beside the report, Disclaimer first; a failing answer writes nothing and exits 1; offline, no monitor route (README "AI analysis of a comparison")
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
  - `set.go`'s `WriteWith` builds `-set`'s temporary proposed file (also `RunNeighbours`' copies); nothing here writes a shipped config.
  Never name a Go file `*_windows.go`/`*_windows_test.go`: Go builds it on Windows only.
