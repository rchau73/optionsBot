package strategy

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

// waitForTickerData blocks until the index price has arrived, or until the
// timeout expires, so the first entry attempt does not run on an empty chain.
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

// logStartupState logs the account's equity and margin once at startup.
func (s *Strategy) logStartupState(ctx context.Context) {
	sum, err := s.exch.GetAccountSummary(ctx, s.cfg.Underlying)
	if err != nil {
		slog.Warn("startup: could not fetch account summary", "err", err)
		return
	}
	ivPct := s.md.IVPercentile()
	price := s.md.UnderlyingPrice()
	u := sum.MarginUsage()

	unit := strings.ToLower(s.cfg.Underlying)
	slog.Info("account state",
		"currency", sum.Currency,
		"equity_"+unit, fmt.Sprintf("%.6f", sum.Equity),
		"equity_usd", fmt.Sprintf("%.2f", sum.Equity*price),
		"available_funds_"+unit, fmt.Sprintf("%.6f", sum.AvailableFunds),
		"available_funds_usd", fmt.Sprintf("%.2f", sum.AvailableFunds*price),
		"margin_used_"+unit, fmt.Sprintf("%.6f", sum.InitialMargin),
		"margin_maintenance_"+unit, fmt.Sprintf("%.6f", sum.MaintenanceMargin),
		"margin_model", sum.MarginModel,
		"cross_collateral", sum.CrossCollateralEnabled,
		"margin_unit", u.Unit,
		"im_pct", fmt.Sprintf("%.2f", u.IMPct()),
		"mm_pct", fmt.Sprintf("%.2f", u.MMPct()),
		"iv_margin_bands", s.riskCfg.SortedBands(),
		"max_mm_pct", s.riskCfg.MaxMMPct,
		"iv_band_confirm_days", s.riskCfg.ConfirmDays,
		"gamma_regime_rule", s.riskCfg.UseRegime,
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

// reconcilePositions rebuilds the in-memory book from the exchange on startup.
// State lives only in memory, so after any restart Deribit is the source of
// truth: open shorts are loaded and regrouped into strangles per expiry.
func (s *Strategy) reconcilePositions(ctx context.Context) {
	// Cancel this currency's open orders first: orders left from before the
	// restart are stale, and the in-memory pending book that tracked them is gone.
	if err := s.exch.CancelAllOrders(ctx, s.cfg.Underlying); err != nil {
		slog.Warn("reconcile: cancel_all failed (non-fatal)", "err", err)
	} else {
		slog.Info("reconcile: cancelled open orders on startup", "currency", s.cfg.Underlying)
	}

	// The exchange is the source of truth: read every open short position.
	raw, err := s.exch.GetPositions(ctx, s.cfg.Underlying)
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
			underlying, expiry, strike, optType, parseErr = marketdata.ParseOptionName(rp.InstrumentName)
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
		for _, leg := range []*orders.Position{call, put} {
			if leg != nil {
				s.journal.LogReconciled(leg, s.instrumentContext(leg.Instrument, slotRef(bestSlot.TargetDTE, bestSlot.EntryDelta)))
			}
		}
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

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
