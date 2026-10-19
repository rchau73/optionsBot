package strategy

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"optionsbot/internal/marketdata"
)

// The liquidity cap (#103): margin alone sized ETH's entries at up to 9× a
// strike's whole mainnet open interest; the book filled a tenth and the slots
// sat one-legged at IM 1.9 % of 20 %. Each new leg — an entry or a rebalance
// top-up — is now at most liquidity_cap_oi_pct (10 %) of its strike's mainnet
// open interest (the GEX manager's book summary, no extra call). Both legs of
// a strangle get the smaller of their caps, so they stay even. A slot the cap
// leaves short of its share stays short (no re-send of the rest). Repairs are
// exempt: they restore the size of a leg already held.

// SkipLiquidityCap is journaled when the cap cuts (or blocks) an order.
const SkipLiquidityCap = "liquidity_cap"

// errLiquidityCap: the cap leaves less than one lot, so nothing is sent. The
// skip is already journaled; callers must not journal it again.
var errLiquidityCap = errors.New("liquidity cap below one lot")

// liquidityLimit returns qty capped by the open interest of the legs that
// will be sold, with each leg's open interest for the journal. known is false
// when a source is wired but has no snapshot yet: the market's size is
// unknown (entries already wait for fresh GEX, gexready.go). With no source
// wired (tests, tools) or the cap off, qty is unchanged. It journals nothing,
// so a dry run can use it too.
func (s *Strategy) liquidityLimit(call, put *marketdata.Instrument, openCall, openPut bool, qty, lot float64) (capped float64, legs []string, known bool) {
	pct := s.cfg.LiquidityCapOIPct
	if s.oi == nil || pct <= 0 {
		return qty, nil, true
	}
	snap := s.oi.OpenInterest()
	if snap == nil {
		return 0, nil, false
	}
	capped = qty
	for _, l := range []struct {
		inst *marketdata.Instrument
		open bool
	}{{call, openCall}, {put, openPut}} {
		if !l.open {
			continue
		}
		oi := snap.ByInstrument[l.inst.Name]
		legCap := LiquidityCapQty(oi, pct, lot)
		capped = math.Min(capped, legCap)
		legs = append(legs, fmt.Sprintf("%s OI %g → ≤ %g", l.inst.Name, oi, legCap))
	}
	return capped, legs, true
}

// capToLiquidity applies liquidityLimit to an order about to be sent and
// journals the cap when it binds. An order that goes out capped, and every
// capped top-up, gets its own liquidity_cap line: noteSkip's per-slot dedup is
// reset only by an entry into an empty slot, so a second cap on the same slot
// (a re-armed top-up, a re-entry) would otherwise go unrecorded. Only an entry
// the cap blocks — retried every cycle — is deduped per slot.
func (s *Strategy) capToLiquidity(call, put *marketdata.Instrument, openCall, openPut bool, qty, lot float64, targetDTE int, entryDelta float64, complement bool) (float64, error) {
	pct := s.cfg.LiquidityCapOIPct
	kind := "entry"
	if complement {
		kind = "top-up"
	}
	capped, legs, known := s.liquidityLimit(call, put, openCall, openPut, qty, lot)
	var detail string
	switch {
	case !known:
		detail = kind + " not sent: no mainnet open interest yet, the market's size is unknown"
	case capped >= qty-qtyEpsilon:
		return qty, nil
	case capped < lot:
		detail = fmt.Sprintf("%s of %g not sent: %g%% of mainnet open interest is under one lot of %g (%s)",
			kind, qty, pct, lot, strings.Join(legs, "; "))
	default:
		detail = fmt.Sprintf("%s of %g capped to %g at %g%% of mainnet open interest (%s)",
			kind, qty, capped, pct, strings.Join(legs, "; "))
	}
	slog.Info("order size capped by the strike's open interest",
		"kind", kind, "target_dte", targetDTE, "entry_delta", entryDelta, "call", call.Name, "put", put.Name,
		"margin_qty", qty, "capped_qty", capped, "cap_oi_pct", pct, "legs", strings.Join(legs, "; "))
	sent := known && capped >= lot
	if sent || complement {
		s.journal.LogSkipped(SkipLiquidityCap+": "+detail, s.eventContext(nil, slotRef(targetDTE, entryDelta)))
	} else {
		s.noteSkip(targetDTE, entryDelta, SkipLiquidityCap, detail)
	}
	if !sent {
		return 0, errLiquidityCap
	}
	return capped, nil
}
