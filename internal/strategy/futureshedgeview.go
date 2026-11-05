package strategy

import (
	"context"
	"fmt"
	"math"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/hedge"
)

// HedgeView is the futures hedge experiment as the monitor shows it
// (GET /api/hedge). Deltas in coin, the perpetual in USD.
type HedgeView struct {
	Available   bool      `json:"available"`
	Unavailable string    `json:"unavailable,omitempty"` // why it cannot be switched on
	Enabled     bool      `json:"enabled"`
	Since       time.Time `json:"since"`
	By          string    `json:"by,omitempty"`
	KillSwitch  bool      `json:"kill_switch"`
	// Halted: the hedge sends no orders for a reason retrying cannot cure,
	// until RecheckAt looks again (#664). NotHolding is why the hedge is not
	// holding the delta now (halted, rejected, unfilled): the shed is back.
	Halted      bool      `json:"halted"`
	HaltReason  string    `json:"halt_reason,omitempty"`
	HaltedAt    time.Time `json:"halted_at"`
	RecheckAt   time.Time `json:"recheck_at"`
	NotHolding  string    `json:"not_holding,omitempty"`
	Underlying  string    `json:"underlying"`
	Instrument  string    `json:"instrument"`
	ContractUSD float64   `json:"contract_usd,omitempty"` // 0 until read from Deribit
	// The perpetual as last read from the exchange (PerpAsOf zero: not read
	// yet; the bot reads it every cycle while the mode is on).
	PerpUSD        float64   `json:"perp_usd"`
	PerpDeltaCoin  float64   `json:"perp_delta_coin"`
	PerpAvg        float64   `json:"perp_avg_price"`
	PerpMark       float64   `json:"perp_mark_price"`
	UnrealisedCoin float64   `json:"unrealised_coin"`
	FundingCoin    float64   `json:"funding_coin"`
	PerpAsOf       time.Time `json:"perp_as_of"`
	OptionsDelta   float64   `json:"options_delta_coin"`
	NetDelta       float64   `json:"net_delta_coin"`
	BandCoin       float64   `json:"band_coin"`
	BandPct        float64   `json:"band_pct"`
	OrdersLastHour int       `json:"orders_last_hour"`
	MaxPerHour     int       `json:"max_orders_per_hour"`
	// ShedNow is the side the GEX shed closes this cycle ("puts", "calls" or
	// ""): with the mode on those legs are kept while the hedge can trade;
	// switched off, they are bought back on the next cycle.
	ShedNow   string        `json:"shed_now"`
	Benchmark hedge.Summary `json:"benchmark"`
}

func shedSideWord(a GammaAction) string {
	switch a {
	case GammaActionClosePuts:
		return "puts"
	case GammaActionCloseCalls:
		return "calls"
	}
	return ""
}

// hedgeView builds the view from loop-owned state. Call on the Run goroutine.
func (s *Strategy) hedgeView(now time.Time) HedgeView {
	spot := s.md.UnderlyingPrice()
	p := s.fh.perp
	mark := perpMark(p, spot)
	optDelta := s.state.TotalCoinDelta()
	perpDelta := hedge.PerpDelta(p.Size, mark)
	v := HedgeView{
		Available: s.futures != nil && s.fh.path != "", Enabled: s.fh.mode.Enabled, Since: s.fh.mode.Since,
		By: s.fh.mode.By.User, Underlying: s.cfg.Underlying, Instrument: s.perpName(), ContractUSD: s.fh.spec.ContractSize,
		PerpUSD: p.Size, PerpDeltaCoin: perpDelta, PerpAvg: p.AveragePrice, PerpMark: mark,
		UnrealisedCoin: p.FloatingPnL, FundingCoin: p.RealizedFunding, PerpAsOf: s.fh.perpAt,
		OptionsDelta: optDelta, NetDelta: optDelta + perpDelta,
		BandCoin: s.fh.band, BandPct: config.FixedFuturesHedgeBandPct,
		OrdersLastHour: len(pruneHour(append([]time.Time(nil), s.fh.sent...), now)), MaxPerHour: config.FixedFuturesHedgeMaxPerHour,
		ShedNow:   shedSideWord(s.fh.shedSide),
		Benchmark: s.hedgePricing().Summarise(s.fh.mode.Ledger, p.RealizedFunding, mark, spot),
		Halted:    s.fh.health.Halted(), HaltReason: s.fh.health.HaltReason, HaltedAt: s.fh.health.HaltedAt, RecheckAt: s.fh.health.RecheckAt,
		NotHolding: s.fh.health.NotHolding(config.FixedFuturesHedgeUnfilledCycles),
	}
	if !v.Enabled { // the real perpetual is only read while the mode is on
		v.NetDelta = optDelta
	}
	if v.UnrealisedCoin == 0 && p.Size != 0 {
		v.UnrealisedCoin = hedge.PerpPnL(p.Size, p.AveragePrice, mark)
	}
	switch {
	case s.futures == nil:
		v.Unavailable = "this bot has no futures connection"
	case s.fh.path == "":
		v.Unavailable = "this bot has no data file to keep the mode in"
	}
	return v
}

// HedgeState is the published hedge view. Safe from any goroutine.
func (s *Strategy) HedgeState() HedgeView {
	s.pub.mu.RLock()
	defer s.pub.mu.RUnlock()
	v := s.pub.hedge
	v.KillSwitch = s.pub.halted
	return v
}

