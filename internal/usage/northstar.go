package usage

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// RunningFile is the bot file that defines a strategy-running day (#801):
// the regime history, one line per UTC day the bot ran (a line is written when
// a day starts or its regime changes). It holds no amount; the report reads
// each line's day and seeded mark only. Seeded lines — the closes a brand-new
// bot writes for the days before its first start — are not running days.
const RunningFile = "regime_history.jsonl"

// RunningRule says in the report what defined a running day.
const RunningRule = "a strategy-running day = a UTC day with a line in a bot's data/" + RunningFile + " (seeded lines left out)"

// NorthStarMinPeople is the fewest people with a visit in the window before
// the share is judged, like the other sections' DropMinBase.
const NorthStarMinPeople = DropMinBase

// Running is one environment's strategy-running days, read from its clone's
// bots (LoadRunning). Env "" is the one environment the window's visits name
// (BuildNorthStar), "unknown" when they name none or several.
type Running struct {
	Env   string
	Files []string            // the files read
	Days  map[string]struct{} // UTC days ("2006-01-02") with a running bot
}

// NorthStar is PRD §7's product-phase north star (#801): environments
// (one Deribit account each) with at least one strategy-running day and at
// least one session_started in the MonthDays days ending with the window.
// Counts and shares only.
type NorthStar struct {
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`      // exclusive
	Running string    `json:"running"` // what defined a running day (RunningRule)
	Files   []string  `json:"files"`   // the bot files read

	Active           int      `json:"active"`                     // running and visited: the monthly active accounts
	VisitsNotRunning int      `json:"visits_not_running"`         // visited, no running day
	RunningNoVisits  int      `json:"running_no_visits"`          // running, no visit
	Environments     int      `json:"environments"`               // with a visit or a running day
	ActiveSharePct   *float64 `json:"active_share_pct,omitempty"` // Active ÷ Environments; absent under NorthStarMinPeople people
	People           int      `json:"people"`                     // distinct people with a session_started in the window
	Note             string   `json:"note,omitempty"`             // StatusTooFew under NorthStarMinPeople people

	ActiveEnvs           []string       `json:"active_envs"`
	VisitsNotRunningEnvs []string       `json:"visits_not_running_envs"`
	RunningNoVisitsEnvs  []string       `json:"running_no_visits_envs"`
	RunningDays          map[string]int `json:"running_days"` // environment → running days in the window
}

// LoadRunning reads the running days of one environment's clone: every
// <dataDir>/<bot>/data/RunningFile. No file is no running day, not an error;
// a torn line is skipped.
func LoadRunning(env, dataDir string) (Running, error) {
	r := Running{Env: env, Days: map[string]struct{}{}}
	files, err := filepath.Glob(filepath.Join(dataDir, "*", "data", RunningFile))
	if err != nil {
		return r, err
	}
	slices.Sort(files)
	r.Files = files
	for _, f := range files {
		if err := readRunningDays(f, r.Days); err != nil {
			return r, err
		}
	}
	return r, nil
}

func readRunningDays(path string, into map[string]struct{}) error {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var d struct {
			Day    time.Time `json:"day"`
			Seeded bool      `json:"seeded"`
		}
		if json.Unmarshal(sc.Bytes(), &d) != nil || d.Day.IsZero() || d.Seeded {
			continue
		}
		into[d.Day.UTC().Format(time.DateOnly)] = struct{}{}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}

// BuildNorthStar counts the north star over the MonthDays days ending at to,
// from the events (a visit = a session_started, by environment; a line
// without one is "unknown") and the running days of each environment.
// Events at or after to are ignored. Every collected event follows a yes to
// analytics from a signed-in person, so every session_started counts.
func BuildNorthStar(events []Event, running []Running, to time.Time) *NorthStar {
	to = to.UTC()
	from := to.AddDate(0, 0, -MonthDays)
	ns := &NorthStar{
		From: from, To: to, Running: RunningRule, Files: []string{},
		ActiveEnvs: []string{}, VisitsNotRunningEnvs: []string{}, RunningNoVisitsEnvs: []string{}, RunningDays: map[string]int{},
	}
	visited := map[string]bool{}
	people := map[string]bool{}
	for _, e := range events {
		if e.Name != "session_started" || e.TS.Before(from) || !e.TS.Before(to) {
			continue
		}
		env := e.Environment
		if env == "" {
			env = "unknown"
		}
		visited[env] = true
		people[e.UserID] = true
	}
	ns.People = len(people)
	envs := map[string]bool{}
	for env := range visited {
		envs[env] = true
	}
	for _, r := range running {
		env := r.Env
		if env == "" {
			env = soleEnv(visited)
		}
		ns.Files = append(ns.Files, r.Files...)
		for d := range r.Days {
			t, err := time.Parse(time.DateOnly, d)
			if err == nil && t.Add(24*time.Hour).After(from) && t.Before(to) {
				ns.RunningDays[env]++
			}
		}
		if ns.RunningDays[env] > 0 {
			envs[env] = true
		}
	}
	names := make([]string, 0, len(envs))
	for env := range envs {
		names = append(names, env)
	}
	slices.Sort(names)
	for _, env := range names {
		switch run := ns.RunningDays[env] > 0; {
		case run && visited[env]:
			ns.ActiveEnvs = append(ns.ActiveEnvs, env)
		case visited[env]:
			ns.VisitsNotRunningEnvs = append(ns.VisitsNotRunningEnvs, env)
		default:
			ns.RunningNoVisitsEnvs = append(ns.RunningNoVisitsEnvs, env)
		}
	}
	ns.Active, ns.VisitsNotRunning, ns.RunningNoVisits = len(ns.ActiveEnvs), len(ns.VisitsNotRunningEnvs), len(ns.RunningNoVisitsEnvs)
	ns.Environments = len(names)
	if ns.People < NorthStarMinPeople {
		ns.Note = StatusTooFew
	} else {
		ns.ActiveSharePct = pct(ns.Active, ns.Environments)
	}
	return ns
}

// soleEnv is the one environment of the visits, "unknown" for none or several.
func soleEnv(visited map[string]bool) string {
	if len(visited) != 1 {
		return "unknown"
	}
	for env := range visited {
		return env
	}
	return "unknown"
}
