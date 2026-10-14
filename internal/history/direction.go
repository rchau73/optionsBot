package history

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DirectionRecord is the GEX picture when either direction source or the
// shed signal changed: the SMA trend and the GEX bias side by side, and
// which one the rules used, so the two can be compared over weeks of live
// data (2026-10-12: the bots switched from the SMA trend to the bias).
type DirectionRecord struct {
	At        time.Time `json:"at"`
	Spot      float64   `json:"spot"`
	Flip      float64   `json:"flip,omitempty"`
	BiasFlip  float64   `json:"bias_flip,omitempty"` // median flip the bias was also held against
	BufferPct float64   `json:"buffer_pct"`
	Regime    string    `json:"regime"`
	Direction string    `json:"direction"` // sma | bias: the source the rules used
	SMATrend  string    `json:"sma_trend"` // bull | bear | neutral
	Bias      string    `json:"gex_bias"`  // up | down | neutral
	Action    string    `json:"action"`    // the live shed signal
	Shed      string    `json:"shed"`      // the confirmed shed
	// What the other source would have shed now (the shadow decision).
	OtherAction string `json:"other_action"`
}

// DirectionLog appends DirectionRecords to a JSONL file. Trading never
// reads it. Safe for concurrent use; an empty path discards the records.
type DirectionLog struct {
	mu   sync.Mutex
	file *os.File
}

// OpenDirections opens (creating if needed) the direction history at path.
func OpenDirections(path string) (*DirectionLog, error) {
	l := &DirectionLog{}
	if path == "" {
		return l, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("direction history dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open direction history %s: %w", path, err)
	}
	l.file = f
	return l, nil
}

// Record appends r.
func (l *DirectionLog) Record(r DirectionRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return
	}
	data, _ := json.Marshal(r)
	if _, err := l.file.Write(append(data, '\n')); err != nil {
		slog.Error("direction history write failed", "err", err)
	}
}

// Close closes the file.
func (l *DirectionLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	return l.file.Close()
}
