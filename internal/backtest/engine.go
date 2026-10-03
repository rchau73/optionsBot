package backtest

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// Engine runs the backtest simulation loop, wiring the HistoricalFeed and
// SimExecutor through the same strategy logic used in live trading.
type Engine struct {
	cfg         *config.Config
	feed        *HistoricalFeed
	exec        *SimExecutor
	state       *orders.StateManager
	gamma       *strategy.GammaMonitor
	marginGuard *strategy.MarginGuard
	dvol        *marketdata.DVOLTracker

	snapshots    []PortfolioSnapshot
	trades       []TradeRecord
	dailyReturns []float64
	equity       float64
	peakEquity   float64

	stopLossTriggers   int
	gammaCloseTriggers int
	rollout19DTE       int
	rolloutDelta       int
	rolloutROI         int

	ivAtEntry    []float64
	ivPctAtEntry []float64
}

func NewEngine(cfg *config.Config, feed *HistoricalFeed, exec *SimExecutor) *Engine {
	return &Engine{
		cfg:         cfg,
		feed:        feed,
		exec:        exec,
		state:       orders.NewStateManager(),
		gamma:       strategy.NewGammaMonitor(cfg.GammaTrendLookbackDays, cfg.SwingPivotN),
		marginGuard: strategy.NewMarginGuard(cfg.MaxMarginPct, cfg.Leverage),
		dvol:        marketdata.NewDVOLTracker(cfg.IVPercentileWindow),
		equity:      exec.equity,
		peakEquity:  exec.equity,
	}
}

// Run executes the full simulation and returns the Summary.
func (e *Engine) Run(ctx context.Context) (Summary, error) {
	// Group ticks by trading day
	type dayGroup struct {
		date  time.Time
		ticks []*marketdata.Tick
	}

	var days []dayGroup
	current := dayGroup{}

	for !e.feed.Done() {
		tick, err := e.feed.NextTick()
		if err != nil {
			break
		}
		if current.date.IsZero() {
			current.date = tick.Timestamp.Truncate(24 * time.Hour)
		}
		dayDate := tick.Timestamp.Truncate(24 * time.Hour)
		if !dayDate.Equal(current.date) {
			days = append(days, current)
			current = dayGroup{date: dayDate}
		}
		current.ticks = append(current.ticks, tick)
	}
	if len(current.ticks) > 0 {
		days = append(days, current)
	}

	prevEquity := e.equity
	for _, day := range days {
		if ctx.Err() != nil {
			return Summary{}, ctx.Err()
		}
		e.processDay(ctx, day.date, day.ticks)

		// Daily return
		dailyRet := 0.0
		if prevEquity > 0 {
			dailyRet = (e.equity - prevEquity) / prevEquity
		}
		e.dailyReturns = append(e.dailyReturns, dailyRet)
		prevEquity = e.equity
	}

	return e.buildSummary(), nil
}

