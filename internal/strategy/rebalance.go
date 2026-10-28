package strategy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/risk"
)

// rebalancePositions resizes the book toward the confirmed IM limit. It runs
// at startup and whenever a confirmed DVOL band or gamma regime change moves
// the limit (never while a change is still unconfirmed); at startup only when
// IM is outside the band around the limit (RebalanceInBand, #123). Each complete
// strangle's share of the limit is limit × margin balance ÷ slots; its IM per
// lot is what Deribit's simulator says buying one lot back would free.
//
//   - IM above the limit: buy back whole lots above each strangle's share, at
//     market — reducing exposure is a risk action, so speed beats price. At
//     least one lot of each strangle stays (only the MM limit closes fully).
//   - IM below the limit: open a complement strangle for the missing size
//     through the normal entry path, capped by the remaining headroom. If it
//     times out short of its size, the rebalance runs again after
//     rebalance_retry_minutes (rearmRebalance).
//
// A slot can hold several strangles (a filled complement is one), so sizes are
// compared per slot: each strangle is sized against its slot's total.
//
// Strangles inside the rollout window are skipped (they roll anyway), and so
// are one-legged strangles (repair completes them first).
//
// With dryRun it changes nothing — no orders, no journal, no re-arm — and
// only reports what it would do: Deribit's margin simulations are read-only,
// so the sizes are the same a real pass would compute (POST /api/rebalance
// ?dry_run=true).
//
// The report's Done is true when the book is done, false when something
// failed and the rebalance should run again next cycle.
func (s *Strategy) rebalancePositions(ctx context.Context, m marginState, dryRun bool) RebalanceReport {
	strangles := s.state.AllStrangles()
	limit := m.status.LimitIMPct
	share := SlotShare(limit, m.usage.MarginBalance, len(s.cfg.Slots()))
	over := m.usage.IMPct() > limit
	headroom := m.usage.Headroom(limit)
	rep := RebalanceReport{DryRun: dryRun, Done: true, LimitIMPct: limit, IMPct: m.usage.IMPct(),
		SharePerSlot: share, MarginBalance: m.usage.MarginBalance, Unit: m.usage.Unit, Actions: []RebalanceAction{}}

	if !dryRun {
		s.startRebalancePass()
		defer s.endRebalancePass()
		s.logRisk(orders.RiskRebalance, fmt.Sprintf("IM %.1f%% vs limit %.0f%% (%s)", m.usage.IMPct(), limit, m.status.Reason),
			m.status, m.usage, "")
	}
	slog.Info("rebalance: resizing toward the IM limit", "dry_run", dryRun,
		"limit_im_pct", limit, "im_pct", fmt.Sprintf("%.2f", m.usage.IMPct()), "unit", m.usage.Unit,
		"share_per_slot", fmt.Sprintf("%.6f", share), "strangles", len(strangles))

	sent := make(map[slotKey]float64) // complement qty sent this pass, per slot
	cut := make(map[string]float64)   // dry run only: qty a real pass would have bought back, per slot+type
	for _, st := range strangles {
		// A closed leg stays linked to its strangle with its last size, so
		// "has both legs" is read from the book (liveLegs), never from the
		// links: 2026-10-11 BTC topped up a strangle whose put had been closed
		// by hand two days earlier, as if it still held 1.2 puts — re-selling
		// the closed side past every repair hold.
		if _, missing := s.liveLegs(st); missing != "" {
			rep.add(st, ActionSkipped, "", 0, 0, "one leg missing: repair completes it first")
			continue
		}
		if dryRun {
			cp := *st // the links below are refreshed in place: never touch the book on a dry run
			st = &cp
		}
		st.CallLeg, st.PutLeg = s.livePosition(st.CallLeg), s.livePosition(st.PutLeg)
		if st.CallLeg == nil || st.PutLeg == nil {
			continue
		}
		if st.CallLeg.DTE() <= s.cfg.RolloutDTE {
			rep.add(st, ActionSkipped, "", 0, 0, "inside the rollout window: it rolls anyway")
			continue
		}
		if s.churnPaused(makeSlotKey(st.TargetDTE, st.EntryDelta), time.Now()) {
			rep.Done = false // retried once the pause ends
			rep.add(st, ActionHeld, "", 0, 0, "slot paused by the churn breaker")
			continue
		}
		callInst, callOk := s.md.GetInstrument(st.CallLeg.Instrument)
		putInst, putOk := s.md.GetInstrument(st.PutLeg.Instrument)
		if !callOk || !putOk {
			slog.Warn("rebalance: instrument not in market data, skipping", "strangle_id", st.ID)
			rep.add(st, ActionSkipped, "", 0, 0, "instrument not in market data")
			continue
		}
		lot := math.Max(callInst.MinTradeAmount, putInst.MinTradeAmount)
		if lot <= 0 {
			lot = s.cfg.MinTradeAmount
		}

		// Buying one lot back (positive size) frees this strangle's IM per
		// lot — measured against the book now, which includes complements
		// sent earlier in this pass.
		base, err := s.simulate(ctx, map[string]float64{}, m.usage.Unit)
		if err != nil {
			slog.Warn("rebalance: simulation failed, will retry", "strangle_id", st.ID, "err", err)
			rep.Done = false
			rep.add(st, ActionSkipped, "", 0, 0, "margin simulation failed: "+err.Error())
			continue
		}
		freed, err := s.simulate(ctx, map[string]float64{callInst.Name: lot, putInst.Name: lot}, m.usage.Unit)
		if err != nil {
			slog.Warn("rebalance: simulation failed, will retry", "strangle_id", st.ID, "err", err)
			rep.Done = false
			rep.add(st, ActionSkipped, "", 0, 0, "margin simulation failed: "+err.Error())
			continue
		}
		imPerLot := base.IM - freed.IM
		targetLots := TargetLots(share, imPerLot)
		if targetLots == 0 {
			slog.Info("rebalance: strangle adds no IM (portfolio netting), size kept", "strangle_id", st.ID)
			rep.add(st, ActionSkipped, "", 0, 0, "adds no IM (portfolio netting): size kept")
			continue
		}
		if targetLots, err = s.capToNormalSize(ctx, callInst.Name, putInst.Name, lot, targetLots, share, m.usage.Unit); err != nil {
			slog.Warn("rebalance: standalone simulation failed, will retry", "strangle_id", st.ID, "err", err)
			rep.Done = false
			rep.add(st, ActionSkipped, "", 0, 0, "standalone margin simulation failed: "+err.Error())
			continue
		}
		targetQty := float64(targetLots) * lot
		key := makeSlotKey(st.TargetDTE, st.EntryDelta)
		callTotal, putTotal := s.slotLegTotals(key)
		callTotal += sent[key] - cut[cutKey(key, "call")]
		putTotal += sent[key] - cut[cutKey(key, "put")]

		addQty := orders.FloorToStep(math.Min(targetQty-callTotal, targetQty-putTotal), lot)
		if addQty >= lot {
			if over || headroom <= 0 {
				rep.add(st, ActionKept, "", 0, targetQty, "below the slot target, but no IM headroom left")
				continue
			}
			addQty = math.Min(addQty, float64(EntryLots(headroom, imPerLot))*lot)
			if addQty >= lot {
				headroom -= addQty / lot * imPerLot
				sent[key] += addQty
				rep.Actions = append(rep.Actions, s.openComplementStrangle(ctx, st, addQty, targetQty, m.status, dryRun))
			}
			continue // never upsize and downsize in the same pass
		}
		if !over {
			continue // under the limit overall: no reason to cut this strangle
		}
		for _, leg := range []struct {
			pos   *orders.Position
			inst  *marketdata.Instrument
			total float64
		}{{st.CallLeg, callInst, callTotal}, {st.PutLeg, putInst, putTotal}} {
			// Keep what the slot's other strangles leave of the target, and
			// at least one lot (only the MM limit closes fully).
			keep := math.Max(targetQty-(leg.total-leg.pos.Qty), lot)
			excess := orders.FloorToStep(leg.pos.Qty-keep, lot)
			if excess < lot {
				continue
			}
			act := RebalanceAction{StrangleID: st.ID, Slot: slotLabel(st), Action: ActionDownsize,
				Instrument: leg.inst.Name, Qty: excess, TargetQty: keep,
				Detail: fmt.Sprintf("IM above the limit: buy back %g at market (%g → %g)", excess, leg.pos.Qty, keep)}
			if dryRun {
				cut[cutKey(key, leg.pos.OptionType)] += excess
			} else if !s.downsizeLeg(ctx, leg.pos, leg.inst, keep, lot) {
				rep.Done = false
				act.Error = "buy-back failed or filled in part: retried next cycle"
			}
			rep.Actions = append(rep.Actions, act)
		}
	}
	return rep
}

