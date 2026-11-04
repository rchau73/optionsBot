package replay

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
	"optionsbot/internal/risk"
	"optionsbot/internal/strategy"
)

// The rebalance model (#638): the bot's rebalancePositions in the replay, so
// downsizes and top-ups happen as the bot sizes them — and a study of which
// strangle a downsize cuts. The bot cuts every slot to its share of the IM
// limit (pro-rata), which buys the tested put back at its worst price
// (2026-10-22 stable BTC: the 27 Nov 75k put cut 3.1 → 1.3 lots at −65 %).
// The study compares that with cutting whole strangles in order of IM freed
// per coin of cost, or the biggest winners first. Study only: the bot's rule
// is unchanged; a rule change is the product owner's call.
//
// When a pass runs, as the bot (strategy/limits.go enforceMarginPolicy):
//   - the limit in force is taken as applied at the replay's start (the bot
//     was already running); a confirmed limit change makes a pass due, and
//     its downsizes go to the exact limit (DownsizeDue with limitChanged);
//   - a top-up that is held or fills short re-arms the pass after
//     rebalance_retry_minutes, backed off as the bot (RebalanceRetryWait);
//     that retry cuts only above the limit + rebalance_startup_band_pct;
//   - no pass while frozen, before the limit rests on confirmed data, without
//     fresh GEX, or at the MM limit (the MM cut runs instead).
//
//   - with Settings.Restarts (the journal's starts), each restart makes the
//     next pass a startup pass: nothing if IM is within ±band of the limit
//     (RebalanceInBand), else a pass that cuts only above limit + band —
//     2026-10-22 19:48 stable BTC: a deploy read IM 24.6 % of 20 % and cut.
//
// Not modelled: the churn breaker, the restart's other effects (the book is
// not regrouped), and a downsize that fills in part (it fills whole at the
// ask or waits for the next decision).

// The cut rules the study compares (Settings.Rebalance; "" = no rebalance).
const (
	RebalanceProRata  = "prorata"  // the bot: every strangle cut to its slot share, at least one lot kept
	RebalanceCheapest = "cheapest" // whole strangles, least realised cost per unit of IM freed first
	RebalanceWinners  = "winners"  // whole strangles, highest captured premium % first
)

// Rebalances are the rules -rebalance and -rebalances accept.
var Rebalances = []string{RebalanceProRata, RebalanceCheapest, RebalanceWinners}

// SlotOverShareRatio is the study's guard: after a cut no slot should hold
// more than 1.5 × its slot share of the IM limit.
const SlotOverShareRatio = 1.5

// Downsize is one leg a replayed downsize bought back.
type Downsize struct {
	At         time.Time
	Instrument string
	Qty        float64 // bought back
	FromQty    float64 // the leg's size before
	PnL        float64 // realised, net of fees
}

// rebalanceState is what the bot keeps between passes (strategy.Strategy's
// appliedLimit, limitChanged, rebalanceRetryAt, unfilledPasses).
type rebalanceState struct {
	started      bool
	next         int     // the next of Settings.Restarts
	startup      bool    // the next pass is the first since a restart: the band applies (#123)
	applied      float64 // the limit the last pass met; NaN = a pass is due
	prev         float64 // the limit at the previous decision
	limitChanged bool
	retryAt      time.Time
	unfilled     int
}

// rebalance runs a pass when one is due (enforceMarginPolicy). It runs after
// the exits and the MM cut, before repairs and entries.
func (e *engine) rebalance(at time.Time, snap recorder.Snapshot, dec strategy.GammaDecision, st risk.Status,
	quotes map[string]recorder.Quote, spot float64, gexFresh, stopsDeferred bool) {
	r := &e.rb
	restarted := false
	for r.next < len(e.set.Restarts) && !e.set.Restarts[r.next].After(at) {
		r.next++
		restarted = true
	}
	if !r.started {
		r.started, r.applied, r.prev = true, st.LimitIMPct, st.LimitIMPct
		return
	}
	if restarted { // a fresh Strategy: nothing applied, the startup band, no pending change
		r.applied, r.startup, r.limitChanged, r.retryAt, r.unfilled = math.NaN(), true, false, time.Time{}, 0
		r.prev = st.LimitIMPct
	}
	if st.LimitIMPct != r.prev { // noteRiskChange
		r.prev, r.limitChanged, r.unfilled, r.retryAt, r.startup = st.LimitIMPct, true, 0, time.Time{}, false
	}
	if st.LimitIMPct == r.applied || at.Before(r.retryAt) {
		return
	}
	if st.Frozen || !st.CanRebalance || !gexFresh {
		return // RebalanceBlockReason: the pass waits
	}
	if r.startup {
		r.startup = false
		if strategy.RebalanceInBand(e.usage(spot).IMPct(), st.LimitIMPct, e.cfg.RebalanceStartupBandPct) {
			r.applied = st.LimitIMPct // skipStartupRebalance
			e.res.StartupInBand++
			return
		}
	}
	r.applied = st.LimitIMPct
	if e.rebalancePass(at, snap, dec, st, quotes, spot, stopsDeferred) {
		r.limitChanged = false
	} else {
		r.applied = math.NaN() // a buy-back found no ask: again next decision
	}
}

