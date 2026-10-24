package evidence

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"optionsbot/internal/perf"
)

// WriteTrackMarkdown writes the track record: the whole span first, then
// one row per week and the counts. Every figure names its source.
func WriteTrackMarkdown(w io.Writer, t TrackRecord) error {
	b := &strings.Builder{}
	p := func(format string, a ...any) { fmt.Fprintf(b, format, a...) }
	u := strings.ToUpper(t.Bot)

	p("# Track record — %s %s", t.Env, u)
	if t.Known {
		p(", %s → %s UTC", t.From.Format(time.DateOnly), t.To.Format(time.DateOnly))
	}
	p("\n\n**%s.** Past figures, net of fees. Account: %s.\n\n", t.Disclaimer, t.Account)
	p("Span: %s (%s). Weekly rows: %s. Amounts in %s unless marked USD. ", t.History, t.Inception, t.Weekly, u)
	if t.Equity > 0 {
		p("Equity at the start: %.4f %s (%s).\n\n", t.Equity, u, t.EquitySource)
	} else {
		p("Equity at the start: **unknown** (%s) — every %% of equity is unknown.\n\n", t.EquitySource)
	}
	if !t.Known {
		p("No P&L history point: nothing to report.\n")
		_, err := io.WriteString(w, b.String())
		return err
	}

	p("## Missing days\n\n")
	if len(t.MissingDays) == 0 {
		p("Every UTC day of the span has a P&L history point.\n\n")
	} else {
		p("**%d UTC day(s) with no P&L history point** — %s. Nothing is interpolated: the figures span them from the points around.\n\n",
			len(t.MissingDays), strings.Join(t.MissingDays, ", "))
	}

	s := t.Perf
	known := s.EquityKnown
	p("## Whole span\n\nSource: `perf.Summarize` over %s, first point to last (%d points, %d of %d days).\n\n", t.History, s.Points, s.DaysCovered, s.Days)
	p("- net **%+.4f** ($%+.0f at the last spot) · %s of equity\n", s.NetPnL, s.NetPnLUSD, pctOrUnknown(s.ReturnPct, known))
	if s.HoldKnown {
		p("- holding the coin: %s %.0f → %.0f, **%+.2f %%** in USD · the strategy in USD %s · excess over holding %s\n",
			u, s.SpotStart, s.SpotEnd, s.HoldReturnUSDPct, pctOrUnknown(s.ReturnUSDPct, known), ppOrUnknown(s.ExcessReturnPct, known))
	} else {
		p("- holding the coin: no point with a spot\n")
	}
	p("- max drawdown %.4f (%s)", s.MaxDrawdown, pctOrUnknown(s.MaxDrawdownPct, known))
	if s.WorstWeekKnown {
		p(" · worst 7 days %+.4f (%s), %s → %s\n", s.WorstWeek, pctOrUnknown(s.WorstWeekPct, known),
			s.WorstWeekStart.UTC().Format("Jan 02 15:04"), s.WorstWeekEnd.UTC().Format("Jan 02 15:04"))
	} else {
		p(" · worst 7 days: the history does not span 7 days\n")
	}
	writeROMLine(p, "return on margin", t.ROM)
	if t.Util.MeanPct != nil {
		p("- margin utilisation (`history.Utilisation`): mean IM ÷ limit while entries were allowed **%.1f %%**\n", *t.Util.MeanPct)
	} else {
		p("- margin utilisation (`history.Utilisation`): no allowed time measured\n")
	}
	p("\n")

	p("## Weeks\n\nOne row per ISO week of the span, read from its weekly report; %% of the margin balance at that week's start. ")
	p("\"%s\": no weekly report for the week (not recomputed). \"%s\": the report predates the field. \"unknown\": the report has no value (e.g. equity unknown).\n\n", NotReported, NotMeasured)
	p("| Week | Net %s | Net %% | Net USD %% | Hold %% | Excess pp | Max DD %% | RoM live %% | RoM replay %% | Gap %s | Gap %% | Utilisation %% | Days missing |\n", u, u)
	p("|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range t.Weeks {
		if !r.Reported {
			p("| %s | %s | | | | | | | | | | | |\n", r.Week, NotReported)
			continue
		}
		p("| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", r.Week,
			coinCell(r.Net), cell(r, r.NetPct, ""), cell(r, r.NetUSDPct, "hold"), cell(r, r.HoldPct, "hold"), cell(r, r.ExcessPP, "hold"),
			cell(r, r.MaxDDPct, ""), cell(r, r.ROMLivePct, "rom_live"), cell(r, r.ROMReplPct, "rom_replay"),
			coinCellOr(r, r.Gap, "gap"), cell(r, r.GapPct, "gap"), unsignedCell(r, r.UtilPct, "utilisation"), daysCell(r.MissingDays))
	}
	p("\n")
	writeCounts(p, t.Counts, "net in coin")
	writeUnreadable(p, t.Unreadable)

	_, err := io.WriteString(w, b.String())
	return err
}

