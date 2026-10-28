// Command usage writes the weekly monitor usage and cohort report (#295)
// from the analytics events the monitor collects (data/monitor/events.jsonl,
// docs/product/analytics/event-plan.md): active users, cohort retention,
// panels opened, first answers, help, the manual-close and rebalance funnels
// and trust signals — counts and shares only, never a user or session id.
// It reads that file only and writes data/monitor/reports/<week>.md|json.
//
//	go run ./cmd/usage -week 2026-W43
//	go run ./cmd/usage -events ~/dev/go/optionsBot-env/stable/data/monitor/events.jsonl
//	go run ./cmd/usage -week 2026-W43 -alert   # + the engagement-drop check (#341): DROPS {json}
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"optionsbot/internal/evidence"
	"optionsbot/internal/usage"
)

func main() {
	events := flag.String("events", "data/monitor/events.jsonl", "the monitor's analytics events file")
	week := flag.String("week", "", "ISO week, e.g. 2026-W43 (default: the last complete week)")
	out := flag.String("out", "data/monitor/reports", "folder for <week>.md and <week>.json")
	alert := flag.Bool("alert", false, "also compare the week with the weeks before it and print the drops as JSON on a DROPS line (#341)")
	rules := usage.DefaultRules()
	flag.Float64Var(&rules.CountPct, "alert-count-pct", rules.CountPct, "study: a count (active users, help used) drops when this % under the base weeks' mean")
	flag.Float64Var(&rules.FunnelPts, "alert-funnel-pts", rules.FunnelPts, "study: a funnel drops when its completion share is this many points under the base weeks'")
	flag.IntVar(&rules.MinBase, "alert-min-base", rules.MinBase, "study: the fewest users or funnel starts a rule judges")
	baseWeeks := flag.Int("alert-base-weeks", usage.DropBaseWeeks, "study: how many weeks before the report's are its base")
	flag.Parse()

	r, err := run(*events, *week, *out, rules, *baseWeeks)
	if err != nil {
		fmt.Fprintln(os.Stderr, "usage:", err)
		os.Exit(1)
	}
	if *alert {
		// The drop check never fails the report: an error is printed, exit 0.
		if err := printDrops(r.Drops); err != nil {
			fmt.Fprintln(os.Stderr, "usage: drop check:", err)
		}
	}
}

// printDrops prints the week's drop check (the one written in the report's
// JSON as `drops`, #445) as one `DROPS {json}` line, after a line per drop
// for a person. Counts and shares only.
func printDrops(a *usage.Alert) error {
	if a == nil {
		return fmt.Errorf("the report carries no drop check")
	}
	for _, d := range a.Drops {
		fmt.Println("drop:", d)
	}
	for _, m := range a.Missing {
		fmt.Println("no events in", m)
	}
	js, err := json.Marshal(a)
	if err != nil {
		return err
	}
	fmt.Printf("DROPS %s\n", js)
	return nil
}

func window(week string) (evidence.Window, error) {
	if week == "" {
		return evidence.LastWeek(time.Now()), nil
	}
	return evidence.ParseWeek(week)
}

func run(events, week, out string, rules usage.Rules, baseWeeks int) (usage.Report, error) {
	w, err := window(week)
	if err != nil {
		return usage.Report{}, err
	}
	r, paths, err := usage.RunWith(events, w.From, w.To, w.Label, out, rules, baseWeeks)
	if err != nil {
		return usage.Report{}, err
	}
	fmt.Printf("%s: %d events in the week, %d lines rejected\n", w.Label, r.EventsInWeek, r.Source.RejectedTotal)
	if r.EventsInWeek == 0 {
		fmt.Println(r.NoEvents)
	}
	for _, p := range paths {
		fmt.Println("wrote", p)
	}
	return r, nil
}
