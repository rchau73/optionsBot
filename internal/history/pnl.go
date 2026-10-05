// Package history keeps the P&L history of one bot on disk, so the monitor
// can chart minutes to months and the line survives restarts.
//
// Storage is an append-only JSON-lines file (one point per line). On start
// the file is reloaded; a torn or corrupt line (e.g. a crash mid-write) is
// skipped, never fatal. The last maxAge of points is kept in memory for
// queries; the file itself is never rewritten.
package history

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Point is the strategy's total P&L at one moment. Coin amounts are in the
// underlying (BTC/ETH); USD amounts use Spot at that moment.
type Point struct {
	T           time.Time `json:"t"`
	Realised    float64   `json:"realised"`   // cumulative across restarts
	Unrealised  float64   `json:"unrealised"` // open legs marked to mid
	Total       float64   `json:"total"`
	Spot        float64   `json:"spot"`
	RealisedUSD float64   `json:"realised_usd"`
	TotalUSD    float64   `json:"total_usd"`
}

// Store is the P&L history of one bot. Safe for concurrent use.
type Store struct {
	mu     sync.RWMutex
	file   *os.File
	points []Point // ordered by time
	maxAge time.Duration
}

// Open loads the history at path (creating the file and its directory if
// needed) and keeps the last maxAge of points in memory.
func Open(path string, maxAge time.Duration, now time.Time) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("pnl history dir: %w", err)
	}
	s := &Store{maxAge: maxAge}
	if err := s.load(path, now); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open pnl history %s: %w", path, err)
	}
	s.file = f
	slog.Info("pnl history loaded", "path", path, "points", len(s.points))
	return s, nil
}

func (s *Store) load(path string, now time.Time) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read pnl history %s: %w", path, err)
	}
	defer f.Close()

	cutoff := now.Add(-s.maxAge)
	skipped := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var p Point
		if err := json.Unmarshal(sc.Bytes(), &p); err != nil || p.T.IsZero() {
			skipped++
			continue
		}
		if p.T.After(cutoff) {
			s.points = append(s.points, p)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read pnl history %s: %w", path, err)
	}
	if skipped > 0 {
		slog.Warn("pnl history: skipped unreadable lines", "path", path, "lines", skipped)
	}
	sort.SliceStable(s.points, func(i, j int) bool { return s.points[i].T.Before(s.points[j].T) })
	return nil
}

// RecordPnL appends a point. realised is cumulative: the bot restores it
// from the journal at startup, so the store adds nothing of its own (it once
// carried its last point forward — two sources that could disagree).
func (s *Store) RecordPnL(t time.Time, realised, unrealised, spot float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := realised
	p := Point{
		T: t, Realised: r, Unrealised: unrealised, Total: r + unrealised,
		Spot: spot, RealisedUSD: r * spot, TotalUSD: (r + unrealised) * spot,
	}
	if data, err := json.Marshal(p); err == nil && s.file != nil {
		if _, err := s.file.Write(append(data, '\n')); err != nil {
			slog.Error("pnl history write failed", "err", err)
		}
	}
	s.points = append(s.points, p)
	cutoff := t.Add(-s.maxAge)
	trim := 0
	for trim < len(s.points) && s.points[trim].T.Before(cutoff) {
		trim++
	}
	if trim > 0 {
		s.points = append(s.points[:0], s.points[trim:]...)
	}
}

// Range returns the points in [from, to] reduced to at most `buckets` points:
// time is split into equal buckets aligned to the clock (so several bots'
// histories line up) and each bucket keeps its last point — P&L is a level,
// so the latest value is the honest one to show.
func (s *Store) Range(from, to time.Time, buckets int) []Point {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Bucket(s.points, from, to, buckets)
}

// Bucket is the pure reduction used by Range.
func Bucket(points []Point, from, to time.Time, buckets int) []Point {
	out := []Point{}
	if buckets <= 0 || !to.After(from) {
		return out
	}
	width := BucketWidth(to.Sub(from), buckets)
	var cur Point
	var curKey int64
	have := false
	for _, p := range points {
		if p.T.Before(from) || p.T.After(to) {
			continue
		}
		key := p.T.UnixNano() / int64(width)
		if have && key != curKey {
			out = append(out, cur)
		}
		cur, curKey, have = p, key, true
		cur.T = time.Unix(0, key*int64(width)).UTC()
	}
	if have {
		out = append(out, cur)
	}
	return out
}

// BucketWidth is span/buckets rounded up to a whole second (at least 1 s).
func BucketWidth(span time.Duration, buckets int) time.Duration {
	n := time.Duration(buckets)
	w := (span + n - 1) / n                               // ceil(span / buckets)
	w = (w + time.Second - 1) / time.Second * time.Second // up to a whole second
	if w < time.Second {
		return time.Second
	}
	return w
}

// Close closes the file.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}

// Archive moves the existing files among paths into dir (created if needed)
// and returns where each went. Nothing is deleted: a reset history can be
// brought back by moving the files again.
func Archive(paths []string, dir string) ([]string, error) {
	var moved []string
	for _, p := range paths {
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return moved, fmt.Errorf("archive dir: %w", err)
		}
		dst := filepath.Join(dir, filepath.Base(p))
		if err := os.Rename(p, dst); err != nil {
			return moved, fmt.Errorf("archive %s: %w", p, err)
		}
		moved = append(moved, dst)
	}
	return moved, nil
}
