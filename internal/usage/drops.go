package usage

import (
	"fmt"
	"slices"
	"time"
)

// The engagement-drop rules (#341, #252). Fixed in code; cmd/usage's -alert-*
// flags vary them to study them.
const (
	// DropBaseWeeks is how many weeks before the report's are its base.
	DropBaseWeeks = 3
	// DropCountPct: a count (weekly active users, help used) is a drop when
	// it is at least this much under the mean of the base weeks, in %.
	DropCountPct = 30.0
	// DropFunnelPts: a funnel is a drop when its completion share is at least
	// this many percentage points under the base weeks' share.
	DropFunnelPts = 20.0
	// DropMinBase: a rule judges only a base of at least this many users (a
	// count) or funnel starts (in the base weeks and in the week itself).
	DropMinBase = 10
)

// TooFew is the reason a rule raised nothing on a small base.
const TooFew = "too few to judge"

// The metrics the drop check watches.
const (
	MetricActive       = "active_users"  // weekly active users
	MetricHelp         = "help_used"     // tips opened + videos played
	MetricFirstAnswers = "first_answers" // first sessions that saw the four answers
	MetricManualClose  = "manual_close"  // manual-close dialogs confirmed ÷ opened
	MetricRebalance    = "rebalance"     // rebalances confirmed ÷ previewed
)

// Rules are the thresholds of the drop check.
type Rules struct {
	CountPct  float64 // DropCountPct
	FunnelPts float64 // DropFunnelPts
	MinBase   int     // DropMinBase
}

// DefaultRules are the fixed rules.
func DefaultRules() Rules {
	return Rules{CountPct: DropCountPct, FunnelPts: DropFunnelPts, MinBase: DropMinBase}
}

// Drop is one metric that fell past its rule. Counts are people or events;
// funnel figures are completion shares in %. ChangePct is the count's change
// in %, ChangePts the funnel share's change in points.
type Drop struct {
	Metric    string  `json:"metric"`
	Kind      string  `json:"kind"` // count | funnel
	Current   float64 `json:"current"`
	Base      float64 `json:"base"` // mean of the base weeks (count) or their pooled share (funnel)
	ChangePct float64 `json:"change_pct,omitempty"`
	ChangePts float64 `json:"change_pts,omitempty"`
	Starts    int     `json:"starts,omitempty"`      // funnel starts in the week
	BaseStart int     `json:"base_starts,omitempty"` // funnel starts in the base weeks
}

// Skip is a metric the check did not judge, and why.
type Skip struct {
	Metric string `json:"metric"`
	Reason string `json:"reason"`
}

// WeekFigure is one week the check read: its active users and whether it
// had any event (a week without is noted, never counted as zero).
type WeekFigure struct {
	Week        string `json:"week"`
	ActiveUsers int    `json:"active_users"`
	Missing     bool   `json:"missing"`
}

// Alert is the drop check of one week against the weeks before it.
type Alert struct {
	Week    string       `json:"week"`
	Rules   Rules        `json:"rules"`
	Weeks   []WeekFigure `json:"weeks"`   // the base weeks, oldest first, then the week itself
	Missing []string     `json:"missing"` // weeks with no event: in no base, nothing raised for them
	Drops   []Drop       `json:"drops"`
	Skipped []Skip       `json:"skipped"`
}