// rearm makes the pass run again after the retry wait (rearmRebalance).
func (e *engine) rearm(at time.Time, backoff bool) {
	c := e.cfg
	base := time.Duration(c.RebalanceRetryMinutes) * time.Minute
	wait := base
	if backoff {
		wait = strategy.RebalanceRetryWait(base, time.Duration(c.RebalanceRetryMaxMinutes)*time.Minute, e.rb.unfilled, c.RebalanceBackoffAfter)
	}
	e.rb.applied = math.NaN()
	e.rb.retryAt = at.Add(wait)
}

// rebalancePass is one pass toward the limit: cuts when IM is over it (by
// the rule under study), top-ups when under (the bot's, for every rule). It
// reports false when a cut could not fill.
func (e *engine) rebalancePass(at time.Time, snap recorder.Snapshot, dec strategy.GammaDecision, st risk.Status,
	quotes map[string]recorder.Quote, spot float64, stopsDeferred bool) bool {
	u := e.usage(spot)
	limit := st.LimitIMPct
	share := strategy.SlotShare(limit, u.MarginBalance, len(e.cfg.Slots()))
	e.res.RebalancePasses++
	if u.IMPct() <= limit {
		e.topUps(at, snap, dec, st, quotes, spot, share, u.Headroom(limit), stopsDeferred)
		return true
	}
	if !strategy.DownsizeDue(u.IMPct(), limit, e.cfg.RebalanceStartupBandPct, e.rb.limitChanged) {
		return true // drift within the band: kept
	}
	before, cost0, legs0 := u.IMPct(), e.res.DownsizeCost, e.res.DownsizeLegs
	var done bool
	if e.set.Rebalance == RebalanceProRata {
		done = e.cutProRata(at, quotes, spot, share)
	} else {
		done = e.cutGreedy(at, quotes, spot, limit)
	}
	e.prune()
	if e.res.DownsizeLegs == legs0 {
		return done
	}
	after := e.usage(spot).IMPct()
	e.res.Downsizes++
	e.res.DownsizeIMFreedPP += before - after
	if after > limit+1e-9 {
		e.res.CutsOverLimit++
	}
	for _, s := range e.book {
		if s.call != nil && s.put != nil && math.Abs(s.call.Qty-s.put.Qty) > e.lot+1e-9 {
			e.res.CutsUneven++
		}
	}
	ratio := e.slotShareRatio(spot, limit)
	e.res.MaxSlotShareRatio = math.Max(e.res.MaxSlotShareRatio, ratio)
	if ratio > SlotOverShareRatio {
		e.res.CutsSlotOverShare++
	}
	e.act(at, "downsize pass (%s): IM %.1f%% → %.1f%% of limit %.0f%%, %+.4f", e.set.Rebalance, before, after, limit,
		e.res.DownsizeCost-cost0)
	return done
}

// resizable are the strangles a pass may resize: both legs in the book and
// outside the rollout window (they roll anyway).
func (e *engine) resizable(at time.Time) []*strangle {
	var out []*strangle
	for _, s := range e.book {
		if s.call != nil && s.put != nil && s.call.DTEAt(at) > e.cfg.RolloutDTE {
			out = append(out, s)
		}
	}
	return out
}

