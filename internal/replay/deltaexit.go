package replay

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"optionsbot/internal/recorder"
)

// The delta-exit study (#769): on 2026-11-08 and 11-10 stable ETH's delta
// exit closed five 16-delta puts at −100 % to −160 % of their premium, near
// where the 2× stop would have closed them, and each held the side for
// repair_cooldown_hours. The study replays the same recorded books once per
// delta_exit_threshold (off = the stop alone) to compare worst week, return
// on margin and re-entries. Study only: the bot's threshold is unchanged; an
// exit change is a core policy and the product owner's call.

// DeltaExitOff is the threshold that disables the delta exit
// (strategy.EvaluateLeg: deltaExit ≤ 0).
const DeltaExitOff = 0.0

// DeltaExitThresholds are the issue's variants (-deltaexits all).
var DeltaExitThresholds = []float64{0.25, 0.30, 0.35, DeltaExitOff}

// ParseDeltaExits reads "0.25,0.30,0.35,off" (or "all"): each a |Δ| between
// 0 and 1, or off.
func ParseDeltaExits(v string) ([]float64, error) {
	if strings.TrimSpace(v) == "all" {
		return DeltaExitThresholds, nil
	}
	var out []float64
	for _, f := range strings.Split(v, ",") {
		f = strings.TrimSpace(f)
		if f == "off" {
			out = append(out, DeltaExitOff)
			continue
		}
		d, err := strconv.ParseFloat(f, 64)
		if err != nil || d <= 0 || d >= 1 {
			return nil, fmt.Errorf("-deltaexits %q: want |Δ| thresholds between 0 and 1 or off, comma-separated (or all)", v)
		}
		out = append(out, d)
	}
	return out, nil
}

// DeltaExitRow is one threshold's replay.
type DeltaExitRow struct {
	Threshold float64 // DeltaExitOff = the stop alone
	Result    Result
}

// Label is the threshold as the table shows it.
func (r DeltaExitRow) Label() string {
	if r.Threshold <= DeltaExitOff {
		return "off"
	}
	return fmt.Sprintf("%.2f", r.Threshold)
}

// DeltaExitStudy replays snaps once per threshold, everything else as set.
// A threshold at or under an entry delta is refused: a leg sold there would
// be exited on the next decision (config.Validate's rule).
func DeltaExitStudy(snaps []recorder.Snapshot, set Settings, thresholds []float64) ([]DeltaExitRow, error) {
	if set.Cfg == nil {
		return nil, fmt.Errorf("replay: no settings")
	}
	var rows []DeltaExitRow
	for _, d := range thresholds {
		for _, sl := range set.Cfg.Slots() {
			if d > DeltaExitOff && d <= sl.EntryDelta {
				return nil, fmt.Errorf("delta exit %.2f: at or under the entry delta %.2f of slot %d DTE", d, sl.EntryDelta, sl.TargetDTE)
			}
		}
		cfg := *set.Cfg
		cfg.DeltaExitThreshold = d
		s := set
		s.Cfg = &cfg
		s.Trace = nil
		res, err := Run(snaps, s)
		if err != nil {
			return nil, fmt.Errorf("delta exit %.2f: %w", d, err)
		}
		rows = append(rows, DeltaExitRow{Threshold: d, Result: res})
	}
	return rows, nil
}

// ReturnOnMargin is the replay's net as % of its average initial margin; 0
// without margin used.
func ReturnOnMargin(r Result) float64 {
	if r.AvgIM <= 0 {
		return 0
	}
	return r.Net / r.AvgIM * 100
}

// lossOfPremium is realised P&L as % of the premium those legs received, "—"
// without any.
func lossOfPremium(pnl, premium float64) string {
	if premium <= 0 {
		return "—"
	}
	return fmt.Sprintf("%+.0f", pnl/premium*100)
}

// DeltaExitMarkdown is one coin's table of the study, marked indicative on a
// short or gapped recording (as the liquidity-cap study).
func DeltaExitMarkdown(coin string, rows []DeltaExitRow, missing []time.Time) string {
	if len(rows) == 0 {
		return ""
	}
	b := studyHeader(coin, "delta exit vs the stop alone", rows[0].Result, missing)
	b.WriteString("| Delta exit | Net " + coin + " | Net (% of balance) | Return on margin (% of avg IM) | Worst week (% of balance) | Delta exits | Delta exit P&L (% of premium) | Stops | Stop P&L (% of premium) | MM cuts | Re-entries | Entries | Repairs |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		res := r.Result
		ww, _ := WorstWeek(res.Days, res.Equity0)
		net := 0.0
		if res.Equity0 > 0 {
			net = res.Net / res.Equity0 * 100
		}
		fmt.Fprintf(b, "| %s | %+.4f | %+.2f | %+.2f | %+.2f | %d | %s | %d | %s | %d | %d | %d | %d |\n",
			r.Label(), res.Net, net, ReturnOnMargin(res), ww, res.DeltaExits, lossOfPremium(res.DeltaExitPnL, res.DeltaExitPremium),
			res.Stops, lossOfPremium(res.StopPnL, res.StopPremium), res.MMCuts, res.ReEntries, res.Entries, res.Repairs)
	}
	return b.String()
}