// Drops compares the week's report with the reports of the weeks before it
// (any order of `previous`, normally DropBaseWeeks of them). Pure. A week
// with no event is listed in Missing and left out of every base; when the
// week itself has none nothing is judged.
func Drops(current Report, previous []Report, rules Rules) Alert {
	a := Alert{Week: labelOf(current), Rules: rules, Missing: []string{}, Drops: []Drop{}, Skipped: []Skip{}}
	var base []Report
	for _, p := range sortByFrom(previous) {
		f := WeekFigure{Week: labelOf(p), Missing: p.EventsInWeek == 0}
		if f.Missing {
			a.Missing = append(a.Missing, f.Week)
		} else {
			f.ActiveUsers = p.Active.Week.Users
			base = append(base, p)
		}
		a.Weeks = append(a.Weeks, f)
	}
	cur := WeekFigure{Week: a.Week, Missing: current.EventsInWeek == 0}
	if !cur.Missing {
		cur.ActiveUsers = current.Active.Week.Users
	}
	a.Weeks = append(a.Weeks, cur)
	if cur.Missing {
		a.Missing = append(a.Missing, cur.Week)
		for _, m := range []string{MetricActive, MetricHelp, MetricFirstAnswers, MetricManualClose, MetricRebalance} {
			a.Skipped = append(a.Skipped, Skip{m, "no events in the week"})
		}
		return a
	}
	if len(base) == 0 {
		for _, m := range []string{MetricActive, MetricHelp, MetricFirstAnswers, MetricManualClose, MetricRebalance} {
			a.Skipped = append(a.Skipped, Skip{m, "no earlier week with events"})
		}
		return a
	}

	count := func(metric string, of func(Report) int) {
		sum := 0
		for _, r := range base {
			sum += of(r)
		}
		mean := float64(sum) / float64(len(base))
		if mean < float64(rules.MinBase) {
			a.Skipped = append(a.Skipped, Skip{metric, TooFew})
			return
		}
		now := float64(of(current))
		change := 100 * (now - mean) / mean
		if -change >= rules.CountPct {
			a.Drops = append(a.Drops, Drop{Metric: metric, Kind: "count", Current: now, Base: mean, ChangePct: change})
		}
	}
	funnel := func(metric string, of func(Report) (done, starts int)) {
		baseDone, baseStarts := 0, 0
		for _, r := range base {
			d, s := of(r)
			baseDone += d
			baseStarts += s
		}
		done, starts := of(current)
		if baseStarts < rules.MinBase || starts < rules.MinBase {
			a.Skipped = append(a.Skipped, Skip{metric, TooFew})
			return
		}
		was := 100 * float64(baseDone) / float64(baseStarts)
		now := 100 * float64(done) / float64(starts)
		if was-now >= rules.FunnelPts {
			a.Drops = append(a.Drops, Drop{Metric: metric, Kind: "funnel", Current: now, Base: was,
				ChangePts: now - was, Starts: starts, BaseStart: baseStarts})
		}
	}

	count(MetricActive, func(r Report) int { return r.Active.Week.Users })
	count(MetricHelp, func(r Report) int { return sumCounts(r.Help.Tips) + sumCounts(r.Help.VideosPlayed) })
	funnel(MetricFirstAnswers, func(r Report) (int, int) { return r.FirstAnswers.WithAnswers, r.FirstAnswers.FirstSessions })
	funnel(MetricManualClose, func(r Report) (int, int) { return r.ManualClose.Confirmed, r.ManualClose.Opened })
	funnel(MetricRebalance, func(r Report) (int, int) {
		return sumCounts(r.Rebalance.Confirmed), sumCounts(r.Rebalance.Previewed)
	})
	return a
}

// BuildWeeks builds the reports of the n (normally DropBaseWeeks) 7-day windows
// before [from, from+7 d), oldest first, from the same events.
func BuildWeeks(events []Event, from time.Time, n int) []Report {
	const week = 7 * 24 * time.Hour
	out := make([]Report, 0, n)
	for k := n; k >= 1; k-- {
		start := from.Add(-time.Duration(k) * week)
		r := Build(events, start, start.Add(week))
		r.Week = weekLabel(start)
		out = append(out, r)
	}
	return out
}

func labelOf(r Report) string {
	if r.Week != "" {
		return r.Week
	}
	return weekLabel(r.From)
}

func sortByFrom(rs []Report) []Report {
	out := slices.Clone(rs)
	slices.SortStableFunc(out, func(x, y Report) int { return x.From.Compare(y.From) })
	return out
}

func sumCounts(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

// String is the drop in one line, counts and shares only.
func (d Drop) String() string {
	if d.Kind == "funnel" {
		return fmt.Sprintf("%s: %.0f %% of %d starts, was %.0f %% of %d (%+.0f pts)", d.Metric, d.Current, d.Starts, d.Base, d.BaseStart, d.ChangePts)
	}
	return fmt.Sprintf("%s: %.0f, mean of the base weeks %.1f (%+.0f %%)", d.Metric, d.Current, d.Base, d.ChangePct)
}
