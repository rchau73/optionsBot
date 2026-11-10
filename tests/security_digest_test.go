package tests

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/backup"
	"optionsbot/internal/evidence"
	"optionsbot/internal/security"
)

// The weekly security digest and the same-day alerts (#717): one test per
// scenario of the ticket, plus the edges of each alert rule.

// secWeek is 2026-W45 (Mon 2026-11-02 to Sun 2026-11-08); the week before is 2026-W44.
func secWeek(t *testing.T) evidence.Window {
	t.Helper()
	w, err := evidence.ParseWeek("2026-W45")
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// secLine is one audit line's fields (without braces) at a time.
func secLine(at time.Time, action, extra string) string {
	s := fmt.Sprintf(`"time":%q,"action":%q`, at.UTC().Format("2006-01-02T15:04:05.000Z"), action)
	if extra != "" {
		s += "," + extra
	}
	return s
}

// writeSecTrail writes the lines as a hash-chained audit.jsonl in a new
// folder, the way lib/auth/audit.js does → the folder.
func writeSecTrail(t *testing.T, fields []string) string {
	t.Helper()
	dir := t.TempDir()
	var b strings.Builder
	prev := ""
	for _, f := range fields {
		l := sealAuditLine(f, prev)
		b.WriteString(l + "\n")
		prev = sha256Text(l)
	}
	if len(fields) > 0 {
		writeBackupFile(t, filepath.Join(dir, backup.AuditFile), b.String())
	}
	return dir
}

// secDigest reads the trail in dir and builds 2026-W45's digest as cmd/security does.
func secDigest(t *testing.T, dir string) security.Digest {
	t.Helper()
	tr, err := security.ReadTrail(dir)
	if err != nil {
		t.Fatal(err)
	}
	return security.Build(tr, security.ChainOf(backup.CheckAuditChain(dir)), secWeek(t))
}

func secCount(t *testing.T, d security.Digest, action string) security.Count {
	t.Helper()
	for _, c := range d.Counts {
		if c.Action == action {
			return c
		}
	}
	t.Fatalf("the digest does not count %s", action)
	return security.Count{}
}

// failures appends n sign_in_failed lines for one username from `from`, `step` apart.
func failures(lines []string, n int, from time.Time, step time.Duration, user, ip string) []string {
	for i := 0; i < n; i++ {
		lines = append(lines, secLine(from.Add(time.Duration(i)*step), "sign_in_failed", fmt.Sprintf(`"username":%q,"ip":%q,"reason":"password"`, user, ip)))
	}
	return lines
}

// Scenario: A quiet week — every count is 0 and it says the audit chain is intact.
func TestSecurityDigest_AQuietWeek(t *testing.T) {
	w := secWeek(t)
	dir := writeSecTrail(t, []string{
		secLine(w.From.AddDate(0, 0, -30), "user_created", `"username":"bootstrap","target":"root","to":"admin"`),
		secLine(w.From.Add(36*time.Hour), "signed_in", `"username":"root","role":"admin","ip":"10.0.0.1"`),
	})
	d := secDigest(t, dir)
	for _, c := range d.Counts {
		if c.Week != 0 || c.Before != 0 {
			t.Errorf("%s: %d against %d, want 0 against 0", c.Action, c.Week, c.Before)
		}
	}
	if !d.Chain.Intact || d.Chain.Checked != 2 || d.Empty {
		t.Fatalf("chain %+v empty %v, want intact with 2 lines checked", d.Chain, d.Empty)
	}
	md := d.Markdown()
	for _, want := range []string{"## Security", "Audit chain intact — 2 lines checked.", "| Failed sign-ins | 0 | 0 |", "| Lockouts | 0 | 0 |",
		"Usernames locked out this week: 0", "Distinct sources signing in this week: 1", "Last change of the admin role: " + w.From.AddDate(0, 0, -30).Format("2006-01-02")} {
		if !strings.Contains(md, want) {
			t.Errorf("digest lacks %q:\n%s", want, md)
		}
	}
	if h := d.Headline(); !strings.Contains(h, "chain intact; 0 failed sign-ins (0 the week before), 0 lockouts (0)") {
		t.Errorf("headline %q", h)
	}
}

// An empty (or missing) trail is "No data yet", not a table of zeros.
func TestSecurityDigest_NoDataYet(t *testing.T) {
	d := secDigest(t, writeSecTrail(t, nil))
	if !d.Empty || !d.Chain.Intact {
		t.Fatalf("want an empty, intact digest: %+v", d)
	}
	if md := d.Markdown(); !strings.Contains(md, "No data yet") || strings.Contains(md, "| What |") {
		t.Errorf("digest of an empty trail:\n%s", md)
	}
	if h := d.Headline(); !strings.Contains(h, "no data yet") {
		t.Errorf("headline %q", h)
	}
}

// Scenario: Counts and the week before — 12 against 3 and 2 against 0.
func TestSecurityDigest_CountsAndTheWeekBefore(t *testing.T) {
	w := secWeek(t)
	var lines []string
	lines = failures(lines, 3, w.From.AddDate(0, 0, -3), time.Hour, "bob", "10.0.0.9")
	lines = failures(lines, 12, w.From.Add(24*time.Hour), 10*time.Minute, "alice", "10.0.0.7")
	lines = append(lines,
		secLine(w.From.Add(26*time.Hour), "locked", `"username":"alice","ip":"10.0.0.7","failures":5`),
		secLine(w.From.Add(27*time.Hour), "locked", `"username":"alice","ip":"10.0.0.7","failures":5`),
		secLine(w.To.Add(time.Hour), "locked", `"username":"carol","failures":5`), // next week: not counted
	)
	d := secDigest(t, writeSecTrail(t, lines))
	if c := secCount(t, d, "sign_in_failed"); c.Week != 12 || c.Before != 3 {
		t.Errorf("failed sign-ins %d against %d, want 12 against 3", c.Week, c.Before)
	}
	if c := secCount(t, d, "locked"); c.Week != 2 || c.Before != 0 {
		t.Errorf("lockouts %d against %d, want 2 against 0", c.Week, c.Before)
	}
	if d.LockedUsernames != 1 {
		t.Errorf("locked usernames %d, want 1 (one person locked twice)", d.LockedUsernames)
	}
	md := d.Markdown()
	for _, want := range []string{"| Failed sign-ins | 12 | 3 |", "| Lockouts | 2 | 0 |", "Usernames locked out this week: 1"} {
		if !strings.Contains(md, want) {
			t.Errorf("digest lacks %q:\n%s", want, md)
		}
	}
	if h := d.Headline(); !strings.Contains(h, "12 failed sign-ins (3 the week before), 2 lockouts (0)") {
		t.Errorf("headline %q", h)
	}
}

// Scenario: A broken audit chain — the digest opens with the chain failure before any count.
func TestSecurityDigest_ABrokenAuditChain(t *testing.T) {
	w := secWeek(t)
	lines := failures(nil, 4, w.From.Add(time.Hour), time.Minute, "alice", "10.0.0.7")
	dir := writeSecTrail(t, lines)
	p := filepath.Join(dir, backup.AuditFile)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// One line removed: the next line's link breaks.
	parts := strings.SplitAfter(string(b), "\n")
	writeBackupFile(t, p, parts[0]+strings.Join(parts[2:], ""))

	d := secDigest(t, dir)
	if d.Chain.Intact || !strings.Contains(d.Chain.Problem, "audit.jsonl line 2: link") {
		t.Fatalf("chain %+v, want broken at line 2 (link)", d.Chain)
	}
	md := d.Markdown()
	broken, firstCount := strings.Index(md, "Audit chain BROKEN"), strings.Index(md, "| What |")
	if broken < 0 || firstCount < 0 || broken > firstCount {
		t.Errorf("the chain failure must open the digest, before any count:\n%s", md)
	}
	if strings.Contains(md, "Audit chain intact") {
		t.Errorf("a broken chain is also called intact:\n%s", md)
	}
	if h := d.Headline(); !strings.Contains(h, "AUDIT CHAIN BROKEN") {
		t.Errorf("headline %q", h)
	}
}

// Scenario: Password guessing alerts the same day — one alert, the rule and
// the count, no username, password or IP address; and the rule's edges.
func TestSecurityAlerts_PasswordGuessingAlertsTheSameDay(t *testing.T) {
	now := time.Date(2026, 11, 8, 15, 0, 0, 0, time.UTC)
	start := now.Add(-3 * time.Hour)
	cases := []struct {
		name  string
		lines []string
		want  int // alerts
		count int
	}{
		{name: "10 within an hour", lines: failures(nil, 10, start, 6*time.Minute, "alice", "203.0.113.5"), want: 1, count: 10},
		{name: "14 within an hour: one alert, the most in any hour", lines: failures(nil, 14, start, 4*time.Minute, "alice", "203.0.113.5"), want: 1, count: 14},
		{name: "9 within an hour", lines: failures(nil, 9, start, time.Minute, "alice", "203.0.113.5")},
		{name: "10 over more than an hour", lines: failures(nil, 10, start, 7*time.Minute, "alice", "203.0.113.5")},
		{name: "10 spread over two usernames", lines: failures(failures(nil, 5, start, time.Minute, "alice", "203.0.113.5"), 5, start, time.Minute, "bob", "203.0.113.5")},
		{name: "older than the look-back", lines: failures(nil, 10, now.Add(-security.AlertLookback-2*time.Hour), time.Minute, "alice", "203.0.113.5")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr, err := security.ReadTrail(writeSecTrail(t, c.lines))
			if err != nil {
				t.Fatal(err)
			}
			got := security.Alerts(tr.Entries, now)
			if len(got) != c.want {
				t.Fatalf("%d alerts, want %d: %+v", len(got), c.want, got)
			}
			if c.want == 0 {
				return
			}
			a := got[0]
			if a.Rule != "failed_sign_ins" || a.Count != c.count {
				t.Errorf("alert %+v, want rule failed_sign_ins with count %d", a, c.count)
			}
			for _, want := range []string{fmt.Sprintf("%d failed sign-ins for one username within an hour", c.count), "rule: 10 or more in any hour", "2026-11-08"} {
				if !strings.Contains(a.Text, want) {
					t.Errorf("alert text %q lacks %q", a.Text, want)
				}
			}
			for _, leak := range []string{"alice", "203.0.113.5", "password"} {
				if strings.Contains(a.Text, leak) || strings.Contains(a.ID, leak) {
					t.Errorf("alert %+v holds %q", a, leak)
				}
			}
		})
	}
}

