package backtest

import (
	"sort"
	"time"
)

// Deribit's listing calendar, as the synthetic data generator (cmd/gendata
// -calendar deribit) models it, so the ladder sweep can compare ladders with
// and without weeklies (#83).
const (
	// WeeklyListDays: a Friday weekly is listed this many days before it
	// expires. Testnet listed 6NOV26 on 2026-10-15 (22 days) and 13NOV26 by
	// 2026-10-23 (21 days).
	WeeklyListDays = 22
	// ListedMonthlies: the next monthlies (last Friday of a month) listed.
	ListedMonthlies = 3
	// ListedQuarterlies: the quarterlies (last Friday of Mar, Jun, Sep, Dec)
	// listed beyond the monthlies.
	ListedQuarterlies = 2
)

// LastFriday is the last Friday of year/month, at 08:00 UTC like Deribit's
// expiries.
func LastFriday(year int, month time.Month) time.Time {
	d := time.Date(year, month+1, 0, 8, 0, 0, 0, time.UTC) // last day of month
	for d.Weekday() != time.Friday {
		d = d.AddDate(0, 0, -1)
	}
	return d
}

// DeribitListedExpiries returns the expiries listed on day (UTC date, any
// time), oldest first: every Friday that expires after day and at most
// WeeklyListDays days later, the next ListedMonthlies monthlies and the next
// ListedQuarterlies quarterlies beyond them. Dailies are left out (no slot
// targets them). Pure: day is the only clock.
func DeribitListedExpiries(day time.Time) []time.Time {
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	seen := map[time.Time]bool{}
	var out []time.Time
	add := func(e time.Time) {
		if e.After(day) && !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}

	for d := day.AddDate(0, 0, 1); !d.After(day.AddDate(0, 0, WeeklyListDays)); d = d.AddDate(0, 0, 1) {
		if d.Weekday() == time.Friday {
			add(time.Date(d.Year(), d.Month(), d.Day(), 8, 0, 0, 0, time.UTC))
		}
	}

	monthlies, quarterlies := 0, 0
	for m := 0; monthlies < ListedMonthlies || quarterlies < ListedQuarterlies; m++ {
		first := time.Date(day.Year(), day.Month()+time.Month(m), 1, 0, 0, 0, 0, time.UTC)
		e := LastFriday(first.Year(), first.Month())
		if !e.After(day) {
			continue
		}
		switch {
		case monthlies < ListedMonthlies:
			monthlies++
			add(e)
		case first.Month()%3 == 0:
			quarterlies++
			add(e)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}
