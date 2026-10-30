package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
	"optionsbot/internal/replay"
	"optionsbot/internal/risk"
)

// -replay: the bot's rules over the recorded mainnet book (internal/replay)
// instead of a modelled path. The settings are the bot's config file
// (-config); a stress flag set on the command line changes the rule it
// names, so two runs that differ by one flag compare one rule on the same
// history. Flags of proposals the replay does not model are refused.

// replayFlags maps the stress flags the replay honours onto the config.
var replayFlags = map[string]func(c *config.Config, v string) error{
	"direction": func(c *config.Config, v string) error {
		if v != "sma" && v != "bias" {
			return fmt.Errorf("-direction %s: the replay runs the bot's sources only (sma | bias)", v)
		}
		c.GEXDirection = v
		return nil
	},
	"flipbufsd":     floatField(func(c *config.Config) *float64 { return &c.GammaFlipBufferSD }),
	"breach":        floatField(func(c *config.Config) *float64 { return &c.DeltaExitThreshold }),
	"drift":         floatField(func(c *config.Config) *float64 { return &c.DeltaDriftThreshold }),
	"tp":            floatField(func(c *config.Config) *float64 { return &c.ROITakeProfit }),
	"cap":           floatField(func(c *config.Config) *float64 { return &c.MaxLegSizeMultiple }),
	"liqcap":        floatField(func(c *config.Config) *float64 { return &c.LiquidityCapOIPct }),
	"strikeivratio": floatField(func(c *config.Config) *float64 { return &c.StrikeIVMaxRatio }),
	"sma9band":      floatField(func(c *config.Config) *float64 { return &c.SMA9BandSD }),
	"shedpersist":   intField(func(c *config.Config) *int { return &c.GEXShedConfirmSnapshots }),
	"shedcross":     intField(func(c *config.Config) *int { return &c.GEXRepairConfirmSnapshots }),
	"shedrelease":   intField(func(c *config.Config) *int { return &c.GEXRepairReleaseHours }),
	"biascooldown":  intField(func(c *config.Config) *int { return &c.GEXBiasRepairCooldownHours }),
	"stopslipwait":  intField(func(c *config.Config) *int { return &c.StopSpreadMaxWaitMin }),
	"stopslip": func(c *config.Config, v string) error { // a fraction here, a percentage in the config
		f, err := strconv.ParseFloat(v, 64)
		c.StopSpreadGuardPct = f * 100
		return err
	},
	"bands": func(c *config.Config, v string) error {
		var lo, mid, hi float64
		if n, _ := fmt.Sscanf(v, "%g,%g,%g", &lo, &mid, &hi); n != 3 {
			return fmt.Errorf("-bands wants three numbers: low,mid,high")
		}
		c.IVMarginBands = []risk.Band{{MinIVPct: 70, MaxIMPct: hi}, {MinIVPct: 30, MaxIMPct: mid}, {MinIVPct: 0, MaxIMPct: lo}}
		return nil
	},
}

// replayOwnFlags are the replay's own flags (and the shared input folder).
var replayOwnFlags = map[string]bool{"replay": true, "from": true, "to": true, "config": true, "equity": true,
	"journal": true, "start": true, "dir": true, "quiet": true, "currency": true, "biasflipwindow": true, "trace": true,
	"gex": true, "liqcaps": true, "regimehold": true, "regimeholds": true}

func floatField(f func(*config.Config) *float64) func(*config.Config, string) error {
	return func(c *config.Config, v string) (err error) {
		*f(c), err = strconv.ParseFloat(v, 64)
		return err
	}
}

func intField(f func(*config.Config) *int) func(*config.Config, string) error {
	return func(c *config.Config, v string) error {
		x, err := strconv.ParseFloat(v, 64) // hours and minutes are floats on the stress flags
		*f(c) = int(x)
		return err
	}
}

