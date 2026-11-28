package tests

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/security"
)

// The access review in the weekly security digest (#919): one test per
// scenario of the ticket, plus the week before and the 90-day rule's twin.

// accessSetup is a trail opening with the first-admin setup 200 days before
// 2026-W45, a manager "mona" linked to one account, and the lines given.
func accessSetup(t *testing.T, extra ...string) security.Digest {
	t.Helper()
	w := secWeek(t)
	start := w.From.AddDate(0, 0, -200)
	lines := []string{
		secLine(start, "user_created", `"username":"bootstrap","target":"root","to":"admin"`),
		secLine(w.From.Add(time.Hour), "signed_in", `"username":"root","role":"admin","ip":"10.0.0.1"`),
	}
	return secDigest(t, writeSecTrail(t, append(lines, extra...)))
}

func created(at time.Time, name, role string) string {
	return secLine(at, "user_created", fmt.Sprintf(`"username":"root","role":"admin","target":%q,"to":%q`, name, role))
}

func signedIn(at time.Time, name, role string) string {
	return secLine(at, "signed_in", fmt.Sprintf(`"username":%q,"role":%q,"ip":"203.0.113.5"`, name, role))
}

// Scenario: counts by role from the trail.
func TestSecurityAccess_CountsByRoleFromTheTrail(t *testing.T) {
	w := secWeek(t)
	at := w.From.AddDate(0, 0, -10)
	extra := []string{
		created(at, "mona", "manager"), created(at, "max", "manager"),
		secLine(at.Add(time.Minute), "account_link_added", `"username":"root","role":"admin","target":"mona","account_id":"btc-main"`),
		secLine(at.Add(time.Minute), "account_link_added", `"username":"root","role":"admin","target":"max","account_id":"eth-main"`),
	}
	for i := 0; i < 5; i++ {
		extra = append(extra, created(at, fmt.Sprintf("u%d", i), "user"))
	}
	extra = append(extra, secLine(w.From.Add(48*time.Hour), "user_deleted", `"username":"root","role":"admin","target":"u3"`))
	d := accessSetup(t, extra...)
	a := d.Access
	if !a.Complete || a.Week == nil || a.Before == nil {
		t.Fatalf("access %+v, want complete", a)
	}
	if a.Week.Admins != 1 || a.Week.Managers != 2 || a.Week.Users != 4 {
		t.Errorf("week %+v, want 1 admin, 2 managers, 4 users", *a.Week)
	}
	if a.Before.Users != 5 {
		t.Errorf("week before %+v, want 5 users (the deletion is this week)", *a.Before)
	}
	want := "Access: 1 admin, 2 managers, 4 users (1 admin, 2 managers, 5 users the week before); 0 managers with no account link (0)"
	for where, text := range map[string]string{"headline": d.Headline(), "markdown": d.Markdown()} {
		if !strings.Contains(text, want) {
			t.Errorf("%s lacks %q:\n%s", where, want, text)
		}
	}
	// An owner deleting their own account counts too.
	d = accessSetup(t, created(at, "u0", "user"), secLine(w.From.Add(time.Hour), "account_deleted", `"username":"u0","role":"user"`))
	if d.Access.Week.Users != 0 || d.Access.Before.Users != 1 {
		t.Errorf("account_deleted: week %+v before %+v", *d.Access.Week, *d.Access.Before)
	}
}

// Scenario: a manager with no link — none ever added, or all removed; a
// manager's links survive a role change, as account_links does.
func TestSecurityAccess_AManagerWithNoLink(t *testing.T) {
	w := secWeek(t)
	at := w.From.AddDate(0, 0, -10)
	link := func(action, who, id string, when time.Time) string {
		return secLine(when, action, fmt.Sprintf(`"username":"root","role":"admin","target":%q,"account_id":%q`, who, id))
	}
	cases := []struct {
		name  string
		lines []string
		want  int
	}{
		{"never linked", []string{created(at, "mona", "manager")}, 1},
		{"all links removed", []string{created(at, "mona", "manager"),
			link("account_link_added", "mona", "a1", at.Add(time.Minute)), link("account_link_added", "mona", "a2", at.Add(time.Minute)),
			link("account_link_removed", "mona", "a1", w.From.Add(time.Hour)), link("account_link_removed", "mona", "a2", w.From.Add(time.Hour))}, 1},
		{"one link left", []string{created(at, "mona", "manager"),
			link("account_link_added", "mona", "a1", at.Add(time.Minute)), link("account_link_added", "mona", "a2", at.Add(time.Minute)),
			link("account_link_removed", "mona", "a1", w.From.Add(time.Hour))}, 0},
		{"a user promoted to manager", []string{created(at, "uma", "user"),
			secLine(w.From.Add(time.Hour), "role_changed", `"username":"root","role":"admin","target":"uma","from":"user","to":"manager"`)}, 1},
		{"a numeric account id", []string{created(at, "mona", "manager"),
			secLine(at.Add(time.Minute), "account_link_added", `"username":"root","role":"admin","target":"mona","account_id":42`)}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := accessSetup(t, c.lines...)
			if !d.Access.Complete || d.Access.Week.UnlinkedManagers != c.want {
				t.Fatalf("access %+v week %+v, want %d unlinked managers", d.Access, d.Access.Week, c.want)
			}
		})
	}
	d := accessSetup(t, cases[1].lines...)
	if d.Access.Before.UnlinkedManagers != 0 {
		t.Errorf("week before: %d unlinked, want 0 (the links were removed this week)", d.Access.Before.UnlinkedManagers)
	}
	if want := "1 manager with no account link (0)"; !strings.Contains(d.Headline(), want) {
		t.Errorf("headline %q lacks %q", d.Headline(), want)
	}
}

