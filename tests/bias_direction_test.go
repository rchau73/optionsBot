package tests

import (
	"strings"
	"sync"
	"testing"
	"time"

	"optionsbot/internal/gex"
	"optionsbot/internal/history"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// 2026-10-12: the side to avoid in a negative regime comes from the GEX bias
// (spot vs flip ± buffer, the reference script's ACCELERATION BIAS) instead
// of the SMA trend, under gex_direction: bias. A leg is shed when the bias
// runs toward it and re-sold once a cooldown has passed and the bias has
// left its side. The SMA trend is still computed and recorded.

func TestBiasDir(t *testing.T) {
	cases := []struct {
		regime string
		found  bool
		spot   float64
		want   int
	}{
		{"NEGATIVE/ACCELERATION", true, 103000, 1},  // above flip + 2 %
		{"NEGATIVE/ACCELERATION", true, 97000, -1},  // below flip − 2 %
		{"NEGATIVE/ACCELERATION", true, 101000, 0},  // inside the buffer: noise
		{"NEGATIVE/ACCELERATION", true, 99000, 0},   // inside the buffer
		{"POSITIVE/PINNING", true, 110000, 0},       // positive regime: neutral
		{"NEGATIVE/ACCELERATION", false, 110000, 0}, // no flip
	}
	for _, c := range cases {
		if got := strategy.BiasDir(c.regime, c.found, c.spot, 100000, 2); got != c.want {
			t.Errorf("%s spot %.0f: bias %d, want %d", c.regime, c.spot, got, c.want)
		}
	}
	if strategy.ResolveBiasAction(1) != strategy.GammaActionCloseCalls || strategy.ResolveBiasAction(-1) != strategy.GammaActionClosePuts ||
		strategy.ResolveBiasAction(0) != strategy.GammaActionNone {
		t.Error("bias up sheds calls, bias down sheds puts, neutral sheds nothing")
	}
}

func TestBiasRepairBlockReason(t *testing.T) {
	shed := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	a := &strategy.ShedAnchor{At: shed}
	snap := func(m int) time.Time { return shed.Add(time.Duration(m) * time.Minute) }

	a.ObserveBias(true, snap(1))
	a.ObserveBias(true, snap(1)) // the same snapshot counts once
	a.ObserveBias(true, snap(2))
	if r := strategy.BiasRepairBlockReason(a, "call", shed.Add(3*time.Hour), 4*time.Hour, 2); !strings.Contains(r, "cooldown until Oct 12 04:00 UTC") {
		t.Errorf("inside the cooldown: %q", r)
	}
	if r := strategy.BiasRepairBlockReason(a, "call", shed.Add(5*time.Hour), 4*time.Hour, 2); r != "" {
		t.Errorf("cooldown over, bias clear on 2 snapshots: held %q", r)
	}
	a.ObserveBias(false, snap(3)) // the bias came back to the side: start over
	if r := strategy.BiasRepairBlockReason(a, "call", shed.Add(5*time.Hour), 4*time.Hour, 2); !strings.Contains(r, "bias to leave up") {
		t.Errorf("bias back on the side: %q", r)
	}
}

func TestConfig_GEXDirection(t *testing.T) {
	cfg, err := loadWithDummyCreds(t, writeTempConfig(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GEXDirection != "sma" {
		t.Errorf("default gex_direction = %q, want sma", cfg.GEXDirection)
	}
	cfg.GEXDirection = "trend"
	if err := cfg.Validate(); err == nil {
		t.Error("gex_direction must be sma or bias")
	}
	cfg.GEXDirection, cfg.GEXBiasRepairCooldownHours = "bias", 200
	if err := cfg.Validate(); err == nil {
		t.Error("gex_bias_repair_cooldown_hours above 168 must be rejected")
	}
}

// Spot inside the flip's buffer in a confirmed negative regime with a bear
// SMA trend (BTC on 2026-10-12): the bias is neutral, so the put is repaired
// — the SMA trend held it.
func TestStrategy_BiasNeutralRepairsWhatSMAHeld(t *testing.T) {
	for _, dir := range []string{"sma", "bias"} {
		t.Run(dir, func(t *testing.T) {
			f := newStrategyFixture(t)
			f.cfg.GEXDirection = dir
			f.withLoneCall(0.1)
			f.withBearTrendAboveFlip()                                                                                              // SMA bear, confirmed negative regime
			f.gex = fixedGEX{&gex.Snapshot{Regime: "NEGATIVE/ACCELERATION", Spot: 100000, GammaFlip: 100000, GammaFlipFound: true}} // bias neutral
			f.startRun()

			if dir == "sma" {
				eventually(t, 2*time.Second, "held by the SMA trend", func() bool {
					return f.skips(strategy.SkipRepairHeld+": put confirmed negative gamma regime in a bear trend") > 0
				})
				if n := putSells(f); n != 0 {
					t.Errorf("sma: put sold %d times", n)
				}
				return
			}
			eventually(t, 2*time.Second, "put repaired under the neutral bias", func() bool { return putSells(f) == 1 })
		})
	}
}

// Bias up in a negative regime (a rally through the flip): calls are shed on
// the second snapshot, puts kept. The call is re-sold only after the
// cooldown, once the bias has left "up" on two snapshots.
func TestStrategy_BiasUpShedsCallsAndRepairsAfterCooldown(t *testing.T) {
	for _, cooldown := range []int{0, 24} {
		t.Run(map[int]string{0: "no cooldown", 24: "24h cooldown"}[cooldown], func(t *testing.T) {
			f := newStrategyFixture(t)
			f.cfg.GEXDirection = "bias"
			f.cfg.GEXBiasRepairCooldownHours = cooldown
			f.cfg.GEXShedConfirmSnapshots = 2
			f.cfg.GEXRepairConfirmSnapshots = 2
			f.withOpenStrangle(0.1, 0.02)
			f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill { return orders.Fill{Qty: o.Qty, FillPrice: 0.02} }
			at := time.Now()
			g := &switchGEX{}
			next := func(flip float64) {
				at = at.Add(time.Minute)
				g.set(&gex.Snapshot{ComputedAt: at, Regime: "NEGATIVE/ACCELERATION", Spot: 100000, GammaFlip: flip, GammaFlipFound: true})
			}
			next(90000) // spot above the flip: bias up
			f.gex = g
			f.startRun()
			time.Sleep(100 * time.Millisecond)
			if n := len(f.exch.buys()); n != 0 {
				t.Fatalf("one snapshot must not shed: %d buys", n)
			}
			next(90000)
			eventually(t, 2*time.Second, "call shed", func() bool { return f.position(f.call) == nil })
			if f.position(f.put) == nil {
				t.Fatal("the put must be kept on bias up")
			}

			next(100000) // the rally stalls at the flip: bias neutral
			time.Sleep(60 * time.Millisecond)
			next(100000)
			if cooldown == 0 {
				eventually(t, 2*time.Second, "call re-sold", func() bool { return callSells(f) == 1 })
				return
			}
			eventually(t, 2*time.Second, "held by the cooldown", func() bool {
				h := f.strat.View().HeldLegs
				return len(h) == 1 && h[0].Rule == strategy.HoldGEXShed && strings.Contains(h[0].Reason, "cooldown until") &&
					h[0].Anchor != nil && h[0].Anchor.Mode == "bias" && h[0].Anchor.Clear >= 1
			})
			if n := callSells(f); n != 0 {
				t.Errorf("re-sold inside the cooldown: %d", n)
			}
		})
	}
}

type recordedDirections struct {
	mu   sync.Mutex
	recs []history.DirectionRecord
}

func (r *recordedDirections) Record(d history.DirectionRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recs = append(r.recs, d)
}

func (r *recordedDirections) all() []history.DirectionRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]history.DirectionRecord(nil), r.recs...)
}

