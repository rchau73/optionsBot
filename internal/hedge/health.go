package hedge

import "time"

// Health says whether the real hedge can hold the book's delta (#664). While
// it cannot, the GEX shed works as with the mode off: the book must never have
// neither. It lives in memory: after a restart the first hedge order finds out.
type Health struct {
	HaltReason string // why no order is sent ("" = not halted), in plain words
	HaltedAt   time.Time
	RecheckAt  time.Time // when a halted hedge looks again

	rejected string // the last order was rejected, and why
	unfilled int    // cycles in a row with a hedge due and nothing filled
	why      string // why the latest of those cycles did not fill
}

// Halted reports a reason that retrying cannot cure: no orders until it clears.
func (h Health) Halted() bool { return h.HaltReason != "" }

// Waiting reports a halted hedge whose next re-check is still to come.
func (h Health) Waiting(now time.Time) bool { return h.Halted() && now.Before(h.RecheckAt) }

// Halt stops the hedge for reason until now + wait, and reports whether the
// reason is new (a re-check that finds the same one is not a new halt).
func (h *Health) Halt(reason string, now time.Time, wait time.Duration) bool {
	fresh := h.HaltReason != reason
	if !h.Halted() {
		h.HaltedAt = now
	}
	h.HaltReason, h.RecheckAt = reason, now.Add(wait)
	return fresh
}

// Resume clears a halt: the reason is gone.
func (h *Health) Resume() { h.HaltReason, h.HaltedAt, h.RecheckAt = "", time.Time{}, time.Time{} }

// Rejected records that the exchange refused the last order.
func (h *Health) Rejected(why string) { h.rejected = why }

// NotFilled counts a cycle with a hedge due (or unknowable) and nothing filled.
func (h *Health) NotFilled(why string) { h.unfilled, h.why = h.unfilled+1, why }

// Holding records a fill, or a book inside the band. A halt stays: only its
// reason clearing lifts it.
func (h *Health) Holding() { h.rejected, h.unfilled, h.why = "", 0, "" }

// NotHolding is why the hedge is not holding the delta, "" while it is:
// halted, the last order rejected, or maxCycles cycles in a row without a fill.
func (h Health) NotHolding(maxCycles int) string {
	switch {
	case h.Halted():
		return h.HaltReason
	case h.rejected != "":
		return "the last hedge order was rejected: " + h.rejected
	case h.unfilled >= maxCycles:
		return h.why
	}
	return ""
}

// InstrumentClosed reports an instrument whose ticker state is not "open".
// An empty state is no reading, not a lock.
func InstrumentClosed(state string) bool { return state != "" && state != "open" }
