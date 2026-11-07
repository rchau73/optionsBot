package assistantreview

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The three ways an answer of the review list is closed (#704), each with what
// its ref holds.
const (
	HowHelpFix        = "help_fix"         // the help passage, or the PR/issue number
	HowGoldenCase     = "golden_case"      // the golden id
	HowAnswerWasRight = "answer_was_right" // a short reason, no personal data
)

// Hows is the report's order of the ways to close.
var Hows = []string{HowHelpFix, HowGoldenCase, HowAnswerWasRight}

// OverdueDays is the review rule's limit: a row open more than this many whole
// days is overdue, so one open exactly 7 days (or 7 days and some hours) is not.
const OverdueDays = 7

// ClosuresFile is the closure record's name in the review folder: private,
// gitignored, 0600, and appended to only by Close.
const ClosuresFile = "closures.jsonl"

// Closure is one line of closures.jsonl: how a person closed one answer.
type Closure struct {
	AnswerID string    `json:"answer_id"`
	ClosedAt time.Time `json:"closed_at"`
	How      string    `json:"how"`
	Ref      string    `json:"ref"`
}

// Closures is the file as read: the closure of each answer id and how many
// lines could not be used. A skipped line leaves its answer open, the safe side.
type Closures struct {
	ByID    map[string]Closure
	Skipped int
}

// ParseClosures reads JSON lines. A line that is not JSON, has no id or time or
// holds an unknown how is skipped and counted; the first closure of an id wins.
func ParseClosures(r io.Reader) (Closures, error) {
	cs := Closures{ByID: map[string]Closure{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), maxLine)
	for sc.Scan() {
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 {
			continue
		}
		var c Closure
		if err := json.Unmarshal(b, &c); err != nil || c.AnswerID == "" || c.ClosedAt.IsZero() || !contains(Hows, c.How) {
			cs.Skipped++
			continue
		}
		if _, seen := cs.ByID[c.AnswerID]; !seen {
			cs.ByID[c.AnswerID] = c
		}
	}
	if err := sc.Err(); err != nil {
		return cs, fmt.Errorf("read closures: %w", err)
	}
	return cs, nil
}

// LoadClosures reads the closure file; a missing file holds no closure.
func LoadClosures(path string) (Closures, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Closures{}, nil
	}
	if err != nil {
		return Closures{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	return ParseClosures(f)
}

// NewClosure checks a closure against the log and the closures so far. It
// refuses, each with its own message, an id no flagged or "I don't know" answer
// has, an unknown how, an empty ref and a second closure of one id.
func NewClosure(lg Log, closures Closures, id, how, ref string, now time.Time) (Closure, error) {
	id, ref = strings.TrimSpace(id), strings.TrimSpace(ref)
	if !inReviewList(lg, id) {
		return Closure{}, fmt.Errorf("unknown id %q: no flagged or \"I don't know\" answer has it", id)
	}
	if !contains(Hows, how) {
		return Closure{}, fmt.Errorf("unknown how %q: use %s", how, strings.Join(Hows, ", "))
	}
	if ref == "" {
		return Closure{}, fmt.Errorf("empty ref: give the help passage or PR (%s), the golden id (%s) or a short reason (%s)",
			HowHelpFix, HowGoldenCase, HowAnswerWasRight)
	}
	if c, closed := closures.ByID[id]; closed {
		return Closure{}, fmt.Errorf("already closed: %s was closed on %s as %s", id, c.ClosedAt.UTC().Format(time.DateOnly), c.How)
	}
	return Closure{AnswerID: id, ClosedAt: now.UTC(), How: how, Ref: ref}, nil
}

// inReviewList says whether the log has this answer and it belongs on a review
// list: flagged, or an "I don't know".
func inReviewList(lg Log, id string) bool {
	answered, listed := false, false
	for _, e := range lg.Entries {
		if e.ID != id {
			continue
		}
		switch e.Kind {
		case KindAnswer:
			answered = true
			listed = listed || kindOf(e.Outcome) == OutcomeDontKnow
		case KindFlag:
			listed = true
		}
	}
	return answered && listed
}

// Close records one closure: it reads the review log (never writes it), checks
// the closure with NewClosure and appends one line. A refused closure writes
// nothing.
func Close(logPath, closuresPath, id, how, ref string, now time.Time) (Closure, error) {
	lg, err := Load(logPath)
	if err != nil {
		return Closure{}, err
	}
	closures, err := LoadClosures(closuresPath)
	if err != nil {
		return Closure{}, err
	}
	c, err := NewClosure(lg, closures, id, how, ref, now)
	if err != nil {
		return Closure{}, err
	}
	return c, appendClosure(closuresPath, c)
}

func appendClosure(path string, c Closure) error {
	line, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode closure: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// ClosureAudit is one line per closed row of the review, for the weekly run's
// log: the closure's date and how, never its id or ref.
func ClosureAudit(r Report) []string {
	var out []string
	for _, it := range r.Review {
		if it.Closure != nil {
			out = append(out, fmt.Sprintf("closure %s %s", it.Closure.ClosedAt.UTC().Format(time.DateOnly), it.Closure.How))
		}
	}
	return out
}

// status is how the report marks a row: "closed (how)" or "open N days",
// overdue named.
func (it ReviewItem) status() string {
	if it.Closure != nil {
		return "closed (" + it.Closure.How + ")"
	}
	s := fmt.Sprintf("open %d days", *it.OpenDays)
	if *it.OpenDays == 1 {
		s = "open 1 day"
	}
	if it.Overdue {
		s += ", overdue"
	}
	return s
}
