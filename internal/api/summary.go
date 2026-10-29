package api

import (
	"math"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// The investor summary (/api/summary, #177) answers four questions in plain
// numbers: what the bot holds, whether it makes or loses money, the worst case
// right now in money, and why it did its last trade. Everything is built from
// the strategy's view and the journal's trades — never a Deribit call.

const (
	// SummaryMonths is how many calendar months the income table covers,
	// the current one included.
	SummaryMonths = 6
	// WorstCaseSigmas is the size of the modelled one-day move, in daily
	// standard deviations of DVOL.
	WorstCaseSigmas = 3.0
	// WorstCaseModel names the model in the payload, so nobody reads it as a
	// quote.
	WorstCaseModel = "model: Black–Scholes on each leg's current IV (DVOL when missing), spot moved ±3 daily σ (σ = DVOL ÷ √365) over one day"
)

// Summary is the /api/summary payload.
type Summary struct {
	AsOf        time.Time `json:"as_of"`
	StrategyID  string    `json:"strategy_id"`
	Underlying  string    `json:"underlying"`
	Environment string    `json:"environment"` // live | testnet
	Spot        float64   `json:"spot"`        // USD
	// StopLossMultiple: a leg is bought back once it has lost this multiple
	// of its premium (stop_loss_multiplier) — the worst-case page's words.
	StopLossMultiple float64 `json:"stop_loss_multiple"`
	// DeltaExitThreshold (delta_exit_threshold) and RepairCooldownHours
	// (repair_cooldown_hours): the "How it works" page's words (#322).
	DeltaExitThreshold  float64        `json:"delta_exit_threshold"`
	RepairCooldownHours int            `json:"repair_cooldown_hours"`
	Holding             HoldingSummary `json:"holding"`
	Income              IncomeSummary  `json:"income"`
	WorstCase           WorstCase      `json:"worst_case"`
	LastAction          *LastAction    `json:"last_action"` // null before the first trade
	// Strangle is the nearest two-legged strangle explained (#322); null when
	// none is held.
	Strangle *HowItWorks `json:"strangle"`
}

// HoldingSummary is what the bot holds now.
type HoldingSummary struct {
	Strangles     int        `json:"strangles"`
	Legs          int        `json:"legs"`
	Calls         int        `json:"calls"`
	Puts          int        `json:"puts"`
	NotionalCoin  float64    `json:"notional_coin"` // Σ contracts (one coin each)
	NotionalUSD   float64    `json:"notional_usd"`  // notional_coin × spot
	NearestExpiry *time.Time `json:"nearest_expiry"`
	NearestDTE    *float64   `json:"nearest_dte"` // days, fractional
	// Nearest is the short strikes on the nearest expiry, for the phone
	// overview's range bar (#320); null with nothing held.
	Nearest *NearestStrangle `json:"nearest"`
	Coin    string           `json:"coin"`
}

// NearestStrangle is the band the book is short on its nearest expiry: the
// highest short put and the lowest short call there (the tightest pair when
// several strangles share the expiry). A side with no short leg is null.
type NearestStrangle struct {
	Expiry     time.Time `json:"expiry"`
	PutStrike  *float64  `json:"put_strike"`
	CallStrike *float64  `json:"call_strike"`
}

// IncomeSummary is realised P&L per calendar month (net of fees, from the
// journal's closes) and the unrealised P&L of the open book.
type IncomeSummary struct {
	Coin           string        `json:"coin"`
	Months         []MonthIncome `json:"months"` // oldest first, the current month last
	ThisMonth      MonthIncome   `json:"this_month"`
	UnrealisedCoin float64       `json:"unrealised_coin"`
	UnrealisedUSD  float64       `json:"unrealised_usd"` // at today's spot
	Basis          string        `json:"basis"`
	// Summary is the months in a few figures, with the P&L history's worst
	// week (/api/income only, #319; null in /api/summary).
	Summary *IncomeStats `json:"summary"`
}

// MonthIncome is one calendar month (UTC) of realised P&L.
type MonthIncome struct {
	Month        string  `json:"month"` // YYYY-MM
	RealisedCoin float64 `json:"realised_coin"`
	RealisedUSD  float64 `json:"realised_usd"` // each close at its own spot
	Closes       int     `json:"closes"`
	// Where the result came from (#319), from the same closes:
	// premium − buyback − fees = realised, in coin and in USD (each close
	// at the spot the journal valued its P&L at).
	PremiumCoin float64 `json:"premium_coin"` // paid to you
	BuybackCoin float64 `json:"buyback_coin"` // paid to buy back (a cost)
	FeesCoin    float64 `json:"fees_coin"`    // exchange fees (a cost)
	PremiumUSD  float64 `json:"premium_usd"`
	BuybackUSD  float64 `json:"buyback_usd"`
	FeesUSD     float64 `json:"fees_usd"`
	// From the P&L history (/api/income only; null when not recorded): the
	// coin's recorded spot first → last in the month, and what the account's
	// equity (coin, at the month's start) gained or lost in USD by just
	// holding the coin. Held in coin it neither gains nor loses.
	SpotStart  *float64 `json:"spot_start"`
	SpotEnd    *float64 `json:"spot_end"`
	SpotDays   *float64 `json:"spot_days"` // days between the two spots
	HoldUSD    *float64 `json:"hold_usd"`
	HoldReason string   `json:"hold_reason,omitempty"` // why hold_usd is null
}

// WorstCase is the modelled loss of the book for a one-day move each way.
type WorstCase struct {
	Model     string    `json:"model"`
	Available bool      `json:"available"`
	Reason    string    `json:"reason,omitempty"` // why not available
	Sigmas    float64   `json:"sigmas"`
	DVOL      float64   `json:"dvol"`
	MovePct   float64   `json:"move_pct"` // the move, % of spot
	Coin      string    `json:"coin"`
	Down      MoveShock `json:"down"`
	Up        MoveShock `json:"up"`
	// Scenarios: the book after each of ScenarioSigmas' one-day moves, worst
	// fall first (#318); empty when not available.
	Scenarios []Scenario `json:"scenarios"`
}

// ScenarioSigmas are the one-day moves of the worst-case page, in daily σ.
var ScenarioSigmas = []float64{-3, -2, 0, 2, 3}

// Scenario is the book repriced after a one-day move of Sigmas daily σ.
// Results are signed: + is a gain, − a loss. The 0σ row is one day of time
// decay (theta), a gain for a short book.
type Scenario struct {
	Sigmas      float64  `json:"sigmas"`
	MovePct     float64  `json:"move_pct"` // signed, % of spot
	Spot        float64  `json:"spot"`     // USD after the move
	ResultCoin  float64  `json:"result_coin"`
	ResultUSD   float64  `json:"result_usd"`
	PctOfEquity *float64 `json:"pct_of_equity"` // signed; null without the equity
}

// MoveShock is the book repriced after one move. A positive loss is money lost.
type MoveShock struct {
	Spot        float64  `json:"spot"` // USD after the move
	LossCoin    float64  `json:"loss_coin"`
	LossUSD     float64  `json:"loss_usd"`
	PctOfEquity *float64 `json:"pct_of_equity"` // null without the equity
}

// LastAction is the latest open or close in the journal, in plain words.
type LastAction struct {
	At         time.Time `json:"at"`
	Kind       string    `json:"kind"` // open | close
	Instrument string    `json:"instrument"`
	OptionType string    `json:"option_type"`
	Qty        float64   `json:"qty"`
	Rule       string    `json:"rule"`   // the journal's reason label
	Reason     string    `json:"reason"` // plain words
	Detail     string    `json:"detail,omitempty"`
	PnLCoin    *float64  `json:"pnl_coin,omitempty"` // closes only
	PnLUSD     *float64  `json:"pnl_usd,omitempty"`
	// Spot is the coin's index price at the action (USD), for "the coin's
	// move since" on the phone overview (#320); omitted when not journaled.
	Spot *float64 `json:"spot,omitempty"`
}

// BuildSummary is the pure core of /api/summary.
func BuildSummary(v strategy.View, trades []orders.Trade, now time.Time) Summary {
	coin, spot := v.Underlying, v.Market.Spot
	sum := Summary{
		AsOf: now, StrategyID: v.StrategyID, Underlying: coin, Environment: v.Environment, Spot: spot,
		StopLossMultiple:    v.StopLossMultiple,
		DeltaExitThreshold:  v.DeltaExitThreshold,
		RepairCooldownHours: v.RepairCooldownHours,
		Holding:             holdingOf(v.Strangles, spot, coin, now),
		Income:              incomeOfView(v, trades, now),
		WorstCase:           WorstCaseOf(v.Strangles, spot, v.Market.DVOL, v.Account.Equity, now),
		Strangle:            HowItWorksOf(v.Strangles, spot, v.Market.DVOL, v.DeltaExitThreshold, now),
	}
	sum.WorstCase.Coin = coin
	if n := len(trades); n > 0 {
		la := lastActionOf(trades[n-1])
		sum.LastAction = &la
	}
	return sum
}

// incomeOfView is the income table of the journal's closes plus the open
// book's unrealised P&L from the view.
func incomeOfView(v strategy.View, trades []orders.Trade, now time.Time) IncomeSummary {
	inc := IncomeOf(trades, now, SummaryMonths)
	inc.Coin = v.Underlying
	if n := len(v.PnL); n > 0 {
		inc.UnrealisedCoin = v.PnL[n-1].Unrealised // last row = strategy total
		inc.UnrealisedUSD = inc.UnrealisedCoin * v.Market.Spot
	}
	return inc
}

func holdingOf(strangles []strategy.StrangleView, spot float64, coin string, now time.Time) HoldingSummary {
	h := HoldingSummary{Strangles: len(strangles), Coin: coin}
	for _, st := range strangles {
		for _, l := range st.Legs {
			h.Legs++
			if l.OptionType == "call" {
				h.Calls++
			} else {
				h.Puts++
			}
			h.NotionalCoin += l.Qty
			if !l.Expiry.IsZero() && (h.NearestExpiry == nil || l.Expiry.Before(*h.NearestExpiry)) {
				exp := l.Expiry
				h.NearestExpiry = &exp
			}
		}
	}
	h.NotionalUSD = h.NotionalCoin * spot
	if h.NearestExpiry != nil {
		dte := h.NearestExpiry.Sub(now).Hours() / 24
		h.NearestDTE = &dte
		h.Nearest = nearestOf(strangles, *h.NearestExpiry)
	}
	return h
}

// nearestOf is the band of short strikes on expiry: the highest put and the
// lowest call (longs are not part of the band).
func nearestOf(strangles []strategy.StrangleView, expiry time.Time) *NearestStrangle {
	n := &NearestStrangle{Expiry: expiry}
	for _, st := range strangles {
		for _, l := range st.Legs {
			if !l.Expiry.Equal(expiry) || l.Side == orders.DirectionBuy {
				continue
			}
			strike := l.Strike
			switch {
			case l.OptionType == "call" && (n.CallStrike == nil || strike < *n.CallStrike):
				n.CallStrike = &strike
			case l.OptionType == "put" && (n.PutStrike == nil || strike > *n.PutStrike):
				n.PutStrike = &strike
			}
		}
	}
	return n
}

// IncomeOf sums the journal's closes per UTC calendar month over the last
// months months (the current one included), oldest first; months without a
// close are zero.
func IncomeOf(trades []orders.Trade, now time.Time, months int) IncomeSummary {
	inc := IncomeSummary{Basis: "realised: journal closes, net of fees; unrealised: open book at its marks"}
	now = now.UTC()
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1-months, 0)
	idx := map[string]int{}
	for i := 0; i < months; i++ {
		m := first.AddDate(0, i, 0).Format("2006-01")
		idx[m] = i
		inc.Months = append(inc.Months, MonthIncome{Month: m})
	}
	for _, t := range trades {
		if t.Kind != "close" {
			continue
		}
		i, ok := idx[t.At.UTC().Format("2006-01")]
		if !ok {
			continue
		}
		m := &inc.Months[i]
		m.RealisedCoin += t.PnL
		m.RealisedUSD += t.PnLUSD
		m.Closes++
		premium, buyback, fees := CloseParts(t)
		spot := CloseSpot(t)
		m.PremiumCoin += premium
		m.BuybackCoin += buyback
		m.FeesCoin += fees
		m.PremiumUSD += premium * spot
		m.BuybackUSD += buyback * spot
		m.FeesUSD += fees * spot
	}
	if months > 0 {
		inc.ThisMonth = inc.Months[months-1]
	}
	return inc
}