// HedgePreview is what switching the hedge off would do now and leave:
// the perpetual's result, delta and margin now → after, and the options'
// P&L for spot moves without the hedge.
type HedgePreview struct {
	hedge.Preview
	Underlying string  `json:"underlying"`
	Instrument string  `json:"instrument"`
	Spot       float64 `json:"spot"`
	// Margin, % of the margin balance (Deribit's figures): now, and after
	// the perpetual is removed (simulate_portfolio). MarginError says why
	// the after figures are missing.
	MarginUnit  string  `json:"margin_unit"`
	IMPctNow    float64 `json:"im_pct_now"`
	MMPctNow    float64 `json:"mm_pct_now"`
	IMPctAfter  float64 `json:"im_pct_after"`
	MMPctAfter  float64 `json:"mm_pct_after"`
	LimitIMPct  float64 `json:"limit_im_pct"`
	MaxMMPct    float64 `json:"max_mm_pct"`
	MarginError string  `json:"margin_error,omitempty"`
	// ShedNext: the side the GEX shed would buy back on the next cycle once
	// the hedge is off ("" none); Warnings in plain words.
	ShedNext string    `json:"shed_next"`
	Warnings []string  `json:"warnings"`
	AsOf     time.Time `json:"as_of"`
}

// HedgePreview reads the exchange and prices the switch-off. Read-only:
// nothing is sent but simulate_portfolio. Safe from any goroutine (it does
// not touch the loop's state).
func (s *Strategy) HedgePreview(ctx context.Context) (HedgePreview, error) {
	if s.futures == nil {
		return HedgePreview{}, fmt.Errorf("the futures hedge is not available on this bot")
	}
	rows, err := s.futures.GetFuturePositions(ctx, s.cfg.Underlying)
	if err != nil {
		return HedgePreview{}, fmt.Errorf("read the perpetual position: %w", err)
	}
	spot := s.md.UnderlyingPrice()
	var p struct {
		size, avg, mark, floating, funding float64
	}
	for _, r := range rows {
		if r.InstrumentName == s.perpName() {
			p.size, p.avg, p.mark, p.floating, p.funding = signedSize(r), r.AveragePrice, r.MarkPrice, r.FloatingPnL, r.RealizedFunding
		}
	}
	out := HedgePreview{
		Preview: hedge.BuildPreview(hedge.PreviewInput{
			Spot: spot, OptionsDelta: s.state.TotalCoinDelta(), ScenarioDelta: s.state.TotalNetDelta(), OptionsGamma: s.state.NetGamma(),
			PerpUSD: p.size, PerpAvg: p.avg, PerpMark: p.mark, Unrealised: p.floating, Funding: p.funding,
			FeeRate: config.FixedFuturesHedgeFeeRate,
		}),
		Underlying: s.cfg.Underlying, Instrument: s.perpName(), Spot: spot, AsOf: time.Now(), Warnings: []string{},
	}
	rs := s.riskView().Status
	out.LimitIMPct, out.MaxMMPct = rs.LimitIMPct, rs.MaxMMPct
	if sum, err := s.exch.GetAccountSummary(ctx, s.cfg.Underlying); err != nil {
		out.MarginError = "account summary: " + err.Error()
	} else {
		u := sum.MarginUsage()
		out.MarginUnit, out.IMPctNow, out.MMPctNow = u.Unit, u.IMPct(), u.MMPct()
		out.IMPctAfter, out.MMPctAfter = out.IMPctNow, out.MMPctNow
		if math.Abs(p.size) >= 0.5 {
			after, err := s.exch.SimulatePortfolio(ctx, s.cfg.Underlying, map[string]float64{s.perpName(): -p.size})
			if err != nil {
				out.MarginError = "simulate_portfolio: " + err.Error()
			} else {
				ua := after.MarginUsage()
				out.IMPctAfter, out.MMPctAfter = ua.IMPct(), ua.MMPct()
			}
		}
	}
	s.pub.mu.RLock()
	out.ShedNext = s.pub.hedge.ShedNow
	s.pub.mu.RUnlock()
	unit := s.cfg.Underlying
	if out.ShedNext != "" {
		out.Warnings = append(out.Warnings, fmt.Sprintf("The GEX shed is active on %s: once the hedge is off, the next cycle buys those legs back at market.", out.ShedNext))
	}
	if math.Abs(out.DeltaAfter) > 0 {
		worst := out.Scenarios[0]
		for _, sc := range out.Scenarios {
			if sc.PnLCoin < worst.PnLCoin {
				worst = sc
			}
		}
		if worst.PnLCoin < 0 {
			out.Warnings = append(out.Warnings, fmt.Sprintf("Without the hedge the options carry %+.4f %s of delta: a %+.0f %% move would cost about %.4f %s (%.0f USD).",
				out.DeltaAfter, unit, worst.MovePct, -worst.PnLCoin, unit, -worst.PnLUSD))
		}
	}
	if out.MarginError == "" && out.MMPctAfter >= out.MaxMMPct && out.MaxMMPct > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("Maintenance margin after the close would be %.1f %%, at or above the %.0f %% limit: the bot would cut positions at market.", out.MMPctAfter, out.MaxMMPct))
	}
	return out, nil
}