// targetLots is a strangle's slot target in lots: the slot share ÷ the IM
// one lot of it adds to the book, capped at the size multiple of its normal
// size (on its own). 0 = it adds no IM (netting): size kept.
func (e *engine) targetLots(s *strangle, spot, share float64) (lots int, imPerLot float64) {
	imPerLot = e.imPerLot(s, spot)
	lots = strategy.TargetLots(share, imPerLot)
	if lots == 0 {
		return 0, imPerLot
	}
	alone := []marginLeg{e.marginLeg(s.call, e.lot), e.marginLeg(s.put, e.lot)}
	lots, _ = strategy.CapLots(lots, strategy.TargetLots(share, scanIM(alone, spot)), e.cfg.MaxLegSizeMultiple)
	return lots, imPerLot
}

// imPerLot is the IM buying one lot of each of s's legs back frees.
func (e *engine) imPerLot(s *strangle, spot float64) float64 {
	return scanIM(e.marginLegs(), spot) - scanIM(e.marginLegsLess(s, e.lot), spot)
}

func (e *engine) marginLeg(p *orders.Position, qty float64) marginLeg {
	return marginLeg{p.OptionType, p.Strike, qty, years(p.Expiry, e.now), p.CurrentGreeks.IV}
}

// marginLegsLess is the book with qty less of each of s's legs.
func (e *engine) marginLegsLess(s *strangle, qty float64) []marginLeg {
	var out []marginLeg
	for _, b := range e.book {
		for _, p := range b.legs() {
			q := p.Qty
			if b == s {
				q = math.Max(q-qty, 0)
			}
			out = append(out, e.marginLeg(p, q))
		}
	}
	return out
}

// slotTotals sums the open call and put sizes of every strangle in slot.
func (e *engine) slotTotals(slot config.StrangleSlot) (call, put float64) {
	for _, s := range e.book {
		if s.slot != slot {
			continue
		}
		if s.call != nil {
			call += s.call.Qty
		}
		if s.put != nil {
			put += s.put.Qty
		}
	}
	return call, put
}

// cutProRata is the bot's downsize: each strangle's legs to what its slot's
// target leaves them, at least one lot kept, in book order, each measured
// against the book as the earlier cuts left it.
func (e *engine) cutProRata(at time.Time, quotes map[string]recorder.Quote, spot, share float64) bool {
	done := true
	for _, s := range e.resizable(at) {
		lots, _ := e.targetLots(s, spot, share)
		if lots == 0 {
			continue
		}
		targetQty := float64(lots) * e.lot
		callTotal, putTotal := e.slotTotals(s.slot)
		if orders.FloorToStep(math.Min(targetQty-callTotal, targetQty-putTotal), e.lot) >= e.lot {
			continue // below its target: kept (no headroom while over the limit)
		}
		for _, leg := range []struct {
			p     *orders.Position
			total float64
		}{{s.call, callTotal}, {s.put, putTotal}} {
			keep := math.Max(targetQty-(leg.total-leg.p.Qty), e.lot)
			excess := orders.FloorToStep(leg.p.Qty-keep, e.lot)
			if excess < e.lot {
				continue
			}
			if !e.downsize(at, s, leg.p, excess, quotes) {
				done = false
			}
		}
	}
	return done
}

// cutGreedy is the study's alternative: the best strangle by the rule is cut
// on both legs evenly, by the lots its IM per lot says are needed (whole if
// need be), then the next, until IM is at the limit or nothing is left.
func (e *engine) cutGreedy(at time.Time, quotes map[string]recorder.Quote, spot, limit float64) bool {
	done := true
	skip := map[*strangle]bool{}
	for {
		u := e.usage(spot)
		need := u.IM - limit/100*u.MarginBalance
		if need <= 0 {
			return done
		}
		var best *strangle
		var bestScore, bestPerLot float64
		for _, s := range e.resizable(at) {
			if skip[s] || math.Min(s.call.Qty, s.put.Qty) < e.lot-1e-9 {
				continue
			}
			perLot := e.imPerLot(s, spot)
			score, ok := e.cutScore(s, quotes, perLot)
			if !ok {
				continue
			}
			if best == nil || score < bestScore || (score == bestScore && s.id < best.id) {
				best, bestScore, bestPerLot = s, score, perLot
			}
		}
		if best == nil {
			return done
		}
		maxLots := math.Floor(math.Min(best.call.Qty, best.put.Qty)/e.lot + 1e-9)
		lots := math.Min(maxLots, math.Max(1, math.Ceil(need/bestPerLot-1e-9)))
		qty := lots * e.lot
		filled := false
		for _, p := range []*orders.Position{best.call, best.put} {
			if e.downsize(at, best, p, qty, quotes) {
				filled = true
			} else {
				done = false
			}
		}
		if !filled || lots >= maxLots {
			skip[best] = true // fully cut, or no ask on either leg
		}
		e.prune()
	}
}

