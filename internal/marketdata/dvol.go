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
//
// It also keeps DVOLHistoryExtraDays more days than the window, so each
// recent daily close has its own percentile (see Daily) — the margin policy
// confirms band changes on those closes.
type DVOLTracker struct {
	mu         sync.RWMutex
	windowDays int
	days       []dvolDay // oldest first; the last entry is the current day
}

type dvolDay struct {
	day   time.Time
	value float64
}

// DVOLHistoryExtraDays is how many recent daily closes get a percentile.
const DVOLHistoryExtraDays = 30

// minKnownDays is the fewest previous days a percentile is trusted with.
const minKnownDays = 20

// DayIV is one day's DVOL and its IV percentile.
type DayIV struct {
	Day        time.Time // UTC midnight
	DVOL       float64
	Percentile float64
	Known      bool // enough previous days to rank against
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
	// Keep the window of previous days, the current one and the extra days.
	if extra := len(d.days) - (d.windowDays + 1 + DVOLHistoryExtraDays); extra > 0 {
		d.days = append(d.days[:0], d.days[extra:]...)
	}
}

// Percentile returns the share (0–100) of previous days whose DVOL was below
// the current day's. It returns 50 until at least one previous day is known.
func (d *DVOLTracker) Percentile() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if len(d.days) < 2 {
		return 50.0
	}
	pct, _ := d.percentileAt(len(d.days) - 1)
	return pct
}

// percentileAt ranks day i against the windowDays days before it.
func (d *DVOLTracker) percentileAt(i int) (pct float64, known bool) {
	from := max(0, i-d.windowDays)
	prev := d.days[from:i]
	if len(prev) == 0 {
		return 50.0, false
	}
	below := 0
	for _, p := range prev {
		if p.value < d.days[i].value {
			below++
		}
	}
	return float64(below) / float64(len(prev)) * 100.0, len(prev) >= min(d.windowDays, minKnownDays)
}

// Daily returns the recent completed days (oldest first, at most
// DVOLHistoryExtraDays) and the current day, each with its own percentile.
// With no data, today is the zero DayIV (not Known).
func (d *DVOLTracker) Daily() (closes []DayIV, today DayIV) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	n := len(d.days)
	if n == 0 {
		return nil, DayIV{}
	}
	day := func(i int) DayIV {
		pct, known := d.percentileAt(i)
		return DayIV{Day: d.days[i].day, DVOL: d.days[i].value, Percentile: pct, Known: known}
	}
	for i := max(0, n-1-DVOLHistoryExtraDays); i < n-1; i++ {
		closes = append(closes, day(i))
	}
	return closes, day(n - 1)
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
