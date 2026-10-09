package strategy

import (
	"context"
	"log/slog"
	"time"

	"optionsbot/internal/orders"
)

// repairIncompleteStrangles reopens the missing leg of any one-legged strangle
// (left by a stop-loss, a GEX close, a single-leg rollout or a partial entry),
// at the remaining leg's expiry and the strangle's entry delta and size.
//
// It skips a leg the GEX regime is actively shedding or the confirmed
// negative regime blocks in the trend's direction (same rules as entry), and
// a strangle whose remaining leg is already inside the rollout window — that
// leg is about to roll, and a fresh partner would roll straight after it.
//
// A leg that was stopped out is only re-sold once the market has calmed
// (RepairBlockReason); a leg shed by GEX once spot is back above the flip it
// was shed at (ShedRepairBlockReason); a leg closed by a take-profit or
// delta-drift roll is reopened at once — that is how the strategy rolls.
func (s *Strategy) repairIncompleteStrangles(ctx context.Context, gammaDec GammaDecision, m marginState) {
	instruments := s.md.AllInstruments()
	s.dropStaleAnchors()

	for _, st := range s.state.AllStrangles() {
		present, missingType := s.liveLegs(st)
		if present == nil || missingType == "" {
			continue // complete, or both legs gone (RemoveStrangleContaining cleans up)
		}
		if s.repairPending(st.ID) || s.slotUnconfirmed(makeSlotKey(st.TargetDTE, st.EntryDelta)) ||
			s.churnPaused(makeSlotKey(st.TargetDTE, st.EntryDelta), time.Now()) {
			continue
		}
		if gammaDec.Sheds(missingType) {
			slog.Debug("repair: skipping leg the GEX regime is shedding",
				"strangle_id", st.ID, "missing", missingType)
			continue
		}
		if present.DTE() <= s.cfg.RolloutDTE {
			continue
		}
		key := stopKey(st.ID, missingType)
		if reason := RegimeSideBlockReason(missingType, gammaDec.TrendDir, m.status); reason != "" {
			if s.repairHeld[key] != reason {
				s.repairHeld[key] = reason
				slog.Info("repair held: side blocked by the regime", "strangle_id", st.ID, "missing", missingType, "reason", reason)
				s.noteSkip(st.TargetDTE, st.EntryDelta, SkipRepairHeld, missingType+" "+reason)
			}
			continue
		}
		if a, shed := s.shedAnchors[key]; shed {
			a.Observe(gammaDec.Spot, gammaDec.SnapshotAt)
			if reason := ShedRepairBlockReason(a, time.Now(), gammaDec.NonNegativeSince,
				s.cfg.GEXRepairConfirmSnapshots, time.Duration(s.cfg.GEXRepairReleaseHours)*time.Hour); reason != "" {
				if s.repairHeld[key] != reason {
					s.repairHeld[key] = reason
					slog.Info("repair held: leg was shed by GEX", "strangle_id", st.ID, "missing", missingType, "reason", reason)
					s.noteSkip(st.TargetDTE, st.EntryDelta, SkipRepairHeld, missingType+" "+reason)
				}
				continue
			}
		}
		if at, stopped := s.stopped[key]; stopped {
			if reason := RepairBlockReason(at, time.Now(), time.Duration(s.cfg.RepairCooldownHours)*time.Hour, m.status); reason != "" {
				if s.repairHeld[key] != reason {
					s.repairHeld[key] = reason
					slog.Info("repair held: leg was stopped out", "strangle_id", st.ID, "missing", missingType, "reason", reason)
					s.noteSkip(st.TargetDTE, st.EntryDelta, SkipRepairHeld, missingType+" stopped out, "+reason)
				}
				continue
			}
		}

		delta := st.EntryDelta
		if delta == 0 {
			delta = s.cfg.EntryDelta
		}
		// Nearest the entry delta, not matched to the open leg: matching re-sells
		// closer to the money after a move and cost ~30 % of the result against
		// holding in stress tests (2026-10-07).
		inst, err := SelectStrike(instruments, present.Expiry, missingType, delta, s.cfg.DeltaSlippage)
		if err != nil {
			slog.Debug("repair: no suitable strike for missing leg",
				"strangle_id", st.ID, "missing", missingType, "err", err)
			continue
		}
		if err := s.checkPremiumFloor(inst, s.cfg.RepairPriceFloor); err != nil {
			slog.Debug("repair: premium below floor", "strangle_id", st.ID, "err", err)
			continue
		}

		leg, err := s.submitEntryLeg(ctx, inst, present.Qty, slotRef(st.TargetDTE, st.EntryDelta), orders.TriggerRepair)
		if err != nil {
			slog.Warn("repair: order submit failed",
				"strangle_id", st.ID, "missing", missingType, "err", err)
			continue
		}

		ps := &pendingStrangle{
			id:               s.state.NextID("ps"),
			targetDTE:        st.TargetDTE,
			entryDelta:       st.EntryDelta,
			expiry:           present.Expiry,
			underlying:       s.cfg.Underlying,
			submittedAt:      time.Now(),
			repairStrangleID: st.ID,
		}
		if missingType == "call" {
			ps.call = leg
		} else {
			ps.put = leg
		}

		if ps.allLegsDone() {
			s.finalizePending(ctx, ps)
			continue
		}
		delete(s.stopped, key)
		delete(s.repairHeld, key)
		delete(s.shedAnchors, key)
		s.addPending(ps)
		slog.Info("repair: missing leg order submitted",
			"strangle_id", st.ID, "missing", missingType,
			"instrument", inst.Name, "order_id", leg.orderID,
			"qty", leg.qty, "limit", leg.limitPrice,
			"gex_action", actionLabel(gammaDec.Action))
	}
}

// liveLegs returns a strangle's remaining open leg and the type of the missing
// one. present is nil when both legs are gone; missingType is "" when the
// strangle is complete.
func (s *Strategy) liveLegs(st *orders.Strangle) (present *orders.Position, missingType string) {
	call := s.livePosition(st.CallLeg)
	put := s.livePosition(st.PutLeg)
	switch {
	case call != nil && put != nil:
		return call, ""
	case call != nil:
		return call, "put"
	case put != nil:
		return put, "call"
	default:
		return nil, ""
	}
}

// livePosition returns leg if it is still an open position in the book.
func (s *Strategy) livePosition(leg *orders.Position) *orders.Position {
	if leg == nil {
		return nil
	}
	if pos, ok := s.state.GetPosition(leg.ID); ok {
		return pos
	}
	return nil
}