// cutScore ranks a strangle for cutGreedy, lowest first; ok is false when
// cutting it frees no IM or a leg has no ask.
//   - cheapest: what one lot of each leg costs to buy back at the ask, less
//     the premium it was sold for, plus fees, per unit of IM freed (a winner
//     costs less than nothing and comes first);
//   - winners: minus the premium captured, as % of the premium received.
func (e *engine) cutScore(s *strangle, quotes map[string]recorder.Quote, perLot float64) (float64, bool) {
	if perLot <= 0 {
		return 0, false
	}
	cost, premium, value := 0.0, 0.0, 0.0
	for _, p := range []*orders.Position{s.call, s.put} {
		f := BuyFill(quotes[p.Instrument])
		if !f.OK || p.Qty <= 0 {
			return 0, false
		}
		cost += (f.Price-p.PremiumReceived/p.Qty)*e.lot + Fee(f.Price, e.lot)
		premium += p.PremiumReceived
		value += p.CurrentMid * p.Qty
	}
	if e.set.Rebalance == RebalanceWinners {
		if premium <= 0 {
			return 0, false
		}
		return -(premium - value) / premium, true
	}
	return cost / perLot, true
}

// downsize buys qty of p back at the ask as a rebalance downsize.
func (e *engine) downsize(at time.Time, s *strangle, p *orders.Position, qty float64, quotes map[string]recorder.Quote) bool {
	from, realised := p.Qty, e.res.Realised
	if !e.buyBack(at, s, p, qty, quotes, "downsize") {
		return false
	}
	pnl := e.res.Realised - realised
	e.res.DownsizeCost += pnl
	e.res.DownsizeLegs++
	e.res.DownsizeLots += qty / e.lot
	e.res.DownsizeLog = append(e.res.DownsizeLog, Downsize{At: at, Instrument: p.Instrument, Qty: qty, FromQty: from, PnL: pnl})
	return true
}

// slotShareRatio is the largest slot's IM (its legs on their own) as a
// multiple of its slot share of the limit (SlotShare): 1 = at its share. A
// vacant slot does not raise it; a cut that leaves one expiry heavy does.
func (e *engine) slotShareRatio(spot, limitPct float64) float64 {
	share := strategy.SlotShare(limitPct, e.usage(spot).MarginBalance, len(e.cfg.Slots()))
	if share <= 0 {
		return 0
	}
	most := 0.0
	for _, sl := range e.cfg.Slots() {
		var legs []marginLeg
		for _, s := range e.book {
			if s.slot == sl {
				for _, p := range s.legs() {
					legs = append(legs, e.marginLeg(p, p.Qty))
				}
			}
		}
		most = math.Max(most, scanIM(legs, spot))
	}
	return most / share
}

