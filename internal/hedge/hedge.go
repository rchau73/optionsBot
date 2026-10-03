// Package hedge writes delta-hedge suggestions to hedge_report.json.
// It never places orders: hedging stays a human decision.
package hedge

import (
	"encoding/json"
	"log/slog"
	"math"
	"os"
	"sync"
	"time"
)

// Report is the structured hedge report output.
type Report struct {
	Timestamp       time.Time `json:"timestamp"`
	UnderlyingPrice float64   `json:"underlying_price"`
	NetDelta        float64   `json:"net_delta"`
	// Side is the perpetual trade that would flatten NetDelta:
	// "buy" when the book is short delta, "sell" when it is long.
	Side          string    `json:"side"`
	UncoveredQty  float64   `json:"uncovered_qty"`
	SuggestedInst string    `json:"suggested_instrument"`
	Tranches      []Tranche `json:"tranches"`
}

// Tranche is one step of a staged hedge (scale in rather than all at once).
type Tranche struct {
	Pct float64 `json:"pct"`
	Qty float64 `json:"qty"`
}

// trancheWeights stage a hedge as 5%, 10%, 15%, 30% and 40% of the exposure.
var trancheWeights = []float64{0.05, 0.10, 0.15, 0.30, 0.40}

// Reporter writes a report when the book's net delta is large enough to be
// worth hedging, and again only when it has moved materially since.
type Reporter struct {
	mu        sync.Mutex
	path      string
	threshold float64 // |net delta| in underlying units that warrants a report
	reported  bool
	lastDelta float64
}

func New(path string, threshold float64) *Reporter {
	return &Reporter{path: path, threshold: threshold}
}

// MaybeReport writes a report when |netDelta| ≥ threshold and net delta has
// changed by at least threshold since the last report. Small or unchanged
// exposure writes nothing, so the file is not rewritten on every tick.
func (r *Reporter) MaybeReport(netDelta, underlyingPrice float64, suggestedInst string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if math.Abs(netDelta) < r.threshold {
		return
	}
	if r.reported && math.Abs(netDelta-r.lastDelta) < r.threshold {
		return
	}
	if err := r.write(BuildReport(netDelta, underlyingPrice, suggestedInst, time.Now())); err != nil {
		slog.Error("hedge report write failed", "path", r.path, "err", err)
		return
	}
	r.reported = true
	r.lastDelta = netDelta
}

// BuildReport computes the hedge suggestion for a given net delta.
func BuildReport(netDelta, underlyingPrice float64, suggestedInst string, now time.Time) Report {
	uncovered := math.Abs(netDelta)
	tranches := make([]Tranche, len(trancheWeights))
	for i, w := range trancheWeights {
		tranches[i] = Tranche{Pct: w * 100, Qty: math.Round(uncovered*w*100) / 100}
	}
	side := "sell"
	if netDelta < 0 {
		side = "buy"
	}
	return Report{
		Timestamp:       now,
		UnderlyingPrice: underlyingPrice,
		NetDelta:        netDelta,
		Side:            side,
		UncoveredQty:    uncovered,
		SuggestedInst:   suggestedInst,
		Tranches:        tranches,
	}
}

func (r *Reporter) write(report Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(r.path, data, 0o644); err != nil {
		return err
	}
	slog.Info("hedge_report",
		"net_delta", report.NetDelta,
		"side", report.Side,
		"uncovered_qty", report.UncoveredQty,
		"suggested_instrument", report.SuggestedInst,
		"underlying_price", report.UnderlyingPrice,
	)
	return nil
}
