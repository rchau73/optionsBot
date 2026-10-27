package evidence

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"optionsbot/internal/coverage"
)

// The recorded-history coverage section (#362): how complete each dataset
// behind the replay and the stress was over the week — the share of its
// hours covered, its gaps and the days the replay could only decide hourly
// — measured by internal/coverage (the engine of cmd/datacoverage). Missing
// time is listed, never filled in. Times, hours and shares only.

// ReplayFullCoveragePct is the share of the week's hours the market books
// must cover for the replay and the replay − live gap to stand for the
// whole week; under it both are labelled partial.
const ReplayFullCoveragePct = 90.0

// LongGapHours: a recorded dataset missing more consecutive hours than this
// is named in the report's notices, which evidence.sh posts and notifies so
// the recorder can be fixed in time.
const LongGapHours = 3.0

// Dataset status in the coverage section.
const (
	CoverageRecorded    = "recorded"
	CoverageNotRecorded = "not recorded" // no file or folder: nothing was ever written there
	CoverageNotRead     = "not read"     // it exists but could not be read
)

// RecordedSet is one dataset's record times, read by Load. Found is false
// when its file or folder does not exist; Err says why it was not read.
type RecordedSet struct {
	Name, Path string
	Step       time.Duration // its recording period (coverage.Datasets)
	Found      bool
	Times      []time.Time
	Err        string
}

// Coverage is the section.
type Coverage struct {
	Known        bool              `json:"known"`
	Note         string            `json:"note,omitempty"` // why not measured
	FullPct      float64           `json:"replay_full_pct"`
	LongGapHours float64           `json:"long_gap_hours"`
	Datasets     []DatasetCoverage `json:"datasets"`
	// MarketPct is the market books' share of the week's hours (nil when
	// not measured); Partial is MarketPct under FullPct, or no books.
	MarketPct *float64 `json:"market_pct"`
	Partial   bool     `json:"partial"`
	// Days with books but no minute GEX record: the replay decided hourly.
	HourlyOnlyDays []string  `json:"hourly_only_days"`
	LongGaps       []LongGap `json:"long_gaps"` // gaps over LongGapHours of recorded datasets, in time order
}

// DatasetCoverage is one dataset's coverage of the week.
type DatasetCoverage struct {
	coverage.Report
	Status string `json:"status"` // recorded | not recorded | not read
	Note   string `json:"note,omitempty"`
}

// LongGap is a gap over LongGapHours in one dataset.
type LongGap struct {
	Dataset string    `json:"dataset"`
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
	Hours   float64   `json:"hours"`
}

// buildCoverage measures every recorded set over the window. Pure.
func buildCoverage(sets []RecordedSet, w Window) Coverage {
	c := Coverage{FullPct: ReplayFullCoveragePct, LongGapHours: LongGapHours,
		Datasets: []DatasetCoverage{}, HourlyOnlyDays: []string{}, LongGaps: []LongGap{}}
	if sets == nil {
		c.Note = "the recorded datasets were not read"
		return c
	}
	c.Known = true
	byName := map[string]coverage.Report{}
	for _, s := range sets {
		r := coverage.Coverage(s.Times, w.From, w.To, s.Step)
		r.Dataset, r.Path, r.Found = s.Name, s.Path, s.Found
		d := DatasetCoverage{Report: r, Status: CoverageRecorded}
		switch {
		case s.Err != "":
			d.Status, d.Note = CoverageNotRead, s.Err
		case !s.Found:
			d.Status = CoverageNotRecorded
		}
		c.Datasets = append(c.Datasets, d)
		byName[s.Name] = r
		if d.Status != CoverageRecorded {
			continue
		}
		if s.Name == "market" {
			pct := r.CoveredPct
			c.MarketPct = &pct
		}
		for _, g := range r.Gaps {
			if g.Hours > LongGapHours {
				c.LongGaps = append(c.LongGaps, LongGap{Dataset: s.Name, From: g.From, To: g.To, Hours: g.Hours})
			}
		}
	}
	c.Partial = c.MarketPct == nil || *c.MarketPct < ReplayFullCoveragePct
	c.HourlyOnlyDays = coverage.HourlyOnlyDays(byName["market"], byName["market_gex"])
	return c
}

