package strategy

import (
	"fmt"
	"time"

	"optionsbot/internal/orders"
)

// RolloutDecision describes what action to take on a leg.
type RolloutDecision struct {
	Action        RolloutAction
	Reason        string
	Detail        string // why, with the numbers (journaled)
	LegID         string
	WholeStrangle bool
}

type RolloutAction int

const (
	ActionNone          RolloutAction = iota
	ActionClose                       // close only, no reopen on that expiry (drift/take-profit under the close-only floor, #753)
	ActionRollNextMonth               // close and reopen at next monthly expiry
	ActionRollSameLeg                 // close and reopen same leg at delta 0.16, same month
	ActionStopLoss                    // emergency market close
	ActionDeltaExit                   // early defensive close; held like a stop-loss
)

// EvaluateLeg applies the exit rules in priority order — stop-loss, DTE roll,
// delta exit, delta drift, take-profit — and returns the highest-priority
// applicable decision for a single leg.
//
// now is the evaluation time: time.Now() live, the simulated date in a backtest.
// deltaExit ≤ 0 disables the delta exit.
//
// closeOnlyBelowDTE (config.FixedCloseOnlyBelowDTE, 25; #753) is where delta
// drift and take-profit stop re-selling the leg: at or above it they roll the
// leg (ActionRollSameLeg, repair re-sells it on the same expiry), below it
// they buy it back only (ActionClose) and the slot re-enters on another
// expiry. ≤ 0 is the rule before #753: the roll from SameLegRollMinDTE, and
// nothing below it.
func EvaluateLeg(pos *orders.Position, now time.Time, rolloutDTE int, deltaDriftThreshold, roiTakeProfit, stopLossMultiplier, deltaExit float64, closeOnlyBelowDTE int) RolloutDecision {
	dte := pos.DTEAt(now)

	// Rule 4.5 — Emergency stop-loss (highest priority)
	if pos.LossPct() >= stopLossMultiplier {
		return RolloutDecision{
			Action: ActionStopLoss,
			Reason: orders.TriggerStopLoss200Pct,
			Detail: fmt.Sprintf("stop-loss: loss %.2f× the premium ≥ %.2f× (mark %.4f vs entry %.4f)", pos.LossPct(), stopLossMultiplier, pos.CurrentMid, pos.EntryPrice),
			LegID:  pos.ID,
		}
	}

	// Rule 4.1 — 19 DTE time-based rollout
	if dte <= rolloutDTE {
		return RolloutDecision{
			Action:        ActionRollNextMonth,
			Reason:        orders.TriggerRollout19DTE,
			Detail:        fmt.Sprintf("time roll: %d days to expiry ≤ %d — the whole strangle closes", dte, rolloutDTE),
			LegID:         pos.ID,
			WholeStrangle: true,
		}
	}

	// Delta drift and take-profit need a live quote: without one, delta and
	// mid are only the last known values (or zero), and acting on them would
	// close a leg for no reason. Stop-loss (on the last known mark) and the
	// time roll above still apply.
	if !pos.MarkLive {
		return RolloutDecision{Action: ActionNone, LegID: pos.ID}
	}

	absDelta := absDelta(pos.CurrentGreeks.Delta)

	// Delta exit — the move against the leg is real (a 0.16-delta short now
	// at 0.30): close before the 2× stop, at a smaller loss. Re-selling at
	// once would sell into the same move, so the leg is held like a stopped
	// one. In stress simulations this beat both the stop alone and a
	// roll-and-re-sell, in trends and in chop.
	if deltaExit > 0 && absDelta >= deltaExit {
		return RolloutDecision{
			Action: ActionDeltaExit,
			Reason: orders.TriggerDeltaExit,
			Detail: fmt.Sprintf("delta exit: |Δ| %.3f ≥ %.2f — the move against the leg is real; re-sold only once calm", absDelta, deltaExit),
			LegID:  pos.ID,
		}
	}

	// Rule 4.2 — Delta drift below threshold
	if absDelta < deltaDriftThreshold {
		detail := fmt.Sprintf("delta drift: |Δ| %.3f < %.2f — little premium left", absDelta, deltaDriftThreshold)
		if d, ok := sameLegExit(pos.ID, orders.TriggerRolloutDelta, detail, "; repair re-sells nearer the money", dte, closeOnlyBelowDTE); ok {
			return d
		}
	}

	// Rule 4.3 — ROI take-profit >= 50%
	if pos.ROIPct() >= roiTakeProfit {
		detail := fmt.Sprintf("take-profit: %.0f%% of the premium earned ≥ %.0f%%", pos.ROIPct()*100, roiTakeProfit*100)
		if d, ok := sameLegExit(pos.ID, orders.TriggerRolloutROI, detail, "", dte, closeOnlyBelowDTE); ok {
			return d
		}
	}

	return RolloutDecision{Action: ActionNone, LegID: pos.ID}
}

// SameLegRollMinDTE is the lowest DTE at which delta drift and take-profit
// roll a leg when the close-only floor is off (closeOnlyBelowDTE ≤ 0): the
// rule before #753.
const SameLegRollMinDTE = 25

// CloseOnlyMarker starts the note a close-only exit's detail ends with; the
// journal replay finds those closes by it (CloseOnlyFromTrades).
const CloseOnlyMarker = "close only: <"

// sameLegExit is a drift or take-profit leg's decision at dte: the roll at or
// above the floor, the close only below it (ok false: nothing, the floor off).
func sameLegExit(legID, reason, detail, rollNote string, dte, closeOnlyBelowDTE int) (RolloutDecision, bool) {
	floor := closeOnlyBelowDTE
	if floor <= 0 {
		floor = SameLegRollMinDTE
	}
	switch {
	case dte >= floor:
		return RolloutDecision{Action: ActionRollSameLeg, Reason: reason, Detail: detail + rollNote, LegID: legID}, true
	case closeOnlyBelowDTE > 0:
		return RolloutDecision{Action: ActionClose, Reason: reason, LegID: legID,
			Detail: fmt.Sprintf("%s — %s %d DTE (%d days left): bought back, not re-sold on this expiry; the slot re-enters on another", detail, CloseOnlyMarker, floor, dte)}, true
	}
	return RolloutDecision{}, false
}

func absDelta(d float64) float64 {
	if d < 0 {
		return -d
	}
	return d
}
