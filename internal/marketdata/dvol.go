package marketdata

import (
	"sync"
	"time"
)

// DVOLTracker ranks today's DVOL (Deribit's 30-day implied volatility index)
// against the previous windowDays daily values, as a percentile.
//
// It keeps one value per UTC day: the live feed pushes DVOL about once a
// second, and ranking against seconds instead of days would make the
// percentile meaningless. The latest reading of a day replaces earlier ones.
type DVOLTracker struct {
	mu         sync.RWMutex
	windowDays int
	days       []dvolDay // oldest first; the last entry is the current day
}

type dvolDay struct {
	day   time.Time
	value float64
}

func NewDVOLTracker(windowDays int) *DVOLTracker {
	return &DVOLTracker{windowDays: windowDays}
}

// Record stores value as the DVOL of at's UTC day. Readings older than the
// current day are ignored.
func (d *DVOLTracker) Record(at time.Time, value float64) {
	day := at.UTC().Truncate(24 * time.Hour)
	d.mu.Lock()
	defer d.mu.Unlock()

	if n := len(d.days); n > 0 {
		last := d.days[n-1].day
		switch {
		case day.Equal(last):
			d.days[n-1].value = value
			return
		case day.Before(last):
			return
		}
	}
	d.days = append(d.days, dvolDay{day: day, value: value})
	// Keep the window of previous days plus the current one.
	if extra := len(d.days) - (d.windowDays + 1); extra > 0 {
		d.days = append(d.days[:0], d.days[extra:]...)
	}
}

// Percentile returns the share (0–100) of previous days whose DVOL was below
// the current day's. It returns 50 until at least one previous day is known.
func (d *DVOLTracker) Percentile() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	n := len(d.days)
	if n < 2 {
		return 50.0
	}
	current := d.days[n-1].value
	below := 0
	for _, p := range d.days[:n-1] {
		if p.value < current {
			below++
		}
	}
	return float64(below) / float64(n-1) * 100.0
}

// Current returns the latest DVOL value, or 0 before any reading.
func (d *DVOLTracker) Current() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if len(d.days) == 0 {
		return 0
	}
	return d.days[len(d.days)-1].value
}
