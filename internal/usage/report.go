package usage

import (
	"fmt"
	"slices"
	"sort"
	"time"
)

// NoEventsWhy says why a week can have no events at all.
const NoEventsWhy = "No events were recorded in the week. That can be because nobody has said yes to analytics yet " +
	"(the consent notice waits for E13's prototype), no screen of the monitor sends events yet (no instrumented screen), " +
	"ANALYTICS_SALT is not set (collection off), or nobody signed in to the monitor that week."

// MonthDays is the window of the monthly active count: the 30 days ending
// with the report's window.
const MonthDays = 30

// CohortWeeks is how many weeks after its first a cohort is followed (N+1 … N+4).
const CohortWeeks = 4

// Report is the usage report of one window (normally an ISO week). Counts
// and shares only: no user_id, no session_id.
type Report struct {
	Week         string    `json:"week,omitempty"`
	From         time.Time `json:"from"`
	To           time.Time `json:"to"` // exclusive
	Source       Source    `json:"source"`
	EventsInWeek int       `json:"events_in_week"`
	NoEvents     string    `json:"no_events,omitempty"` // why, when EventsInWeek is 0; every figure below is then absent

	Active       *Active       `json:"active,omitempty"`        // Q1, Q2
	Cohorts      []Cohort      `json:"cohorts,omitempty"`       // Q2
	Panels       *Panels       `json:"panels,omitempty"`        // Q3
	FirstAnswers *FirstAnswers `json:"first_answers,omitempty"` // Q4
	Help         *Help         `json:"help,omitempty"`          // Q5
	ManualClose  *ManualClose  `json:"manual_close,omitempty"`  // Q6
	Rebalance    *Rebalance    `json:"rebalance,omitempty"`     // Q6
	Trust        *Trust        `json:"trust,omitempty"`         // Q7
	Survey       *Survey       `json:"survey,omitempty"`        // Q8; absent when the window has no survey event
}

// Source is the events file and the span its accepted lines cover.
type Source struct {
	File          string         `json:"file"`
	Missing       bool           `json:"missing"`
	Lines         int            `json:"lines"`
	Accepted      int            `json:"accepted"`
	Rejected      map[string]int `json:"rejected"` // reason → lines; never used in a figure
	RejectedTotal int            `json:"rejected_total"`
	First         *time.Time     `json:"first,omitempty"` // first accepted event
	Last          *time.Time     `json:"last,omitempty"`
}

// Count is distinct users, in total and split by role and environment (a
// person whose role changed counts once in each; a line without an
// environment counts under "unknown").
type Count struct {
	Users         int            `json:"users"`
	ByRole        map[string]int `json:"by_role"`
	ByEnvironment map[string]int `json:"by_environment"`
}

// DayCount is one UTC day's active users.
type DayCount struct {
	Day string `json:"day"`
	Count
}

// Active is Q1/Q2's active users: anyone with an accepted event.
type Active struct {
	Days      []DayCount `json:"days"`
	Week      Count      `json:"week"`
	Month     Count      `json:"month"` // the MonthDays days ending with the window
	MonthFrom time.Time  `json:"month_from"`
}

// Cohort is the users whose first session_started fell in one week and how
// many of them came back (any event) in each later week.
type Cohort struct {
	Week     string      `json:"week"`
	From     time.Time   `json:"from"`
	Users    int         `json:"users"`
	Retained []Retention `json:"retained"` // weeks +1 … +CohortWeeks that ended by the window's end
}

// Retention is one later week of a cohort.
type Retention struct {
	Offset int      `json:"offset"` // weeks after the cohort's
	Users  int      `json:"users"`
	Pct    *float64 `json:"pct,omitempty"` // absent for an empty cohort
}

// Panels is Q3: what of the monitor is opened, in the window.
type Panels struct {
	Opens       map[string]int `json:"opens"`        // panel_expanded per panel
	NeverOpened []string       `json:"never_opened"` // panels with no panel_expanded
	Drill       map[string]int `json:"drill"`        // drill_opened per level
	Filters     map[string]int `json:"filters"`      // filter_changed per filter
	Screens     map[string]int `json:"screens"`      // screen_viewed per screen
}

