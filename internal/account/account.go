// Package account polls the Deribit account Summary — collateral per asset,
// margin model and initial/maintenance margin — and caches it for the
// monitor. Every margin figure is Deribit's own; the only values computed
// here are IM % and MM % of margin balance, as Deribit's UI shows them.
package account

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"optionsbot/internal/gateway"
)

// rpcCaller is the slice of the gateway the poller uses.
type rpcCaller interface {
	Call(ctx context.Context, method string, params any, priority int) (gateway.JSONRPCResponse, error)
}

// Asset is the collateral and margin of one currency, as Deribit reports it.
// Amounts are in that currency.
type Asset struct {
	Currency          string  `json:"currency"`
	Balance           float64 `json:"balance"`
	Equity            float64 `json:"equity"`
	MarginBalance     float64 `json:"margin_balance"`
	AvailableFunds    float64 `json:"available_funds"`
	AvailableWithdraw float64 `json:"available_withdrawal_funds"`
	InitialMargin     float64 `json:"initial_margin"`
	MaintenanceMargin float64 `json:"maintenance_margin"`
	ProjectedIM       float64 `json:"projected_initial_margin"`
	ProjectedMM       float64 `json:"projected_maintenance_margin"`
	SpotReserve       float64 `json:"spot_reserve"`
	// IMPct and MMPct are initial / maintenance margin as % of margin balance.
	IMPct float64 `json:"im_pct"`
	MMPct float64 `json:"mm_pct"`
}

// Totals are Deribit's cross-collateral totals in USD (present only when
// cross collateral is enabled).
type Totals struct {
	EquityUSD            float64 `json:"equity_usd"`
	MarginBalanceUSD     float64 `json:"margin_balance_usd"`
	InitialMarginUSD     float64 `json:"initial_margin_usd"`
	MaintenanceMarginUSD float64 `json:"maintenance_margin_usd"`
	IMPct                float64 `json:"im_pct"`
	MMPct                float64 `json:"mm_pct"`
}

// Position is one open position on the account, of any kind, as Deribit
// reports it (private/get_positions). Prices, P&L and margin are in Currency
// (the coin for inverse instruments). Size is signed: negative = short.
type Position struct {
	Currency          string  `json:"currency"`
	Instrument        string  `json:"instrument"`
	Kind              string  `json:"kind"` // option | future | …
	Direction         string  `json:"direction"`
	Size              float64 `json:"size"`
	AveragePrice      float64 `json:"average_price"`
	MarkPrice         float64 `json:"mark_price"`
	TotalPnL          float64 `json:"total_pnl"`
	Delta             float64 `json:"delta"` // position delta
	InitialMargin     float64 `json:"initial_margin"`
	MaintenanceMargin float64 `json:"maintenance_margin"`
}

// PositionRow is the subset of private/get_positions we read.
type PositionRow struct {
	InstrumentName    string  `json:"instrument_name"`
	Kind              string  `json:"kind"`
	Direction         string  `json:"direction"`
	Size              float64 `json:"size"`
	AveragePrice      float64 `json:"average_price"`
	MarkPrice         float64 `json:"mark_price"`
	TotalProfitLoss   float64 `json:"total_profit_loss"`
	Delta             float64 `json:"delta"`
	InitialMargin     float64 `json:"initial_margin"`
	MaintenanceMargin float64 `json:"maintenance_margin"`
}

// Snapshot is one poll of the account.
type Snapshot struct {
	AsOf               time.Time `json:"as_of"`
	MarginModel        string    `json:"margin_model"`
	PortfolioMargining bool      `json:"portfolio_margining"`
	CrossCollateral    bool      `json:"cross_collateral"`
	Totals             *Totals   `json:"totals,omitempty"`
	Assets             []Asset   `json:"assets"`
	Source             string    `json:"source"` // get_account_summaries | get_account_summary
	// Positions is every open position on the account (all currencies the
	// account holds, all kinds) — including what no bot manages, which
	// still uses margin. Nil with PositionsError set when the read failed.
	Positions      []Position `json:"positions"`
	PositionsError string     `json:"positions_error,omitempty"`
}

// Status is the cached snapshot plus how fresh it is.
type Status struct {
	Snapshot *Snapshot `json:"snapshot"` // nil before the first successful poll
	Error    string    `json:"error,omitempty"`
	ErrorAt  time.Time `json:"error_at,omitempty"`
}