type replayOpts struct {
	dir, from, to, configPath, journal, start, inputs string
	gexDir                                            string // minute GEX records; "" = market_gex beside dir, "none" = hourly
	liqCaps                                           string // the liquidity-cap study's caps (e.g. "5,10,20"); "" = one replay
	regimeHold                                        string // the regime-side rule of one replay (#550): confirmed (the bot) | live
	regimeHolds                                       string // the regime-hold study's rules (e.g. "confirmed,live"); "" = one replay
	equity, flipWindowMin                             float64
	quiet, trace                                      bool
	set                                               map[string]string // flags given on the command line
}

func runReplay(o replayOpts) error {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil))) // the bot's rules log; the replay prints
	cfg, err := config.Load(o.configPath)
	if err != nil {
		return err
	}
	var changed []string
	for name, v := range o.set {
		if apply, ok := replayFlags[name]; ok {
			if err := apply(cfg, v); err != nil {
				return err
			}
			changed = append(changed, "-"+name+"="+v)
		} else if !replayOwnFlags[name] {
			return fmt.Errorf("-%s is not modelled by -replay (it runs the bot's rules on recorded books)", name)
		}
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	from, err := time.Parse(time.DateOnly, o.from)
	if err != nil {
		return fmt.Errorf("-from %q: want YYYY-MM-DD", o.from)
	}
	to := time.Time{}
	if o.to != "" {
		if to, err = time.Parse(time.DateOnly, o.to); err != nil {
			return fmt.Errorf("-to %q: want YYYY-MM-DD", o.to)
		}
		to = to.Add(24*time.Hour - time.Nanosecond) // the whole last day
	}
	// Everything recorded before -from warms up the GEX, DVOL and regime.
	snaps, err := recorder.ReadDir(o.dir, time.Time{}, to)
	if err != nil {
		return err
	}
	if len(snaps) == 0 {
		return fmt.Errorf("no recorded snapshot in %s", o.dir)
	}

	set := replay.Settings{Cfg: cfg, From: from, To: to, Equity: o.equity,
		FlipWindow: time.Duration(o.flipWindowMin * float64(time.Minute))}
	gexDir := o.gexDir
	if gexDir == "" {
		gexDir = filepath.Join(filepath.Dir(filepath.Clean(o.dir)), "market_gex")
	}
	if gexDir != "none" {
		if set.GEX, err = recorder.ReadGEXDir(gexDir, time.Time{}, to); err != nil {
			return err
		}
	}
	set.Closes, set.DVOL = readMarketJSON(filepath.Join(o.inputs, "market.json"))
	if set.Equity <= 0 {
		set.Equity = replay.ReadEquity(filepath.Join(o.inputs, "account.json"), cfg.Underlying)
	}
	if set.Equity <= 0 {
		set.Equity = 1
	}
	var trades []orders.Trade
	if o.journal != "" {
		rp, err := orders.ReplayFile(o.journal, 0)
		if err != nil {
			return err
		}
		trades = rp.Trades
	}
	switch o.start {
	case "empty":
	case "journal":
		if trades == nil {
			return fmt.Errorf("-start journal needs -journal orders.log")
		}
		set.Book = replay.BookAt(trades, from)
	default:
		return fmt.Errorf("-start %q: empty | journal", o.start)
	}
	if set.RegimeHold, err = parseHold(o.regimeHold); err != nil {
		return err
	}
	if set.RegimeHold != replay.RegimeHoldConfirmed {
		changed = append(changed, "-regimehold="+set.RegimeHold)
	}
	if o.liqCaps != "" && o.regimeHolds != "" {
		return fmt.Errorf("-liqcaps and -regimeholds are two studies: run one at a time")
	}
	if o.liqCaps != "" {
		return runCapStudy(snaps, set, o, changed)
	}
	if o.regimeHolds != "" {
		return runHoldStudy(snaps, set, o, changed)
	}
	if o.trace {
		set.Trace = func(at time.Time, line string) { fmt.Printf("%s  %s\n", at.UTC().Format("Jan 02 15:04"), line) }
	}
	res, err := replay.Run(snaps, set)
	if err != nil {
		return err
	}
	u := cfg.Underlying
	fmt.Printf("### REPLAY of recorded mainnet books — %s, direction %s, %s → %s, %d snapshots", u, cfg.GEXDirection,
		res.Start.Format("Jan 02 15:04"), res.End.Format("Jan 02 15:04"), res.Snapshots)
	if len(changed) > 0 {
		fmt.Printf(", %s", strings.Join(changed, " "))
	}
	fmt.Printf("\n\nStart: %s book, margin balance %.4f %s. Prices, spreads, greeks, OI and DVOL recorded; MODEL: margin (scan ±16 %% spot, IV −20/+30 %%, MM = %.0f %% of IM) and fills (sells at the price floor if the book has a bid, buy-backs at the ask).\n",
		o.start, set.Equity, u, replay.MMRatio*100)
	if res.GEXMinutes > 0 {
		fmt.Printf("GEX: %d of %d decisions on minute records (%s, %d read): regime, flip, bias, sheds and repair anchors each minute; prices held at the last hourly book.\n\n",
			res.GEXMinutes, res.Snapshots, gexDir, len(set.GEX))
	} else {
		fmt.Printf("GEX: rebuilt from each hourly book (no minute records in the window).\n\n")
	}
	if !o.quiet {
		fmt.Println("| Date | Spot | DVOL | GEX live / confirmed neg | Direction | Limit | IM % | Equity " + u + " | Day P&L | Fees | Trades | Sheds | Repairs | Actions |")
		fmt.Println("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|")
		for _, d := range res.Days {
			acts := strings.Join(d.Actions, "; ")
			if acts == "" {
				acts = "—"
			}
			fmt.Printf("| %s | %.0f | %.1f | %s / %v | %+d | %.0f%% | %.1f | %.4f | %+.4f | %.4f | %d | %d | %d | %s |\n",
				d.Date.Format("Jan 02"), d.Spot, d.DVOL, d.Regime, d.ConfirmedNegative, d.Direction, d.LimitPct, d.IMPct,
				d.Equity, d.PnL, d.Fees, d.Trades, d.Sheds, d.Reps, acts)
		}
		fmt.Println()
	}
	fmt.Printf("**Replay %s %s:** net %+.4f %s (%+.2f %%) · realised %+.4f · fees %.4f · trades %d (entries %d, repairs %d) · GEX sheds %d · stops %d · delta exits %d · rolls %d · MM cuts %d · unfilled %d · liquidity-capped legs %d · legs open at end %d · worst drawdown %.4f %s · IM max %.1f %% avg %.1f %%\n",
		u, cfg.GEXDirection, res.Net, u, res.Net/res.Equity0*100, res.Realised, res.Fees, res.Trades, res.Entries, res.Repairs,
		res.Sheds, res.Stops, res.DeltaExits, res.Rolls, res.MMCuts, res.Unfilled, res.LiquidityCapped, res.OpenAtEnd, res.MaxDrawdown, u, res.MaxIMPct, res.AvgIMPct)
	fmt.Printf("REPLAY\t%s\t%s\t%.4f\t%.4f\t%.4f\t%d\t%d\t%d\t%d\t%d\t%.4f\t%.1f\n", u, cfg.GEXDirection, res.Net, res.Realised, res.Fees,
		res.Trades, res.Entries, res.Repairs, res.Sheds, res.Stops, res.MaxDrawdown, res.AvgIMPct)
	if trades != nil {
		var window []recorder.Snapshot
		for _, s := range snaps {
			if !s.At.Before(res.Start) && !s.At.After(res.End) {
				window = append(window, s)
			}
		}
		a := replay.ActualPnL(trades, window, res.Start, res.End)
		fmt.Printf("**Journal (what the bot did), same window:** net %+.4f %s = realised %+.4f + unrealised %+.4f → %+.4f · opens %d · closes %d · legs without a recorded quote %d · replay − journal %+.4f %s\n",
			a.Net, u, a.Realised, a.Unrealised0, a.Unrealised1, a.Opens, a.Closes, a.Unmarked, res.Net-a.Net, u)
	}
	return nil
}

