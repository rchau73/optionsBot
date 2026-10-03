package tests

import (
	"math"
	"sync"
	"testing"

	"optionsbot/internal/orders"
)

func shortLeg(id, optType string, qty, delta float64) *orders.Position {
	return &orders.Position{
		ID: id, Instrument: "BTC-X-" + id, OptionType: optType, Qty: qty,
		PremiumReceived: 0.02 * qty,
		CurrentGreeks:   orders.Greeks{Delta: delta, Gamma: 0.0001, Vega: 10, Theta: -5},
	}
}

func TestStateManager_ReadersGetSnapshots(t *testing.T) {
	sm := orders.NewStateManager()
	sm.AddPosition(shortLeg("p1", "call", 0.1, 0.16))

	snap, _ := sm.GetPosition("p1")
	snap.Qty = 99 // mutating a snapshot must not touch the book

	got, _ := sm.GetPosition("p1")
	if got.Qty != 0.1 {
		t.Errorf("book changed through a snapshot: qty = %v", got.Qty)
	}
	sm.UpdatePositionMid("p1", 0.05, orders.Greeks{Delta: 0.3})
	if snap.CurrentMid == 0.05 {
		t.Error("a snapshot taken earlier must not see later updates")
	}
}

func TestStateManager_StrangleLegsFollowPositionUpdates(t *testing.T) {
	sm := orders.NewStateManager()
	call, put := shortLeg("c", "call", 0.2, 0.16), shortLeg("p", "put", 0.2, -0.16)
	sm.AddPosition(call)
	sm.AddPosition(put)
	sm.AddStrangle(&orders.Strangle{ID: "st", CallLeg: call, PutLeg: put})

	sm.UpdatePositionQty("c", 0.1, 0.002)
	st := sm.AllStrangles()[0]
	if st.CallLeg.Qty != 0.1 {
		t.Errorf("strangle leg should reflect the updated position, got qty %v", st.CallLeg.Qty)
	}
}

func TestStateManager_RemoveStrangleOnlyWhenBothLegsGone(t *testing.T) {
	sm := orders.NewStateManager()
	call, put := shortLeg("c", "call", 0.1, 0.16), shortLeg("p", "put", 0.1, -0.16)
	sm.AddPosition(call)
	sm.AddPosition(put)
	sm.AddStrangle(&orders.Strangle{ID: "st", CallLeg: call, PutLeg: put})

	sm.RemovePosition("c")
	sm.RemoveStrangleContaining("c")
	if len(sm.AllStrangles()) != 1 {
		t.Fatal("strangle must stay while its put is open (it keeps the slot)")
	}
	sm.RemovePosition("p")
	sm.RemoveStrangleContaining("p")
	if len(sm.AllStrangles()) != 0 {
		t.Error("strangle must be removed once both legs are closed")
	}
}

func TestStateManager_SetStrangleLegRepairsMissingLeg(t *testing.T) {
	sm := orders.NewStateManager()
	call := shortLeg("c", "call", 0.1, 0.16)
	sm.AddPosition(call)
	sm.AddStrangle(&orders.Strangle{ID: "st", CallLeg: call})

	put := shortLeg("p2", "put", 0.1, -0.16)
	sm.AddPosition(put)
	sm.SetStrangleLeg("st", "put", put)
	if st := sm.AllStrangles()[0]; st.PutLeg == nil || st.PutLeg.ID != "p2" {
		t.Errorf("put leg not set: %+v", st.PutLeg)
	}
	sm.SetStrangleLeg("missing", "put", put) // unknown strangle: no-op, no panic
}

// Greeks from Deribit are long-perspective; the book is short, so every
// portfolio greek is negated. A short call (+0.16) and a short put (-0.10)
// of 1 BTC each net to -0.16 + 0.10 = -0.06.
func TestStateManager_PortfolioGreeksAreShortSigned(t *testing.T) {
	sm := orders.NewStateManager()
	sm.AddPosition(shortLeg("c", "call", 1, 0.16))
	sm.AddPosition(shortLeg("p", "put", 1, -0.10))

	if d := sm.TotalNetDelta(); math.Abs(d-(-0.06)) > 1e-12 {
		t.Errorf("net delta = %v, want -0.06", d)
	}
	if g := sm.NetGamma(); g >= 0 {
		t.Errorf("short book must be short gamma, got %v", g)
	}
	if v := sm.NetVega(); v >= 0 {
		t.Errorf("short book must be short vega, got %v", v)
	}
	if th := sm.NetTheta(); th <= 0 {
		t.Errorf("short book must earn theta, got %v", th)
	}
	if q := sm.TotalMarginUsed(); q != 2 {
		t.Errorf("TotalMarginUsed = %v, want summed qty 2", q)
	}
}

func TestStateManager_NextIDIsUnique(t *testing.T) {
	sm := orders.NewStateManager()
	seen := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := sm.NextID("pos")
			mu.Lock()
			defer mu.Unlock()
			if seen[id] {
				t.Errorf("duplicate ID %s", id)
			}
			seen[id] = true
		}()
	}
	wg.Wait()
}

func TestStateManager_ConcurrentReadersAndWriters(t *testing.T) {
	sm := orders.NewStateManager()
	sm.AddPosition(shortLeg("p", "put", 0.1, -0.16))
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				sm.UpdatePositionMid("p", float64(j), orders.Greeks{Delta: -0.2})
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				for _, p := range sm.AllPositions() {
					_ = p.CurrentMid * p.Qty // read fields of the snapshot
				}
			}
		}()
	}
	wg.Wait() // the race detector fails this test if snapshots share memory
}

func TestPosition_PnLHelpers(t *testing.T) {
	p := &orders.Position{Qty: 0.1, PremiumReceived: 0.002, CurrentMid: 0.005}
	if got := p.MtMPnL(); math.Abs(got-0.0015) > 1e-12 {
		t.Errorf("MtMPnL = %v, want 0.0015", got)
	}
	if got := p.ROIPct(); math.Abs(got-0.75) > 1e-12 {
		t.Errorf("ROIPct = %v, want 0.75", got)
	}
	p.CurrentMid = 0.06
	if got := p.LossPct(); math.Abs(got-2.0) > 1e-12 {
		t.Errorf("LossPct = %v, want 2.0 (stop-loss level)", got)
	}
	zero := &orders.Position{Qty: 0.1}
	if zero.ROIPct() != 0 || zero.LossPct() != 0 {
		t.Error("zero premium must not divide by zero")
	}
}
