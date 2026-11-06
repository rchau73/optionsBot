package replay

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/hedge"
	"optionsbot/internal/recorder"
	"optionsbot/internal/risk"
	"optionsbot/internal/strategy"
)

// The full-strangle study (#677): with the futures hedge holding the book's
// delta, must a strangle ever miss a leg? A missing leg earns nothing and
// makes the book directional (mvp ETH 2026-11-05: calls only). The
// study replays the same recorded books under today's rules and under the
// hedge with the rules that leave a leg missing relaxed as
// docs/studies/full-strangle-hedge.md proposes. Study only: the bot's rules
// are unchanged; which relaxation to build is the product owner's call, not
// before 2026-11-12.

// DefaultHedgedCooldownHours is the proposal's wait before a stopped leg is
// re-sold under the hedge (the bot's repair_cooldown_hours is 72).
const DefaultHedgedCooldownHours = 24

// FullStrangleVariant is one set of rules. The zero value is today's.
type FullStrangleVariant struct {
	// Hedged runs the proposal: the bot's perpetual holds the whole book's
	// coin delta, the GEX shed closes nothing, neither the regime side nor
	// the live shed signal holds a leg, and a stopped leg waits only for
	// CooldownHours, frozen or not.
	Hedged        bool
	CooldownHours int
}

func (v FullStrangleVariant) String() string {
	switch {
	case !v.Hedged:
		return "today"
	case v.CooldownHours == DefaultHedgedCooldownHours:
		return "hedged"
	}
	return fmt.Sprintf("hedged:%d", v.CooldownHours)
}

// ParseFullStrangle reads today | hedged | hedged:H (H = the cooldown in hours).
func ParseFullStrangle(s string) (FullStrangleVariant, error) {
	name, arg, hasArg := strings.Cut(strings.TrimSpace(s), ":")
	switch {
	case name == "today" && !hasArg:
		return FullStrangleVariant{}, nil
	case name == "hedged" && !hasArg:
		return FullStrangleVariant{Hedged: true, CooldownHours: DefaultHedgedCooldownHours}, nil
	case name == "hedged":
		if h, err := strconv.Atoi(arg); err == nil && h >= 0 {
			return FullStrangleVariant{Hedged: true, CooldownHours: h}, nil
		}
	}
	return FullStrangleVariant{}, fmt.Errorf("fullstrangle %q: want today | hedged | hedged:H with H ≥ 0 hours", s)
}