// FirstAnswers is Q4: did a new person see E3's four answers in their first
// session, and how fast.
type FirstAnswers struct {
	FirstSessions   int      `json:"first_sessions"` // users whose first session_started is in the window
	WithAnswers     int      `json:"with_answers"`   // … with first_answers_seen in that session
	Pct             *float64 `json:"pct,omitempty"`
	MedianElapsedMs *int64   `json:"median_elapsed_ms,omitempty"`
}

// Help is Q5: tips and videos used, and whether they helped.
type Help struct {
	Tips            map[string]int `json:"tips"`             // tip_opened per tip_id
	VideosPlayed    map[string]int `json:"videos_played"`    // per video_id
	VideosCompleted map[string]int `json:"videos_completed"` // per video_id
	Answers         int            `json:"answers"`          // help_resolved
	Helped          int            `json:"helped"`           // … helped=true
	HelpedPct       *float64       `json:"helped_pct,omitempty"`
}

// ManualClose is Q6's manual-close funnel.
type ManualClose struct {
	Opened           int            `json:"opened"`
	OpenedAllowed    int            `json:"opened_allowed"`
	OpenedNotAllowed int            `json:"opened_not_allowed"`
	Cancelled        int            `json:"cancelled"`
	Confirmed        int            `json:"confirmed"`
	ConfirmedLastLeg int            `json:"confirmed_last_leg"`
	Results          map[string]int `json:"results"` // manual_close_result per outcome
}

// Rebalance is Q6's rebalance funnel.
type Rebalance struct {
	Previewed map[string]int `json:"previewed"` // per outcome
	Confirmed map[string]int `json:"confirmed"` // per outcome
}

// Trust is Q7: stale data and sign-outs under the person.
type Trust struct {
	Stale     map[string]int `json:"stale"`      // data_stale_shown per source
	SignedOut map[string]int `json:"signed_out"` // per reason
}

// BuildLog is Build on a loaded file, with its Source.
func BuildLog(lg Log, from, to time.Time) Report {
	r := Build(lg.Events, from, to)
	r.Source = SourceOf(lg)
	return r
}

// SourceOf describes a loaded file: its lines, the rejected ones by reason
// and the span of the accepted ones.
func SourceOf(lg Log) Source {
	s := Source{File: lg.Path, Missing: lg.Missing, Lines: lg.Lines, Accepted: len(lg.Events), Rejected: map[string]int{}}
	for k, n := range lg.Rejected {
		s.Rejected[k] = n
		s.RejectedTotal += n
	}
	for _, e := range lg.Events {
		t := e.TS
		if s.First == nil || t.Before(*s.First) {
			s.First = &t
		}
		if s.Last == nil || t.After(*s.Last) {
			s.Last = &t
		}
	}
	return s
}

// Build computes the report of [from, to) from accepted events. Events at
// or after `to` are ignored, so a report does not change when later lines
// are appended; earlier ones feed the month and the cohorts.
func Build(events []Event, from, to time.Time) Report {
	from, to = from.UTC(), to.UTC()
	r := Report{From: from, To: to}
	var past, week []Event
	for _, e := range events {
		if !e.TS.Before(to) {
			continue
		}
		past = append(past, e)
		if !e.TS.Before(from) {
			week = append(week, e)
		}
	}
	sort.SliceStable(past, func(i, j int) bool { return past[i].TS.Before(past[j].TS) })
	sort.SliceStable(week, func(i, j int) bool { return week[i].TS.Before(week[j].TS) })
	r.EventsInWeek = len(week)
	if len(week) == 0 {
		r.NoEvents = NoEventsWhy
		return r
	}
	r.Active = buildActive(past, from, to)
	r.Cohorts = buildCohorts(past, from, to)
	r.Panels = buildPanels(week)
	r.FirstAnswers = buildFirstAnswers(past, from)
	r.Help = buildHelp(week)
	r.ManualClose, r.Rebalance = buildFunnels(week)
	r.Trust = buildTrust(week)
	r.Survey = buildSurvey(week)
	return r
}

