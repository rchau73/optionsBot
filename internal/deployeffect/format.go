package deployeffect

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Format prints one line per startup (time, commit, class, orders and lots
// by trigger), each non-neutral order under it with its reason, then the
// summary over the window. Counts and lots only — never an amount.
func Format(w io.Writer, bot string, r Report) error {
	const ts = "2006-01-02 15:04"
	fmt.Fprintf(w, "Deploy effect — %s — %s → %s UTC — orders in the first %.0f min after each startup\n\n",
		bot, r.From.UTC().Format(ts), r.To.UTC().Format(ts), Window.Minutes())
	for _, s := range r.Startups {
		commit := s.Commit
		if commit == "" {
			commit = "commit ?"
		} else if len(commit) > 7 {
			commit = commit[:7]
		}
		if s.Rollback {
			commit += " (rollback)"
		}
		source := ""
		if s.Source == "deploy" {
			source = " [deploys.tsv only: no startup line in the journal]"
		}
		fmt.Fprintf(w, "%s  %-18s %-12s %s%s\n", s.At.UTC().Format("2006-01-02 15:04:05"), commit, s.Class, Counts(s.Triggers), source)
		for _, o := range s.Orders {
			if o.Class == Neutral {
				continue
			}
			fmt.Fprintf(w, "    %s %-12s %s %s %s lots: %s\n", o.At.UTC().Format("15:04:05"), o.Class, o.Trigger, o.Instrument, lots(o.Lots), o.Why)
		}
	}
	n := len(r.Startups)
	fmt.Fprintf(w, "\nSummary: %d startups — neutral %d, expected %d, restart-made %d", n, r.ByClass[Neutral], r.ByClass[Expected], r.ByClass[RestartMade])
	if share, ok := r.RestartMadeShare(); ok {
		fmt.Fprintf(w, " (restart-made %.0f %%, target 0)", 100*share)
	}
	fmt.Fprintf(w, "\nOrders by trigger: %s\n", Counts(r.Triggers))
	return nil
}

// Counts is "rebalance_upsize 2 orders 1.2 lots, entry 1 order 0.5 lots
// (3 amends, 1 cancel)", triggers by name; "no order" when empty.
func Counts(m map[string]*TriggerCount) string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	var parts []string
	for _, name := range names {
		c := m[name]
		var p []string
		if c.Orders > 0 {
			p = append(p, fmt.Sprintf("%d %s %s lots", c.Orders, plural(c.Orders, "order"), lots(c.Lots)))
		}
		if c.Amends > 0 {
			p = append(p, fmt.Sprintf("%d %s", c.Amends, plural(c.Amends, "amend")))
		}
		if c.Cancels > 0 {
			p = append(p, fmt.Sprintf("%d %s", c.Cancels, plural(c.Cancels, "cancel")))
		}
		parts = append(parts, name+" "+strings.Join(p, " "))
	}
	if len(parts) == 0 {
		return "no order"
	}
	return strings.Join(parts, ", ")
}

func lots(q float64) string { return fmt.Sprintf("%.6g", q) }

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
