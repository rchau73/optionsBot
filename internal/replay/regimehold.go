package replay

import (
	"fmt"
	"strings"
	"time"

	"optionsbot/internal/recorder"
	"optionsbot/internal/risk"
	"optionsbot/internal/strategy"
)

// The regime-hold study (#550): on 2026-10-29 12:35 -03 stable ETH sold a
// 358-lot put in a bear trend while the live gamma regime was already
// negative, a day before the regime was confirmed; that put took most of
// the week's −9.4 ETH. The bot holds the trend's side only once the regime is
// confirmed negative (strategy.RegimeSideBlockReason). The study replays the
// same recorded books once per rule to compare it with holding that side as
// soon as the live regime is negative. Study only: the bot's rule is
// unchanged; a rule change is the product owner's call.

// The rules the study compares (Settings.RegimeHold).
const (
	RegimeHoldConfirmed = "confirmed" // the bot: the trend's side is held under a confirmed negative regime
	RegimeHoldLive      = "live"      // also held while the live regime is negative, before confirmation
)

// RegimeHolds are the rules -regimeholds accepts.
var RegimeHolds = []string{RegimeHoldConfirmed, RegimeHoldLive}

// onTrendSide reports whether a short of optType sells into the trend: a put
// in a bear trend, a call in a bull trend.
func onTrendSide(optType string, trend int) bool {
	return (optType == "put" && trend < 0) || (optType == "call" && trend > 0)
}

func liveNegative(dec strategy.GammaDecision) bool {
	return strings.HasPrefix(dec.Regime, "NEGATIVE")
}

// LiveRegimeSideBlockReason is the study's rule: the bot's
// RegimeSideBlockReason, and under hold "live" also the trend's side while
// the live regime is negative. "" = optType may be sold.
func LiveRegimeSideBlockReason(hold, optType string, dec strategy.GammaDecision, st risk.Status) string {
	if why := strategy.RegimeSideBlockReason(optType, dec.TrendDir, st); why != "" {
		return why
	}
	if hold == RegimeHoldLive && liveNegative(dec) && onTrendSide(optType, dec.TrendDir) {
		return "live negative gamma regime on the trend's side: no new " + optType + "s"
	}
	return ""
}

// sideHold is the regime-side rule the replay runs (Settings.RegimeHold).
func (e *engine) sideHold(optType string, dec strategy.GammaDecision, st risk.Status) string {
	return LiveRegimeSideBlockReason(e.set.RegimeHold, optType, dec, st)
}

// HoldRow is one rule's replay.
type HoldRow struct {
	Hold   string
	Result Result
}

// RegimeHoldStudy replays snaps once per rule in holds, everything else as set.
func RegimeHoldStudy(snaps []recorder.Snapshot, set Settings, holds []string) ([]HoldRow, error) {
	if set.Cfg == nil {
		return nil, fmt.Errorf("replay: no settings")
	}
	var rows []HoldRow
	for _, h := range holds {
		s := set
		s.RegimeHold = h
		s.Trace = nil
		res, err := Run(snaps, s)
		if err != nil {
			return nil, fmt.Errorf("regime hold %s: %w", h, err)
		}
		rows = append(rows, HoldRow{Hold: h, Result: res})
	}
	return rows, nil
}

// RegimeHoldMarkdown is one coin's table of the study, marked indicative on
// a short or gapped recording (as the liquidity-cap study).
func RegimeHoldMarkdown(coin string, rows []HoldRow, missing []time.Time) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	r0 := rows[0].Result
	fmt.Fprintf(&b, "### %s — regime-side hold, %s → %s, %d decisions\n\n", coin,
		r0.Start.Format("2006-01-02 15:04"), r0.End.Format("2006-01-02 15:04"), r0.Snapshots)
	_, full := WorstWeek(r0.Days, r0.Equity0)
	if len(missing) > 0 || !full {
		fmt.Fprintf(&b, "**Indicative:** %d replayed days", len(r0.Days))
		if len(missing) > 0 {
			days := make([]string, len(missing))
			for i, d := range missing {
				days[i] = d.Format("2006-01-02")
			}
			fmt.Fprintf(&b, "; no recording on %d days (%s)", len(missing), strings.Join(days, ", "))
		}
		if !full {
			b.WriteString("; under a week, so the worst week is the whole window")
		}
		b.WriteString(".\n\n")
	}
	b.WriteString("| Hold | Net " + coin + " | Net (% of balance) | Realised | Entries | Entries opened without the held side | Side holds (decisions) | Legs sold into a live negative regime on the trend's side | Stops | Delta exits | Worst week (% of balance) |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		res := r.Result
		ww, _ := WorstWeek(res.Days, res.Equity0)
		net := 0.0
		if res.Equity0 > 0 {
			net = res.Net / res.Equity0 * 100
		}
		fmt.Fprintf(&b, "| %s | %+.4f | %+.2f | %+.4f | %d | %d | %d | %d | %d | %d | %+.2f |\n",
			r.Hold, res.Net, net, res.Realised, res.Entries, res.RegimeHeldEntries, res.RegimeSideHolds,
			res.SoldIntoLiveNegative, res.Stops, res.DeltaExits, ww)
	}
	return b.String()
}