// countUsers counts the distinct users of events in [from, to).
func countUsers(events []Event, from, to time.Time) Count {
	users := map[string]bool{}
	byRole := map[string]map[string]bool{}
	byEnv := map[string]map[string]bool{}
	add := func(m map[string]map[string]bool, k, u string) {
		if m[k] == nil {
			m[k] = map[string]bool{}
		}
		m[k][u] = true
	}
	for _, e := range events {
		if e.TS.Before(from) || !e.TS.Before(to) {
			continue
		}
		users[e.UserID] = true
		add(byRole, e.Role, e.UserID)
		env := e.Environment
		if env == "" {
			env = "unknown"
		}
		add(byEnv, env, e.UserID)
	}
	c := Count{Users: len(users), ByRole: zeroMap(roles), ByEnvironment: zeroMap(environments)}
	for k, set := range byRole {
		c.ByRole[k] = len(set)
	}
	for k, set := range byEnv {
		c.ByEnvironment[k] = len(set)
	}
	return c
}

func buildActive(past []Event, from, to time.Time) *Active {
	a := &Active{
		Week:      countUsers(past, from, to),
		MonthFrom: to.AddDate(0, 0, -MonthDays),
	}
	a.Month = countUsers(past, a.MonthFrom, to)
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		end := d.AddDate(0, 0, 1)
		if end.After(to) {
			end = to
		}
		a.Days = append(a.Days, DayCount{Day: d.Format(time.DateOnly), Count: countUsers(past, d, end)})
	}
	return a
}

// buildCohorts follows the cohorts of the window's week and the CohortWeeks
// weeks before it; weeks are 7-day steps aligned on `from`.
func buildCohorts(past []Event, from, to time.Time) []Cohort {
	first := map[string]time.Time{} // user → first session_started
	for _, e := range past {
		if e.Name != "session_started" {
			continue
		}
		if t, ok := first[e.UserID]; !ok || e.TS.Before(t) {
			first[e.UserID] = e.TS
		}
	}
	const week = 7 * 24 * time.Hour
	var out []Cohort
	for k := -CohortWeeks; k <= 0; k++ {
		start := from.Add(time.Duration(k) * week)
		c := Cohort{Week: weekLabel(start), From: start}
		members := map[string]bool{}
		for u, t := range first {
			if !t.Before(start) && t.Before(start.Add(week)) {
				members[u] = true
			}
		}
		c.Users = len(members)
		for j := 1; j <= CohortWeeks; j++ {
			ws, we := start.Add(time.Duration(j)*week), start.Add(time.Duration(j+1)*week)
			if we.After(to) {
				break // that week has not ended
			}
			back := map[string]bool{}
			for _, e := range past {
				if members[e.UserID] && !e.TS.Before(ws) && e.TS.Before(we) {
					back[e.UserID] = true
				}
			}
			c.Retained = append(c.Retained, Retention{Offset: j, Users: len(back), Pct: pct(len(back), c.Users)})
		}
		out = append(out, c)
	}
	return out
}

func buildPanels(week []Event) *Panels {
	p := &Panels{
		Opens:   zeroMap(Properties["panel"].Values),
		Drill:   zeroMap(Properties["level"].Values),
		Filters: zeroMap(Properties["filter"].Values),
		Screens: zeroMap(Properties["screen"].Values),
	}
	for _, e := range week {
		switch e.Name {
		case "panel_expanded":
			inc(p.Opens, e.Props["panel"])
		case "drill_opened":
			inc(p.Drill, e.Props["level"])
		case "filter_changed":
			inc(p.Filters, e.Props["filter"])
		case "screen_viewed":
			inc(p.Screens, e.Props["screen"])
		}
	}
	p.NeverOpened = []string{}
	for _, panel := range Properties["panel"].Values {
		if p.Opens[panel] == 0 {
			p.NeverOpened = append(p.NeverOpened, panel)
		}
	}
	return p
}

