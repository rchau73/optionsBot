package strategy

import (
	"context"
	"time"
)

// WatchProgress is a backstop against a decision loop that stops cycling
// for any reason (a hung dependency, a bug). Every `every` it reads
// progress(); once the last completed cycle is older than limit it calls
// onStall once and returns. The caller exits the process, and the supervisor
// (Docker's restart policy) starts a fresh one, which reconciles positions
// with the exchange — a stalled loop checks no stop-loss, a restarted one does.
//
// A halted strategy (kill switch) is idle on purpose and never stalls:
// restarting it would put it straight back into the market.
func WatchProgress(ctx context.Context, progress func() (last time.Time, halted bool), limit, every time.Duration, onStall func(idle time.Duration)) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			last, halted := progress()
			if halted || last.IsZero() {
				continue
			}
			if idle := now.Sub(last); idle > limit {
				onStall(idle)
				return
			}
		}
	}
}

// Progress reports when the decision loop last completed a cycle (or
// started) and whether the kill switch has halted it. Safe from any goroutine.
func (s *Strategy) Progress() (last time.Time, halted bool) {
	s.pub.mu.RLock()
	defer s.pub.mu.RUnlock()
	return s.pub.at, s.pub.halted
}

// markStarted counts the start of Run as progress, so startup (reconcile,
// warm-up, first entries) is covered by the watchdog too.
func (s *Strategy) markStarted() {
	s.pub.mu.Lock()
	defer s.pub.mu.Unlock()
	s.pub.at = time.Now()
}
