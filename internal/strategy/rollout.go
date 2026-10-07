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
	ActionClose                       // close only, no reopen
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
func EvaluateLeg(pos *orders.Position, now time.Time, rolloutDTE int, deltaDriftThreshold, roiTakeProfit, stopLossMultiplier, deltaExit float64) RolloutDecision {
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
	if absDelta < deltaDriftThreshold && dte >= 25 {
		return RolloutDecision{
			Action: ActionRollSameLeg,
			Reason: orders.TriggerRolloutDelta,
			Detail: fmt.Sprintf("delta drift: |Δ| %.3f < %.2f — little premium left; repair re-sells nearer the money", absDelta, deltaDriftThreshold),
			LegID:  pos.ID,
		}
	}

	// Rule 4.3 — ROI take-profit >= 50%
	if pos.ROIPct() >= roiTakeProfit && dte >= 25 {
		return RolloutDecision{
			Action: ActionRollSameLeg,
			Reason: orders.TriggerRolloutROI,
			Detail: fmt.Sprintf("take-profit: %.0f%% of the premium earned ≥ %.0f%%", pos.ROIPct()*100, roiTakeProfit*100),
			LegID:  pos.ID,
		}
	}

	return RolloutDecision{Action: ActionNone, LegID: pos.ID}
}

func absDelta(d float64) float64 {
	if d < 0 {
		return -d
	}
	return d
}