func TestSecurityAlerts_LockoutsInADay(t *testing.T) {
	now := time.Date(2026, 11, 8, 20, 0, 0, 0, time.UTC)
	locks := func(n int) []string {
		var lines []string
		for i := 0; i < n; i++ {
			lines = append(lines, secLine(now.Add(-time.Duration(i+1)*time.Hour), "locked", fmt.Sprintf(`"username":"user%d","failures":5`, i)))
		}
		return lines
	}
	for _, c := range []struct{ n, want int }{{2, 0}, {3, 1}, {5, 1}} {
		tr, err := security.ReadTrail(writeSecTrail(t, locks(c.n)))
		if err != nil {
			t.Fatal(err)
		}
		got := security.Alerts(tr.Entries, now)
		if len(got) != c.want {
			t.Fatalf("%d lockouts: %d alerts, want %d", c.n, len(got), c.want)
		}
		if c.want == 1 && (got[0].Rule != "lockouts" || got[0].Count != c.n || !strings.Contains(got[0].Text, fmt.Sprintf("%d lockouts on 2026-11-08 (rule: 3 or more in a day)", c.n))) {
			t.Errorf("%d lockouts: alert %+v", c.n, got[0])
		}
	}
}

// Scenario: A promotion to admin is reported — one alert with its date, and
// the same change does not alert twice; the first-admin setup never alerts.
func TestSecurityAlerts_APromotionToAdminIsReported(t *testing.T) {
	now := time.Date(2026, 11, 8, 15, 0, 0, 0, time.UTC)
	at := time.Date(2026, 11, 8, 9, 30, 0, 0, time.UTC)
	dir := writeSecTrail(t, []string{
		secLine(at.Add(-2*time.Hour), "user_created", `"username":"bootstrap","target":"root","to":"admin"`),
		secLine(at.Add(-time.Hour), "role_changed", `"username":"root","role":"admin","target":"dave","from":"user","to":"manager"`),
		secLine(at, "role_changed", `"username":"root","role":"admin","target":"dave","from":"manager","to":"admin"`),
	})
	tr, err := security.ReadTrail(dir)
	if err != nil {
		t.Fatal(err)
	}
	sent := filepath.Join(t.TempDir(), "mvp.alerts")
	first, err := security.Raise(security.Alerts(tr.Entries, now), sent)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].Rule != "admin_role" || !strings.Contains(first[0].Text, "a role change to admin on 2026-11-08 09:30 UTC") {
		t.Fatalf("alerts %+v, want one role change to admin with its date", first)
	}
	for _, leak := range []string{"dave", "root"} {
		if strings.Contains(first[0].Text, leak) || strings.Contains(first[0].ID, leak) {
			t.Errorf("alert %+v names %q", first[0], leak)
		}
	}
	again, err := security.Raise(security.Alerts(tr.Entries, now.Add(30*time.Minute)), sent)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Errorf("the same change alerted twice: %+v", again)
	}
	if info, err := os.Stat(sent); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("sent file: %v, mode %v; want owner-only", err, info.Mode().Perm())
	}

	// An admin creating another admin is a promotion too; demotions are not.
	tr2, err := security.ReadTrail(writeSecTrail(t, []string{
		secLine(at, "user_created", `"username":"root","role":"admin","target":"erin","to":"admin"`),
		secLine(at.Add(time.Minute), "role_changed", `"username":"root","role":"admin","target":"erin","from":"admin","to":"user"`),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := security.Alerts(tr2.Entries, now); len(got) != 1 || got[0].Rule != "admin_role" {
		t.Errorf("alerts %+v, want only the admin created by an admin", got)
	}
}

// Scenario: Nothing sensitive leaves — over any audit lines, the digest (its
// markdown, JSON and headline) and every alert's text hold no token, reset
// link, password, IP address or username.
func TestSecurity_NothingSensitiveLeaves(t *testing.T) {
	w := secWeek(t)
	now := w.To.Add(-time.Hour)
	secrets := []string{"alice", "bob", "mallory", "203.0.113.5", "2001:db8::7", "passpass-word", "redactme-redactme", "https://monitor.example/reset?token=abc123", "abc123", "s3ss10n1d"}
	var lines []string
	lines = failures(lines, 12, now.Add(-50*time.Minute), 4*time.Minute, "alice", "203.0.113.5")
	for i := 0; i < 3; i++ {
		lines = append(lines, secLine(now.Add(-time.Duration(i+1)*time.Minute), "locked", `"username":"bob","ip":"2001:db8::7","failures":5`))
	}
	lines = append(lines,
		secLine(now.Add(-20*time.Minute), "signed_in", `"username":"mallory","role":"user","ip":"2001:db8::7"`),
		secLine(now.Add(-19*time.Minute), "role_changed", `"username":"mallory","role":"admin","target":"bob","from":"user","to":"admin"`),
		// Fields that must never be in a line, and if they were, never leave.
		secLine(now.Add(-18*time.Minute), "reset_link_issued", `"username":"mallory","target":"alice","url":"https://monitor.example/reset?token=abc123","password":"passpass-word","token":"redactme-redactme"`),
		secLine(now.Add(-17*time.Minute), "session_ended", `"username":"alice","target_session":"s3ss10n1d"`),
	)
	dir := writeSecTrail(t, lines)
	d := secDigest(t, dir)
	js, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := security.ReadTrail(dir)
	if err != nil {
		t.Fatal(err)
	}
	alerts := security.Alerts(tr.Entries, now)
	if len(alerts) != 3 {
		t.Fatalf("%d alerts, want the three rules to fire: %+v", len(alerts), alerts)
	}
	out := map[string]string{"markdown": d.Markdown(), "json": string(js), "headline": d.Headline()}
	for i, a := range alerts {
		out[fmt.Sprintf("alert %d", i)] = a.Text
	}
	for where, text := range out {
		for _, s := range secrets {
			if strings.Contains(text, s) {
				t.Errorf("%s holds %q:\n%s", where, s, text)
			}
		}
	}
}
