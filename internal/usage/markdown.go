package usage

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Markdown writes the report for a person: every section names its source
// file and the span that file covers. Counts and shares only.
func Markdown(w io.Writer, r Report) error {
	var b strings.Builder
	title := r.Week
	if title == "" {
		title = r.From.Format(time.DateOnly) + " → " + r.To.Add(-time.Second).Format(time.DateOnly)
	}
	fmt.Fprintf(&b, "# Monitor usage — %s\n\n", title)
	fmt.Fprintf(&b, "Window: %s to %s UTC (end excluded). Counts and shares only: no user or session id.\n\n",
		r.From.Format(time.RFC3339), r.To.Format(time.RFC3339))

	src := sourceLine(r.Source)
	b.WriteString("## Source\n\n")
	b.WriteString(src + "\n\n")
	fmt.Fprintf(&b, "Lines read: %d · accepted: %d · rejected: %d (never used in a figure)\n\n",
		r.Source.Lines, r.Source.Accepted, r.Source.RejectedTotal)
	if r.Source.RejectedTotal > 0 {
		b.WriteString("| Rejected because | Lines |\n|---|---:|\n")
		for _, k := range sortedKeys(r.Source.Rejected) {
			fmt.Fprintf(&b, "| %s | %d |\n", k, r.Source.Rejected[k])
		}
		b.WriteString("\n")
	}

	if r.EventsInWeek == 0 {
		b.WriteString("## No events\n\n" + r.NoEvents + "\n\nNothing below is filled in.\n")
		_, err := io.WriteString(w, b.String())
		return err
	}
	fmt.Fprintf(&b, "Events in the window: %d\n\n", r.EventsInWeek)

	section := func(title, q string) {
		fmt.Fprintf(&b, "## %s (%s)\n\n%s\n\n", title, q, src)
	}

	if a := r.Active; a != nil {
		section("Active users", "Q1, Q2")
		b.WriteString("| Period | Users | " + strings.Join(roles, " | ") + " | " + strings.Join(append(append([]string{}, environments...), "unknown"), " | ") + " |\n")
		b.WriteString("|---|---:|" + strings.Repeat("---:|", len(roles)+len(environments)+1) + "\n")
		row := func(label string, c Count) {
			fmt.Fprintf(&b, "| %s | %d |", label, c.Users)
			for _, k := range roles {
				fmt.Fprintf(&b, " %d |", c.ByRole[k])
			}
			for _, k := range append(append([]string{}, environments...), "unknown") {
				fmt.Fprintf(&b, " %d |", c.ByEnvironment[k])
			}
			b.WriteString("\n")
		}
		for _, d := range a.Days {
			row(d.Day, d.Count)
		}
		row("**week**", a.Week)
		row(fmt.Sprintf("**%d days from %s**", MonthDays, a.MonthFrom.Format(time.DateOnly)), a.Month)
		b.WriteString("\nActive = at least one accepted event. A person whose role changed counts once under each role.\n\n")
	}

	if len(r.Cohorts) > 0 {
		section("Weekly cohort retention", "Q2")
		b.WriteString("Users whose first `session_started` fell in the week, and the share with any event in each later week (— = that week has not ended).\n\n")
		b.WriteString("| Cohort | Users |")
		for j := 1; j <= CohortWeeks; j++ {
			fmt.Fprintf(&b, " +%d |", j)
		}
		b.WriteString("\n|---|---:|" + strings.Repeat("---:|", CohortWeeks) + "\n")
		for _, c := range r.Cohorts {
			fmt.Fprintf(&b, "| %s | %d |", c.Week, c.Users)
			for j := 1; j <= CohortWeeks; j++ {
				cell := "—"
				for _, ret := range c.Retained {
					if ret.Offset == j {
						cell = fmt.Sprintf("%d (%s)", ret.Users, fmtPct(ret.Pct))
					}
				}
				b.WriteString(" " + cell + " |")
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if p := r.Panels; p != nil {
		section("What is opened", "Q3")
		countTable(&b, "Panel opened", p.Opens)
		never := "none"
		if len(p.NeverOpened) > 0 {
			never = strings.Join(p.NeverOpened, ", ")
		}
		fmt.Fprintf(&b, "Panels never opened in the window: %s\n\n", never)
		countTable(&b, "Drill level", p.Drill)
		countTable(&b, "Filter changed", p.Filters)
		countTable(&b, "Screen viewed", p.Screens)
	}

	if f := r.FirstAnswers; f != nil {
		section("First answers within the first session", "Q4")
		fmt.Fprintf(&b, "First sessions in the window: %d · with `first_answers_seen`: %d (%s)\n\n", f.FirstSessions, f.WithAnswers, fmtPct(f.Pct))
		med := "—"
		if f.MedianElapsedMs != nil {
			med = fmt.Sprintf("%d ms (%.1f min)", *f.MedianElapsedMs, float64(*f.MedianElapsedMs)/60000)
		}
		fmt.Fprintf(&b, "Median `elapsed_ms`: %s\n\n", med)
	}

	if h := r.Help; h != nil {
		section("Help", "Q5")
		countTable(&b, "Tip opened", h.Tips)
		countTable(&b, "Video played", h.VideosPlayed)
		countTable(&b, "Video completed", h.VideosCompleted)
		fmt.Fprintf(&b, "`help_resolved` answers: %d · helped: %d (%s)\n\n", h.Answers, h.Helped, fmtPct(h.HelpedPct))
	}

	if mc := r.ManualClose; mc != nil {
		section("Manual close funnel", "Q6")
		fmt.Fprintf(&b, "| Step | Count |\n|---|---:|\n| opened | %d |\n| … allowed | %d |\n| … not allowed | %d |\n| cancelled | %d |\n| confirmed | %d |\n| … last leg | %d |\n\n",
			mc.Opened, mc.OpenedAllowed, mc.OpenedNotAllowed, mc.Cancelled, mc.Confirmed, mc.ConfirmedLastLeg)
		countTable(&b, "Result outcome", mc.Results)
	}
	if rb := r.Rebalance; rb != nil {
		section("Rebalance funnel", "Q6")
		countTable(&b, "Preview outcome", rb.Previewed)
		countTable(&b, "Confirm outcome", rb.Confirmed)
	}

	if t := r.Trust; t != nil {
		section("Trust", "Q7")
		countTable(&b, "Stale data shown (source)", t.Stale)
		countTable(&b, "Signed out (reason)", t.SignedOut)
	}

	_, err := io.WriteString(w, b.String())
	return err
}

func sourceLine(s Source) string {
	file := s.File
	if file == "" {
		file = "(events)"
	}
	switch {
	case s.Missing:
		return fmt.Sprintf("Source: `%s` — the file does not exist.", file)
	case s.First == nil:
		return fmt.Sprintf("Source: `%s` — no accepted event in the file.", file)
	}
	return fmt.Sprintf("Source: `%s`, accepted events from %s to %s UTC.", file,
		s.First.Format(time.RFC3339), s.Last.Format(time.RFC3339))
}

func countTable(b *strings.Builder, head string, m map[string]int) {
	if len(m) == 0 {
		fmt.Fprintf(b, "%s: none\n\n", head)
		return
	}
	fmt.Fprintf(b, "| %s | Count |\n|---|---:|\n", head)
	for _, k := range sortedKeys(m) {
		fmt.Fprintf(b, "| %s | %d |\n", k, m[k])
	}
	b.WriteString("\n")
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func fmtPct(p *float64) string {
	if p == nil {
		return "—"
	}
	return fmt.Sprintf("%.0f %%", *p)
}
