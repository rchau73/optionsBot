package coverage

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// maxGapsShown is how many gaps the text report lists per dataset (the
// longest first); -json lists them all.
const maxGapsShown = 10

// Format prints the coverage of every dataset: counts, hours and dates only.
func Format(w io.Writer, r Result) error {
	const ts = "2006-01-02 15:04"
	fmt.Fprintf(w, "Recorded-history coverage — %s — %s → %s UTC (end excluded)\n", r.Dir, r.From.Format(ts), r.To.Format(ts))
	for _, d := range r.Datasets {
		fmt.Fprintf(w, "\n%s (%s, one record every %s)\n", d.Dataset, d.Path, stepText(d.StepMinutes))
		if !d.Found {
			fmt.Fprintln(w, "  not found: the whole window is missing")
			continue
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if d.Records > 0 {
			fmt.Fprintf(tw, "  records\t%d, first %s, last %s\n", d.Records, d.First.Format(ts), d.Last.Format(ts))
		} else {
			fmt.Fprintf(tw, "  records\tnone in the window\n")
		}
		fmt.Fprintf(tw, "  covered\t%d of %d bins (%.1f %%), %.1f h missing\n", d.CoveredBins, d.Bins, d.CoveredPct, d.MissingHours)
		fmt.Fprintf(tw, "  days\t%d of %d with records, %d complete\n", d.DaysCovered, d.DaysInWindow, d.DaysComplete)
		fmt.Fprintf(tw, "  missing days\t%s\n", list(d.MissingDays))
		if g := d.Longest(); g.Hours > 0 {
			fmt.Fprintf(tw, "  longest gap\t%s (%s → %s)\n", hours(g.Hours), g.From.Format(ts), g.To.Format(ts))
		} else {
			fmt.Fprintf(tw, "  longest gap\tnone\n")
		}
		if err := tw.Flush(); err != nil {
			return err
		}

		var partial []string
		for _, day := range d.Days {
			if day.Records > 0 && day.MissingBins > 0 {
				partial = append(partial, fmt.Sprintf("%s %s", day.Date, hours(day.MissingHours)))
			}
		}
		if len(partial) > 0 {
			fmt.Fprintf(w, "  days with missing hours: %s\n", strings.Join(partial, ", "))
		}
		if len(d.Gaps) > 0 {
			gaps := append([]Gap{}, d.Gaps...)
			sort.SliceStable(gaps, func(i, j int) bool { return gaps[i].Hours > gaps[j].Hours })
			shown := min(len(gaps), maxGapsShown)
			fmt.Fprintf(w, "  gaps (%d, longest %d):\n", len(gaps), shown)
			for _, g := range gaps[:shown] {
				fmt.Fprintf(w, "    %s → %s  %s\n", g.From.Format(ts), g.To.Format(ts), hours(g.Hours))
			}
		}
	}
	fmt.Fprintf(w, "\nReplay: days with books but no minute GEX (decided hourly): %s\n", list(r.HourlyOnlyDays))
	return nil
}

func list(days []string) string {
	if len(days) == 0 {
		return "none"
	}
	return strings.Join(days, ", ")
}

func hours(h float64) string {
	if h < 1 {
		return fmt.Sprintf("%.0f min", h*60)
	}
	return fmt.Sprintf("%.1f h", h)
}

func stepText(minutes float64) string {
	if minutes >= 60 {
		return fmt.Sprintf("%.0f h", minutes/60)
	}
	return fmt.Sprintf("%.0f min", minutes)
}