// Both sources are recorded on change, with what the unused one would shed.
func TestStrategy_DirectionHistoryRecordsSMAAndBias(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.GEXDirection = "bias"
	rec := &recordedDirections{}
	f.dirs = rec
	f.withPutSheddingRegime() // SMA bear; spot 100000 under the flip 120000
	f.gex = fixedGEX{&gex.Snapshot{ComputedAt: time.Now(), Regime: "NEGATIVE/ACCELERATION", Spot: 100000, GammaFlip: 120000, GammaFlipFound: true}}
	f.startRun()

	eventually(t, 2*time.Second, "direction recorded", func() bool { return len(rec.all()) > 0 })
	time.Sleep(50 * time.Millisecond)
	recs := rec.all()
	if len(recs) != 1 {
		t.Errorf("a state is recorded once, not per cycle: %d records", len(recs))
	}
	r := recs[0]
	if r.Direction != "bias" || r.SMATrend != "bear" || r.Bias != "down" || r.Action != "close_puts" || r.OtherAction != "close_puts" || r.Flip != 120000 {
		t.Errorf("record = %+v", r)
	}
	v := f.strat.View()
	if v.Direction != "bias" || v.SMATrend != "bear" || v.Bias != "down" || v.Trend != "bear" {
		t.Errorf("view direction %q sma %q bias %q trend %q", v.Direction, v.SMATrend, v.Bias, v.Trend)
	}
}

func callSells(f *strategyFixture) int {
	n := 0
	for _, o := range f.exch.sells() {
		if o.Instrument == f.call {
			n++
		}
	}
	return n
}
