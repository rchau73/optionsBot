package tests

import (
	"context"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// Slots 25/45/60 are meant to sit on separate dates, but Deribit lists
// weeklies only a few weeks out, then month- and quarter-ends. On 2026-10-05
// the 45-day window (35–55) and the 60-day window (50–70) both picked
// Nov 27, so two strangles shared one expiry and would roll together. A slot
// whose expiry another slot holds now takes the nearest free one up to
// target × expiry_stretch, or waits.

// deribitCalendar is BTC's listed expiries on 2026-10-05 (mainnet = testnet).
func deribitCalendar() (time.Time, []*marketdata.Instrument) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	var insts []*marketdata.Instrument
	for _, d := range []string{"2026-10-09", "2026-10-16", "2026-10-23", "2026-10-30", "2026-11-27", "2026-12-25", "2027-03-26", "2027-06-25"} {
		exp, _ := time.Parse("2006-01-02", d)
		insts = append(insts, &marketdata.Instrument{Name: "BTC-" + d, Expiry: exp.Add(8 * time.Hour)})
	}
	return now, insts
}

func day(t time.Time) string { return t.Format("Jan 2") }

func TestSelectSlotExpiry_SlotsSpreadOverDeribitsCalendar(t *testing.T) {
	now, insts := deribitCalendar()
	held := map[time.Time]bool{}
	want := map[int]string{25: "Oct 23", 45: "Nov 27", 60: "Dec 25"}
	picks := map[int]marketdata.ExpiryPick{}
	for _, dte := range []int{25, 45, 60} {
		exp, pick := strategy.SelectSlotExpiry(insts, now, dte, 10, 15, 1.5, held)
		if day(exp) != want[dte] {
			t.Errorf("slot %d: %s, want %s", dte, day(exp), want[dte])
		}
		picks[dte] = pick
		held[exp] = true
	}
	if picks[45] != marketdata.PickWindow || picks[60] != marketdata.PickStretched {
		t.Errorf("45 uses its own window, 60 is stretched past Nov 27 (held): %v", picks)
	}
}

func TestSelectSlotExpiry_WaitsRatherThanShare(t *testing.T) {
	now, insts := deribitCalendar()
	nov27 := time.Date(2026, 11, 27, 8, 0, 0, 0, time.UTC)
	held := map[time.Time]bool{nov27: true}

	// No stretch: the only expiry in 50–70 days is held → wait.
	if _, pick := strategy.SelectSlotExpiry(insts, now, 60, 10, 15, 1, held); pick != marketdata.PickAllHeld {
		t.Errorf("stretch 1: want wait, got %v", pick)
	}
	// Stretch 1.5 but Dec 25 held too: nothing free up to 90 days → wait.
	held[time.Date(2026, 12, 25, 8, 0, 0, 0, time.UTC)] = true
	if _, pick := strategy.SelectSlotExpiry(insts, now, 60, 10, 15, 1.5, held); pick != marketdata.PickAllHeld {
		t.Errorf("all held up to 90 days: want wait, got %v", pick)
	}
	// An expiry held elsewhere (outside the window) does not matter.
	if exp, pick := strategy.SelectSlotExpiry(insts, now, 25, 10, 15, 1.5, held); pick != marketdata.PickWindow || day(exp) != "Oct 23" {
		t.Errorf("slot 25 unaffected: %s %v", day(exp), pick)
	}
}

func TestSelectSlotExpiry_NoListedExpiryInWindowStaysNone(t *testing.T) {
	// Oct 8: nothing in the 60-day window (50–70). The stretch is for
	// collisions only — an empty window still waits, as before.
	_, insts := deribitCalendar()
	oct8 := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	if _, pick := strategy.SelectSlotExpiry(insts, oct8, 60, 10, 15, 1.5, nil); pick != marketdata.PickNone {
		t.Errorf("want none, got %v", pick)
	}
}

func TestStretchHi(t *testing.T) {
	for _, c := range []struct {
		dte, dev int
		stretch  float64
		want     int
	}{{60, 10, 1.5, 90}, {25, 10, 1.5, 37}, {25, 10, 1, 35}, {45, 10, 1.2, 55}} {
		if got := marketdata.StretchHi(c.dte, c.dev, c.stretch); got != c.want {
			t.Errorf("StretchHi(%d, %d, %.1f) = %d, want %d", c.dte, c.dev, c.stretch, got, c.want)
		}
	}
}

