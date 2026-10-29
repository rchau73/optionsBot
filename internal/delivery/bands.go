package delivery

import (
	"fmt"
	"math"
	"sort"
)

// DORA performance bands (the State of DevOps reports' clusters). A figure
// with fewer than MinSamples samples gets no band: it says so instead.
const (
	BandElite  = "elite"
	BandHigh   = "high"
	BandMedium = "medium"
	BandLow    = "low"

	MinSamples = 3
)

// FrequencyBand bands deploys per day: elite ≥ 1 a day (on demand), high
// ≥ 1 a week, medium ≥ 1 a month, else low.
func FrequencyBand(perDay float64) string {
	switch {
	case perDay >= 1:
		return BandElite
	case perDay >= 1.0/7:
		return BandHigh
	case perDay >= 1.0/30:
		return BandMedium
	}
	return BandLow
}

// LeadTimeBand bands a median lead time: elite < 1 day, high < 1 week,
// medium < 1 month (30 days), else low.
func LeadTimeBand(hours float64) string {
	switch {
	case hours < 24:
		return BandElite
	case hours < 7*24:
		return BandHigh
	case hours < 30*24:
		return BandMedium
	}
	return BandLow
}

// FailureRateBand bands a change failure rate in %: elite ≤ 5, high ≤ 10,
// medium ≤ 15, else low.
func FailureRateBand(pct float64) string {
	switch {
	case pct <= 5:
		return BandElite
	case pct <= 10:
		return BandHigh
	case pct <= 15:
		return BandMedium
	}
	return BandLow
}

// RestoreBand bands a median time to restore: elite < 1 hour, high < 1 day,
// medium < 1 week, else low.
func RestoreBand(hours float64) string {
	switch {
	case hours < 1:
		return BandElite
	case hours < 24:
		return BandHigh
	case hours < 7*24:
		return BandMedium
	}
	return BandLow
}

// Stat is a duration figure over its samples, in hours.
type Stat struct {
	N           int      `json:"n"`
	MedianHours *float64 `json:"median_hours"`
	P90Hours    *float64 `json:"p90_hours"`
	Band        string   `json:"band,omitempty"`
	Note        string   `json:"note,omitempty"`
}

// StatOf is the median and p90 (nearest rank) of hours, banded by band on
// the median when there are at least MinSamples.
func StatOf(hours []float64, band func(float64) string) Stat {
	s := Stat{N: len(hours)}
	if len(hours) == 0 {
		s.Note = "no sample"
		return s
	}
	v := append([]float64(nil), hours...)
	sort.Float64s(v)
	med := v[len(v)/2]
	if len(v)%2 == 0 {
		med = (v[len(v)/2-1] + v[len(v)/2]) / 2
	}
	p90 := v[int(math.Ceil(0.9*float64(len(v))))-1]
	s.MedianHours, s.P90Hours = ptr(round2(med)), ptr(round2(p90))
	if len(v) < MinSamples {
		s.Note = tooFew(len(v))
		return s
	}
	s.Band = band(med)
	return s
}

func tooFew(n int) string { return fmt.Sprintf("too few samples (%d, need %d)", n, MinSamples) }

func ptr(v float64) *float64 { return &v }

func round2(v float64) float64 { return math.Round(v*100) / 100 }
