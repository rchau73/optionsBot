package strategy

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

// rebalancePositions runs once at startup, after reconcile and ticker warm-up.
// It resizes reconciled strangles to the current budget, so a config change
// between restarts (leverage, max_margin_pct) takes effect without a manual roll:
//
//   - Over budget by at least one lot: buy back the excess of each leg at
//     market — reducing exposure is a risk action, so speed beats price.
//   - Under budget by at least one lot: open a complement strangle for the
//     difference through the normal entry path (limit orders, fill tracking).
//     The existing legs are never closed to re-enter: that would realise P&L
//     for no reason.
//
// Strangles inside the rollout window are skipped (they roll anyway), and so
// are one-legged strangles (repair completes them first).
func (s *Strategy) rebalancePositions(ctx context.Context) {
	strangles := s.state.AllStrangles()
	if len(strangles) == 0 {
		return
	}

	equity, initialMarginUsed, err := s.fetchMarginState(ctx)
	if err != nil {
		slog.Warn("rebalance: cannot fetch margin state, skipping", "err", err)
		return
	}

	allowed := s.marginGuard.AllowedMargin(equity)
	targetPerSlot := allowed / float64(len(s.cfg.Slots()))
	unit := strings.ToLower(s.cfg.Underlying)

	slog.Info("rebalance: evaluating reconciled positions against current budget",
		"equity_"+unit, fmt.Sprintf("%.6f", equity),
		"margin_allowed_"+unit, fmt.Sprintf("%.6f", allowed),
		"margin_used_"+unit, fmt.Sprintf("%.6f", initialMarginUsed),
		"target_per_slot_"+unit, fmt.Sprintf("%.6f", targetPerSlot),
		"strangles", len(strangles),
	)

	for _, st := range strangles {
		if st.CallLeg == nil || st.PutLeg == nil {
			continue
		}
		if st.CallLeg.DTE() <= s.cfg.RolloutDTE {
			slog.Debug("rebalance: skipping near-expiry strangle",
				"strangle_id", st.ID, "dte", st.CallLeg.DTE())
			continue
		}

		callInst, callOk := s.md.GetInstrument(st.CallLeg.Instrument)
		putInst, putOk := s.md.GetInstrument(st.PutLeg.Instrument)
		if !callOk || !putOk {
			slog.Warn("rebalance: instrument not in market data, skipping",
				"strangle_id", st.ID,
				"call", st.CallLeg.Instrument, "call_found", callOk,
				"put", st.PutLeg.Instrument, "put_found", putOk)
			continue
		}

		lot := math.Max(callInst.MinTradeAmount, putInst.MinTradeAmount)
		targetQty := s.resolveQty(ctx, callInst, putInst, targetPerSlot)

		// Upsize by the smaller of the two shortfalls so the complement stays symmetric.
		addQty := orders.FloorToStep(math.Min(targetQty-st.CallLeg.Qty, targetQty-st.PutLeg.Qty), lot)
		if addQty >= lot {
			s.openComplementStrangle(ctx, st, addQty, targetQty)
			continue // never upsize and downsize in the same pass
		}

		for _, leg := range []struct {
			pos  *orders.Position
			inst *marketdata.Instrument
		}{{st.CallLeg, callInst}, {st.PutLeg, putInst}} {
			s.downsizeLeg(ctx, leg.pos, leg.inst, targetQty, lot)
		}
	}
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
	if err := s.openStrangle(ctx, call, put, st.TargetDTE, st.EntryDelta, s.md.IVPercentile(), addQty, s.gamma.Evaluate()); err != nil {
		slog.Warn("rebalance: complement strangle open failed",
			"original_strangle_id", st.ID, "err", err)
	}
}

// downsizeLeg buys back whole lots above targetQty. The strike is kept.
func (s *Strategy) downsizeLeg(ctx context.Context, pos *orders.Position, inst *marketdata.Instrument, targetQty, lot float64) {
	excess := orders.FloorToStep(pos.Qty-targetQty, lot)
	if excess < lot {
		return
	}
	oldQty := pos.Qty
	filled, err := s.buyToClose(ctx, pos, excess, orders.TriggerRebalanceDownsize, 0)
	if err != nil {
		slog.Warn("rebalance: downsize failed",
			"instrument", inst.Name, "excess_qty", excess, "err", err)
		return
	}
	slog.Info("rebalance: downsized position",
		"instrument", inst.Name,
		"old_qty", fmt.Sprintf("%.4f", oldQty),
		"new_qty", fmt.Sprintf("%.4f", oldQty-filled),
		"closed_qty", fmt.Sprintf("%.4f", filled),
		"requested_qty", fmt.Sprintf("%.4f", excess),
	)
}