// runCapStudy is -liqcaps (#487): one replay per cap on the same books and
// settings, printed as one table with the recording's gaps.
func runCapStudy(snaps []recorder.Snapshot, set replay.Settings, o replayOpts, changed []string) error {
	if _, ok := o.set["liqcap"]; ok {
		return fmt.Errorf("-liqcaps varies the cap itself: drop -liqcap")
	}
	caps, err := parseCaps(o.liqCaps)
	if err != nil {
		return err
	}
	rows, err := replay.CapStudy(snaps, set, caps)
	if err != nil {
		return err
	}
	fmt.Printf("Liquidity-cap study (#487), %s, direction %s, start %s", set.Cfg.Underlying, set.Cfg.GEXDirection, o.start)
	if len(changed) > 0 {
		fmt.Printf(", %s", strings.Join(changed, " "))
	}
	fmt.Print(". Entries only: the replay has no rebalance top-ups.\n\n")
	fmt.Print(replay.CapStudyMarkdown(set.Cfg.Underlying, rows, replay.MissingDays(snaps, set.From, set.To)))
	return nil
}

// parseCaps reads "5,10,20": each a percentage of open interest, 0 = off.
func parseCaps(v string) ([]float64, error) {
	var caps []float64
	for _, f := range strings.Split(v, ",") {
		c, err := strconv.ParseFloat(strings.TrimSpace(f), 64)
		if err != nil || c < 0 || c > 100 {
			return nil, fmt.Errorf("-liqcaps %q: want percentages from 0 to 100, comma-separated", v)
		}
		caps = append(caps, c)
	}
	return caps, nil
}