// partialNote is the label the replay and the gap carry when the books do
// not cover enough of the week ("" when they do, or coverage is unknown).
func (c Coverage) partialNote() string {
	if !c.Known || !c.Partial {
		return ""
	}
	if c.MarketPct == nil {
		return "partial: the market books were not recorded"
	}
	return fmt.Sprintf("partial: the market books cover %.1f %% of the week's hours, under %.0f %%", *c.MarketPct, c.FullPct)
}

// maxGapsListed is how many gaps per dataset the Markdown lists (the
// longest first); the .json lists them all.
const maxGapsListed = 10

// writeCoverage writes the section: one row per dataset, then each
// recorded dataset's missing hours — its gaps and the days they fall on.
func writeCoverage(p func(string, ...any), c Coverage) {
	const ts = "2006-01-02 15:04"
	p("## Recorded-history coverage\n\n")
	if !c.Known {
		p("Not measured: %s.\n\n", c.Note)
		return
	}
	p("Source: `internal/coverage` (as `cmd/datacoverage`) over the week's hours; missing time is listed, never filled in. "+
		"The replay and the gap are labelled partial when the market books cover under %.0f %% of the hours. "+
		"A gap over %.0f h is a recorder notice.\n\n", c.FullPct, c.LongGapHours)
	p("| Dataset | Source | Covered | Missing | Longest gap | Days complete |\n|---|---|---|---|---|---|\n")
	for _, d := range c.Datasets {
		if d.Status != CoverageRecorded {
			status := d.Status
			if d.Note != "" {
				status += ": " + strings.ReplaceAll(d.Note, "|", "/")
			}
			p("| %s | %s | %s | | | |\n", d.Dataset, d.Path, status)
			continue
		}
		longest := "none"
		if g := d.Longest(); g.Hours > 0 {
			longest = fmt.Sprintf("%s from %s", hoursText(g.Hours), g.From.Format(ts))
		}
		p("| %s | %s | %.1f %% | %s | %s | %d of %d |\n", d.Dataset, d.Path, d.CoveredPct, hoursText(d.MissingHours), longest, d.DaysComplete, d.DaysInWindow)
	}
	p("\n")
	switch {
	case c.MarketPct == nil:
		p("- Replay and gap: **partial** — the market books were not recorded or not read.\n")
	case c.Partial:
		p("- Replay and gap: **partial** — the market books cover %.1f %% of the hours (under %.0f %%).\n", *c.MarketPct, c.FullPct)
	default:
		p("- Replay and gap: the market books cover %.1f %% of the hours (at least %.0f %%).\n", *c.MarketPct, c.FullPct)
	}
	if len(c.HourlyOnlyDays) == 0 {
		p("- Days the replay decided hourly (books, no minute GEX): none\n")
	} else {
		p("- Days the replay decided hourly (books, no minute GEX): %s\n", strings.Join(c.HourlyOnlyDays, ", "))
	}
	if len(c.LongGaps) == 0 {
		p("- Gaps over %.0f h: none\n", c.LongGapHours)
	} else {
		p("- **Gaps over %.0f h** (fix the recorder):", c.LongGapHours)
		for _, g := range c.LongGaps {
			p(" %s %s from %s UTC;", g.Dataset, hoursText(g.Hours), g.From.Format(ts))
		}
		p("\n")
	}
	for _, d := range c.Datasets {
		if d.Status != CoverageRecorded || len(d.Gaps) == 0 {
			continue
		}
		var days []string
		for _, day := range d.Days {
			if day.MissingBins > 0 {
				days = append(days, fmt.Sprintf("%s %s", day.Date, hoursText(day.MissingHours)))
			}
		}
		gaps := append([]coverage.Gap{}, d.Gaps...)
		sort.SliceStable(gaps, func(i, j int) bool { return gaps[i].Hours > gaps[j].Hours })
		shown := min(len(gaps), maxGapsListed)
		var list []string
		for _, g := range gaps[:shown] {
			list = append(list, fmt.Sprintf("%s → %s (%s)", g.From.Format(ts), g.To.Format(ts), hoursText(g.Hours)))
		}
		more := ""
		if len(gaps) > shown {
			more = fmt.Sprintf("; %d shorter in the .json", len(gaps)-shown)
		}
		p("- %s missing: %s · gaps %s%s\n", d.Dataset, strings.Join(days, ", "), strings.Join(list, ", "), more)
	}
	p("\n")
}

func hoursText(h float64) string {
	if h == 0 {
		return "0 h"
	}
	if h < 1 {
		return fmt.Sprintf("%.0f min", h*60)
	}
	return fmt.Sprintf("%.1f h", h)
}