func (e *Engine) processDay(ctx context.Context, date time.Time, ticks []*marketdata.Tick) {
	slog.Debug("processing day", "date", date.Format("2006-01-02"), "ticks", len(ticks),
		"open_positions", len(e.state.AllPositions()), "equity", fmt.Sprintf("%.2f", e.equity))

	instruments := make(map[string]*marketdata.Instrument)
	var underlyingPrice float64
	var ivPercentile float64

	for _, t := range ticks {
		e.exec.UpdateTick(t)
		e.dvol.Push(t.DVOLIndex)
		underlyingPrice = t.UnderlyingPrice
		ivPercentile = t.IVPercentile
		instruments[t.Instrument] = &marketdata.Instrument{
			Name:            t.Instrument,
			Underlying:      t.Underlying,
			Strike:          t.Strike,
			Expiry:          t.Expiry,
			OptionType:      t.OptionType,
			Bid:             t.Bid,
			Ask:             t.Ask,
			Mid:             t.Mid,
			UnderlyingPrice: t.UnderlyingPrice,
			Greeks: marketdata.Greeks{
				Delta: t.Greeks.Delta,
				Gamma: t.Greeks.Gamma,
				Theta: t.Greeks.Theta,
				Vega:  t.Greeks.Vega,
				Rho:   t.Greeks.Rho,
				IV:    t.Greeks.IV,
			},
			DVOLIndex:    t.DVOLIndex,
			IVPercentile: t.IVPercentile,
		}
	}

	// Update open position mids
	for _, pos := range e.state.AllPositions() {
		if inst, ok := instruments[pos.Instrument]; ok {
			e.state.UpdatePositionMid(pos.ID, inst.Mid, orders.Greeks{
				Delta: inst.Greeks.Delta,
				Gamma: inst.Greeks.Gamma,
				Theta: inst.Greeks.Theta,
				Vega:  inst.Greeks.Vega,
				Rho:   inst.Greeks.Rho,
				IV:    inst.Greeks.IV,
			})
		}
	}

	slog.Debug("day snapshot", "date", date.Format("2006-01-02"),
		"instruments", len(instruments), "underlying_price", fmt.Sprintf("%.2f", underlyingPrice),
		"iv_percentile", fmt.Sprintf("%.2f", ivPercentile))

	// Gamma monitor — backtest uses portfolio-gamma fallback (no live GEX manager)
	e.gamma.PushPrice(underlyingPrice)
	gammaDec := e.gamma.Evaluate()
	if gammaDec.Action != strategy.GammaActionNone {
		slog.Debug("gamma action triggered", "date", date.Format("2006-01-02"),
			"regime", gammaDec.Regime, "score", gammaDec.RegimeScore)
		e.handleGamma(ctx, gammaDec.Action, date, ivPercentile)
	}

	// Per-position rollout checks
	for _, pos := range e.state.AllPositions() {
		dec := strategy.EvaluateLeg(pos, date,
			e.cfg.RolloutDTE,
			e.cfg.DeltaDriftThreshold,
			e.cfg.ROITakeProfit,
			e.cfg.StopLossMultiplier,
		)
		if dec.Action == strategy.ActionNone {
			slog.Debug("position hold", "instrument", pos.Instrument, "action", "none",
				"dte", pos.DTEAt(date), "delta", fmt.Sprintf("%.4f", pos.CurrentGreeks.Delta),
				"roi_pct", fmt.Sprintf("%.2f", (pos.PremiumReceived-pos.CurrentMid)/pos.PremiumReceived*100))
			continue
		}
		slog.Debug("position rollout decision", "instrument", pos.Instrument,
			"action", dec.Action, "reason", dec.Reason,
			"dte", pos.DTEAt(date),
			"delta", fmt.Sprintf("%.4f", pos.CurrentGreeks.Delta),
			"current_mid", fmt.Sprintf("%.4f", pos.CurrentMid),
			"premium_received", fmt.Sprintf("%.4f", pos.PremiumReceived))
		e.handleRollout(ctx, dec, pos, date, ivPercentile, instruments)
	}

	// Entry conditions
	instList := make([]*marketdata.Instrument, 0, len(instruments))
	for _, v := range instruments {
		instList = append(instList, v)
	}
	e.maybeOpenStrangles(ctx, instList, date, ivPercentile)

	// Snapshot equity
	if eq, err := e.exec.AccountEquity(ctx, e.cfg.Underlying); err == nil {
		e.equity = eq
	} else {
		slog.Warn("backtest: equity snapshot failed, keeping previous value", "date", date, "err", err)
	}
	if e.equity > e.peakEquity {
		e.peakEquity = e.equity
	}
	dd := 0.0
	ddUSD := 0.0
	if e.peakEquity > 0 {
		ddUSD = e.peakEquity - e.equity
		dd = ddUSD / e.peakEquity * 100
	}
	marginUsed := e.state.TotalMarginUsed()
	marginPct := 0.0
	if e.equity > 0 {
		marginPct = marginUsed / e.equity * 100
	}
	e.snapshots = append(e.snapshots, PortfolioSnapshot{
		Date:          date,
		EquityUSD:     e.equity,
		OpenPositions: len(e.state.AllPositions()),
		MarginUsedPct: marginPct,
		IVPercentile:  ivPercentile,
		DrawdownUSD:   ddUSD,
		DrawdownPct:   dd,
	})
}

