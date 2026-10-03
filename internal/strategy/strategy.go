package strategy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/orders"
)

// authBackoff is how long the strategy pauses after Deribit answers
// "forbidden" (bad credentials or missing API scopes), to avoid log spam.
const authBackoff = 60 * time.Second

// ErrKillSwitch is returned by Run after the kill switch has flattened the book.
var ErrKillSwitch = errors.New("kill switch activated")

// Strategy is the top-level coordinator that runs the short strangle loop.
//
// Goroutines: Run owns the decision loop; heartbeat runs alongside it and only
// reads. Everything that places orders runs on the Run goroutine, so trading
// decisions never race each other.
type Strategy struct {
	cfg         *config.Config
	md          MarketData
	exch        Exchange
	state       *orders.StateManager
	journal     TradeJournal
	hedge       HedgeReporter
	gamma       *GammaMonitor
	marginGuard *MarginGuard
	oi          OISource // may be nil
	pnl         *pnlBook

	lastSkip map[slotKey]string // last skip reason journaled per slot (decision loop only)
	pub      published          // loop-owned state copied for View()

	killOnce     sync.Once
	killSwitchCh chan struct{}

	mu          sync.Mutex // guards lastAuthErr (written by heartbeat and loop)
	lastAuthErr time.Time

	pendingMu        sync.Mutex
	pendingStrangles map[string]*pendingStrangle
}

// New builds a Strategy. Call Run to start it.
func New(cfg *config.Config, d Deps) *Strategy {
	gamma := NewGammaMonitor(cfg.GammaTrendLookbackDays, cfg.SwingPivotN)
	if d.GEX != nil {
		gamma.SetGEXSource(d.GEX)
	}
	return &Strategy{
		cfg:              cfg,
		md:               d.Market,
		exch:             d.Exchange,
		state:            d.State,
		journal:          d.Journal,
		hedge:            d.Hedge,
		gamma:            gamma,
		marginGuard:      NewMarginGuard(cfg.MaxMarginPct, cfg.Leverage),
		oi:               d.OI,
		pnl:              newPnLBook(),
		lastSkip:         make(map[slotKey]string),
		killSwitchCh:     make(chan struct{}),
		pendingStrangles: make(map[string]*pendingStrangle),
	}
}

// Run restores state from the exchange and then evaluates the book every
// eval_interval_ms until ctx is cancelled or the kill switch fires.
func (s *Strategy) Run(ctx context.Context) error {
	go s.heartbeat(ctx)

	s.loadGammaPriceHistory(ctx)
	s.logStartupState(ctx)
	s.reconcilePositions(ctx)
	s.waitForTickerData(ctx, 30*time.Second)
	s.rebalancePositions(ctx)

	if err := s.maybeOpenStrangles(ctx, s.gamma.Evaluate()); err != nil {
		slog.Error("initial strangle open failed", "err", err)
	}
	s.publish()

	evalInterval := time.Duration(s.cfg.EvalIntervalMS) * time.Millisecond
	slog.Info("strategy loop started", "eval_interval", evalInterval)
	ticker := time.NewTicker(evalInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.killSwitchCh:
			return s.killSwitch(ctx)
		case <-ticker.C:
			s.evaluate(ctx)
		}
	}
}

// KillSwitch asks Run to flatten every position at market and stop trading.
// Safe to call more than once and from any goroutine (e.g. a signal handler).
func (s *Strategy) KillSwitch() {
	s.killOnce.Do(func() { close(s.killSwitchCh) })
}

// evaluate is one decision cycle. Order matters: refresh marks, then
// lifecycle of pending orders, then risk exits (GEX, rollout rules), then
// repairs, and only then new entries.
func (s *Strategy) evaluate(ctx context.Context) {
	defer s.publish() // refresh what View() serves, every cycle
	underlyingPrice := s.md.UnderlyingPrice()
	if underlyingPrice == 0 {
		slog.Debug("evaluate: no underlying price yet, skipping")
		return
	}
	if s.authBackoffActive() {
		return
	}

	s.refreshMarks()
	s.gamma.PushPrice(underlyingPrice)

	gammaDec := s.gamma.Evaluate()
	slog.Debug("gex check",
		"net_portfolio_gamma", fmt.Sprintf("%.8f", s.state.NetGamma()),
		"gex_regime", gammaDec.Regime,
		"gex_score", gammaDec.RegimeScore,
		"gamma_flip", gammaDec.GammaFlip,
		"gamma_flip_found", gammaDec.GammaFlipFound,
		"trend", gammaDec.Trend,
		"action", actionLabel(gammaDec.Action),
		"swing_high", gammaDec.SwingHigh,
		"swing_low", gammaDec.SwingLow,
		"sma9", gammaDec.SMA9,
		"sma21", gammaDec.SMA21,
		"open_positions", len(s.state.AllPositions()),
	)

	// Order lifecycle first: fills here change what the rules below see.
	s.checkPendingOrders(ctx)

	if gammaDec.Action != GammaActionNone {
		s.handleGammaAction(ctx, gammaDec)
	}

	for _, pos := range s.state.AllPositions() {
		decision := EvaluateLeg(pos, time.Now(),
			s.cfg.RolloutDTE,
			s.cfg.DeltaDriftThreshold,
			s.cfg.ROITakeProfit,
			s.cfg.StopLossMultiplier,
		)
		if decision.Action != ActionNone {
			s.handleRollout(ctx, decision)
		}
	}

	// Repair skips a leg only when GEX is actively shedding that leg type,
	// using the same GammaDecision as entry, so the two never disagree.
	s.repairIncompleteStrangles(ctx, gammaDec)

	s.hedge.MaybeReport(s.state.TotalNetDelta(), underlyingPrice, s.suggestedHedgeInst())

	if err := s.maybeOpenStrangles(ctx, gammaDec); err != nil {
		slog.Warn("maybeOpenStrangles error", "err", err)
	}
}

