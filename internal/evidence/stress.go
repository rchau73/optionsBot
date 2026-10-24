package evidence

import (
	"fmt"
	"strconv"
	"strings"

	"optionsbot/internal/config"
)

// StandardShocks are the cmd/stress paths every report runs on the book,
// with no calm days first: the book as captured meets the shock at once.
var StandardShocks = []string{"crash", "rally", "crash40", "rally40", "chop"}

// StressOutcome is one shock's result, from cmd/stress's RESULT line.
// Coin figures are in the underlying; "vs hold" is the strategy's equity
// minus the starting equity held as coin (cmd/stress's lead figure).
type StressOutcome struct {
	Shock          string  `json:"shock"`
	EquityEnd      float64 `json:"equity_end"`        // coin
	ReturnPct      float64 `json:"return_pct"`        // equity change in coin, %
	VsHoldCoin     float64 `json:"vs_hold_coin"`      // coin
	VsHoldUSD      float64 `json:"vs_hold_usd"`       // that coin at the path's final price
	Realised       float64 `json:"realised"`          // coin, net of fees
	Stops          int     `json:"stops"`             //
	DeltaExits     int     `json:"delta_exits"`       //
	Rolls          int     `json:"rolls"`             //
	WorstMMPct     float64 `json:"worst_mm_pct"`      // highest MM, % of margin balance
	WorstEquityPct float64 `json:"worst_equity_pct"`  // lowest equity on the path vs the start, %
	Error          string  `json:"error,omitempty"`   // the run failed: the rest is zero
	Command        string  `json:"command,omitempty"` // how to re-run it
}

// StressArgs are cmd/stress's flags for one standard shock on the book in
// dir, set from the bot's config so the stress runs today's rules: its
// slots, exit thresholds, size cap, flip buffer, direction source and IM
// bands (when they are the three DVOL bands the tool models).
func StressArgs(cfg *config.Config, dir, shock string) []string {
	f := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
	var slots []string
	for _, s := range cfg.Slots() {
		slots = append(slots, fmt.Sprintf("%d:%s", s.TargetDTE, f(s.EntryDelta)))
	}
	args := []string{"-dir", dir, "-currency", strings.ToUpper(cfg.Underlying), "-shock", shock, "-calm", "0", "-quiet",
		"-slots", strings.Join(slots, ","),
		"-tp", f(cfg.ROITakeProfit), "-drift", f(cfg.DeltaDriftThreshold), "-breach", f(cfg.DeltaExitThreshold),
		"-cap", f(cfg.MaxLegSizeMultiple), "-flipbufsd", f(cfg.GammaFlipBufferSD), "-direction", cfg.GEXDirection}
	if !strings.EqualFold(cfg.Underlying, "BTC") {
		args = append(args, "-volscale", "1.4") // README "Stress test": ETH-like moves
	}
	if b := bandsFlag(cfg); b != "" {
		args = append(args, "-bands", b)
	}
	return args
}

// stressKeys are the YAML settings StressArgs passes to cmd/stress (by
// their top-level key); a change of StressArgs needs this list too.
var stressKeys = []string{"underlying", "dte_delta_matrix", "roi_take_profit", "delta_drift_threshold",
	"delta_exit_threshold", "max_leg_size_multiple", "gamma_flip_buffer_sd", "gex_direction", "iv_margin_bands"}

// StressModels says whether cmd/stress, run with StressArgs, sees the YAML
// setting key ("roi_take_profit", "iv_margin_bands[1].max_im_pct") of every
// given config: the IM bands only when each config's are the three bands the
// tool models.
func StressModels(key string, cfgs ...*config.Config) bool {
	top := key
	if i := strings.IndexAny(key, ".["); i >= 0 {
		top = key[:i]
	}
	for _, k := range stressKeys {
		if k != top {
			continue
		}
		if k == "iv_margin_bands" {
			for _, c := range cfgs {
				if bandsFlag(c) == "" {
					return false
				}
			}
		}
		return true
	}
	return false
}

// bandsFlag is "low,mid,high" when the config's bands start at IV
// percentiles 0, 30 and 70 — the only bands cmd/stress models — else "".
func bandsFlag(cfg *config.Config) string {
	im := map[float64]float64{}
	for _, b := range cfg.IVMarginBands {
		im[b.MinIVPct] = b.MaxIMPct
	}
	lo, ok0 := im[0]
	mid, ok30 := im[30]
	hi, ok70 := im[70]
	if len(im) != 3 || !ok0 || !ok30 || !ok70 {
		return ""
	}
	f := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
	return f(lo) + "," + f(mid) + "," + f(hi)
}

// ParseStressResult reads the RESULT line of cmd/stress's output:
// RESULT name breach equity return% usd_change hold_usd_change stops
// breaches rolls realised worst_mm worst_equity% wings wing_cost
// trigger_days vs_hold_coin vs_hold_usd.
func ParseStressResult(shock, output string) (StressOutcome, error) {
	for _, line := range strings.Split(output, "\n") {
		f := strings.Split(strings.TrimSpace(line), "\t")
		if f[0] != "RESULT" {
			continue
		}
		if len(f) < 18 {
			return StressOutcome{}, fmt.Errorf("RESULT line has %d fields, want 18", len(f))
		}
		var err error
		num := func(i int) float64 {
			v, e := strconv.ParseFloat(f[i], 64)
			if e != nil && err == nil {
				err = fmt.Errorf("RESULT field %d %q: %w", i, f[i], e)
			}
			return v
		}
		o := StressOutcome{Shock: shock, EquityEnd: num(3), ReturnPct: num(4), Stops: int(num(7)), DeltaExits: int(num(8)),
			Rolls: int(num(9)), Realised: num(10), WorstMMPct: num(11), WorstEquityPct: num(12),
			VsHoldCoin: num(16), VsHoldUSD: num(17)}
		return o, err
	}
	return StressOutcome{}, fmt.Errorf("no RESULT line in the output")
}
