package recorder

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"optionsbot/internal/gex"
)

// The GEX side at the bot's pace (#122): the hourly book is too coarse to see
// the flip jumps that lasted minutes and were confirmed within minutes, so
// every GEX snapshot the manager publishes (one a minute) is also written,
// as one small JSON line, to data/market_gex/YYYY-MM-DD.jsonl.gz. It is the
// snapshot the bot already computed: no Deribit call, never read by trading.

// GEXRecord is one GEX snapshot as the bot saw it.
type GEXRecord struct {
	ComputedAt    time.Time `json:"computed_at"`
	Spot          float64   `json:"spot"`   // the snapshot's spot (gex.Build)
	Regime        string    `json:"regime"` // as published (hysteresis applied)
	Flip          float64   `json:"flip"`   // the live flip; 0 when not found
	FlipFound     bool      `json:"flip_found"`
	MedianFlip    float64   `json:"median_flip"`     // median flip of the bias window (StableBiasDir); 0 without a window
	GEXSum        float64   `json:"gex_sum"`         // Σ weighted signed GEX (RegimeScore)
	DVOL          float64   `json:"dvol"`            // the bot's DVOL reading (0 = none yet)
	FlipBufferPct float64   `json:"flip_buffer_pct"` // the shed buffer at that DVOL, % of spot
}

// Snapshot is the record as the GEX snapshot the strategy's GammaMonitor reads.
func (r GEXRecord) Snapshot() *gex.Snapshot {
	return &gex.Snapshot{ComputedAt: r.ComputedAt, Spot: r.Spot, RegimeScore: r.GEXSum, Regime: r.Regime,
		GammaFlip: r.Flip, GammaFlipFound: r.FlipFound}
}

// GEXSource is the GEX manager's published snapshot.
type GEXSource interface {
	Snapshot() *gex.Snapshot
}

// gexPoll is how often the recorder looks for a new snapshot; each one is
// written once, so this only bounds the delay (the manager refreshes every 60 s).
const gexPoll = 10 * time.Second

// GEXRecorder writes each new GEX snapshot into dir.
type GEXRecorder struct {
	src        GEXSource
	dvol       func() float64
	bufferPct  func() float64
	flipWindow time.Duration
	dir        string

	last  time.Time // ComputedAt of the last snapshot written
	flips []flipAt  // flips of the bias window, for MedianFlip
}

type flipAt struct {
	at   time.Time
	flip float64
}

// NewGEXRecorder returns a recorder of src's snapshots; dvol and bufferPct
// read the bot's DVOL and shed buffer, flipWindow is the bias's median window
// (gex_bias_flip_window_minutes).
func NewGEXRecorder(src GEXSource, dvol, bufferPct func() float64, flipWindow time.Duration, dir string) *GEXRecorder {
	return &GEXRecorder{src: src, dvol: dvol, bufferPct: bufferPct, flipWindow: flipWindow, dir: dir}
}

// Run records every new snapshot until ctx ends.
func (r *GEXRecorder) Run(ctx context.Context) {
	slog.Info("gex recorder started", "dir", r.dir)
	t := time.NewTicker(gexPoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if _, err := r.Record(); err != nil {
			slog.Warn("gex snapshot not recorded", "dir", r.dir, "err", err)
		}
	}
}

// Record appends the current snapshot to its day's file unless it was
// already written; it reports whether it wrote one.
func (r *GEXRecorder) Record() (bool, error) {
	snap := r.src.Snapshot()
	if snap == nil || !snap.ComputedAt.After(r.last) {
		return false, nil
	}
	r.last = snap.ComputedAt
	rec := GEXRecord{ComputedAt: snap.ComputedAt.UTC(), Spot: snap.Spot, Regime: snap.Regime, Flip: snap.GammaFlip,
		FlipFound: snap.GammaFlipFound, MedianFlip: r.medianFlip(snap), GEXSum: snap.RegimeScore}
	if r.dvol != nil {
		rec.DVOL = r.dvol()
	}
	if r.bufferPct != nil {
		rec.FlipBufferPct = r.bufferPct()
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return false, err
	}
	path := filepath.Join(r.dir, rec.ComputedAt.Format("2006-01-02")+".jsonl.gz")
	return true, appendGzip(path, append(line, '\n'))
}

// medianFlip keeps snap's flip in the window and returns the window's
// median, as GammaMonitor.medianFlip does for StableBiasDir.
func (r *GEXRecorder) medianFlip(snap *gex.Snapshot) float64 {
	if r.flipWindow <= 0 {
		return 0
	}
	if snap.GammaFlipFound && snap.GammaFlip > 0 {
		r.flips = append(r.flips, flipAt{snap.ComputedAt, snap.GammaFlip})
	}
	cut := snap.ComputedAt.Add(-r.flipWindow)
	i := 0
	for i < len(r.flips)-1 && !r.flips[i].at.After(cut) {
		i++
	}
	r.flips = r.flips[i:]
	if len(r.flips) == 0 {
		return 0
	}
	v := make([]float64, len(r.flips))
	for i, f := range r.flips {
		v[i] = f.flip
	}
	sort.Float64s(v)
	n := len(v)
	if n%2 == 1 {
		return v[n/2]
	}
	return (v[n/2-1] + v[n/2]) / 2
}

// appendGzip appends data to path as one more gzip member (see appendGzipCSV).
func appendGzip(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	zw := gzip.NewWriter(file)
	if _, err := zw.Write(data); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return file.Sync()
}

// ReadGEXDir reads every GEX day file in dir from the UTC day of from through
// the day of to (both included; a zero bound is open), in time order. A
// missing dir is no records, not an error: the replay then runs hourly.
func ReadGEXDir(dir string, from, to time.Time) ([]GEXRecord, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.jsonl.gz"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var out []GEXRecord
	for _, p := range paths {
		day, err := time.Parse("2006-01-02", filepath.Base(p)[:min(10, len(filepath.Base(p)))])
		if err != nil {
			continue // not a day file
		}
		if (!from.IsZero() && day.Before(from.UTC().Truncate(24*time.Hour))) || (!to.IsZero() && day.After(to.UTC())) {
			continue
		}
		recs, err := ReadGEXFile(p)
		if err != nil {
			return nil, err
		}
		out = append(out, recs...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ComputedAt.Before(out[j].ComputedAt) })
	return out, nil
}

// ReadGEXFile reads one GEX day file. A torn last member (a crash while
// appending) is dropped; a bad line in a complete member is an error.
func ReadGEXFile(path string) ([]GEXRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(bufio.NewReader(f))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	data, err := io.ReadAll(zr) // multistream: every member in turn
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, gzip.ErrChecksum) {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	torn := err != nil
	var out []GEXRecord
	lines := bytes.Split(data, []byte("\n"))
	for i, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec GEXRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			if torn && i == len(lines)-1 {
				break // the torn member's partial line
			}
			return nil, fmt.Errorf("read %s: line %d: %w", path, i+1, err)
		}
		out = append(out, rec)
	}
	return out, nil
}