// refreshMarks copies the latest mid and greeks onto every open position.
func (s *Strategy) refreshMarks() {
	for _, pos := range s.state.AllPositions() {
		if inst, ok := s.md.GetInstrument(pos.Instrument); ok {
			s.state.UpdatePositionMid(pos.ID, inst.Mid, toOrderGreeks(inst))
		}
	}
}

// heartbeat logs account state and writes the P&L journal lines every
// report_interval_sec (default 60 s).
func (s *Strategy) heartbeat(ctx context.Context) {
	every := time.Duration(s.cfg.ReportIntervalSec) * time.Second
	if every <= 0 {
		every = 60 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.logHeartbeat(ctx)
			s.logPnL()
		}
	}
}

func (s *Strategy) logHeartbeat(ctx context.Context) {
	equity, initialMarginUsed, err := s.fetchMarginState(ctx)
	if err != nil {
		slog.Warn("heartbeat: margin state fetch failed", "err", err)
		return
	}
	ivPct := s.md.IVPercentile()
	allowed := s.marginGuard.AllowedMargin(equity)
	unit := strings.ToLower(s.cfg.Underlying)
	slog.Info("heartbeat",
		"open_positions", len(s.state.AllPositions()),
		"open_strangles", len(s.state.AllStrangles()),
		"equity_"+unit, fmt.Sprintf("%.6f", equity),
		"iv_percentile", fmt.Sprintf("%.1f", ivPct),
		"margin_used_"+unit, fmt.Sprintf("%.6f", initialMarginUsed),
		"margin_allowed_"+unit, fmt.Sprintf("%.6f", allowed),
		"margin_used_pct", formatPct(initialMarginUsed, equity),
		"margin_cap_pct", fmt.Sprintf("%.0f%%", s.cfg.MaxMarginPct*100),
	)
	for _, pos := range s.state.AllPositions() {
		slog.Debug("position status",
			"instrument", pos.Instrument,
			"type", pos.OptionType,
			"strike", pos.Strike,
			"expiry", pos.Expiry.Format("2006-01-02"),
			"dte", pos.DTE(),
			"delta", fmt.Sprintf("%.4f", pos.CurrentGreeks.Delta),
			"iv", fmt.Sprintf("%.4f", pos.CurrentGreeks.IV),
			"entry_price", fmt.Sprintf("%.4f", pos.EntryPrice),
			"current_mid", fmt.Sprintf("%.4f", pos.CurrentMid),
			"pnl", fmt.Sprintf("%.4f", pos.MtMPnL()),
		)
	}
}

// formatPct renders part/whole as a percentage, or "n/a" when whole is zero
// (e.g. an empty account) instead of printing NaN or +Inf.
func formatPct(part, whole float64) string {
	if whole == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", part/whole*100)
}

// fetchMarginState returns total equity and the Portfolio Margin initial
// margin in use, both in the underlying currency, from private/get_account_summary.
func (s *Strategy) fetchMarginState(ctx context.Context) (equity, initialMarginUsed float64, err error) {
	sum, err := s.exch.GetAccountSummary(ctx, s.cfg.Underlying)
	if err != nil {
		if errors.Is(err, orders.ErrForbidden) {
			s.noteAuthError()
		}
		return 0, 0, fmt.Errorf("account summary: %w", err)
	}
	s.clearAuthError()
	s.recordAccount(sum.Equity, sum.InitialMargin)
	return sum.Equity, sum.InitialMargin, nil
}

func (s *Strategy) noteAuthError() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastAuthErr = time.Now()
}

func (s *Strategy) clearAuthError() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastAuthErr = time.Time{}
}

func (s *Strategy) authBackoffActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.lastAuthErr.IsZero() && time.Since(s.lastAuthErr) < authBackoff
}

// marketContext snapshots the current portfolio Greeks and market trend.
func (s *Strategy) marketContext() orders.MarketContext {
	return orders.MarketContext{
		Trend:    s.gamma.Trend(),
		NetDelta: s.state.TotalNetDelta(),
		NetGamma: s.state.NetGamma(),
		NetVega:  s.state.NetVega(),
		NetTheta: s.state.NetTheta(),
	}
}

func (s *Strategy) suggestedHedgeInst() string {
	return s.cfg.Underlying + "-PERPETUAL"
}

// loadGammaPriceHistory seeds the gamma monitor with recent daily closes so
// trend and swing decisions work from the first cycle instead of after weeks.
func (s *Strategy) loadGammaPriceHistory(ctx context.Context) {
	days := s.cfg.GammaTrendLookbackDays + s.cfg.SwingPivotN + 2
	instrument := s.suggestedHedgeInst()
	rawCloses, err := s.exch.GetDailyCloses(ctx, instrument, days)
	if err != nil {
		slog.Warn("gamma history load failed, starting without history", "instrument", instrument, "err", err)
		return
	}
	closes := make([]pricePoint, len(rawCloses))
	for i, c := range rawCloses {
		closes[i] = pricePoint{timestamp: c.Date, price: c.Close}
	}
	s.gamma.SeedDailyCloses(closes)
	slog.Info("gamma price history seeded", "instrument", instrument, "days", len(closes))
}
