package evidence

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"optionsbot/internal/coverage"
)

// The daily recording check (#716): the previous UTC day's coverage of every
// market recording (the books and the minute GEX), measured by the same
// reader as cmd/datacoverage and the weekly coverage section, against the
// same floor the weekly report labels a replay partial under
// (ReplayFullCoveragePct) and the same long gap (LongGapHours). It tells a
// person the next morning, instead of in the weekly report. Hours and
// shares only: never an amount.

// DailyRecording is one bot's check of one UTC day.
type DailyRecording struct {
	Day          string         `json:"day"` // YYYY-MM-DD, UTC
	Dir          string         `json:"dir"`
	FloorPct     float64        `json:"floor_pct"`
	LongGapHours float64        `json:"long_gap_hours"`
	Datasets     []DailyDataset `json:"datasets"`
	Short        bool           `json:"short"` // a recorded dataset is under the floor or has a long gap
}

// DailyDataset is one market recording's coverage of the day. A dataset
// whose folder does not exist is not recorded by this bot and is never
// short; a folder without the day's file is 0 % covered.
type DailyDataset struct {
	Dataset      string         `json:"dataset"`
	Status       string         `json:"status"` // recorded | not recorded
	MarketHours  float64        `json:"market_hours"`
	CoveredHours float64        `json:"covered_hours"`
	CoveredPct   float64        `json:"covered_pct"`
	Records      int            `json:"records"`
	Longest      coverage.Gap   `json:"longest_gap"`
	LongGaps     []coverage.Gap `json:"long_gaps"` // gaps over LongGapHours, in time order
	Short        bool           `json:"short"`
}

// YesterdayWindow is the UTC day before now's: [from, to). Pure.
func YesterdayWindow(now time.Time) (from, to time.Time) {
	to = now.UTC().Truncate(24 * time.Hour)
	return to.AddDate(0, 0, -1), to
}

// CheckYesterday reads the bot folder dir and checks the day before now.
// A damaged day file is a gap (the recorder's readers skip the stretch).
func CheckYesterday(dir string, now time.Time) (DailyRecording, error) {
	from, to := YesterdayWindow(now)
	res, err := coverage.Run(dir, from, to, now)
	if err != nil {
		return DailyRecording{}, err
	}
	return BuildDailyRecording(res), nil
}

// BuildDailyRecording keeps the market recordings of res (every dataset but
// the P&L history, which the bot writes, not the recorder) and checks each
// against the floor. Pure.
func BuildDailyRecording(res coverage.Result) DailyRecording {
	market := map[string]bool{}
	for _, ds := range coverage.Datasets("") {
		market[ds.Name] = ds.Kind != coverage.KindPnL
	}
	hours := res.To.Sub(res.From).Hours()
	d := DailyRecording{Day: res.From.Format(time.DateOnly), Dir: res.Dir, FloorPct: ReplayFullCoveragePct,
		LongGapHours: LongGapHours, Datasets: []DailyDataset{}}
	for _, r := range res.Datasets {
		if !market[r.Dataset] {
			continue
		}
		ds := DailyDataset{Dataset: r.Dataset, Status: CoverageRecorded, MarketHours: hours,
			CoveredHours: hours - r.MissingHours, CoveredPct: r.CoveredPct, Records: r.Records,
			Longest: r.Longest(), LongGaps: []coverage.Gap{}}
		if !r.Found {
			ds.Status = CoverageNotRecorded
			d.Datasets = append(d.Datasets, ds)
			continue
		}
		for _, g := range r.Gaps {
			if g.Hours > LongGapHours {
				ds.LongGaps = append(ds.LongGaps, g)
			}
		}
		ds.Short = ds.CoveredPct < ReplayFullCoveragePct || len(ds.LongGaps) > 0
		d.Short = d.Short || ds.Short
		d.Datasets = append(d.Datasets, ds)
	}
	return d
}

// FormatDaily prints the check: one line per dataset, hours and shares only.
func FormatDaily(w io.Writer, d DailyRecording) error {
	const ts = "2006-01-02 15:04"
	fmt.Fprintf(w, "Recording check — %s — %s UTC (floor %.0f %%, long gap over %.0f h)\n", d.Dir, d.Day, d.FloorPct, d.LongGapHours)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, ds := range d.Datasets {
		if ds.Status != CoverageRecorded {
			fmt.Fprintf(tw, "  %s\t%s\n", ds.Dataset, ds.Status)
			continue
		}
		verdict := "ok"
		if ds.Short {
			verdict = "SHORT"
		}
		longest := "none"
		if ds.Longest.Hours > 0 {
			longest = fmt.Sprintf("%s from %s", hoursText(ds.Longest.Hours), ds.Longest.From.Format(ts))
		}
		fmt.Fprintf(tw, "  %s\t%.1f %%\t%.1f of %.0f h\tlongest gap %s\t%s\n", ds.Dataset, ds.CoveredPct, ds.CoveredHours, ds.MarketHours, longest, verdict)
	}
	return tw.Flush()
}
