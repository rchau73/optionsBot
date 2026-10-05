package tests

import (
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"optionsbot/internal/gex"
	"optionsbot/internal/orders"
)

// switchGEX is a GEX source the test can change mid-run.
type switchGEX struct {
	mu   sync.Mutex
	snap *gex.Snapshot
}

func (g *switchGEX) Snapshot() *gex.Snapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.snap
}

func (g *switchGEX) set(s *gex.Snapshot) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.snap = s
}

// startWithRestingEntryThenShedPuts opens a strangle whose orders rest on the
// book (positive regime: no shed), then turns the regime to a confirmed put
// shed. beforeShed runs while both orders are resting.
func startWithRestingEntryThenShedPuts(t *testing.T, beforeShed func(f *strategyFixture)) *strategyFixture {
	f := newStrategyFixture(t)
	f.withPutSheddingRegime()                 // bear trend from the daily closes
	f.withConfirmedRegime("POSITIVE/PINNING") // entries allowed
	g := &switchGEX{snap: &gex.Snapshot{Regime: "POSITIVE/PINNING", Spot: 100000, GammaFlip: 95000, GammaFlipFound: true}}
	f.gex = g
	f.startRun()
	eventually(t, 2*time.Second, "both entry orders resting", func() bool { return len(f.exch.sells()) == 2 })
	if beforeShed != nil {
		beforeShed(f)
	}
	g.set(&gex.Snapshot{Regime: "NEGATIVE/ACCELERATION", Spot: 100000, GammaFlip: 120000, GammaFlipFound: true})
	return f
}

func (f *fakeExchange) wasCancelled(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.cancelled, id)
}

// A put order resting when a put shed starts is cancelled, never filled and
// bought back. The whole entry is cancelled; the call side is not re-entered
// here because the regime change freezes new risk until it is confirmed
// (the margin policy), and no put is sold while puts are shed.
func TestStrategy_ShedCancelsWorkingOrdersOfThatSide(t *testing.T) {
	f := startWithRestingEntryThenShedPuts(t, nil)
	putID := f.exch.orderIDFor(f.put, 0)

	eventually(t, 2*time.Second, "put order cancelled by the shed", func() bool { return f.exch.wasCancelled(putID) })
	eventually(t, 2*time.Second, "entries frozen by the regime change", func() bool {
		for _, e := range f.journal.events(orders.EventSkipped) {
			if strings.HasPrefix(e.reason, "risk_frozen") {
				return true
			}
		}
		return false
	})
	time.Sleep(100 * time.Millisecond)
	if n := len(f.exch.buys()); n != 0 {
		t.Errorf("nothing filled, so nothing is bought back: %d buys", n)
	}
	puts := 0
	for _, o := range f.exch.sells() {
		if o.Instrument == f.put {
			puts++
		}
	}
	if puts != 1 {
		t.Errorf("no new put may be sold during the put shed: %d put sells", puts)
	}
	cancels := 0
	for _, e := range f.journal.events(orders.EventCancelled) {
		if e.instrument == f.put && e.trigger == orders.TriggerGammaClose {
			cancels++
		}
	}
	if cancels != 1 {
		t.Errorf("the put cancel is journaled once as gamma_close: %d", cancels)
	}
}

// A put that filled partly before the cancel landed is booked, then shed
// with the rest of that side — never left untracked.
func TestStrategy_ShedBooksAPartialFillThenClosesIt(t *testing.T) {
	f := startWithRestingEntryThenShedPuts(t, func(f *strategyFixture) {
		f.exch.partial(f.exch.orderIDFor(f.put, 0), 0.05, 0.021)
	})
	eventually(t, 2*time.Second, "partial put bought back by the shed", func() bool {
		for _, b := range f.exch.buys() {
			if b.Instrument == f.put && b.TriggerReason == orders.TriggerGammaClose && math.Abs(b.Qty-0.05) < 1e-9 {
				return true
			}
		}
		return false
	})
}
