package replay

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
	"optionsbot/internal/risk"
	"optionsbot/internal/strategy"
)

// The shed study (#637): on 2026-10-29 both BTC bots shed every put in the
// same minute after BTC fell from ~81.1k to ~77.7k, at −71 % to −184 % of the
// premium, and BTC never came near the strikes afterwards. The bot buys the
// whole shed side back (GammaDecision.Shed); the study replays the same
// recorded books once per variant of what a shed does, at the recorded costs
// and at twice them. Study only: the bot's shed is unchanged; a rule change
// is the product owner's call. Whatever the variant, entries and repairs
// still skip the side the live signal sheds (GammaDecision.Action), and the
// stop-loss, delta exit and MM cut run as in the bot.

// What a shed does (ShedVariant.Mode).
const (
	ShedToday   = "today"   // the bot: buy every leg of the side back
	ShedNone    = "none"    // nothing: the delta exit, the stop-loss and the MM cut guard the leg
	ShedPartial = "partial" // cut the side to its slot target × Keep (rebalancePositions' sizing)
	ShedDelta   = "delta"   // buy back only the legs with |Δ| ≥ MinDelta; the rest stay with the delta exit
	ShedHedge   = "hedge"   // keep the legs, hedge the side's delta with the perpetual until the shed's release
)

// DefaultShedVariants are the issue's variants, the bot's first.
const DefaultShedVariants = "today,none,partial:0.5,partial:0,delta:0.20,delta:0.25,hedge"

// The perp hedge (MODEL): Deribit's taker fee on the perpetual, and the band
// the hedge is left to drift in, in delta per option of the side.
const (
	PerpFeeRate = 0.0005
	HedgeBand   = 0.05
)

// ShedVariant is one way of shedding. The zero value is the bot's.
type ShedVariant struct {
	Mode     string
	Keep     float64 // partial: the share of the slot target kept (0 = the whole leg goes)
	MinDelta float64 // delta: the |Δ| from which a leg is bought back
}

func (v ShedVariant) mode() string {
	if v.Mode == "" {
		return ShedToday
	}
	return v.Mode
}

func (v ShedVariant) String() string {
	switch v.mode() {
	case ShedPartial:
		return fmt.Sprintf("partial:%g", v.Keep)
	case ShedDelta:
		return fmt.Sprintf("delta:%.2f", v.MinDelta)
	}
	return v.mode()
}

// ParseShedVariant reads today | none | partial:K | delta:D | hedge.
func ParseShedVariant(s string) (ShedVariant, error) {
	name, arg, hasArg := strings.Cut(strings.TrimSpace(s), ":")
	num := func(lo, hi float64, loOpen bool) (float64, error) {
		x, err := strconv.ParseFloat(arg, 64)
		if err != nil || x > hi || x < lo || (loOpen && x == lo) {
			return 0, fmt.Errorf("shed %q: want %s:N with N in %s%g, %g]", s, name, map[bool]string{true: "(", false: "["}[loOpen], lo, hi)
		}
		return x, nil
	}
	switch name {
	case ShedToday, ShedNone, ShedHedge:
		if hasArg {
			return ShedVariant{}, fmt.Errorf("shed %q: %s takes no number", s, name)
		}
		return ShedVariant{Mode: name}, nil
	case ShedPartial:
		k, err := num(0, 1, false)
		return ShedVariant{Mode: name, Keep: k}, err
	case ShedDelta:
		d, err := num(0, 1, true)
		return ShedVariant{Mode: name, MinDelta: d}, err
	}
	return ShedVariant{}, fmt.Errorf("shed %q: want today | none | partial:K | delta:D | hedge", s)
}

