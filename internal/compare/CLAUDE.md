# internal/compare

Folder notes for `CLAUDE.md` (the root file's trading rules apply here too).

```bash
go run ./cmd/compare -base config_btc.yaml -proposed my.yaml -from 2026-10-12 -to 2026-10-18 -replay data/btc/data/market -stress data/stress/btc   # two configs side by side → data/compare/<time>/report.md|json: key-by-key diff, backtest, replay, stress; `-windows week|day` adds backtest and replay per window and how often the proposal wins (#301) (README "Settings comparison")
go run ./cmd/analyze -report data/compare/<time>/report.json   # AI analysis of a comparison (#426): one model call (ANTHROPIC_API_KEY; -fake <answer.md> sends nothing), checked by aianalysis.Check (#257) → analysis.md|json beside the report, Disclaimer first; a failing answer writes nothing and exits 1; offline, no monitor route (README "AI analysis of a comparison")
```

- **`internal/compare`** + `cmd/compare` — two configs through the same backtest, replay (each from a flat book) and `cmd/stress` standard shocks (`evidence.RunStress` once per config), side by side (#186):
  - `DiffConfigs` lists the differing YAML keys first (flattened leaves; fixed values and `.env` are never compared), every report is headed `Disclaimer` ("Backtested/replayed, not a promise");
  - differing keys `cmd/stress` does not take are listed as `Stress.NotModelled` (`evidence.StressModels` — a new `StressArgs` flag needs its key in `stressKeys`).
  Reads files only; writes `data/compare/<time>/report.md|json`.
  A new figure in `backtest.Summary` or `replay.Result` worth comparing needs a line in `BacktestRun`/`ReplayRun` and `markdown.go`.
  `RunWindows` (`windows.go`, #301, `-windows week|day`) re-runs `Run` per ISO week or day (stress not windowed) into `Report.Windows`;
  - pure `Windows` and `SummarizeWindows` (wins, median, spread, worst window, replay/backtest order flips; a window that did not run is listed, counted nowhere).
  Never name a Go file `*_windows.go`/`*_windows_test.go`: Go builds it on Windows only.
