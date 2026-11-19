package marketdata

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// IVHistory keeps one chain-IV reading per UTC day on disk (#834). Deribit has
// DVOL's history but none of a coin's chain IV, so the daily closes the
// percentile and the band confirmation rank against must be recorded here to
// survive restarts — 90 of them before the bands apply.
//
// A line is written when a day starts, then at most every
// ivHistoryWriteEvery, and the day's last reading when the next day starts;
// on load the last line of each day wins, so a day's
// stored value is its close (or the last reading before the bot stopped).
// Safe for concurrent use. An empty path keeps the history in memory only.
type IVHistory struct {
	mu        sync.Mutex
	file      *os.File
	days      []VolClose // oldest first, one per UTC day (At = UTC midnight)
	lastWrite time.Time
	dirty     bool // the current day holds a reading not written yet
}

// ivHistoryWriteEvery bounds the writes of one day's moving reading.
const ivHistoryWriteEvery = time.Hour

type ivLine struct {
	Day time.Time `json:"day"` // UTC midnight
	IV  float64   `json:"iv"`  // percent
}

// OpenIVHistory loads the chain-IV history at path (creating it if needed).
func OpenIVHistory(path string) (*IVHistory, error) {
	h := &IVHistory{}
	if path == "" {
		return h, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("chain IV history dir: %w", err)
	}
	if err := h.load(path); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open chain IV history %s: %w", path, err)
	}
	h.file = f
	slog.Info("chain IV history loaded", "path", path, "days", len(h.days))
	return h, nil
}

func (h *IVHistory) load(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read chain IV history %s: %w", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var l ivLine
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Day.IsZero() || l.IV <= 0 {
			continue // torn line from a crash
		}
		h.put(l.Day, l.IV)
	}
	return sc.Err()
}

// put stores v as day's value (last write wins); an older day is ignored.
// It reports whether day is a new one. Caller holds mu or owns h.
func (h *IVHistory) put(at time.Time, v float64) (newDay, ok bool) {
	day := at.UTC().Truncate(24 * time.Hour)
	n := len(h.days)
	switch {
	case n > 0 && h.days[n-1].At.Equal(day):
		h.days[n-1].Close = v
		return false, true
	case n > 0 && day.Before(h.days[n-1].At):
		return false, false
	}
	h.days = append(h.days, VolClose{At: day, Close: v})
	return true, true
}

// Record notes the reading v (percent) at t: a new day is written at once,
// a change within the day at most every ivHistoryWriteEvery.
func (h *IVHistory) Record(t time.Time, v float64) {
	if v <= 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var prev VolClose
	if n := len(h.days); n > 0 {
		prev = h.days[n-1]
	}
	newDay, ok := h.put(t, v)
	if !ok || h.file == nil {
		return
	}
	if newDay && h.dirty {
		h.write(prev) // the day that ended: its last reading is its close
	}
	if !newDay && t.Sub(h.lastWrite) < ivHistoryWriteEvery {
		h.dirty = true
		return
	}
	h.lastWrite = t
	h.write(h.days[len(h.days)-1])
}

// write appends one day's line. Caller holds mu.
func (h *IVHistory) write(d VolClose) {
	h.dirty = false
	data, _ := json.Marshal(ivLine{Day: d.At, IV: d.Close})
	if _, err := h.file.Write(append(data, '\n')); err != nil {
		slog.Error("chain IV history write failed", "err", err)
	}
}

// Daily returns the stored days, oldest first (the last may be today).
func (h *IVHistory) Daily() []VolClose {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]VolClose(nil), h.days...)
}

// Close closes the file.
func (h *IVHistory) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.file == nil {
		return nil
	}
	err := h.file.Close()
	h.file = nil
	return err
}
