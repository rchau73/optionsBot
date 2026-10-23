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
	_, err := io.WriteString(w, b.String())
	return err
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