// ParseFullStrangles reads a comma-separated list.
func ParseFullStrangles(s string) ([]FullStrangleVariant, error) {
	var out []FullStrangleVariant
	for _, f := range strings.Split(s, ",") {
		v, err := ParseFullStrangle(f)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// StopHold says why a stopped leg may not be re-sold yet, "" when it may:
// today's RepairBlockReason, or under the hedge its cooldown alone (the
// freeze and the negative regime no longer hold it).
func (v FullStrangleVariant) StopHold(stoppedAt, now time.Time, cooldown time.Duration, st risk.Status) string {
	if v.Hedged {
		return strategy.RepairBlockReason(stoppedAt, now, time.Duration(v.CooldownHours)*time.Hour, risk.Status{})
	}
	return strategy.RepairBlockReason(stoppedAt, now, cooldown, st)
}

// stopHold is StopHold on the bot's cooldown.
func (e *engine) stopHold(stoppedAt, at time.Time, st risk.Status) string {
	return e.set.FullStrangle.StopHold(stoppedAt, at, time.Duration(e.cfg.RepairCooldownHours)*time.Hour, st)
}

// shedSkips reports whether the live shed signal holds a new short of optType.
func (e *engine) shedSkips(optType string, dec strategy.GammaDecision) bool {
	return !e.set.FullStrangle.Hedged && dec.Sheds(optType)
}

// perpContractUSD is the perpetual's contract on Deribit.
func perpContractUSD(underlying string) float64 {
	if strings.EqualFold(underlying, "BTC") {
		return 10
	}
	return 1
}

// coinDelta is the book's delta in the coin (StateManager.TotalCoinDelta).
func (e *engine) coinDelta() float64 {
	delta := 0.0
	for _, s := range e.book {
		for _, p := range s.legs() {
			delta -= p.Qty * (p.CurrentGreeks.Delta - p.CurrentMid)
		}
	}
	return delta
}

// holdBookDelta is the bot's stepFuturesHedge: the perpetual trades by the
// net delta once it is outside the band, at most FixedFuturesHedgeMaxPerHour
// orders a rolling hour. MODEL: filled at spot, and the options' delta is the
// last hourly book's, so what a move inside the hour adds is not seen.
func (e *engine) holdBookDelta(at time.Time, marginBalance, spot float64) {
	if !e.set.FullStrangle.Hedged {
		return
	}
	options := e.coinDelta()
	band := hedge.Band(marginBalance, false, spot, config.FixedFuturesHedgeBandPct)
	usd, side := hedge.HedgeOrder(options, hedge.PerpDelta(e.hedge.usd, spot), band, spot, perpContractUSD(e.cfg.Underlying))
	if side == "" {
		return
	}
	if hedge.RateLimited(e.perpOrders, at, config.FixedFuturesHedgeMaxPerHour) {
		e.res.HedgeRateLimited++
		return
	}
	e.perpOrders = append(e.perpOrders, at)
	if len(e.perpOrders) > config.FixedFuturesHedgeMaxPerHour { // only the rolling hour counts
		e.perpOrders = e.perpOrders[1:]
	}
	if side == hedge.SideSell {
		usd = -usd
	}
	e.tradePerp(at, e.hedge.usd+usd, fmt.Sprintf("hold the book's delta %+.3f", options))
}

// noteMissingLegs adds the time since the last decision for each leg that was
// missing over it (a one-legged strangle: a vacant slot misses no leg). A hole
// in the recording counts as maxBookAge at most.
func (e *engine) noteMissingLegs(at time.Time) {
	if !e.lastDecision.IsZero() {
		e.res.MissingLegHours += float64(e.missingLegs) * math.Min(at.Sub(e.lastDecision).Hours(), maxBookAge.Hours())
	}
	e.lastDecision, e.missingLegs = at, 0
	for _, s := range e.book {
		if len(s.legs()) == 1 {
			e.missingLegs++
		}
	}
}

// FullStrangleRow is one variant's replay at one cost multiple.
type FullStrangleRow struct {
	Variant      FullStrangleVariant
	CostMultiple float64
	Result       Result
}

// FullStrangleStudy replays snaps once per variant and cost multiple,
// everything else as set. Today's rules are added first when variants lack
// them: every verdict is against them.
func FullStrangleStudy(snaps []recorder.Snapshot, set Settings, variants []FullStrangleVariant, costs []float64) ([]FullStrangleRow, error) {
	if set.Cfg == nil {
		return nil, fmt.Errorf("replay: no settings")
	}
	hasToday := false
	for _, v := range variants {
		hasToday = hasToday || !v.Hedged
	}
	if !hasToday {
		variants = append([]FullStrangleVariant{{}}, variants...)
	}
	if len(costs) == 0 {
		costs = []float64{1}
	}
	var rows []FullStrangleRow
	for _, m := range costs {
		for _, v := range variants {
			s := set
			s.FullStrangle, s.CostMultiple = v, m
			if set.Trace != nil { // each row's actions, named
				s.Trace = func(at time.Time, line string) { set.Trace(at, fmt.Sprintf("%s %g×: %s", v, m, line)) }
			}
			res, err := Run(snaps, s)
			if err != nil {
				return nil, fmt.Errorf("fullstrangle %s at %g× costs: %w", v, m, err)
			}
			rows = append(rows, FullStrangleRow{Variant: v, CostMultiple: m, Result: res})
		}
	}
	return rows, nil
}

// MinComparableIMShare: a variant that averages less than this share of
// today's IM ran a different book, and a smaller book looks safer by its size
// alone (the replay's ETH from an empty book: a seventh of today's IM, because
// repair keeps a leg's size and the liquidity cap shrinks fresh entries).
const MinComparableIMShare = 0.5

// The study's own verdicts (FullStrangleVerdict).
const (
	VerdictNotComparable = "not comparable: book under half of today's average IM"
	VerdictWorseGap      = "not recommended: −20 % gap loss worse than today's"
)

// FullStrangleVerdict judges a variant against today's rules on the same books
// and costs: no verdict on a book too small to compare, never recommended when
// its gap loss is worse, then ShedVerdict's tail rule (worst week, max MM), a
// candidate only when it also nets more.
func FullStrangleVerdict(r FullStrangleRow, base Result, maxMMPct float64) string {
	if !r.Variant.Hedged {
		return VerdictBaseline
	}
	if r.Result.AvgIMPct < base.AvgIMPct*MinComparableIMShare {
		return VerdictNotComparable
	}
	if r.Result.MaxGapLossPct > base.MaxGapLossPct+1e-9 {
		return VerdictWorseGap
	}
	return ShedVerdict(ShedRow{Variant: ShedVariant{Mode: ShedHedge}, Result: r.Result}, base, maxMMPct)
}

// perWeek scales hours of the replay to hours per week.
func perWeek(hours float64, res Result) float64 {
	weeks := res.End.Sub(res.Start).Hours() / (7 * 24)
	if weeks <= 0 {
		return 0
	}
	return hours / weeks
}

// FullStrangleMarkdown is one coin's table of the study, marked indicative on
// a short or gapped recording (as the other studies).
func FullStrangleMarkdown(coin string, rows []FullStrangleRow, missing []time.Time, maxMMPct float64) string {
	if len(rows) == 0 {
		return ""
	}
	b := studyHeader(coin, "full strangles under the hedge", rows[0].Result, missing)
	b.WriteString("| Rules | Costs | Net " + coin + " | Net USD | Net (% of balance) | Legs missing (leg-hours) | Per week | Perp traded (USD) | Largest perp (USD) | Perp orders | Perp fees " + coin + " | Funding " + coin + " | Hedge P&L " + coin)
	for _, w := range ShedWindows {
		b.WriteString(" | " + w.Name + " (%)")
	}
	b.WriteString(" | Worst day (%) | Worst week (%) | Avg IM (%) | Net ÷ avg IM | Max MM (%) | −20 % gap loss (% of balance) | Stops | Delta exits | MM cuts | Verdict |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---" + strings.Repeat("|---", len(ShedWindows)) + "|---|---|---|---|---|---|---|---|---|---|\n")
	base := map[float64]Result{}
	for _, r := range rows {
		if !r.Variant.Hedged {
			base[r.CostMultiple] = r.Result
		}
	}
	for _, r := range rows {
		res := r.Result
		net := 0.0
		if res.Equity0 > 0 {
			net = res.Net / res.Equity0 * 100
		}
		fmt.Fprintf(b, "| %s | %g× | %+.4f | %+.0f | %+.2f | %.0f | %.0f | %.0f | %.0f | %d | %.4f | %.4f | %+.4f", r.Variant, r.CostMultiple, res.Net, NetUSD(res.Days),
			net, res.MissingLegHours, perWeek(res.MissingLegHours, res), res.PerpTradedUSD, res.MaxPerpUSD, res.HedgeTrades, res.PerpFees, res.PerpFunding, res.HedgePnL)
		for _, w := range ShedWindows {
			if p, n := WindowPnL(res.Days, w, res.Equity0); n > 0 {
				fmt.Fprintf(b, " | %+.2f (%d d)", p, n)
			} else {
				b.WriteString(" | not replayed")
			}
		}
		ww, _ := WorstWeek(res.Days, res.Equity0)
		perIM := "—"
		if res.AvgIM > 0 {
			perIM = fmt.Sprintf("%+.3f", res.Net/res.AvgIM)
		}
		fmt.Fprintf(b, " | %+.2f | %+.2f | %.1f | %s | %.1f | %.1f | %d | %d | %d | %s |\n", WorstDay(res.Days, res.Equity0), ww, res.AvgIMPct, perIM,
			res.MaxMMPct, res.MaxGapLossPct, res.Stops, res.DeltaExits, res.MMCuts, FullStrangleVerdict(r, base[r.CostMultiple], maxMMPct))
	}
	return b.String()
}
