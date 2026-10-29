package compare

import (
	"fmt"
	"io"
	"strings"
	"time"

	"optionsbot/internal/evidence"
)

// WriteMarkdown writes the report as Markdown: the disclaimer, the settings
// that differ, then one side-by-side table per source.
func WriteMarkdown(w io.Writer, r Report) error {
	b := &strings.Builder{}
	p := func(format string, a ...any) { fmt.Fprintf(b, format, a...) }
	u := strings.ToUpper(r.Underlying)

	p("# Settings comparison — %s, %s → %s UTC\n\n", u, r.From.Format(time.DateOnly), r.To.Format(time.DateOnly))
	p("> **%s**\n\n", r.Disclaimer)
	p("- Base: `%s`\n- Proposed: `%s`\n\n", r.Base, r.Proposed)

	p("## Differences\n\n")
	if len(r.Diffs) == 0 {
		p("None: the two configs give the same settings, so every figure below is the same.\n\n")
	}
	for _, d := range r.Diffs {
		p("- `%s` %s → %s\n", d.Key, d.Base, d.Proposed)
	}
	if len(r.Diffs) > 0 {
		p("\n")
	}

	bt := r.Backtest
	p("## Backtest (USD)\n\n")
	if !bt.Ran {
		p("Not run: %s.\n\n", orNote(bt.Note, bt.Base.Error, bt.Proposed.Error))
	} else {
		p("Source: %s · daily engine, starting equity $%.0f · IM limit approximated (`backtest.ApproxIMLimitPct`).\n\n", bt.Source, bt.EquityUSD)
		t := newTable(b)
		t.errors(bt.Base.Error, bt.Proposed.Error)
		t.num("Net (equity change)", "%+.2f", bt.Base.NetUSD, bt.Proposed.NetUSD)
		t.num("Return %", "%+.2f", bt.Base.ReturnPct, bt.Proposed.ReturnPct)
		t.num("Realised (net of fees)", "%+.2f", bt.Base.RealisedUSD, bt.Proposed.RealisedUSD)
		t.num("Fees", "%.2f", bt.Base.FeesUSD, bt.Proposed.FeesUSD)
		t.num("Worst week", "%+.2f", bt.Base.WorstWeekUSD, bt.Proposed.WorstWeekUSD)
		t.num("Worst week %", "%+.2f", bt.Base.WorstWeekPct, bt.Proposed.WorstWeekPct)
		t.num("Max drawdown", "%.2f", bt.Base.MaxDrawdownUSD, bt.Proposed.MaxDrawdownUSD)
		t.num("Max drawdown %", "%.2f", bt.Base.MaxDrawdownPct, bt.Proposed.MaxDrawdownPct)
		t.count("Trades", bt.Base.Trades, bt.Proposed.Trades)
		t.count("GEX sheds", bt.Base.Sheds, bt.Proposed.Sheds)
		t.count("Stops", bt.Base.Stops, bt.Proposed.Stops)
		t.count("Delta exits", bt.Base.DeltaExits, bt.Proposed.DeltaExits)
		t.count("Rolls", bt.Base.Rolls, bt.Proposed.Rolls)
		t.num("Sharpe", "%.2f", bt.Base.Sharpe, bt.Proposed.Sharpe)
		p("\n")
	}

	rp := r.Replay
	p("## Replay of recorded books (%s)\n\n", u)
	if !rp.Ran {
		p("Not run: %s.\n\n", orNote(rp.Note, rp.Base.Error, rp.Proposed.Error))
	} else {
		p("Source: %s · the bot's rules on recorded mainnet books, each from a flat book · starting margin balance %.4f %s (%s) · margin and fills modelled; not modelled: %s.\n\n",
			rp.Source, rp.Equity, u, rp.EquitySource, strings.Join(evidence.Unmodelled, ", "))
		t := newTable(b)
		t.errors(rp.Base.Error, rp.Proposed.Error)
		t.count("Decisions", rp.Base.Decisions, rp.Proposed.Decisions)
		t.num("Net (realised + open)", "%+.4f", rp.Base.Net, rp.Proposed.Net)
		t.num("Return %", "%+.2f", rp.Base.ReturnPct, rp.Proposed.ReturnPct)
		t.num("Realised", "%+.4f", rp.Base.Realised, rp.Proposed.Realised)
		t.num("Fees", "%.4f", rp.Base.Fees, rp.Proposed.Fees)
		t.num("Worst week", "%+.4f", rp.Base.WorstWeek, rp.Proposed.WorstWeek)
		t.num("Worst week %", "%+.2f", rp.Base.WorstWeekPct, rp.Proposed.WorstWeekPct)
		t.num("Max drawdown", "%.4f", rp.Base.MaxDrawdown, rp.Proposed.MaxDrawdown)
		t.count("Trades", rp.Base.Trades, rp.Proposed.Trades)
		t.count("Entries", rp.Base.Entries, rp.Proposed.Entries)
		t.count("Repairs", rp.Base.Repairs, rp.Proposed.Repairs)
		t.count("GEX sheds", rp.Base.Sheds, rp.Proposed.Sheds)
		t.count("Stops", rp.Base.Stops, rp.Proposed.Stops)
		t.count("Delta exits", rp.Base.DeltaExits, rp.Proposed.DeltaExits)
		t.count("Rolls", rp.Base.Rolls, rp.Proposed.Rolls)
		t.count("MM cuts", rp.Base.MMCuts, rp.Proposed.MMCuts)
		t.num("IM max %", "%.1f", rp.Base.MaxIMPct, rp.Proposed.MaxIMPct)
		t.num("IM avg %", "%.1f", rp.Base.AvgIMPct, rp.Proposed.AvgIMPct)
		p("\n")
	}

	st := r.Stress
	p("## Stress shocks (%s)\n\n", u)
	if !st.Ran {
		p("Not run: %s.\n\n", orNote(st.Note, stressErr(st.Base), stressErr(st.Proposed)))
	} else {
		p("Source: %s", st.Source)
		if st.BookAt != nil {
			p(" · book captured %s UTC", st.BookAt.Format("2006-01-02 15:04"))
		}
		p(" · scripted paths, modelled prices, fills and fees.\n\n")
		p("| Shock | Return %% base | proposed | vs hold base | proposed | Realised base | proposed | Stops base | proposed | Worst equity %% base | proposed | Worst MM %% base | proposed |\n")
		p("|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
		prop := map[string]evidence.StressOutcome{}
		for _, o := range st.Proposed {
			prop[o.Shock] = o
		}
		for _, a := range st.Base {
			o := prop[a.Shock]
			if a.Error != "" || o.Error != "" {
				p("| %s | error: %s | | | | | | | | | | | |\n", a.Shock, orNote("", a.Error, o.Error))
				continue
			}
			p("| %s | %+.2f | %+.2f | %+.4f | %+.4f | %+.4f | %+.4f | %d | %d | %+.1f | %+.1f | %.0f | %.0f |\n", a.Shock,
				a.ReturnPct, o.ReturnPct, a.VsHoldCoin, o.VsHoldCoin, a.Realised, o.Realised, a.Stops, o.Stops,
				a.WorstEquityPct, o.WorstEquityPct, a.WorstMMPct, o.WorstMMPct)
		}
		p("\n")
	}
	if len(st.NotModelled) > 0 {
		p("Not modelled by the stress (cmd/stress does not take them, so they cannot change a row above): `%s`.\n\n",
			strings.Join(st.NotModelled, "`, `"))
	}
	if r.Windows != nil {
		writeWindows(b, *r.Windows, u)
	}
	if r.Neighbours != nil {
		writeNeighbours(b, *r.Neighbours, u)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// writeWindows writes the "By week" (or "By day") section: one table per
// source, then its summary. A window a source did not run in shows its note
// and is listed as missing, never filled.
func writeWindows(b *strings.Builder, wr WindowReport, u string) {
	p := func(format string, a ...any) { fmt.Fprintf(b, format, a...) }
	p("## By %s\n\n", wr.Unit)
	p("Each window runs the backtest and the replay on its own (the replay from a flat book at the window's first day); %s.\n\n", wr.StressNote)
	sources := []struct {
		name, unit, format string
		side               func(WindowRow) WindowSide
		sum                SourceSummary
	}{
		{"Replay", u, "%+.4f", func(r WindowRow) WindowSide { return r.Replay }, wr.Summary.Replay},
		{"Backtest", "USD", "%+.2f", func(r WindowRow) WindowSide { return r.Backtest }, wr.Summary.Backtest},
	}
	for _, s := range sources {
		p("### %s (%s)\n\n", s.name, s.unit)
		p("| Window | Days | Net base | proposed | Change | Return %% base | proposed | Change | Worst week %% base | proposed | Max drawdown base | proposed | Note |\n")
		p("|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
		for _, r := range wr.Rows {
			days := r.From.Format("01-02") + " → " + r.To.Format("01-02")
			sd := s.side(r)
			if !sd.Ran {
				p("| %s | %s | | | | | | | | | | | %s |\n", r.Label, days, sd.Note)
				continue
			}
			p("| %s | %s | %s | %s | %s | %+.2f | %+.2f | %+.2f | %+.2f | %+.2f | %s | %s | |\n", r.Label, days,
				fmt.Sprintf(s.format, sd.Base.Net), fmt.Sprintf(s.format, sd.Proposed.Net), fmt.Sprintf(s.format, sd.Diff.Net),
				sd.Base.ReturnPct, sd.Proposed.ReturnPct, sd.Diff.ReturnPct, sd.Base.WorstWeekPct, sd.Proposed.WorstWeekPct,
				strings.TrimPrefix(fmt.Sprintf(s.format, sd.Base.MaxDrawdown), "+"), strings.TrimPrefix(fmt.Sprintf(s.format, sd.Proposed.MaxDrawdown), "+"))
		}
		p("\n")
		sum := s.sum
		if sum.Run == 0 {
			p("Summary: no window ran.\n\n")
		} else {
			p("Summary: the proposal's net is higher in **%d of %d** windows (base higher in %d, equal in %d) · median change %s (%+.2f pp of return) · spread of the change %s to %s",
				sum.ProposedWins, sum.Run, sum.BaseWins, sum.Run-sum.ProposedWins-sum.BaseWins,
				fmt.Sprintf(s.format, sum.MedianDiff), sum.MedianDiffPP, fmt.Sprintf(s.format, sum.WorstDiff), fmt.Sprintf(s.format, sum.BestDiff))
			p(" · worst window base %s (%s), proposed %s (%s).\n\n", sum.WorstBase.Label, fmt.Sprintf(s.format, sum.WorstBase.Net),
				sum.WorstProp.Label, fmt.Sprintf(s.format, sum.WorstProp.Net))
		}
		if len(sum.Missing) > 0 {
			p("Not run (left out of the summary): %s.\n\n", strings.Join(sum.Missing, ", "))
		}
	}
	if len(wr.Summary.OrderFlips) > 0 {
		p("Order flips between replay and backtest (one has the proposal ahead, the other behind): %s.\n\n", strings.Join(wr.Summary.OrderFlips, ", "))
	} else {
		p("Order flips between replay and backtest: none.\n\n")
	}
}

// writeNeighbours writes the "Neighbours" section: one table per source
// (a skipped value shows why), then the cliffs.
func writeNeighbours(b *strings.Builder, nb Neighbours, u string) {
	p := func(format string, a ...any) { fmt.Fprintf(b, format, a...) }
	unit := ""
	if nb.Unit != "" {
		unit = " " + nb.Unit
	}
	p("## Neighbours of `%s` (fragility)\n\n", nb.Key)
	p("%s: the proposed config re-run with only this setting moved −20 %%, −10 %%, +10 %% and +20 %% of %s%s (the catalogue's range: %s). ",
		nb.Title, fmtValue(nb.Proposed), unit, nb.Range)
	p("Backtested and replayed, not a promise: the replay does not model %s; %s. ", strings.Join(evidence.Unmodelled, ", "), nb.StressNote)
	p("A cliff is a worst week or max drawdown that moves by more than %g pp of equity between adjacent values, or a return that flips sign. ", nb.CliffThresholdPP)
	p("Nothing is applied: a change takes effect only after a manager confirms it.\n\n")
	sources := []struct {
		name, fees, format string
		of                 func(NeighbourRow) NeighbourFigures
	}{
		{"Backtest", "USD", "%.2f", func(r NeighbourRow) NeighbourFigures { return r.Backtest }},
		{"Replay", u, "%.4f", func(r NeighbourRow) NeighbourFigures { return r.Replay }},
	}
	for _, s := range sources {
		p("### %s\n\n", s.name)
		p("| Step | Value | Return %% | Worst week %% | Max drawdown %% | Fees (%s) | Trades | Note |\n", s.fees)
		p("|---|---|---|---|---|---|---|---|\n")
		for _, r := range nb.Rows {
			step := fmt.Sprintf("%+g %%", r.StepPct)
			if r.StepPct == 0 {
				step = "proposed"
			}
			f := s.of(r)
			switch {
			case r.Skipped != "":
				p("| %s | %s | | | | | | skipped: %s |\n", step, fmtValue(r.Value), r.Skipped)
			case !f.Ran:
				p("| %s | %s | | | | | | not run: %s |\n", step, fmtValue(r.Value), f.Note)
			default:
				p("| %s | %s | %+.2f | %+.2f | %.2f | %s | %d | %s |\n", step, fmtValue(r.Value), f.ReturnPct, f.WorstWeekPct,
					f.MaxDrawdownPct, fmt.Sprintf(s.format, f.Fees), f.Trades, r.Note)
			}
		}
		p("\n")
	}
	if len(nb.Cliffs) == 0 {
		p("Cliffs: none between the values that ran.\n\n")
		return
	}
	p("**Cliffs** (the result changes sharply between neighbours):\n\n")
	for _, c := range nb.Cliffs {
		p("- ⚠ %s.\n", c.Detail)
	}
	p("\n")
}

// table writes "| Figure | Base | Proposed | Change |" rows; the header is
// written with the first row.
type table struct {
	b      *strings.Builder
	header bool
}

func newTable(b *strings.Builder) *table { return &table{b: b} }

func (t *table) row(name, base, proposed, change string) {
	if !t.header {
		t.b.WriteString("| Figure | Base | Proposed | Change |\n|---|---|---|---|\n")
		t.header = true
	}
	fmt.Fprintf(t.b, "| %s | %s | %s | %s |\n", name, base, proposed, change)
}

func (t *table) num(name, format string, a, b float64) {
	sa, sb := fmt.Sprintf(format, a), fmt.Sprintf(format, b)
	change := "="
	if sa != sb {
		change = fmt.Sprintf(signed(format), b-a)
	}
	t.row(name, sa, sb, change)
}

func (t *table) count(name string, a, b int) {
	change := "="
	if a != b {
		change = fmt.Sprintf("%+d", b-a)
	}
	t.row(name, fmt.Sprint(a), fmt.Sprint(b), change)
}

func (t *table) errors(a, b string) {
	if a != "" || b != "" {
		t.row("Error", orDash(a), orDash(b), "")
	}
}

// signed makes a format print its sign ("%.2f" → "%+.2f").
func signed(format string) string {
	if strings.HasPrefix(format, "%+") {
		return format
	}
	return "%+" + strings.TrimPrefix(format, "%")
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// orNote is the first non-empty of its arguments.
func orNote(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return "unknown"
}

func stressErr(outcomes []evidence.StressOutcome) string {
	for _, o := range outcomes {
		if o.Error != "" {
			return o.Shock + ": " + o.Error
		}
	}
	return ""
}
