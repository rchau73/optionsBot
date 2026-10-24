package strategy

import (
	"fmt"
	"log/slog"
	"time"

	"optionsbot/internal/orders"
)

// noteTrendChange journals the SMA trend when it changes (trend_changed),
// with the condition that fired and the levels at that moment, so the monitor's
// Activity shows when the trend starts or stops blocking or shedding a side.
// It runs whatever the direction source: under bias the SMA trend is still
// recorded for comparison. The first trend seen (startup) is only remembered:
// it is not a change. Call on the Run goroutine.
func (s *Strategy) noteTrendChange(l SMALevels) {
	prev := s.lastSMATrend
	s.lastSMATrend = l.Trend
	if prev == "" || prev == l.Trend {
		return
	}
	detail := fmt.Sprintf("SMA trend %s → %s: %s (spot %.2f, SMA9 %.2f ± %.2f%%, SMA21 %.2f, swing high %s, swing low %s)",
		prev, l.Trend, l.Reason, l.Price, l.SMA9, l.SMA9BandPct, l.SMA21,
		swingText(l.SwingHigh, l.SwingHighDate), swingText(l.SwingLow, l.SwingLowDate))
	slog.Info("sma trend changed", "event", orders.EventTrend, "from", prev, "to", l.Trend, "reason", l.Reason,
		"direction", s.cfg.GEXDirection, "spot", l.Price, "sma9", l.SMA9, "sma21", l.SMA21,
		"swing_high", l.SwingHigh, "swing_high_date", l.SwingHighDate,
		"swing_low", l.SwingLow, "swing_low_date", l.SwingLowDate)
	direction := s.cfg.GEXDirection
	if direction != DirectionBias {
		direction = DirectionSMA
	}
	s.journal.LogTrend(orders.TrendRecord{
		Timestamp: time.Now(), StrategyID: s.strategyID(), Direction: direction,
		From: prev, To: l.Trend, Reason: l.Reason, Detail: detail, Spot: l.Price,
		SMA9: l.SMA9, SMA21: l.SMA21,
		SwingHigh: l.SwingHigh, SwingHighDate: l.SwingHighDate,
		SwingLow: l.SwingLow, SwingLowDate: l.SwingLowDate,
	})
}

func swingText(level float64, day string) string {
	if level <= 0 {
		return "none"
	}
	return fmt.Sprintf("%.2f on %s", level, day)
}