// Rebalance action kinds, as reported by RebalanceReport.
const (
	ActionTopUp    = "top_up"   // a complement strangle sold to reach the slot target
	ActionDownsize = "downsize" // a leg bought back to the slot target
	ActionHeld     = "held"     // a top-up or slot waits (regime side, stop hold, churn pause)
	ActionKept     = "kept"     // below target, but the IM limit leaves no headroom
	ActionSkipped  = "skipped"  // not resized (one-legged, rolling, no data, netting)
)

// RebalanceAction is one decision of a rebalance pass.
type RebalanceAction struct {
	StrangleID string  `json:"strangle_id"`
	Slot       string  `json:"slot"`
	Action     string  `json:"action"`
	Instrument string  `json:"instrument,omitempty"` // downsize: the leg
	Call       string  `json:"call,omitempty"`       // top-up: the complement's strikes
	Put        string  `json:"put,omitempty"`
	Qty        float64 `json:"qty"`        // top-up: qty sold per leg; downsize: qty bought back
	TargetQty  float64 `json:"target_qty"` // the slot target (top-up) or the leg size kept (downsize)
	Detail     string  `json:"detail,omitempty"`
	Error      string  `json:"error,omitempty"` // real pass only: the order failed
}

// RebalanceReport is what a rebalance pass did, or on a dry run would do.
type RebalanceReport struct {
	DryRun        bool              `json:"dry_run"`
	Done          bool              `json:"done"` // false: part of it runs again (failure, churn pause, short fill)
	LimitIMPct    float64           `json:"limit_im_pct"`
	IMPct         float64           `json:"im_pct"`
	SharePerSlot  float64           `json:"share_per_slot"`
	MarginBalance float64           `json:"margin_balance"`
	Unit          string            `json:"unit"`
	Actions       []RebalanceAction `json:"actions"`
}

