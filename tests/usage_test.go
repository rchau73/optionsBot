package tests

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/evidence"
	"optionsbot/internal/usage"
)

// The weekly usage and cohort report (#295, cmd/usage) over the monitor's
// analytics events.

// usageUser is a fixture user: a pseudonymous id and one session id.
func usageUser(n int) (uid, sid string) {
	return fmt.Sprintf("%016x", 0xa0000+n), fmt.Sprintf("%032x", 0xb0000+n)
}

// usageLine is one stored line as the collector writes it.
func usageLine(t *testing.T, event string, ts time.Time, n int, props map[string]any) string {
	t.Helper()
	uid, sid := usageUser(n)
	m := map[string]any{"event": event, "ts": ts.UTC().Format(time.RFC3339), "user_id": uid, "session_id": sid,
		"role": "user", "environment": "mvp", "app_version": "v0.3.0"}
	for k, v := range props {
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeUsageEvents(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func usageWeek(t *testing.T, label string) evidence.Window {
	t.Helper()
	w, err := evidence.ParseWeek(label)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// cohortFixture: 3 users start in week 42, user 1 comes back in week 43.
func cohortFixture(t *testing.T) []string {
	w42 := usageWeek(t, "2026-W42").From
	w43 := usageWeek(t, "2026-W43").From
	var lines []string
	for n := 1; n <= 3; n++ {
		lines = append(lines, usageLine(t, "session_started", w42.Add(time.Duration(n)*time.Hour), n, map[string]any{"entry_screen": "monitor", "viewport": "desktop"}))
	}
	lines = append(lines,
		usageLine(t, "session_started", w43.Add(26*time.Hour), 1, nil),
		usageLine(t, "panel_expanded", w43.Add(26*time.Hour+time.Minute), 1, map[string]any{"panel": "held"}),
	)
	return lines
}

func TestUsage_CohortRetentionFromASyntheticLog(t *testing.T) {
	path := writeUsageEvents(t, cohortFixture(t))
	w := usageWeek(t, "2026-W43")
	r, _, err := usage.Run(path, w.From, w.To, w.Label, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var c42 *usage.Cohort
	for i := range r.Cohorts {
		if r.Cohorts[i].Week == "2026-W42" {
			c42 = &r.Cohorts[i]
		}
	}
	if c42 == nil {
		t.Fatalf("no 2026-W42 cohort in %+v", r.Cohorts)
	}
	if c42.Users != 3 {
		t.Fatalf("W42 cohort users = %d, want 3", c42.Users)
	}
	if len(c42.Retained) != 1 || c42.Retained[0].Offset != 1 || c42.Retained[0].Users != 1 {
		t.Fatalf("W42 retention = %+v, want +1: 1 user (later weeks not ended)", c42.Retained)
	}
	if p := c42.Retained[0].Pct; p == nil || *p < 33.3 || *p > 33.4 {
		t.Fatalf("W42 retained +1 = %v, want 33 %%", p)
	}
	var md strings.Builder
	if err := usage.Markdown(&md, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md.String(), "| 2026-W42 | 3 | 1 (33 %) | — | — | — |") {
		t.Fatalf("markdown cohort row missing:\n%s", md.String())
	}
	// The report week's own cohort is empty; week 43 has 1 active user.
	if r.Active.Week.Users != 1 || r.Active.Week.ByRole["user"] != 1 || r.Active.Week.ByEnvironment["mvp"] != 1 {
		t.Fatalf("active week = %+v", r.Active.Week)
	}
	if r.Active.Month.Users != 3 {
		t.Fatalf("active 30 days = %d, want 3", r.Active.Month.Users)
	}
}

func TestUsage_NoPersonInTheOutput(t *testing.T) {
	w := usageWeek(t, "2026-W43")
	lines := cohortFixture(t)
	for n := 1; n <= 4; n++ {
		at := w.From.Add(time.Duration(n) * 3 * time.Hour)
		lines = append(lines,
			usageLine(t, "session_started", at, 10+n, nil),
			usageLine(t, "first_answers_seen", at.Add(time.Minute), 10+n, map[string]any{"elapsed_ms": 60000 * n}),
			usageLine(t, "tip_opened", at.Add(2*time.Minute), 10+n, map[string]any{"tip_id": "tip.gamma_flip", "screen": "overview"}),
			usageLine(t, "help_resolved", at.Add(3*time.Minute), 10+n, map[string]any{"tip_id": "tip.gamma_flip", "helped": n%2 == 0}),
			usageLine(t, "manual_close_opened", at.Add(4*time.Minute), 10+n, map[string]any{"side": "put", "allowed": true}),
			usageLine(t, "manual_close_confirmed", at.Add(5*time.Minute), 10+n, map[string]any{"side": "put", "last_leg": false}),
			usageLine(t, "manual_close_result", at.Add(6*time.Minute), 10+n, map[string]any{"side": "put", "outcome": "ok"}),
			usageLine(t, "data_stale_shown", at.Add(7*time.Minute), 10+n, map[string]any{"source": "bot_api"}),
			usageLine(t, "signed_out", at.Add(8*time.Minute), 10+n, map[string]any{"reason": "expired"}),
		)
	}
	path := writeUsageEvents(t, lines)
	out := t.TempDir()
	r, paths, err := usage.Run(path, w.From, w.To, w.Label, out)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("paths = %v", paths)
	}
	var written string
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		written += string(b)
	}
	for n := 0; n <= 20; n++ {
		uid, sid := usageUser(n)
		if strings.Contains(written, uid) || strings.Contains(written, sid) {
			t.Fatalf("user %d's id appears in the report", n)
		}
	}
	for _, field := range []string{"user_id", "session_id"} {
		if strings.Contains(written, field) {
			t.Fatalf("report names %q", field)
		}
	}
	// The figures are there all the same.
	if f := r.FirstAnswers; f.FirstSessions != 4 || f.WithAnswers != 4 || f.MedianElapsedMs == nil || *f.MedianElapsedMs != 150000 {
		t.Fatalf("first answers = %+v", f)
	}
	if h := r.Help; h.Tips["tip.gamma_flip"] != 4 || h.Answers != 4 || h.Helped != 2 || *h.HelpedPct != 50 {
		t.Fatalf("help = %+v", h)
	}
	if mc := r.ManualClose; mc.Opened != 4 || mc.OpenedAllowed != 4 || mc.Confirmed != 4 || mc.Results["ok"] != 4 {
		t.Fatalf("manual close = %+v", mc)
	}
	if r.Trust.Stale["bot_api"] != 4 || r.Trust.SignedOut["expired"] != 4 {
		t.Fatalf("trust = %+v", r.Trust)
	}
	if !slices.Equal(r.Panels.NeverOpened, []string{"account", "others", "trades"}) {
		t.Fatalf("never opened = %v", r.Panels.NeverOpened)
	}
	if !strings.Contains(written, "Source: `"+path+"`") {
		t.Fatal("sections do not name their source file")
	}
}

func TestUsage_NoEventsYet(t *testing.T) {
	w := usageWeek(t, "2026-W43")
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"missing": filepath.Join(dir, "none.jsonl"), "empty": empty} {
		out := t.TempDir()
		r, paths, err := usage.Run(path, w.From, w.To, w.Label, out)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if r.EventsInWeek != 0 || r.NoEvents == "" || r.Active != nil || r.Cohorts != nil {
			t.Fatalf("%s: report filled in: %+v", name, r)
		}
		if r.Source.Missing != (name == "missing") {
			t.Fatalf("%s: missing = %v", name, r.Source.Missing)
		}
		md, err := os.ReadFile(paths[0])
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"No events were recorded in the week", "consent", "instrumented screen"} {
			if !strings.Contains(string(md), want) {
				t.Fatalf("%s: markdown lacks %q:\n%s", name, want, md)
			}
		}
		if filepath.Base(paths[0]) != "2026-W43.md" || filepath.Base(paths[1]) != "2026-W43.json" {
			t.Fatalf("%s: paths = %v", name, paths)
		}
	}
}

// Events outside the week only: still "no events in the week".
func TestUsage_NoEventsInTheWeekButBefore(t *testing.T) {
	path := writeUsageEvents(t, cohortFixture(t)[:3]) // week 42 only
	w := usageWeek(t, "2026-W43")
	r, _, err := usage.Run(path, w.From, w.To, w.Label, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if r.EventsInWeek != 0 || r.NoEvents == "" || r.Source.Accepted != 3 {
		t.Fatalf("report = %+v", r)
	}
}

func TestUsage_ALineOutsideThePlan(t *testing.T) {
	w := usageWeek(t, "2026-W43")
	at := w.From.Add(time.Hour)
	good := usageLine(t, "panel_expanded", at, 1, map[string]any{"panel": "held"})
	lines := []string{
		good,
		usageLine(t, "mouse_moved", at, 2, nil), // unknown event
		usageLine(t, "panel_expanded", at, 3, map[string]any{"panel": "held", "premium": 0.01}), // unknown property
		usageLine(t, "panel_expanded", at, 4, map[string]any{"panel": "orders"}),                // value outside the enum
		usageLine(t, "panel_expanded", at, 5, map[string]any{"panel": "held", "role": "root"}),  // bad envelope
		`{"event": "panel_expanded", broken`,
		"",
	}
	path := writeUsageEvents(t, lines)
	r, _, err := usage.Run(path, w.From, w.To, w.Label, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{usage.RejectEvent: 1, usage.RejectProperty: 1, usage.RejectValue: 1, usage.RejectEnvelope: 1, usage.RejectParse: 1}
	for k, n := range want {
		if r.Source.Rejected[k] != n {
			t.Errorf("rejected[%s] = %d, want %d (all: %v)", k, r.Source.Rejected[k], n, r.Source.Rejected)
		}
	}
	if r.Source.RejectedTotal != 5 || r.Source.Accepted != 1 || r.Source.Lines != 6 {
		t.Fatalf("source = %+v", r.Source)
	}
	// Used in no figure: only user 1's line counts.
	if r.EventsInWeek != 1 || r.Active.Week.Users != 1 || r.Panels.Opens["held"] != 1 || r.Panels.Opens["orders"] != 0 {
		t.Fatalf("a rejected line reached a figure: events %d, active %d, opens %v", r.EventsInWeek, r.Active.Week.Users, r.Panels.Opens)
	}
}

func TestUsage_ParseLineRules(t *testing.T) {
	uid, sid := usageUser(1)
	base := fmt.Sprintf(`"ts":"2026-10-20T10:00:00Z","user_id":%q,"session_id":%q,"role":"admin"`, uid, sid)
	cases := []struct {
		line, reject string
	}{
		{`{"event":"panel_collapsed",` + base + `,"panel":"held","open_ms":1000}`, ""},
		{`{"event":"panel_collapsed",` + base + `,"panel":"held","open_ms":3600001}`, usage.RejectValue}, // over the 1 h cap
		{`{"event":"panel_collapsed",` + base + `,"panel":"held","open_ms":1.5}`, usage.RejectValue},     // not whole ms
		{`{"event":"tip_opened",` + base + `,"tip_id":"tip.gamma_flip"}`, ""},
		{`{"event":"tip_opened",` + base + `,"tip_id":"tip.flip_70000"}`, usage.RejectValue}, // a number smuggled in
		{`{"event":"help_resolved",` + base + `,"helped":"yes"}`, usage.RejectValue},
		{`{"event":"signed_out",` + base + `,"environment":"prod"}`, usage.RejectEnvelope},
		{`{"event":"signed_out","ts":"yesterday","user_id":"` + uid + `","session_id":"` + sid + `","role":"user"}`, usage.RejectEnvelope},
		{`{"event":"signed_out",` + base + `,"user_id":"alice@example.com"}`, usage.RejectEnvelope},
		{`["signed_out"]`, usage.RejectParse},
		{`null`, usage.RejectParse},
	}
	for _, c := range cases {
		_, got := usage.ParseLine([]byte(c.line))
		if got != c.reject {
			t.Errorf("%s: reject %q, want %q", c.line, got, c.reject)
		}
	}
}

// The report's allow-list is the plan's (§2, §4), so a line the collector
// stores is never rejected here and the reverse.
func TestUsage_AllowListMatchesThePlan(t *testing.T) {
	p := loadEventPlan(t)
	if len(p.events) != len(usage.Events) {
		t.Fatalf("plan has %d events, usage %d", len(p.events), len(usage.Events))
	}
	for ev, props := range p.events {
		got, ok := usage.Events[ev]
		if !ok || !slices.Equal(got, props) {
			t.Errorf("event %s: plan %v, usage %v", ev, props, got)
		}
	}
	if len(p.props) != len(usage.Properties) {
		t.Fatalf("plan has %d properties, usage %d", len(p.props), len(usage.Properties))
	}
	for prop, typ := range p.props {
		if usage.Properties[prop].Type != typ {
			t.Errorf("property %s: plan %q, usage %q", prop, typ, usage.Properties[prop].Type)
		}
	}
}

// ── Engagement drops (#194, #341, #252) ────────────────────────────────────

// sixWeekFixture is a log over 2026-W38 … W43, hand-computed:
//
//	W38  users 101–105 start (an older cohort)
//	W39  users 1–40 start (cohort W39 = 40); 101–102 come back
//	W40  users 1–30 active                      → W39 +1: 30/40 = 75 %
//	W41  users 1–40 active; 10 manual closes opened, 8 confirmed
//	W42  users 1–40 active + 41–50 start (cohort W42 = 10); 10 opened, 8 confirmed
//	W43  users 1–20 active; 10 opened, 5 confirmed
//
// Base of W43 = W40–W42: active mean (30 + 40 + 50) / 3 = 40, W43 has 20 →
// −50 %; manual close 16/20 = 80 % before, 5/10 = 50 % now → −30 pts.
func sixWeekFixture(t *testing.T) []string {
	wk := func(label string) time.Time { return usageWeek(t, label).From }
	var lines []string
	active := func(at time.Time, from, to int) {
		for n := from; n <= to; n++ {
			lines = append(lines, usageLine(t, "session_started", at.Add(time.Duration(n)*time.Minute), n, nil))
		}
	}
	closes := func(at time.Time, opened, confirmed int) {
		for i := 0; i < opened; i++ {
			lines = append(lines, usageLine(t, "manual_close_opened", at.Add(time.Duration(i)*time.Minute), 1, map[string]any{"side": "put", "allowed": true}))
			if i < confirmed {
				lines = append(lines, usageLine(t, "manual_close_confirmed", at.Add(time.Duration(i)*time.Minute+time.Second), 1, map[string]any{"side": "put", "last_leg": false}))
			}
		}
	}
	active(wk("2026-W38").Add(2*time.Hour), 101, 105)
	active(wk("2026-W39").Add(2*time.Hour), 1, 40)
	active(wk("2026-W39").Add(26*time.Hour), 101, 102)
	active(wk("2026-W40").Add(50*time.Hour), 1, 30)
	active(wk("2026-W41").Add(50*time.Hour), 1, 40)
	closes(wk("2026-W41").Add(60*time.Hour), 10, 8)
	active(wk("2026-W42").Add(50*time.Hour), 1, 50)
	closes(wk("2026-W42").Add(60*time.Hour), 10, 8)
	active(wk("2026-W43").Add(50*time.Hour), 1, 20)
	closes(wk("2026-W43").Add(60*time.Hour), 10, 5)
	return lines
}

// Scenario (#194): fixtures — weekly actives, cohort retention and the drop
// alert match hand-computed values.
func TestUsage_SixWeekFixtureMatchesHandComputedValues(t *testing.T) {
	path := writeUsageEvents(t, sixWeekFixture(t))
	lg, err := usage.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	w := usageWeek(t, "2026-W43")
	cur := usage.Build(lg.Events, w.From, w.To)
	cur.Week = w.Label
	prev := usage.BuildWeeks(lg.Events, w.From, usage.DropBaseWeeks)

	// Weekly actives.
	wantActive := map[string]int{"2026-W40": 30, "2026-W41": 40, "2026-W42": 50}
	for _, r := range prev {
		if r.Active.Week.Users != wantActive[r.Week] {
			t.Errorf("%s active = %d, want %d", r.Week, r.Active.Week.Users, wantActive[r.Week])
		}
	}
	if len(prev) != 3 || prev[0].Week != "2026-W40" || prev[2].Week != "2026-W42" {
		t.Fatalf("base weeks = %v", []string{prev[0].Week, prev[len(prev)-1].Week})
	}
	if cur.Active.Week.Users != 20 || cur.Active.Month.Users != 50 {
		t.Fatalf("W43 active week %d (want 20), 30 days %d (want 50)", cur.Active.Week.Users, cur.Active.Month.Users)
	}

	// Cohort retention.
	cohort := func(week string) usage.Cohort {
		for _, c := range cur.Cohorts {
			if c.Week == week {
				return c
			}
		}
		t.Fatalf("no cohort %s", week)
		return usage.Cohort{}
	}
	c39 := cohort("2026-W39")
	wantRet := []struct {
		users int
		pct   float64
	}{{30, 75}, {40, 100}, {40, 100}, {20, 50}}
	if c39.Users != 40 || len(c39.Retained) != 4 {
		t.Fatalf("W39 cohort = %+v", c39)
	}
	for i, want := range wantRet {
		got := c39.Retained[i]
		if got.Offset != i+1 || got.Users != want.users || got.Pct == nil || *got.Pct != want.pct {
			t.Errorf("W39 +%d = %d (%v), want %d (%.0f %%)", i+1, got.Users, got.Pct, want.users, want.pct)
		}
	}
	c42 := cohort("2026-W42")
	if c42.Users != 10 || len(c42.Retained) != 1 || c42.Retained[0].Users != 0 || *c42.Retained[0].Pct != 0 {
		t.Fatalf("W42 cohort = %+v", c42)
	}

	// The drop alert.
	a := usage.Drops(cur, prev, usage.DefaultRules())
	if len(a.Drops) != 2 {
		t.Fatalf("drops = %+v", a.Drops)
	}
	act, mc := a.Drops[0], a.Drops[1]
	if act.Metric != usage.MetricActive || act.Current != 20 || act.Base != 40 || act.ChangePct != -50 {
		t.Errorf("active drop = %+v", act)
	}
	if mc.Metric != usage.MetricManualClose || mc.Current != 50 || mc.Base != 80 || mc.ChangePts != -30 || mc.Starts != 10 || mc.BaseStart != 20 {
		t.Errorf("manual close drop = %+v", mc)
	}
	skipped := map[string]string{}
	for _, s := range a.Skipped {
		skipped[s.Metric] = s.Reason
	}
	for _, m := range []string{usage.MetricHelp, usage.MetricFirstAnswers, usage.MetricRebalance} {
		if skipped[m] != usage.TooFew {
			t.Errorf("%s skipped %q, want %q", m, skipped[m], usage.TooFew)
		}
	}
	if len(a.Missing) != 0 || len(a.Weeks) != 4 || a.Weeks[3].Week != "2026-W43" || a.Weeks[3].ActiveUsers != 20 {
		t.Errorf("weeks = %+v, missing %v", a.Weeks, a.Missing)
	}
}

// #445: the written report carries the same drop check as `drops`, so the
// monitor's Analytics page reads it instead of recomputing it.
func TestUsage_ReportJSONCarriesTheWeeksDrops(t *testing.T) {
	path := writeUsageEvents(t, sixWeekFixture(t))
	w := usageWeek(t, "2026-W43")
	dir := t.TempDir()
	r, _, err := usage.Run(path, w.From, w.To, w.Label, dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "2026-W43.json"))
	if err != nil {
		t.Fatal(err)
	}
	var written usage.Report
	if err := json.Unmarshal(b, &written); err != nil {
		t.Fatal(err)
	}
	for _, got := range []*usage.Alert{r.Drops, written.Drops} {
		if got == nil || got.Week != "2026-W43" || len(got.Drops) != 2 ||
			got.Drops[0].Metric != usage.MetricActive || got.Drops[1].Metric != usage.MetricManualClose {
			t.Fatalf("drops = %+v", got)
		}
	}
}

// usageReport is a report with only the figures the drop check reads.
func usageReport(week string, active int, opened, confirmed int) usage.Report {
	from, _ := evidence.ParseWeek(week)
	return usage.Report{
		Week: week, From: from.From, To: from.To, EventsInWeek: active + opened + confirmed + 1,
		Active:       &usage.Active{Week: usage.Count{Users: active}},
		Help:         &usage.Help{},
		FirstAnswers: &usage.FirstAnswers{},
		ManualClose:  &usage.ManualClose{Opened: opened, Confirmed: confirmed},
		Rebalance:    &usage.Rebalance{},
	}
}

func skipReason(a usage.Alert, metric string) string {
	for _, s := range a.Skipped {
		if s.Metric == metric {
			return s.Reason
		}
	}
	return ""
}

// Scenario (#341): the last 3 weeks averaged 40 active users and this week
// has 20 → one drop for active users with both figures and the change in %.
func TestUsage_DropActiveUsersAgainstTheMeanOfThreeWeeks(t *testing.T) {
	prev := []usage.Report{usageReport("2026-W42", 45, 0, 0), usageReport("2026-W40", 35, 0, 0), usageReport("2026-W41", 40, 0, 0)}
	a := usage.Drops(usageReport("2026-W43", 20, 0, 0), prev, usage.DefaultRules())
	if len(a.Drops) != 1 {
		t.Fatalf("drops = %+v", a.Drops)
	}
	d := a.Drops[0]
	if d.Metric != usage.MetricActive || d.Current != 20 || d.Base != 40 || d.ChangePct != -50 {
		t.Fatalf("drop = %+v", d)
	}
	if a.Weeks[0].Week != "2026-W40" { // sorted oldest first whatever the order given
		t.Fatalf("weeks = %+v", a.Weeks)
	}
	// 29 % down is under the rule; 30 % is on it.
	if a := usage.Drops(usageReport("2026-W43", 29, 0, 0), prev, usage.DefaultRules()); len(a.Drops) != 0 {
		t.Fatalf("−27.5 %% raised %+v", a.Drops)
	}
	if a := usage.Drops(usageReport("2026-W43", 28, 0, 0), prev, usage.DefaultRules()); len(a.Drops) != 1 {
		t.Fatalf("−30 %% raised nothing")
	}
}

// Scenarios (#341, #252): earlier weeks with fewer than 10 users (a base of
// 3 or 8 people) → no drop, "too few to judge".
func TestUsage_DropTooFewToJudge(t *testing.T) {
	for _, base := range []int{3, 8} {
		prev := []usage.Report{usageReport("2026-W40", base, 0, 0), usageReport("2026-W41", base, 0, 0), usageReport("2026-W42", base, 0, 0)}
		a := usage.Drops(usageReport("2026-W43", 0, 0, 0), prev, usage.DefaultRules())
		if len(a.Drops) != 0 {
			t.Fatalf("base %d: drops = %+v", base, a.Drops)
		}
		if got := skipReason(a, usage.MetricActive); got != usage.TooFew {
			t.Fatalf("base %d: active skipped %q", base, got)
		}
	}
}

// Scenario (#341): the manual-close funnel completed 80 % before and 50 %
// now, with at least 10 starts in each → a drop for that funnel.
func TestUsage_DropManualCloseFunnel(t *testing.T) {
	prev := []usage.Report{usageReport("2026-W40", 40, 10, 8), usageReport("2026-W41", 40, 10, 8), usageReport("2026-W42", 40, 10, 8)}
	a := usage.Drops(usageReport("2026-W43", 40, 10, 5), prev, usage.DefaultRules())
	if len(a.Drops) != 1 || a.Drops[0].Metric != usage.MetricManualClose || a.Drops[0].Current != 50 || a.Drops[0].Base != 80 {
		t.Fatalf("drops = %+v", a.Drops)
	}
	// 9 starts this week: too few.
	a = usage.Drops(usageReport("2026-W43", 40, 9, 1), prev, usage.DefaultRules())
	if len(a.Drops) != 0 || skipReason(a, usage.MetricManualClose) != usage.TooFew {
		t.Fatalf("9 starts: drops %+v, skipped %+v", a.Drops, a.Skipped)
	}
	// A flat week raises nothing.
	if a := usage.Drops(usageReport("2026-W43", 40, 10, 8), prev, usage.DefaultRules()); len(a.Drops) != 0 {
		t.Fatalf("flat week: %+v", a.Drops)
	}
}

// Scenario (#252): a week with no event data → no alert, the week is noted.
func TestUsage_DropMissingWeekIsNotedNeverCounted(t *testing.T) {
	empty := func(week string) usage.Report {
		w, _ := evidence.ParseWeek(week)
		return usage.Report{Week: week, From: w.From, To: w.To, NoEvents: usage.NoEventsWhy}
	}
	// A base week without events is left out of the mean (not a zero).
	prev := []usage.Report{usageReport("2026-W40", 40, 0, 0), empty("2026-W41"), usageReport("2026-W42", 40, 0, 0)}
	a := usage.Drops(usageReport("2026-W43", 35, 0, 0), prev, usage.DefaultRules())
	if len(a.Drops) != 0 || !slices.Equal(a.Missing, []string{"2026-W41"}) {
		t.Fatalf("drops %+v, missing %v", a.Drops, a.Missing)
	}
	// The week itself without events: nothing judged, the week noted.
	a = usage.Drops(empty("2026-W43"), prev, usage.DefaultRules())
	if len(a.Drops) != 0 || !slices.Contains(a.Missing, "2026-W43") || skipReason(a, usage.MetricActive) != "no events in the week" {
		t.Fatalf("alert = %+v", a)
	}
	// From a real log: no event at all in the four weeks.
	lg, _ := usage.Read(strings.NewReader(""))
	w := usageWeek(t, "2026-W43")
	a = usage.Drops(usage.Build(lg.Events, w.From, w.To), usage.BuildWeeks(lg.Events, w.From, 3), usage.DefaultRules())
	if len(a.Drops) != 0 || len(a.Missing) != 4 {
		t.Fatalf("empty log: %+v", a)
	}
}

// The drop check carries counts and shares only.
func TestUsage_DropsCarryNoPerson(t *testing.T) {
	path := writeUsageEvents(t, sixWeekFixture(t))
	lg, err := usage.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	w := usageWeek(t, "2026-W43")
	a := usage.Drops(usage.Build(lg.Events, w.From, w.To), usage.BuildWeeks(lg.Events, w.From, 3), usage.DefaultRules())
	js, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 105; n++ {
		uid, sid := usageUser(n)
		if strings.Contains(string(js), uid) || strings.Contains(string(js), sid) {
			t.Fatalf("user %d's id in the drop check", n)
		}
	}
}
