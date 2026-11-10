package security

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"
)

// The alert rules' fixed values (#717: not config keys).
const (
	FailedSignInsPerHour = 10             // failed sign-ins for one username within any hour
	LockoutsPerDay       = 3              // lockouts in one UTC day
	AlertLookback        = 48 * time.Hour // lines older than this are not judged again
)

// Alert is one alert the check raises. ID names it so that the same pattern
// never alerts twice (it holds a hash of the username, never the name); Text
// is what an admin reads: the rule, the count and the day, nothing else.
type Alert struct {
	ID    string    `json:"id"`
	Rule  string    `json:"rule"`
	Count int       `json:"count"`
	At    time.Time `json:"at"`
	Text  string    `json:"text"`
}

// Alerts judges the lines of the AlertLookback before now (and none after
// it) on the three rules, oldest first:
//   - FailedSignInsPerHour or more sign_in_failed for one username within an
//     hour — one alert per username and UTC day, with the most in any hour;
//   - LockoutsPerDay or more `locked` lines in one UTC day — one per day;
//   - every role change to admin outside the first-admin setup (promotesToAdmin) — one per line.
func Alerts(entries []Entry, now time.Time) []Alert {
	from := now.Add(-AlertLookback)
	failures := map[string][]time.Time{}
	locks := map[string][]time.Time{}
	var out []Alert
	for _, e := range entries {
		if e.Time.Before(from) || e.Time.After(now) {
			continue
		}
		switch {
		case e.Action == "sign_in_failed":
			failures[e.Username] = append(failures[e.Username], e.Time)
		case e.Action == "locked":
			day := e.Time.Format(time.DateOnly)
			locks[day] = append(locks[day], e.Time)
		case promotesToAdmin(e):
			out = append(out, Alert{
				ID: "admin_role:" + e.Time.Format(time.RFC3339Nano) + ":" + nameHash(e.Target), Rule: "admin_role", Count: 1, At: e.Time,
				Text: fmt.Sprintf("Security alert: a role change to admin on %s UTC (rule: any change to admin outside the first-admin setup). The Audit page names it.",
					e.Time.Format("2006-01-02 15:04")),
			})
		}
	}
	for name, times := range failures {
		for day, burst := range busiestHour(times) {
			if burst.count < FailedSignInsPerHour {
				continue
			}
			out = append(out, Alert{
				ID: "failed_sign_ins:" + day + ":" + nameHash(name), Rule: "failed_sign_ins", Count: burst.count, At: burst.at,
				Text: fmt.Sprintf("Security alert: %d failed sign-ins for one username within an hour on %s (rule: %d or more in any hour). The Audit page names it.",
					burst.count, day, FailedSignInsPerHour),
			})
		}
	}
	for day, times := range locks {
		if len(times) < LockoutsPerDay {
			continue
		}
		out = append(out, Alert{
			ID: "lockouts:" + day, Rule: "lockouts", Count: len(times), At: times[0],
			Text: fmt.Sprintf("Security alert: %d lockouts on %s (rule: %d or more in a day). The Audit page names them.", len(times), day, LockoutsPerDay),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.Before(out[j].At)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// New keeps the alerts whose ID is not in sent: the ones not raised before.
func New(alerts []Alert, sent map[string]bool) []Alert {
	var out []Alert
	for _, a := range alerts {
		if !sent[a.ID] {
			out = append(out, a)
		}
	}
	return out
}

type burst struct {
	count int
	at    time.Time // the first failure of the busiest hour
}

// busiestHour is, per UTC day (of the hour's first failure), the most
// failures within any hour that starts at one of them.
func busiestHour(times []time.Time) map[string]burst {
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	best := map[string]burst{}
	end := 0
	for start, t := range times {
		for end < len(times) && times[end].Sub(t) < time.Hour {
			end++
		}
		day := t.Format(time.DateOnly)
		if n := end - start; n > best[day].count {
			best[day] = burst{count: n, at: t}
		}
	}
	return best
}

// nameHash is a short SHA-256 of a username: an alert's ID tells two people
// apart without holding the name.
func nameHash(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:6])
}