func (r *RebalanceReport) add(st *orders.Strangle, action, instrument string, qty, target float64, detail string) {
	r.Actions = append(r.Actions, RebalanceAction{StrangleID: st.ID, Slot: slotLabel(st), Action: action,
		Instrument: instrument, Qty: qty, TargetQty: target, Detail: detail})
}

func slotLabel(st *orders.Strangle) string {
	return fmt.Sprintf("%dd/%.2fΔ", st.TargetDTE, st.EntryDelta)
}

func cutKey(key slotKey, optType string) string { return fmt.Sprintf("%v/%s", key, optType) }

// complementHold says why a top-up of st's slot must wait, with the skip code
// to journal, or "" when it may go.
//
// A top-up adds both sides or none: while the confirmed negative regime and
// the trend block one side, a one-sided top-up would leave the slot uneven and
// balanceStrangles would buy the excess straight back.
func (s *Strategy) complementHold(st *orders.Strangle, trendDir int, rs risk.Status) (why, skip string) {
	for _, typ := range []string{"call", "put"} {
		if why := RegimeSideBlockReason(typ, trendDir, rs); why != "" {
			return why, SkipRegimeSide
		}
		// A leg of this slot stopped out (or closed by hand) waits for a calm
		// market before repair re-sells it; a top-up re-selling that side in
		// the slot would undo the hold.
		if why := s.slotStopHold(makeSlotKey(st.TargetDTE, st.EntryDelta), typ, rs); why != "" {
			return why, SkipRepairHeld
		}
	}
	return "", ""
}

