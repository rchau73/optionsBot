package strategy

import (
	"fmt"

	"optionsbot/internal/history"
)

// recordDirection writes the SMA trend and the GEX bias side by side, with
// the shed each would make, whenever one of them changes — the record that
// lets the bias (in use since 2026-10-12) be compared with the SMA trend.
func (s *Strategy) recordDirection(dec GammaDecision) {
	if s.directions == nil || dec.SnapshotAt.IsZero() {
		return
	}
	other := dec.shadowAction()
	key := fmt.Sprintf("%s|%s|%s|%d|%d|%d", dec.Direction, dec.SMATrend, dec.Bias, dec.Action, dec.Shed, other)
	if key == s.lastDirection {
		return
	}
	s.lastDirection = key
	r := history.DirectionRecord{
		At: dec.SnapshotAt, Spot: dec.Spot, BufferPct: dec.FlipBufferPct, Regime: dec.Regime,
		Direction: dec.Direction, SMATrend: dec.SMATrend, Bias: dec.Bias, BiasFlip: dec.BiasFlip,
		Action: actionLabel(dec.Action), Shed: actionLabel(dec.Shed), OtherAction: actionLabel(other),
	}
	if dec.GammaFlipFound {
		r.Flip = dec.GammaFlip
	}
	s.directions.Record(r)
}
