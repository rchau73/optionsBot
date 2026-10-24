package coverage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/idlemargin"
	"optionsbot/internal/recorder"
)

// Kind is how a dataset is stored and read.
type Kind string

const (
	KindBooks Kind = "books" // gzip CSV day files, recorder.ReadFile
	KindGEX   Kind = "gex"   // gzip JSON-lines day files, recorder.ReadGEXFile
	KindPnL   Kind = "pnl"   // pnl_history.jsonl
)

// Dataset is one recorded history of a bot and its recording period.
type Dataset struct {
	Name string
	Path string
	Kind Kind
	Step time.Duration
}

// Datasets are the recorded histories under a bot's data directory (the
// bot's own data/, e.g. data/btc/data), with the period each is written at.
func Datasets(base string) []Dataset {
	hourly := time.Duration(config.FixedMarketRecordMinutes) * time.Minute
	return []Dataset{
		{Name: "market", Path: filepath.Join(base, "market"), Kind: KindBooks, Step: hourly},
		{Name: "market_gex", Path: filepath.Join(base, "market_gex"), Kind: KindGEX, Step: time.Minute}, // one GEX snapshot a minute
		{Name: "market_usdc", Path: filepath.Join(base, "market_usdc"), Kind: KindBooks, Step: hourly},
		{Name: "pnl_history", Path: filepath.Join(base, "pnl_history.jsonl"), Kind: KindPnL,
			Step: time.Duration(config.FixedReportIntervalSec) * time.Second},
	}
}

// BaseDir accepts a bot folder (data/btc, whose files are in data/btc/data)
// or its data directory itself.
func BaseDir(dir string) string {
	if fi, err := os.Stat(filepath.Join(dir, "data")); err == nil && fi.IsDir() {
		return filepath.Join(dir, "data")
	}
	return dir
}

// ReadTimes returns the time of every record of a dataset whose day file
// falls in [from, to] by its name (a zero bound is open). found is false
// when the dataset does not exist, which is no records, not an error.
func ReadTimes(ds Dataset, from, to time.Time) (times []time.Time, found bool, err error) {
	if _, err := os.Stat(ds.Path); errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	} else if err != nil {
		return nil, false, err
	}
	if ds.Kind == KindPnL {
		points, _, err := idlemargin.ReadHistoryFile(ds.Path)
		if err != nil {
			return nil, true, err
		}
		for _, p := range points {
			times = append(times, p.T)
		}
		return times, true, nil
	}

	pattern := "*.csv.gz"
	if ds.Kind == KindGEX {
		pattern = "*.jsonl.gz"
	}
	paths, err := filepath.Glob(filepath.Join(ds.Path, pattern))
	if err != nil {
		return nil, true, err
	}
	sort.Strings(paths)
	for _, p := range paths {
		day, err := time.Parse(time.DateOnly, strings.SplitN(filepath.Base(p), ".", 2)[0])
		if err != nil {
			continue // not a day file
		}
		// A day file holds records of its own UTC day; keep one day either
		// side so a record can bridge a bin at the window's edge.
		if (!from.IsZero() && day.Before(from.UTC().Truncate(24*time.Hour).AddDate(0, 0, -1))) ||
			(!to.IsZero() && day.After(to.UTC().AddDate(0, 0, 1))) {
			continue
		}
		if ds.Kind == KindGEX {
			recs, err := recorder.ReadGEXFile(p)
			if err != nil {
				return nil, true, err
			}
			for _, r := range recs {
				times = append(times, r.ComputedAt)
			}
			continue
		}
		snaps, err := recorder.ReadFile(p)
		if err != nil {
			return nil, true, err
		}
		for _, s := range snaps {
			times = append(times, s.At)
		}
	}
	return times, true, nil
}

// Result is the coverage of every dataset of one bot over one window.
type Result struct {
	Dir      string    `json:"dir"`
	From     time.Time `json:"from"`
	To       time.Time `json:"to"` // excluded
	Datasets []Report  `json:"datasets"`
	// Days with hourly books but no minute GEX: the replay decides hourly
	// there (cmd/stress -replay falls back to the books alone).
	HourlyOnlyDays []string `json:"hourly_only_days"`
}

// Run reads every dataset under dir and reports its coverage of [from, to).
// A zero from starts at the first recorded day; a zero to ends at the end of
// the last recorded day, but never after now truncated to the hour (the
// current hour is still being recorded).
func Run(dir string, from, to, now time.Time) (Result, error) {
	base := BaseDir(dir)
	sets := Datasets(base)
	times := make([][]time.Time, len(sets))
	found := make([]bool, len(sets))
	var first, last time.Time
	for i, ds := range sets {
		ts, ok, err := ReadTimes(ds, from, to)
		if err != nil {
			return Result{}, fmt.Errorf("read %s: %w", ds.Name, err)
		}
		times[i], found[i] = ts, ok
		for _, t := range ts {
			if first.IsZero() || t.Before(first) {
				first = t
			}
			if t.After(last) {
				last = t
			}
		}
	}
	if from.IsZero() || to.IsZero() {
		if first.IsZero() {
			return Result{}, fmt.Errorf("no records under %s: pass -from and -to to report an empty window", base)
		}
		if from.IsZero() {
			from = first.UTC().Truncate(24 * time.Hour)
		}
		if to.IsZero() {
			to = last.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
			if limit := now.UTC().Truncate(time.Hour); limit.Before(to) {
				to = limit
			}
		}
	}
	if !to.After(from) {
		return Result{}, fmt.Errorf("window %s → %s is empty", from.Format(time.RFC3339), to.Format(time.RFC3339))
	}

	res := Result{Dir: base, From: from.UTC(), To: to.UTC()}
	byName := map[string]Report{}
	for i, ds := range sets {
		r := Coverage(times[i], from, to, ds.Step)
		r.Dataset, r.Path, r.Found = ds.Name, ds.Path, found[i]
		res.Datasets = append(res.Datasets, r)
		byName[ds.Name] = r
	}
	res.HourlyOnlyDays = HourlyOnlyDays(byName["market"], byName["market_gex"])
	return res, nil
}
