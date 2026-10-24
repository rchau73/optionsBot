// Command usage writes the weekly monitor usage and cohort report (#295)
// from the analytics events the monitor collects (data/monitor/events.jsonl,
// docs/product/analytics/event-plan.md): active users, cohort retention,
// panels opened, first answers, help, the manual-close and rebalance funnels
// and trust signals — counts and shares only, never a user or session id.
// It reads that file only and writes data/monitor/reports/<week>.md|json.
//
//	go run ./cmd/usage -week 2026-W43
//	go run ./cmd/usage -events ~/dev/go/optionsBot-env/stable/data/monitor/events.jsonl
package main

import (
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
	flag.Parse()

	if err := run(*events, *week, *out); err != nil {
		fmt.Fprintln(os.Stderr, "usage:", err)
		os.Exit(1)
	}
}

func run(events, week, out string) error {
	w := evidence.LastWeek(time.Now())
	if week != "" {
		var err error
		if w, err = evidence.ParseWeek(week); err != nil {
			return err
		}
	}
	r, paths, err := usage.Run(events, w.From, w.To, w.Label, out)
	if err != nil {
		return err
	}
	fmt.Printf("%s: %d events in the week, %d lines rejected\n", w.Label, r.EventsInWeek, r.Source.RejectedTotal)
	if r.EventsInWeek == 0 {
		fmt.Println(r.NoEvents)
	}
	for _, p := range paths {
		fmt.Println("wrote", p)
	}
	return nil
}
