package tests

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/gex"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// Shed anchors lived only in memory: on 2026-10-14 the bias shed BTC's put
// at 11:45, a deploy restarted the bot at 12:05 and repair re-sold the put
// at 12:14, 24 h early. They are rebuilt from the journal's closes, like
// stop records.

// Through the real journal: a GEX shed and a stop-loss are written, replayed
// and read back as a shed anchor and a stop record. The stop-loss case is
// the 2026-10-14 finding: the journal writes close_reason "stop_loss", the
// restore looked for "stop_loss_200pct", so a stop was never restored.
func TestJournal_ShedAndStopSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orders.log")
	l, err := orders.NewLogger(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	shedAt := time.Now().Add(-time.Hour)
	put := &orders.Position{Instrument: "BTC-27NOV26-73000-P", OptionType: "put", Qty: 1.5, PremiumReceived: 0.02, EntryTime: shedAt.Add(-time.Hour)}
	l.LogClose(put, orders.Fill{FillPrice: 0.01}, orders.TriggerGammaClose, orders.TypeMarket, orders.EventContext{
		Detail: "GEX shed (bias down)",
		Market: orders.MarketSnapshot{Spot: 81743, DVOL: 38, GammaFlip: 83595, FlipBufferPct: 2.0},
	})
	call := &orders.Position{Instrument: "BTC-27NOV26-96000-C", OptionType: "call", Qty: 1.5, PremiumReceived: 0.02, EntryTime: shedAt.Add(-time.Hour)}
	l.LogClose(call, orders.Fill{FillPrice: 0.05}, orders.TriggerStopLoss200Pct, orders.TypeMarket, orders.EventContext{Detail: "stop-loss"})
	l.Close()

	r, err := orders.ReplayFile(path, 10)
	if err != nil || len(r.Trades) != 2 {
		t.Fatalf("replay: %v %+v", err, r.Trades)
	}
	nov27 := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	sheds := strategy.ShedsFromTrades(r.Trades)
	s, ok := sheds[strategy.StopSide{Expiry: nov27, OptionType: "put"}]
	if !ok || len(sheds) != 1 {
		t.Fatalf("sheds = %+v, want the put", sheds)
	}
	if s.Flip != 83595 || s.BufferPct != 2.0 || s.DVOL != 38 || math.Abs(s.At.Sub(r.Trades[0].At).Seconds()) > 1 {
		t.Errorf("shed = %+v, want flip 83595, buffer 2.0, DVOL 38 at the close", s)
	}
	stops := strategy.StopsFromTrades(r.Trades)
	if _, ok := stops[strategy.StopSide{Expiry: nov27, OptionType: "call"}]; !ok || len(stops) != 1 {
		t.Errorf("stops = %+v, want the stopped-out call (close_reason %q)", stops, r.Trades[1].Reason)
	}
}

func TestShedsFromTrades(t *testing.T) {
	t0 := time.Date(2026, 10, 14, 14, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return t0.Add(time.Duration(h) * time.Hour) }
	shed := orders.CloseReasonLabel(orders.TriggerGammaClose)
	trades := []orders.Trade{
		{Kind: "close", Reason: shed, Instrument: "BTC-27NOV26-73000-P", At: at(0), GammaFlip: 83595, FlipBufferPct: 2},
		{Kind: "close", Reason: orders.TriggerRebalanceLegs, Instrument: "BTC-27NOV26-72000-P", At: at(1)}, // partial trim: kept
		{Kind: "close", Reason: shed, Instrument: "BTC-27NOV26-96000-C", At: at(2), GammaFlip: 90000},
		{Kind: "open", Reason: orders.TriggerRepair, Instrument: "BTC-27NOV26-97000-C", At: at(30)}, // re-sold by repair: released
		{Kind: "close", Reason: orders.TriggerGammaClose, Instrument: "BTC-25DEC26-70000-P", At: at(3), GammaFlip: 80000},
		{Kind: "close", Reason: "stop_loss", Instrument: "BTC-25DEC26-69000-P", At: at(4)}, // a later stop replaces it
		{Kind: "close", Reason: shed, Instrument: "not-an-option", At: at(5)},
	}
	got := strategy.ShedsFromTrades(trades)
	want := strategy.StopSide{Expiry: time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC), OptionType: "put"}
	if len(got) != 1 || got[want].Flip != 83595 || got[want].BufferPct != 2 || !got[want].At.Equal(at(0)) {
		t.Errorf("got %+v, want only the Nov 27 put shed at flip 83595", got)
	}
}

