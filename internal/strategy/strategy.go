package strategy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/history"
	"optionsbot/internal/orders"
	"optionsbot/internal/risk"
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
	cfg     *config.Config
	md      MarketData
	exch    Exchange
	state   *orders.StateManager
	journal TradeJournal
	hedge   HedgeReporter
	gamma   *GammaMonitor
	riskCfg risk.Config
	regimes RegimeHistory
	oi      OISource    // may be nil
	history PnLRecorder // may be nil
	pnl     *pnlBook

	lastSkip map[slotKey]string // last skip reason journaled per slot (decision loop only)
	noQuote  map[string]bool    // held instruments already warned about missing quotes (decision loop only)
	// Legs lost to a stop-loss, by strangle and type, and the last reason
	// their repair was held (decision loop only). In memory: after a restart
	// the freeze and regime conditions still apply, the cooldown does not.
	stopped    map[string]time.Time
	repairHeld map[string]string
	// Orders whose submit outcome is unknown, by label; position differences
	// seen per instrument; instruments to match to the exchange this cycle
	// (decision loop only). See orphans.go.
	unconfirmed map[string]unconfirmedOrder
	drift       map[string]int
	forceCheck  map[string]bool
	// Margin policy state, decision loop only: the last status (to journal
	// changes) and the IM limit the book was last resized to (NaN: never).
	lastRisk     *risk.Status
	appliedLimit float64
	pub          published // loop-owned state copied for View()

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
	regimes := d.Regimes
	if regimes == nil {
		regimes, _ = history.OpenRegimes("") // in memory only; cannot fail
	}
	return &Strategy{
		cfg:              cfg,
		md:               d.Market,
		exch:             d.Exchange,
		state:            d.State,
		journal:          d.Journal,
		hedge:            d.Hedge,
		gamma:            gamma,
		riskCfg:          cfg.RiskPolicy(d.GEX != nil),
		regimes:          regimes,
		appliedLimit:     math.NaN(),
		oi:               d.OI,
		history:          d.History,
		pnl:              newPnLBook(),
		lastSkip:         make(map[slotKey]string),
		noQuote:          make(map[string]bool),
		stopped:          make(map[string]time.Time),
		repairHeld:       make(map[string]string),
		unconfirmed:      make(map[string]unconfirmedOrder),
		drift:            make(map[string]int),
		forceCheck:       make(map[string]bool),
		killSwitchCh:     make(chan struct{}),
		pendingStrangles: make(map[string]*pendingStrangle),
	}
}

// Run restores state from the exchange and then evaluates the book every
// eval_interval_ms until ctx is cancelled or the kill switch fires.
func (s *Strategy) Run(ctx context.Context) error {
	s.markStarted()
	go s.heartbeat(ctx)

	s.loadGammaPriceHistory(ctx)
	s.logStartupState(ctx)
	s.reconcilePositions(ctx)
	s.trackPositions(ctx)
	s.waitForTickerData(ctx, 30*time.Second)

	// Startup applies the margin policy like any cycle: the first confirmed
	// limit resizes the book (rebalance), then vacant slots are entered.
	gammaDec := s.gamma.Evaluate()
	m := s.marginNow(ctx, gammaDec)
	s.applyMarginPolicy(ctx, m)
	if err := s.maybeOpenStrangles(ctx, gammaDec, m); err != nil {
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

	s.trackPositions(ctx)
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
	// Then make sure the book is the exchange's: cancel orders whose submit
	// outcome is unknown, and adopt any position the book does not match.
	s.resolveUnconfirmed(ctx)
	s.checkPositions(ctx)

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

	// Margin policy after exits (they free margin) and before anything that
	// adds risk: MM breach reduces at once, a confirmed limit change resizes.
	m := s.marginNow(ctx, gammaDec)
	s.applyMarginPolicy(ctx, m)

	// Uneven strangles (partial fills, a double fill) carry an unintended
	// directional bet: trim the larger leg. Risk-reducing, so not frozen.
	s.balanceStrangles(ctx)

	// Repair skips a leg only when GEX is actively shedding that leg type,
	// using the same GammaDecision as entry, so the two never disagree.
	// It restores a structure already held, so a freeze does not stop it;
	// a maintenance-margin breach does.
	if !m.mmBreached() {
		s.repairIncompleteStrangles(ctx, gammaDec, m)
	}

	s.hedge.MaybeReport(s.state.TotalNetDelta(), underlyingPrice, s.suggestedHedgeInst())

	if err := s.maybeOpenStrangles(ctx, gammaDec, m); err != nil {
		slog.Warn("maybeOpenStrangles error", "err", err)
	}
}

// trackPositions makes sure every held instrument has a ticker, whatever
// its expiry. Positions loaded at startup can sit in expiries the strategy
// would not open today.
func (s *Strategy) trackPositions(ctx context.Context) {
	positions := s.state.AllPositions()
	names := make([]string, 0, len(positions))
	for _, pos := range positions {
		names = append(names, pos.Instrument)
	}
	if err := s.md.Track(ctx, names); err != nil {
		slog.Warn("cannot subscribe to held instruments", "err", err)
	}
}

// refreshMarks copies the latest mid and greeks onto every open position.
// An instrument without a live quote is skipped: its empty mid and greeks
// would zero the mark, blinding the stop-loss and firing delta drift. The
// position keeps its last known mark, flagged not live (see EvaluateLeg).
func (s *Strategy) refreshMarks() {
	for _, pos := range s.state.AllPositions() {
		inst, ok := s.md.GetInstrument(pos.Instrument)
		if ok && inst.HasQuote() {
			s.state.UpdatePositionMid(pos.ID, inst.Mid, toOrderGreeks(inst))
			delete(s.noQuote, pos.Instrument)
			continue
		}
		if !s.noQuote[pos.Instrument] {
			s.noQuote[pos.Instrument] = true
			slog.Warn("no live quote for a held position: keeping its last known mark",
				"instrument", pos.Instrument, "mark", pos.CurrentMid, "mark_live", pos.MarkLive)
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
	sum, err := s.fetchAccount(ctx)
	if err != nil {
		slog.Warn("heartbeat: account summary fetch failed", "err", err)
		return
	}
	u := sum.MarginUsage()
	rv := s.riskView()
	unit := strings.ToLower(s.cfg.Underlying)
	slog.Info("heartbeat",
		"open_positions", len(s.state.AllPositions()),
		"open_strangles", len(s.state.AllStrangles()),
		"equity_"+unit, fmt.Sprintf("%.6f", sum.Equity),
		"iv_percentile", fmt.Sprintf("%.1f", s.md.IVPercentile()),
		"margin_unit", u.Unit,
		"im_pct", fmt.Sprintf("%.2f", u.IMPct()),
		"mm_pct", fmt.Sprintf("%.2f", u.MMPct()),
		"limit_im_pct", rv.Status.LimitIMPct,
		"max_mm_pct", rv.Status.MaxMMPct,
		"limit_reason", rv.Status.Reason,
		"frozen", rv.Status.Frozen,
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

// fetchAccount reads Deribit's account summary for the underlying.
func (s *Strategy) fetchAccount(ctx context.Context) (orders.AccountSummary, error) {
	sum, err := s.exch.GetAccountSummary(ctx, s.cfg.Underlying)
	if err != nil {
		if errors.Is(err, orders.ErrForbidden) {
			s.noteAuthError()
		}
		return orders.AccountSummary{}, fmt.Errorf("account summary: %w", err)
	}
	s.clearAuthError()
	s.recordAccount(sum.Equity, sum.InitialMargin)
	return sum, nil
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
