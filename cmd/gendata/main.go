// Command gendata writes synthetic option data for the backtest
// (backtest.WriteSynthetic). -calendar deribit lists weeklies, monthlies and
// quarterlies as Deribit does, for the ladder sweep (#83).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"optionsbot/internal/backtest"
)

func main() {
	out := flag.String("out", "data/historical/options.csv", "output CSV path")
	calendar := flag.String("calendar", backtest.CalendarMonthly, "listed expiries: monthly (4 monthlies at ≥ 21 DTE) | deribit (weeklies, monthlies, quarterlies)")
	fromStr := flag.String("from", "2026-01-01", "first day (YYYY-MM-DD)")
	toStr := flag.String("to", "2026-12-31", "last day (YYYY-MM-DD)")
	flag.Parse()

	from, err1 := time.Parse("2006-01-02", *fromStr)
	to, err2 := time.Parse("2006-01-02", *toStr)
	if err1 != nil || err2 != nil {
		fmt.Fprintln(os.Stderr, "-from/-to must be YYYY-MM-DD")
		os.Exit(2)
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	rows, err := backtest.WriteSynthetic(f, backtest.SyntheticOptions{From: from, To: to, Calendar: *calendar})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %d rows to %s (calendar %s)\n", rows, *out, *calendar)
}
