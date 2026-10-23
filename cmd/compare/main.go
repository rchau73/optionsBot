// Command compare runs the same evidence for two configs — the backtest, the
// bot's rules replayed on the recorded books and the standard stress shocks
// of the captured book — and writes a side-by-side report (#186), the
// settings that differ listed key by key first. It reads files only (no
// Deribit call, no order) and writes <out>/<UTC time>/report.md and .json.
// See README "Settings comparison".
//
//	go run ./cmd/compare -base config_btc.yaml -proposed my.yaml -from 2026-10-12 -to 2026-10-18 \
//	    -replay data/btc/data/market -stress data/stress/btc
package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"optionsbot/internal/compare"
)

func main() {
	base := flag.String("base", "", "the current config (e.g. config_btc.yaml)")
	proposed := flag.String("proposed", "", "the proposed config")
	from := flag.String("from", "", "first day, YYYY-MM-DD")
	to := flag.String("to", "", "last day, YYYY-MM-DD (included)")
	csv := flag.String("csv", "data/historical/options.csv", "backtest data (none = skip the backtest)")
	market := flag.String("replay", "", "recorded books, e.g. data/btc/data/market (empty = skip the replay)")
	gexDir := flag.String("gex", "", "minute GEX records (default market_gex beside -replay; none = hourly)")
	stress := flag.String("stress", "", "captured book of scripts/stress_inputs.sh, e.g. data/stress/btc: the stress shocks, and market.json/account.json for the replay (empty = skip the stress)")
	equity := flag.Float64("equity", 0, "replay's starting margin balance in the coin (default account.json of -stress)")
	stressCmd := flag.String("stress-cmd", "go run ./cmd/stress", "command that runs cmd/stress, from the repository (none = skip the stress)")
	out := flag.String("out", "data/compare", "reports folder: <out>/<UTC time>/report.md|json")
	flag.Parse()

	paths := compare.Paths{Base: *base, Proposed: *proposed, CSV: *csv, Market: *market, GEX: *gexDir, Stress: *stress}
	if err := run(paths, *from, *to, *equity, *stressCmd, *out); err != nil {
		fmt.Fprintln(os.Stderr, "compare:", err)
		os.Exit(1)
	}
}

func run(p compare.Paths, fromStr, toStr string, equity float64, stressCmd, out string) error {
	if p.Base == "" || p.Proposed == "" {
		return fmt.Errorf("-base and -proposed are both needed")
	}
	from, err := time.Parse(time.DateOnly, fromStr)
	if err != nil {
		return fmt.Errorf("-from %q: want YYYY-MM-DD", fromStr)
	}
	to, err := time.Parse(time.DateOnly, toStr)
	if err != nil {
		return fmt.Errorf("-to %q: want YYYY-MM-DD", toStr)
	}
	if to.Before(from) {
		return fmt.Errorf("-to %s is before -from %s", toStr, fromStr)
	}
	if p.CSV == "none" {
		p.CSV = ""
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil))) // the bot's rules log; the report says it all
	in, err := compare.Load(p, from, to, equity, os.Stderr)
	if err != nil {
		return err
	}
	compare.RunStress(&in, stressCmd)
	md, js, err := compare.Write(compare.Run(in), compare.OutDir(out, time.Now()))
	if err != nil {
		return err
	}
	fmt.Printf("%s, %s\n", md, js)
	return nil
}