func (e *Engine) handleGamma(ctx context.Context, action strategy.GammaAction, date time.Time, ivPct float64) {
	e.gammaCloseTriggers++
	for _, pos := range e.state.AllPositions() {
		shouldClose := (action == strategy.GammaActionClosePuts && pos.OptionType == "put") ||
			(action == strategy.GammaActionCloseCalls && pos.OptionType == "call")
		if !shouldClose {
			continue
		}
		fill, err := e.exec.Submit(ctx, orders.Order{
			Instrument:    pos.Instrument,
			Direction:     orders.DirectionBuy,
			OrderType:     orders.TypeMarket,
			Qty:           pos.Qty,
			TriggerReason: orders.TriggerGammaClose,
		})
		if err != nil {
			slog.Debug("gamma close submit failed", "instrument", pos.Instrument, "err", err)
			continue
		}
		pnl := pos.PremiumReceived - fill.FillPrice*pos.Qty
		slog.Debug("gamma close filled", "instrument", pos.Instrument,
			"fill_price", fmt.Sprintf("%.4f", fill.FillPrice),
			"premium_received", fmt.Sprintf("%.4f", pos.PremiumReceived),
			"pnl", fmt.Sprintf("%.4f", pnl))
		e.recordTrade(pos, fill, date, orders.TriggerGammaClose)
		e.exec.AdjustEquity(pnl)
		e.state.RemovePosition(pos.ID)
		e.state.RemoveStrangleContaining(pos.ID)
	}
}

func (e *Engine) handleRollout(ctx context.Context, dec strategy.RolloutDecision, pos *orders.Position, date time.Time, ivPct float64, instruments map[string]*marketdata.Instrument) {
	orderType := orders.TypeLimit
	if dec.Action == strategy.ActionStopLoss {
		orderType = orders.TypeMarket
		e.stopLossTriggers++
	}
	switch dec.Reason {
	case orders.TriggerRollout19DTE:
		e.rollout19DTE++
	case orders.TriggerRolloutDelta:
		e.rolloutDelta++
	case orders.TriggerRolloutROI:
		e.rolloutROI++
	}

	fill, err := e.exec.Submit(ctx, orders.Order{
		Instrument:    pos.Instrument,
		Direction:     orders.DirectionBuy,
		OrderType:     orderType,
		Qty:           pos.Qty,
		LimitPrice:    pos.CurrentMid,
		TriggerReason: dec.Reason,
	})
	if err != nil {
		slog.Warn("backtest rollout close failed", "instrument", pos.Instrument, "reason", dec.Reason, "err", err)
		return
	}
	pnl := pos.PremiumReceived - fill.FillPrice*pos.Qty
	slog.Debug("rollout close filled", "instrument", pos.Instrument, "reason", dec.Reason,
		"fill_price", fmt.Sprintf("%.4f", fill.FillPrice),
		"premium_received", fmt.Sprintf("%.4f", pos.PremiumReceived),
		"pnl", fmt.Sprintf("%.4f", pnl))
	e.recordTrade(pos, fill, date, dec.Reason)
	e.exec.AdjustEquity(pnl)
	e.state.RemovePosition(pos.ID)

	// Find the parent strangle's delta so reopen uses the same slot delta.
	posEntryDelta := e.cfg.EntryDelta
	for _, st := range e.state.AllStrangles() {
		if (st.CallLeg != nil && st.CallLeg.ID == pos.ID) ||
			(st.PutLeg != nil && st.PutLeg.ID == pos.ID) {
			if st.EntryDelta != 0 {
				posEntryDelta = st.EntryDelta
			}
			break
		}
	}

	// Reopen if needed
	if dec.Action == strategy.ActionStopLoss || dec.Action == strategy.ActionRollNextMonth {
		instList := make([]*marketdata.Instrument, 0, len(instruments))
		for _, v := range instruments {
			instList = append(instList, v)
		}
		expiries := strategy.AvailableExpiries(instList)
		var newExpiry time.Time
		var ok bool
		if dec.Action == strategy.ActionRollNextMonth {
			newExpiry, ok = strategy.NextMonthlyExpiry(date, expiries)
		} else {
			for _, e := range expiries {
				if strategy.DaysToExpiry(e, date) >= 25 {
					newExpiry = e
					ok = true
					break
				}
			}
		}
		if ok {
			e.openLeg(ctx, instList, newExpiry, pos.OptionType, date, ivPct, posEntryDelta)
		}
	} else if dec.Action == strategy.ActionRollSameLeg {
		instList := make([]*marketdata.Instrument, 0, len(instruments))
		for _, v := range instruments {
			instList = append(instList, v)
		}
		expiries := strategy.AvailableExpiries(instList)
		for _, exp := range expiries {
			if strategy.DaysToExpiry(exp, date) >= 25 {
				e.openLeg(ctx, instList, exp, pos.OptionType, date, ivPct, posEntryDelta)
				break
			}
		}
	}
}

