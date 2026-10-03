package history

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

// RegimeDay is the gamma regime at the close of one UTC day.
type RegimeDay struct {
	Day    time.Time `json:"day"` // UTC midnight
	Regime string    `json:"regime"`
}

// RegimeStore keeps the gamma regime per UTC day on disk. Deribit has no
// history of the GEX regime, so the margin policy's "confirmed at N daily
// closes" rule needs it recorded here to survive restarts.
//
// A line is appended when a day starts or its regime changes; on load the
// last line of each day wins, so the stored value is that day's close.
// Safe for concurrent use. An empty path keeps the history in memory only.
type RegimeStore struct {
	mu   sync.Mutex
	file *os.File
	days []RegimeDay // oldest first, one per day
	keep int
}

// regimeKeepDays bounds memory; confirmation needs only a few days.
const regimeKeepDays = 60

// OpenRegimes loads the regime history at path (creating it if needed).
func OpenRegimes(path string) (*RegimeStore, error) {
	s := &RegimeStore{keep: regimeKeepDays}
	if path == "" {
		return s, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("regime history dir: %w", err)
	}
	if err := s.load(path); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open regime history %s: %w", path, err)
	}
	s.file = f
	slog.Info("regime history loaded", "path", path, "days", len(s.days))
	return s, nil
}

func (s *RegimeStore) load(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read regime history %s: %w", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var d RegimeDay
		if json.Unmarshal(sc.Bytes(), &d) != nil || d.Day.IsZero() {
			continue // torn line from a crash
		}
		s.put(d)
	}
	return sc.Err()
}

// put stores d as its day's value (last write wins). Caller holds mu or owns s.
func (s *RegimeStore) put(d RegimeDay) (changed bool) {
	d.Day = d.Day.UTC().Truncate(24 * time.Hour)
	n := len(s.days)
	switch {
	case n > 0 && s.days[n-1].Day.Equal(d.Day):
		if s.days[n-1].Regime == d.Regime {
			return false
		}
		s.days[n-1].Regime = d.Regime
	case n > 0 && d.Day.Before(s.days[n-1].Day):
		return false
	default:
		s.days = append(s.days, d)
		if extra := len(s.days) - s.keep; extra > 0 {
			s.days = append(s.days[:0], s.days[extra:]...)
		}
	}
	return true
}

// Record notes the regime seen at t. Only a new day or a change is written.
func (s *RegimeStore) Record(t time.Time, regime string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := RegimeDay{Day: t, Regime: regime}
	if !s.put(d) || s.file == nil {
		return
	}
	d.Day = d.Day.UTC().Truncate(24 * time.Hour)
	data, _ := json.Marshal(d)
	if _, err := s.file.Write(append(data, '\n')); err != nil {
		slog.Error("regime history write failed", "err", err)
	}
}

// Daily returns the stored days, oldest first (the last may be today).
func (s *RegimeStore) Daily() []RegimeDay {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]RegimeDay(nil), s.days...)
}

// Close closes the file.
func (s *RegimeStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}
