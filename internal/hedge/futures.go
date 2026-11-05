package hedge

import (
	"math"
	"time"
)

// The futures hedge experiment (#657): while it is on, the bot keeps the legs
// the GEX shed would close and holds the book's delta near zero with the
// coin's inverse perpetual. Everything here is pure: the strategy reads the
// exchange, calls these and places the orders.

// Sides of a perpetual trade.
const (
	SideBuy  = "buy"
	SideSell = "sell"
)

// PerpDelta is the coin delta of an inverse perpetual position of sizeUSD
// (signed: + long, − short) at spot: one USD of contract is 1/spot coin.
func PerpDelta(sizeUSD, spot float64) float64 {
	if spot <= 0 {
		return 0
	}
	return sizeUSD / spot
}

// Band is the |net delta| (coin) the hedge tolerates: pct % of the margin
// balance. A balance in USD (cross collateral) is converted at spot.
func Band(marginBalance float64, balanceInUSD bool, spot, pct float64) float64 {
	if balanceInUSD {
		if spot <= 0 {
			return 0
		}
		marginBalance /= spot
	}
	return math.Abs(marginBalance) * pct / 100
}

// HedgeOrder is the perpetual trade that brings the book's net delta (the
// options' plus the perpetual's, in coin) back to zero: usdAmount in whole
// contracts of contractUSD and its side. Nothing (0, "") while |net| is under
// bandCoin, without a price or contract size, or when the trade rounds to
// less than one contract. A long book (net > 0) sells, a short one buys.
func HedgeOrder(optionsDelta, hedgeDelta, bandCoin, spot, contractUSD float64) (usdAmount float64, side string) {
	net := optionsDelta + hedgeDelta
	if spot <= 0 || contractUSD <= 0 || math.IsNaN(net) || math.Abs(net) < bandCoin || net == 0 {
		return 0, ""
	}
	usd := math.Round(math.Abs(net)*spot/contractUSD) * contractUSD
	if usd < contractUSD {
		return 0, ""
	}
	if net > 0 {
		return usd, SideSell
	}
	return usd, SideBuy
}

// PerpPnL is the coin P&L of an inverse perpetual position of sizeUSD
// (signed) opened at avg and valued at mark: size × (1/avg − 1/mark).
func PerpPnL(sizeUSD, avg, mark float64) float64 {
	if avg <= 0 || mark <= 0 {
		return 0
	}
	return sizeUSD * (1/avg - 1/mark)
}

// TakerFee is the fee (coin) of trading usd of the perpetual at price.
func TakerFee(usd, price, rate float64) float64 {
	if price <= 0 {
		return 0
	}
	return math.Abs(usd) / price * rate
}

// RateLimited reports whether another hedge order would be the
// (max+1)-th within the rolling hour before now.
func RateLimited(sent []time.Time, now time.Time, max int) bool {
	n := 0
	for _, t := range sent {
		if now.Sub(t) < time.Hour {
			n++
		}
	}
	return n >= max
}

// ── Switch-off preview ───────────────────────────────────────────────────────

// PreviewInput is what the switch-off preview is built from.
type PreviewInput struct {
	Spot         float64
	OptionsDelta float64 // coin delta (premium-adjusted: what the hedge holds at zero)
	// ScenarioDelta is the options' Black–Scholes (USD) delta, which the
	// delta–gamma scenarios in USD use with OptionsGamma.
	ScenarioDelta float64
	OptionsGamma  float64 // coin per USD of spot
	PerpUSD       float64 // signed perpetual position
	PerpAvg       float64
	PerpMark      float64
	Unrealised    float64 // coin, as Deribit reports it (0: computed from the prices)
	Funding       float64 // coin, realised funding of the position
	FeeRate       float64 // taker fee rate for the close
}

// Scenario is the options' P&L for a spot move once the hedge is closed:
// delta–gamma, δ·ΔS + ½·Γ·ΔS² in USD, and in coin at the moved price.
type Scenario struct {
	MovePct float64 `json:"move_pct"`
	Spot    float64 `json:"spot"`
	PnLCoin float64 `json:"pnl_coin"`
	PnLUSD  float64 `json:"pnl_usd"`
}

