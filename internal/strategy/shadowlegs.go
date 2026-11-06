package strategy

import (
	"cmp"
	"fmt"
	"log/slog"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/hedge"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/shadow"
)

// Shadow legs (#676): every missing leg the rules hold back is recorded as
// the leg repair would have sold, and followed at its mark until the rules
// release it or an exit rule would close it (data/shadow_legs.jsonl, read by
// the full-strangle study, #677). A one-sided entry is covered too: once it
// fills, its strangle has a missing leg. Records only: this file sends no
// order and nothing here reaches a trading decision.

// Holds repair applies after its gate (repairGate returns "" for them).
const (
	shadowRuleNoStrike     = SkipNoStrike
	shadowRulePremiumFloor = "premium_floor"
)

// shadowHold is a missing leg something holds this cycle.
type shadowHold struct {
	view         HeldLegView
	rule, reason string
	qty          float64
	inst         *marketdata.Instrument // the leg repair would pick; nil: no strike
}

// shadowHolds sorts this cycle's missing legs (heldLegViews) into those a rule
// holds, by shadow.LegKey. A leg with a repair working or free to sell is not
// held, and neither is one the roll is about to take with its open leg:
// repair would not sell it either, and a shadow leg would roll at once.
func (s *Strategy) shadowHolds(held []HeldLegView) map[string]*shadowHold {
	out := make(map[string]*shadowHold, len(held))
	instruments := s.md.AllInstruments()
	for _, hv := range held {
		if hv.Rule == HoldRepairWorking || hv.Rule == HoldRolloutWindow {
			continue
		}
		key := shadow.LegKey(hv.OpenLeg, hv.Missing)
		if h, ok := out[key]; ok {
			h.qty += hv.OpenQty // two strangles sharing a strike: one held leg
			continue
		}
		h := &shadowHold{view: hv, rule: hv.Rule, reason: hv.Reason, qty: hv.OpenQty}
		delta := cmp.Or(hv.Slot.Delta, s.cfg.EntryDelta)
		inst, err := SelectStrike(instruments, hv.Expiry, hv.Missing, delta, s.cfg.DeltaSlippage, s.cfg.StrikeIVMaxRatio)
		if err == nil {
			h.inst = inst
		}
		if h.rule == "" { // the gate let it through: repair's own two checks
			if err != nil {
				h.rule, h.reason = shadowRuleNoStrike, err.Error()
			} else if floorErr := s.checkPremiumFloor(inst, s.cfg.RepairPriceFloor); floorErr != nil {
				h.rule, h.reason = shadowRulePremiumFloor, floorErr.Error()
			} else {
				continue // nothing holds it: repair is free to sell
			}
		}
		out[key] = h
	}
	return out
}

// stepShadowLegs runs once a cycle, after repair: it follows the open shadow
// legs, ends those an exit rule closes or the rules release, and opens one
// for each newly held leg.
func (s *Strategy) stepShadowLegs(held []HeldLegView, now time.Time) {
	if s.shadow == nil {
		return
	}
	holds := s.shadowHolds(held)
	spot := s.md.UnderlyingPrice()
	for _, leg := range s.shadow.Legs() {
		s.followShadowLeg(leg, holds[leg.Key], spot, now)
	}
	for _, key := range s.shadow.Held() {
		if holds[key] == nil {
			s.shadow.Forget(key)
		}
	}
	for key, h := range holds {
		if !s.shadow.Following(key) {
			s.startShadowLeg(key, h, spot, now)
		}
	}
}

