package orders

import (
	"sync"
	"time"
)

// CallMinute is one UTC minute of one exchange method: the calls sent and
// how many Deribit rejected as rate limited (10028/10040). #888: ETH's margin
// simulation was refused on most cycles and nothing said how often, nor how
// many calls the bot itself sent.
type CallMinute struct {
	Method      string
	Minute      time.Time // the minute's start (UTC)
	Calls       int
	RateLimited int
}

// RejectedPct is the share of the minute's calls rejected as rate limited.
func (m CallMinute) RejectedPct() float64 {
	if m.Calls == 0 {
		return 0
	}
	return 100 * float64(m.RateLimited) / float64(m.Calls)
}

// CallStats counts calls per method and minute. Safe for concurrent use.
type CallStats struct {
	mu  sync.Mutex
	cur map[string]*CallMinute
}

// Record counts one call of method at now. When the call is the first of a
// new minute it returns the method's previous minute (ok), for the caller to
// log: the count of a minute is known only once it is over.
func (c *CallStats) Record(method string, rateLimited bool, now time.Time) (CallMinute, bool) {
	minute := now.UTC().Truncate(time.Minute)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cur == nil {
		c.cur = make(map[string]*CallMinute)
	}
	var done CallMinute
	var ok bool
	m := c.cur[method]
	if m == nil || !m.Minute.Equal(minute) {
		if m != nil {
			done, ok = *m, true
		}
		m = &CallMinute{Method: method, Minute: minute}
		c.cur[method] = m
	}
	m.Calls++
	if rateLimited {
		m.RateLimited++
	}
	return done, ok
}
