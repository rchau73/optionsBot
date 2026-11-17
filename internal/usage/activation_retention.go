package usage

import (
	"fmt"
	"time"
)

// Retention by activation (Q9, #818, event-plan.md §9): do the new people
// activated in their first day (the §8 rule, unchanged) come back more often
// than the others? Each of the last ActivationWeeks cohorts is split in two
// and followed for ActivationReturnWeeks weeks, with "active" as the cohort
// table's (any accepted event). An association, not proof; no alert reads it.
// Counts and shares only, never a user id.

// ActivationReturnWeeks is how many weeks after its own a cohort is followed.
const ActivationReturnWeeks = 4

// ActivationHeadlineWeek is the week after the cohort's that the one-line
// summary (cmd/usage, the E13 comment) reads: the card's "week-4 return".
const ActivationHeadlineWeek = 4

// AssociationNote is said beside every figure of the measure.
const AssociationNote = "association, not proof"

// NotYet marks a later week that has not ended: never counted as zero.
const NotYet = "not yet"

// ActivationRetention is the measure over the last ActivationWeeks cohorts,
// oldest first, and the cohorts pooled per later week.
type ActivationRetention struct {
	Weeks       []ActivationCohort `json:"weeks"`
	Pooled      []PooledReturn     `json:"pooled"` // weeks +1 … +ActivationReturnWeeks over every cohort that has it
	WindowHours int                `json:"window_hours"`
	MinUsers    int                `json:"min_users"` // ActivationMinUsers: a group's share is given from this many people
	Note        string             `json:"note"`      // AssociationNote
}

// ActivationCohort is the people whose first session_started fell in one
// week, split by activation.
type ActivationCohort struct {
	Week         string      `json:"week"`
	From         time.Time   `json:"from"`
	NoEvents     bool        `json:"no_events,omitempty"` // no event at all in the week: noted and left out
	Activated    ReturnGroup `json:"activated"`
	NotActivated ReturnGroup `json:"not_activated"`
}

// ReturnGroup is one half of a cohort and who of it came back each week.
type ReturnGroup struct {
	Users   int          `json:"users"`
	Note    string       `json:"note,omitempty"` // TooFew under MinUsers people: no share
	Returns []WeekReturn `json:"returns"`        // weeks +1 … +ActivationReturnWeeks
}

// WeekReturn is one later week of a group.
type WeekReturn struct {
	Offset   int      `json:"offset"`
	NotYet   bool     `json:"not_yet,omitempty"`   // the week has not ended: no count
	NoEvents bool     `json:"no_events,omitempty"` // no event at all in that week: no count, never a zero
	Users    int      `json:"users"`
	Pct      *float64 `json:"pct,omitempty"` // absent under MinUsers, not yet or no events
}

// PooledReturn is one later week over every cohort where it has ended and
// had events: the people of those cohorts in each group, and who came back.
type PooledReturn struct {
	Offset           int      `json:"offset"`
	Activated        int      `json:"activated"` // base: activated people of those cohorts
	ActivatedBack    int      `json:"activated_back"`
	ActivatedPct     *float64 `json:"activated_pct,omitempty"` // absent under MinUsers
	NotActivated     int      `json:"not_activated"`
	NotActivatedBack int      `json:"not_activated_back"`
	NotActivatedPct  *float64 `json:"not_activated_pct,omitempty"`
	ActivatedNote    string   `json:"activated_note,omitempty"`     // TooFew
	NotActivatedNote string   `json:"not_activated_note,omitempty"` // TooFew
	Cohorts          int      `json:"cohorts"`                      // cohorts pooled
}

