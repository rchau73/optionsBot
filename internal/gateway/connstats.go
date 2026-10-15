package gateway

import (
	"sync"
	"time"
)

// ConnStats says how stable a gateway's connection has been, for the monitor
// (/api/status). Counts are per UTC day and restart with the process; the
// times survive the day change so the last drop stays visible.
type ConnStats struct {
	Day             string     `json:"day"`              // UTC date the counts are for
	DropsToday      int        `json:"drops_today"`      // connection lost (read error)
	ReconnectsToday int        `json:"reconnects_today"` // successful reconnects
	TimeoutsToday   int        `json:"timeouts_today"`   // requests that got no reply in time
	LastDropAt      *time.Time `json:"last_drop_at"`     // null until the first drop
	LastDropReason  string     `json:"last_drop_reason,omitempty"`
	LastReconnectAt *time.Time `json:"last_reconnect_at"`
	LastTimeoutAt   *time.Time `json:"last_timeout_at"`
}

// ConnTracker records drops, reconnects and timeouts. Every method takes now
// so tests can cross a UTC midnight. The zero value is ready to use.
type ConnTracker struct {
	mu sync.Mutex
	s  ConnStats
}

// rollDay zeroes the counts when now is on another UTC day. Caller holds mu.
func (t *ConnTracker) rollDay(now time.Time) {
	day := now.UTC().Format(time.DateOnly)
	if t.s.Day != day {
		t.s.Day = day
		t.s.DropsToday, t.s.ReconnectsToday, t.s.TimeoutsToday = 0, 0, 0
	}
}

// Drop records a lost connection and why.
func (t *ConnTracker) Drop(now time.Time, reason string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rollDay(now)
	t.s.DropsToday++
	t.s.LastDropAt = &now
	t.s.LastDropReason = reason
}

// Reconnect records a successful reconnect (subscriptions restored).
func (t *ConnTracker) Reconnect(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rollDay(now)
	t.s.ReconnectsToday++
	t.s.LastReconnectAt = &now
}

// Timeout records a request that got no reply in time.
func (t *ConnTracker) Timeout(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rollDay(now)
	t.s.TimeoutsToday++
	t.s.LastTimeoutAt = &now
}

// Stats returns a copy with today's counts as of now. The time pointers can
// be shared: a record always stores a new one and never writes through it.
func (t *ConnTracker) Stats(now time.Time) ConnStats {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rollDay(now)
	return t.s
}

// ConnStats reports the connection's drops, reconnects and timeouts today.
func (g *Gateway) ConnStats() ConnStats { return g.conns.Stats(time.Now()) }