func (e *Engine) openLeg(ctx context.Context, instruments []*marketdata.Instrument, expiry time.Time, optType string, date time.Time, ivPct, entryDelta float64) {
	if entryDelta == 0 {
		entryDelta = e.cfg.EntryDelta // legacy fallback
	}
	inst, err := strategy.SelectStrike(instruments, expiry, optType, entryDelta, e.cfg.DeltaSlippage)
	if err != nil {
		return
	}
	fill, err := e.exec.Submit(ctx, orders.Order{
		Instrument:    inst.Name,
		Direction:     orders.DirectionSell,
		OrderType:     orders.TypeLimit,
		Qty:           1.0,
		LimitPrice:    inst.Mid,
		TriggerReason: orders.TriggerEntry,
	})
	if err != nil {
		return
	}
	pos := &orders.Position{
		ID:              e.state.NextID("pos"),
		Instrument:      inst.Name,
		Underlying:      inst.Underlying,
		Strike:          inst.Strike,
		Expiry:          inst.Expiry,
		OptionType:      inst.OptionType,
		Qty:             1.0,
		EntryPrice:      fill.FillPrice,
		UnderlyingPrice: inst.UnderlyingPrice,
		EntryTime:       date,
		PremiumReceived: fill.FillPrice,
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
	e.state.AddPosition(pos)
	e.ivAtEntry = append(e.ivAtEntry, inst.Greeks.IV)
	e.ivPctAtEntry = append(e.ivPctAtEntry, ivPct)
	slog.Debug("leg opened", "instrument", inst.Name, "type", inst.OptionType,
		"strike", fmt.Sprintf("%.0f", inst.Strike), "expiry", inst.Expiry.Format("2006-01-02"),
		"fill_price", fmt.Sprintf("%.4f", fill.FillPrice), "delta", fmt.Sprintf("%.4f", inst.Greeks.Delta))
}

func (e *Engine) maybeOpenStrangles(ctx context.Context, instruments []*marketdata.Instrument, date time.Time, ivPct float64) {
	expiries := strategy.AvailableExpiries(instruments)
	equity, _ := e.exec.AccountEquity(ctx, e.cfg.Underlying)

	slog.Debug("entry check", "date", date.Format("2006-01-02"),
		"available_expiries", len(expiries), "equity", fmt.Sprintf("%.2f", equity),
		"iv_percentile", fmt.Sprintf("%.2f", ivPct), "open_strangles", len(e.state.AllStrangles()))

	slots := e.cfg.Slots()
	openSlots := make(map[struct {
		DTE       int
		DeltaX100 int
	}]bool)
	for _, st := range e.state.AllStrangles() {
		openSlots[struct {
			DTE       int
			DeltaX100 int
		}{st.TargetDTE, int(math.Round(st.EntryDelta * 100))}] = true
	}

	for _, slot := range slots {
		key := struct {
			DTE       int
			DeltaX100 int
		}{slot.TargetDTE, int(math.Round(slot.EntryDelta * 100))}
		if openSlots[key] {
			slog.Debug("skip entry: slot already open",
				"target_dte", slot.TargetDTE, "entry_delta", slot.EntryDelta)
			continue
		}

		expiry, ok := strategy.SelectExpiry(instruments, date, slot.TargetDTE, e.cfg.MaxDTEDeviation, e.cfg.RolloutDTE)
		if !ok {
			slog.Debug("skip entry: no suitable expiry",
				"target_dte", slot.TargetDTE, "entry_delta", slot.EntryDelta,
				"available_expiries", len(expiries))
			continue
		}

		call, err := strategy.SelectStrike(instruments, expiry, "call", slot.EntryDelta, e.cfg.DeltaSlippage)
		if err != nil {
			slog.Debug("skip entry: call strike selection failed",
				"target_dte", slot.TargetDTE, "entry_delta", slot.EntryDelta,
				"expiry", expiry.Format("2006-01-02"), "err", err)
			continue
		}
		put, err := strategy.SelectStrike(instruments, expiry, "put", slot.EntryDelta, e.cfg.DeltaSlippage)
		if err != nil {
			slog.Debug("skip entry: put strike selection failed",
				"target_dte", slot.TargetDTE, "entry_delta", slot.EntryDelta,
				"expiry", expiry.Format("2006-01-02"), "err", err)
			continue
		}

		marginNeeded := call.Mid + put.Mid
		if !e.marginGuard.WithinLimit(e.state.TotalMarginUsed(), marginNeeded, equity) {
			slog.Debug("skip entry: margin limit",
				"target_dte", slot.TargetDTE, "entry_delta", slot.EntryDelta,
				"margin_needed", fmt.Sprintf("%.4f", marginNeeded),
				"margin_used", fmt.Sprintf("%.4f", e.state.TotalMarginUsed()),
				"equity", fmt.Sprintf("%.2f", equity))
			continue
		}

		callFill, err := e.exec.Submit(ctx, orders.Order{
			Instrument: call.Name, Direction: orders.DirectionSell,
			OrderType: orders.TypeLimit, Qty: 1.0, LimitPrice: call.Mid,
			TriggerReason: orders.TriggerEntry,
		})
		if err != nil {
			slog.Debug("skip entry: call submit failed", "instrument", call.Name, "err", err)
			continue
		}
		putFill, err := e.exec.Submit(ctx, orders.Order{
			Instrument: put.Name, Direction: orders.DirectionSell,
			OrderType: orders.TypeLimit, Qty: 1.0, LimitPrice: put.Mid,
			TriggerReason: orders.TriggerEntry,
		})
		if err != nil {
			slog.Debug("skip entry: put submit failed", "instrument", put.Name, "err", err)
			continue
		}

		targetDTE := slot.TargetDTE
		slog.Debug("strangle opened", "date", date.Format("2006-01-02"),
			"target_dte", targetDTE, "entry_delta", slot.EntryDelta,
			"expiry", expiry.Format("2006-01-02"),
			"call", call.Name, "call_strike", fmt.Sprintf("%.0f", call.Strike), "call_mid", fmt.Sprintf("%.4f", callFill.FillPrice),
			"put", put.Name, "put_strike", fmt.Sprintf("%.0f", put.Strike), "put_mid", fmt.Sprintf("%.4f", putFill.FillPrice))

		stID := e.state.NextID("st")
		callPos := &orders.Position{
			ID: e.state.NextID("pos"), Instrument: call.Name,
			Underlying: call.Underlying, Strike: call.Strike,
			Expiry: call.Expiry, OptionType: "call", Qty: 1.0,
			EntryPrice: callFill.FillPrice, UnderlyingPrice: call.UnderlyingPrice, EntryTime: date,
			PremiumReceived: callFill.FillPrice,
			CurrentMid:      call.Mid,
			CurrentGreeks: orders.Greeks{Delta: call.Greeks.Delta, Gamma: call.Greeks.Gamma,
				Theta: call.Greeks.Theta, Vega: call.Greeks.Vega, IV: call.Greeks.IV},
		}
		putPos := &orders.Position{
			ID: e.state.NextID("pos"), Instrument: put.Name,
			Underlying: put.Underlying, Strike: put.Strike,
			Expiry: put.Expiry, OptionType: "put", Qty: 1.0,
			EntryPrice: putFill.FillPrice, UnderlyingPrice: put.UnderlyingPrice, EntryTime: date,
			PremiumReceived: putFill.FillPrice,
			CurrentMid:      put.Mid,
			CurrentGreeks: orders.Greeks{Delta: put.Greeks.Delta, Gamma: put.Greeks.Gamma,
				Theta: put.Greeks.Theta, Vega: put.Greeks.Vega, IV: put.Greeks.IV},
		}
		e.state.AddPosition(callPos)
		e.state.AddPosition(putPos)
		e.state.AddStrangle(&orders.Strangle{
			ID: stID, TargetDTE: targetDTE, EntryDelta: slot.EntryDelta,
			CallLeg: callPos, PutLeg: putPos, OpenedAt: date,
		})
		e.ivAtEntry = append(e.ivAtEntry, call.Greeks.IV, put.Greeks.IV)
		e.ivPctAtEntry = append(e.ivPctAtEntry, ivPct, ivPct)
	}
}

func (e *Engine) recordTrade(pos *orders.Position, fill orders.Fill, exitDate time.Time, reason string) {
	holdDays := int(math.Round(exitDate.Sub(pos.EntryTime).Hours() / 24))
	closeCost := fill.FillPrice * pos.Qty
	pnl := pos.PremiumReceived - closeCost
	roi := 0.0
	if pos.PremiumReceived > 0 {
		roi = pnl / pos.PremiumReceived * 100
	}
	e.trades = append(e.trades, TradeRecord{
		EntryDate:    pos.EntryTime,
		ExitDate:     exitDate,
		ExitReason:   reason,
		Instrument:   pos.Instrument,
		OptionType:   pos.OptionType,
		Strike:       pos.Strike,
		Expiry:       pos.Expiry,
		Qty:          pos.Qty,
		EntryPrice:   pos.EntryPrice,
		ExitPrice:    fill.FillPrice,
		PremiumRecvd: pos.PremiumReceived,
		CloseCost:    closeCost,
		PnLUSD:       pnl,
		ROIPct:       roi,
		HoldDays:     holdDays,
		Commission:   e.cfg.Backtest.CommissionPerContract * pos.Qty,
	})
}

func (e *Engine) buildSummary() Summary {
	pnls := make([]float64, len(e.trades))
	holdDays := make([]float64, len(e.trades))
	rois := make([]float64, len(e.trades))
	roiAnns := make([]float64, len(e.trades))
	thetas := make([]float64, len(e.trades))

	for i, t := range e.trades {
		pnls[i] = t.PnLUSD
		holdDays[i] = float64(t.HoldDays)
		rois[i] = t.ROIPct
		roiAnn := 0.0
		if t.HoldDays > 0 {
			roiAnn = t.ROIPct / float64(t.HoldDays) * 365
		}
		roiAnns[i] = roiAnn
		thetas[i] = 0
	}

	equityCurve := make([]float64, len(e.snapshots))
	for i, s := range e.snapshots {
		equityCurve[i] = s.EquityUSD
	}
	maxDDPct, maxDDUSD := MaxDrawdown(equityCurve)
	sort.Float64s(pnls) // for total

	total := 0.0
	for _, p := range pnls {
		total += p
	}

	return Summary{
		TotalTrades:            len(e.trades),
		WinRatePct:             WinRate(pnls),
		TotalPnLUSD:            total,
		MaxDrawdownUSD:         maxDDUSD,
		MaxDrawdownPct:         maxDDPct * 100,
		SharpeRatio:            Sharpe(e.dailyReturns),
		SortinoRatio:           Sortino(e.dailyReturns),
		CalmarRatio:            Calmar(e.dailyReturns, maxDDPct),
		AvgHoldDays:            Mean(holdDays),
		AvgROIPct:              Mean(rois),
		AvgROIAnnualized:       Mean(roiAnns),
		TotalCommissionUSD:     e.exec.TotalCommission(),
		StopLossTriggers:       e.stopLossTriggers,
		GammaCloseTriggers:     e.gammaCloseTriggers,
		Rollout19DTE:           e.rollout19DTE,
		RolloutDeltaDrift:      e.rolloutDelta,
		RolloutROI:             e.rolloutROI,
		AvgThetaCapturedUSD:    Mean(thetas),
		AvgIVAtEntry:           Mean(e.ivAtEntry),
		AvgIVPercentileAtEntry: Mean(e.ivPctAtEntry),
	}
}

func (e *Engine) Snapshots() []PortfolioSnapshot { return e.snapshots }
func (e *Engine) Trades() []TradeRecord          { return e.trades }

// RunScenarioSweep runs all scenarios in parallel and writes scenario_comparison.csv.
func RunScenarioSweep(cfg *config.Config, csvPath string, from, to time.Time, outputDir string) error {
	scenarios := DefaultScenarios()
	results := make([]ScenarioResult, len(scenarios))
	var wg sync.WaitGroup

	for i, sc := range scenarios {
		wg.Add(1)
		go func(i int, sc Scenario) {
			defer wg.Done()

			scCfg := *cfg
			scCfg.EntryDelta = sc.EntryDelta
			scCfg.TargetDTE = sc.TargetDTE
			scCfg.RolloutDTE = sc.RolloutDTE
			scCfg.StopLossMultiplier = sc.StopLossMulti
			scCfg.ROITakeProfit = sc.ROITakeProfit

			feed, err := NewHistoricalFeed(csvPath, from, to, cfg.IVPercentileWindow)
			if err != nil {
				slog.Error("scenario feed error", "scenario", sc.Name, "err", err)
				return
			}
			exec := NewSimExecutor(cfg.Backtest, 100000)
			engine := NewEngine(&scCfg, feed, exec)
			summary, err := engine.Run(context.Background())
			if err != nil {
				slog.Error("scenario run error", "scenario", sc.Name, "err", err)
				return
			}
			summary.Scenario = sc.Name
			results[i] = ScenarioResult{Summary: summary, ScenarioName: sc.Name, RunAt: time.Now()}
		}(i, sc)
	}
	wg.Wait()

	// Sort by Sharpe descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].SharpeRatio > results[j].SharpeRatio
	})

	w, err := NewResultWriter(outputDir)
	if err != nil {
		return err
	}
	return w.WriteScenarioComparison(results)
}

