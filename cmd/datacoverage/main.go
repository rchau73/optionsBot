// Command datacoverage reports how much recorded history one bot has (#233):
// for data/market (hourly books), data/market_gex (minute GEX),
// data/market_usdc (USDC books) and data/pnl_history.jsonl, the first and
// last record, the days and hours covered, the missing days and the gaps,
// and the days a replay can only decide hourly (books without minute GEX).
// Research tool: it reads files only — no Deribit call, no trading state —
// and prints counts and dates, never an amount.
//
//	go run ./cmd/datacoverage -dir data/btc
//	go run ./cmd/datacoverage -dir data/btc -from 2026-10-12 -to 2026-10-18 -json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"optionsbot/internal/coverage"
)

func main() {
	dir := flag.String("dir", "", "the bot's folder (data/btc) or its data directory (data/btc/data)")
	fromStr := flag.String("from", "", "window start, YYYY-MM-DD or RFC 3339 (default: the first recorded day)")
	toStr := flag.String("to", "", "window end: YYYY-MM-DD = that day included, RFC 3339 = excluded (default: the last recorded day, up to the current hour)")
	asJSON := flag.Bool("json", false, "print JSON instead of text")
	flag.Parse()

	if err := run(*dir, *fromStr, *toStr, *asJSON); err != nil {
		fmt.Fprintln(os.Stderr, "datacoverage:", err)
		os.Exit(1)
	}
}

func run(dir, fromStr, toStr string, asJSON bool) error {
	if dir == "" {
		return fmt.Errorf("-dir is required")
	}
	var from, to time.Time
	if fromStr != "" {
		t, _, err := parseTime(fromStr)
		if err != nil {
			return fmt.Errorf("-from: %w", err)
		}
		from = t
	}
	if toStr != "" {
		t, dateOnly, err := parseTime(toStr)
		if err != nil {
			return fmt.Errorf("-to: %w", err)
		}
		if dateOnly {
			t = t.AddDate(0, 0, 1) // the day is included
		}
		to = t
	}
	res, err := coverage.Run(dir, from, to, time.Now())
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	return coverage.Format(os.Stdout, res)
}

func parseTime(s string) (t time.Time, dateOnly bool, err error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), false, nil
	}
	t, err = time.Parse(time.DateOnly, s)
	return t, true, err
}
