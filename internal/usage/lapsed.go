package usage

import (
	"strconv"
	"time"
)

// LapsedLookbackWeeks is how many weeks before the window a person must have
// visited in to count as lapsed when they did not visit in it (#569).
const LapsedLookbackWeeks = 4

// LapsedWeeksAway are the weeks_away keys: weeks since the last visit, the
// last one meaning that many or more.
var LapsedWeeksAway = []string{"1", "2", "3", "4+"}

// Lapsed is who stopped coming back (#569): people with a session_started in
// the LapsedLookbackWeeks weeks before the window and none in it. Counts
// only, never a user id.
type Lapsed struct {
	Users     int            `json:"users"`
	ByRole    map[string]int `json:"by_role"`    // the role of each person's last visit
	WeeksAway map[string]int `json:"weeks_away"` // weeks since the last visit: 1, 2, 3, 4+
	Base      int            `json:"base"`       // people with a visit in the lookback weeks
	SharePct  *float64       `json:"share_pct,omitempty"`
	Note      string         `json:"note,omitempty"` // TooFew under DropMinBase people in the base: no share then
}

// buildLapsed counts the lapsed people of [from, to) from the events before
// `to`. A visit is a session_started; weeks are 7-day steps back from `from`.
func buildLapsed(past []Event, from, to time.Time) *Lapsed {
	const week = 7 * 24 * time.Hour
	since := from.Add(-LapsedLookbackWeeks * week)
	type visit struct {
		at   time.Time
		role string
	}
	last := map[string]visit{} // user → last visit in the lookback weeks
	back := map[string]bool{}  // users with a visit in the window
	for _, e := range past {
		if e.Name != "session_started" || e.TS.Before(since) || !e.TS.Before(to) {
			continue
		}
		if !e.TS.Before(from) {
			back[e.UserID] = true
			continue
		}
		if v, ok := last[e.UserID]; !ok || !e.TS.Before(v.at) {
			last[e.UserID] = visit{e.TS, e.Role}
		}
	}
	l := &Lapsed{ByRole: zeroMap(roles), WeeksAway: zeroMap(LapsedWeeksAway), Base: len(last)}
	for u, v := range last {
		if back[u] {
			continue
		}
		l.Users++
		l.ByRole[v.role]++
		l.WeeksAway[weeksAwayKey(from.Sub(v.at))]++
	}
	if l.Base < DropMinBase {
		l.Note = TooFew
	} else {
		l.SharePct = pct(l.Users, l.Base)
	}
	return l
}

// weeksAwayKey: a visit in the week just before the window is 1 week away.
func weeksAwayKey(d time.Duration) string {
	const week = 7 * 24 * time.Hour
	n := int((d-1)/week) + 1
	if n >= LapsedLookbackWeeks {
		return LapsedWeeksAway[len(LapsedWeeksAway)-1]
	}
	return strconv.Itoa(n)
}