// Under bias, a put shed an hour before the restart stays held by the 24 h
// cooldown; without the restored anchor the neutral bias re-sells it at once
// (TestStrategy_BiasNeutralRepairsWhatSMAHeld).
func TestStrategy_RestoredBiasShedHoldsRepair(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.GEXDirection = "bias"
	f.cfg.GEXBiasRepairCooldownHours = 24
	f.withLoneCall(0.1)
	f.withBearTrendAboveFlip()
	f.gex = fixedGEX{&gex.Snapshot{ComputedAt: time.Now(), Regime: "NEGATIVE/ACCELERATION", Spot: 100000, GammaFlip: 100000, GammaFlipFound: true}} // bias neutral
	f.beforeRun = func(s *strategy.Strategy) {
		s.RestoreStops([]orders.Trade{{Kind: "close", Reason: orders.CloseReasonLabel(orders.TriggerGammaClose), Instrument: f.put,
			At: time.Now().Add(-time.Hour), GammaFlip: 103000, FlipBufferPct: 2}})
	}
	f.startRun()

	eventually(t, 2*time.Second, "held by the restored anchor's cooldown", func() bool {
		h := heldLegs(f)
		return len(h) == 1 && h[0].Rule == strategy.HoldGEXShed && strings.Contains(h[0].Reason, "cooldown until") &&
			h[0].Anchor != nil && h[0].Anchor.Mode == "bias"
	})
	time.Sleep(100 * time.Millisecond)
	if n := putSells(f); n != 0 {
		t.Errorf("the shed put must stay held across the restart: %d sells", n)
	}
	if h := heldLegs(f)[0]; !strings.Contains(h.Reason, time.Now().Add(23*time.Hour).UTC().Format("Jan 2")) {
		t.Errorf("the cooldown runs from the shed an hour ago: %q", h.Reason)
	}
}

// A restored bias anchor past its cooldown releases once the bias has left
// the shed side on two snapshots, as it would have without the restart.
func TestStrategy_RestoredBiasShedPastCooldownRepairs(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.GEXDirection = "bias"
	f.cfg.GEXBiasRepairCooldownHours = 24
	f.cfg.GEXRepairConfirmSnapshots = 2
	f.withLoneCall(0.1)
	f.withBearTrendAboveFlip()
	at := time.Now()
	g := &switchGEX{}
	next := func() {
		at = at.Add(time.Minute)
		g.set(&gex.Snapshot{ComputedAt: at, Regime: "NEGATIVE/ACCELERATION", Spot: 100000, GammaFlip: 100000, GammaFlipFound: true})
	}
	next()
	f.gex = g
	f.beforeRun = func(s *strategy.Strategy) {
		s.RestoreStops([]orders.Trade{{Kind: "close", Reason: orders.CloseReasonLabel(orders.TriggerGammaClose), Instrument: f.put,
			At: time.Now().Add(-30 * time.Hour), GammaFlip: 103000, FlipBufferPct: 2}})
	}
	f.startRun()
	time.Sleep(100 * time.Millisecond)
	next()
	eventually(t, 2*time.Second, "put repaired after the cooldown", func() bool { return putSells(f) == 1 })
}

// Under the SMA direction the restored anchor holds the put until spot is
// back above the shed flip + buffer — not because the live flip is now under
// spot. A record journaled before the buffer was (2026-10-14) rebuilds it
// from the DVOL at the shed and today's buffer setting.
func TestStrategy_RestoredSMAShedHoldsAgainstTheShedFlip(t *testing.T) {
	for _, tc := range []struct {
		name      string
		bufferPct float64
		wantLevel float64
	}{
		{"buffer journaled", 2, 120000 * 1.02},
		{"older record: buffer from the DVOL", 0, 120000 * (1 + 1.5*55/math.Sqrt(365)/100)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStrategyFixture(t)
			f.cfg.GammaFlipBufferSD = 1.5
			f.cfg.GEXRepairConfirmSnapshots = 2
			f.cfg.GEXRepairReleaseHours = 24
			f.withLoneCall(0.1)
			f.gex = fixedGEX{&gex.Snapshot{ComputedAt: time.Now(), Regime: "POSITIVE", Spot: 100000, GammaFlip: 90000, GammaFlipFound: true}}
			f.beforeRun = func(s *strategy.Strategy) {
				s.RestoreStops([]orders.Trade{{Kind: "close", Reason: orders.CloseReasonLabel(orders.TriggerGammaClose), Instrument: f.put,
					At: time.Now().Add(-time.Hour), GammaFlip: 120000, FlipBufferPct: tc.bufferPct, DVOL: 55}})
			}
			f.startRun()

			eventually(t, 2*time.Second, "held by the restored anchor", func() bool {
				h := heldLegs(f)
				return len(h) == 1 && h[0].Rule == strategy.HoldGEXShed && h[0].Anchor != nil && h[0].Anchor.Flip == 120000
			})
			if lvl := heldLegs(f)[0].Anchor.Level; math.Abs(lvl-tc.wantLevel) > 1 {
				t.Errorf("anchor level %.0f, want %.0f", lvl, tc.wantLevel)
			}
			time.Sleep(100 * time.Millisecond)
			if n := putSells(f); n != 0 {
				t.Errorf("re-sold with spot under the shed flip: %d sells", n)
			}
		})
	}
}

// The journal records the buffer with the flip, so the next restart has it.
func TestMarketSnapshot_RecordsFlipBuffer(t *testing.T) {
	snap := strategy.BuildMarketSnapshot(strategy.SnapshotInput{
		Now: time.Now(), Spot: 81743, FlipBufferPct: 3.01,
		GEX: &gex.Snapshot{Regime: "NEGATIVE/ACCELERATION", GammaFlip: 83595, GammaFlipFound: true},
	})
	if snap.GammaFlip != 83595 || snap.FlipBufferPct != 3.01 {
		t.Errorf("snapshot = flip %v buffer %v, want 83595 and 3.01", snap.GammaFlip, snap.FlipBufferPct)
	}
	if none := strategy.BuildMarketSnapshot(strategy.SnapshotInput{Now: time.Now(), FlipBufferPct: 3.01}); none.FlipBufferPct != 0 {
		t.Errorf("no flip, no buffer: %v", none.FlipBufferPct)
	}
}
