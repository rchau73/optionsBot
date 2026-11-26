package replay

import (
	"fmt"
	"strings"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/recorder"
)

// The one-sided hedge study (#812): since 2026-11-05 the calls-only books
// lost on the futures hedge although every call closed at a profit — the
// hedge's long perpetual cancelled the bear book's delta through the fall.
// Each variant runs today's rules with the bot's hedge (the shed closes
// nothing, entries still skip the shed side, the regime side holds) and
// changes one thing about what the perpetual holds or when it trades. Study
// only: the bot's hedge is unchanged.

// The rules of the study (HedgeMode.Rule).
const (
	HedgeOff      = "off"      // the bot with the hedge off: today's GEX shed, no perpetual
	HedgeBot      = "bot"      // the bot with the hedge on (#657)
	HedgeOneSided = "onesided" // (1) the hedge stands down while the book holds one side only
	HedgeTrend    = "trend"    // (2) the hedge holds only the delta that opposes the trend
)

// DefaultHedgeModes is the study's list when none is given.
const DefaultHedgeModes = "off,bot,onesided,trend,bot+after,onesided+after,trend+after"

// HedgeMode is one variant: the rule and, with After, (3) the hedge trading
// after repairs and entries, so a roll's close and its re-sell net out first.
type HedgeMode struct {
	Rule  string
	After bool
}

func (m HedgeMode) String() string {
	if m.After {
		return m.Rule + "+after"
	}
	return m.Rule
}

// on reports whether the study's hedge replaces the shed (the zero value and
// off: no study hedge).
func (m HedgeMode) on() bool { return m.Rule != "" && m.Rule != HedgeOff }

// ParseHedgeMode reads off | bot | onesided | trend, each optionally +after.
func ParseHedgeMode(s string) (HedgeMode, error) {
	rule, after := strings.CutSuffix(strings.TrimSpace(s), "+after")
	switch rule {
	case HedgeOff:
		if after {
			return HedgeMode{}, fmt.Errorf("hedge mode %q: off has no hedge to hold", s)
		}
		return HedgeMode{Rule: rule}, nil
	case HedgeBot, HedgeOneSided, HedgeTrend:
		return HedgeMode{Rule: rule, After: after}, nil
	}
	return HedgeMode{}, fmt.Errorf("hedge mode %q: want off | bot | onesided | trend, each optionally +after", s)
}

