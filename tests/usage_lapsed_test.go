package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/usage"
)

// Who stopped coming back (#569): the Lapsed block of the weekly usage report
// and its lapsed_share drop rule.

const lapsedWeek = 7 * 24 * time.Hour

// lapsedFrom is the window's Monday (2026-W44).
var lapsedFrom = time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)

// visit is user n's session_started k weeks before the window (k = 0: in it).
func visit(n, weeksBefore int, role string) usage.Event {
	uid, sid := usageUser(n)
	return usage.Event{Name: "session_started", TS: lapsedFrom.Add(-time.Duration(weeksBefore)*lapsedWeek + 2*time.Hour),
		UserID: uid, SessionID: sid, Role: role, Environment: "mvp"}
}

func buildLapsed(t *testing.T, events []usage.Event) *usage.Lapsed {
	t.Helper()
	r := usage.Build(events, lapsedFrom, lapsedFrom.Add(lapsedWeek))
	if r.Lapsed == nil {
		t.Fatal("no Lapsed block in a week with events")
	}
	return r.Lapsed
}

func TestUsage_LapsedVisitedLastWeekNotThisWeek(t *testing.T) {
	// Scenario: a person who visited last week but not this week is lapsed.
	events := []usage.Event{visit(1, 1, "user"), visit(2, 0, "user")}
	l := buildLapsed(t, events)
	if l.Users != 1 || l.WeeksAway["1"] != 1 || l.Base != 1 || l.ByRole["user"] != 1 {
		t.Fatalf("lapsed %+v, want user 1 once, 1 week away", l)
	}
	b, err := json.Marshal(usage.Build(events, lapsedFrom, lapsedFrom.Add(lapsedWeek)))
	if err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 2; n++ {
		uid, sid := usageUser(n)
		if bytes.Contains(b, []byte(uid)) || bytes.Contains(b, []byte(sid)) {
			t.Fatalf("report holds user %d's id", n)
		}
	}
}

func TestUsage_LapsedReturnedIsNotLapsed(t *testing.T) {
	// Scenario: a person who returns is not lapsed.
	l := buildLapsed(t, []usage.Event{visit(1, 3, "user"), visit(1, 0, "user")})
	if l.Users != 0 || l.Base != 1 {
		t.Fatalf("lapsed %+v, want 0 of a base of 1", l)
	}
}

func TestUsage_LapsedWeeksAwayAndRoles(t *testing.T) {
	// The last visit names the weeks away and the role; a visit older than the
	// lookback weeks is in no base; the window's first second is "in" it.
	events := []usage.Event{
		visit(1, 1, "user"), visit(2, 2, "manager"), visit(3, 3, "user"), visit(4, 4, "admin"),
		visit(5, 4, "user"), visit(5, 2, "manager"), // last visit 2 weeks before, as a manager
		visit(6, 5, "user"), // outside the lookback
		visit(7, 1, "user"),
	}
	uid, sid := usageUser(7)
	events = append(events, usage.Event{Name: "session_started", TS: lapsedFrom, UserID: uid, SessionID: sid, Role: "user"})
	// The exact start of the week before is still 1 week away.
	uid, sid = usageUser(8)
	events = append(events, usage.Event{Name: "session_started", TS: lapsedFrom.Add(-lapsedWeek), UserID: uid, SessionID: sid, Role: "user"})
	l := buildLapsed(t, events)
	want := map[string]int{"1": 2, "2": 2, "3": 1, "4+": 1}
	for k, n := range want {
		if l.WeeksAway[k] != n {
			t.Errorf("weeks_away[%s] = %d, want %d (%v)", k, l.WeeksAway[k], n, l.WeeksAway)
		}
	}
	if l.Users != 6 || l.Base != 7 {
		t.Errorf("users %d of base %d, want 6 of 7", l.Users, l.Base)
	}
	if l.ByRole["manager"] != 2 || l.ByRole["admin"] != 1 || l.ByRole["user"] != 3 {
		t.Errorf("by_role %v", l.ByRole)
	}
}

// lapsedEvents: `base` people visited 1 week before, `lapsed` of them not in the window.
func lapsedEvents(base, lapsed, offset int) []usage.Event {
	var ev []usage.Event
	for n := 0; n < base; n++ {
		ev = append(ev, visit(offset+n, 1, "user"))
		if n >= lapsed {
			ev = append(ev, visit(offset+n, 0, "user"))
		}
	}
	return ev
}