// runHoldStudy is -regimeholds (#550): one replay per regime-side rule on the
// same books and settings, printed as one table with the recording's gaps.
func runHoldStudy(snaps []recorder.Snapshot, set replay.Settings, o replayOpts, changed []string) error {
	if _, ok := o.set["regimehold"]; ok {
		return fmt.Errorf("-regimeholds varies the hold itself: drop -regimehold")
	}
	var holds []string
	for _, f := range strings.Split(o.regimeHolds, ",") {
		h, err := parseHold(strings.TrimSpace(f))
		if err != nil {
			return err
		}
		holds = append(holds, h)
	}
	rows, err := replay.RegimeHoldStudy(snaps, set, holds)
	if err != nil {
		return err
	}
	fmt.Printf("Regime-hold study (#550), %s, direction %s, start %s", set.Cfg.Underlying, set.Cfg.GEXDirection, o.start)
	if len(changed) > 0 {
		fmt.Printf(", %s", strings.Join(changed, " "))
	}
	fmt.Print(". confirmed = the bot (the trend's side held under a confirmed negative regime); live = also held while the live regime is negative. Entries and repairs; no rebalance top-ups in the replay.\n\n")
	fmt.Print(replay.RegimeHoldMarkdown(set.Cfg.Underlying, rows, replay.MissingDays(snaps, set.From, set.To)))
	return nil
}

// parseHold reads one regime-side rule; "" is the bot's.
func parseHold(v string) (string, error) {
	if v == "" {
		return replay.RegimeHoldConfirmed, nil
	}
	for _, h := range replay.RegimeHolds {
		if v == h {
			return h, nil
		}
	}
	return "", fmt.Errorf("-regimehold %q: want %s", v, strings.Join(replay.RegimeHolds, " | "))
}

// readMarketJSON reads market.json (replay.ReadMarketJSON); a missing file
// seeds nothing.
func readMarketJSON(path string) (closes, dvol []orders.DailyClose) {
	closes, dvol, err := replay.ReadMarketJSON(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "note: %s not read (%v): the SMA trend and IV percentile start from the recording alone\n", path, err)
	}
	return closes, dvol
}