// ParseShedVariants reads a comma-separated list; "all" is DefaultShedVariants.
func ParseShedVariants(s string) ([]ShedVariant, error) {
	if s == "all" {
		s = DefaultShedVariants
	}
	var out []ShedVariant
	for _, f := range strings.Split(s, ",") {
		v, err := ParseShedVariant(f)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// shed runs the variant on the side GEX sheds (the bot: every leg bought
// back and anchored, shed.go).
func (e *engine) shed(at time.Time, dec strategy.GammaDecision, st risk.Status, quotes map[string]recorder.Quote, spot float64) {
	typ := "put"
	if dec.Shed == strategy.GammaActionCloseCalls {
		typ = "call"
	}
	if e.set.FullStrangle.Hedged { // as the bot with the hedge on: the perpetual holds the delta
		return
	}
	switch e.set.Shed.mode() {
	case ShedNone:
		return
	case ShedHedge:
		e.openHedge(at, typ, dec)
		return
	}
	for _, s := range e.book {
		p := s.leg(typ)
		if p == nil {
			continue
		}
		qty := e.shedQty(s, p, st, spot)
		if qty < 1e-9 {
			continue
		}
		before := e.res.Realised
		if e.buyBack(at, s, p, qty, quotes, "GEX shed") {
			e.res.ShedPnL += e.res.Realised - before
			e.res.Sheds++
			e.day.Sheds++
			if p.Qty < 1e-9 { // a leg left in part is not repaired: no anchor
				e.anchors[key(s, typ)] = &strategy.ShedAnchor{At: at, Flip: dec.GammaFlip, BufferPct: dec.FlipBufferPct}
			}
		}
	}
	e.prune()
}

// shedQty is how much of p the variant buys back.
func (e *engine) shedQty(s *strangle, p *orders.Position, st risk.Status, spot float64) float64 {
	v := e.set.Shed
	switch v.mode() {
	case ShedDelta:
		if math.Abs(p.CurrentGreeks.Delta) < v.MinDelta {
			return 0
		}
	case ShedPartial:
		keep := math.Floor(float64(e.slotTargetLots(s, st, spot))*v.Keep+1e-9) * e.lot
		if keep <= 0 {
			return p.Qty
		}
		return math.Max(math.Floor((p.Qty-keep)/e.lot+1e-9)*e.lot, 0)
	}
	return p.Qty
}

// slotTargetLots is the strangle's target as rebalancePositions sizes it: the
// slot share of the current limit over the margin of one lot of its legs on
// their own.
func (e *engine) slotTargetLots(s *strangle, st risk.Status, spot float64) int {
	u := e.usage(spot)
	share := strategy.SlotShare(st.LimitIMPct, u.MarginBalance, len(e.cfg.Slots()))
	var one []marginLeg
	for _, p := range s.legs() {
		one = append(one, marginLeg{p.OptionType, p.Strike, e.lot, years(p.Expiry, e.now), p.CurrentGreeks.IV})
	}
	return strategy.TargetLots(share, scanIM(one, spot))
}

// perpHedge is the hedge variant's short (or long) perpetual (MODEL: priced
// at the snapshot's spot, Deribit's taker fee, funding at
// Settings.PerpFundingPct; the contract's USD 10 step is not modelled).
type perpHedge struct {
	usd    float64   // USD notional, negative = short
	price  float64   // the price it was last marked at
	at     time.Time // when (funding accrues from then)
	side   string    // the shed side it hedges; "" = no hedge
	anchor *strategy.ShedAnchor
}

// markPerp books the hedge's move and funding since its last mark.
func (e *engine) markPerp(at time.Time, spot float64) {
	h := &e.hedge
	if h.usd != 0 && h.price > 0 {
		pnl := perpPnL(h.usd, h.price, spot)
		funding := math.Abs(h.usd) / spot * e.set.PerpFundingPct / 100 * at.Sub(h.at).Hours() / 24 / 365
		e.cash += pnl - funding
		e.res.HedgePnL += pnl - funding
		e.res.PerpFunding += funding
	}
	h.price, h.at = spot, at
}

// openHedge starts hedging typ at a shed, anchored as a shed leg would be.
func (e *engine) openHedge(at time.Time, typ string, dec strategy.GammaDecision) {
	if e.hedge.side == typ {
		return
	}
	if e.hedge.side != "" { // the other side now sheds: start over
		e.tradePerp(at, 0, "hedge unwound (side turned)")
	}
	e.hedge.side = typ
	e.hedge.anchor = &strategy.ShedAnchor{At: at, Flip: dec.GammaFlip, BufferPct: dec.FlipBufferPct}
	e.act(at, "GEX shed of the %ss: hedged with the perpetual instead", typ)
}

// maintainHedge re-hedges the side's delta outside the band and unwinds the
// hedge once the shed is released (the repair release a shed leg would wait
// for: ShedRepairBlockReason, or BiasRepairBlockReason under bias).
func (e *engine) maintainHedge(at time.Time, dec strategy.GammaDecision, spot float64) {
	h := &e.hedge
	if h.side == "" {
		return
	}
	c := e.cfg
	var why string
	if c.GEXDirection == strategy.DirectionBias {
		points := (h.side == "put" && dec.BiasDir < 0) || (h.side == "call" && dec.BiasDir > 0)
		h.anchor.ObserveBias(!points, dec.SnapshotAt)
		why = strategy.BiasRepairBlockReason(h.anchor, h.side, at, time.Duration(c.GEXBiasRepairCooldownHours)*time.Hour, c.GEXRepairConfirmSnapshots)
	} else {
		h.anchor.Observe(dec.Spot, dec.SnapshotAt)
		why = strategy.ShedRepairBlockReason(h.anchor, at, dec.NonNegativeSince, c.GEXRepairConfirmSnapshots,
			time.Duration(c.GEXRepairReleaseHours)*time.Hour)
	}
	if why == "" && !dec.Sheds(h.side) {
		if h.usd != 0 {
			e.tradePerp(at, 0, "hedge unwound at the shed's release")
		}
		h.side, h.anchor = "", nil
		return
	}
	delta, qty := 0.0, 0.0 // the side's delta in the coin: a short put is long the coin
	for _, s := range e.book {
		if p := s.leg(h.side); p != nil {
			delta -= p.Qty * p.CurrentGreeks.Delta
			qty += p.Qty
		}
	}
	target := -delta * spot
	if math.Abs(target-h.usd)/spot > HedgeBand*qty || (qty == 0 && h.usd != 0) {
		e.tradePerp(at, target, fmt.Sprintf("hedge the %ss' delta %+.3f", h.side, delta))
	}
}

// tradePerp moves the hedge to usd (USD notional) at the last mark, paying
// the taker fee on the change.
func (e *engine) tradePerp(at time.Time, usd float64, why string) {
	h := &e.hedge
	change := usd - h.usd
	if change == 0 || h.price <= 0 {
		return
	}
	fee := math.Abs(change) / h.price * PerpFeeRate * e.costMultiple()
	e.cash -= fee
	e.res.HedgePnL -= fee
	e.res.Fees += fee
	e.day.Fees += fee
	e.res.HedgeTrades++
	e.res.PerpFees += fee
	e.res.PerpTradedUSD += math.Abs(change)
	e.res.MaxPerpUSD = math.Max(e.res.MaxPerpUSD, math.Abs(usd))
	e.day.Trades++
	h.usd = usd
	e.act(at, "%s: perp %+.0f USD at %.0f (fee %.5f)", why, usd, h.price, fee)
}

func (e *engine) costMultiple() float64 { return math.Max(e.set.CostMultiple, 1) }

// widen is the quote the study's cost multiple fills on.
func (e *engine) widen(q recorder.Quote) recorder.Quote { return Widen(q, e.costMultiple()) }

// fee is Deribit's option fee times the study's cost multiple.
func (e *engine) fee(price, qty float64) float64 { return Fee(price, qty) * e.costMultiple() }

// ShedRow is one variant's replay at one cost multiple.
type ShedRow struct {
	Variant      ShedVariant
	CostMultiple float64
	Result       Result
}

// ShedStudy replays snaps once per variant and cost multiple, everything
// else as set. The bot's shed is added first when variants lack it: every
// verdict is against it.
func ShedStudy(snaps []recorder.Snapshot, set Settings, variants []ShedVariant, costs []float64) ([]ShedRow, error) {
	if set.Cfg == nil {
		return nil, fmt.Errorf("replay: no settings")
	}
	hasToday := false
	for _, v := range variants {
		hasToday = hasToday || v.mode() == ShedToday
	}
	if !hasToday {
		variants = append([]ShedVariant{{Mode: ShedToday}}, variants...)
	}
	if len(costs) == 0 {
		costs = []float64{1}
	}
	var rows []ShedRow
	for _, m := range costs {
		for _, v := range variants {
			s := set
			s.Shed, s.CostMultiple, s.Trace = v, m, nil
			res, err := Run(snaps, s)
			if err != nil {
				return nil, fmt.Errorf("shed %s at %g× costs: %w", v, m, err)
			}
			rows = append(rows, ShedRow{Variant: v, CostMultiple: m, Result: res})
		}
	}
	return rows, nil
}

// Window is a named stretch of days the study reports on its own.
type Window struct {
	Name     string
	From, To time.Time // UTC days, both included
}

func utcDay(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// ShedWindows are the issue's named windows: the early sheds and the
// 2026-10-29 shed with the week after it.
var ShedWindows = []Window{
	{"9–17 Oct", utcDay(2026, 10, 9), utcDay(2026, 10, 17)},
	{"29 Oct–4 Nov", utcDay(2026, 10, 29), utcDay(2026, 11, 4)},
}

// WindowPnL is the change of the margin balance over the replayed days inside
// w, as % of the starting balance, and how many replayed days it covers.
func WindowPnL(days []Day, w Window, equity0 float64) (pct float64, n int) {
	if equity0 <= 0 {
		return 0, 0
	}
	sum := 0.0
	for _, d := range days {
		if !d.Date.Before(w.From) && !d.Date.After(w.To) {
			sum += d.PnL
			n++
		}
	}
	return sum / equity0 * 100, n
}

// WorstDay is the worst day's change of the margin balance, as % of the
// starting balance (0 when no day lost).
func WorstDay(days []Day, equity0 float64) float64 {
	if equity0 <= 0 {
		return 0
	}
	worst := 0.0
	for _, d := range days {
		worst = math.Min(worst, d.PnL)
	}
	return worst / equity0 * 100
}

// NetUSD is the replay's net in USD: each day's change at that day's spot.
func NetUSD(days []Day) float64 {
	usd := 0.0
	for _, d := range days {
		usd += d.PnL * d.Spot
	}
	return usd
}

// The study's verdicts (ShedVerdict).
const (
	VerdictBaseline     = "baseline"
	VerdictCandidate    = "candidate"
	VerdictWorseWeek    = "not recommended: worst week worse than today's"
	VerdictWorseMM      = "not recommended: max MM above today's"
	VerdictReachesMaxMM = "not recommended: MM reached max_mm_pct"
	VerdictNoGain       = "no gain on today's"
)

// ShedVerdict judges a variant against the bot's shed on the same books and
// costs (#637's tail rule): never recommended when its worst week or its
// highest MM is worse than today's, or its MM reached maxMMPct; a candidate
// only when it also nets more.
func ShedVerdict(r ShedRow, base Result, maxMMPct float64) string {
	if r.Variant.mode() == ShedToday {
		return VerdictBaseline
	}
	res := r.Result
	ww, _ := WorstWeek(res.Days, res.Equity0)
	bw, _ := WorstWeek(base.Days, base.Equity0)
	switch {
	case ww < bw-1e-9:
		return VerdictWorseWeek
	case res.MaxMMPct >= maxMMPct:
		return VerdictReachesMaxMM
	case res.MaxMMPct > base.MaxMMPct+1e-9:
		return VerdictWorseMM
	case res.Net <= base.Net:
		return VerdictNoGain
	}
	return VerdictCandidate
}

// shedBase is the bot's row at r's cost multiple.
func shedBase(rows []ShedRow, m float64) (Result, bool) {
	for _, r := range rows {
		if r.CostMultiple == m && r.Variant.mode() == ShedToday {
			return r.Result, true
		}
	}
	return Result{}, false
}

// ShedCandidates are the variants that are candidates at every cost multiple.
func ShedCandidates(rows []ShedRow, maxMMPct float64) []string {
	verdicts := map[string][]string{}
	var order []string
	for _, r := range rows {
		base, _ := shedBase(rows, r.CostMultiple)
		name := r.Variant.String()
		if _, seen := verdicts[name]; !seen {
			order = append(order, name)
		}
		verdicts[name] = append(verdicts[name], ShedVerdict(r, base, maxMMPct))
	}
	var out []string
	for _, name := range order {
		all := true
		for _, v := range verdicts[name] {
			all = all && v == VerdictCandidate
		}
		if all {
			out = append(out, name)
		}
	}
	return out
}

// ShedStudyMarkdown is one coin's table of the study, marked indicative on a
// short or gapped recording (as the other studies), with the named windows
// and the modelled gap.
func ShedStudyMarkdown(coin string, rows []ShedRow, missing []time.Time, maxMMPct float64) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	r0 := rows[0].Result
	fmt.Fprintf(&b, "### %s — GEX shed variants, %s → %s, %d decisions\n\n", coin,
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
	b.WriteString("| Shed | Costs | Net " + coin + " | Net USD | Net (% of balance) | Shed closes (% of balance, per break) | Hedge P&L " + coin)
	for _, w := range ShedWindows {
		b.WriteString(" | " + w.Name + " (%)")
	}
	b.WriteString(" | Worst day (%) | Worst week (%) | Max MM (%) | −20 % gap loss (% of balance) | Stops | Delta exits | MM cuts | Sheds | Net ÷ avg IM | Verdict |\n")
	b.WriteString("|---|---|---|---|---|---|---" + strings.Repeat("|---", len(ShedWindows)) + "|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		res := r.Result
		pct := func(x float64) float64 {
			if res.Equity0 <= 0 {
				return 0
			}
			return x / res.Equity0 * 100
		}
		perBreak := "—"
		if res.ShedBreaks > 0 {
			perBreak = fmt.Sprintf("%+.2f (%+.2f × %d)", pct(res.ShedPnL), pct(res.ShedPnL)/float64(res.ShedBreaks), res.ShedBreaks)
		}
		fmt.Fprintf(&b, "| %s | %g× | %+.4f | %+.0f | %+.2f | %s | %+.4f", r.Variant, r.CostMultiple, res.Net, NetUSD(res.Days),
			pct(res.Net), perBreak, res.HedgePnL)
		for _, w := range ShedWindows {
			if p, n := WindowPnL(res.Days, w, res.Equity0); n > 0 {
				fmt.Fprintf(&b, " | %+.2f (%d d)", p, n)
			} else {
				b.WriteString(" | not replayed")
			}
		}
		ww, _ := WorstWeek(res.Days, res.Equity0)
		perIM := "—"
		if res.AvgIM > 0 {
			perIM = fmt.Sprintf("%+.3f", res.Net/res.AvgIM)
		}
		base, _ := shedBase(rows, r.CostMultiple)
		fmt.Fprintf(&b, " | %+.2f | %+.2f | %.1f | %.1f | %d | %d | %d | %d | %s | %s |\n", WorstDay(res.Days, res.Equity0), ww,
			res.MaxMMPct, res.MaxGapLossPct, res.Stops, res.DeltaExits, res.MMCuts, res.Sheds, perIM, ShedVerdict(r, base, maxMMPct))
	}
	if c := ShedCandidates(rows, maxMMPct); len(c) > 0 {
		fmt.Fprintf(&b, "\nCandidates at every cost multiple: %s.\n", strings.Join(c, ", "))
	} else {
		b.WriteString("\nNo variant beats today's shed without a worse worst week or max MM at every cost multiple.\n")
	}
	return b.String()
}
