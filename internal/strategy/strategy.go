package strategy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/gex"
	"optionsbot/internal/hedge"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

// OrderExecutor is satisfied by both the live executor and the backtest SimExecutor.
type OrderExecutor interface {
	Submit(ctx context.Context, order orders.Order) (orders.Fill, error)
	Cancel(ctx context.Context, orderID string) error
	AccountEquity(ctx context.Context, currency string) (float64, error)
}

// orderAdjuster is the live-only interface for order state queries and amendments.
// SimExecutor does not implement it so pending order logic is skipped in backtest.
type orderAdjuster interface {
	GetOrderState(ctx context.Context, orderID string) (orders.OrderStateInfo, error)
	AmendOrder(ctx context.Context, orderID string, qty, price float64) error
}

// marginProvider is the live-only interface for Portfolio Margin-aware sizing.
// When present, replaces the BTC-qty proxy with actual Deribit margin figures.
type marginProvider interface {
	GetAccountSummary(ctx context.Context, currency string) (orders.AccountSummary, error)
	GetMargins(ctx context.Context, instrument string, amount, price float64) (orders.MarginInfo, error)
}

// candleLoader is the live-only interface for fetching historical daily candles.
// SimExecutor does not implement it — gamma history seeding is skipped in backtest.
type candleLoader interface {
	GetDailyCloses(ctx context.Context, instrument string, days int) ([]orders.DailyClose, error)
}

// pendingLeg tracks one leg of a not-yet-fully-filled strangle order.
type pendingLeg struct {
	orderID    string
	instrument string
	optionType string
	qty        float64
	limitPrice float64
	filled     bool
	fillPrice  float64
}

// slotKey uniquely identifies a (DTE, delta) strangle slot.
// DeltaX100 avoids float comparison issues (0.16 → 16, 0.18 → 18).
type slotKey struct {
	DTE       int
	DeltaX100 int
}

func makeSlotKey(dte int, delta float64) slotKey {
	return slotKey{DTE: dte, DeltaX100: int(math.Round(delta * 100))}
}

// pendingStrangle tracks a strangle where one or both legs are still open orders.
// If repairStrangleID is set, on fill this updates an existing strangle's missing
// leg rather than creating a new strangle (avoids doubling up on the active leg).
type pendingStrangle struct {
	id               string
	targetDTE        int
	entryDelta       float64 // the delta target for this slot
	expiry           time.Time
	underlying       string
	call             *pendingLeg
	put              *pendingLeg
	submittedAt      time.Time
	adjustments      int
	repairStrangleID string // non-empty = repair mode
}

// Strategy is the top-level coordinator that runs the short strangle loop.
type Strategy struct {
	cfg          *config.Config
	md           *marketdata.Manager
	exec         OrderExecutor
	state        *orders.StateManager
	orderLog     *orders.Logger
	hedgeRpt     *hedge.Reporter
	gamma        *GammaMonitor
	marginGuard  *MarginGuard
	killSwitchCh chan struct{}
	mu           sync.Mutex
	lastAuthErr  time.Time

	pendingMu        sync.Mutex
	pendingStrangles map[string]*pendingStrangle
}

// SetGEXManager wires the GEX manager into the strategy so that the gamma
// monitor uses market-wide GEX regime instead of net portfolio gamma.
// Call this after New() and before Run().
func (s *Strategy) SetGEXManager(mgr *gex.Manager) {
	s.gamma.SetGEXManager(mgr)
}

// LoadGammaPriceHistory pre-seeds the gamma monitor with historical daily closes
// fetched from Deribit, so trend and swing decisions work immediately on startup.
// Call this after SetGEXManager and before Run. No-op in backtest.
func (s *Strategy) LoadGammaPriceHistory(ctx context.Context) {
	loader, ok := s.exec.(candleLoader)
	if !ok {
		return
	}
	days := s.cfg.GammaTrendLookbackDays + s.cfg.SwingPivotN + 2
	instrument := s.cfg.Underlying + "-PERPETUAL"
	rawCloses, err := loader.GetDailyCloses(ctx, instrument, days)
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

// gexContext extracts a GEXContext from the current GEX snapshot for embedding
// into every order log entry.
func (s *Strategy) gexContext() orders.GEXContext {
	snap := s.gamma.CurrentGEXSnapshot()
	if snap == nil {
		return orders.GEXContext{Regime: "UNKNOWN"}
	}
	return orders.GEXContext{
		Regime:      snap.Regime,
		RegimeScore: snap.RegimeScore,
		GammaFlip:   snap.GammaFlip,
		FlipFound:   snap.GammaFlipFound,
	}
}

func New(
	cfg *config.Config,
	md *marketdata.Manager,
	exec OrderExecutor,
	state *orders.StateManager,
	orderLog *orders.Logger,
	hedgeRpt *hedge.Reporter,
) *Strategy {
	return &Strategy{
		cfg:              cfg,
		md:               md,
		exec:             exec,
		state:            state,
		orderLog:         orderLog,
		hedgeRpt:         hedgeRpt,
		gamma:            NewGammaMonitor(cfg.GammaTrendLookbackDays, cfg.SwingPivotN),
		marginGuard:      NewMarginGuard(cfg.MaxMarginPct, cfg.Leverage),
		killSwitchCh:     make(chan struct{}),
		pendingStrangles: make(map[string]*pendingStrangle),
	}
}

// Run starts the main strategy event loop.
func (s *Strategy) Run(ctx context.Context) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGUSR1)
	go func() {
		select {
		case <-sigCh:
			slog.Warn("kill switch triggered via SIGUSR1")
			close(s.killSwitchCh)
		case <-ctx.Done():
		}
	}()

	go s.heartbeat(ctx)

	s.logStartupState(ctx)
	s.reconcilePositions(ctx)
	s.waitForTickerData(ctx, 30*time.Second)

	if err := s.openStrangles(ctx); err != nil {
		slog.Error("initial strangle open failed", "err", err)
	}

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

func (s *Strategy) heartbeat(ctx context.Context) {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			equity, initialMarginUsed, err := s.fetchMarginState(ctx)
			if err != nil {
				slog.Warn("heartbeat: margin state fetch failed", "err", err)
				continue
			}
			ivPct := s.md.IVPercentile()
			allowed := s.marginGuard.AllowedMargin(equity, ivPct)
			unit := strings.ToLower(s.cfg.Underlying)
			slog.Info("heartbeat",
				"open_positions", len(s.state.AllPositions()),
				"open_strangles", len(s.state.AllStrangles()),
				"equity_"+unit, fmt.Sprintf("%.6f", equity),
				"iv_percentile", fmt.Sprintf("%.1f", ivPct),
				"margin_used_"+unit, fmt.Sprintf("%.6f", initialMarginUsed),
				"margin_allowed_"+unit, fmt.Sprintf("%.6f", allowed),
				"margin_used_pct", fmt.Sprintf("%.1f%%", initialMarginUsed/equity*100),
				"margin_cap_pct", fmt.Sprintf("%.0f%%", s.cfg.MaxMarginPct*100),
			)
			for _, pos := range s.state.AllPositions() {
				dte := int(time.Until(pos.Expiry).Hours() / 24)
				pnl := pos.PremiumReceived - pos.CurrentMid*pos.Qty
				slog.Debug("position status",
					"instrument", pos.Instrument,
					"type", pos.OptionType,
					"strike", pos.Strike,
					"expiry", pos.Expiry.Format("2006-01-02"),
					"dte", dte,
					"delta", fmt.Sprintf("%.4f", pos.CurrentGreeks.Delta),
					"iv", fmt.Sprintf("%.4f", pos.CurrentGreeks.IV),
					"entry_price", fmt.Sprintf("%.4f", pos.EntryPrice),
					"current_mid", fmt.Sprintf("%.4f", pos.CurrentMid),
					"pnl", fmt.Sprintf("%.4f", pnl),
				)
			}
		}
	}
}