// openComplementStrangle opens addQty more of st's slot through normal entry.
// A held top-up is retried (rearmRebalance), so the slot fills up once the
// block lifts. With dryRun it only reports what it would send.
func (s *Strategy) openComplementStrangle(ctx context.Context, st *orders.Strangle, addQty, targetQty float64, rs risk.Status, dryRun bool) RebalanceAction {
	act := RebalanceAction{StrangleID: st.ID, Slot: slotLabel(st), Action: ActionTopUp, Qty: addQty, TargetQty: targetQty}
	gammaDec := s.gamma.Evaluate()
	if why, skip := s.complementHold(st, gammaDec.TrendDir, rs); why != "" {
		act.Action, act.Detail = ActionHeld, "top-up held: "+why
		if !dryRun {
			s.noteSkip(st.TargetDTE, st.EntryDelta, skip, "top-up held: "+why)
			s.rearmRebalance(fmt.Sprintf("complement for %s held: %s", st.ID, why), false)
		}
		return act
	}
	instruments := s.md.AllInstruments()
	// The complement adds to this strangle, so it uses this strangle's
	// expiry — never a fresh pick, which could land on another slot's date.
	expiry := st.CallLeg.Expiry
	call, put, err := SelectStrikePair(instruments, expiry, st.EntryDelta, s.cfg.DeltaSlippage)
	if err != nil {
		slog.Warn("rebalance: strike selection failed for complement strangle, skipping upsize",
			"strangle_id", st.ID, "err", err)
		act.Action, act.Detail = ActionSkipped, "strike selection failed: "+err.Error()
		return act
	}
	act.Call, act.Put = call.Name, put.Name
	act.Detail = fmt.Sprintf("sell %g of each leg (limit) toward the slot target %g", addQty, targetQty)
	if dryRun {
		// Report the size the liquidity cap would let out (liquidity.go).
		lot := math.Max(call.MinTradeAmount, put.MinTradeAmount)
		if lot <= 0 {
			lot = s.cfg.MinTradeAmount
		}
		if capped, legs, known := s.liquidityLimit(call, put, true, true, addQty, lot); !known || capped < lot {
			act.Action, act.Qty, act.Detail = ActionKept, 0, "below the slot target, but the strike's open interest takes less than a lot"
		} else if capped < addQty-qtyEpsilon {
			act.Qty = capped
			act.Detail = fmt.Sprintf("sell %g of each leg (limit) toward the slot target %g, capped from %g by open interest (%s)",
				capped, targetQty, addQty, strings.Join(legs, "; "))
		}
		return act
	}
	slog.Info("rebalance: opening complement strangle for missing balance",
		"original_strangle_id", st.ID,
		"call_current_qty", fmt.Sprintf("%.4f", st.CallLeg.Qty),
		"put_current_qty", fmt.Sprintf("%.4f", st.PutLeg.Qty),
		"target_qty", fmt.Sprintf("%.4f", targetQty),
		"complement_qty", fmt.Sprintf("%.4f", addQty),
		"call_instrument", call.Name,
		"put_instrument", put.Name,
	)
	sent, err := s.openStrangle(ctx, call, put, st.TargetDTE, st.EntryDelta, addQty, gammaDec, rs, true)
	if err != nil {
		if errors.Is(err, errLiquidityCap) {
			// The strike's market cannot take a lot: the slot stays short (#103),
			// no retry — the next limit change or restart re-measures it.
			act.Action, act.Qty, act.Detail = ActionKept, 0, "below the slot target, but the strike's open interest takes less than a lot"
			return act
		}
		slog.Warn("rebalance: complement strangle open failed",
			"original_strangle_id", st.ID, "err", err)
		s.rearmRebalance(fmt.Sprintf("complement for %s not opened: %v", st.ID, err), false)
		act.Error = err.Error()
		return act
	}
	if sent < addQty-qtyEpsilon {
		// Report what went out: the exchange minimum or the liquidity cap cut it.
		act.Qty = sent
		act.Detail = fmt.Sprintf("sell %g of each leg (limit) toward the slot target %g (cut from %g)", sent, targetQty, addQty)
	}
	return act
}

