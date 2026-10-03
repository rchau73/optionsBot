package strategy

import (
	"time"

	"optionsbot/internal/orders"
)

// RolloutDecision describes what action to take on a leg.
type RolloutDecision struct {
	Action        RolloutAction
	Reason        string
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
)

// EvaluateLeg applies rollout rules 4.1–4.5 in priority order and returns the
// highest-priority applicable decision for a single leg.
//
// now is the evaluation time: time.Now() live, the simulated date in a backtest.
func EvaluateLeg(pos *orders.Position, now time.Time, rolloutDTE int, deltaDriftThreshold, roiTakeProfit, stopLossMultiplier float64) RolloutDecision {
	dte := pos.DTEAt(now)

	// Rule 4.5 — Emergency stop-loss (highest priority)
	if pos.LossPct() >= stopLossMultiplier {
		return RolloutDecision{
			Action: ActionStopLoss,
			Reason: orders.TriggerStopLoss200Pct,
			LegID:  pos.ID,
		}
	}

	// Rule 4.1 — 19 DTE time-based rollout
	if dte <= rolloutDTE {
		return RolloutDecision{
			Action:        ActionRollNextMonth,
			Reason:        orders.TriggerRollout19DTE,
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

	// Rule 4.2 — Delta drift below threshold
	absDelta := absDelta(pos.CurrentGreeks.Delta)
	if absDelta < deltaDriftThreshold && dte >= 25 {
		return RolloutDecision{
			Action: ActionRollSameLeg,
			Reason: orders.TriggerRolloutDelta,
			LegID:  pos.ID,
		}
	}

	// Rule 4.3 — ROI take-profit >= 50%
	if pos.ROIPct() >= roiTakeProfit && dte >= 25 {
		return RolloutDecision{
			Action: ActionRollSameLeg,
			Reason: orders.TriggerRolloutROI,
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
