package replay

import (
	"fmt"
	"math"
	"strings"
	"time"

	"optionsbot/internal/recorder"
	"optionsbot/internal/strategy"
)

// The liquidity-cap study (#487): the same recorded books replayed once per
// liquidity_cap_oi_pct, to see what the cap costs in margin use (E1's target:
// IM at 60 % of the limit or more while entries are allowed) and how often it
// cuts an entry. Study only: the bot's cap stays fixed in config/fixed.go.
// Not modelled: rebalance top-ups (the replay has no rebalance), so only
// entries can be cut here.

// CapRow is one cap's replay.
type CapRow struct {
	CapPct float64
	Result Result
}

// UnderTargetShare is the share of the decisions with entries allowed whose
// IM stayed under the 60 % target, in %; 0 when entries were never allowed.
func (r CapRow) UnderTargetShare() float64 {
	if r.Result.EntryCycles == 0 {
		return 0
	}
	return float64(r.Result.UnderTargetCycles) / float64(r.Result.EntryCycles) * 100
}

// CapStudy replays snaps once per cap in caps, everything else as set.
func CapStudy(snaps []recorder.Snapshot, set Settings, caps []float64) ([]CapRow, error) {
	if set.Cfg == nil {
		return nil, fmt.Errorf("replay: no settings")
	}
	var rows []CapRow
	for _, pct := range caps {
		cfg := *set.Cfg
		cfg.LiquidityCapOIPct = pct
		s := set
		s.Cfg = &cfg
		s.Trace = nil
		res, err := Run(snaps, s)
		if err != nil {
			return nil, fmt.Errorf("cap %g %%: %w", pct, err)
		}
		rows = append(rows, CapRow{CapPct: pct, Result: res})
	}
	return rows, nil
}

// WorstWeek is the worst change of the margin balance over any 7 consecutive
// replayed days, as % of the starting balance. full is false when fewer than
// 7 days were replayed: the change is then over all of them.
func WorstWeek(days []Day, equity0 float64) (pct float64, full bool) {
	if len(days) == 0 || equity0 <= 0 {
		return 0, false
	}
	const week = 7
	n := min(week, len(days))
	worst := math.Inf(1)
	for i := 0; i+n <= len(days); i++ {
		sum := 0.0
		for _, d := range days[i : i+n] {
			sum += d.PnL
		}
		worst = math.Min(worst, sum)
	}
	return worst / equity0 * 100, len(days) >= week
}

// MissingDays lists the UTC days from `from` to `to` (both included) with no
// recorded snapshot. A zero `to` is the last snapshot's day.
func MissingDays(snaps []recorder.Snapshot, from, to time.Time) []time.Time {
	if len(snaps) == 0 {
		return nil
	}
	have := map[time.Time]bool{}
	for _, s := range snaps {
		have[s.At.UTC().Truncate(24*time.Hour)] = true
	}
	if to.IsZero() {
		to = snaps[len(snaps)-1].At
	}
	var out []time.Time
	for d := from.UTC().Truncate(24 * time.Hour); !d.After(to.UTC()); d = d.AddDate(0, 0, 1) {
		if !have[d] {
			out = append(out, d)
		}
	}
	return out
}

// CapStudyMarkdown is one coin's table of the study. A window with missing
// days or under a week is marked indicative and its gaps are listed.
func CapStudyMarkdown(coin string, rows []CapRow, missing []time.Time) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	r0 := rows[0].Result
	fmt.Fprintf(&b, "### %s — liquidity cap %s → %s, %d decisions\n\n", coin,
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
	fmt.Fprintf(&b, "| Cap (%% of OI) | Avg IM (%% of limit) | Cycles under %.0f %% of limit, entries allowed | Entries cut or blocked | Lots cut | Legs capped | Net (%% of balance) | Worst week (%% of balance) |\n",
		strategy.UnderUseTargetPct)
	b.WriteString("|---|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		res := r.Result
		ww, _ := WorstWeek(res.Days, res.Equity0)
		net := 0.0
		if res.Equity0 > 0 {
			net = res.Net / res.Equity0 * 100
		}
		fmt.Fprintf(&b, "| %g | %.1f | %.1f %% (%d of %d) | %d (%d sent) | %.0f | %d | %+.2f | %+.2f |\n",
			r.CapPct, res.AvgUseOfLimitPct, r.UnderTargetShare(), res.UnderTargetCycles, res.EntryCycles,
			res.LiquidityEntriesCut, res.Entries, res.LiquidityLotsCut, res.LiquidityCapped, net, ww)
	}
	return b.String()
}
