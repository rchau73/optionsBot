# Strategy-change lab (E9)

A manager's parameter change is studied offline before anyone decides on it:

1. `cmd/compare` (#186, #301) runs the current and the proposed settings through the backtest, the replay of the recorded books and the stress shocks → `data/compare/<time>/report.md|json`.
   `-neighbours <key>` (#495) adds the fragility check: the proposed config re-run with that setting at −20 %, −10 %, +10 % and +20 % (rounded to the catalogue's step, clipped to its range; a value `Validate` refuses is skipped with the reason) and the return, worst week, drawdown and fees of each.
   A cliff is flagged where the worst week or drawdown moves by more than 1 pp of equity between adjacent values or the return flips sign (`compare.CliffPP`, its reason in `internal/compare/neighbours.go`).
2. `cmd/analyze` (#426) asks the model for a plain-words analysis of that `report.json`, checked number by number against it (`internal/aianalysis`, contract in `docs/ai-analysis.md`, #257) → `analysis.md|json` beside the report, or nothing when the check fails.

3. The monitor serves the runs read-only to managers and admins (#539, `frontend/docs/users-and-roles.md`): `GET /api/lab/runs` lists them newest first with the settings changed and the headline numbers in percentages (return, worst week, drawdown, fees), each labelled "backtested, not a forecast"; `GET /api/lab/runs/<id>` returns one run with its `report.json`.
   The AI analysis is not served: `analysis` says "withheld pending legal review" until counsel answers legal question L10 and the constant `L10_ANALYSIS_GATE_OPEN` (`frontend/lib/lab.js`) is changed in a reviewed PR.

No page shows them yet: that waits for E9's UI prototype. The AI never changes a setting, and neither tool nor route applies one: a change takes effect only after a manager confirms it.

- [`example-analysis.md`](example-analysis.md) — a saved `analysis.md` (percentages and counts only), kept passing the checker by `tests/aianalysis_generate_test.go`.
