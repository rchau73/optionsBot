package tests

import (
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// #243: with spot on SMA9 the SMA trend flipped bear ↔ neutral every
// 40-second cycle (ETH 31 changes on 2026-10-20) and the regime-side rule let
// a put repair through in a one-cycle neutral blip; the order kept working
// after the trend turned bear again and filled 100 lots. The fix: a band of
// 0.10 daily σ around SMA9 (the price changes side only past it), and the
// working sells of a side the rule blocks again are cancelled.

func TestSMA9Side(t *testing.T) {
	cases := []struct {
		name                 string
		price, sma9, bandPct float64
		prev, want           int
	}{
		{"above the band", 100.3, 100, 0.2, -1, 1},
		{"below the band", 99.7, 100, 0.2, 1, -1},
		{"inside the band, was below: stays below", 100.1, 100, 0.2, -1, -1},
		{"inside the band, was above: stays above", 99.9, 100, 0.2, 1, 1},
		{"inside the band, no side yet: plain comparison above", 100.1, 100, 0.2, 0, 1},
		{"inside the band, no side yet: plain comparison below", 99.9, 100, 0.2, 0, -1},
		{"on SMA9 with no side yet", 100, 100, 0.2, 0, 0},
		{"on the band's edge is inside", 100.2, 100, 0.2, -1, -1},
		{"no band: plain comparison whatever the prev", 100.01, 100, 0, -1, 1},
		{"no SMA9", 100, 0, 0.2, 1, 0},
		{"negative band is none", 100.01, 100, -1, -1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := strategy.SMA9Side(c.price, c.sma9, c.bandPct, c.prev); got != c.want {
				t.Errorf("SMA9Side(%v, %v, %v, %d) = %d, want %d", c.price, c.sma9, c.bandPct, c.prev, got, c.want)
			}
		})
	}
}

// SMATrendOf (no band) is unchanged: the SMA condition reads the price itself.
func TestSMATrendOfSide_BandHoldsTheSMACondition(t *testing.T) {
	// Bear SMAs (SMA9 < SMA21), swings far away, price just above SMA9.
	if dir, _ := strategy.SMATrendOf(98.05, 110, 90, 98, 100); dir != 0 {
		t.Errorf("no band: price above SMA9 is neutral, got %d", dir)
	}
	if dir, reason := strategy.SMATrendOfSide(98.05, -1, 110, 90, 98, 100); dir != -1 || reason != strategy.TrendReasonBearSMA {
		t.Errorf("still below SMA9 (band): bear, got %d %q", dir, reason)
	}
	// Swing breakouts read the price itself, band or not.
	if dir, _ := strategy.SMATrendOfSide(89, 1, 110, 90, 98, 100); dir != -1 {
		t.Errorf("below the swing low is bear whatever the SMA9 side, got %d", dir)
	}
}

func TestConfig_SMA9BandFixed(t *testing.T) {
	var c config.Config
	c.ApplyFixed()
	if c.SMA9BandSD != 0.10 || config.FixedSMA9BandSD != 0.10 {
		t.Errorf("sma9_band_sd = %v, want 0.10 (#243)", c.SMA9BandSD)
	}
}

// bearCloses: 30 daily closes up to a 120,000 peak then a slide to 100,000 —
// SMA9 < SMA21, a bear trend below SMA9 (as withPutSheddingRegime).
func bearCloses() []orders.DailyClose {
	start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -30)
	var out []orders.DailyClose
	for i := range 30 {
		price := 110000.0 + float64(i)*1000
		if i > 10 {
			price = 120000 - float64(i-10)*1000
		}
		out = append(out, orders.DailyClose{Date: start.AddDate(0, 0, i), Close: price})
	}
	out[29].Close = 100000
	return out
}

// trendFlips starts a gamma monitor in a bear trend (spot 100,000 under
// SMA9), then feeds it SMA9 × each of rel, one 40-second cycle apart, and
// counts the SMA trend changes.
func trendFlips(bandSD float64, rel []float64) (flips int, trends []string) {
	g := strategy.NewGammaMonitor(21, 2)
	g.SeedDailyCloses(bearCloses())
	g.SetFlipBuffer(1.5, func() float64 { return 55 })
	g.SetSMA9Band(bandSD)
	now := time.Now().UTC().Truncate(24 * time.Hour).Add(time.Hour) // the whole run on one UTC day
	g.PushPriceAt(now, 100000)
	sma9, prev := g.SMALevels().SMA9, g.SMALevels().Trend
	for i, r := range rel {
		g.PushPriceAt(now.Add(time.Duration(i+1)*40*time.Second), sma9*r)
		tr := g.SMALevels().Trend
		if tr != prev {
			flips++
		}
		prev = tr
		trends = append(trends, tr)
	}
	return flips, trends
}