// Summary is the subset of a Deribit account summary we read.
type Summary struct {
	Currency                   string  `json:"currency"`
	Balance                    float64 `json:"balance"`
	Equity                     float64 `json:"equity"`
	MarginBalance              float64 `json:"margin_balance"`
	AvailableFunds             float64 `json:"available_funds"`
	AvailableWithdrawalFunds   float64 `json:"available_withdrawal_funds"`
	InitialMargin              float64 `json:"initial_margin"`
	MaintenanceMargin          float64 `json:"maintenance_margin"`
	ProjectedInitialMargin     float64 `json:"projected_initial_margin"`
	ProjectedMaintenanceMargin float64 `json:"projected_maintenance_margin"`
	SpotReserve                float64 `json:"spot_reserve"`
	MarginModel                string  `json:"margin_model"`
	PortfolioMarginingEnabled  bool    `json:"portfolio_margining_enabled"`
	CrossCollateralEnabled     bool    `json:"cross_collateral_enabled"`
	TotalEquityUSD             float64 `json:"total_equity_usd"`
	TotalMarginBalanceUSD      float64 `json:"total_margin_balance_usd"`
	TotalInitialMarginUSD      float64 `json:"total_initial_margin_usd"`
	TotalMaintenanceMarginUSD  float64 `json:"total_maintenance_margin_usd"`
}

// fallbackCurrencies are queried one by one when the account-wide method is
// unavailable.
var fallbackCurrencies = []string{"BTC", "ETH", "USDC", "USDT"}

// Poller refreshes the account snapshot on an interval. Safe for concurrent use.
type Poller struct {
	gw rpcCaller

	mu        sync.RWMutex
	snap      *Snapshot
	err       string
	errAt     time.Time
	useLegacy bool // the account-wide method failed; use per-currency calls
}

func NewPoller(gw rpcCaller) *Poller { return &Poller{gw: gw} }

// Status returns the latest snapshot and the last error, if any.
func (p *Poller) Status() Status {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return Status{Snapshot: p.snap, Error: p.err, ErrorAt: p.errAt}
}

// Start polls immediately and then every interval until ctx ends.
func (p *Poller) Start(ctx context.Context, interval time.Duration) {
	go func() {
		p.Refresh(ctx)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				p.Refresh(ctx)
			}
		}
	}()
}

// Refresh polls once. On failure the previous snapshot is kept and the error
// recorded, so the monitor can show stale data as stale.
func (p *Poller) Refresh(ctx context.Context) {
	snap, err := p.fetch(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		if p.err == "" {
			slog.Warn("account Summary poll failed; keeping last snapshot", "err", err)
		}
		p.err, p.errAt = err.Error(), time.Now()
		return
	}
	p.snap, p.err = snap, ""
}

// attachPositions reads every open position for each currency the account
// holds (one read-only call per currency, all kinds). A failure leaves the
// summary in place and records why the list is missing.
func (p *Poller) attachPositions(ctx context.Context, snap *Snapshot) {
	positions := []Position{}
	for _, a := range snap.Assets {
		resp, err := p.gw.Call(ctx, "private/get_positions", map[string]any{"currency": a.Currency}, gateway.PriorityLow)
		if err != nil {
			snap.PositionsError = fmt.Sprintf("get_positions %s: %v", a.Currency, err)
			return
		}
		var rows []PositionRow
		if err := json.Unmarshal(resp.Result, &rows); err != nil {
			snap.PositionsError = fmt.Sprintf("decode positions %s: %v", a.Currency, err)
			return
		}
		positions = append(positions, BuildPositions(a.Currency, rows)...)
	}
	snap.Positions = positions
}

