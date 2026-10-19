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

// SkipLiquidityCap is journaled once when the cap cuts (or blocks) an order.
const SkipLiquidityCap = "liquidity_cap"

// errLiquidityCap: the cap leaves less than one lot, so nothing is sent. The
// skip is already journaled; callers must not journal it again.
var errLiquidityCap = errors.New("liquidity cap below one lot")

// capToLiquidity returns qty capped by the open interest of the legs that
// will be sold. With no open-interest source wired (tests, tools) or the cap
// off, qty is unchanged. With a source but no snapshot, nothing is sent: the
// market's size is unknown (entries already wait for fresh GEX, gexready.go).
func (s *Strategy) capToLiquidity(call, put *marketdata.Instrument, openCall, openPut bool, qty, lot float64, targetDTE int, entryDelta float64, complement bool) (float64, error) {
	pct := s.cfg.LiquidityCapOIPct
	if s.oi == nil || pct <= 0 {
		return qty, nil
	}
	snap := s.oi.OpenInterest()
	if snap == nil {
		s.noteSkip(targetDTE, entryDelta, SkipLiquidityCap, "no mainnet open interest yet: the market's size is unknown")
		return 0, errLiquidityCap
	}
	capped := qty
	var legs []string
	for _, l := range []struct {
		inst *marketdata.Instrument
		open bool
	}{{call, openCall}, {put, openPut}} {
		if !l.open {
			continue
		}
		oi := snap.ByInstrument[l.inst.Name]
		capped = math.Min(capped, LiquidityCapQty(oi, pct, lot))
		legs = append(legs, fmt.Sprintf("%s OI %g → ≤ %g", l.inst.Name, oi, LiquidityCapQty(oi, pct, lot)))
	}
	if capped >= qty-qtyEpsilon {
		return qty, nil
	}
	kind := "entry"
	if complement {
		kind = "top-up"
	}
	detail := fmt.Sprintf("%s of %g capped to %g at %g%% of mainnet open interest (%s)",
		kind, qty, capped, pct, strings.Join(legs, "; "))
	if capped < lot {
		detail = fmt.Sprintf("%s of %g not sent: %g%% of mainnet open interest is under one lot of %g (%s)",
			kind, qty, pct, lot, strings.Join(legs, "; "))
	}
	slog.Info("order size capped by the strike's open interest",
		"kind", kind, "target_dte", targetDTE, "entry_delta", entryDelta, "call", call.Name, "put", put.Name,
		"margin_qty", qty, "capped_qty", capped, "cap_oi_pct", pct, "legs", strings.Join(legs, "; "))
	s.noteSkip(targetDTE, entryDelta, SkipLiquidityCap, detail)
	if capped < lot {
		return 0, errLiquidityCap
	}
	return capped, nil
}
