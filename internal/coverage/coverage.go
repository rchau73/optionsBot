// Package coverage measures how much recorded history stands behind a
// replay or a backtest (#233): for each dataset a bot records (hourly books,
// USDC books, minute GEX, the P&L history) the days and hours of a window it
// covers and its gaps. Missing time is reported, never filled in. Research
// only: it reads files, never trading state, never Deribit.
package coverage

import (
	"sort"
	"time"
)

// Report is the coverage of one dataset over a window [From, To), cut into
// bins of Step (one hour for the books, one minute for GEX and P&L).
type Report struct {
	Dataset     string        `json:"dataset"`
	Path        string        `json:"path,omitempty"`
	Found       bool          `json:"found"` // the file or directory exists
	Step        time.Duration `json:"-"`
	StepMinutes float64       `json:"step_minutes"`
	From        time.Time     `json:"from"`
	To          time.Time     `json:"to"` // excluded
	// Records inside the window, and the first and last of them (zero when
	// there are none).
	Records int       `json:"records"`
	First   time.Time `json:"first,omitempty"`
	Last    time.Time `json:"last,omitempty"`
	// Bins of the window, those with a record and the share covered.
	Bins         int     `json:"bins"`
	CoveredBins  int     `json:"covered_bins"`
	MissingBins  int     `json:"missing_bins"`
	MissingHours float64 `json:"missing_hours"`
	CoveredPct   float64 `json:"covered_pct"`
	// Days of the window: with at least one record, without any missing
	// bin, and the days with no record at all (YYYY-MM-DD).
	DaysInWindow    int      `json:"days_in_window"`
	DaysCovered     int      `json:"days_covered"`
	DaysComplete    int      `json:"days_complete"`
	MissingDays     []string `json:"missing_days"`
	Days            []Day    `json:"days"`
	Gaps            []Gap    `json:"gaps"` // runs of missing bins, in time order
	LongestGapHours float64  `json:"longest_gap_hours"`
}

// Day is one UTC day of the window.
type Day struct {
	Date         string  `json:"date"` // YYYY-MM-DD
	Records      int     `json:"records"`
	Bins         int     `json:"bins"`
	MissingBins  int     `json:"missing_bins"`
	MissingHours float64 `json:"missing_hours"`
}

// Gap is a run of missing bins [From, To).
type Gap struct {
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
	Hours float64   `json:"hours"`
}

// Longest returns the gap with the most hours (the zero Gap when none).
func (r Report) Longest() Gap {
	var g Gap
	for _, x := range r.Gaps {
		if x.Hours > g.Hours {
			g = x
		}
	}
	return g
}

// jitterFactor: an empty bin between two records at most this many steps
// apart is not a gap. The recorders run on a ticker, so a record a second
// late can land in the next bin and leave one empty although nothing was
// missed (GEX at 10:00:59, then 10:02:01: minute 10:01 is empty).
const jitterFactor = 1.5

// Coverage cuts [from, to) into bins of step from `from` and reports which
// hold a record of times. Records outside the window only bridge an empty
// bin at its edge (see jitterFactor); they are not counted. Pure.
func Coverage(times []time.Time, from, to time.Time, step time.Duration) Report {
	from, to = from.UTC(), to.UTC()
	r := Report{From: from, To: to, Step: step, StepMinutes: step.Minutes(), MissingDays: []string{}, Days: []Day{}, Gaps: []Gap{}}
	if step <= 0 || !to.After(from) {
		return r
	}
	ts := make([]time.Time, len(times))
	for i, t := range times {
		ts[i] = t.UTC()
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i].Before(ts[j]) })

	days := map[string]*Day{}
	var order []string
	day := func(t time.Time) *Day {
		key := t.Format(time.DateOnly)
		d, ok := days[key]
		if !ok {
			d = &Day{Date: key}
			days[key] = d
			order = append(order, key)
		}
		return d
	}

	// Missing time is summed as durations (a partial last bin counts its
	// own length) and turned into hours once, so 1,440 missing minutes are
	// 24 h, not 23.99…
	missing := map[string]time.Duration{}
	var total time.Duration
	inGap := false
	for start := from; start.Before(to); start = start.Add(step) {
		end := start.Add(step)
		if end.After(to) {
			end = to
		}
		d := day(start)
		d.Bins++
		r.Bins++
		if covered(ts, start, end, step) {
			r.CoveredBins++
			inGap = false
			continue
		}
		d.MissingBins++
		r.MissingBins++
		missing[d.Date] += end.Sub(start)
		total += end.Sub(start)
		if !inGap {
			r.Gaps = append(r.Gaps, Gap{From: start})
			inGap = true
		}
		g := &r.Gaps[len(r.Gaps)-1]
		g.To = end
		g.Hours = g.To.Sub(g.From).Hours()
	}
	r.MissingHours = total.Hours()

	for _, t := range ts {
		if t.Before(from) || !t.Before(to) {
			continue
		}
		if r.Records == 0 {
			r.First = t
		}
		r.Last = t
		r.Records++
		day(t).Records++
	}

	for _, key := range order {
		d := days[key]
		d.MissingHours = missing[key].Hours()
		r.Days = append(r.Days, *d)
		r.DaysInWindow++
		if d.Records > 0 {
			r.DaysCovered++
		} else {
			r.MissingDays = append(r.MissingDays, key)
		}
		if d.MissingBins == 0 {
			r.DaysComplete++
		}
	}
	r.CoveredPct = 100 * float64(r.CoveredBins) / float64(r.Bins)
	r.LongestGapHours = r.Longest().Hours
	return r
}

// covered: a record in [start, end), or the records either side of the bin
// at most jitterFactor steps apart. ts is sorted.
func covered(ts []time.Time, start, end time.Time, step time.Duration) bool {
	j := sort.Search(len(ts), func(i int) bool { return !ts[i].Before(start) })
	if j < len(ts) && ts[j].Before(end) {
		return true
	}
	if j == 0 || j == len(ts) {
		return false
	}
	return ts[j].Sub(ts[j-1]) <= time.Duration(jitterFactor*float64(step))
}

// HourlyOnlyDays are the days that have recorded books but no minute GEX
// record: cmd/stress -replay decides hourly on them, on the books alone.
func HourlyOnlyDays(books, gexMinute Report) []string {
	gexDays := map[string]bool{}
	for _, d := range gexMinute.Days {
		if d.Records > 0 {
			gexDays[d.Date] = true
		}
	}
	out := []string{}
	for _, d := range books.Days {
		if d.Records > 0 && !gexDays[d.Date] {
			out = append(out, d.Date)
		}
	}
	return out
}
