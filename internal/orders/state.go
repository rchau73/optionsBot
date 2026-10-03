package orders

import (
	"strconv"
	"sync"
	"time"
)

// StateManager is the in-memory book of open positions and strangles.
//
// It is safe for concurrent use. Readers always receive copies (snapshots):
// a caller can never observe, or race with, a later update. All changes go
// through the methods below, keyed by position or strangle ID.
//
// The book is not persisted. After a restart the strategy rebuilds it from
// the exchange (reconcile), which is the source of truth.
type StateManager struct {
	mu        sync.RWMutex
	positions map[string]*Position
	strangles map[string]*Strangle // legs point at entries in positions
	nextID    int
}

func NewStateManager() *StateManager {
	return &StateManager{
		positions: make(map[string]*Position),
		strangles: make(map[string]*Strangle),
	}
}

// AddPosition stores a copy of pos.
func (s *StateManager) AddPosition(pos *Position) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *pos
	s.positions[pos.ID] = &cp
}

func (s *StateManager) RemovePosition(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.positions, id)
}

// GetPosition returns a snapshot of the position with the given ID.
func (s *StateManager) GetPosition(id string) (*Position, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.positions[id]
	if !ok {
		return nil, false
	}
	cp := *p
	return &cp, true
}

// AllPositions returns snapshots of every open position.
func (s *StateManager) AllPositions() []*Position {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Position, 0, len(s.positions))
	for _, p := range s.positions {
		cp := *p
		out = append(out, &cp)
	}
	return out
}

// AddStrangle stores a strangle. Its legs are linked to the stored positions
// with the same IDs, so later updates to those positions show through.
func (s *StateManager) AddStrangle(st *Strangle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *st
	cp.CallLeg = s.linkLocked(st.CallLeg)
	cp.PutLeg = s.linkLocked(st.PutLeg)
	s.strangles[st.ID] = &cp
}

// linkLocked returns the stored position for leg's ID, or a private copy of
// leg when it is not (or no longer) in the book. Callers hold s.mu.
func (s *StateManager) linkLocked(leg *Position) *Position {
	if leg == nil {
		return nil
	}
	if p, ok := s.positions[leg.ID]; ok {
		return p
	}
	cp := *leg
	return &cp
}

func (s *StateManager) RemoveStrangle(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.strangles, id)
}

// SetStrangleLeg replaces one leg of an existing strangle with a position.
// Used when a previously-closed leg is repaired by reopening it.
func (s *StateManager) SetStrangleLeg(stID, optType string, pos *Position) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.strangles[stID]
	if !ok {
		return
	}
	if optType == "call" {
		st.CallLeg = s.linkLocked(pos)
	} else {
		st.PutLeg = s.linkLocked(pos)
	}
}

// RemoveStrangleContaining removes any strangle that contains posID as a leg,
// but only if ALL of that strangle's legs are no longer in the positions map.
// If one leg is still active the strangle stays — it keeps its DTE slot occupied.
func (s *StateManager) RemoveStrangleContaining(posID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, st := range s.strangles {
		if (st.CallLeg == nil || st.CallLeg.ID != posID) &&
			(st.PutLeg == nil || st.PutLeg.ID != posID) {
			continue
		}
		callActive := st.CallLeg != nil && s.positions[st.CallLeg.ID] != nil
		putActive := st.PutLeg != nil && s.positions[st.PutLeg.ID] != nil
		if !callActive && !putActive {
			delete(s.strangles, id)
		}
	}
}

// AllStrangles returns snapshots of every strangle, legs included.
func (s *StateManager) AllStrangles() []*Strangle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Strangle, 0, len(s.strangles))
	for _, st := range s.strangles {
		cp := *st
		if st.CallLeg != nil {
			leg := *st.CallLeg
			cp.CallLeg = &leg
		}
		if st.PutLeg != nil {
			leg := *st.PutLeg
			cp.PutLeg = &leg
		}
		out = append(out, &cp)
	}
	return out
}

// UpdatePositionQty adjusts a position's size and premium after a partial
// close or a rebalance. Scale newPremiumReceived with the size so ROI and
// stop-loss math stay correct.
func (s *StateManager) UpdatePositionQty(id string, newQty, newPremiumReceived float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.positions[id]; ok {
		p.Qty = newQty
		p.PremiumReceived = newPremiumReceived
	}
}

// UpdatePositionMid sets a position's mark and greeks from a live quote and
// marks them live. Call it only with real market data: an instrument with no
// quote must leave the last known mark in place.
func (s *StateManager) UpdatePositionMid(id string, mid float64, greeks Greeks) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.positions[id]; ok {
		p.CurrentMid = mid
		p.CurrentGreeks = greeks
		p.MarkLive = true
	}
}

// Deribit reports greeks from the long holder's perspective. Every position in
// this book is short, so each portfolio greek below negates the per-contract value.

// NetGamma returns portfolio gamma (negative for a short-only book).
func (s *StateManager) NetGamma() float64 {
	return s.sumShort(func(g Greeks) float64 { return g.Gamma })
}

// NetVega returns portfolio vega (negative for a short-only book).
func (s *StateManager) NetVega() float64 {
	return s.sumShort(func(g Greeks) float64 { return g.Vega })
}

// NetTheta returns portfolio theta. Short options earn time decay, so this is
// positive (Deribit's long-perspective theta is negative).
func (s *StateManager) NetTheta() float64 {
	return s.sumShort(func(g Greeks) float64 { return g.Theta })
}

// TotalNetDelta returns portfolio delta in units of the underlying. A short
// call (long delta +0.16) contributes -0.16 × qty; a short put (-0.16)
// contributes +0.16 × qty.
func (s *StateManager) TotalNetDelta() float64 {
	return s.sumShort(func(g Greeks) float64 { return g.Delta })
}

func (s *StateManager) sumShort(greek func(Greeks) float64) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0.0
	for _, p := range s.positions {
		total -= greek(p.CurrentGreeks) * p.Qty
	}
	return total
}

// TotalMarginUsed returns the summed contract quantity of open positions.
// It is a rough proxy only; under Portfolio Margin use the exchange's
// initial margin instead (see strategy.fetchMarginState).
func (s *StateManager) TotalMarginUsed() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0.0
	for _, p := range s.positions {
		total += p.Qty
	}
	return total
}

// NextID returns a unique position/strangle ID such as "pos-20260102-7".
func (s *StateManager) NextID(prefix string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	return prefix + "-" + time.Now().Format("20060102") + "-" + strconv.Itoa(s.nextID)
}
