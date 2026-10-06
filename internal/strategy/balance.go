package strategy

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"optionsbot/internal/orders"
)

// balanceStrangles buys back the excess of the larger leg of any strangle
// whose call and put sizes differ by at least one lot. Equal sizes keep a
// strangle roughly delta-neutral; an uneven one carries an unintended
// directional bet. Uneven strangles come from partial entry fills, or — on
// 2026-10-04 — from an order that filled twice (2.4 calls against 1.2 puts).
//
// Buying back reduces risk, so it runs while entries are frozen too. It uses
// an IOC limit at the ask, like rolls: no resting order, and no fill means
// try again next cycle. One-legged strangles are repair's job; strangles in
// the rollout window are about to roll anyway.
func (s *Strategy) balanceStrangles(ctx context.Context) {
	for _, st := range s.state.AllStrangles() {
		call, put := s.livePosition(st.CallLeg), s.livePosition(st.PutLeg)
		if call == nil || put == nil || s.repairPending(st.ID) ||
			s.churnPaused(makeSlotKey(st.TargetDTE, st.EntryDelta), time.Now()) {
			continue
		}
		if call.DTE() <= s.cfg.RolloutDTE {
			continue
		}
		lot := s.cfg.MinTradeAmount
		for _, p := range []*orders.Position{call, put} {
			if inst, ok := s.md.GetInstrument(p.Instrument); ok && inst.MinTradeAmount > lot {
				lot = inst.MinTradeAmount
			}
		}
		larger, smaller := call, put
		if put.Qty > call.Qty {
			larger, smaller = put, call
		}
		excess := orders.FloorToStep(larger.Qty-smaller.Qty, lot)
		if excess < lot-qtyEpsilon {
			continue
		}
		inst, ok := s.md.GetInstrument(larger.Instrument)
		if !ok || inst.Ask <= 0 {
			slog.Warn("balance legs: no ask quote, retrying next cycle",
				"strangle_id", st.ID, "instrument", larger.Instrument, "excess", excess)
			continue
		}
		slog.Warn("balance legs: strangle is uneven, buying back the excess of the larger leg",
			"strangle_id", st.ID, "larger", larger.Instrument, "larger_qty", larger.Qty,
			"smaller", smaller.Instrument, "smaller_qty", smaller.Qty, "excess", excess,
			"ask", fmt.Sprintf("%.6f", inst.Ask))
		filled, err := s.buyToClose(ctx, larger, math.Min(excess, larger.Qty), orders.TriggerRebalanceLegs, inst.Ask)
		if err != nil {
			slog.Warn("balance legs: buy back failed, retrying next cycle", "instrument", larger.Instrument, "err", err)
			continue
		}
		slog.Info("balance legs: done", "strangle_id", st.ID, "instrument", larger.Instrument, "bought_back", filled)
	}
}