// slotLegTotals sums the open call and put quantities of every strangle in a
// slot, read fresh so closes made earlier in this pass are counted (a closed
// leg's link keeps its last size: only legs still in the book count).
func (s *Strategy) slotLegTotals(key slotKey) (call, put float64) {
	for _, st := range s.state.AllStrangles() {
		if makeSlotKey(st.TargetDTE, st.EntryDelta) != key {
			continue
		}
		if c := s.livePosition(st.CallLeg); c != nil {
			call += c.Qty
		}
		if p := s.livePosition(st.PutLeg); p != nil {
			put += p.Qty
		}
	}
	return call, put
}

// rearmRebalance makes the rebalance run again once rebalance_retry_minutes
// have passed. A complement that did not fill (or filled in part) leaves the
// book under the IM limit, and nothing else would trigger a new pass until
// the next confirmed limit change or a restart. The next pass re-measures
// the book, so whatever did fill is accounted for.
//
// With backoff the wait grows (RebalanceRetryWait) once rebalance_backoff_after
// passes in a row filled no complement on both legs (#98): on a book where nobody trades at mid the
// same order would otherwise be re-sent every 15 minutes for nothing
// (2026-10-14 BTC: 12 orders in 1.5 h, zero fills). A held or failed top-up
// re-arms without it: the hold says nothing about fills (#93). Exits, stops
// and the MM cut never wait on it. Nothing re-arms while the kill switch
// flattens the book.
func (s *Strategy) rearmRebalance(reason string, backoff bool) {
	if s.flattening {
		return
	}
	base := time.Duration(s.cfg.RebalanceRetryMinutes) * time.Minute
	wait := base
	if backoff {
		wait = RebalanceRetryWait(base, time.Duration(s.cfg.RebalanceRetryMaxMinutes)*time.Minute,
			s.unfilledPasses, s.cfg.RebalanceBackoffAfter)
	}
	s.appliedLimit = math.NaN()
	s.rebalanceRetryAt = time.Now().Add(wait)
	detail := fmt.Sprintf("%s · retry in %s", reason, wait)
	if wait > base {
		detail = fmt.Sprintf("%s · %d passes in a row unfilled: retry backed off to %s", reason, s.unfilledPasses, wait)
	}
	slog.Warn("rebalance: book still short of the IM limit, will retry",
		"reason", reason, "retry_in", wait.String(), "unfilled_passes_in_a_row", s.unfilledPasses)
	var st risk.Status
	if s.lastRisk != nil {
		st = *s.lastRisk
	}
	s.logRisk(orders.RiskRebalanceRetry, detail, st, s.lastUsage(), "")
}

// rebalancePass collects what the complements of one real rebalance pass
// did. Only one pass has complements working at a time (a new pass waits for
// them: RebalanceBlockReason), so one value is enough.
type rebalancePass struct {
	sending  bool     // the pass is still sending complements
	finished int      // complements finalized
	timedOut int      // of which ended at the fill timeout
	filled   bool     // a complement filled on every leg it sent (#98)
	oneSided int      // complements that filled on one side only
	short    []string // complements that filled short of their size, with the legs that filled
}

// startRebalancePass opens the outcome record of a real pass.
func (s *Strategy) startRebalancePass() {
	s.pass = rebalancePass{sending: true}
}

// endRebalancePass marks the pass as done sending; its outcome is settled
// now if every complement already finished (filled or rejected at once).
func (s *Strategy) endRebalancePass() {
	s.pass.sending = false
	s.settleRebalancePass()
}

// noteComplementOutcome records one finalized complement in its pass.
func (s *Strategy) noteComplementOutcome(ps *pendingStrangle) {
	s.pass.finished++
	if ps.timedOut {
		s.pass.timedOut++
	}
	legs := ps.filledLegs()
	both := ps.filledEveryLeg()
	s.pass.filled = s.pass.filled || both
	if !both && legs != "none" {
		s.pass.oneSided++
	}
	if ps.underfilled() {
		s.pass.short = append(s.pass.short, fmt.Sprintf("%s filled: %s", ps.id, legs))
	}
	s.settleRebalancePass()
}

