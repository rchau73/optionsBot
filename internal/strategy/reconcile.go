package strategy

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
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
	best := slots[0]
	bestScore := math.MaxFloat64
	for _, sl := range slots {
		if score := slotScore(call, put, expiry, now, sl); score < bestScore {
			bestScore = score
			best = sl
		}
	}
	return best
}

// slotScore is how far a strangle sits from a slot: DTE distance plus delta
// distance × 100 (lower is closer).
func slotScore(call, put *orders.Position, expiry, now time.Time, sl config.StrangleSlot) float64 {
	refDelta := 0.0
	if call != nil {
		refDelta = math.Abs(call.CurrentGreeks.Delta)
	} else if put != nil {
		refDelta = math.Abs(put.CurrentGreeks.Delta)
	}
	dteDist := float64(absInt(sl.TargetDTE - marketdata.DaysToExpiry(expiry, now)))
	return dteDist + math.Abs(sl.EntryDelta-refDelta)*100
}

// splitOff moves qty of p into a new position (no ID) with the same
// instrument, entry price and greeks, and its share of premium and fees.
func splitOff(p *orders.Position, qty float64) *orders.Position {
	piece := *p
	piece.ID = ""
	piece.Qty = qty
	piece.PremiumReceived = p.PremiumReceived * qty / p.Qty
	piece.Fees = p.Fees * qty / p.Qty
	p.PremiumReceived -= piece.PremiumReceived
	p.Fees -= piece.Fees
	p.Qty -= qty
	return &piece
}

// closestQty is the index of the position whose size is closest to qty.
func closestQty(ps []*orders.Position, qty float64) int {
	best := 0
	for i, p := range ps {
		if math.Abs(p.Qty-qty) < math.Abs(ps[best].Qty-qty) {
			best = i
		}
	}
	return best
}

// ReconciledStrangle is one strangle rebuilt from exchange positions; Call
// or Put is nil for a one-legged strangle (repair completes it).
type ReconciledStrangle struct {
	Call, Put *orders.Position
	Expiry    time.Time
	Slot      config.StrangleSlot
}