// BuildPositions keeps the non-zero positions, sorted by instrument (pure).
func BuildPositions(currency string, rows []PositionRow) []Position {
	var out []Position
	for _, r := range rows {
		if r.Size == 0 {
			continue
		}
		out = append(out, Position{
			Currency: currency, Instrument: r.InstrumentName, Kind: r.Kind, Direction: r.Direction,
			Size: r.Size, AveragePrice: r.AveragePrice, MarkPrice: r.MarkPrice, TotalPnL: r.TotalProfitLoss,
			Delta: r.Delta, InitialMargin: r.InitialMargin, MaintenanceMargin: r.MaintenanceMargin,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Instrument < out[j].Instrument })
	return out
}

func (p *Poller) fetch(ctx context.Context) (*Snapshot, error) {
	p.mu.RLock()
	legacy := p.useLegacy
	p.mu.RUnlock()

	if !legacy {
		snap, err := p.fetchAll(ctx)
		if err == nil {
			p.attachPositions(ctx, snap)
			return snap, nil
		}
		if ctx.Err() != nil {
			return nil, err
		}
		slog.Warn("private/get_account_summaries unavailable; falling back to per-currency summaries", "err", err)
		p.mu.Lock()
		p.useLegacy = true
		p.mu.Unlock()
	}
	snap, err := p.fetchEach(ctx)
	if err == nil {
		p.attachPositions(ctx, snap)
	}
	return snap, err
}

// fetchAll uses private/get_account_summaries: every currency in one call.
func (p *Poller) fetchAll(ctx context.Context) (*Snapshot, error) {
	resp, err := p.gw.Call(ctx, "private/get_account_summaries", map[string]any{"extended": true}, gateway.PriorityLow)
	if err != nil {
		return nil, fmt.Errorf("get_account_summaries: %w", err)
	}
	var result struct {
		Summaries []Summary `json:"summaries"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("decode account summaries: %w", err)
	}
	return Build(result.Summaries, "get_account_summaries", time.Now()), nil
}

// fetchEach queries the fallback currencies one by one; currencies the
// account does not hold are skipped.
func (p *Poller) fetchEach(ctx context.Context) (*Snapshot, error) {
	var rows []Summary
	var lastErr error
	for _, c := range fallbackCurrencies {
		resp, err := p.gw.Call(ctx, "private/get_account_summary", map[string]any{"currency": c, "extended": true}, gateway.PriorityLow)
		if err != nil {
			lastErr = err
			continue
		}
		var s Summary
		if err := json.Unmarshal(resp.Result, &s); err != nil {
			lastErr = err
			continue
		}
		if s.Currency == "" {
			s.Currency = c
		}
		rows = append(rows, s)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("get_account_summary: %w", lastErr)
	}
	return Build(rows, "get_account_summary", time.Now()), nil
}

// Build turns Deribit summaries into a Snapshot (pure; used by tests).
// Currencies with no balance and no margin are left out.
func Build(rows []Summary, source string, now time.Time) *Snapshot {
	snap := &Snapshot{AsOf: now, Source: source, Assets: []Asset{}}
	for _, s := range rows {
		if s.MarginModel != "" {
			snap.MarginModel = s.MarginModel
		}
		snap.PortfolioMargining = snap.PortfolioMargining || s.PortfolioMarginingEnabled
		snap.CrossCollateral = snap.CrossCollateral || s.CrossCollateralEnabled
		if s.CrossCollateralEnabled && s.TotalMarginBalanceUSD != 0 && snap.Totals == nil {
			snap.Totals = &Totals{
				EquityUSD:            s.TotalEquityUSD,
				MarginBalanceUSD:     s.TotalMarginBalanceUSD,
				InitialMarginUSD:     s.TotalInitialMarginUSD,
				MaintenanceMarginUSD: s.TotalMaintenanceMarginUSD,
				IMPct:                Pct(s.TotalInitialMarginUSD, s.TotalMarginBalanceUSD),
				MMPct:                Pct(s.TotalMaintenanceMarginUSD, s.TotalMarginBalanceUSD),
			}
		}
		if s.Balance == 0 && s.Equity == 0 && s.InitialMargin == 0 && s.MaintenanceMargin == 0 {
			continue
		}
		snap.Assets = append(snap.Assets, Asset{
			Currency: s.Currency, Balance: s.Balance, Equity: s.Equity, MarginBalance: s.MarginBalance,
			AvailableFunds: s.AvailableFunds, AvailableWithdraw: s.AvailableWithdrawalFunds,
			InitialMargin: s.InitialMargin, MaintenanceMargin: s.MaintenanceMargin,
			ProjectedIM: s.ProjectedInitialMargin, ProjectedMM: s.ProjectedMaintenanceMargin,
			SpotReserve: s.SpotReserve,
			IMPct:       Pct(s.InitialMargin, s.MarginBalance),
			MMPct:       Pct(s.MaintenanceMargin, s.MarginBalance),
		})
	}
	sort.Slice(snap.Assets, func(i, j int) bool { return snap.Assets[i].Currency < snap.Assets[j].Currency })
	return snap
}

// Pct is part as a percentage of whole. With no positive collateral, any
// requirement counts as fully used (100 %) — never 0 %, which would hide it.
func Pct(part, whole float64) float64 {
	if whole <= 0 {
		if part > 0 {
			return 100
		}
		return 0
	}
	return part / whole * 100
}
