package tests

import (
	"math"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/gex"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// Issue #82: the 60-minute median flip that guards the bias against flip
// jumps is in memory, so a restart empties it. On 2026-10-14 BTC restarted at
// ~16:57 and at 17:07 the window held ten minutes of the 79,500 jump: the
// median was the jump, and every call was shed. Until the window covers its
// span the bias sheds nothing (entries and repairs still skip the side).

const btcNeg = "NEGATIVE/ACCELERATION"

// bufferSDFor gives the flip buffer pct at dvol in daily standard deviations.
func bufferSDFor(pct, dvol float64) float64 { return pct * math.Sqrt(365) / dvol }

func newBiasMonitor(window time.Duration) (*strategy.GammaMonitor, func(at time.Time, spot, flip float64) strategy.GammaDecision) {
	g := strategy.NewGammaMonitor(21, 2)
	src := &switchGEX{}
	g.SetGEXSource(src)
	g.SetDirection(strategy.DirectionBias)
	g.SetShedConfirm(2)
	g.SetBiasFlipWindow(window)
	g.SetFlipBuffer(bufferSDFor(3, 40), func() float64 { return 40 }) // 3.0 %
	return g, func(at time.Time, spot, flip float64) strategy.GammaDecision {
		src.set(&gex.Snapshot{ComputedAt: at, Regime: btcNeg, Spot: spot, GammaFlip: flip, GammaFlipFound: true})
		return g.Evaluate()
	}
}

func TestGammaMonitor_BiasWarmupHoldsShedAfterRestart(t *testing.T) {
	_, step := newBiasMonitor(time.Hour)
	start := time.Date(2026, 10, 14, 16, 57, 0, 0, time.UTC)
	step(start, 81900, 83595)
	step(start.Add(time.Minute), 81900, 83595)
	var d strategy.GammaDecision
	for m := 2; m <= 10; m++ { // the flip jumps to 79,500 and stays
		d = step(start.Add(time.Duration(m)*time.Minute), 81938, 79500)
		if d.Shed != strategy.GammaActionNone {
			t.Fatalf("minute %d: shed %v during the warm-up", m, d.Shed)
		}
	}
	if d.Bias != "up" || d.BiasFlip != 79500 {
		t.Fatalf("the median is the jump itself: bias %s median %.0f", d.Bias, d.BiasFlip)
	}
	if d.Action != strategy.GammaActionCloseCalls {
		t.Errorf("live signal %v: entries and repairs still skip calls", d.Action)
	}
	if d.WarmupHeld != strategy.GammaActionCloseCalls || !d.BiasWarmupUntil.Equal(start.Add(time.Hour)) {
		t.Errorf("held %v until %v, want calls until %v", d.WarmupHeld, d.BiasWarmupUntil, start.Add(time.Hour))
	}
}

func TestGammaMonitor_BiasGuardAfterWarmupUnchanged(t *testing.T) {
	_, step := newBiasMonitor(time.Hour)
	start := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	m := 0
	next := func(spot, flip float64) strategy.GammaDecision {
		d := step(start.Add(time.Duration(m)*time.Minute), spot, flip)
		m++
		return d
	}
	for m <= 60 {
		if d := next(83500, 83500); !d.BiasWarmupUntil.IsZero() != (m <= 60) {
			t.Fatalf("minute %d: warm-up until %v", m-1, d.BiasWarmupUntil)
		}
	}
	d1 := next(80500, 83500)
	d2 := next(80500, 83500)
	if d1.Shed != strategy.GammaActionNone || d2.Shed != strategy.GammaActionClosePuts || d2.WarmupHeld != strategy.GammaActionNone {
		t.Errorf("after warm-up: shed %v then %v (held %v), want puts on the 2nd snapshot", d1.Shed, d2.Shed, d2.WarmupHeld)
	}
}

// A GEX gap longer than the window empties it like a restart.
func TestGammaMonitor_BiasWarmupRestartsAfterGap(t *testing.T) {
	_, step := newBiasMonitor(time.Hour)
	start := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	for m := 0; m <= 61; m++ {
		step(start.Add(time.Duration(m)*time.Minute), 83500, 83500)
	}
	resume := start.Add(4 * time.Hour)
	if d := step(resume, 83500, 83500); !d.BiasWarmupUntil.Equal(resume.Add(time.Hour)) {
		t.Errorf("after a 3 h gap: warm-up until %v, want %v", d.BiasWarmupUntil, resume.Add(time.Hour))
	}
}

// The warm-up guards the median, which only the bias uses.
func TestGammaMonitor_NoBiasWarmupWithoutWindowOrUnderSMA(t *testing.T) {
	at := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	g, step := newBiasMonitor(0)
	step(at, 81938, 79500)
	if d := step(at.Add(time.Minute), 81938, 79500); d.Shed != strategy.GammaActionCloseCalls || !d.BiasWarmupUntil.IsZero() {
		t.Errorf("no window: shed %v warm-up %v, want calls at once", d.Shed, d.BiasWarmupUntil)
	}
	g, step = newBiasMonitor(time.Hour)
	g.SetDirection(strategy.DirectionSMA)
	if d := step(at, 81938, 79500); !d.BiasWarmupUntil.IsZero() {
		t.Errorf("sma: warm-up %v", d.BiasWarmupUntil)
	}
}

// biasWarmupFixture: an open strangle under gex_direction bias with the
// bot's 60-minute flip window and a 3.0 % buffer (the fixture's DVOL is 55).
func biasWarmupFixture(t *testing.T) (*strategyFixture, func(at time.Time, spot, flip float64)) {
	f := newStrategyFixture(t)
	f.cfg.GEXDirection = "bias"
	f.cfg.GEXShedConfirmSnapshots = 2
	f.cfg.GEXBiasFlipWindowMinutes = 60
	f.cfg.GammaFlipBufferSD = bufferSDFor(3, 55)
	f.withOpenStrangle(0.1, 0.02)
	f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill { return orders.Fill{Qty: o.Qty, FillPrice: 0.02} }
	g := &switchGEX{}
	f.gex = g
	return f, func(at time.Time, spot, flip float64) {
		g.set(&gex.Snapshot{ComputedAt: at, Regime: btcNeg, Spot: spot, GammaFlip: flip, GammaFlipFound: true})
		time.Sleep(60 * time.Millisecond) // a few 10 ms cycles see it
	}
}

// Scenario: flip jump right after a restart.
func TestStrategy_BiasWarmup_FlipJumpRightAfterRestart(t *testing.T) {
	f, set := biasWarmupFixture(t)
	start := time.Now()
	f.gex.(*switchGEX).set(&gex.Snapshot{ComputedAt: start, Regime: btcNeg, Spot: 81900, GammaFlip: 83595, GammaFlipFound: true})
	f.startRun()
	time.Sleep(60 * time.Millisecond)
	for m := 1; m <= 10; m++ { // ten minutes after the restart the flip jumps
		set(start.Add(time.Duration(m)*time.Minute), 81938, 79500)
	}
	eventually(t, 2*time.Second, "bias_warmup journaled", func() bool { return f.skips(strategy.SkipBiasWarmup) == 1 })
	if n := len(f.exch.buys()); n != 0 {
		t.Errorf("no leg may be shed during the warm-up: %d buys", n)
	}
	if f.position(f.call) == nil || f.position(f.put) == nil {
		t.Error("both legs must stay open")
	}
	for _, e := range f.journal.events(orders.EventSkipped) {
		if strings.HasPrefix(e.reason, strategy.SkipBiasWarmup) && !strings.Contains(e.reason, "would shed 1 call leg") {
			t.Errorf("skip detail %q", e.reason)
		}
	}
}

// Scenario: the guard after warm-up is unchanged.
func TestStrategy_BiasWarmup_GuardAfterWarmupUnchanged(t *testing.T) {
	f, set := biasWarmupFixture(t)
	start := time.Now()
	f.gex.(*switchGEX).set(&gex.Snapshot{ComputedAt: start, Regime: btcNeg, Spot: 83500, GammaFlip: 83500, GammaFlipFound: true})
	f.startRun()
	time.Sleep(60 * time.Millisecond)
	for _, m := range []int{20, 40, 60} { // the window fills at 83,500
		set(start.Add(time.Duration(m)*time.Minute), 83500, 83500)
	}
	set(start.Add(61*time.Minute), 80500, 83500) // bias down on the live and the median flip
	if n := len(f.exch.buys()); n != 0 {
		t.Fatalf("one snapshot must not shed: %d buys", n)
	}
	set(start.Add(62*time.Minute), 80500, 83500)
	eventually(t, 2*time.Second, "puts shed", func() bool { return f.position(f.put) == nil })
	if f.position(f.call) == nil {
		t.Error("the call must be kept on bias down")
	}
	if n := f.skips(strategy.SkipBiasWarmup); n != 0 {
		t.Errorf("bias_warmup journaled %d times after the warm-up", n)
	}
}
