// Package backup checks a nightly copy of a bot's history (#328): the copied
// decision journal must replay, and it must hold no fewer events than the
// previous night's copy. scripts/agents/backup_data.sh makes the copy and runs
// cmd/backupcheck on it. Reads files only; never touches a live bot's files.
package backup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"optionsbot/internal/orders"
)

// ResultFile is the check's record, written into the backup directory so the
// next night's check can compare with it.
const ResultFile = "check.json"

// Result is what one backup directory holds.
type Result struct {
	Events         uint64    `json:"events"`          // events replayed from orders.log
	ArchivedEvents uint64    `json:"archived_events"` // events in archive/*/orders.log (--reset-history)
	Skipped        int       `json:"skipped"`         // unreadable lines (a torn last line is allowed)
	CheckedAt      time.Time `json:"checked_at"`
}

// Total is every event the bot ever journaled in this copy: an archived
// journal (--reset-history) still counts, so a reset is not a loss.
func (r Result) Total() uint64 { return r.Events + r.ArchivedEvents }

// Check replays dir/orders.log and every dir/archive/*/orders.log with
// orders.ReplayFile. It fails when a journal has an unreadable line other than
// a torn last one (the copy raced an append), or when the total is lower than
// the previous backup's (prevDir empty or without a check.json: no comparison).
func Check(dir, prevDir string, now time.Time) (Result, error) {
	res := Result{CheckedAt: now.UTC()}
	n, skipped, err := replay(filepath.Join(dir, "orders.log"))
	if err != nil {
		return res, err
	}
	res.Events, res.Skipped = n, skipped
	archived, err := filepath.Glob(filepath.Join(dir, "archive", "*", "orders.log"))
	if err != nil {
		return res, fmt.Errorf("list archived journals: %w", err)
	}
	sort.Strings(archived)
	for _, path := range archived {
		n, skipped, err := replay(path)
		if err != nil {
			return res, err
		}
		res.ArchivedEvents += n
		res.Skipped += skipped
	}
	if prevDir == "" {
		return res, nil
	}
	prev, ok, err := ReadResult(prevDir)
	if err != nil {
		return res, err
	}
	if ok && res.Total() < prev.Total() {
		return res, fmt.Errorf("journal shrank: %d events now, %d in the previous backup %s", res.Total(), prev.Total(), prevDir)
	}
	return res, nil
}

// replay returns the events of one journal and its unreadable lines. A
// missing journal is empty (the comparison with the previous night catches a
// lost one).
func replay(path string) (uint64, int, error) {
	rp, err := orders.ReplayFile(path, 0)
	if err != nil {
		return 0, 0, fmt.Errorf("replay %s: %w", path, err)
	}
	allowed := 0
	if torn, err := endsTorn(path); err != nil {
		return 0, 0, err
	} else if torn {
		allowed = 1
	}
	if rp.Skipped > allowed {
		return rp.Events, rp.Skipped, fmt.Errorf("replay %s: %d unreadable lines", path, rp.Skipped)
	}
	return rp.Events, rp.Skipped, nil
}

// endsTorn is true when the file's last byte is not a newline: the copy caught
// a line half-written, which the next night's copy will hold whole.
func endsTorn(path string) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return false, err
	}
	last := make([]byte, 1)
	if _, err := f.ReadAt(last, st.Size()-1); err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	return !bytes.Equal(last, []byte{'\n'}), nil
}

// LinesResult is the line check of a copied JSON-lines file.
type LinesResult struct {
	Lines   int // lines that parse as JSON
	Skipped int // unreadable lines (at most a torn last one, else Check fails)
}

// CheckLines checks a copied JSON-lines file (the monitor's assistant.jsonl,
// #438): every non-empty line must parse as JSON, except a torn last line
// (the copy raced an append). A missing file has no lines.
func CheckLines(path string) (LinesResult, error) {
	var res LinesResult
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return res, nil
	}
	if err != nil {
		return res, fmt.Errorf("read %s: %w", path, err)
	}
	torn := len(b) > 0 && b[len(b)-1] != '\n'
	lines := bytes.Split(b, []byte{'\n'})
	for i, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if json.Valid(line) {
			res.Lines++
			continue
		}
		res.Skipped++
		if !(torn && i == len(lines)-1) {
			return res, fmt.Errorf("%s: line %d is not JSON", path, i+1)
		}
	}
	return res, nil
}

// ReadResult reads dir/check.json; ok is false when there is none.
func ReadResult(dir string) (Result, bool, error) {
	b, err := os.ReadFile(filepath.Join(dir, ResultFile))
	if errors.Is(err, os.ErrNotExist) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, fmt.Errorf("read previous check: %w", err)
	}
	var r Result
	if err := json.Unmarshal(b, &r); err != nil {
		return Result{}, false, fmt.Errorf("decode %s: %w", filepath.Join(dir, ResultFile), err)
	}
	return r, true, nil
}

// WriteResult writes dir/check.json.
func WriteResult(dir string, r Result) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ResultFile), append(b, '\n'), 0o644)
}
