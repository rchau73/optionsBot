# Strategy-change lab (E9)

A manager's parameter change is studied offline before anyone decides on it:

1. `cmd/compare` (#186, #301) runs the current and the proposed settings through the backtest, the replay of the recorded books and the stress shocks → `data/compare/<time>/report.md|json`.
2. `cmd/analyze` (#426) asks the model for a plain-words analysis of that `report.json`, checked number by number against it (`internal/aianalysis`, contract in `docs/ai-analysis.md`, #257) → `analysis.md|json` beside the report, or nothing when the check fails.

Nothing here is shown in the monitor: that waits for legal question L10 and E9's UI prototype. The AI never changes a setting.

- [`example-analysis.md`](example-analysis.md) — a saved `analysis.md` (percentages and counts only), kept passing the checker by `tests/aianalysis_generate_test.go`.
