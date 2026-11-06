package api

import (
	"math"
	"time"

	"optionsbot/internal/hedge"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// Each strangle's share of the futures hedge on its payoff chart (#695). A
// share is the perpetual × the strangle's part of the options' coin delta, the
// measure the hedge itself holds (TotalCoinDelta), so the shares add up to the
// perpetual. The perpetual is drawn as held now: its inverse P&L at each price
// from its average price, so the curve at spot carries what it already gained
// or lost. Inside the hedge band the options' delta is too small to divide by
// and nothing is split: the hedge stays on the whole book (the product owner,
// 2026-11-06; a split by |delta| can hedge a strangle further the wrong way).

// PayoffHedgeMoves are the moves of a strangle's table, % of spot.
var PayoffHedgeMoves = []float64{-20, -10, -5, 5, 10, 20}

// Why a held perpetual is not split per strangle.
const (
	hedgeNotSplitBand   = "the options' net delta is inside the hedge band"
	hedgeNotSplitGreeks = "a leg has no greeks yet"
)

// PayoffHedge is the perpetual behind the shares; null when none is held.
type PayoffHedge struct {
	PerpUSD          float64 `json:"perp_usd"`        // signed
	PerpDeltaCoin    float64 `json:"perp_delta_coin"` // signed
	OptionsDeltaCoin float64 `json:"options_delta_coin"`
	BandCoin         float64 `json:"band_coin"`
	// AvgPrice is what the curves count from: the ledger's at index pricing
	// (testnet, #664), else the exchange's; spot when neither is known.
	AvgPrice float64 `json:"avg_price"`
	// Split: the strangles carry shares; NotSplitReason says why not.
	Split          bool   `json:"split"`
	NotSplitReason string `json:"not_split_reason,omitempty"`
}

// PayoffMove is one move of a strangle's table: bought back today at the moved
// price, coin first, USD at that price.
type PayoffMove struct {
	MovePct     float64 `json:"move_pct"`
	Spot        float64 `json:"spot"`
	OptionsCoin float64 `json:"options_coin"`
	OptionsUSD  float64 `json:"options_usd"`
	HedgedCoin  float64 `json:"hedged_coin"`
	HedgedUSD   float64 `json:"hedged_usd"`
}

// PayoffWithHedge is PayoffOf with each strangle's share of the perpetual in
// h. No perpetual leaves the hedged fields null.
func PayoffWithHedge(v strategy.View, h strategy.HedgeView, now time.Time) PayoffReport {
	rep := PayoffOf(v, now)
	if math.Abs(h.PerpUSD) < perpFlatUSD {
		return rep
	}
	spot := v.Market.Spot
	var options float64
	for i := range rep.Strangles {
		options += strangleCoinDelta(rep.Strangles[i])
	}
	ph := PayoffHedge{PerpUSD: h.PerpUSD, PerpDeltaCoin: h.PerpDeltaCoin, OptionsDeltaCoin: options, BandCoin: h.BandCoin, AvgPrice: perpAnchor(h, spot)}
	rep.Hedge = &ph
	if ph.NotSplitReason = hedgeNotSplitReason(v, options, h.BandCoin); ph.NotSplitReason != "" {
		return rep
	}
	ph.Split = true
	for i := range rep.Strangles {
		sp := &rep.Strangles[i]
		share := strangleCoinDelta(*sp) / options
		addHedgeShare(sp, ph, share, now)
	}
	return rep
}

// perpAnchor is the average price of the perpetual at the view's pricing:
// testnet's own fills sit about 3.5 % from the index the curves are drawn on.
func perpAnchor(h strategy.HedgeView, spot float64) float64 {
	avg := h.PerpAvg
	if h.Benchmark.Pricing == string(hedge.PricingIndex) {
		avg = h.LedgerAvg
	}
	if avg > 0 {
		return avg
	}
	return spot
}

// hedgeNotSplitReason is why the perpetual cannot be shared out; "" when it
// can. An unknown band (0) cannot tell a stable share from one that blows up.
func hedgeNotSplitReason(v strategy.View, options, band float64) string {
	for _, st := range v.Strangles {
		for _, l := range st.Legs {
			if l.Greeks == (orders.Greeks{}) {
				return hedgeNotSplitGreeks
			}
		}
	}
	if band <= 0 || options == 0 || math.Abs(options) < band {
		return hedgeNotSplitBand
	}
	return ""
}

// strangleCoinDelta is the strangle's delta as the hedge counts it: each leg's
// delta less its mark in coin (orders.StateManager.TotalCoinDelta).
func strangleCoinDelta(sp StranglePayoff) float64 {
	var d float64
	for _, l := range sp.Legs {
		d += l.PositionDelta + l.Mark*l.Qty
	}
	return d
}

// addHedgeShare fills one strangle's hedged fields; share is its fraction of
// the perpetual.
func addHedgeShare(sp *StranglePayoff, ph PayoffHedge, share float64, now time.Time) {
	usd, deltaShare, coinDelta := ph.PerpUSD*share, ph.PerpDeltaCoin*share, strangleCoinDelta(*sp)
	perpAt := func(price float64) float64 { return hedge.PerpPnL(usd, ph.AvgPrice, price) }
	sp.HedgeShareUSD, sp.HedgeShareDeltaCoin = &usd, &deltaShare
	sp.CoinDelta = &coinDelta
	sp.NetDeltaWithHedgeCoin = ptr(coinDelta + deltaShare)
	sp.UnrealisedWithHedgeCoin = ptr(sp.UnrealisedCoin + perpAt(sp.Spot))
	sp.UnrealisedWithHedgeUSD = ptr(*sp.UnrealisedWithHedgeCoin * sp.Spot)
	for i := range sp.Points {
		pt := &sp.Points[i]
		gain := perpAt(pt.Price)
		pt.ExpiryHedgedPnL = ptr(pt.ExpiryPnL + gain)
		if pt.TodayPnL != nil {
			pt.TodayHedgedPnL = ptr(*pt.TodayPnL + gain)
		}
	}
	sp.HedgedBreakEvens = hedgedBreakEvens(sp.Points)
	if sp.Today {
		years := sp.Expiry.Sub(now).Hours() / 24 / 365
		sp.HedgeMoves = hedgeMoves(*sp, years, perpAt)
	}
}

// hedgedBreakEvens are the prices where the hedged curve at expiry crosses
// zero on the chart (linear between its points), ascending; [] when none.
func hedgedBreakEvens(pts []PayoffPoint) []float64 {
	out := []float64{}
	for i := 1; i < len(pts); i++ {
		a, b := *pts[i-1].ExpiryHedgedPnL, *pts[i].ExpiryHedgedPnL
		if a == b || (a < 0) == (b < 0) {
			continue
		}
		out = append(out, pts[i-1].Price+(pts[i].Price-pts[i-1].Price)*a/(a-b))
	}
	return out
}

// hedgeMoves prices PayoffHedgeMoves bought back today, with and without the
// share (callers checked the today curve exists).
func hedgeMoves(sp StranglePayoff, years float64, perpAt func(float64) float64) []PayoffMove {
	out := make([]PayoffMove, 0, len(PayoffHedgeMoves))
	for _, m := range PayoffHedgeMoves {
		s := sp.Spot * (1 + m/100)
		var options float64
		for _, l := range sp.Legs {
			options += legTodayAt(l, s, sp.Spot, years)
		}
		hedged := options + perpAt(s)
		out = append(out, PayoffMove{MovePct: m, Spot: s, OptionsCoin: options, OptionsUSD: options * s, HedgedCoin: hedged, HedgedUSD: hedged * s})
	}
	return out
}
