package strategy

import (
	"fmt"
	"log/slog"
)

// noteBiasWarmup journals a bias shed held back because the flip window does
// not cover its span yet (GammaDecision.WarmupHeld), once per held action.
// The window is in memory, so every restart empties it: with a few minutes of
// snapshots its median is the live flip, and a pre-expiry flip jump shed
// BTC's calls 10 minutes after the 2026-10-14 restart. A real move is shed
// once the window is full, if the bias still holds against its median.
func (s *Strategy) noteBiasWarmup(dec GammaDecision) {
	if dec.WarmupHeld == s.warmupHeld {
		return
	}
	s.warmupHeld = dec.WarmupHeld
	if dec.WarmupHeld == GammaActionNone {
		return
	}
	side := "put"
	if dec.WarmupHeld == GammaActionCloseCalls {
		side = "call"
	}
	legs := 0
	for _, pos := range s.state.AllPositions() {
		if pos.OptionType == side {
			legs++
		}
	}
	detail := fmt.Sprintf("%s: bias %s would shed %d %s leg(s) (spot %.0f, flip %.0f, median %.0f, buffer %.1f%%), but the flip window is warming up until %s",
		SkipBiasWarmup, dec.Bias, legs, side, dec.Spot, dec.GammaFlip, dec.BiasFlip, dec.FlipBufferPct,
		dec.BiasWarmupUntil.UTC().Format("Jan 02 15:04 UTC"))
	slog.Warn("bias shed held: flip window warming up", "event", SkipBiasWarmup, "action", actionLabel(dec.WarmupHeld),
		"legs", legs, "gex_spot", dec.Spot, "gamma_flip", dec.GammaFlip, "bias_flip", dec.BiasFlip,
		"flip_buffer_pct", dec.FlipBufferPct, "warmup_until", dec.BiasWarmupUntil)
	s.journal.LogSkipped(detail, s.eventContext(nil, nil))
}