// RunWalkForward splits the date range into N windows and validates each.
func RunWalkForward(cfg *config.Config, csvPath string, from, to time.Time, windows int, outputDir string) error {
	total := to.Sub(from)
	windowSize := total / time.Duration(windows)
	writer, err := NewResultWriter(outputDir)
	if err != nil {
		return err
	}
	var wfResults []WalkForwardResult

	for i := 0; i < windows; i++ {
		winStart := from.Add(time.Duration(i) * windowSize)
		winEnd := winStart.Add(windowSize)
		splitPoint := winStart.Add(time.Duration(float64(windowSize) * 0.75))

		// Train
		trainFeed, err := NewHistoricalFeed(csvPath, winStart, splitPoint, cfg.IVPercentileWindow)
		if err != nil {
			return fmt.Errorf("walk-forward train feed window %d: %w", i+1, err)
		}
		trainExec := NewSimExecutor(cfg.Backtest, 100000)
		trainEngine := NewEngine(cfg, trainFeed, trainExec)
		trainSummary, err := trainEngine.Run(context.Background())
		if err != nil {
			return err
		}
		if err := writer.WriteWindowResult(i+1, "train", trainSummary); err != nil {
			return err
		}

		// Validate
		valFeed, err := NewHistoricalFeed(csvPath, splitPoint, winEnd, cfg.IVPercentileWindow)
		if err != nil {
			return fmt.Errorf("walk-forward validate feed window %d: %w", i+1, err)
		}
		valExec := NewSimExecutor(cfg.Backtest, 100000)
		valEngine := NewEngine(cfg, valFeed, valExec)
		valSummary, err := valEngine.Run(context.Background())
		if err != nil {
			return err
		}
		if err := writer.WriteWindowResult(i+1, "validate", valSummary); err != nil {
			return err
		}

		degradation := 0.0
		overfit := false
		if trainSummary.SharpeRatio != 0 {
			degradation = (trainSummary.SharpeRatio - valSummary.SharpeRatio) / math.Abs(trainSummary.SharpeRatio) * 100
			overfit = degradation > 30
		}

		wfResults = append(wfResults, WalkForwardResult{
			Window:         i + 1,
			TrainFrom:      winStart.Format("2006-01-02"),
			TrainTo:        splitPoint.Format("2006-01-02"),
			ValidateFrom:   splitPoint.Format("2006-01-02"),
			ValidateTo:     winEnd.Format("2006-01-02"),
			TrainSharpe:    trainSummary.SharpeRatio,
			ValidateSharpe: valSummary.SharpeRatio,
			Degradation:    degradation,
			Overfit:        overfit,
		})
	}

	return writer.WriteWalkForwardSummary(wfResults)
}
