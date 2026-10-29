# internal/perf

Folder notes for `CLAUDE.md` (the root file's trading rules apply here too).

- **`internal/perf`** — pure `Summarize(points, equity, from, to)` over `history.Point`s (#175):
  - net P&L in coin and USD, return % of the equity passed in (coin, from the account summary — never recomputed from P&L), hold-the-coin return over the same window (spot first → last),
    excess return, max drawdown, worst 7 days, and the UTC days with no point (reported, never filled in).
  Served by `/api/performance` (above) and used by `internal/evidence`.
  Pure `ReturnOnMargin(points, equity, from, to)` (`margin.go`, #278) is the PRD's engine-phase north star:
  - net P&L ÷ (time-weighted mean `im_pct`, each line ≤ `history.MaxSampleGap` as in `Utilisation`, × the equity passed in), in %,
    plus a 30-day rate (scaled by the span the net covers, downtime included, like the replay's window) and the measured hours;
  - `Known=false` with a `Reason` when no point carries `im_pct`, equity is unknown or mean IM is 0;
  - labelled `MarginApprox` (IM used ≈ IM % × balance at the window start).
  `ReturnOnMarginOf` gives the replay's equivalent.
  Not wired to the API yet; `internal/evidence` uses it (section "Return on margin", live and replay side by side; `evidence_week.py` posts it).
