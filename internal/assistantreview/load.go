// Package assistantreview is the assistant's weekly quality review (#438): it
// reads the monitor's review log, data/monitor/assistant.jsonl (one "answer"
// line per question, one "flag" line each time someone marks an answer wrong,
// frontend/lib/assistant/log.js), and reports how the week's questions were
// answered, which answers were flagged, which questions the help could not
// answer and the help passage nearest each of them. Pure apart from Load and
// Write: no model, no network, and the log is only read.
//
// The full report holds question and answer text and stays in the private
// data/monitor/assistant_reviews/; only Summary (counts and percentages) may
// be posted anywhere.
package assistantreview

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// Line kinds of assistant.jsonl.
const (
	KindAnswer = "answer"
	KindFlag   = "flag"
)

// Entry is one line of assistant.jsonl: the fields the review reads. The
// user who asked is never read (the review names no one).
type Entry struct {
	Kind      string    `json:"kind"`
	ID        string    `json:"id"`
	TS        time.Time `json:"ts"`
	Bot       string    `json:"bot"`
	Question  string    `json:"question"`
	Sources   []string  `json:"sources"`
	Citations []string  `json:"citations"`
	Verdict   []string  `json:"verdict"`
	Outcome   string    `json:"outcome"`
	Answer    string    `json:"answer"`
	Note      string    `json:"note"` // a flag's note
	TestRun   bool      `json:"test_run"`
}

// Log is the file as read: its usable lines and how many could not be read.
type Log struct {
	Entries []Entry
	Skipped int // unreadable lines (a torn last line, a damaged one): counted, never fatal
	// TestRuns counts the lines of the golden-set run (#536, test_run: true),
	// left out: the run writes a log of its own, so one here was copied in by
	// hand, and a test question is never a person's.
	TestRuns int
}

// maxLine bounds one line (an answer line carries the model's text and a
// request draft; the monitor caps a question at a few hundred characters).
const maxLine = 4 << 20

// Parse reads JSON lines. A line that is not JSON, or has no kind or id, is
// skipped and counted; a test-run line is left out and counted; the rest are read.
func Parse(r io.Reader) (Log, error) {
	var lg Log
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), maxLine)
	for sc.Scan() {
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(b, &e); err != nil || e.Kind == "" || e.ID == "" {
			lg.Skipped++
			continue
		}
		if e.TestRun {
			lg.TestRuns++
			continue
		}
		lg.Entries = append(lg.Entries, e)
	}
	if err := sc.Err(); err != nil {
		return lg, fmt.Errorf("read assistant log: %w", err)
	}
	return lg, nil
}

// Load reads the log file; a missing file is an empty log.
func Load(path string) (Log, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Log{}, nil
	}
	if err != nil {
		return Log{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	return Parse(f)
}