// Run level: a second slot whose only expiry is held by the first slot's
// strangle waits (journaled), instead of stacking on the same date.
func TestStrategy_SlotWaitsWhenItsExpiryIsHeld(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.ExpiryStretch = 1.5
	f.cfg.DTEDeltaMatrix = []config.DTEDeltaEntry{{DTE: 45, Deltas: []float64{0.16}}, {DTE: 50, Deltas: []float64{0.18}}}
	f.withOpenStrangle(0.1, 0.02) // on the fixture's only expiry, matched to a slot
	f.startRun()

	eventually(t, 2*time.Second, "vacant slot skipped: no free expiry", func() bool {
		for _, e := range f.journal.events(orders.EventSkipped) {
			if strings.HasPrefix(e.reason, strategy.SkipNoFreeExpiry) {
				return true
			}
		}
		return false
	})
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("no entry may stack on the held expiry: %d sells", n)
	}
}

// A pending entry holds its expiry too: two vacant slots wanting the same
// expiry open one strangle, not two.
func TestStrategy_PendingEntryHoldsItsExpiry(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.ExpiryStretch = 1.5
	f.cfg.DTEDeltaMatrix = []config.DTEDeltaEntry{{DTE: 45, Deltas: []float64{0.16, 0.18}}}
	f.startRun()

	eventually(t, 2*time.Second, "first slot entered", func() bool { return len(f.exch.sells()) == 2 })
	time.Sleep(100 * time.Millisecond) // many cycles
	if n := len(f.exch.sells()); n != 2 {
		t.Errorf("the second slot must not stack on the pending entry's expiry: %d sells", n)
	}
}

func TestMarketData_SubscribesTheStretch(t *testing.T) {
	for _, c := range []struct {
		stretch float64
		want37  bool
	}{{1.5, true}, {1, false}} {
		gw := newFakeFeedGateway()
		gw.results["public/get_instruments"] = instrumentsJSON(time.Now(), 25, 37, 45, 115)
		cfg := newMarketCfg()
		cfg.DTEDeltaMatrix = []config.DTEDeltaEntry{{DTE: 25, Deltas: []float64{0.16}}, {DTE: 120, Deltas: []float64{0.16}}}
		cfg.ExpiryStretch = c.stretch
		ctx, cancel := context.WithCancel(context.Background())
		m := marketdata.New(cfg, gw)
		if err := m.Start(ctx); err != nil {
			t.Fatal(err)
		}
		subs := strings.Join(gw.subscriptions(), " ")
		cancel()
		// 25 is slot 25's own pick; 115 is slot 120's; 37 (36 DTE) lies in
		// slot 25's stretch (up to 37 days at 1.5); 45 is in no slot's range.
		if !strings.Contains(subs, "BTC-D25-C") || !strings.Contains(subs, "BTC-D115-C") {
			t.Errorf("stretch %.1f: own picks must be subscribed: %s", c.stretch, subs)
		}
		if got := strings.Contains(subs, "BTC-D37-C"); got != c.want37 {
			t.Errorf("stretch %.1f: 36 DTE subscribed = %v, want %v", c.stretch, got, c.want37)
		}
		if strings.Contains(subs, "BTC-D45-") {
			t.Errorf("stretch %.1f: an expiry outside every slot's range must not be subscribed", c.stretch)
		}
	}
}

// withSecondExpiry lists a second expiry a week after the fixture's, so two
// slots of the same DTE can sit on separate dates.
func (f *strategyFixture) withSecondExpiry() (call, put string) {
	exp := f.expiry.AddDate(0, 0, 7)
	label := strings.ToUpper(exp.Format("2Jan06"))
	call, put = "BTC-"+label+"-110000-C", "BTC-"+label+"-90000-P"
	f.market.mu.Lock()
	defer f.market.mu.Unlock()
	for name, src := range map[string]string{call: f.call, put: f.put} {
		cp := *f.market.instruments[src]
		cp.Name, cp.Expiry, cp.UpdatedAt = name, exp, time.Now()
		f.market.instruments[name] = &cp
	}
	return call, put
}
