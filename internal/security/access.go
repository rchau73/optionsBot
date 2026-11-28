package security

import (
	"fmt"
	"sort"
	"time"
)

// UnusedDays is the access review's "unused" rule (#919): no sign-in for this
// many days, or never signed in and created this long ago. It is the Users
// page's UNUSED_DAYS (frontend/lib/users.js, #910); a test keeps the two equal.
const UnusedDays = 90

// Holders is who holds access at one moment, replayed from the audit trail:
// counts only, never a name.
type Holders struct {
	Admins           int `json:"admins"`
	Managers         int `json:"managers"`
	Users            int `json:"users"`
	UnlinkedManagers int `json:"managers_without_link"` // managers with no account_link_added still in force
	Unused           int `json:"unused"`                // UnusedDays or more without a sign-in
}

// Access is the digest's access review: people per role, managers with no
// account link and unused accounts at the end of the week and of the week
// before. Complete is false when the trail cannot rebuild them (its archives
// do not reach back to the first user_created): then no figure is given.
type Access struct {
	Complete bool     `json:"complete"`
	Week     *Holders `json:"week,omitempty"`
	Before   *Holders `json:"week_before,omitempty"`
}

// person is one account in the replay; the username is the map key only.
type person struct {
	role       string
	created    time.Time
	lastSignIn time.Time // zero: never signed in
	links      map[string]bool
}

// replay is the accounts the trail has created and not deleted, and whether
// every line it read made sense against them.
type replay struct {
	people    map[string]*person
	bootstrap bool // the first-admin setup's user_created line was read
	broken    bool // a line acted on someone the trail never created
}

// alive is the person named, marking the replay broken when there is none.
func (r *replay) alive(name string) *person {
	p := r.people[name]
	if p == nil {
		r.broken = true
	}
	return p
}

// apply replays one audit line.
func (r *replay) apply(e Entry) {
	switch e.Action {
	case "user_created":
		if e.Username == bootstrapActor {
			r.bootstrap = true
		}
		r.people[e.Target] = &person{role: e.To, created: e.Time, links: map[string]bool{}}
	case "role_changed":
		if p := r.alive(e.Target); p != nil {
			p.role = e.To
		}
	case "user_deleted":
		if r.alive(e.Target) != nil {
			delete(r.people, e.Target)
		}
	case "account_deleted":
		if r.alive(e.Username) != nil {
			delete(r.people, e.Username)
		}
	case "signed_in":
		if p := r.alive(e.Username); p != nil {
			p.lastSignIn = e.Time
		}
	case "account_link_added":
		if p := r.alive(e.Target); p != nil {
			p.links[e.AccountID] = true
		}
	case "account_link_removed":
		if p := r.alive(e.Target); p != nil {
			delete(p.links, e.AccountID)
		}
	}
}

// holders counts the replay at `at`.
func (r *replay) holders(at time.Time) *Holders {
	h := &Holders{}
	for _, p := range r.people {
		switch p.role {
		case "admin":
			h.Admins++
		case "manager":
			h.Managers++
			if len(p.links) == 0 {
				h.UnlinkedManagers++
			}
		case "user":
			h.Users++
		}
		if isUnused(p, at) {
			h.Unused++
		}
	}
	return h
}

// isUnused is the Users page's rule (unusedLabel): whole days since the last
// sign-in, or since creation when it never signed in, at least UnusedDays.
func isUnused(p *person, at time.Time) bool {
	since := p.lastSignIn
	if since.IsZero() {
		since = p.created
	}
	return int(at.Sub(since)/(24*time.Hour)) >= UnusedDays
}

// BuildAccess replays the trail's lines up to the end of each week: the week
// before ends at `from`, the week at `to`. It is complete only when the trail
// holds the first-admin setup's line and no line acts on someone it never created.
func BuildAccess(entries []Entry, from, to time.Time) Access {
	sorted := append([]Entry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })
	r := &replay{people: map[string]*person{}}
	var before *Holders
	for _, e := range sorted {
		if !e.Time.Before(to) {
			break
		}
		if before == nil && !e.Time.Before(from) {
			before = r.holders(from)
		}
		r.apply(e)
	}
	if before == nil {
		before = r.holders(from)
	}
	if !r.bootstrap || r.broken {
		return Access{}
	}
	return Access{Complete: true, Week: r.holders(to), Before: before}
}

// plural is "1 admin", "2 admins".
func plural(n int, one string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %ss", n, one)
}

// roles is the people per role in words.
func (h Holders) roles() string {
	return fmt.Sprintf("%s, %s, %s", plural(h.Admins, "admin"), plural(h.Managers, "manager"), plural(h.Users, "user"))
}

// Line is the access review in one sentence, this week against the week
// before, or "access count incomplete" with no figure.
func (a Access) Line() string {
	if !a.Complete || a.Week == nil || a.Before == nil {
		return "Access: access count incomplete (the audit trail does not reach back to the first user created)."
	}
	w, b := a.Week, a.Before
	return fmt.Sprintf("Access: %s (%s the week before); %s with no account link (%d); %d unused for %d days (%d).",
		w.roles(), b.roles(), plural(w.UnlinkedManagers, "manager"), b.UnlinkedManagers, w.Unused, UnusedDays, b.Unused)
}