// topUps is the bot's upsize: each strangle below its slot's target gets a
// complement strangle on its expiry, capped by the IM headroom left.
func (e *engine) topUps(at time.Time, snap recorder.Snapshot, dec strategy.GammaDecision, st risk.Status,
	quotes map[string]recorder.Quote, spot, share, headroom float64, stopsDeferred bool) {
	sent := map[config.StrangleSlot]float64{}
	finished, short, filledBoth := 0, 0, false
	var insts []*marketdata.Instrument
	for _, s := range e.resizable(at) {
		lots, imPerLot := e.targetLots(s, spot, share)
		if lots == 0 {
			continue
		}
		targetQty := float64(lots) * e.lot
		callTotal, putTotal := e.slotTotals(s.slot)
		addQty := orders.FloorToStep(math.Min(targetQty-callTotal, targetQty-putTotal)-sent[s.slot], e.lot)
		if addQty < e.lot || headroom <= 0 {
			continue
		}
		addQty = math.Min(addQty, float64(strategy.EntryLots(headroom, imPerLot))*e.lot)
		if addQty < e.lot {
			continue
		}
		headroom -= addQty / e.lot * imPerLot
		sent[s.slot] += addQty
		if e.complementHeld(at, s, dec, st, stopsDeferred) {
			e.res.TopUpsHeld++
			e.rearm(at, false)
			continue
		}
		if insts == nil {
			insts = snap.Instruments(e.lot)
		}
		ci, pi, err := strategy.SelectStrikePair(insts, s.call.Expiry, s.slot.EntryDelta, e.cfg.DeltaSlippage, e.cfg.StrikeIVMaxRatio)
		if err != nil {
			continue
		}
		cq, pq := quotes[ci.Name], quotes[pi.Name]
		if c := e.cfg.MinPremiumBTC; c > 0 && (cq.LiveMid() < c || pq.LiveMid() < c) {
			continue
		}
		for _, q := range []recorder.Quote{cq, pq} {
			addQty = math.Min(addQty, strategy.LiquidityCapQty(q.OpenInterest, e.cfg.LiquidityCapOIPct, e.lot))
		}
		if addQty < e.lot-1e-9 {
			continue // the strike's market takes less than a lot: the slot stays short, no retry
		}
		e.nextID++
		c := &strangle{id: e.nextID, slot: s.slot}
		for _, q := range []recorder.Quote{cq, pq} {
			if f := SellFill(q, e.cfg.EntryPriceFloor); f.OK {
				e.sellInto(at, c, q, addQty, f.Price)
			} else {
				e.res.Unfilled++
			}
		}
		finished++
		if c.call != nil && c.put != nil {
			filledBoth = true
		} else {
			short++
		}
		if len(c.legs()) == 0 {
			continue
		}
		e.book = append(e.book, c)
		e.res.TopUps++
		e.res.TopUpLots += addQty / e.lot
		e.act(at, "top-up %dd ×%.1f: %s + %s", s.slot.TargetDTE, addQty, ci.Name, pi.Name)
	}
	e.rb.unfilled = strategy.NextUnfilledPasses(e.rb.unfilled, finished, short, filledBoth)
	if short > 0 {
		e.rearm(at, true)
	}
}

// complementHeld is complementHold: a top-up adds both sides or none, so
// either side held by the regime-side rule, a GEX shed, a stop in its slot
// (RepairBlockReason) or a deferred stop holds it.
func (e *engine) complementHeld(at time.Time, s *strangle, dec strategy.GammaDecision, st risk.Status, stopsDeferred bool) bool {
	if stopsDeferred {
		return true
	}
	for _, typ := range []string{"call", "put"} {
		if dec.Sheds(typ) || e.sideHold(typ, dec, st) != "" {
			return true
		}
		for _, b := range e.book {
			if b.slot != s.slot {
				continue
			}
			if stoppedAt, ok := e.stopped[key(b, typ)]; ok &&
				strategy.RepairBlockReason(stoppedAt, at, time.Duration(e.cfg.RepairCooldownHours)*time.Hour, st) != "" {
				return true
			}
		}
	}
	return false
}

// RebalanceRow is one cut rule's replay.
type RebalanceRow struct {
	Rule   string
	Result Result
}

// CostPerPP is the realised cost of the downsizes per percentage point of
// IM freed (coin; positive = a cost). 0 when nothing was freed.
func (r RebalanceRow) CostPerPP() float64 {
	if r.Result.DownsizeIMFreedPP <= 0 {
		return 0
	}
	return -r.Result.DownsizeCost / r.Result.DownsizeIMFreedPP
}

// RebalanceStudy replays snaps once per cut rule, everything else as set.
func RebalanceStudy(snaps []recorder.Snapshot, set Settings, rules []string) ([]RebalanceRow, error) {
	if set.Cfg == nil {
		return nil, fmt.Errorf("replay: no settings")
	}
	var rows []RebalanceRow
	for _, rule := range rules {
		s := set
		s.Rebalance = rule
		s.Trace = nil
		res, err := Run(snaps, s)
		if err != nil {
			return nil, fmt.Errorf("rebalance %s: %w", rule, err)
		}
		rows = append(rows, RebalanceRow{Rule: rule, Result: res})
	}
	return rows, nil
}