// WriteTrendMarkdown writes the trend: percentages only.
func WriteTrendMarkdown(w io.Writer, tr Trend) error {
	b := &strings.Builder{}
	p := func(format string, a ...any) { fmt.Fprintf(b, format, a...) }
	p("# Evidence trend — %s %s\n\n", tr.Env, strings.ToUpper(tr.Bot))
	p("**%s.** Past figures, net of fees. Percentages only, of the margin balance at each week's start. Source: %s.\n\n", tr.Disclaimer, tr.Weekly)
	if len(tr.Weeks) == 0 {
		p("No weekly report found.\n")
		_, err := io.WriteString(w, b.String())
		return err
	}
	var missing []string
	for _, r := range tr.Weeks {
		if !r.Reported {
			missing = append(missing, r.Week)
		}
	}
	if len(missing) > 0 {
		p("**Weeks with no report:** %s (listed, never filled in).\n\n", strings.Join(missing, ", "))
	}
	p("\"%s\": the report predates the field. \"unknown\": the report has no value (e.g. equity unknown).\n\n", NotMeasured)
	p("| Week | Net %% | Net USD %% | RoM live %% | RoM replay %% | Gap (replay − live) %% | Hold %% | Excess pp | Max DD %% | Utilisation %% | Days missing |\n")
	p("|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range tr.Weeks {
		if !r.Reported {
			p("| %s | %s | | | | | | | | | |\n", r.Week, NotReported)
			continue
		}
		p("| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", r.Week,
			cell(r, r.NetPct, ""), cell(r, r.NetUSDPct, "hold"), cell(r, r.ROMLivePct, "rom_live"), cell(r, r.ROMReplPct, "rom_replay"),
			cell(r, r.GapPct, "gap"), cell(r, r.HoldPct, "hold"), cell(r, r.ExcessPP, "hold"), cell(r, r.MaxDDPct, ""),
			unsignedCell(r, r.UtilPct, "utilisation"), daysCell(r.MissingDays))
	}
	p("\n")
	writeCounts(p, tr.Counts, "net %")
	writeUnreadable(p, tr.Unreadable)
	_, err := io.WriteString(w, b.String())
	return err
}

func writeROMLine(p func(string, ...any), name string, m perf.MarginReturn) {
	if !m.Known {
		p("- %s: not known — %s\n", name, m.Reason)
		return
	}
	p("- %s (%s): **%+.2f %%** of a mean IM used of %.4f (%.1f %% of equity) · %+.2f %% per 30 days over %.1f h\n",
		name, perf.MarginApprox, m.ReturnPct, m.MeanIM, m.MeanIMPct, m.Return30dPct, m.SpanHours)
}

func writeCounts(p func(string, ...any), c Counts, lostBy string) {
	p("## Counts\n\nFacts over the reported weeks, no forecast.\n\n")
	p("- weeks reported %d · not reported %d\n", c.Reported, c.NotReported)
	p("- weeks beating holding the coin: %d of %d with the excess known\n", c.BeatHold, c.HoldKnown)
	p("- weeks lost (%s < 0): %d · longest losing streak %d (a week not reported, or without a net, ends a streak)\n", lostBy, c.Lost, c.LongestLosing)
	if c.WorstWeek != "" {
		p("- worst week: %s", c.WorstWeek)
		if c.WorstWeekPct != nil {
			p(" (%+.2f %%)", *c.WorstWeekPct)
		}
		p("\n")
	}
	p("\n")
}

func writeUnreadable(p func(string, ...any), files []string) {
	if len(files) > 0 {
		p("Weekly files not read (their weeks show as not reported): %s\n", strings.Join(files, ", "))
	}
}

// cell is a signed % of a reported row: the value, NotMeasured when the
// report lacks field, else "unknown".
func cell(r WeekRow, v *float64, field string) string {
	if v != nil {
		return fmt.Sprintf("%+.2f", *v)
	}
	if field != "" && slices.Contains(r.NotMeasured, field) {
		return NotMeasured
	}
	return "unknown"
}

func unsignedCell(r WeekRow, v *float64, field string) string {
	if v != nil {
		return fmt.Sprintf("%.1f", *v)
	}
	return cell(r, nil, field)
}

func coinCell(v *float64) string {
	if v == nil {
		return "no point"
	}
	return fmt.Sprintf("%+.4f", *v)
}

func coinCellOr(r WeekRow, v *float64, field string) string {
	if v != nil {
		return fmt.Sprintf("%+.4f", *v)
	}
	return cell(r, nil, field)
}

func daysCell(days []string) string {
	if len(days) == 0 {
		return "0"
	}
	return fmt.Sprintf("%d (%s)", len(days), strings.Join(days, ", "))
}

func pctOrUnknown(v float64, known bool) string {
	if !known {
		return "unknown %"
	}
	return fmt.Sprintf("%+.2f %%", v)
}

func ppOrUnknown(v float64, known bool) string {
	if !known {
		return "unknown"
	}
	return fmt.Sprintf("%+.2f pp", v)
}