// ParseHedgeModes reads a comma-separated list.
func ParseHedgeModes(s string) ([]HedgeMode, error) {
	var out []HedgeMode
	for _, f := range strings.Split(s, ",") {
		m, err := ParseHedgeMode(f)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// HedgedDelta is the part of the options' coin delta the perpetual holds at
// zero under rule, and whether the hedge stands down altogether (perpetual
// flat, the GEX shed back). trendDir: +1 bull, −1 bear, 0 neutral.
//   - bot: all of it.
//   - onesided: all of it, unless the book holds one side only: then none.
//   - trend: in a bear trend only a long book delta (it loses in the fall),
//     in a bull trend only a short one; neutral: all of it.
func HedgedDelta(rule string, options float64, trendDir int, oneSided bool) (delta float64, standDown bool) {
	switch rule {
	case HedgeOneSided:
		if oneSided {
			return 0, true
		}
	case HedgeTrend:
		switch {
		case trendDir < 0 && options < 0, trendDir > 0 && options > 0:
			return 0, false // the book leans with the trend: kept
		}
	}
	return options, false
}

// oneSided reports whether the book holds legs of one option type only.
func (e *engine) oneSided() bool {
	types := map[string]bool{}
	for _, s := range e.book {
		for _, p := range s.legs() {
			types[p.OptionType] = true
		}
	}
	return len(types) == 1
}

// hedgeKeepsShed is the bot's hedgeKeepsShed under the study: the hedge is
// on and has not stood down.
func (e *engine) hedgeKeepsShed() bool {
	m := e.set.Hedge
	return m.on() && !(m.Rule == HedgeOneSided && e.oneSided())
}

// studyHedgeDelta is the delta holdBookDelta holds for the study, or ok =
// false after standing down (the perpetual closed).
func (e *engine) studyHedgeDelta(at time.Time, options float64) (float64, bool) {
	delta, standDown := HedgedDelta(e.set.Hedge.Rule, options, e.dec.TrendDir, e.oneSided())
	if standDown {
		if e.hedge.usd != 0 {
			e.tradePerp(at, 0, "hedge stands down: the book holds one side only")
		}
		return 0, false
	}
	return delta, true
}

// HedgeModeRow is one variant's replay at one cost multiple.
type HedgeModeRow struct {
	Mode         HedgeMode
	CostMultiple float64
	Result       Result
}

// HedgeModeStudy replays snaps once per mode and cost multiple, everything
// else as set. The bot's row is added when modes lack it: every verdict is
// against it.
func HedgeModeStudy(snaps []recorder.Snapshot, set Settings, modes []HedgeMode, costs []float64) ([]HedgeModeRow, error) {
	if set.Cfg == nil {
		return nil, fmt.Errorf("replay: no settings")
	}
	hasBot := false
	for _, m := range modes {
		hasBot = hasBot || m == HedgeMode{Rule: HedgeBot}
	}
	if !hasBot {
		modes = append([]HedgeMode{{Rule: HedgeBot}}, modes...)
	}
	if len(costs) == 0 {
		costs = []float64{1}
	}
	var rows []HedgeModeRow
	for _, c := range costs {
		for _, m := range modes {
			s := set
			s.Hedge, s.CostMultiple = m, c
			if set.Trace != nil {
				s.Trace = func(at time.Time, line string) { set.Trace(at, fmt.Sprintf("%s %g×: %s", m, c, line)) }
			}
			res, err := Run(snaps, s)
			if err != nil {
				return nil, fmt.Errorf("hedge mode %s at %g× costs: %w", m, c, err)
			}
			rows = append(rows, HedgeModeRow{Mode: m, CostMultiple: c, Result: res})
		}
	}
	return rows, nil
}

// HedgeModeVerdict judges a mode against the bot's hedge on the same books
// and costs, by ShedVerdict's tail rule (worst week, max MM), a candidate
// only when it also nets more.
func HedgeModeVerdict(r HedgeModeRow, base Result, maxMMPct float64) string {
	if r.Mode == (HedgeMode{Rule: HedgeBot}) {
		return VerdictBaseline
	}
	return ShedVerdict(ShedRow{Variant: ShedVariant{Mode: ShedHedge}, Result: r.Result}, base, maxMMPct)
}

// HedgeModeMarkdown is one coin's table of the study.
func HedgeModeMarkdown(coin string, rows []HedgeModeRow, missing []time.Time, maxMMPct float64) string {
	if len(rows) == 0 {
		return ""
	}
	b := studyHeader(coin, "the futures hedge on a one-sided book", rows[0].Result, missing)
	b.WriteString("| Hedge | Costs | Net " + coin + " | Net USD | Net (% of balance) | Hedge P&L " + coin + " | Options " + coin +
		" | Perp orders | Perp traded (USD) | Largest perp (USD) | Perp fees " + coin +
		" | Worst day (%) | Worst week (%) | Avg IM (%) | Max MM (%) | −20 % gap loss (% of balance) | Stops | Delta exits | Verdict |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	base := map[float64]Result{}
	for _, r := range rows {
		if r.Mode == (HedgeMode{Rule: HedgeBot}) {
			base[r.CostMultiple] = r.Result
		}
	}
	for _, r := range rows {
		res := r.Result
		net := 0.0
		if res.Equity0 > 0 {
			net = res.Net / res.Equity0 * 100
		}
		ww, _ := WorstWeek(res.Days, res.Equity0)
		fmt.Fprintf(b, "| %s | %g× | %+.4f | %+.0f | %+.2f | %+.4f | %+.4f | %d | %.0f | %.0f | %.4f | %+.2f | %+.2f | %.1f | %.1f | %.1f | %d | %d | %s |\n",
			r.Mode, r.CostMultiple, res.Net, res.NetInUSD(), net, res.HedgePnL, res.Net-res.HedgePnL,
			res.HedgeTrades, res.PerpTradedUSD, res.MaxPerpUSD, res.PerpFees,
			WorstDay(res.Days, res.Equity0), ww, res.AvgIMPct, res.MaxMMPct, res.MaxGapLossPct, res.Stops, res.DeltaExits,
			HedgeModeVerdict(r, base[r.CostMultiple], maxMMPct))
	}
	return b.String()
}

// HedgeModeNote is the study's rules line for the command's header.
func HedgeModeNote() string {
	return fmt.Sprintf("off = the bot with the hedge off (GEX shed). bot = the hedge on: the perpetual holds the options' coin delta within %d %% of the balance (at most %d orders/h), the shed closes nothing, entries still skip the shed side, the regime side holds, the hedge trades before repairs and entries. onesided = the hedge stands down (perpetual closed, shed back) while the book holds one side only. trend = only the delta opposing the trend is hedged (bear: a long book delta, bull: a short one, neutral: all). +after = the hedge trades after repairs and entries, so a roll's close and re-sell net out first.",
		config.FixedFuturesHedgeBandPct, config.FixedFuturesHedgeMaxPerHour)
}