// GroupPositions rebuilds strangles from short positions after a restart.
// An expiry can hold several strangles (two slots on one month-end, plus
// top-ups), so on each expiry calls and puts are paired by size — legs
// opened together have the same size — and, among equal sizes, by rank from
// the money (nearest call with nearest put); any leg left over becomes a
// one-legged strangle. Every position lands in
// exactly one strangle. Each strangle gets the closest slot not yet taken
// (closest pairs first); only when every slot is taken may two share one.
//
// Deribit reports one position per instrument, so a strike that backed two
// strangles (two slots on one expiry picking the same call, say) comes back
// as one larger leg. When a pair is uneven and the same expiry has a lone leg
// of the other type, the larger leg is split: its excess (up to the lone
// leg's size) becomes a second position paired with the lone leg. Without
// the split, leg balancing bought the "excess" back and repair re-sold it at
// every restart (2026-10-06: ETH 3300-C 262 = 162 + 100, two round trips).
// Split pieces have no ID; the caller stores them and the reduced originals.
// The input positions are not modified.
//
// It replaced a one-strangle-per-expiry rule that kept only the last call and
// put of an expiry: the other legs were loaded but in no strangle, so the
// startup rebalance undercounted the book and added top-ups at every restart,
// and leg balancing bought back legs it saw as excess.
func GroupPositions(positions []*orders.Position, now time.Time, slots []config.StrangleSlot) []ReconciledStrangle {
	type side struct{ calls, puts []*orders.Position }
	byExpiry := map[time.Time]*side{}
	var expiries []time.Time
	for _, orig := range positions {
		cp := *orig // work on copies: splitting changes sizes
		p := &cp
		sd := byExpiry[p.Expiry]
		if sd == nil {
			sd = &side{}
			byExpiry[p.Expiry] = sd
			expiries = append(expiries, p.Expiry)
		}
		if p.OptionType == "call" {
			sd.calls = append(sd.calls, p)
		} else {
			sd.puts = append(sd.puts, p)
		}
	}
	sort.Slice(expiries, func(i, j int) bool { return expiries[i].Before(expiries[j]) })

	nearestFirst := func(ps []*orders.Position) {
		sort.SliceStable(ps, func(i, j int) bool {
			di, dj := math.Abs(ps[i].CurrentGreeks.Delta), math.Abs(ps[j].CurrentGreeks.Delta)
			if di != dj {
				return di > dj
			}
			return ps[i].Instrument < ps[j].Instrument
		})
	}
	var out []ReconciledStrangle
	for _, exp := range expiries {
		sd := byExpiry[exp]
		nearestFirst(sd.calls)
		nearestFirst(sd.puts)
		// Legs opened together have the same size, so pair the closest
		// sizes first; among equal sizes, the same rank from the money.
		type pair struct {
			c, p     int
			qty, rnk float64
		}
		var cands []pair
		for i, c := range sd.calls {
			for j, p := range sd.puts {
				cands = append(cands, pair{i, j, math.Abs(c.Qty - p.Qty), math.Abs(float64(i - j))})
			}
		}
		sort.SliceStable(cands, func(a, b int) bool {
			if cands[a].qty != cands[b].qty {
				return cands[a].qty < cands[b].qty
			}
			return cands[a].rnk < cands[b].rnk
		})
		usedC, usedP := map[int]bool{}, map[int]bool{}
		var pairs []ReconciledStrangle
		for _, cd := range cands {
			if usedC[cd.c] || usedP[cd.p] {
				continue
			}
			usedC[cd.c], usedP[cd.p] = true, true
			pairs = append(pairs, ReconciledStrangle{Call: sd.calls[cd.c], Put: sd.puts[cd.p], Expiry: exp})
		}
		var loneCalls, lonePuts []*orders.Position
		for i, c := range sd.calls {
			if !usedC[i] {
				loneCalls = append(loneCalls, c)
			}
		}
		for j, p := range sd.puts {
			if !usedP[j] {
				lonePuts = append(lonePuts, p)
			}
		}
		// Split an uneven pair's excess onto a lone leg of the other type.
		for i := range pairs {
			pr := &pairs[i]
			for {
				larger, smaller, lone := pr.Call, pr.Put, &lonePuts
				if pr.Put.Qty > pr.Call.Qty {
					larger, smaller, lone = pr.Put, pr.Call, &loneCalls
				}
				excess := larger.Qty - smaller.Qty
				if excess <= qtyEpsilon || len(*lone) == 0 {
					break
				}
				k := closestQty(*lone, excess)
				partner := (*lone)[k]
				*lone = append((*lone)[:k], (*lone)[k+1:]...)
				piece := splitOff(larger, math.Min(excess, partner.Qty))
				if piece.OptionType == "call" {
					pairs = append(pairs, ReconciledStrangle{Call: piece, Put: partner, Expiry: exp})
				} else {
					pairs = append(pairs, ReconciledStrangle{Call: partner, Put: piece, Expiry: exp})
				}
				pr = &pairs[i] // append may have moved the slice
			}
		}
		out = append(out, pairs...)
		for _, c := range loneCalls {
			out = append(out, ReconciledStrangle{Call: c, Expiry: exp})
		}
		for _, p := range lonePuts {
			out = append(out, ReconciledStrangle{Put: p, Expiry: exp})
		}
	}

	// Slots: repeatedly take the closest (strangle, free slot) pair.
	taken := map[int]bool{}
	assigned := make([]bool, len(out))
	for range out {
		bi, bs, best := -1, -1, math.MaxFloat64
		for i, st := range out {
			if assigned[i] {
				continue
			}
			for j, sl := range slots {
				if taken[j] && len(taken) < len(slots) {
					continue
				}
				if sc := slotScore(st.Call, st.Put, st.Expiry, now, sl); sc < best {
					bi, bs, best = i, j, sc
				}
			}
		}
		out[bi].Slot, assigned[bi], taken[bs] = slots[bs], true, true
	}
	return out
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
	var loaded []*orders.Position

	for _, rp := range shorts {
		pos, err := s.positionFromRaw(rp, now)
		if err != nil {
			slog.Warn("reconcile: cannot parse instrument", "name", rp.InstrumentName, "err", err)
			continue
		}
		s.state.AddPosition(pos)
		loaded = append(loaded, pos)

		slog.Info("reconcile: loaded position",
			"instrument", pos.Instrument,
			"type", pos.OptionType,
			"strike", pos.Strike,
			"expiry", pos.Expiry.Format("2006-01-02"),
			"dte", pos.DTE(),
			"qty", pos.Qty,
			"avg_price_btc", fmt.Sprintf("%.6f", pos.EntryPrice),
			"mark_price_btc", fmt.Sprintf("%.6f", pos.CurrentMid),
			"unrealised_pnl_btc", fmt.Sprintf("%.6f", pos.NetPnL()),
			"delta", fmt.Sprintf("%.4f", pos.CurrentGreeks.Delta),
		)
	}

	// Reconstruct strangles: every position in exactly one strangle, pairs
	// matched by delta on each expiry (GroupPositions). Single legs are
	// registered as partial strangles so repair can fill the missing leg.
	for _, g := range GroupPositions(loaded, now, s.cfg.Slots()) {
		call, put, expiry, bestSlot := g.Call, g.Put, g.Expiry, g.Slot
		// Store split pieces and the reduced sizes of the legs they came from.
		for _, leg := range []*orders.Position{call, put} {
			switch {
			case leg == nil:
			case leg.ID == "":
				leg.ID = s.state.NextID("pos")
				s.state.AddPosition(leg)
				slog.Info("reconcile: split a strike shared by two strangles",
					"instrument", leg.Instrument, "qty", leg.Qty)
			default:
				s.state.UpdatePositionQty(leg.ID, leg.Qty, leg.PremiumReceived)
			}
		}
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

	// Invariant: every loaded short belongs to exactly one strangle. A leg
	// outside every strangle is invisible to sizing and balancing.
	inStrangle := map[string]int{}
	for _, st := range s.state.AllStrangles() {
		for _, leg := range []*orders.Position{st.CallLeg, st.PutLeg} {
			if leg != nil {
				inStrangle[leg.ID]++
			}
		}
	}
	for _, p := range loaded {
		if inStrangle[p.ID] != 1 {
			slog.Error("reconcile: position not in exactly one strangle — sizing and balancing would misread the book",
				"instrument", p.Instrument, "qty", p.Qty, "strangles", inStrangle[p.ID])
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

// PerOptionGreeks converts the greeks private/get_positions reports for a
// position (size-weighted, with size negative for a short) into the greeks
// of one option, as tickers report them: a 1,239-contract short put with
// position delta +114 is a put of delta −0.092.
func PerOptionGreeks(rp orders.RawPosition) orders.Greeks {
	if rp.Size == 0 {
		return orders.Greeks{}
	}
	return orders.Greeks{
		Delta: rp.Delta / rp.Size,
		Gamma: rp.Gamma / rp.Size,
		Theta: rp.Theta / rp.Size,
		Vega:  rp.Vega / rp.Size,
		Rho:   rp.Rho / rp.Size,
	}
}