// buildActivationRetention follows the window's week and the
// ActivationWeeks-1 before it (7-day steps aligned on `from`) from the
// events before `to`, which are sorted by time.
func buildActivationRetention(past []Event, from, to time.Time) *ActivationRetention {
	const week = 7 * 24 * time.Hour
	first := firstStarts(past)
	people := activationProgress(past, first)
	ar := &ActivationRetention{WindowHours: int(ActivationWindow / time.Hour), MinUsers: ActivationMinUsers, Note: AssociationNote}
	for j := 1; j <= ActivationReturnWeeks; j++ {
		ar.Pooled = append(ar.Pooled, PooledReturn{Offset: j})
	}
	for k := -(ActivationWeeks - 1); k <= 0; k++ {
		start := from.Add(time.Duration(k) * week)
		c := ActivationCohort{Week: weekLabel(start), From: start, NoEvents: !anyEventIn(past, start, start.Add(week))}
		activated, other := map[string]bool{}, map[string]bool{}
		for u, t0 := range first {
			if t0.Before(start) || !t0.Before(start.Add(week)) {
				continue
			}
			if p := people[u]; p != nil && p.activated {
				activated[u] = true
			} else {
				other[u] = true
			}
		}
		for j := 1; j <= ActivationReturnWeeks; j++ {
			ws, we := start.Add(time.Duration(j)*week), start.Add(time.Duration(j+1)*week)
			r := WeekReturn{Offset: j, NotYet: we.After(to)}
			if !r.NotYet {
				r.NoEvents = !anyEventIn(past, ws, we)
			}
			ra, ro := r, r
			if !r.NotYet && !r.NoEvents {
				back := usersIn(past, ws, we)
				ra.Users, ro.Users = countIn(back, activated), countIn(back, other)
				if !c.NoEvents && len(activated)+len(other) > 0 {
					p := &ar.Pooled[j-1]
					p.Cohorts++
					p.Activated += len(activated)
					p.ActivatedBack += ra.Users
					p.NotActivated += len(other)
					p.NotActivatedBack += ro.Users
				}
			}
			c.Activated.Returns = append(c.Activated.Returns, ra)
			c.NotActivated.Returns = append(c.NotActivated.Returns, ro)
		}
		c.Activated.Users, c.NotActivated.Users = len(activated), len(other)
		c.Activated.judge()
		c.NotActivated.judge()
		ar.Weeks = append(ar.Weeks, c)
	}
	for i := range ar.Pooled {
		p := &ar.Pooled[i]
		p.ActivatedPct, p.ActivatedNote = shareOrTooFew(p.ActivatedBack, p.Activated)
		p.NotActivatedPct, p.NotActivatedNote = shareOrTooFew(p.NotActivatedBack, p.NotActivated)
	}
	return ar
}

// judge gives each ended week's share, or marks the group too few to judge.
func (g *ReturnGroup) judge() {
	if g.Users < ActivationMinUsers {
		g.Note = TooFew
		return
	}
	for i := range g.Returns {
		if r := &g.Returns[i]; !r.NotYet && !r.NoEvents {
			r.Pct = pct(r.Users, g.Users)
		}
	}
}

func shareOrTooFew(n, of int) (*float64, string) {
	if of < ActivationMinUsers {
		return nil, TooFew
	}
	return pct(n, of), ""
}

// usersIn is the set of users with an event in [from, to).
func usersIn(events []Event, from, to time.Time) map[string]bool {
	set := map[string]bool{}
	for _, e := range events {
		if !e.TS.Before(from) && e.TS.Before(to) {
			set[e.UserID] = true
		}
	}
	return set
}

func countIn(set, of map[string]bool) int {
	n := 0
	for u := range of {
		if set[u] {
			n++
		}
	}
	return n
}

// Headline is the pooled week ActivationHeadlineWeek in one line, as
// cmd/usage prints it: both shares and both bases, or why there are none.
func (ar *ActivationRetention) Headline() string {
	p := ar.Pooled[ActivationHeadlineWeek-1]
	if p.Cohorts == 0 {
		return fmt.Sprintf("retention by activation: no cohort has an ended week %d yet (%s)", p.Offset, AssociationNote)
	}
	side := func(v *float64, note string) string {
		if note != "" {
			return note
		}
		return fmtPct(v)
	}
	return fmt.Sprintf("retention by activation: activated come back %s in week %d, not activated %s (n = %d / %d; %s)",
		side(p.ActivatedPct, p.ActivatedNote), p.Offset, side(p.NotActivatedPct, p.NotActivatedNote), p.Activated, p.NotActivated, AssociationNote)
}