func (s *Strategy) evaluate(ctx context.Context) {
	underlyingPrice := s.md.UnderlyingPrice()
	if underlyingPrice == 0 {
		slog.Debug("evaluate: no underlying price yet, skipping")
		return
	}

	// Guard: if we recently got a forbidden/auth error, suppress retries for 60s
	// to avoid log spam. The underlying issue is credentials or API key scopes.
	if !s.lastAuthErr.IsZero() && time.Since(s.lastAuthErr) < 60*time.Second {
		return
	}

	// Update all open position mids from current market data
	for _, pos := range s.state.AllPositions() {
		if inst, ok := s.md.GetInstrument(pos.Instrument); ok {
			s.state.UpdatePositionMid(pos.ID, inst.Mid, orders.Greeks{
				Delta: inst.Greeks.Delta,
				Gamma: inst.Greeks.Gamma,
				Theta: inst.Greeks.Theta,
				Vega:  inst.Greeks.Vega,
				Rho:   inst.Greeks.Rho,
				IV:    inst.Greeks.IV,
			})
		}
	}

	s.gamma.PushPrice(underlyingPrice)

	netGamma := s.state.NetGamma()
	gammaDec := s.gamma.Evaluate()
	slog.Debug("gex check",
		"net_portfolio_gamma", fmt.Sprintf("%.8f", netGamma),
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
	// Always poll pending order fills regardless of gamma state — this is
	// order lifecycle management, not a trading decision.
	s.checkPendingOrders(ctx)

	if gammaDec.Action != GammaActionNone {
		s.handleGammaAction(ctx, gammaDec)
	}

	for _, pos := range s.state.AllPositions() {
		decision := EvaluateLeg(pos,
			s.cfg.RolloutDTE,
			s.cfg.DeltaDriftThreshold,
			s.cfg.ROITakeProfit,
			s.cfg.StopLossMultiplier,
		)
		if decision.Action != ActionNone {
			s.handleRollout(ctx, decision)
		}
	}

	// Repair incomplete strangles. repairIncompleteStrangles has internal
	// per-leg gating: it skips adding a put when ClosePuts and a call when
	// CloseCalls, so the safe leg is still added during a regime action.
	s.repairIncompleteStrangles(ctx, gammaDec)

	netDelta := s.state.TotalNetDelta()
	s.hedgeRpt.MaybeReport(netDelta, underlyingPrice, s.suggestedHedgeInst())

	// openStrangle gates individual legs: openCall = (Action != CloseCalls),
	// openPut = (Action != ClosePuts). In bear regime only calls are opened;
	// in bull regime only puts. Stable trend detection (SMA AND fix) prevents
	// the rapid oscillation that previously caused near-zero P&L churn.
	if err := s.maybeOpenStrangles(ctx, gammaDec); err != nil {
		slog.Warn("maybeOpenStrangles error", "err", err)
	}
}

func (s *Strategy) openStrangles(ctx context.Context) error {
	gammaDec := s.gamma.Evaluate()
	instruments := s.md.AllInstruments()

	equity, initialMarginUsed, err := s.fetchMarginState(ctx)
	if err != nil {
		return err
	}

	ivPercentile := s.md.IVPercentile()
	allowed := s.marginGuard.AllowedMargin(equity, ivPercentile)
	budget := allowed - initialMarginUsed

	slots := s.cfg.Slots()

	unit := strings.ToLower(s.cfg.Underlying)
	slog.Info("opening initial strangles",
		"underlying", s.cfg.Underlying,
		"instruments_available", len(instruments),
		"equity_"+unit, fmt.Sprintf("%.6f", equity),
		"iv_percentile", fmt.Sprintf("%.1f", ivPercentile),
		"margin_allowed_"+unit, fmt.Sprintf("%.6f", allowed),
		"margin_used_"+unit, fmt.Sprintf("%.6f", initialMarginUsed),
		"margin_budget_"+unit, fmt.Sprintf("%.6f", budget),
		"total_slots", len(slots),
	)

	if budget <= 0 {
		slog.Info("margin budget exhausted — no new strangles",
			"margin_allowed_"+unit, fmt.Sprintf("%.6f", allowed),
			"margin_used_"+unit, fmt.Sprintf("%.6f", initialMarginUsed),
		)
		return nil
	}

	// Skip slots already occupied by reconciled positions. A partial strangle
	// (one leg) still counts as occupied — repairIncompleteStrangles will open
	// the missing leg on the first eval cycle.
	occupiedSlots := make(map[slotKey]bool)
	for _, st := range s.state.AllStrangles() {
		occupiedSlots[makeSlotKey(st.TargetDTE, st.EntryDelta)] = true
	}

	for _, slot := range slots {
		if occupiedSlots[makeSlotKey(slot.TargetDTE, slot.EntryDelta)] {
			slog.Info("skip initial strangle: slot occupied by reconciled position",
				"target_dte", slot.TargetDTE, "entry_delta", slot.EntryDelta)
			continue
		}

		expiry, ok := SelectExpiry(instruments, slot.TargetDTE, s.cfg.MaxDTEDeviation, s.cfg.RolloutDTE)
		if !ok {
			slog.Info("no expiry within deviation of target DTE, skipping",
				"target_dte", slot.TargetDTE, "entry_delta", slot.EntryDelta)
			continue
		}

		call, err := SelectStrike(instruments, expiry, "call", slot.EntryDelta, s.cfg.DeltaSlippage)
		if err != nil {
			slog.Warn("call strike selection failed",
				"err", err, "target_dte", slot.TargetDTE, "entry_delta", slot.EntryDelta,
				"expiry", expiry.Format("2006-01-02"))
			continue
		}
		put, err := SelectStrike(instruments, expiry, "put", slot.EntryDelta, s.cfg.DeltaSlippage)
		if err != nil {
			slog.Warn("put strike selection failed",
				"err", err, "target_dte", slot.TargetDTE, "entry_delta", slot.EntryDelta,
				"expiry", expiry.Format("2006-01-02"))
			continue
		}

		targetMarginPerSlot := budget / float64(len(slots))
		qty := s.resolveQty(ctx, call, put, targetMarginPerSlot)

		slog.Debug("entry margin check",
			"target_dte", slot.TargetDTE,
			"entry_delta", slot.EntryDelta,
			"expiry", expiry.Format("2006-01-02"),
			"call", call.Name, "call_delta", fmt.Sprintf("%.4f", call.Greeks.Delta), "call_mid", fmt.Sprintf("%.6f", call.Mid),
			"put", put.Name, "put_delta", fmt.Sprintf("%.4f", put.Greeks.Delta), "put_mid", fmt.Sprintf("%.6f", put.Mid),
			"qty_"+unit, fmt.Sprintf("%.6f", qty),
			"target_margin_per_slot_"+unit, fmt.Sprintf("%.6f", targetMarginPerSlot),
		)

		if err := s.openStrangle(ctx, call, put, slot.TargetDTE, slot.EntryDelta, equity, ivPercentile, qty, gammaDec); err != nil {
			slog.Error("open strangle failed",
				"err", err, "target_dte", slot.TargetDTE, "entry_delta", slot.EntryDelta)
		}
	}
	return nil
}

func (s *Strategy) openStrangle(ctx context.Context, call, put *marketdata.Instrument, targetDTE int, entryDelta float64, equity, ivPercentile, qty float64, gammaDec GammaDecision) error {
	// Re-quantize against the exchange's actual min_trade_amount for these instruments.
	// The pre-computed qty uses cfg.MinTradeAmount as the step, but Deribit enforces
	// per-instrument minimums (e.g. 0.1 BTC for BTC options). Using a step smaller than
	// the exchange enforces produces amounts like 0.15 that are rejected with -32602.
	exchStep := math.Max(call.MinTradeAmount, put.MinTradeAmount)
	if exchStep > s.cfg.MinTradeAmount {
		qty = orders.FloorToStep(qty, exchStep)
		if qty < exchStep {
			return fmt.Errorf("qty %.6f BTC after exchange-step quantization is below min_trade_amount %.6f (call=%s put=%s)",
				qty, exchStep, call.Name, put.Name)
		}
	}

	// Only skip a leg when the GEX regime is actively closing that leg type.
	// In positive/neutral GEX (ActionNone), always open both legs.
	openCall := gammaDec.Action != GammaActionCloseCalls
	openPut := gammaDec.Action != GammaActionClosePuts

	if !openCall || !openPut {
		skipped := "put"
		if !openCall {
			skipped = "call"
		}
		slog.Info("single-leg entry due to GEX regime",
			"regime", gammaDec.Regime, "action", actionLabel(gammaDec.Action),
			"skipping", skipped, "target_dte", targetDTE)
	}

	// Pre-compute limit prices for both legs (max(mid, ask) for short sells ensures
	// we never submit below the exchange minimum when bid=0).
	callPrice := math.Max(call.Mid, call.Ask)
	putPrice := math.Max(put.Mid, put.Ask)

	// Minimum premium floor: gate both legs before submitting any order.
	// Prevents opening positions for near-zero premiums that don't justify the risk.
	if s.cfg.MinPremiumBTC > 0 {
		if openCall && callPrice < s.cfg.MinPremiumBTC {
			return fmt.Errorf("call premium %.6f BTC below floor %.6f BTC — skipping (%s)",
				callPrice, s.cfg.MinPremiumBTC, call.Name)
		}
		if openPut && putPrice < s.cfg.MinPremiumBTC {
			return fmt.Errorf("put premium %.6f BTC below floor %.6f BTC — skipping (%s)",
				putPrice, s.cfg.MinPremiumBTC, put.Name)
		}
	}

	psID := s.state.NextID("ps")
	ps := &pendingStrangle{
		id:          psID,
		targetDTE:   targetDTE,
		entryDelta:  entryDelta,
		expiry:      call.Expiry,
		underlying:  call.Underlying,
		submittedAt: time.Now(),
	}

	gexCtx := s.gexContext()
	mkt := s.marketContext()

	if openCall {
		callFill, err := s.exec.Submit(ctx, orders.Order{
			Instrument:    call.Name,
			Direction:     orders.DirectionSell,
			OrderType:     orders.TypeLimit,
			Qty:           qty,
			LimitPrice:    callPrice,
			TickSize:      call.EffectiveTick(callPrice),
			TriggerReason: orders.TriggerEntry,
		})
		if err != nil {
			return fmt.Errorf("sell call: %w", err)
		}
		ps.call = &pendingLeg{
			orderID: callFill.OrderID, instrument: call.Name, optionType: "call",
			qty: qty, limitPrice: callPrice,
			filled: callFill.FillPrice > 0, fillPrice: callFill.FillPrice,
		}
		// Log the order submission immediately — before waiting for fill confirmation.
		s.orderLog.LogSubmit(orders.PendingOrderRecord{
			OrderID: callFill.OrderID, Instrument: call.Name, OptionType: "call",
			Direction: orders.DirectionSell, TriggerReason: orders.TriggerEntry,
			Qty: qty, LimitPrice: callPrice, Strike: call.Strike,
			UnderlyingPrice: call.UnderlyingPrice, Bid: call.Bid, Ask: call.Ask,
			Greeks: orders.Greeks{Delta: call.Greeks.Delta, Gamma: call.Greeks.Gamma,
				Theta: call.Greeks.Theta, Vega: call.Greeks.Vega, Rho: call.Greeks.Rho},
			IV: call.Greeks.IV, IVPercentile: ivPercentile,
		}, mkt, gexCtx)
	}

	if openPut {
		putFill, err := s.exec.Submit(ctx, orders.Order{
			Instrument:    put.Name,
			Direction:     orders.DirectionSell,
			OrderType:     orders.TypeLimit,
			Qty:           qty,
			LimitPrice:    putPrice,
			TickSize:      put.EffectiveTick(putPrice),
			TriggerReason: orders.TriggerEntry,
		})
		if err != nil {
			if ps.call != nil && ps.call.orderID != "" {
				// A resting call without its put is a naked short leg — make a
				// failed cancel loud so it can be cleaned up by hand.
				if cerr := s.exec.Cancel(ctx, ps.call.orderID); cerr != nil {
					slog.Error("entry: put failed and call order could not be cancelled",
						"call_order_id", ps.call.orderID, "call", call.Name, "err", cerr)
				}
			}
			return fmt.Errorf("sell put: %w", err)
		}
		ps.put = &pendingLeg{
			orderID: putFill.OrderID, instrument: put.Name, optionType: "put",
			qty: qty, limitPrice: putPrice,
			filled: putFill.FillPrice > 0, fillPrice: putFill.FillPrice,
		}
		s.orderLog.LogSubmit(orders.PendingOrderRecord{
			OrderID: putFill.OrderID, Instrument: put.Name, OptionType: "put",
			Direction: orders.DirectionSell, TriggerReason: orders.TriggerEntry,
			Qty: qty, LimitPrice: putPrice, Strike: put.Strike,
			UnderlyingPrice: put.UnderlyingPrice, Bid: put.Bid, Ask: put.Ask,
			Greeks: orders.Greeks{Delta: put.Greeks.Delta, Gamma: put.Greeks.Gamma,
				Theta: put.Greeks.Theta, Vega: put.Greeks.Vega, Rho: put.Greeks.Rho},
			IV: put.Greeks.IV, IVPercentile: ivPercentile,
		}, mkt, gexCtx)
	}

	callDone := ps.call == nil || ps.call.filled
	putDone := ps.put == nil || ps.put.filled
	if callDone && putDone {
		s.activateStrangle(ctx, ps, call, put, ivPercentile, equity)
		return nil
	}

	s.pendingMu.Lock()
	s.pendingStrangles[psID] = ps
	s.pendingMu.Unlock()

	callInfo := "skipped"
	if ps.call != nil {
		callInfo = fmt.Sprintf("%s limit=%.6f", ps.call.orderID, ps.call.limitPrice)
	}
	putInfo := "skipped"
	if ps.put != nil {
		putInfo = fmt.Sprintf("%s limit=%.6f", ps.put.orderID, ps.put.limitPrice)
	}
	slog.Info("strangle orders submitted, awaiting fill",
		"pending_id", psID,
		"target_dte", targetDTE,
		"expiry", call.Expiry.Format("2006-01-02"),
		"gex_regime", gammaDec.Regime,
		"call", callInfo,
		"put", putInfo,
		"qty_btc", fmt.Sprintf("%.6f", qty),
		"fill_timeout_sec", s.cfg.OrderFillTimeoutSec,
	)
	return nil
}

// activateStrangle promotes a fully-filled pending strangle into active positions.
// In repair mode (ps.repairStrangleID != "") it updates the existing strangle's
// missing leg instead of creating a new strangle.
func (s *Strategy) activateStrangle(ctx context.Context, ps *pendingStrangle, call, put *marketdata.Instrument, ivPercentile, equity float64) {
	now := time.Now()
	mkt := s.marketContext()

	// Determine which leg we're activating (could be one or both).
	var filledLeg *pendingLeg
	var filledInst *marketdata.Instrument
	if ps.call != nil && ps.call.filled {
		filledLeg, filledInst = ps.call, call
	} else if ps.put != nil && ps.put.filled {
		filledLeg, filledInst = ps.put, put
	}

	if filledInst == nil && filledLeg != nil {
		// Instrument lookup fallback when called from repair path with nil inst.
		if inst, ok := s.md.GetInstrument(filledLeg.instrument); ok {
			filledInst = inst
		}
	}

	buildPos := func(leg *pendingLeg, inst *marketdata.Instrument) *orders.Position {
		if leg == nil || inst == nil {
			return nil
		}
		return &orders.Position{
			ID: s.state.NextID("pos"), Instrument: leg.instrument,
			Underlying: ps.underlying, Strike: inst.Strike, Expiry: ps.expiry,
			OptionType: leg.optionType, Qty: leg.qty,
			EntryPrice: leg.fillPrice, UnderlyingPrice: inst.UnderlyingPrice, EntryTime: now,
			PremiumReceived: leg.fillPrice * leg.qty,
			CurrentMid:      inst.Mid,
			CurrentGreeks: orders.Greeks{Delta: inst.Greeks.Delta, Gamma: inst.Greeks.Gamma,
				Theta: inst.Greeks.Theta, Vega: inst.Greeks.Vega, Rho: inst.Greeks.Rho, IV: inst.Greeks.IV},
		}
	}

	// Repair mode: update the missing leg of an existing strangle.
	if ps.repairStrangleID != "" && filledLeg != nil && filledInst != nil {
		newPos := buildPos(filledLeg, filledInst)
		if newPos != nil {
			s.state.AddPosition(newPos)
			s.state.SetStrangleLeg(ps.repairStrangleID, filledLeg.optionType, newPos)
			s.orderLog.LogOpen(newPos,
				orders.Fill{OrderID: filledLeg.orderID, FillPrice: filledLeg.fillPrice, Qty: filledLeg.qty, Timestamp: now},
				ivPercentile, s.cfg.SpreadAlertThreshold, mkt, s.gexContext())
			slog.Info("strangle repaired: missing leg filled",
				"strangle_id", ps.repairStrangleID, "leg_type", filledLeg.optionType,
				"instrument", filledLeg.instrument, "fill_price", fmt.Sprintf("%.6f", filledLeg.fillPrice),
				"market_trend", mkt.Trend)
		}
		return
	}

	// Normal mode: create both legs and a new strangle.
	callPos := buildPos(ps.call, call)
	putPos := buildPos(ps.put, put)

	if callPos != nil {
		s.state.AddPosition(callPos)
		s.orderLog.LogOpen(callPos,
			orders.Fill{OrderID: ps.call.orderID, FillPrice: ps.call.fillPrice, Qty: ps.call.qty, Timestamp: now},
			ivPercentile, s.cfg.SpreadAlertThreshold, mkt, s.gexContext())
	}
	if putPos != nil {
		s.state.AddPosition(putPos)
		s.orderLog.LogOpen(putPos,
			orders.Fill{OrderID: ps.put.orderID, FillPrice: ps.put.fillPrice, Qty: ps.put.qty, Timestamp: now},
			ivPercentile, s.cfg.SpreadAlertThreshold, mkt, s.gexContext())
	}

	stID := s.state.NextID("st")
	s.state.AddStrangle(&orders.Strangle{
		ID: stID, TargetDTE: ps.targetDTE, EntryDelta: ps.entryDelta,
		CallLeg: callPos, PutLeg: putPos, OpenedAt: now,
	})

	callFill := "skipped"
	if ps.call != nil {
		callFill = fmt.Sprintf("%.6f", ps.call.fillPrice)
	}
	putFill := "skipped"
	if ps.put != nil {
		putFill = fmt.Sprintf("%.6f", ps.put.fillPrice)
	}
	marginUsedAfter := s.state.TotalMarginUsed()
	slog.Info("strangle filled and active",
		"strangle_id", stID, "pending_id", ps.id,
		"underlying", ps.underlying, "target_dte", ps.targetDTE,
		"expiry", ps.expiry.Format("2006-01-02"),
		"market_trend", mkt.Trend,
		"call_fill", callFill, "put_fill", putFill,
		"adjustments", ps.adjustments,
		"iv_percentile", fmt.Sprintf("%.1f", ivPercentile),
		"margin_used_after_btc", fmt.Sprintf("%.6f", marginUsedAfter),
		"port_net_delta", fmt.Sprintf("%.4f", mkt.NetDelta),
		"port_net_gamma", fmt.Sprintf("%.6f", mkt.NetGamma),
	)
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

// checkPendingOrders reviews all pending strangles: checks fills, amends drifted
// prices, and cancels/reopens timed-out orders.
func (s *Strategy) checkPendingOrders(ctx context.Context) {
	adjuster, ok := s.exec.(orderAdjuster)
	if !ok {
		return // SimExecutor — orders fill instantly, nothing to track
	}

	s.pendingMu.Lock()
	snapshot := make([]*pendingStrangle, 0, len(s.pendingStrangles))
	for _, ps := range s.pendingStrangles {
		snapshot = append(snapshot, ps)
	}
	s.pendingMu.Unlock()

	if len(snapshot) > 0 {
		slog.Info("checking pending orders", "count", len(snapshot))
	}

	for _, ps := range snapshot {
		s.handlePendingStrangle(ctx, adjuster, ps)
	}
}

func (s *Strategy) handlePendingStrangle(ctx context.Context, adjuster orderAdjuster, ps *pendingStrangle) {
	timeout := time.Duration(s.cfg.OrderFillTimeoutSec) * time.Second
	age := time.Since(ps.submittedAt)

	// Poll fill status for unfilled legs.
	for _, leg := range []*pendingLeg{ps.call, ps.put} {
		if leg == nil || leg.filled || leg.orderID == "" {
			continue
		}
		slog.Debug("pending: polling order state",
			"pending_id", ps.id, "instrument", leg.instrument,
			"order_id", leg.orderID, "limit_price", fmt.Sprintf("%.6f", leg.limitPrice),
			"age", age.Round(time.Second),
			"timeout_in", (timeout - age).Round(time.Second),
		)
		state, err := adjuster.GetOrderState(ctx, leg.orderID)
		if err != nil {
			slog.Warn("pending: could not get order state", "order_id", leg.orderID, "err", err)
			continue
		}
		switch state.State {
		case "filled":
			leg.filled = true
			leg.fillPrice = state.AvgPrice
			slog.Info("pending: order filled",
				"pending_id", ps.id, "instrument", leg.instrument,
				"order_id", leg.orderID,
				"fill_price", fmt.Sprintf("%.6f", leg.fillPrice),
				"limit_price", fmt.Sprintf("%.6f", leg.limitPrice),
				"slippage", fmt.Sprintf("%.6f", leg.fillPrice-leg.limitPrice),
			)
		case "cancelled", "rejected":
			slog.Warn("pending: order cancelled/rejected by exchange",
				"pending_id", ps.id, "instrument", leg.instrument,
				"order_id", leg.orderID, "state", state.State)
		default:
			slog.Debug("pending: order still open",
				"pending_id", ps.id, "instrument", leg.instrument,
				"order_id", leg.orderID, "state", state.State,
				"limit_price", fmt.Sprintf("%.6f", leg.limitPrice),
			)
		}
	}

	// Both legs done (filled or skipped) → activate.
	callDone := ps.call == nil || ps.call.filled
	putDone := ps.put == nil || ps.put.filled
	if callDone && putDone {
		var call, put *marketdata.Instrument
		if ps.call != nil {
			if inst, ok := s.md.GetInstrument(ps.call.instrument); ok {
				call = inst
			}
		}
		if ps.put != nil {
			if inst, ok := s.md.GetInstrument(ps.put.instrument); ok {
				put = inst
			}
		}
		ivPct := s.md.IVPercentile()
		equity, _ := s.exec.AccountEquity(ctx, s.cfg.Underlying)
		s.activateStrangle(ctx, ps, call, put, ivPct, equity)
		s.removePending(ps.id)
		return
	}

	// Timed out or exhausted adjustments → cancel remaining legs and reopen.
	if age > timeout {
		slog.Info("pending: order timed out — cancelling legs and releasing slot for reopen",
			"pending_id", ps.id,
			"target_dte", ps.targetDTE, "entry_delta", ps.entryDelta,
			"expiry", ps.expiry.Format("2006-01-02"),
			"age", age.Round(time.Second),
			"timeout_sec", s.cfg.OrderFillTimeoutSec,
			"adjustments", ps.adjustments,
		)
		s.cancelPendingLegs(ctx, ps)
		s.removePending(ps.id)
		// Slot is now free. maybeOpenStrangles() will place a fresh order on the
		// next eval cycle at the current best strike/delta for this DTE.
		slog.Info("pending: slot released, fresh order expected on next eval cycle",
			"target_dte", ps.targetDTE, "entry_delta", ps.entryDelta)
		return
	}

	// Check price drift and amend if within adjustment budget.
	if ps.adjustments >= s.cfg.OrderMaxAdjustments {
		slog.Info("pending: max price adjustments reached, waiting for timeout or fill",
			"pending_id", ps.id,
			"target_dte", ps.targetDTE, "entry_delta", ps.entryDelta,
			"adjustments", ps.adjustments, "max_adjustments", s.cfg.OrderMaxAdjustments,
			"remaining_before_timeout", (timeout - age).Round(time.Second))
		return
	}

	amended := false
	for _, leg := range []*pendingLeg{ps.call, ps.put} {
		if leg == nil || leg.filled {
			continue
		}
		inst, ok := s.md.GetInstrument(leg.instrument)
		// For a sell limit, track Ask drift: we submitted at the market ask, and
		// we want to follow it down if sellers reprice. Using Bid would compare
		// against buyer-side prices, producing huge spurious drift when the spread
		// is wide (e.g. testnet bid ≈ 0 while ask is the real market price).
		if !ok || inst.Ask <= 0 {
			continue
		}
		drift := math.Abs(inst.Ask-leg.limitPrice) / leg.limitPrice
		if drift > s.cfg.OrderSlippagePct {
			newPrice := orders.RoundToStep(inst.Ask, inst.EffectiveTick(inst.Ask))
			if newPrice <= 0 {
				continue
			}
			if err := adjuster.AmendOrder(ctx, leg.orderID, leg.qty, newPrice); err != nil {
				slog.Warn("pending: amend failed", "order_id", leg.orderID, "err", err)
				continue
			}
			slog.Info("pending: order amended due to price drift",
				"pending_id", ps.id,
				"instrument", leg.instrument,
				"old_price", fmt.Sprintf("%.6f", leg.limitPrice),
				"new_price", fmt.Sprintf("%.6f", newPrice),
				"drift_pct", fmt.Sprintf("%.2f%%", drift*100),
				"adjustment", ps.adjustments+1,
				"max_adjustments", s.cfg.OrderMaxAdjustments,
			)
			leg.limitPrice = newPrice
			amended = true
		}
	}
	if amended {
		ps.adjustments++
	}
}

func (s *Strategy) cancelPendingLegs(ctx context.Context, ps *pendingStrangle) {
	ivPct := s.md.IVPercentile()
	mkt := s.marketContext()
	gexCtx := s.gexContext()

	for _, leg := range []*pendingLeg{ps.call, ps.put} {
		if leg == nil || leg.filled || leg.orderID == "" {
			continue
		}
		if err := s.exec.Cancel(ctx, leg.orderID); err != nil {
			slog.Warn("pending: cancel failed", "order_id", leg.orderID, "err", err)
		} else {
			slog.Info("pending: order cancelled",
				"pending_id", ps.id, "instrument", leg.instrument,
				"order_id", leg.orderID, "limit_price", fmt.Sprintf("%.6f", leg.limitPrice))
		}
		// Write cancellation to orders.log regardless of cancel API success,
		// since the slot is being released and the position won't be opened.
		rec := orders.PendingOrderRecord{
			OrderID: leg.orderID, Instrument: leg.instrument, OptionType: leg.optionType,
			Direction: orders.DirectionSell, TriggerReason: orders.TriggerTimeout,
			Qty: leg.qty, LimitPrice: leg.limitPrice,
			IVPercentile: ivPct,
		}
		if inst, ok := s.md.GetInstrument(leg.instrument); ok {
			rec.Strike = inst.Strike
			rec.UnderlyingPrice = inst.UnderlyingPrice
			rec.Bid = inst.Bid
			rec.Ask = inst.Ask
			rec.Greeks = orders.Greeks{
				Delta: inst.Greeks.Delta, Gamma: inst.Greeks.Gamma,
				Theta: inst.Greeks.Theta, Vega: inst.Greeks.Vega, Rho: inst.Greeks.Rho,
			}
			rec.IV = inst.Greeks.IV
		}
		s.orderLog.LogCancelled(rec, mkt, gexCtx)
	}
}

func (s *Strategy) removePending(id string) {
	s.pendingMu.Lock()
	delete(s.pendingStrangles, id)
	s.pendingMu.Unlock()
}

func (s *Strategy) pendingSlots() map[slotKey]bool {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	m := make(map[slotKey]bool, len(s.pendingStrangles))
	for _, ps := range s.pendingStrangles {
		m[makeSlotKey(ps.targetDTE, ps.entryDelta)] = true
	}
	return m
}

// occupiedExpiriesForDelta returns the set of expiry dates already filled for
// strangles whose entry delta matches delta (within ±0.005 tolerance).
// Used for the next-DTE fallback when a slot's primary expiry is occupied.
func (s *Strategy) occupiedExpiriesForDelta(delta float64) map[time.Time]bool {
	dKey := int(math.Round(delta * 100))
	occupied := make(map[time.Time]bool)
	for _, st := range s.state.AllStrangles() {
		if int(math.Round(st.EntryDelta*100)) != dKey {
			continue
		}
		if st.CallLeg != nil {
			occupied[st.CallLeg.Expiry] = true
		}
		if st.PutLeg != nil {
			occupied[st.PutLeg.Expiry] = true
		}
	}
	return occupied
}

func (s *Strategy) handleRollout(ctx context.Context, d RolloutDecision) {
	pos, ok := s.state.GetPosition(d.LegID)
	if !ok {
		return
	}

	ivPercentile := s.md.IVPercentile()

	dte := int(time.Until(pos.Expiry).Hours() / 24)
	slog.Info("rollout triggered",
		"instrument", pos.Instrument,
		"action", d.Action,
		"reason", d.Reason,
		"dte", dte,
		"delta", fmt.Sprintf("%.4f", pos.CurrentGreeks.Delta),
		"current_mid", fmt.Sprintf("%.4f", pos.CurrentMid),
		"premium_received", fmt.Sprintf("%.4f", pos.PremiumReceived),
		"unrealised_pnl", fmt.Sprintf("%.4f", pos.PremiumReceived-pos.CurrentMid*pos.Qty),
	)

	switch d.Action {
	case ActionStopLoss:
		fill, err := s.exec.Submit(ctx, orders.Order{
			Instrument:    pos.Instrument,
			Direction:     orders.DirectionBuy,
			OrderType:     orders.TypeMarket,
			Qty:           pos.Qty,
			TriggerReason: orders.TriggerStopLoss200Pct,
		})
		if err != nil {
			slog.Error("stop loss close failed", "err", err, "instrument", pos.Instrument)
			return
		}
		s.orderLog.LogClose(pos, fill, ivPercentile, orders.TriggerStopLoss200Pct, s.marketContext(), s.gexContext())
		s.state.RemovePosition(pos.ID)
		s.state.RemoveStrangleContaining(pos.ID)
		slog.Warn("stop loss triggered",
			"instrument", pos.Instrument,
			"fill_price", fmt.Sprintf("%.4f", fill.FillPrice),
			"loss_pct", pos.LossPct(),
		)

	case ActionRollNextMonth, ActionRollSameLeg:
		fill, err := s.exec.Submit(ctx, orders.Order{
			Instrument:    pos.Instrument,
			Direction:     orders.DirectionBuy,
			OrderType:     orders.TypeLimit,
			Qty:           pos.Qty,
			LimitPrice:    pos.CurrentMid,
			TriggerReason: d.Reason,
		})
		if err != nil {
			slog.Warn("rollout close failed", "err", err, "instrument", pos.Instrument)
			return
		}
		pnl := pos.PremiumReceived - fill.FillPrice*pos.Qty
		slog.Info("rollout closed",
			"instrument", pos.Instrument,
			"reason", d.Reason,
			"fill_price", fmt.Sprintf("%.4f", fill.FillPrice),
			"pnl", fmt.Sprintf("%.4f", pnl),
		)
		s.orderLog.LogClose(pos, fill, ivPercentile, d.Reason, s.marketContext(), s.gexContext())
		s.state.RemovePosition(pos.ID)
		s.state.RemoveStrangleContaining(pos.ID)

		if err := s.reopenLeg(ctx, pos, d, ivPercentile); err != nil {
			slog.Warn("reopen leg failed", "err", err)
		}
	}
}

func (s *Strategy) reopenLeg(ctx context.Context, old *orders.Position, d RolloutDecision, ivPercentile float64) error {
	instruments := s.md.AllInstruments()
	expiries := AvailableExpiries(instruments)

	var expiry time.Time
	var ok bool

	if d.Action == ActionRollNextMonth {
		expiry, ok = NextMonthlyExpiry(time.Now(), expiries)
	} else {
		// Roll same leg: find nearest expiry >= 25 DTE
		for _, e := range expiries {
			dte := int(time.Until(e).Hours() / 24)
			if dte >= 25 {
				expiry = e
				ok = true
				break
			}
		}
	}

	if !ok {
		return fmt.Errorf("no suitable expiry for reopen")
	}

	// Use the delta from the strangle this leg belonged to.
	// Fall back to cfg.EntryDelta only if the strangle can't be found.
	entryDelta := s.cfg.EntryDelta
	for _, st := range s.state.AllStrangles() {
		if (st.CallLeg != nil && st.CallLeg.ID == old.ID) ||
			(st.PutLeg != nil && st.PutLeg.ID == old.ID) {
			entryDelta = st.EntryDelta
			break
		}
	}

	inst, err := SelectStrike(instruments, expiry, old.OptionType, entryDelta, s.cfg.DeltaSlippage)
	if err != nil {
		return err
	}

	// Preserve the original position size. Rollouts are not the place to
	// resize — they maintain continuity with the leg being closed.
	qty := old.Qty

	// Margin safety gate: ensure reopening stays within the PM budget.
	equity, initialMarginUsed, _ := s.fetchMarginState(ctx)
	budget := s.marginGuard.AllowedMargin(equity, ivPercentile) - initialMarginUsed
	if budget <= 0 {
		return fmt.Errorf("insufficient margin to reopen leg (budget %.6f %s)", budget, s.cfg.Underlying)
	}

	limitPrice := math.Max(inst.Mid, inst.Ask)
	if s.cfg.MinPremiumBTC > 0 && limitPrice < s.cfg.MinPremiumBTC {
		return fmt.Errorf("reopen: premium %.6f BTC below floor %.6f BTC — skipping (%s)",
			limitPrice, s.cfg.MinPremiumBTC, inst.Name)
	}

	fill, err := s.exec.Submit(ctx, orders.Order{
		Instrument:    inst.Name,
		Direction:     orders.DirectionSell,
		OrderType:     orders.TypeLimit,
		Qty:           qty,
		LimitPrice:    limitPrice,
		TickSize:      inst.EffectiveTick(limitPrice),
		TriggerReason: orders.TriggerEntry,
	})
	if err != nil {
		return err
	}

	newPos := &orders.Position{
		ID:              s.state.NextID("pos"),
		Instrument:      inst.Name,
		Underlying:      inst.Underlying,
		Strike:          inst.Strike,
		Expiry:          inst.Expiry,
		OptionType:      inst.OptionType,
		Qty:             qty,
		EntryPrice:      fill.FillPrice,
		UnderlyingPrice: inst.UnderlyingPrice,
		EntryTime:       fill.Timestamp,
		PremiumReceived: fill.FillPrice * qty,
		CurrentMid:      inst.Mid,
		CurrentGreeks: orders.Greeks{
			Delta: inst.Greeks.Delta,
			Gamma: inst.Greeks.Gamma,
			Theta: inst.Greeks.Theta,
			Vega:  inst.Greeks.Vega,
			Rho:   inst.Greeks.Rho,
			IV:    inst.Greeks.IV,
		},
	}
	s.state.AddPosition(newPos)
	s.orderLog.LogOpen(newPos, fill, ivPercentile, s.cfg.SpreadAlertThreshold, s.marketContext(), s.gexContext())
	return nil
}

func (s *Strategy) handleGammaAction(ctx context.Context, dec GammaDecision) {
	ivPercentile := s.md.IVPercentile()
	gexCtx := s.gexContext()
	for _, pos := range s.state.AllPositions() {
		shouldClose := false
		switch dec.Action {
		case GammaActionClosePuts:
			shouldClose = pos.OptionType == "put"
		case GammaActionCloseCalls:
			shouldClose = pos.OptionType == "call"
		}
		if !shouldClose {
			continue
		}
		fill, err := s.exec.Submit(ctx, orders.Order{
			Instrument:    pos.Instrument,
			Direction:     orders.DirectionBuy,
			OrderType:     orders.TypeMarket,
			Qty:           pos.Qty,
			TriggerReason: orders.TriggerGammaClose,
		})
		if err != nil {
			slog.Error("gamma close failed", "err", err, "instrument", pos.Instrument)
			continue
		}
		s.orderLog.LogClose(pos, fill, ivPercentile, orders.TriggerGammaClose, s.marketContext(), gexCtx)
		s.state.RemovePosition(pos.ID)
		s.state.RemoveStrangleContaining(pos.ID)
	}
}

func (s *Strategy) maybeOpenStrangles(ctx context.Context, gammaDec GammaDecision) error {
	// Build the set of already-occupied (DTE, delta) slots from open and pending strangles.
	openSlots := make(map[slotKey]bool)
	for _, st := range s.state.AllStrangles() {
		openSlots[makeSlotKey(st.TargetDTE, st.EntryDelta)] = true
	}
	for key := range s.pendingSlots() {
		openSlots[key] = true
	}

	slots := s.cfg.Slots()
	needsOpen := 0
	for _, slot := range slots {
		if !openSlots[makeSlotKey(slot.TargetDTE, slot.EntryDelta)] {
			needsOpen++
		}
	}
	if needsOpen == 0 {
		return nil
	}

	instruments := s.md.AllInstruments()
	equity, initialMarginUsed, err := s.fetchMarginState(ctx)
	if err != nil {
		return err
	}

	ivPercentile := s.md.IVPercentile()
	allowed := s.marginGuard.AllowedMargin(equity, ivPercentile)
	budget := allowed - initialMarginUsed

	unit := strings.ToLower(s.cfg.Underlying)
	if budget <= 0 {
		slog.Debug("skip entry: margin limit",
			"margin_allowed_"+unit, fmt.Sprintf("%.6f", allowed),
			"margin_used_"+unit, fmt.Sprintf("%.6f", initialMarginUsed),
			"slots_needed", needsOpen,
		)
		return nil
	}

	for _, slot := range slots {
		key := makeSlotKey(slot.TargetDTE, slot.EntryDelta)
		if openSlots[key] {
			slog.Debug("skip entry: slot already open",
				"target_dte", slot.TargetDTE, "entry_delta", slot.EntryDelta)
			continue
		}

		// Find the expiry for this slot. If the primary expiry is already occupied
		// by another position at this delta, fall back to the next available expiry.
		occupied := s.occupiedExpiriesForDelta(slot.EntryDelta)
		expiry, ok := SelectExpiry(instruments, slot.TargetDTE, s.cfg.MaxDTEDeviation, s.cfg.RolloutDTE)
		if ok && occupied[expiry] {
			// Primary expiry already has a position at this delta — try the next one.
			expiry, ok = SelectExpiryFallback(instruments, slot.TargetDTE, s.cfg.MaxDTEDeviation, s.cfg.RolloutDTE, occupied)
			if ok {
				slog.Debug("slot: using fallback expiry",
					"target_dte", slot.TargetDTE, "entry_delta", slot.EntryDelta,
					"fallback_expiry", expiry.Format("2006-01-02"))
			}
		}
		if !ok {
			slog.Info("skip slot: no suitable expiry available",
				"target_dte", slot.TargetDTE, "entry_delta", slot.EntryDelta)
			continue
		}

		call, err := SelectStrike(instruments, expiry, "call", slot.EntryDelta, s.cfg.DeltaSlippage)
		if err != nil {
			slog.Debug("skip entry: call strike selection failed",
				"target_dte", slot.TargetDTE, "entry_delta", slot.EntryDelta,
				"expiry", expiry.Format("2006-01-02"), "err", err)
			continue
		}
		put, err := SelectStrike(instruments, expiry, "put", slot.EntryDelta, s.cfg.DeltaSlippage)
		if err != nil {
			slog.Debug("skip entry: put strike selection failed",
				"target_dte", slot.TargetDTE, "entry_delta", slot.EntryDelta,
				"expiry", expiry.Format("2006-01-02"), "err", err)
			continue
		}
		targetMarginPerSlot := budget / float64(needsOpen)
		qty := s.resolveQty(ctx, call, put, targetMarginPerSlot)

		slog.Debug("entry margin check",
			"target_dte", slot.TargetDTE,
			"entry_delta", slot.EntryDelta,
			"expiry", expiry.Format("2006-01-02"),
			"call", call.Name, "call_delta", fmt.Sprintf("%.4f", call.Greeks.Delta), "call_mid", fmt.Sprintf("%.6f", call.Mid),
			"put", put.Name, "put_delta", fmt.Sprintf("%.4f", put.Greeks.Delta), "put_mid", fmt.Sprintf("%.6f", put.Mid),
			"qty_"+unit, fmt.Sprintf("%.6f", qty),
			"target_margin_per_slot_"+unit, fmt.Sprintf("%.6f", targetMarginPerSlot),
		)
		if err := s.openStrangle(ctx, call, put, slot.TargetDTE, slot.EntryDelta, equity, ivPercentile, qty, gammaDec); err != nil {
			slog.Warn("maybeOpenStrangles: open strangle failed",
				"target_dte", slot.TargetDTE, "entry_delta", slot.EntryDelta, "err", err)
		}
	}
	return nil
}

// fetchMarginState returns (totalEquity, initialMarginUsed).
// totalEquity is sum.Equity (balance + unrealized PnL) — the correct base for
// applying max_margin_pct as a fraction of total account value.
// In live mode it calls private/get_account_summary for accurate PM figures.
// In backtest it falls back to AccountEquity + TotalMarginUsed (qty proxy).
func (s *Strategy) fetchMarginState(ctx context.Context) (equity, initialMarginUsed float64, err error) {
	if mp, ok := s.exec.(marginProvider); ok {
		sum, e := mp.GetAccountSummary(ctx, s.cfg.Underlying)
		if e != nil {
			if errors.Is(e, orders.ErrForbidden) {
				s.lastAuthErr = time.Now()
			}
			return 0, 0, fmt.Errorf("account summary: %w", e)
		}
		s.lastAuthErr = time.Time{}
		return sum.Equity, sum.InitialMargin, nil
	}
	// Backtest fallback: SimExecutor doesn't implement marginProvider.
	eq, e := s.exec.AccountEquity(ctx, s.cfg.Underlying)
	if e != nil {
		return 0, 0, e
	}
	return eq, s.state.TotalMarginUsed(), nil
}

// resolveQty returns the position size for a strangle slot using PM-aware leverage
// sizing. It calls private/get_margins for each leg separately at exchMin quantity
// to learn the incremental IM rate, then scales up to as many whole exchMin lots as
// fit within targetMarginPerSlot. Falls back to exchMin when the marginProvider
// is unavailable (backtest) or GetMargins returns an error or zero/negative IM.
func (s *Strategy) resolveQty(ctx context.Context, call, put *marketdata.Instrument, targetMarginPerSlot float64) float64 {
	exchMin := math.Max(call.MinTradeAmount, put.MinTradeAmount)
	if exchMin <= 0 {
		exchMin = s.cfg.MinTradeAmount
	}

	mp, ok := s.exec.(marginProvider)
	if !ok {
		// Backtest: SimExecutor has no PM API — use exchange minimum.
		return exchMin
	}

	// Use max(mid, ask) as the price — same floor as order submission.
	callPrice := math.Max(call.Mid, call.Ask)
	if callPrice <= 0 {
		callPrice = call.Ask
	}
	putPrice := math.Max(put.Mid, put.Ask)
	if putPrice <= 0 {
		putPrice = put.Ask
	}

	callInfo, err := mp.GetMargins(ctx, call.Name, exchMin, callPrice)
	if err != nil {
		slog.Warn("resolveQty: GetMargins failed for call, using exchange minimum",
			"instrument", call.Name, "err", err)
		return exchMin
	}
	putInfo, err := mp.GetMargins(ctx, put.Name, exchMin, putPrice)
	if err != nil {
		slog.Warn("resolveQty: GetMargins failed for put, using exchange minimum",
			"instrument", put.Name, "err", err)
		return exchMin
	}

	qty := ComputeQtyFromIM(exchMin, targetMarginPerSlot, callInfo.InitialMargin, putInfo.InitialMargin)
	unit := strings.ToLower(s.cfg.Underlying)
	imTotal := callInfo.InitialMargin + putInfo.InitialMargin
	sizingMethod := "pm_aware"
	if imTotal <= 0 || qty == exchMin {
		sizingMethod = "exchange_minimum"
	}
	slog.Info("resolveQty",
		"call", call.Name, "put", put.Name,
		"sizing_method", sizingMethod,
		"exch_min_"+unit, fmt.Sprintf("%.4f", exchMin),
		"call_im_per_lot", fmt.Sprintf("%.6f", callInfo.InitialMargin),
		"put_im_per_lot", fmt.Sprintf("%.6f", putInfo.InitialMargin),
		"im_per_lot_total", fmt.Sprintf("%.6f", imTotal),
		"target_margin_per_slot_"+unit, fmt.Sprintf("%.6f", targetMarginPerSlot),
		"qty_"+unit, fmt.Sprintf("%.4f", qty),
	)
	return qty
}

// liveAccountProvider is satisfied by the real Executor but not the SimExecutor,
// so reconciliation and startup logging are safely no-ops in backtest.
type liveAccountProvider interface {
	GetAccountSummary(ctx context.Context, currency string) (orders.AccountSummary, error)
	GetPositions(ctx context.Context, currency string) ([]orders.RawPosition, error)
}

// allOrderCanceller is satisfied by the real Executor. Used on startup to
// cancel every open order before restoring state (bot-only account assumption).
type allOrderCanceller interface {
	CancelAllOrders(ctx context.Context) error
}

// waitForTickerData blocks until at least one subscribed instrument has live
// ticker data (Mid > 0), or until the timeout expires. Prevents the startup
// open attempt from running before any ticker messages have arrived.
func (s *Strategy) waitForTickerData(ctx context.Context, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.md.UnderlyingPrice() > 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
	slog.Warn("ticker warmup timeout — proceeding without confirmed ticker data",
		"timeout", timeout)
}

func (s *Strategy) logStartupState(ctx context.Context) {
	provider, ok := s.exec.(liveAccountProvider)
	if !ok {
		return
	}
	sum, err := provider.GetAccountSummary(ctx, s.cfg.Underlying)
	if err != nil {
		slog.Warn("startup: could not fetch account summary", "err", err)
		return
	}
	ivPct := s.md.IVPercentile()
	allowedMargin := s.marginGuard.AllowedMargin(sum.Equity, ivPct)
	price := s.md.UnderlyingPrice()
	var usedPct float64
	if sum.Equity > 0 {
		usedPct = sum.InitialMargin / sum.Equity * 100
	}

	unit := strings.ToLower(s.cfg.Underlying)
	slog.Info("account state",
		"currency", sum.Currency,
		"equity_"+unit, fmt.Sprintf("%.6f", sum.Equity),
		"equity_usd", fmt.Sprintf("%.2f", sum.Equity*price),
		"available_funds_"+unit, fmt.Sprintf("%.6f", sum.AvailableFunds),
		"available_funds_usd", fmt.Sprintf("%.2f", sum.AvailableFunds*price),
		"margin_used_"+unit, fmt.Sprintf("%.6f", sum.InitialMargin),
		"margin_used_pct", fmt.Sprintf("%.1f%%", usedPct),
		"margin_maintenance_"+unit, fmt.Sprintf("%.6f", sum.MaintenanceMargin),
		"margin_allowed_"+unit, fmt.Sprintf("%.6f", allowedMargin),
		"margin_allowed_usd", fmt.Sprintf("%.2f", allowedMargin*price),
		"margin_cap_pct", fmt.Sprintf("%.0f%%", s.cfg.MaxMarginPct*100),
		"leverage", fmt.Sprintf("%.2f×", s.cfg.Leverage),
		"iv_percentile", fmt.Sprintf("%.1f", ivPct),
		"options_value_"+unit, fmt.Sprintf("%.6f", sum.OptionsValue),
		"options_pl_"+unit, fmt.Sprintf("%.6f", sum.OptionsPL),
		"net_delta", fmt.Sprintf("%.4f", sum.DeltaTotal),
	)
}

// MatchSlotToPosition picks the configured (DTE, delta) slot that best matches a
// reconciled position. call and/or put may be nil (single-leg position). It scores
// slots by DTE distance + delta distance×100, preferring the exact slot the position
// was opened under. Exported so it can be unit-tested without a full Strategy.
func MatchSlotToPosition(call, put *orders.Position, expiry, now time.Time, slots []config.StrangleSlot) config.StrangleSlot {
	actualDTE := int(expiry.Sub(now).Hours() / 24)
	refDelta := 0.0
	if call != nil {
		refDelta = math.Abs(call.CurrentGreeks.Delta)
	} else if put != nil {
		refDelta = math.Abs(put.CurrentGreeks.Delta)
	}
	best := slots[0]
	bestScore := math.MaxFloat64
	for _, sl := range slots {
		dteDist := float64(absInt(sl.TargetDTE - actualDTE))
		deltaDist := math.Abs(sl.EntryDelta-refDelta) * 100
		if score := dteDist + deltaDist; score < bestScore {
			bestScore = score
			best = sl
		}
	}
	return best
}

func (s *Strategy) reconcilePositions(ctx context.Context) {
	provider, ok := s.exec.(liveAccountProvider)
	if !ok {
		return
	}

	// Cancel all open orders first. The bot is the sole manager of this account;
	// any orders left from before the restart are stale and must be cleared before
	// the strategy re-runs.
	if canceller, ok := s.exec.(allOrderCanceller); ok {
		if err := canceller.CancelAllOrders(ctx); err != nil {
			slog.Warn("reconcile: cancel_all failed (non-fatal)", "err", err)
		} else {
			slog.Info("reconcile: cancelled all open orders on startup")
		}
	}

	// The exchange is the source of truth: read every open short position.
	raw, err := provider.GetPositions(ctx, s.cfg.Underlying)
	if err != nil {
		slog.Warn("reconcile: could not fetch positions from exchange", "err", err)
		return
	}

	var shorts []orders.RawPosition
	for _, p := range raw {
		if p.Size != 0 && p.Direction == "sell" {
			shorts = append(shorts, p)
		}
	}
	if len(shorts) == 0 {
		slog.Info("reconcile: no open short positions on exchange")
		return
	}

	now := time.Now()
	byExpiry := map[time.Time][]*orders.Position{}

	for _, rp := range shorts {
		var strike float64
		var expiry time.Time
		var optType, underlying string

		if inst, ok := s.md.GetInstrument(rp.InstrumentName); ok {
			strike = inst.Strike
			expiry = inst.Expiry
			optType = inst.OptionType
			underlying = inst.Underlying
		} else {
			var parseErr error
			underlying, expiry, strike, optType, parseErr = parseInstrumentName(rp.InstrumentName)
			if parseErr != nil {
				slog.Warn("reconcile: cannot parse instrument", "name", rp.InstrumentName, "err", parseErr)
				continue
			}
		}

		qty := math.Abs(rp.Size)
		pos := &orders.Position{
			ID:              s.state.NextID("pos"),
			Instrument:      rp.InstrumentName,
			Underlying:      underlying,
			Strike:          strike,
			Expiry:          expiry,
			OptionType:      optType,
			Qty:             qty,
			EntryPrice:      rp.AveragePrice,
			UnderlyingPrice: rp.IndexPrice,
			EntryTime:       now,
			PremiumReceived: rp.AveragePrice * qty,
			CurrentMid:      rp.MarkPrice,
			CurrentGreeks: orders.Greeks{
				Delta: rp.Delta,
				Gamma: rp.Gamma,
				Theta: rp.Theta,
				Vega:  rp.Vega,
				Rho:   rp.Rho,
			},
		}
		s.state.AddPosition(pos)
		byExpiry[expiry] = append(byExpiry[expiry], pos)

		slog.Info("reconcile: loaded position",
			"instrument", pos.Instrument,
			"type", pos.OptionType,
			"strike", pos.Strike,
			"expiry", pos.Expiry.Format("2006-01-02"),
			"dte", pos.DTE(),
			"qty", pos.Qty,
			"avg_price_btc", fmt.Sprintf("%.6f", pos.EntryPrice),
			"mark_price_btc", fmt.Sprintf("%.6f", pos.CurrentMid),
			"unrealised_pnl_btc", fmt.Sprintf("%.6f", pos.MtMPnL()),
			"delta", fmt.Sprintf("%.4f", pos.CurrentGreeks.Delta),
		)
		s.orderLog.LogReconciled(pos, s.md.IVPercentile(), s.marketContext(), s.gexContext())
	}

	// Reconstruct strangles from matched call+put pairs per expiry.
	// Single-leg positions are registered as partial strangles so that
	// repairIncompleteStrangles can detect and fill the missing leg.
	for expiry, positions := range byExpiry {
		var call, put *orders.Position
		for _, p := range positions {
			switch p.OptionType {
			case "call":
				call = p
			case "put":
				put = p
			}
		}
		bestSlot := MatchSlotToPosition(call, put, expiry, now, s.cfg.Slots())
		stID := s.state.NextID("st")
		s.state.AddStrangle(&orders.Strangle{
			ID: stID, TargetDTE: bestSlot.TargetDTE, EntryDelta: bestSlot.EntryDelta,
			CallLeg: call, PutLeg: put, OpenedAt: now,
		})
		if call != nil && put != nil {
			slog.Info("reconcile: reconstructed strangle",
				"strangle_id", stID,
				"call", call.Instrument,
				"put", put.Instrument,
				"target_dte", bestSlot.TargetDTE,
				"entry_delta", bestSlot.EntryDelta,
				"actual_dte", int(expiry.Sub(now).Hours()/24),
			)
		} else {
			missing := "call"
			present := put
			if call != nil {
				missing = "put"
				present = call
			}
			slog.Warn("reconcile: single-leg position — registering partial strangle for repair",
				"strangle_id", stID,
				"present_leg", present.Instrument,
				"missing_leg", missing,
				"target_dte", bestSlot.TargetDTE,
				"entry_delta", bestSlot.EntryDelta,
				"actual_dte", int(expiry.Sub(now).Hours()/24),
			)
		}
	}

	// Remove any strangles whose legs are not in the live position set.
	// This cleans up ghost records left by gamma/rollout closes before a restart.
	for _, st := range s.state.AllStrangles() {
		callActive := st.CallLeg != nil
		putActive := st.PutLeg != nil
		if callActive {
			if _, ok := s.state.GetPosition(st.CallLeg.ID); !ok {
				callActive = false
			}
		}
		if putActive {
			if _, ok := s.state.GetPosition(st.PutLeg.ID); !ok {
				putActive = false
			}
		}
		if !callActive && !putActive {
			s.state.RemoveStrangle(st.ID)
			slog.Info("reconcile: removed stale strangle", "strangle_id", st.ID, "target_dte", st.TargetDTE)
		}
	}

	slog.Info("reconcile complete",
		"positions", len(shorts),
		"strangles", len(s.state.AllStrangles()),
	)
}

func parseInstrumentName(name string) (underlying string, expiry time.Time, strike float64, optType string, err error) {
	// Format: BTC-27JUN25-70000-C
	parts := strings.Split(name, "-")
	if len(parts) != 4 {
		return "", time.Time{}, 0, "", fmt.Errorf("expected 4 parts in %q", name)
	}
	underlying = parts[0]
	expiry, err = time.Parse("02Jan06", parts[1])
	if err != nil {
		return "", time.Time{}, 0, "", fmt.Errorf("parse expiry %q: %w", parts[1], err)
	}
	strike, err = strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return "", time.Time{}, 0, "", fmt.Errorf("parse strike %q: %w", parts[2], err)
	}
	switch parts[3] {
	case "C":
		optType = "call"
	case "P":
		optType = "put"
	default:
		return "", time.Time{}, 0, "", fmt.Errorf("unknown option type %q", parts[3])
	}
	return
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// repairIncompleteStrangles detects strangles missing one leg (closed by a
// previous gamma action) and reopens the missing leg when the trend allows it.
// Bear trend: keep call, don't reopen put yet.
// Bull trend: keep put, don't reopen call yet.
// Neutral: reopen whichever leg is missing.
func (s *Strategy) repairIncompleteStrangles(ctx context.Context, gammaDec GammaDecision) {
	instruments := s.md.AllInstruments()

	for _, st := range s.state.AllStrangles() {
		// Check which legs are live in the positions map.
		callActive := st.CallLeg != nil
		if callActive {
			if _, ok := s.state.GetPosition(st.CallLeg.ID); !ok {
				callActive = false
			}
		}
		putActive := st.PutLeg != nil
		if putActive {
			if _, ok := s.state.GetPosition(st.PutLeg.ID); !ok {
				putActive = false
			}
		}

		if callActive && putActive {
			continue // strangle is complete, nothing to do
		}
		if !callActive && !putActive {
			continue // both gone — handled by RemoveStrangleContaining
		}

		// Skip if a repair is already pending for this strangle.
		alreadyPending := false
		s.pendingMu.Lock()
		for _, ps := range s.pendingStrangles {
			if ps.repairStrangleID == st.ID {
				alreadyPending = true
				break
			}
		}
		s.pendingMu.Unlock()
		if alreadyPending {
			continue
		}

		// Determine which leg needs repair and whether the trend allows it.
		missingType := "put"
		if !callActive {
			missingType = "call"
		}
		// Only block repair when the GEX regime is actively telling us to shed that
		// specific leg type. In positive/neutral GEX (ActionNone), always repair.
		if missingType == "put" && gammaDec.Action == GammaActionClosePuts {
			slog.Debug("repair: skipping put leg reopen — GEX regime closing puts", "strangle_id", st.ID)
			continue
		}
		if missingType == "call" && gammaDec.Action == GammaActionCloseCalls {
			slog.Debug("repair: skipping call leg reopen — GEX regime closing calls", "strangle_id", st.ID)
			continue
		}

		// Find the replacement instrument using the same expiry and entry delta.
		var expiry time.Time
		var qty float64
		if callActive && st.CallLeg != nil {
			expiry = st.CallLeg.Expiry
			qty = st.CallLeg.Qty
		} else if putActive && st.PutLeg != nil {
			expiry = st.PutLeg.Expiry
			qty = st.PutLeg.Qty
		} else {
			continue
		}

		// Use the strangle's recorded entry delta so repairs stay on the original slot.
		repairDelta := st.EntryDelta
		if repairDelta == 0 {
			repairDelta = s.cfg.EntryDelta
		}
		inst, err := SelectStrike(instruments, expiry, missingType, repairDelta, s.cfg.DeltaSlippage)
		if err != nil {
			slog.Debug("repair: no suitable strike for missing leg",
				"strangle_id", st.ID, "missing", missingType, "err", err)
			continue
		}

		fill, err := s.exec.Submit(ctx, orders.Order{
			Instrument:    inst.Name,
			Direction:     orders.DirectionSell,
			OrderType:     orders.TypeLimit,
			Qty:           qty,
			LimitPrice:    inst.Mid,
			TickSize:      inst.EffectiveTick(inst.Mid),
			TriggerReason: orders.TriggerEntry,
		})
		if err != nil {
			slog.Warn("repair: order submit failed",
				"strangle_id", st.ID, "missing", missingType, "err", err)
			continue
		}

		leg := &pendingLeg{
			orderID: fill.OrderID, instrument: inst.Name, optionType: missingType,
			qty: qty, limitPrice: inst.Mid,
			filled: fill.FillPrice > 0, fillPrice: fill.FillPrice,
		}

		var callLeg, putLeg *pendingLeg
		if missingType == "call" {
			callLeg = leg
		} else {
			putLeg = leg
		}

		psID := s.state.NextID("ps")
		ps := &pendingStrangle{
			id: psID, targetDTE: st.TargetDTE, entryDelta: st.EntryDelta, expiry: expiry,
			underlying: s.cfg.Underlying,
			call:       callLeg, put: putLeg,
			submittedAt:      time.Now(),
			repairStrangleID: st.ID,
		}

		if leg.filled {
			s.activateStrangle(ctx, ps, nil, nil, s.md.IVPercentile(), 0)
		} else {
			s.pendingMu.Lock()
			s.pendingStrangles[psID] = ps
			s.pendingMu.Unlock()
			slog.Info("repair: missing leg order submitted",
				"strangle_id", st.ID, "missing", missingType,
				"instrument", inst.Name, "order_id", fill.OrderID,
				"limit", fmt.Sprintf("%.6f", inst.Mid), "gex_action", gammaDec.Action)
		}
	}
}

func (s *Strategy) killSwitch(ctx context.Context) error {
	slog.Warn("kill switch: flattening all positions at market")
	ivPercentile := s.md.IVPercentile()
	for _, pos := range s.state.AllPositions() {
		fill, err := s.exec.Submit(ctx, orders.Order{
			Instrument:    pos.Instrument,
			Direction:     orders.DirectionBuy,
			OrderType:     orders.TypeMarket,
			Qty:           pos.Qty,
			TriggerReason: orders.TriggerKillSwitch,
		})
		if err != nil {
			slog.Error("kill switch close failed", "err", err, "instrument", pos.Instrument)
			continue
		}
		s.orderLog.LogClose(pos, fill, ivPercentile, orders.TriggerKillSwitch, s.marketContext(), s.gexContext())
		s.state.RemovePosition(pos.ID)
		s.state.RemoveStrangleContaining(pos.ID)
	}
	return fmt.Errorf("kill switch activated")
}

func (s *Strategy) suggestedHedgeInst() string {
	return fmt.Sprintf("%s-PERPETUAL", s.cfg.Underlying)
}

// KillSwitch allows external callers (e.g. env var check at startup) to trigger shutdown.
func (s *Strategy) KillSwitch() {
	select {
	case <-s.killSwitchCh:
	default:
		close(s.killSwitchCh)
	}
}
