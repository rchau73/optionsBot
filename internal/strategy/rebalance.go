package strategy

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

// rebalancePositions resizes the book toward the confirmed IM limit. It runs
// at startup and whenever a confirmed DVOL band or gamma regime change moves
// the limit (never while a change is still unconfirmed). Each complete
// strangle's share of the limit is limit × margin balance ÷ slots; its IM per
// lot is what Deribit's simulator says buying one lot back would free.
//
//   - IM above the limit: buy back whole lots above each strangle's share, at
//     market — reducing exposure is a risk action, so speed beats price. At
//     least one lot of each strangle stays (only the MM limit closes fully).
//   - IM below the limit: open a complement strangle for the missing size
//     through the normal entry path, capped by the remaining headroom.
//
// Strangles inside the rollout window are skipped (they roll anyway), and so
// are one-legged strangles (repair completes them first).
//
// It returns true when the book is done, false when something failed and the
// rebalance should run again next cycle.
func (s *Strategy) rebalancePositions(ctx context.Context, m marginState) bool {
	strangles := s.state.AllStrangles()
	limit := m.status.LimitIMPct
	share := SlotShare(limit, m.usage.MarginBalance, len(s.cfg.Slots()))
	over := m.usage.IMPct() > limit
	headroom := m.usage.Headroom(limit)

	s.logRisk(orders.RiskRebalance, fmt.Sprintf("IM %.1f%% vs limit %.0f%% (%s)", m.usage.IMPct(), limit, m.status.Reason),
		m.status, m.usage, "")
	slog.Info("rebalance: resizing toward the IM limit",
		"limit_im_pct", limit, "im_pct", fmt.Sprintf("%.2f", m.usage.IMPct()), "unit", m.usage.Unit,
		"share_per_slot", fmt.Sprintf("%.6f", share), "strangles", len(strangles))

	done := true
	for _, st := range strangles {
		if st.CallLeg == nil || st.PutLeg == nil || st.CallLeg.DTE() <= s.cfg.RolloutDTE {
			continue
		}
		callInst, callOk := s.md.GetInstrument(st.CallLeg.Instrument)
		putInst, putOk := s.md.GetInstrument(st.PutLeg.Instrument)
		if !callOk || !putOk {
			slog.Warn("rebalance: instrument not in market data, skipping", "strangle_id", st.ID)
			continue
		}
		lot := math.Max(callInst.MinTradeAmount, putInst.MinTradeAmount)
		if lot <= 0 {
			lot = s.cfg.MinTradeAmount
		}

		// Buying one lot back (positive size) frees this strangle's IM per lot.
		freed, err := s.simulate(ctx, map[string]float64{callInst.Name: lot, putInst.Name: lot}, m.usage.Unit)
		if err != nil {
			slog.Warn("rebalance: simulation failed, will retry", "strangle_id", st.ID, "err", err)
			done = false
			continue
		}
		imPerLot := m.usage.IM - freed.IM
		targetLots := TargetLots(share, imPerLot)
		if targetLots == 0 {
			slog.Info("rebalance: strangle adds no IM (portfolio netting), size kept", "strangle_id", st.ID)
			continue
		}
		targetQty := float64(targetLots) * lot

		addQty := orders.FloorToStep(math.Min(targetQty-st.CallLeg.Qty, targetQty-st.PutLeg.Qty), lot)
		if addQty >= lot {
			if over || headroom <= 0 {
				continue
			}
			addQty = math.Min(addQty, float64(EntryLots(headroom, imPerLot))*lot)
			if addQty >= lot {
				headroom -= addQty / lot * imPerLot
				s.openComplementStrangle(ctx, st, addQty, targetQty)
			}
			continue // never upsize and downsize in the same pass
		}
		if !over {
			continue // under the limit overall: no reason to cut this strangle
		}
		for _, leg := range []struct {
			pos  *orders.Position
			inst *marketdata.Instrument
		}{{st.CallLeg, callInst}, {st.PutLeg, putInst}} {
			if !s.downsizeLeg(ctx, leg.pos, leg.inst, targetQty, lot) {
				done = false
			}
		}
	}
	return done
}

// openComplementStrangle opens addQty more of st's slot through normal entry.
func (s *Strategy) openComplementStrangle(ctx context.Context, st *orders.Strangle, addQty, targetQty float64) {
	instruments := s.md.AllInstruments()
	expiry, ok := SelectExpiry(instruments, time.Now(), st.TargetDTE, s.cfg.MaxDTEDeviation, s.cfg.RolloutDTE)
	if !ok {
		slog.Warn("rebalance: no expiry found for complement strangle, skipping upsize",
			"strangle_id", st.ID, "target_dte", st.TargetDTE)
		return
	}
	call, callErr := SelectStrike(instruments, expiry, "call", st.EntryDelta, s.cfg.DeltaSlippage)
	put, putErr := SelectStrike(instruments, expiry, "put", st.EntryDelta, s.cfg.DeltaSlippage)
	if callErr != nil || putErr != nil {
		slog.Warn("rebalance: strike selection failed for complement strangle, skipping upsize",
			"strangle_id", st.ID, "call_err", callErr, "put_err", putErr)
		return
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
	if err := s.openStrangle(ctx, call, put, st.TargetDTE, st.EntryDelta, addQty, s.gamma.Evaluate()); err != nil {
		slog.Warn("rebalance: complement strangle open failed",
			"original_strangle_id", st.ID, "err", err)
	}
}

// downsizeLeg buys back whole lots above targetQty. The strike is kept.
// It returns false when the close failed or only partly filled.
func (s *Strategy) downsizeLeg(ctx context.Context, pos *orders.Position, inst *marketdata.Instrument, targetQty, lot float64) bool {
	excess := orders.FloorToStep(pos.Qty-targetQty, lot)
	if excess < lot {
		return true
	}
	oldQty := pos.Qty
	filled, err := s.buyToClose(ctx, pos, excess, orders.TriggerRebalanceDownsize, 0)
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
