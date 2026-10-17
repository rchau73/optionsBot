package tests

import (
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// #120: cmd/stress reads the trend with strategy.SMALevelsOf, the rule the
// bot's detectTrend runs, so the two cannot drift apart. The old stress
// model (SMA9 vs SMA21 only) missed swing breakouts: on the 2026-10-16 rally
// run it was not bull at 89.8k while the bot, above the 86,207 swing high,
// would have been — and the stress re-sold a call into the rally.

// dailySeries turns prices into daily closes ending yesterday (UTC).
func dailySeries(prices []float64) []orders.DailyClose {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	out := make([]orders.DailyClose, len(prices))
	for i, p := range prices {
		out[i] = orders.DailyClose{Date: today.AddDate(0, 0, i-len(prices)), Close: p}
	}
	return out
}

// botMonitorLevels is what the bot's GammaMonitor reads on the same closes
// with spot as the live price.
func botMonitorLevels(closes []orders.DailyClose, spot float64) strategy.SMALevels {
	g := strategy.NewGammaMonitor(config.FixedGammaTrendLookbackDays, config.FixedSwingPivotN)
	g.SeedDailyCloses(closes)
	g.PushPriceAt(closes[len(closes)-1].Date.Add(12*time.Hour), spot) // same day as the last close: no new close
	return g.SMALevels()
}

func TestStressTrend_MatchesDetectTrend(t *testing.T) {
	// A slide from 90k to 80k, a bounce to a swing high of 86,207, a dip to
	// a swing low of 81,000, then a drift back up — SMA9 still under SMA21.
	var prices []float64
	for i := 0; i < 15; i++ {
		prices = append(prices, 90000-float64(i)*600) // 90,000 → 81,600
	}
	prices = append(prices, 83000, 84500, 86207, 85000, 84000, 83000, 82000, 81500, 81000, 81400, 81800, 82200, 82600, 83000, 83300, 83600)
	closes := dailySeries(prices)

	cases := []struct {
		name   string
		spot   float64
		dir    int
		reason string
	}{
		{"above the swing high: bull although SMA9 < SMA21", 89800, 1, strategy.TrendReasonBreakout},
		{"inside the swings", 84000, 0, strategy.TrendReasonInside},
		{"below the swing low: bear", 80500, -1, strategy.TrendReasonBreakdown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stress := strategy.SMALevelsOf(closes, c.spot, config.FixedSwingPivotN)
			bot := botMonitorLevels(closes, c.spot)
			if stress != bot {
				t.Fatalf("stress levels %+v\nbot levels    %+v", stress, bot)
			}
			if stress.TrendDir != c.dir || stress.Reason != c.reason {
				t.Errorf("trend %d (%s), want %d (%s)", stress.TrendDir, stress.Reason, c.dir, c.reason)
			}
			if stress.SwingHigh != 86207 || stress.SwingLow != 81000 {
				t.Errorf("swings %v / %v, want 86207 / 81000", stress.SwingHigh, stress.SwingLow)
			}
			if !(stress.SMA9 < stress.SMA21) {
				t.Errorf("SMA9 %.0f ≥ SMA21 %.0f: the case needs the SMAs bearish", stress.SMA9, stress.SMA21)
			}
		})
	}
}
