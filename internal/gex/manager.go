package gex

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"optionsbot/internal/gateway"
)

// rpcCaller is the slice of the gateway the manager uses.
type rpcCaller interface {
	Call(ctx context.Context, method string, params any, priority int) (gateway.JSONRPCResponse, error)
}

// Manager polls public/get_book_summary_by_currency and recomputes the GEX
// snapshot (see Build) on each Refresh. In production the caller wires it
// to a mainnet connection: open interest only means something on the real
// market, whatever environment the bot trades in.
//
// Refresh runs on a background timer, not on every tick, because the book
// summary counts against the non-matching rate limit.
type Manager struct {
	gw      rpcCaller
	params  Params
	bandPct float64 // hysteresis band around the flip (MethodNearestFlip only); 0 disables

	mu         sync.RWMutex
	snapshot   *Snapshot
	oi         *OISnapshot
	lastRegime string       // last published regime, used for hysteresis
	summary    []SummaryRow // last book summary, as fetched (never modified)
	summaryAt  time.Time
}

// NewManager creates a GEX manager for params.Underlying.
func NewManager(gw rpcCaller, params Params, bandPct float64) *Manager {
	if params.NExpiries <= 0 {
		params.NExpiries = 5
	}
	if params.Method == "" {
		params.Method = MethodScript
	}
	return &Manager{gw: gw, params: params, bandPct: bandPct}
}

// Snapshot returns the most recent computed GEX snapshot, or nil if Refresh
// has not succeeded yet. Each refresh publishes a new snapshot, so the
// returned value is never modified afterwards; callers must not modify it.
func (m *Manager) Snapshot() *Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot
}

// LatestSummary returns the last book summary (every option of the
// underlying on mainnet) and when it was fetched. Callers must not modify it.
func (m *Manager) LatestSummary() ([]SummaryRow, time.Time) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.summary, m.summaryAt
}

// OISnapshot is the open interest per instrument from the last refresh.
// Like Snapshot it is immutable once published.
type OISnapshot struct {
	AsOf         time.Time
	ByInstrument map[string]float64 // instrument name → open interest (contracts)
}

// OpenInterest returns the open interest from the last successful refresh,
// or nil before the first one. It is at most one refresh interval old.
func (m *Manager) OpenInterest() *OISnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.oi
}

// Refresh fetches the current book summary, recomputes GEX across the
// nearest expiries and publishes the snapshot and the open interest.
func (m *Manager) Refresh(ctx context.Context) error {
	rows, err := m.fetchBookSummary(ctx)
	if err != nil {
		return fmt.Errorf("gex refresh: %w", err)
	}

	// Open interest is useful on its own (journal snapshots), even when no
	// GEX profile can be built from it.
	oi := &OISnapshot{AsOf: time.Now(), ByInstrument: make(map[string]float64, len(rows))}
	for _, r := range rows {
		oi.ByInstrument[r.InstrumentName] = r.OpenInterest
	}
	m.mu.Lock()
	m.oi = oi
	m.summary, m.summaryAt = rows, oi.AsOf
	m.mu.Unlock()

	snap, st, err := Build(rows, time.Now(), m.params)
	if err != nil {
		slog.Warn("gex: no snapshot computed", "err", err)
		return nil
	}

	// Hysteresis belongs to the spot-vs-flip rule: it holds the regime while
	// spot sits within bandPct of the flip. The script's rule has none.
	m.mu.Lock()
	rawRegime := snap.Regime
	overridden := false
	if m.params.Method == MethodNearestFlip {
		snap.Regime, overridden = ApplyRegimeHysteresis(snap.Regime, m.lastRegime, snap.Spot, snap.GammaFlip, m.bandPct)
	}
	m.lastRegime = snap.Regime
	m.snapshot = &snap
	m.mu.Unlock()

	if overridden {
		slog.Info("gex_regime_hysteresis_held",
			"held_regime", snap.Regime, "raw_regime", rawRegime, "spot", snap.Spot, "flip", snap.GammaFlip,
			"band_pct", m.bandPct)
	}
	slog.Info("gex_snapshot",
		"method", m.params.Method,
		"regime", snap.Regime,
		"regime_score", snap.RegimeScore,
		"gamma_flip_found", snap.GammaFlipFound,
		"gamma_flip", snap.GammaFlip,
		"spot", snap.Spot,
		"expiries_used", len(st.Expiries),
		"first_expiry", st.Expiries[0].Format("2006-01-02"),
		"instruments_total", st.InstrumentsTotal,
		"instruments_in_range", st.InstrumentsUsed,
		"strike_range_pct", m.params.StrikeRangePct,
		"agg_call_wall", snap.AggCallWall,
		"agg_put_wall", snap.AggPutWall,
	)
	return nil
}

// StartBackground launches a goroutine that calls Refresh every interval.
func (m *Manager) StartBackground(ctx context.Context, interval time.Duration) {
	go func() {
		// Initial refresh on start
		if err := m.Refresh(ctx); err != nil {
			slog.Warn("gex initial refresh failed", "err", err)
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := m.Refresh(ctx); err != nil {
					slog.Warn("gex refresh failed", "err", err)
				}
			}
		}
	}()
}

// ── book summary fetch ────────────────────────────────────────────────────────

func (m *Manager) fetchBookSummary(ctx context.Context) ([]SummaryRow, error) {
	resp, err := m.gw.Call(ctx, "public/get_book_summary_by_currency", map[string]any{
		"currency": m.params.Underlying,
		"kind":     "option",
	}, gateway.PriorityLow)
	if err != nil {
		return nil, err
	}

	var rows []SummaryRow
	if err := json.Unmarshal(resp.Result, &rows); err != nil {
		return nil, fmt.Errorf("decode book summary: %w", err)
	}
	return rows, nil
}