func TestUsage_LapsedSmallBaseGivesANote(t *testing.T) {
	// Scenario: a small base gives a note, not a percentage.
	l := buildLapsed(t, lapsedEvents(usage.DropMinBase-1, 5, 1))
	if l.Note != usage.TooFew || l.SharePct != nil || l.Base != usage.DropMinBase-1 || l.Users != 5 {
		t.Fatalf("small base: %+v", l)
	}
	var md strings.Builder
	if err := usage.Markdown(&md, usage.Build(lapsedEvents(usage.DropMinBase-1, 5, 1), lapsedFrom, lapsedFrom.Add(lapsedWeek))); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md.String(), "## Lapsed (Q2)") || !strings.Contains(md.String(), "too few to judge: 9 people in the base, under 10") {
		t.Fatalf("markdown lacks the small-sample note:\n%s", md.String())
	}
	if !strings.Contains(md.String(), "People who visited in the last 4 weeks and did not visit this week") {
		t.Fatal("markdown lacks the plain-words line")
	}
	// No lapsed_share alert on a small base, even when 8 of its 9 lapsed.
	cur := usage.Build(lapsedEvents(usage.DropMinBase-1, usage.DropMinBase-2, 1), lapsedFrom, lapsedFrom.Add(lapsedWeek))
	prev := lapsedReport(t, 30, 0, -1)
	a := usage.Drops(cur, []usage.Report{prev}, usage.DefaultRules())
	if hasDrop(a, usage.MetricLapsed) || skipReason(a, usage.MetricLapsed) != usage.TooFew {
		t.Fatalf("small base raised or judged lapsed_share: %+v", a)
	}

	big := buildLapsed(t, lapsedEvents(20, 5, 1))
	if big.Note != "" || big.SharePct == nil || *big.SharePct != 25 {
		t.Fatalf("base of 20: %+v", big)
	}
}

// lapsedReport is a week `weeksFromNow` from the window's with `base` people
// in its lookback and `lapsed` of them lapsed.
func lapsedReport(t *testing.T, base, lapsed, weeksFromNow int) usage.Report {
	t.Helper()
	from := lapsedFrom.Add(time.Duration(weeksFromNow) * lapsedWeek)
	var ev []usage.Event
	for n := 0; n < base; n++ {
		uid, sid := usageUser(1000 + n)
		ev = append(ev, usage.Event{Name: "session_started", TS: from.Add(-time.Hour), UserID: uid, SessionID: sid, Role: "user"})
		if n >= lapsed {
			ev = append(ev, usage.Event{Name: "session_started", TS: from.Add(time.Hour), UserID: uid, SessionID: sid, Role: "user"})
		}
	}
	if lapsed == base { // a week with events, yet everyone lapsed
		uid, sid := usageUser(9999)
		ev = append(ev, usage.Event{Name: "session_started", TS: from.Add(time.Hour), UserID: uid, SessionID: sid, Role: "user"})
	}
	r := usage.Build(ev, from, from.Add(lapsedWeek))
	r.Week = fmt.Sprintf("W%+d", weeksFromNow)
	return r
}

func hasDrop(a usage.Alert, metric string) bool {
	for _, d := range a.Drops {
		if d.Metric == metric {
			return true
		}
	}
	return false
}

func TestUsage_LapsedShareRisingIsADrop(t *testing.T) {
	// The lapsed share rising FunnelPts or more over the base weeks' is a drop;
	// a smaller rise or a base report without Lapsed (before #569) is not.
	base := []usage.Report{lapsedReport(t, 20, 2, -3), lapsedReport(t, 20, 2, -2), lapsedReport(t, 20, 2, -1)} // 10 %
	cases := []struct {
		name   string
		lapsed int
		want   bool
	}{
		{"rose 25 pts", 7, true},  // 35 %
		{"rose 20 pts", 6, true},  // 30 %: at the rule
		{"rose 15 pts", 5, false}, // 25 %
		{"fell", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := usage.Drops(lapsedReport(t, 20, c.lapsed, 0), base, usage.DefaultRules())
			if hasDrop(a, usage.MetricLapsed) != c.want {
				t.Fatalf("drops %+v", a.Drops)
			}
			if !c.want {
				return
			}
			for _, d := range a.Drops {
				if d.Metric == usage.MetricLapsed && (d.Kind != "share" || d.Starts != 20 || d.BaseStart != 60 || d.ChangePts < 20) {
					t.Fatalf("drop %+v", d)
				}
				if d.Metric == usage.MetricLapsed && !strings.HasPrefix(d.String(), "lapsed_share: ") {
					t.Fatalf("String %q", d.String())
				}
			}
		})
	}

	old := base[0]
	old.Lapsed = nil
	a := usage.Drops(lapsedReport(t, 20, 7, 0), []usage.Report{old}, usage.DefaultRules())
	if hasDrop(a, usage.MetricLapsed) || skipReason(a, usage.MetricLapsed) != usage.TooFew {
		t.Fatalf("a base without Lapsed judged lapsed_share: %+v", a)
	}
}

func TestUsage_LapsedAbsentWithoutEvents(t *testing.T) {
	// Scenario: no events in the week → no Lapsed block.
	r := usage.Build([]usage.Event{visit(1, 1, "user")}, lapsedFrom, lapsedFrom.Add(lapsedWeek))
	if r.Lapsed != nil || r.NoEvents == "" {
		t.Fatalf("a week without events has a Lapsed block: %+v", r.Lapsed)
	}
	b, _ := json.Marshal(r)
	if bytes.Contains(b, []byte(`"lapsed"`)) {
		t.Fatal("the JSON carries lapsed in a week without events")
	}
	a := usage.Drops(r, nil, usage.DefaultRules())
	if skipReason(a, usage.MetricLapsed) != "no events in the week" {
		t.Fatalf("skipped %+v", a.Skipped)
	}
}
