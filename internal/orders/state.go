package orders

import (
	"sync"
	"time"
)

// StateManager tracks all open positions and strangles in memory.
type StateManager struct {
	mu        sync.RWMutex
	positions map[string]*Position
	strangles map[string]*Strangle
	nextID    int
}

func NewStateManager() *StateManager {
	return &StateManager{
		positions: make(map[string]*Position),
		strangles: make(map[string]*Strangle),
	}
}

func (s *StateManager) AddPosition(pos *Position) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.positions[pos.ID] = pos
}

func (s *StateManager) RemovePosition(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.positions, id)
}

func (s *StateManager) GetPosition(id string) (*Position, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.positions[id]
	return p, ok
}

func (s *StateManager) AllPositions() []*Position {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Position, 0, len(s.positions))
	for _, p := range s.positions {
		out = append(out, p)
	}
	return out
}

func (s *StateManager) AddStrangle(st *Strangle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.strangles[st.ID] = st
}

func (s *StateManager) RemoveStrangle(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.strangles, id)
}

// RemoveStrangleContaining removes any strangle that contains posID as a leg,
// but only if ALL of that strangle's legs are no longer in the positions map.
// If one leg is still active the strangle stays — it keeps its DTE slot occupied.
// SetStrangleLeg replaces one leg of an existing strangle with a new position.
// Used when a previously-closed leg is repaired by reopening it.
func (s *StateManager) SetStrangleLeg(stID, optType string, pos *Position) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.strangles[stID]
	if !ok {
		return
	}
	if optType == "call" {
		st.CallLeg = pos
	} else {
		st.PutLeg = pos
	}
}

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

func (s *StateManager) AllStrangles() []*Strangle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Strangle, 0, len(s.strangles))
	for _, st := range s.strangles {
		out = append(out, st)
	}
	return out
}

func (s *StateManager) UpdatePositionMid(id string, mid float64, greeks Greeks) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.positions[id]; ok {
		p.CurrentMid = mid
		p.CurrentGreeks = greeks
	}
}

// NetGamma returns portfolio gamma for all open positions.
// Deribit reports greeks from the long perspective (gamma always positive).
// Short positions negate it, so a short-only book has negative net gamma.
func (s *StateManager) NetGamma() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0.0
	for _, p := range s.positions {
		total -= p.CurrentGreeks.Gamma * p.Qty
	}
	return total
}

// TotalMarginUsed returns the total BTC notional deployed across all open positions.
func (s *StateManager) TotalMarginUsed() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0.0
	for _, p := range s.positions {
		total += p.Qty
	}
	return total
}

// NetVega returns portfolio vega. Short positions have negative vega.
func (s *StateManager) NetVega() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0.0
	for _, p := range s.positions {
		total -= p.CurrentGreeks.Vega * p.Qty
	}
	return total
}

// NetTheta returns portfolio theta. Short options collect positive theta
// (Deribit reports theta as negative from the long perspective, so we negate).
func (s *StateManager) NetTheta() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0.0
	for _, p := range s.positions {
		total -= p.CurrentGreeks.Theta * p.Qty
	}
	return total
}

// TotalNetDelta returns the sum of delta × qty across all open positions.
func (s *StateManager) TotalNetDelta() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0.0
	for _, p := range s.positions {
		total += p.CurrentGreeks.Delta * p.Qty
	}
	return total
}

// NextID returns a unique position/strangle ID.
func (s *StateManager) NextID(prefix string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	return prefix + "-" + time.Now().Format("20060102") + "-" + itoa(s.nextID)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