// RebalanceMarkdown is one coin's table of the study, marked indicative on a
// short or gapped recording (as the other studies).
func RebalanceMarkdown(coin string, rows []RebalanceRow, missing []time.Time) string {
	if len(rows) == 0 {
		return ""
	}
	r0 := rows[0].Result
	b := studyHeader(coin, "which strangle a downsize cuts", r0, missing)
	b.WriteString("| Rule | Net " + coin + " | Net (% of balance) | Downsize passes | Legs cut | Lots cut | Realised P&L of downsizes (% of balance) | IM freed (pp) | Cost per 1 pp of IM freed (% of balance) | Cuts left above the limit | Strangles uneven after a cut | Largest slot ÷ its share (max) | Cuts with a slot above 1.5× | Top-ups | Top-ups held | Entries | Worst week (% of balance) |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		res := r.Result
		ww, _ := WorstWeek(res.Days, res.Equity0)
		pct := func(v float64) float64 {
			if res.Equity0 <= 0 {
				return 0
			}
			return v / res.Equity0 * 100
		}
		fmt.Fprintf(b, "| %s | %+.4f | %+.2f | %d | %d | %.0f | %+.3f | %.1f | %.4f | %d | %d | %.2f | %d | %d | %d | %d | %+.2f |\n",
			r.Rule, res.Net, pct(res.Net), res.Downsizes, res.DownsizeLegs, res.DownsizeLots, pct(res.DownsizeCost), res.DownsizeIMFreedPP,
			pct(r.CostPerPP()), res.CutsOverLimit, res.CutsUneven, res.MaxSlotShareRatio, res.CutsSlotOverShare,
			res.TopUps, res.TopUpsHeld, res.Entries, ww)
	}
	return b.String()
}

// DownsizeMatch is one UTC day and side (call or put) of the journal's
// downsizes next to the replay's. By side, not by strike: once the replay's
// own rolls and repairs pick other strikes than the bot did, the same cut
// lands on another instrument.
type DownsizeMatch struct {
	Day             time.Time
	Side            string  // call | put
	Journal, Replay float64 // qty bought back that day
	Match           bool    // within one lot step
}

// CompareDownsizes sets the journal's rebalance downsizes in [from, to]
// beside the replayed ones, per UTC day and side, oldest first.
func CompareDownsizes(trades []orders.Trade, replayed []Downsize, from, to time.Time, lot float64) []DownsizeMatch {
	type k struct {
		day  time.Time
		side string
	}
	rows := map[k]*DownsizeMatch{}
	get := func(at time.Time, inst string) *DownsizeMatch {
		side := "put"
		if strings.HasSuffix(inst, "-C") {
			side = "call"
		}
		id := k{at.UTC().Truncate(24 * time.Hour), side}
		if rows[id] == nil {
			rows[id] = &DownsizeMatch{Day: id.day, Side: side}
		}
		return rows[id]
	}
	for _, t := range trades {
		if t.Kind == "close" && t.Reason == orders.TriggerRebalanceDownsize && !t.At.Before(from) && (to.IsZero() || !t.At.After(to)) {
			get(t.At, t.Instrument).Journal += t.Qty
		}
	}
	for _, d := range replayed {
		get(d.At, d.Instrument).Replay += d.Qty
	}
	out := make([]DownsizeMatch, 0, len(rows))
	for _, r := range rows {
		r.Match = math.Abs(r.Journal-r.Replay) <= lot+1e-9
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Day.Equal(out[j].Day) {
			return out[i].Day.Before(out[j].Day)
		}
		return out[i].Side < out[j].Side
	})
	return out
}

// DownsizeMatchMarkdown is the journal check: the bot's downsizes beside the
// replay's, with how many agree within one lot step.
func DownsizeMatchMarkdown(coin string, rows []DownsizeMatch) string {
	if len(rows) == 0 {
		return "**Journal check:** no downsize in the journal or the replay over the window.\n"
	}
	var b strings.Builder
	matched := 0
	for _, r := range rows {
		if r.Match {
			matched++
		}
	}
	fmt.Fprintf(&b, "**Journal check (%s):** %d of %d day × side rows within one lot step of the bot's downsizes.\n\n", coin, matched, len(rows))
	b.WriteString("| Day (UTC) | Side | Journal (bought back) | Replay | Within a lot |\n|---|---|---|---|---|\n")
	for _, r := range rows {
		ok := "no"
		if r.Match {
			ok = "yes"
		}
		fmt.Fprintf(&b, "| %s | %s | %g | %g | %s |\n", r.Day.Format(time.DateOnly), r.Side,
			orders.FloorToStep(r.Journal+1e-9, 1e-4), orders.FloorToStep(r.Replay+1e-9, 1e-4), ok)
	}
	return b.String()
}
