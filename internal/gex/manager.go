package gex

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"optionsbot/internal/gateway"
	"optionsbot/internal/marketdata"
)

// Manager fetches open-interest data from public/get_book_summary_by_currency,
// merges it with the instrument chain already held by the MarketData manager,
// and recomputes the GEX Snapshot on each Refresh call.
//
// Refresh is called on a background timer by the strategy — not on every tick,
// because the book_summary endpoint counts against the non-matching rate limiter.
type Manager struct {
	gw             *gateway.Gateway
	md             *marketdata.Manager
	underlying     string
	nExpiries      int     // number of nearest expiries to include (default 5)
	bandPct        float64 // hysteresis band around gamma flip; 0 disables
	strikeRangePct float64 // include only strikes within ±this fraction of spot; 0 = all

	mu         sync.RWMutex
	snapshot   *Snapshot
	lastRegime string // last published regime, used for hysteresis
}

// NewManager creates a GEX manager. nExpiries controls how many nearest
// expirations are consolidated (mirrors Python's n_expiries=5 default).
// bandPct is the hysteresis band around the gamma flip (e.g. 0.01 = 1%).
// strikeRangePct limits GEX inputs to strikes within ±rangePct of spot
// (e.g. 0.25 = ±25%); 0 includes all strikes.
func NewManager(gw *gateway.Gateway, md *marketdata.Manager, underlying string, nExpiries int, bandPct, strikeRangePct float64) *Manager {
	if nExpiries <= 0 {
		nExpiries = 5
	}
	return &Manager{
		gw:             gw,
		md:             md,
		underlying:     underlying,
		nExpiries:      nExpiries,
		bandPct:        bandPct,
		strikeRangePct: strikeRangePct,
	}
}

// Snapshot returns the most recent computed GEX snapshot, or nil if Refresh
// has not succeeded yet.
func (m *Manager) Snapshot() *Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot
}

// Refresh fetches the current book summary, recomputes GEX across the top
// nExpiries, and stores the resulting Snapshot.
func (m *Manager) Refresh(ctx context.Context) error {
	summaries, err := m.fetchBookSummary(ctx)
	if err != nil {
		return err
	}

	// Build lookup: instrument_name → (OI, markIV, spot)
	type sumRow struct {
		oi, markIV, spot float64
	}
	lookup := make(map[string]sumRow, len(summaries))
	var latestSpot float64
	for _, s := range summaries {
		lookup[s.InstrumentName] = sumRow{oi: s.OpenInterest, markIV: s.MarkIV / 100, spot: s.UnderlyingPrice}
		if s.UnderlyingPrice > 0 {
			latestSpot = s.UnderlyingPrice
		}
	}

	// Get instrument chain from MarketData and group by expiry
	instruments := m.md.AllInstruments()

	type expiryKey = time.Time
	byExpiry := make(map[expiryKey][]InstrumentGEXInput)
	for _, inst := range instruments {
		row, ok := lookup[inst.Name]
		if !ok || row.oi <= 0 || row.markIV <= 0 {
			continue
		}
		spot := row.spot
		if spot <= 0 {
			spot = latestSpot
		}
		byExpiry[inst.Expiry] = append(byExpiry[inst.Expiry], InstrumentGEXInput{
			Instrument:   inst.Name,
			Strike:       inst.Strike,
			Expiry:       inst.Expiry,
			OptionType:   inst.OptionType,
			Spot:         spot,
			OpenInterest: row.oi,
			MarkIV:       row.markIV,
		})
	}

	// Sort expiries ascending and take the nearest nExpiries
	expiries := make([]time.Time, 0, len(byExpiry))
	for exp := range byExpiry {
		expiries = append(expiries, exp)
	}
	sortAsc(expiries)
	if len(expiries) > m.nExpiries {
		expiries = expiries[:m.nExpiries]
	}

	// Compute per-expiry GEX profiles
	profiles := make([]ExpiryProfile, 0, len(expiries))
	var totalInsts, filteredInsts int
	for _, exp := range expiries {
		insts := byExpiry[exp]
		totalInsts += len(insts)
		if m.strikeRangePct > 0 {
			insts = FilterByStrikeRange(insts, latestSpot, m.strikeRangePct)
		}
		filteredInsts += len(insts)
		strikes := ComputeExpiryGEX(insts)

		totalOI := 0.0
		for _, s := range strikes {
			totalOI += s.CallOI + s.PutOI
		}

		profiles = append(profiles, ExpiryProfile{
			Expiry:     exp,
			Label:      exp.Format("2006-01-02"),
			TotalOI:    totalOI,
			TimeWeight: ExpiryWeight(exp),
			Strikes:    strikes,
		})
	}

	if len(profiles) == 0 {
		slog.Warn("gex: no profiles computed — no OI data available")
		return nil
	}

	consolidated := ConsolidateProfiles(profiles)
	snap := BuildSnapshot(consolidated, latestSpot)

	// Apply hysteresis inside the write lock so lastRegime and snapshot are
	// updated atomically. Hysteresis holds the current regime when spot is
	// within bandPct of the flip, preventing churn on small oscillations.
	m.mu.Lock()
	rawRegime := snap.Regime
	adjusted, overridden := ApplyRegimeHysteresis(snap.Regime, m.lastRegime, snap.Spot, snap.GammaFlip, m.bandPct)
	snap.Regime = adjusted
	m.lastRegime = adjusted
	m.snapshot = &snap
	m.mu.Unlock()

	if overridden {
		slog.Info("gex_regime_hysteresis_held",
			"held_regime", adjusted,
			"raw_regime", rawRegime,
			"spot", snap.Spot,
			"flip", snap.GammaFlip,
			"band_pct", m.bandPct,
			"lower_band", snap.GammaFlip*(1-m.bandPct),
			"upper_band", snap.GammaFlip*(1+m.bandPct),
		)
	}

	slog.Info("gex_snapshot",
		"regime", snap.Regime,
		"regime_score", snap.RegimeScore,
		"gamma_flip_found", snap.GammaFlipFound,
		"gamma_flip", snap.GammaFlip,
		"spot", snap.Spot,
		"expiries_used", len(profiles),
		"instruments_total", totalInsts,
		"instruments_in_range", filteredInsts,
		"strike_range_pct", m.strikeRangePct,
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

type bookSummaryRow struct {
	InstrumentName  string  `json:"instrument_name"`
	OpenInterest    float64 `json:"open_interest"`
	UnderlyingPrice float64 `json:"underlying_price"`
	MarkIV          float64 `json:"mark_iv"`
	MarkPrice       float64 `json:"mark_price"`
}

func (m *Manager) fetchBookSummary(ctx context.Context) ([]bookSummaryRow, error) {
	resp, err := m.gw.Call(ctx, "public/get_book_summary_by_currency", map[string]any{
		"currency": m.underlying,
		"kind":     "option",
	}, gateway.PriorityLow)
	if err != nil {
		return nil, err
	}

	var rows []bookSummaryRow
	if err := json.Unmarshal(resp.Result, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func sortAsc(ts []time.Time) {
	for i := 1; i < len(ts); i++ {
		for j := i; j > 0 && ts[j].Before(ts[j-1]); j-- {
			ts[j], ts[j-1] = ts[j-1], ts[j]
		}
	}
}
