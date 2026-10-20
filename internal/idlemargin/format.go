package idlemargin

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"
)

// Rows returns the report's rows in print order: occupied, capped, frozen,
// then the vacancy reasons by total hours (largest first), not measured last.
func Rows(r Report) []string {
	fixed := map[string]bool{RowOccupied: true, RowOccupiedCapped: true, RowFrozen: true, RowNotMeasured: true}
	var reasons []string
	for row := range r.Total.Hours {
		if !fixed[row] {
			reasons = append(reasons, row)
		}
	}
	sort.Slice(reasons, func(i, j int) bool {
		a, b := r.Total.Hours[reasons[i]], r.Total.Hours[reasons[j]]
		if a != b {
			return a > b
		}
		return reasons[i] < reasons[j]
	})
	rows := []string{RowOccupied, RowOccupiedCapped, RowFrozen}
	rows = append(rows, reasons...)
	return append(rows, RowNotMeasured)
}

// Format prints the report of one bot: hours, shares of measured time and
// percentages of the IM limit only — never an amount.
func Format(w io.Writer, bot string, r Report) error {
	const ts = "2006-01-02 15:04"
	fmt.Fprintf(w, "Idle-margin attribution — %s — %s → %s UTC\n", bot, r.From.UTC().Format(ts), r.To.UTC().Format(ts))
	u := r.Utilisation
	fmt.Fprintf(w, "Utilisation (history.Utilisation): mean %s of the IM limit while entries were allowed; allowed %s of %.1f h measured\n\n",
		pct(u.MeanPct), share(u.AllowedShare), u.MeasuredHours)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprint(tw, "Slot time (h, share of measured)\t")
	for _, s := range r.Slots {
		fmt.Fprintf(tw, "%dd/%.2f\t\t", s.Slot.DTE, s.Slot.Delta)
	}
	fmt.Fprint(tw, "total\t\t\n")
	reports := append(append([]SlotReport{}, r.Slots...), r.Total)
	for _, row := range Rows(r) {
		fmt.Fprintf(tw, "%s\t", row)
		for _, s := range reports {
			h := s.Hours[row]
			sh := "—"
			if m := s.MeasuredHours(); row != RowNotMeasured && m > 0 {
				sh = fmt.Sprintf("%.0f %%", 100*h/m)
			}
			fmt.Fprintf(tw, "%.1f\t%s\t", h, sh)
		}
		fmt.Fprint(tw, "\n")
	}
	fmt.Fprint(tw, "measured\t")
	for _, s := range reports {
		fmt.Fprintf(tw, "%.1f\t\t", s.MeasuredHours())
	}
	fmt.Fprint(tw, "\n\n")

	fmt.Fprint(tw, "Book state, entries allowed\thours\tshare\tmean IM ÷ limit\tshare of unused margin\t\n")
	var allowed float64
	for _, name := range BookStates {
		allowed += r.Book[name].Hours
	}
	for _, name := range BookStates {
		bs := r.Book[name]
		var sh *float64
		if allowed > 0 {
			v := bs.Hours / allowed
			sh = &v
		}
		fmt.Fprintf(tw, "%s\t%.1f\t%s\t%s\t%s\t\n", name, bs.Hours, share(sh), pct(bs.MeanPct), share(bs.UnusedShare))
	}
	return tw.Flush()
}

func pct(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%.1f %%", *v)
}

func share(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%.1f %%", 100**v)
}