// buildFirstAnswers: a user's first session is the session of their first
// session_started ever; counted when that start is in the window.
func buildFirstAnswers(past []Event, from time.Time) *FirstAnswers {
	type start struct {
		at      time.Time
		session string
	}
	first := map[string]start{}
	for _, e := range past {
		if e.Name != "session_started" {
			continue
		}
		if s, ok := first[e.UserID]; !ok || e.TS.Before(s.at) {
			first[e.UserID] = start{e.TS, e.SessionID}
		}
	}
	f := &FirstAnswers{}
	inWeek := map[string]string{} // user → first session id, for starts in the window
	for u, s := range first {
		if !s.at.Before(from) {
			inWeek[u] = s.session
		}
	}
	f.FirstSessions = len(inWeek)
	seen := map[string]int64{} // user → elapsed_ms of the first first_answers_seen in that session
	for _, e := range past {
		if e.Name != "first_answers_seen" || inWeek[e.UserID] == "" || inWeek[e.UserID] != e.SessionID {
			continue
		}
		if _, done := seen[e.UserID]; done {
			continue
		}
		ms := int64(-1)
		if v, ok := e.Props["elapsed_ms"].(float64); ok {
			ms = int64(v)
		}
		seen[e.UserID] = ms
	}
	f.WithAnswers = len(seen)
	f.Pct = pct(f.WithAnswers, f.FirstSessions)
	var elapsed []int64
	for _, ms := range seen {
		if ms >= 0 {
			elapsed = append(elapsed, ms)
		}
	}
	if len(elapsed) > 0 {
		m := median(elapsed)
		f.MedianElapsedMs = &m
	}
	return f
}

func buildHelp(week []Event) *Help {
	h := &Help{Tips: map[string]int{}, VideosPlayed: map[string]int{}, VideosCompleted: map[string]int{}}
	for _, e := range week {
		switch e.Name {
		case "tip_opened":
			inc(h.Tips, e.Props["tip_id"])
		case "video_played":
			inc(h.VideosPlayed, e.Props["video_id"])
		case "video_completed":
			inc(h.VideosCompleted, e.Props["video_id"])
		case "help_resolved":
			if v, ok := e.Props["helped"].(bool); ok {
				h.Answers++
				if v {
					h.Helped++
				}
			}
		}
	}
	h.HelpedPct = pct(h.Helped, h.Answers)
	return h
}

func buildFunnels(week []Event) (*ManualClose, *Rebalance) {
	outcomes := Properties["outcome"].Values
	mc := &ManualClose{Results: zeroMap(outcomes)}
	rb := &Rebalance{Previewed: zeroMap(outcomes), Confirmed: zeroMap(outcomes)}
	for _, e := range week {
		switch e.Name {
		case "manual_close_opened":
			mc.Opened++
			switch e.Props["allowed"] {
			case true:
				mc.OpenedAllowed++
			case false:
				mc.OpenedNotAllowed++
			}
		case "manual_close_cancelled":
			mc.Cancelled++
		case "manual_close_confirmed":
			mc.Confirmed++
			if e.Props["last_leg"] == true {
				mc.ConfirmedLastLeg++
			}
		case "manual_close_result":
			inc(mc.Results, e.Props["outcome"])
		case "rebalance_previewed":
			inc(rb.Previewed, e.Props["outcome"])
		case "rebalance_confirmed":
			inc(rb.Confirmed, e.Props["outcome"])
		}
	}
	return mc, rb
}

func buildTrust(week []Event) *Trust {
	t := &Trust{Stale: zeroMap(Properties["source"].Values), SignedOut: zeroMap(Properties["reason"].Values)}
	for _, e := range week {
		switch e.Name {
		case "data_stale_shown":
			inc(t.Stale, e.Props["source"])
		case "signed_out":
			inc(t.SignedOut, e.Props["reason"])
		}
	}
	return t
}

// inc counts an allow-listed string value; an event without the property
// counts under "unset".
func inc(m map[string]int, v any) {
	s, ok := v.(string)
	if !ok {
		s = "unset"
	}
	m[s]++
}

func zeroMap(keys []string) map[string]int {
	m := make(map[string]int, len(keys))
	for _, k := range keys {
		m[k] = 0
	}
	return m
}

func pct(n, of int) *float64 {
	if of == 0 {
		return nil
	}
	v := 100 * float64(n) / float64(of)
	return &v
}

func median(v []int64) int64 {
	s := slices.Clone(v)
	slices.Sort(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// weekLabel is the ISO week of a Monday ("2026-W42"); another start is
// labelled by its date.
func weekLabel(start time.Time) string {
	if start.Weekday() == time.Monday && start.Hour() == 0 && start.Minute() == 0 {
		y, w := start.ISOWeek()
		return fmt.Sprintf("%d-W%02d", y, w)
	}
	return start.Format(time.DateOnly)
}
