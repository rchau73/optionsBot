package marketdata

import "sync"

// DVOLTracker computes the rolling IV percentile from DVOL index history.
type DVOLTracker struct {
	mu      sync.RWMutex
	window  int
	history []float64
}

func NewDVOLTracker(window int) *DVOLTracker {
	return &DVOLTracker{window: window}
}

// Push adds a new DVOL reading and trims history to the configured window.
func (d *DVOLTracker) Push(value float64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.history = append(d.history, value)
	if len(d.history) > d.window {
		d.history = d.history[len(d.history)-d.window:]
	}
}

// Percentile returns the percentile rank of the current value within the window.
// Returns 50.0 when insufficient data is available.
func (d *DVOLTracker) Percentile() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	n := len(d.history)
	if n < 2 {
		return 50.0
	}
	current := d.history[n-1]
	below := 0
	for _, v := range d.history[:n-1] {
		if v < current {
			below++
		}
	}
	return float64(below) / float64(n-1) * 100.0
}

// Current returns the most recent DVOL value.
func (d *DVOLTracker) Current() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if len(d.history) == 0 {
		return 0
	}
	return d.history[len(d.history)-1]
}

// AllowedMarginPct returns the IV-risk-model margin fraction based on percentile.
func AllowedMarginPct(ivPercentile float64) float64 {
	switch {
	case ivPercentile >= 70:
		return 0.35
	case ivPercentile >= 30:
		return 0.25
	default:
		return 0.15
	}
}
