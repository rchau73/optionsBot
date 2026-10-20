package evidence

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// WriteMarkdown writes the report as Markdown: one section per source,
// each saying where its figures come from.
func WriteMarkdown(w io.Writer, r Report) error {
	b := &strings.Builder{}
	u := strings.ToUpper(r.Bot)
	p := func(format string, a ...any) { fmt.Fprintf(b, format, a...) }
	last := r.To.Add(-time.Nanosecond)

	p("# Evidence report — %s %s, %s (%s → %s UTC)\n\n", r.Env, u, r.Week, r.From.Format(time.DateOnly), last.Format(time.DateOnly))
	p("Amounts in %s unless marked USD. Live figures are net of fees. ", u)
	if r.Equity > 0 {
		p("Equity at the start: %.4f %s (%s).\n\n", r.Equity, u, r.EquitySrc)
	} else {
		p("Equity at the start: **unknown** (%s) — percentages of equity and the replay are not available.\n\n", r.EquitySrc)
	}

	// Missing inputs first: a reader must know what the numbers stand on.
	p("## Missing inputs\n\n")
	missing := func(name string, days []string) {
		if len(days) == 0 {
			p("- %s: every day present\n", name)
		} else {
			p("- %s: **%d day(s) missing** — %s (the figures use the other days)\n", name, len(days), strings.Join(days, ", "))
		}
	}
	missing("P&L history", r.Missing.HistoryDays)
	missing("recorded books", r.Missing.BookDays)
	missing("minute GEX records", r.Missing.GEXDays)
	p("\n")

	l := r.Live
	p("## Live\n\nSource: %s; %s.\n\n", l.Source, l.PerfSource)
	p("- Journal: %d opens, %d closes · realised **%+.4f** (net of fees %.4f)", l.Opens, l.Closes, l.Realised, l.Fees)
	if l.Unrealised != nil {
		p(" · unrealised at the week's end %+.4f\n", *l.Unrealised)
	} else {
		p(" · unrealised at the week's end: no P&L history point\n")
	}
	if rs := l.reasons(); len(rs) > 0 {
		p("- Closes by reason:")
		for _, k := range rs {
			s := l.ByReason[k]
			p(" %s %d (%+.4f);", k, s.Closes, s.PnL)
		}
		p("\n")
	}
	s := l.Perf
	if s.Points == 0 {
		p("- P&L history: no point in the week\n")
	} else {
		p("- P&L history (%d points, %d of %d days): net **%+.4f** ($%+.0f at the last spot)", s.Points, s.DaysCovered, s.Days, s.NetPnL, s.NetPnLUSD)
		if s.EquityKnown {
			p(" = %+.2f %% of equity", s.ReturnPct)
		}
		p(" · max drawdown %.4f", s.MaxDrawdown)
		if s.EquityKnown {
			p(" (%.2f %%)", s.MaxDrawdownPct)
		}
		if s.WorstWeekKnown {
			p(" · worst 7 days %+.4f", s.WorstWeek)
		} else {
			p(" · worst 7 days: the history does not span 7 days")
		}
		p("\n")
	}
	p("\n")

	rp := r.Replay
	p("## Replay\n\nSource: the bot's rules (`internal/replay`, %s, direction %s) on %s, starting from the journal's book at the week's start.\n\n", rp.ConfigPath, rp.Direction, rp.Source)
	if !rp.Ran {
		p("Not run: %s.\n\n", rp.Note)
	} else {
		gex := "hourly GEX rebuilt from the books"
		if rp.GEXMinutes > 0 {
			gex = fmt.Sprintf("%d of them on minute GEX records", rp.GEXMinutes)
		}
		p("- %s → %s, %d decisions (%s); started with %d legs\n", rp.Start.Format("Jan 02 15:04"), rp.End.Format("Jan 02 15:04"), rp.Snapshots, gex, rp.StartLegs)
		p("- net **%+.4f** · realised %+.4f · fees %.4f · trades %d (entries %d, repairs %d) · sheds %d · stops %d · delta exits %d · rolls %d · MM cuts %d · unfilled %d · legs open at end %d\n",
			rp.Net, rp.Realised, rp.Fees, rp.Trades, rp.Entries, rp.Repairs, rp.Sheds, rp.Stops, rp.DeltaExits, rp.Rolls, rp.MMCuts, rp.Unfilled, rp.OpenAtEnd)
		p("- worst drawdown %.4f · IM max %.1f %% avg %.1f %% (MODEL margin)\n\n", rp.MaxDrawdown, rp.MaxIMPct, rp.AvgIMPct)
	}

	g := r.Gap
	p("## Gap (replay − live)\n\n")
	if !g.Known {
		p("Not available: the replay did not run.\n\n")
	} else {
		p("Source: %s, over the replay's window.\n\n", g.Source)
		p("- live **%+.4f** = realised %+.4f + unrealised %+.4f → %+.4f (%d opens, %d closes; %d leg marks without a recorded quote)\n",
			g.Live.Net, g.Live.Realised, g.Live.Unrealised0, g.Live.Unrealised1, g.Live.Opens, g.Live.Closes, g.Live.Unmarked)
		p("- replay %+.4f · **gap %+.4f**\n", g.Replay, g.Gap)
		p("- not modelled by the replay: %s\n\n", strings.Join(g.Unmodelled, "; "))
	}

	st := r.Stress
	p("## Stress\n\nSource: %s", st.Source)
	if st.BookAt != nil {
		p(", book captured %s UTC", st.BookAt.UTC().Format("2006-01-02 15:04"))
		if st.Stale {
			p(" — **not the week-end book** (re-run `scripts/stress_inputs.sh %s` at the week's end)", r.Bot)
		}
	}
	p(". MODEL prices, margin and fills; one path per shock.\n\n")
	if len(st.Outcomes) == 0 {
		p("Not run: %s.\n\n", st.Note)
	} else {
		p("| Shock | vs holding %s | vs holding USD | Equity change %% | Realised | Stops | Delta exits | Rolls | Worst MM %% | Worst equity %% |\n", u)
		p("|---|---|---|---|---|---|---|---|---|---|\n")
		for _, o := range st.Outcomes {
			if o.Error != "" {
				p("| %s | failed: %s | | | | | | | | |\n", o.Shock, strings.ReplaceAll(o.Error, "|", "/"))
				continue
			}
			p("| %s | %+.4f | %+.0f | %+.2f | %+.4f | %d | %d | %d | %.1f | %.1f |\n",
				o.Shock, o.VsHoldCoin, o.VsHoldUSD, o.ReturnPct, o.Realised, o.Stops, o.DeltaExits, o.Rolls, o.WorstMMPct, o.WorstEquityPct)
		}
		p("\n")
	}

	h := r.Hold
	p("## Benchmark: holding the coin\n\nSource: %s.\n\n", h.Source)
	if !h.Known {
		p("Not available: no P&L history point with a spot in the week.\n\n")
	} else {
		p("- %s %.0f → %.0f: holding **%+.2f %%** in USD (0 %% in coin)\n", u, h.SpotStart, h.SpotEnd, h.HoldReturnUSDPct)
		if h.StrategyKnown {
			p("- the strategy in USD: %+.2f %% · **excess over holding %+.2f pp**\n\n", h.ReturnUSDPct, h.ExcessReturnPct)
		} else {
			p("- the strategy in USD: needs the starting equity\n\n")
		}
	}

	ut := r.Util.Stats
	p("## Margin utilisation\n\nSource: %s (`history.Utilisation`, the figure of `/api/status`).\n\n", r.Util.Source)
	if ut.MeanPct == nil {
		p("- mean IM ÷ limit while entries were allowed: no allowed time measured")
	} else {
		p("- mean IM ÷ limit while entries were allowed: **%.1f %%**", *ut.MeanPct)
	}
	if ut.AllowedShare != nil {
		p(" · allowed %.0f %% of %.1f measured hours", *ut.AllowedShare*100, ut.MeasuredHours)
	}
	p("\n")

	_, err := io.WriteString(w, b.String())
	return err
}
