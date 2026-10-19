package history

import "time"

// MaxSampleGap is how long one P&L point stands for at most. Points come
// every report interval (60 s); a longer gap is a bot that was down, its
// margin then unknown, so the gap is not counted.
const MaxSampleGap = 15 * time.Minute

// UtilisationStats is how much of the approved margin a bot used (#174).
type UtilisationStats struct {
	// MeanPct is the time-weighted mean of IM ÷ IM limit, in %, over the time
	// entries were allowed; nil when they never were.
	MeanPct *float64 `json:"mean_pct"`
	// AllowedShare is the share (0–1) of the measured time entries were
	// allowed; nil when nothing was measured.
	AllowedShare  *float64 `json:"allowed_share"`
	AllowedHours  float64  `json:"allowed_hours"`
	MeasuredHours float64  `json:"measured_hours"` // time covered by points with margin data
}

// Utilisation returns the margin utilisation over [from, to] from points
// ordered by time. Each point stands for the time until the next one (at
// most MaxSampleGap, never past to). Points without margin data (lines
// written before #174, or before the first margin read) are skipped, and so
// are allowed points without a positive IM limit.
func Utilisation(points []Point, from, to time.Time) UtilisationStats {
	var measured, allowed time.Duration
	var weighted float64 // Σ (IM ÷ limit) × seconds, while entries were allowed
	for i, p := range points {
		if p.EntriesAllowed == nil || p.T.Before(from) || !p.T.Before(to) {
			continue
		}
		end := p.T.Add(MaxSampleGap)
		if i+1 < len(points) && points[i+1].T.Before(end) {
			end = points[i+1].T
		}
		if end.After(to) {
			end = to
		}
		d := end.Sub(p.T)
		if d <= 0 {
			continue
		}
		if !*p.EntriesAllowed {
			measured += d
			continue
		}
		if p.IMPct == nil || p.IMLimitPct == nil || *p.IMLimitPct <= 0 {
			continue
		}
		measured += d
		allowed += d
		weighted += *p.IMPct / *p.IMLimitPct * d.Seconds()
	}
	out := UtilisationStats{AllowedHours: allowed.Hours(), MeasuredHours: measured.Hours()}
	if measured > 0 {
		share := allowed.Seconds() / measured.Seconds()
		out.AllowedShare = &share
	}
	if allowed > 0 {
		mean := 100 * weighted / allowed.Seconds()
		out.MeanPct = &mean
	}
	return out
}