// Preview is what switching the hedge off would do and leave.
type Preview struct {
	PerpUSD        float64    `json:"perp_usd"`
	PerpDeltaCoin  float64    `json:"perp_delta_coin"`
	PerpAvg        float64    `json:"perp_avg_price"`
	PerpMark       float64    `json:"perp_mark_price"`
	UnrealisedCoin float64    `json:"unrealised_coin"`
	UnrealisedUSD  float64    `json:"unrealised_usd"`
	FundingCoin    float64    `json:"funding_coin"`
	CloseFeeCoin   float64    `json:"close_fee_coin"`
	CloseFeeUSD    float64    `json:"close_fee_usd"`
	DeltaNow       float64    `json:"delta_now_coin"`
	DeltaAfter     float64    `json:"delta_after_coin"`
	Scenarios      []Scenario `json:"scenarios"`
}

// ScenarioMoves are the spot moves the preview prices (%).
var ScenarioMoves = []float64{-10, -5, 5, 10}

// BuildPreview prices closing the perpetual at its mark and what the
// options carry alone afterwards.
func BuildPreview(in PreviewInput) Preview {
	mark := in.PerpMark
	if mark <= 0 {
		mark = in.Spot
	}
	unreal := in.Unrealised
	if unreal == 0 {
		unreal = PerpPnL(in.PerpUSD, in.PerpAvg, mark)
	}
	perpDelta := PerpDelta(in.PerpUSD, mark)
	fee := TakerFee(in.PerpUSD, mark, in.FeeRate)
	p := Preview{
		PerpUSD: in.PerpUSD, PerpDeltaCoin: perpDelta, PerpAvg: in.PerpAvg, PerpMark: mark,
		UnrealisedCoin: unreal, UnrealisedUSD: unreal * mark,
		FundingCoin:  in.Funding,
		CloseFeeCoin: fee, CloseFeeUSD: fee * mark,
		DeltaNow: in.OptionsDelta + perpDelta, DeltaAfter: in.OptionsDelta,
	}
	for _, m := range ScenarioMoves {
		p.Scenarios = append(p.Scenarios, ScenarioPnL(in.Spot, in.ScenarioDelta, in.OptionsGamma, m))
	}
	return p
}

// ScenarioPnL is the delta–gamma P&L of the options for a movePct spot move.
func ScenarioPnL(spot, delta, gamma, movePct float64) Scenario {
	dS := spot * movePct / 100
	usd := delta*dS + 0.5*gamma*dS*dS
	moved := spot + dS
	coin := 0.0
	if moved > 0 {
		coin = usd / moved
	}
	return Scenario{MovePct: movePct, Spot: moved, PnLCoin: coin, PnLUSD: usd}
}

// BookScenario is the book's result for a one-day spot move: the options
// alone (delta–gamma, as ScenarioPnL) and with the perpetual's inverse P&L
// over the same move. Coin and, at the moved price, USD.
type BookScenario struct {
	MovePct     float64 `json:"move_pct"`
	Spot        float64 `json:"spot"`
	OptionsCoin float64 `json:"options_coin"`
	OptionsUSD  float64 `json:"options_usd"`
	BookCoin    float64 `json:"book_coin"`
	BookUSD     float64 `json:"book_usd"`
}

// BookScenarios prices ScenarioMoves on the whole book (#666). Empty without
// a spot price: nothing to move.
func BookScenarios(spot, delta, gamma, perpUSD float64) []BookScenario {
	out := []BookScenario{}
	if spot <= 0 {
		return out
	}
	for _, m := range ScenarioMoves {
		sc := ScenarioPnL(spot, delta, gamma, m)
		perp := PerpPnL(perpUSD, spot, sc.Spot)
		out = append(out, BookScenario{MovePct: m, Spot: sc.Spot, OptionsCoin: sc.PnLCoin, OptionsUSD: sc.PnLUSD,
			BookCoin: sc.PnLCoin + perp, BookUSD: sc.PnLUSD + perp*sc.Spot})
	}
	return out
}
