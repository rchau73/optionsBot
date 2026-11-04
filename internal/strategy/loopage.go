package strategy

import (
	"context"
	"log/slog"
	"time"
)

// LoopLateCycles: the decision loop is late once its last completed cycle is
// more than this many evaluation intervals old (#593). Nothing is restarted
// or traded on it: WatchLoopAge logs an ERROR and /api/status shows it.
const LoopLateCycles = 2

// SuspendMinGap is how much further the wall clock must have moved than the
// process's own clock between two watchdog ticks before the host counts as
// suspended (a laptop asleep, a paused VM). NTP corrections are far smaller.
const SuspendMinGap = time.Minute

// LoopAge is how old the last completed cycle is on the wall clock, the
// clock loop_at is shown in (pure). Go's monotonic clock can stand still
// while the host sleeps, so it would hide exactly the gap a person sees.
// A zero last (no cycle yet) is age 0.
func LoopAge(last, now time.Time) time.Duration {
	if last.IsZero() {
		return 0
	}
	if age := now.Round(0).Sub(last.Round(0)); age > 0 {
		return age
	}
	return 0
}

// LoopLate reports whether the loop's last cycle is more than LoopLateCycles
// intervals old (pure). A halted loop (kill switch) is idle on purpose and a
// loop that never cycled has no age: neither is late.
func LoopLate(last time.Time, halted bool, now time.Time, evalInterval time.Duration) bool {
	if halted || last.IsZero() || evalInterval <= 0 {
		return false
	}
	return LoopAge(last, now) > LoopLateCycles*evalInterval
}

// SuspendedFor is how long the host was suspended between two readings: the
// wall time that passed beyond the process's own (monotonic) time, when it is
// at least SuspendMinGap; 0 otherwise (pure).
func SuspendedFor(wallElapsed, monoElapsed time.Duration) time.Duration {
	if gap := wallElapsed - monoElapsed; gap >= SuspendMinGap {
		return gap
	}
	return 0
}

// WatchLoopAge checks the decision loop every `every` until ctx ends and logs,
// never acts (#593):
//   - an ERROR when the host was suspended since the last check
//     (the bot did not run at all — no exit was checked);
//   - one ERROR when the last cycle becomes more than LoopLateCycles intervals
//     old, and an INFO when cycles are on time again.
//
// The restart backstop stays WatchProgress; this one only makes a late loop
// visible early, in the log and (LoopLate) in /api/status.
func WatchLoopAge(ctx context.Context, progress func() (last time.Time, halted bool), evalInterval, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	prev := time.Now()
	late := false
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if gap := SuspendedFor(now.Round(0).Sub(prev.Round(0)), now.Sub(prev)); gap > 0 {
				slog.Error("host was suspended: the bot did not run and no exit was checked",
					"suspended", gap.Round(time.Second), "from", prev.Round(0), "to", now.Round(0))
			}
			prev = now
			last, halted := progress()
			isLate := LoopLate(last, halted, now, evalInterval)
			switch {
			case isLate && !late:
				slog.Error("decision loop late: no cycle completed, exits are not being checked",
					"loop_at", last.Round(0), "loop_age", LoopAge(last, now).Round(time.Second),
					"limit", LoopLateCycles*evalInterval)
			case !isLate && late:
				slog.Info("decision loop on time again", "loop_at", last.Round(0))
			}
			late = isLate
		}
	}
}
