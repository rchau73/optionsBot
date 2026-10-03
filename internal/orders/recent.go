package orders

import (
	"encoding/json"
	"sync"
	"time"
)

// RecentEvent is one journal line kept in memory for the live monitor.
type RecentEvent struct {
	Seq   uint64          `json:"seq"`
	Event string          `json:"event"`
	At    time.Time       `json:"at"`
	Data  json.RawMessage `json:"data"` // the exact orders.log line
}

// recentRing keeps the last N journal events and counts every event type
// since start, so the monitor API can serve "what just happened" without
// reading log files. Safe for concurrent use.
type recentRing struct {
	mu     sync.Mutex
	buf    []RecentEvent
	next   int // write index once the buffer is full
	seq    uint64
	counts map[string]int
}

func newRecentRing(size int) *recentRing {
	return &recentRing{buf: make([]RecentEvent, 0, size), counts: map[string]int{}}
}

func (r *recentRing) add(event string, line []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	r.counts[event]++
	e := RecentEvent{Seq: r.seq, Event: event, At: time.Now(), Data: append(json.RawMessage(nil), line...)}
	if len(r.buf) < cap(r.buf) {
		r.buf = append(r.buf, e)
		return
	}
	r.buf[r.next] = e
	r.next = (r.next + 1) % len(r.buf)
}

// since returns events with Seq > after, oldest first, at most limit (the
// newest ones when more are available).
func (r *recentRing) since(after uint64, limit int) []RecentEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	ordered := make([]RecentEvent, 0, len(r.buf))
	ordered = append(ordered, r.buf[r.next:]...)
	ordered = append(ordered, r.buf[:r.next]...)
	out := make([]RecentEvent, 0, len(ordered))
	for _, e := range ordered {
		if e.Seq > after {
			out = append(out, e)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

func (r *recentRing) countsCopy() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := make(map[string]int, len(r.counts))
	for k, v := range r.counts {
		c[k] = v
	}
	return c
}
