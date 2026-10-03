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
	UncoveredQty    float64   `json:"uncovered_qty"`
	SuggestedInst   string    `json:"suggested_instrument"`
	Tranches        []Tranche `json:"tranches"`
}

type Tranche struct {
	Pct float64 `json:"pct"`
	Qty float64 `json:"qty"`
}

// Reporter generates hedge reports and writes them to hedge_report.json.
// It never places orders — reporting only.
type Reporter struct {
	mu        sync.Mutex
	path      string
	lastDelta float64
	threshold float64 // fractional change threshold to trigger refresh
}

func New(path string, threshold float64) *Reporter {
	return &Reporter{path: path, threshold: threshold}
}

// MaybeReport emits a new report if the delta changed materially.
func (r *Reporter) MaybeReport(netDelta, underlyingPrice float64, suggestedInst string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.lastDelta != 0 {
		change := math.Abs((netDelta - r.lastDelta) / r.lastDelta)
		if change < r.threshold {
			return
		}
	}
	r.lastDelta = netDelta
	r.emit(netDelta, underlyingPrice, suggestedInst)
}

func (r *Reporter) emit(netDelta, underlyingPrice float64, suggestedInst string) {
	uncovered := math.Abs(netDelta)
	pcts := []float64{0.05, 0.10, 0.15, 0.30, 0.40}
	tranches := make([]Tranche, len(pcts))
	for i, p := range pcts {
		tranches[i] = Tranche{Pct: p * 100, Qty: math.Round(uncovered*p*100) / 100}
	}

	report := Report{
		Timestamp:       time.Now(),
		UnderlyingPrice: underlyingPrice,
		NetDelta:        netDelta,
		UncoveredQty:    uncovered,
		SuggestedInst:   suggestedInst,
		Tranches:        tranches,
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		slog.Error("hedge report marshal", "err", err)
		return
	}
	if err := os.WriteFile(r.path, data, 0644); err != nil {
		slog.Error("hedge report write", "err", err)
		return
	}

	slog.Info("hedge_report",
		"net_delta", netDelta,
		"uncovered_qty", uncovered,
		"suggested_instrument", suggestedInst,
		"underlying_price", underlyingPrice,
	)
}