// settleRebalancePass applies the outcome of a pass once all its complements
// are finalized (NextUnfilledPasses). One re-arm per pass that fell short, so
// the wait matches the count.
func (s *Strategy) settleRebalancePass() {
	p := s.pass
	if p.sending || p.finished == 0 || s.complementPending() {
		return
	}
	s.pass = rebalancePass{}
	s.unfilledPasses = NextUnfilledPasses(s.unfilledPasses, p.finished, p.timedOut, p.filled)
	if len(p.short) > 0 {
		reason := fmt.Sprintf("%d complement(s) filled short of their size (%s)",
			len(p.short), strings.Join(p.short, ", "))
		if p.oneSided > 0 && !p.filled {
			reason += fmt.Sprintf(" · %d filled on one side only: not a filled pass", p.oneSided)
		}
		s.rearmRebalance(reason, true)
	}
}

// NextUnfilledPasses is the count of rebalance passes in a row that did not
// fill, after a pass of `finished` complements of which `timedOut` ran to
// their fill timeout (#93). `filled` means a complement of the pass filled on
// both legs (#98: a put-only fill ends as a repair of the call at the bid,
// not a trade at our price): that resets it; a pass whose every complement
// timed out without filling both legs adds one; a pass with a complement the
// bot cancelled itself (freeze, GEX shed, kill switch) leaves it alone — the
// market never decided. Pure.
func NextUnfilledPasses(count, finished, timedOut int, filled bool) int {
	switch {
	case filled:
		return 0
	case finished > 0 && timedOut == finished:
		return count + 1
	}
	return count
}

// resetRebalanceBackoff clears the back-off after a limit change: the new
// limit gets a pass at once, and its retries start again from the base wait.
func (s *Strategy) resetRebalanceBackoff() {
	s.unfilledPasses = 0
	s.rebalanceRetryAt = time.Time{}
}

// RebalanceRetryWait is how long the rebalance waits before retrying a
// complement that fell short. Below `after` unfilled passes in a row it is
// base; from there it doubles with each one (base 15 min, after 3: 15, 15,
// 30, 60 …), capped at max (never below base). after ≤ 0 turns the back-off
// off. Pure.
func RebalanceRetryWait(base, max time.Duration, unfilled, after int) time.Duration {
	if after <= 0 || unfilled < after {
		return base
	}
	if max < base {
		max = base
	}
	wait := base
	for i := after; i <= unfilled && wait < max; i++ {
		wait *= 2
	}
	return min(wait, max)
}

// downsizeLeg buys back whole lots above targetQty. The strike is kept.
// It returns false when the close failed or only partly filled.
func (s *Strategy) downsizeLeg(ctx context.Context, pos *orders.Position, inst *marketdata.Instrument, targetQty, lot float64) bool {
	excess := orders.FloorToStep(pos.Qty-targetQty, lot)
	if excess < lot {
		return true
	}
	oldQty := pos.Qty
	detail := fmt.Sprintf("downsize: IM above the confirmed limit — %s cut from %g to the slot target %g", pos.Instrument, pos.Qty, targetQty)
	filled, err := s.buyToClose(ctx, pos, excess, orders.TriggerRebalanceDownsize, 0, detail)
	if err != nil {
		slog.Warn("rebalance: downsize failed",
			"instrument", inst.Name, "excess_qty", excess, "err", err)
		return false
	}
	slog.Info("rebalance: downsized position",
		"instrument", inst.Name,
		"old_qty", fmt.Sprintf("%.4f", oldQty),
		"new_qty", fmt.Sprintf("%.4f", oldQty-filled),
		"closed_qty", fmt.Sprintf("%.4f", filled),
		"requested_qty", fmt.Sprintf("%.4f", excess),
	)
	return filled >= excess-qtyEpsilon
}