// Scenario: unused for 90 days — 91 days before the week's end counts, 89 does not;
// one never signed in counts from its creation.
func TestSecurityAccess_UnusedFor90Days(t *testing.T) {
	w := secWeek(t)
	end := w.To
	d := accessSetup(t,
		created(end.AddDate(0, 0, -300), "old", "user"), signedIn(end.AddDate(0, 0, -91), "old", "user"),
		created(end.AddDate(0, 0, -300), "recent", "user"), signedIn(end.AddDate(0, 0, -89), "recent", "user"),
		created(end.AddDate(0, 0, -95), "never", "user"),
		created(end.AddDate(0, 0, -30), "fresh", "user"),
	)
	// At the week's end: old (91 d) and never (95 d) are unused; root signed in this week.
	if !d.Access.Complete || d.Access.Week.Unused != 2 {
		t.Fatalf("week %+v, want 2 unused", d.Access.Week)
	}
	// A week earlier: old was 84 days, never 88 days; root last signed in at the setup, 193 days.
	if d.Access.Before.Unused != 1 {
		t.Errorf("week before %+v, want 1 unused (root)", d.Access.Before)
	}
	if want := "2 unused for 90 days (1)"; !strings.Contains(d.Headline(), want) {
		t.Errorf("headline %q lacks %q", d.Headline(), want)
	}
}

// Scenario: missing history — no first-admin line, or a line on someone the
// trail never created: "access count incomplete" and no figure.
func TestSecurityAccess_MissingHistory(t *testing.T) {
	w := secWeek(t)
	at := w.From.AddDate(0, 0, -5)
	cases := map[string][]string{
		"no first user_created": {created(at, "mona", "manager"), signedIn(at, "mona", "manager")},
		"a sign-in of someone never created": {
			secLine(w.From.AddDate(0, 0, -200), "user_created", `"username":"bootstrap","target":"root","to":"admin"`),
			signedIn(at, "ghost", "user"),
		},
		"a role change of someone never created": {
			secLine(w.From.AddDate(0, 0, -200), "user_created", `"username":"bootstrap","target":"root","to":"admin"`),
			secLine(at, "role_changed", `"username":"root","role":"admin","target":"ghost","from":"user","to":"manager"`),
		},
	}
	for name, lines := range cases {
		t.Run(name, func(t *testing.T) {
			d := secDigest(t, writeSecTrail(t, lines))
			js, err := json.Marshal(d.Access)
			if err != nil {
				t.Fatal(err)
			}
			if d.Access.Complete || d.Access.Week != nil || d.Access.Before != nil || string(js) != `{"complete":false}` {
				t.Fatalf("access %s, want incomplete with no figure", js)
			}
			for where, text := range map[string]string{"headline": d.Headline(), "markdown": d.Markdown()} {
				if !strings.Contains(text, "access count incomplete") || strings.Contains(text, "admin,") || strings.Contains(text, "unused for") {
					t.Errorf("%s: %s", where, text)
				}
			}
		})
	}
}

// Scenario: no personal data — the access figures name no username, e-mail, IP or account.
func TestSecurityAccess_NoPersonalData(t *testing.T) {
	w := secWeek(t)
	at := w.From.AddDate(0, 0, -10)
	d := accessSetup(t,
		secLine(at, "user_created", `"username":"root","role":"admin","target":"mona","to":"manager","email":"mona@example.com"`),
		secLine(at.Add(time.Minute), "account_link_added", `"username":"root","role":"admin","target":"mona","account_id":"acct-7731"`),
		signedIn(at.Add(time.Hour), "mona", "manager"),
	)
	js, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	for where, text := range map[string]string{"headline": d.Headline(), "markdown": d.Markdown(), "json": string(js)} {
		for _, leak := range []string{"mona", "root", "example.com", "203.0.113.5", "10.0.0.1", "acct-7731"} {
			if strings.Contains(text, leak) {
				t.Errorf("%s holds %q:\n%s", where, leak, text)
			}
		}
	}
}

// The 90-day rule is the Users page's UNUSED_DAYS (frontend/lib/users.js).
func TestSecurityAccess_UnusedDaysMatchesTheUsersPage(t *testing.T) {
	b, err := os.ReadFile("../frontend/lib/users.js")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`export const UNUSED_DAYS = (\d+);`).FindSubmatch(b)
	if m == nil {
		t.Fatal("frontend/lib/users.js: no `export const UNUSED_DAYS = N;`")
	}
	if n, _ := strconv.Atoi(string(m[1])); n != security.UnusedDays {
		t.Errorf("UNUSED_DAYS is %d in lib/users.js, security.UnusedDays %d: keep them equal", n, security.UnusedDays)
	}
}
