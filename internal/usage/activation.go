package usage

import (
	"fmt"
	"time"
)

// Activation (time-to-value, #620): did a new person reach a useful answer
// within a day of their first session, and how fast? From events the plan
// already collects (event-plan.md §8), so only people who said yes to
// analytics are counted. Fixed in code.
const (
	// ActivationWeeks is how many weeks the measure covers, the report's last.
	ActivationWeeks = 12
	// ActivationWindow: the steps count when they come within this long of
	// the person's first session_started.
	ActivationWindow = 24 * time.Hour
	// ActivationMinUsers: a week's share is given from this many new people.
	ActivationMinUsers = DropMinBase
)

// The activating steps, done after the Overview.
const (
	StepWorstCase   = "worst_case"   // screen_viewed worst_case: the risk / worst-case view
	StepTradeReason = "trade_reason" // assistant_open why (Why? on a trade) or details_trade_expand
	StepHelp        = "help"         // tip_opened, assistant_open menu or assistant_ask
)

// ActivationSteps are the step keys, in the plan's order.
var ActivationSteps = []string{StepWorstCase, StepTradeReason, StepHelp}

// Activation is the measure over the last ActivationWeeks weeks, oldest
// first. Counts and shares only, never a user id.
type Activation struct {
	Weeks       []ActivationWeek `json:"weeks"`
	WindowHours int              `json:"window_hours"` // ActivationWindow
	MinUsers    int              `json:"min_users"`    // ActivationMinUsers
}

// ActivationWeek is the people whose first session_started fell in one week.
type ActivationWeek struct {
	Week          string         `json:"week"`
	From          time.Time      `json:"from"`
	NoEvents      bool           `json:"no_events,omitempty"` // no event at all in the week: noted, never a zero
	Users         int            `json:"users"`               // new people
	Overview      int            `json:"overview"`            // … who opened the Overview within the window
	Activated     int            `json:"activated"`           // … and then did an activating step
	ByStep        map[string]int `json:"by_step"`             // the step that activated each person
	Pct           *float64       `json:"pct,omitempty"`       // activated ÷ users; absent under MinUsers
	MedianMinutes *float64       `json:"median_minutes,omitempty"`
	Note          string         `json:"note,omitempty"` // TooFew under MinUsers new people
}

// activationStep is the step an event is, or "" when it is none.
func activationStep(e Event) string {
	switch e.Name {
	case "screen_viewed":
		if e.Props["screen"] == "worst_case" {
			return StepWorstCase
		}
	case "details_trade_expand":
		return StepTradeReason
	case "assistant_open":
		if e.Props["entry"] == "why" {
			return StepTradeReason
		}
		return StepHelp
	case "assistant_ask", "tip_opened":
		return StepHelp
	}
	return ""
}

// firstStarts maps each user to their first session_started.
func firstStarts(past []Event) map[string]time.Time {
	first := map[string]time.Time{}
	for _, e := range past {
		if e.Name != "session_started" {
			continue
		}
		if t, ok := first[e.UserID]; !ok || e.TS.Before(t) {
			first[e.UserID] = e.TS
		}
	}
	return first
}

// buildActivation measures the window's week and the ActivationWeeks-1
// before it (7-day steps aligned on `from`, as the cohorts) from the events
// before `to`, which are sorted by time. A person counts as activated when,
// within ActivationWindow of their first session_started, they viewed the
// Overview and then did an activating step; time-to-value is the minutes
// from that start to the step.
func buildActivation(past []Event, from time.Time) *Activation {
	const week = 7 * 24 * time.Hour
	first := firstStarts(past)
	type progress struct {
		overview  bool
		activated bool
		step      string
		minutes   float64
	}
	people := map[string]*progress{}
	for _, e := range past {
		t0, ok := first[e.UserID]
		if !ok || e.TS.Before(t0) || !e.TS.Before(t0.Add(ActivationWindow)) {
			continue
		}
		p := people[e.UserID]
		if p == nil {
			p = &progress{}
			people[e.UserID] = p
		}
		switch {
		case p.activated:
		case e.Name == "screen_viewed" && e.Props["screen"] == "overview":
			p.overview = true
		case p.overview && activationStep(e) != "":
			p.activated, p.step, p.minutes = true, activationStep(e), e.TS.Sub(t0).Minutes()
		}
	}
	a := &Activation{WindowHours: int(ActivationWindow / time.Hour), MinUsers: ActivationMinUsers}
	for k := -(ActivationWeeks - 1); k <= 0; k++ {
		start := from.Add(time.Duration(k) * week)
		end := start.Add(week)
		w := ActivationWeek{Week: weekLabel(start), From: start, ByStep: zeroMap(ActivationSteps), NoEvents: !anyEventIn(past, start, end)}
		var minutes []float64
		for u, t0 := range first {
			if t0.Before(start) || !t0.Before(end) {
				continue
			}
			w.Users++
			p := people[u]
			if p == nil || !p.overview {
				continue
			}
			w.Overview++
			if p.activated {
				w.Activated++
				w.ByStep[p.step]++
				minutes = append(minutes, p.minutes)
			}
		}
		if w.Users < ActivationMinUsers {
			w.Note = TooFew
		} else {
			w.Pct = pct(w.Activated, w.Users)
			if len(minutes) > 0 {
				m := median(minutes)
				w.MedianMinutes = &m
			}
		}
		a.Weeks = append(a.Weeks, w)
	}
	return a
}

func anyEventIn(events []Event, from, to time.Time) bool {
	for _, e := range events {
		if !e.TS.Before(from) && e.TS.Before(to) {
			return true
		}
	}
	return false
}

// Line is the week in one line, as cmd/usage prints it: counts, the share
// and the median, or why there are none.
func (w ActivationWeek) Line() string {
	switch {
	case w.NoEvents:
		return fmt.Sprintf("activation %s: no events in the week (not counted as zero)", w.Week)
	case w.Note != "":
		return fmt.Sprintf("activation %s: %d of %d new people activated — %s (under %d)", w.Week, w.Activated, w.Users, w.Note, ActivationMinUsers)
	}
	med := "—"
	if w.MedianMinutes != nil {
		med = fmt.Sprintf("%.1f min", *w.MedianMinutes)
	}
	return fmt.Sprintf("activation %s: %d of %d new people activated (%s), median %s to the first answer", w.Week, w.Activated, w.Users, fmtPct(w.Pct), med)
}
