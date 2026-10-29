package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Run is RunWith on the fixed drop rules and DropBaseWeeks.
func Run(eventsPath string, from, to time.Time, label, outDir string) (Report, []string, error) {
	return RunWith(eventsPath, from, to, label, outDir, DefaultRules(), DropBaseWeeks)
}

// RunWith reads the events file, builds the report of [from, to), adds its
// drop check against the baseWeeks weeks before it (Drops with rules) and
// writes <outDir>/<label>.md and .json. A missing or empty file is a report
// that says no events were recorded, not an error. Returns the two paths.
func RunWith(eventsPath string, from, to time.Time, label, outDir string, rules Rules, baseWeeks int) (Report, []string, error) {
	lg, err := Load(eventsPath)
	if err != nil {
		return Report{}, nil, err
	}
	r := BuildLog(lg, from, to)
	r.Week = label
	a := Drops(r, BuildWeeks(lg.Events, from, baseWeeks), rules)
	r.Drops = &a
	paths, err := Write(outDir, label, r)
	return r, paths, err
}

// Write saves the report as <dir>/<label>.md and .json (0600: the monitor's
// data stays its own).
func Write(dir, label string, r Report) ([]string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	var md strings.Builder
	if err := Markdown(&md, r); err != nil {
		return nil, err
	}
	js, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode report: %w", err)
	}
	mdPath := filepath.Join(dir, label+".md")
	jsPath := filepath.Join(dir, label+".json")
	if err := os.WriteFile(mdPath, []byte(md.String()), 0o600); err != nil {
		return nil, fmt.Errorf("write %s: %w", mdPath, err)
	}
	if err := os.WriteFile(jsPath, append(js, '\n'), 0o600); err != nil {
		return nil, fmt.Errorf("write %s: %w", jsPath, err)
	}
	return []string{mdPath, jsPath}, nil
}