// WorstCaseOf reprices the book after a one-day move of ±WorstCaseSigmas
// daily σ (from DVOL, %), each leg at its own IV (DVOL when it has none).
// The loss is model against model — the book now and after the move — so
// the spread between mark and model does not count as a loss. loss_coin is
// the USD loss in coin at the moved spot; equity is in coin.
func WorstCaseOf(strangles []strategy.StrangleView, spot, dvol, equity float64, now time.Time) WorstCase {
	wc := WorstCase{Model: WorstCaseModel, Sigmas: WorstCaseSigmas, DVOL: dvol, Scenarios: []Scenario{}}
	switch {
	case spot <= 0:
		wc.Reason = "no spot price"
		return wc
	case dvol <= 0:
		wc.Reason = "no DVOL reading"
		return wc
	}
	sigma := dvol / 100 / math.Sqrt(365) // one daily σ, as a fraction of spot
	move := WorstCaseSigmas * sigma
	wc.Available, wc.MovePct = true, move*100
	wc.Down = shock(strangles, spot, spot*(1-move), dvol, equity, now)
	wc.Up = shock(strangles, spot, spot*(1+move), dvol, equity, now)
	for _, n := range ScenarioSigmas {
		ms := shock(strangles, spot, spot*(1+n*sigma), dvol, equity, now)
		sc := Scenario{Sigmas: n, MovePct: n * sigma * 100, Spot: ms.Spot, ResultCoin: -ms.LossCoin, ResultUSD: -ms.LossUSD}
		if ms.PctOfEquity != nil {
			pct := -*ms.PctOfEquity
			sc.PctOfEquity = &pct
		}
		wc.Scenarios = append(wc.Scenarios, sc)
	}
	return wc
}

