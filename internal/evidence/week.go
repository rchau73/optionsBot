package evidence

import (
	"fmt"
	"time"
)

// Window is the report's period: [From, To) in UTC, with the label that
// names its folder (data/evidence/<Label>/).
type Window struct {
	From, To time.Time // To is exclusive
	Label    string
}

// Last is the last instant inside the window (the inclusive end the
// replay and perf take).
func (w Window) Last() time.Time { return w.To.Add(-time.Nanosecond) }

// ParseWeek reads an ISO week "2026-W42": Monday 00:00 UTC to the next
// Monday.
func ParseWeek(s string) (Window, error) {
	var year, week int
	if n, err := fmt.Sscanf(s, "%d-W%d", &year, &week); n != 2 || err != nil {
		return Window{}, fmt.Errorf("week %q: want YYYY-Www (e.g. 2026-W42)", s)
	}
	// 4 January is always in week 1; its Monday starts the ISO year.
	jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, time.UTC)
	week1 := jan4.AddDate(0, 0, -((int(jan4.Weekday()) + 6) % 7))
	from := week1.AddDate(0, 0, 7*(week-1))
	if y, w := from.ISOWeek(); y != year || w != week {
		return Window{}, fmt.Errorf("week %q: %d has no week %d", s, year, week)
	}
	return Window{From: from, To: from.AddDate(0, 0, 7), Label: weekLabel(from)}, nil
}

// LastWeek is the last complete ISO week before now.
func LastWeek(now time.Time) Window {
	day := utcDay(now)
	monday := day.AddDate(0, 0, -((int(day.Weekday())+6)%7)-7)
	return Window{From: monday, To: monday.AddDate(0, 0, 7), Label: weekLabel(monday)}
}

// ParseDays reads -from/-to dates (YYYY-MM-DD, both included).
func ParseDays(from, to string) (Window, error) {
	f, err := time.Parse(time.DateOnly, from)
	if err != nil {
		return Window{}, fmt.Errorf("-from %q: want YYYY-MM-DD", from)
	}
	t, err := time.Parse(time.DateOnly, to)
	if err != nil {
		return Window{}, fmt.Errorf("-to %q: want YYYY-MM-DD", to)
	}
	if t.Before(f) {
		return Window{}, fmt.Errorf("-to %s is before -from %s", to, from)
	}
	return Window{From: f, To: t.AddDate(0, 0, 1), Label: from + "_" + to}, nil
}

func weekLabel(monday time.Time) string {
	y, w := monday.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
