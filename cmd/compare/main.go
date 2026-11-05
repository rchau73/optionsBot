// Command compare runs the same evidence for two configs — the backtest, the
// bot's rules replayed on the recorded books and the standard stress shocks
// of the captured book — and writes a side-by-side report (#186), the
// settings that differ listed key by key first. It reads files only (no
// Deribit call, no order) and writes <out>/<UTC time>/report.md and .json.
// -windows week|day also compares the backtest and the replay window by
// window (#301). -neighbours <key> re-runs the proposed config with that
// setting moved ±10 % and ±20 % and flags cliffs between neighbours (#495).
// -set key=value (repeatable, #641) builds the proposed config from -base
// (or -proposed) with only those values, on a temporary copy: the lab's run
// requests (cmd/labrun) carry values, not a file.
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
	windows := flag.String("windows", "", "also compare per window: week (ISO weeks, UTC) or day (empty = the whole span only)")
	neighbours := flag.String("neighbours", "", "also re-run the proposed config with this setting (a key as in the Differences, e.g. gamma_flip_buffer_sd) at −20, −10, +10 and +20 % and flag cliffs (empty = skip)")
	var sets setFlags
	flag.Var(&sets, "set", "key=value, repeatable: the proposed config is -proposed (or, without it, -base) with these values, on a temporary copy (#641); [] in a key = every element of the list")
	flag.Parse()

	paths := compare.Paths{Base: *base, Proposed: *proposed, CSV: *csv, Market: *market, GEX: *gexDir, Stress: *stress}
	if err := run(paths, sets, *from, *to, *equity, *stressCmd, *out, *windows, *neighbours); err != nil {
		fmt.Fprintln(os.Stderr, "compare:", err)
		os.Exit(1)
	}
}

// setFlags collects the repeatable -set key=value.
type setFlags []compare.Setting

func (s *setFlags) String() string { return fmt.Sprint(len(*s)) }

func (s *setFlags) Set(v string) error {
	one, err := compare.ParseSetting(v)
	if err != nil {
		return err
	}
	*s = append(*s, one)
	return nil
}

func run(p compare.Paths, sets []compare.Setting, fromStr, toStr string, equity float64, stressCmd, out, windows, neighbours string) error {
	label := ""
	if p.Base != "" && len(sets) > 0 {
		from := p.Proposed
		if from == "" {
			from = p.Base
		}
		tmp, err := compare.WriteWith(from, sets)
		if err != nil {
			return fmt.Errorf("-set: %w", err)
		}
		defer os.Remove(tmp)
		p.Proposed, label = tmp, compare.SettingsLabel(from, sets)
	}
	if p.Base == "" || p.Proposed == "" {
		return fmt.Errorf("-base and -proposed (or -set) are both needed")
	}
	unit, err := compare.ParseUnit(windows)
	if err != nil {
		return fmt.Errorf("-%w", err)
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
	in, err := compare.Load(p, from, to, equity, os.Stderr)
	if err != nil {
		return err
	}
	// After Load, so a damaged day's warning reaches stderr (#661).
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil))) // the bot's rules log; the report says it all
	var plan compare.Neighbours
	if neighbours != "" {
		if plan, err = compare.PlanNeighbours(in.Proposed, neighbours); err != nil {
			return fmt.Errorf("-%w", err)
		}
	}
	compare.RunStress(&in, stressCmd)
	r := compare.Run(in)
	if label != "" {
		r.Proposed = label // the temporary file's name says nothing
	}
	if unit != "" {
		wr := compare.RunWindows(in, unit)
		r.Windows = &wr
	}
	if neighbours != "" {
		nb := compare.RunNeighbours(in, r, plan)
		r.Neighbours = &nb
	}
	md, js, err := compare.Write(r, compare.OutDir(out, time.Now()))
	if err != nil {
		return err
	}
	fmt.Printf("%s, %s\n", md, js)
	return nil
}
