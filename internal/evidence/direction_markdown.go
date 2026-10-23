package evidence

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// WriteDirectionMarkdown writes the scorecard as Markdown: the live rows and
// the replay rows in two tables, each row naming its files. Facts only.
func WriteDirectionMarkdown(w io.Writer, r DirectionReport) error {
	b := &strings.Builder{}
	p := func(format string, a ...any) { fmt.Fprintf(b, format, a...) }
	u := strings.ToUpper(r.Bot)
	last := r.To.Add(-time.Nanosecond)

	p("# Direction trial scorecard — %s, %s (%s → %s UTC)\n\n", u, r.Week, r.From.Format(time.DateOnly), last.Format(time.DateOnly))
	p("The GEX direction sources side by side: `bias` and `sma`. Units: %s. Facts only; a person decides the trial.\n\n", r.Units)

	p("## Missing inputs\n\n")
	missing := func(name string, days []string) {
		if len(days) == 0 {
			p("- %s: every day present\n", name)
		} else {
			p("- %s: **%d day(s) missing** — %s (not filled in: the replay uses the other days)\n", name, len(days), strings.Join(days, ", "))
		}
	}
	missing("recorded books", r.Missing.BookDays)
	missing("minute GEX records", r.Missing.GEXDays)
	p("\n")

	p("## Live — %s\n\n", LiveRowLabel)
	p("| Source | Env | GEX sheds | Shed repairs | Stop-losses | Shed calls h | Shed puts h | History covers h | Realised net %% | Marked net %% | Replay net %% | Live − replay pp |\n")
	p("|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, l := range r.Live {
		p("| %s | %s | %d | %d | %d | %.1f | %.1f | %.1f | %s | %s | %s | %s |\n", l.Source, l.Env, l.Sheds, l.Repairs, l.Stops,
			l.Shed.Calls, l.Shed.Puts, l.Shed.Covered, pctCell(l.RealisedPct), pctCell(l.MarkedPct), pctCell(l.ReplayPct), pctCell(l.GapPP))
	}
	p("\n")
	for _, l := range r.Live {
		p("- **%s (%s)** — sources: %s; equity: %s", l.Source, l.Env, strings.Join(l.Files, ", "), l.EquitySource)
		if !l.EquityKnown {
			p(" (**unknown**: no percentage)")
		}
		p("\n")
		if len(l.BySlotPct) > 0 {
			keys := make([]string, 0, len(l.BySlotPct))
			for k := range l.BySlotPct {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			p("  - realised net by slot:")
			for _, k := range keys {
				p(" %s %+.2f %%;", k, l.BySlotPct[k])
			}
			p("\n")
		}
		if l.Shadow.Source != "" {
			p("  - shadow: %s would have shed calls %.1f h, puts %.1f h (live action, unconfirmed)\n", l.Shadow.Source, l.Shadow.Calls, l.Shadow.Puts)
		}
		if l.GapSource != "" {
			p("  - marked net: %s\n", l.GapSource)
		}
	}
	p("\n")

	p("## Replay — %s\n\n", ReplayRowLabel)
	p("| Source | Ran | Start legs | Books read | Decisions (minute GEX) | Sheds | Repairs | Stops | Net %% | Max drawdown %% | IM max %% |\n")
	p("|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, rp := range r.Replay {
		if !rp.Ran {
			p("| %s | not run: %s | | | | | | | | | |\n", rp.Source, strings.ReplaceAll(rp.Note, "|", "/"))
			continue
		}
		p("| %s | %s → %s | %d | %d | %d (%d) | %d | %d | %d | %s | %s | %.1f |\n", rp.Source,
			rp.Start.Format("Jan 02 15:04"), rp.End.Format("Jan 02 15:04"), rp.StartLegs, rp.BooksRead,
			rp.Decisions, rp.GEXMinutes, rp.Sheds, rp.Repairs, rp.Stops, pctCell(rp.NetPct), pctCell(rp.MaxDDPct), rp.MaxIMPct)
	}
	p("\n")
	for _, rp := range r.Replay {
		p("- **%s** — sources: %s\n", rp.Source, strings.Join(rp.Files, ", "))
	}
	p("- both replays start from the same book and read the same recorded books; only `gex_direction` differs. Not modelled: %s\n", strings.Join(Unmodelled, "; "))

	_, err := io.WriteString(w, b.String())
	return err
}

func pctCell(v *float64) string {
	if v == nil {
		return "n/a"
	}
	return fmt.Sprintf("%+.2f", *v)
}