func shock(strangles []strategy.StrangleView, spot, moved, dvol, equity float64, now time.Time) MoveShock {
	const day = 1.0 / 365
	ms := MoveShock{Spot: moved}
	for _, st := range strangles {
		for _, l := range st.Legs {
			iv := l.Greeks.IV
			if iv <= 0 {
				iv = dvol / 100
			}
			t := l.Expiry.Sub(now).Hours() / 24 / 365
			change := l.Qty * (BlackScholesUSD(l.OptionType, moved, l.Strike, t-day, iv) - BlackScholesUSD(l.OptionType, spot, l.Strike, t, iv))
			if l.Side == orders.DirectionBuy {
				change = -change // a long gains what a short loses
			}
			ms.LossUSD += change
		}
	}
	ms.LossCoin = ms.LossUSD / moved
	if equity > 0 {
		pct := ms.LossCoin / equity * 100
		ms.PctOfEquity = &pct
	}
	return ms
}

// BlackScholesUSD is the Black–Scholes value of one option in USD (zero
// rate), its intrinsic value at or after expiry.
func BlackScholesUSD(optType string, spot, strike, years, sigma float64) float64 {
	if years <= 0 || sigma <= 0 {
		if optType == "call" {
			return math.Max(spot-strike, 0)
		}
		return math.Max(strike-spot, 0)
	}
	n := func(x float64) float64 { return 0.5 * math.Erfc(-x/math.Sqrt2) }
	sq := sigma * math.Sqrt(years)
	d1 := (math.Log(spot/strike) + 0.5*sigma*sigma*years) / sq
	d2 := d1 - sq
	if optType == "call" {
		return spot*n(d1) - strike*n(d2)
	}
	return strike*n(-d2) - spot*n(-d1)
}

func lastActionOf(t orders.Trade) LastAction {
	la := LastAction{At: t.At, Kind: t.Kind, Instrument: t.Instrument, OptionType: t.OptionType, Qty: t.Qty,
		Rule: t.Reason, Reason: PlainReason(t), Detail: t.Detail}
	if t.Kind == "close" {
		pnl, usd := t.PnL, t.PnLUSD
		la.PnLCoin, la.PnLUSD = &pnl, &usd
	}
	if t.Spot > 0 {
		spot := t.Spot
		la.Spot = &spot
	}
	return la
}