// followShadowLeg marks one open leg at its quote, then applies the exit
// rules in their real order; a leg they leave open and no rule holds any more
// is released.
func (s *Strategy) followShadowLeg(leg *shadow.Leg, h *shadowHold, spot float64, now time.Time) {
	inst, known := s.md.GetInstrument(leg.Instrument)
	live := known && inst.HasQuote()
	if live {
		leg.Follow(inst.Bid, inst.Ask, inst.Mid, toOrderGreeks(inst), spot, config.FixedFuturesHedgeFeeRate)
	}
	leg.MarkLive = live
	decision := s.evaluateLeg(leg.Position())
	switch decision.Action {
	case ActionStopLoss:
		s.endShadowLeg(leg, shadow.ReasonStopLoss, decision.Detail, s.shadowAsk(leg), spot, now)
		return
	case ActionRollNextMonth:
		s.endShadowLeg(leg, shadow.ReasonDTERoll, decision.Detail, leg.Mark, spot, now)
		return
	case ActionDeltaExit:
		s.endShadowLeg(leg, shadow.ReasonDelta, decision.Detail, s.shadowAsk(leg), spot, now)
		return
	}
	if h == nil {
		if s.heldQty(leg.OpenLeg) > qtyEpsilon {
			s.endShadowLeg(leg, shadow.ReasonReleased, "the rules let the leg be sold", leg.Mark, spot, now)
		} else {
			s.endShadowLeg(leg, shadow.ReasonStrangleClosed, "the open leg closed: nothing is held", leg.Mark, spot, now)
		}
		return
	}
	changed := leg.HeldBy != h.rule
	leg.HeldBy = h.rule
	s.shadow.Save(leg, now, changed)
}

// shadowAsk is what a stop or a delta exit would pay: the ask, else the mark.
func (s *Strategy) shadowAsk(leg *shadow.Leg) float64 {
	if leg.MarkLive && leg.Ask > 0 {
		return leg.Ask
	}
	return leg.Mark
}

func (s *Strategy) endShadowLeg(leg *shadow.Leg, reason, detail string, price, spot float64, now time.Time) {
	leg.Finish(reason, detail, price, spot, config.FixedFuturesHedgeFeeRate, now)
	r := s.shadowRecord(leg, spot, now)
	s.shadow.End(r)
	slog.Info("shadow leg closed", "id", leg.ID, "instrument", leg.Instrument, "reason", reason,
		"premium", leg.Premium, "exit", price, "pnl_coin", leg.PnLCoin, "net_pnl_coin", leg.NetPnLCoin, "days_held", leg.DaysHeld)
}

// startShadowLeg opens the leg repair would have sold, at the bid (the
// modelled fill). A hold without a strike has nothing to follow: it is
// written once as no_leg, so the coverage metric counts it.
func (s *Strategy) startShadowLeg(key string, h *shadowHold, spot float64, now time.Time) {
	hv := h.view
	leg := shadow.Leg{
		Key: key, Rule: h.rule, Reason: h.reason, HeldBy: h.rule, StrangleID: hv.StrangleID, Slot: hv.Slot,
		OpenLeg: hv.OpenLeg, OptionType: hv.Missing, Expiry: hv.Expiry, Qty: h.qty, OpenedAt: now,
	}
	if h.inst == nil {
		s.shadow.NoteNoLeg(leg, now)
		return
	}
	leg.ID = fmt.Sprintf("%s@%d", key, now.Unix())
	leg.Instrument, leg.Strike, leg.Premium = h.inst.Name, h.inst.Strike, h.inst.Bid
	leg.Follow(h.inst.Bid, h.inst.Ask, h.inst.Mid, toOrderGreeks(h.inst), spot, config.FixedFuturesHedgeFeeRate)
	s.shadow.Start(s.shadowRecord(&leg, spot, now))
	slog.Info("shadow leg opened", "id", leg.ID, "instrument", leg.Instrument, "qty", leg.Qty,
		"rule", h.rule, "reason", h.reason, "strangle_id", hv.StrangleID, "premium", leg.Premium)
}

// shadowRecord is a line with the market and the book's net coin delta. The
// perpetual is the last one the hedge read (none while the mode is off): this
// adds no exchange call.
func (s *Strategy) shadowRecord(leg *shadow.Leg, spot float64, now time.Time) shadow.Record {
	ectx := s.instrumentContext(leg.Instrument, &leg.Slot)
	options := s.state.TotalCoinDelta()
	perp := hedge.PerpDelta(s.fh.perp.Size, perpMark(s.fh.perp, spot))
	return shadow.Record{At: now, Leg: *leg, Market: &ectx.Market, Portfolio: &ectx.Portfolio,
		Book: &shadow.BookDelta{Options: options, Perp: perp, Net: options + perp}}
}

// shadowInstruments are the instruments the open shadow legs follow
// (trackPositions keeps their tickers).
func (s *Strategy) shadowInstruments() []string {
	if s.shadow == nil {
		return nil
	}
	var out []string
	for _, leg := range s.shadow.Legs() {
		out = append(out, leg.Instrument)
	}
	return out
}