func TestGammaMonitor_SMA9BandStopsFlaps(t *testing.T) {
	// Spot hugging SMA9: 0.05 % either side, cycle after cycle.
	var hug []float64
	for range 20 {
		hug = append(hug, 1+0.0005, 1-0.0005)
	}
	if flips, _ := trendFlips(0, hug); flips != 40 {
		t.Fatalf("without the band the trend flaps every cycle, got %d flips", flips)
	}
	flips, trends := trendFlips(0.10, hug)
	if flips != 0 || trends[len(trends)-1] != "bear" {
		t.Errorf("with the 0.10 σ band (%.3f %%) the trend holds bear, got %d flips, last %s",
			strategy.FlipBufferPct(0.10, 55), flips, trends[len(trends)-1])
	}

	// A sustained move past the band still changes the trend, at once.
	band := strategy.FlipBufferPct(0.10, 55) / 100
	flips, trends = trendFlips(0.10, []float64{1 + 2*band, 1 + 2*band})
	if flips != 1 || trends[0] != "neutral" {
		t.Errorf("past the band the trend changes on the first price: trends %v", trends)
	}
	// And it comes back only past the band on the other side.
	_, trends = trendFlips(0.10, []float64{1 + 2*band, 1 - band/2, 1 - 2*band})
	if trends[1] != "neutral" || trends[2] != "bear" {
		t.Errorf("back into the band keeps neutral, below it bear: %v", trends)
	}
}

// withBandedBearRegime: a lone call, a confirmed negative regime with a bear
// trend and spot above the flip (only the regime-side rule applies), and the
// bot's SMA9 band.
func withBandedBearRegime(f *strategyFixture, bandSD float64) {
	f.withLoneCall(0.2)
	f.withBearTrendAboveFlip()
	f.cfg.SMA9BandSD = bandSD
}

// Scenario: a one-cycle blip does not unblock puts.
func TestStrategy_SMA9BlipDoesNotUnblockPuts(t *testing.T) {
	for _, c := range []struct {
		name   string
		bandSD float64
		leaks  bool
	}{
		{"with the 0.10 σ band", 0.10, false},
		{"without a band (before #243)", 0, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newStrategyFixture(t)
			withBandedBearRegime(f, c.bandSD)
			f.startRun()

			eventually(t, 2*time.Second, "put repair held", func() bool {
				return f.skips(strategy.SkipRepairHeld+": put confirmed negative gamma regime in a bear trend") > 0
			})
			sma9 := f.strat.View().SMA.SMA9
			setPrice(f, sma9*1.0005) // 0.05 % over SMA9 …
			if c.leaks {
				eventually(t, 2*time.Second, "put sold in the blip", func() bool { return putSells(f) > 0 })
				return
			}
			time.Sleep(60 * time.Millisecond) // … for several 10 ms cycles …
			setPrice(f, 100000)               // … and back
			time.Sleep(50 * time.Millisecond)
			if n := putSells(f); n != 0 {
				t.Errorf("no put may be sold in a blip inside the band, got %d", n)
			}
			if n := len(f.journal.trendChanges()); n != 0 {
				t.Errorf("a blip inside the band is not a trend change, got %d", n)
			}
		})
	}
}

// Scenario: a working order of a newly blocked side is cancelled.
func TestStrategy_RegimeSideBlockCancelsWorkingPut(t *testing.T) {
	f := newStrategyFixture(t)
	withBandedBearRegime(f, 0.10)
	f.market.price = 106000 // above SMA9 + band, below SMA21: neutral, puts allowed
	f.startRun()

	eventually(t, 2*time.Second, "put repair working", func() bool { return putSells(f) == 1 })
	id := f.exch.orderIDFor(f.put, 0)
	f.exch.partial(id, 0.1, 0.021) // half of it fills
	setPrice(f, 100000)            // below SMA9 − band: bear, the side is blocked again

	var cancel journalEntry
	eventually(t, 2*time.Second, "put repair cancelled by the regime-side rule", func() bool {
		for _, e := range f.journal.events(orders.EventCancelled) {
			if e.instrument == f.put && e.trigger == orders.TriggerRegimeSide {
				cancel = e
				return true
			}
		}
		return false
	})
	if cancel.filledQty != 0.1 {
		t.Errorf("the cancel reads back the partial fill: filled %v, want 0.1", cancel.filledQty)
	}
	if p := f.position(f.put); p == nil || p.Qty != 0.1 {
		t.Errorf("the partial fill is booked as the put leg, got %+v", p)
	}
	time.Sleep(50 * time.Millisecond)
	if n := putSells(f); n != 1 {
		t.Errorf("no further put is sold while the side is blocked, got %d sells", n)
	}
}

// Scenario: a sustained change still counts.
func TestStrategy_SustainedMovePastSMA9BandSellsPuts(t *testing.T) {
	f := newStrategyFixture(t)
	withBandedBearRegime(f, 0.10)
	f.startRun()

	eventually(t, 2*time.Second, "put repair held", func() bool {
		return f.skips(strategy.SkipRepairHeld+": put confirmed negative gamma regime in a bear trend") > 0
	})
	setPrice(f, 106000) // past SMA9 + band and staying there: neutral
	eventually(t, 2*time.Second, "put repaired as today", func() bool { return putSells(f) == 1 })
	eventually(t, 2*time.Second, "trend neutral", func() bool { return f.strat.View().SMA.Trend == "neutral" })
}
