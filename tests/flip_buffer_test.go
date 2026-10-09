package tests

import (
	"math"
	"testing"
	"time"

	"optionsbot/internal/gex"
	"optionsbot/internal/strategy"
)

// A leg is shed only once spot is gamma_flip_buffer_sd daily standard
// deviations (DVOL ÷ √365) below the gamma flip. On 2026-10-05 ETH's calls
// were shed with spot 0.6 % below the flip (2,699.5 vs 2,715.4), and the
// Oct 23 call, sold by repair two hours earlier, was bought back at no
// profit. In stress simulations a 1σ buffer beat no buffer on 10 of 11 paths
// at BTC-, ETH- and SOL-like volatility; a fixed % did not transfer.

func TestFlipBufferPct_ScalesWithVolatility(t *testing.T) {
	for _, c := range []struct {
		sd, dvol, want float64
	}{
		{1, 36.5, 36.5 / math.Sqrt(365)}, // BTC today ≈ 1.91 %
		{1, 50, 2.617},                   // ETH ≈ 2.6 %
		{1, 88, 4.606},                   // SOL-like ≈ 4.6 %
		{0.5, 50, 1.309},
		{0, 50, 0}, // disabled
		{1, 0, 0},  // no DVOL reading: shed as before, never less
	} {
		if got := strategy.FlipBufferPct(c.sd, c.dvol); math.Abs(got-c.want) > 1e-3 {
			t.Errorf("FlipBufferPct(%v, %v) = %.4f, want %.4f", c.sd, c.dvol, got, c.want)
		}
	}
}

func TestResolveGammaAction_FlipBuffer(t *testing.T) {
	flip := 2715.38
	buf := strategy.FlipBufferPct(1, 50) // ≈ 2.62 %: shed below ≈ 2,644.3
	cases := []struct {
		name string
		spot float64
		want strategy.GammaAction
	}{
		{"ETH on 2026-10-05: 0.6 % below the flip — noise, keep the calls", 2699.5, strategy.GammaActionNone},
		{"just inside the buffer", flip*(1-buf/100) + 0.01, strategy.GammaActionNone},
		{"past the buffer — a real break", flip * (1 - buf/100 - 0.001), strategy.GammaActionCloseCalls},
		{"well below", 2600, strategy.GammaActionCloseCalls},
	}
	for _, c := range cases {
		if got := strategy.ResolveGammaAction("NEGATIVE/ACCELERATION", true, c.spot, flip, 1, buf); got != c.want {
			t.Errorf("%s: spot %.2f → %v, want %v", c.name, c.spot, got, c.want)
		}
	}
	// No buffer: today's rule, which shed at the first tick below.
	if got := strategy.ResolveGammaAction("NEGATIVE/ACCELERATION", true, 2699.5, flip, 1, 0); got != strategy.GammaActionCloseCalls {
		t.Errorf("without a buffer the old rule sheds: %v", got)
	}
}

// Run level, with the fixture's DVOL of 55 (1σ ≈ 2.88 %): a dip 1 % under the
// flip keeps both legs; a break 5 % under sheds the put as before.
func TestStrategy_FlipBufferKeepsLegsOnSmallDips(t *testing.T) {
	for _, c := range []struct {
		flip     float64
		wantShed bool
	}{{101000, false}, {105300, true}} {
		f := newStrategyFixture(t)
		f.cfg.GammaFlipBufferSD = 1
		f.withOpenStrangle(0.1, 0.02)
		f.withPutSheddingRegime() // bear trend, regime negative
		f.gex = fixedGEX{&gex.Snapshot{Regime: "NEGATIVE/ACCELERATION", Spot: 100000, GammaFlip: c.flip, GammaFlipFound: true}}
		f.startRun()

		if c.wantShed {
			eventually(t, 2*time.Second, "put shed", func() bool { return f.position(f.put) == nil })
			continue
		}
		eventually(t, 2*time.Second, "positions loaded", func() bool { return len(f.state.AllPositions()) == 2 })
		time.Sleep(150 * time.Millisecond) // many cycles
		if n := len(f.exch.buys()); n != 0 || f.position(f.put) == nil {
			t.Errorf("flip %.0f (1 %% above spot, inside the 2.9 %% buffer): nothing may be shed, got %d buys", c.flip, n)
		}
	}
}

// The monitor shows the flip's ±1σ band from the bot's own buffer, so the
// band on screen is the one the shed and its repair use.
func TestGammaMonitor_FlipBufferFollowsLiveDVOL(t *testing.T) {
	g := strategy.NewGammaMonitor(120, 5)
	if got := g.FlipBuffer(); got != 0 {
		t.Fatalf("no buffer configured: FlipBuffer = %.4f, want 0", got)
	}
	dvol := 36.5
	g.SetFlipBuffer(1, func() float64 { return dvol })
	if got, want := g.FlipBuffer(), strategy.FlipBufferPct(1, 36.5); math.Abs(got-want) > 1e-9 {
		t.Errorf("FlipBuffer = %.4f, want %.4f", got, want)
	}
	dvol = 73
	if got, want := g.FlipBuffer(), strategy.FlipBufferPct(1, 73); math.Abs(got-want) > 1e-9 {
		t.Errorf("after DVOL moved: FlipBuffer = %.4f, want %.4f", got, want)
	}
}
