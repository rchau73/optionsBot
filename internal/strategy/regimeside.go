package strategy

import (
	"optionsbot/internal/risk"
)

// A confirmed negative gamma regime used to change only the IM limit (the
// lowest band): once confirmed it also lifted the freeze, and entries and
// top-ups sold both sides again in the middle of the move — 2026-10-09 00:00
// UTC, ETH in a bear trend topped up 2200-P and 2100-P. Dealers short gamma
// push price further the way it is going, so the side the trend runs toward
// is not sold; the other side still collects the rich premium. Stress test
// (19 paths, net of fees, vs holding): blocking all new risk instead cost
// ETH 21–27 %; this rule was +0.4–1.8 % on ETH and +20–24 % on BTC.

// RegimeSideBlockReason says why no new short of optType may be sold now (an
// entry, a top-up or a repair), or "" when it may: under a confirmed negative
// gamma regime, no puts in a bear trend and no calls in a bull trend.
func RegimeSideBlockReason(optType string, trend int, st risk.Status) string {
	if !st.RegimeUsed || !st.RegimeNegative {
		return ""
	}
	switch {
	case optType == "put" && trend < 0:
		return "confirmed negative gamma regime in a bear trend: no new puts"
	case optType == "call" && trend > 0:
		return "confirmed negative gamma regime in a bull trend: no new calls"
	}
	return ""
}

// avoidsSide reports why a new short of optType may not be sold now: the GEX
// live signal sheds that side, or the confirmed regime and the trend block it.
func (d GammaDecision) avoidsSide(optType string, st risk.Status) string {
	if d.Sheds(optType) {
		return "GEX is shedding " + optType + "s"
	}
	return RegimeSideBlockReason(optType, d.TrendDir, st)
}
