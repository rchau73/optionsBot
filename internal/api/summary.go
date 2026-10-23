package api

import (
	"math"
	"net/http"
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
	AsOf       time.Time      `json:"as_of"`
	StrategyID string         `json:"strategy_id"`
	Underlying string         `json:"underlying"`
	Spot       float64        `json:"spot"` // USD
	Holding    HoldingSummary `json:"holding"`
	Income     IncomeSummary  `json:"income"`
	WorstCase  WorstCase      `json:"worst_case"`
	LastAction *LastAction    `json:"last_action"` // null before the first trade
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
	Coin          string     `json:"coin"`
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
}

// MonthIncome is one calendar month (UTC) of realised P&L.
type MonthIncome struct {
	Month        string  `json:"month"` // YYYY-MM
	RealisedCoin float64 `json:"realised_coin"`
	RealisedUSD  float64 `json:"realised_usd"` // each close at its own spot
	Closes       int     `json:"closes"`
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
}

// summary serves GET /api/summary.
func (s *Server) summary(w http.ResponseWriter, _ *http.Request) {
	var trades []orders.Trade
	if ts, ok := s.events.(TradeSource); ok {
		trades = ts.Trades(0, 0)
	}
	writeJSON(w, BuildSummary(s.strategy.View(), trades, time.Now()))
}

// BuildSummary is the pure core of /api/summary.
func BuildSummary(v strategy.View, trades []orders.Trade, now time.Time) Summary {
	coin, spot := v.Underlying, v.Market.Spot
	sum := Summary{
		AsOf: now, StrategyID: v.StrategyID, Underlying: coin, Spot: spot,
		Holding:   holdingOf(v.Strangles, spot, coin, now),
		Income:    IncomeOf(trades, now, SummaryMonths),
		WorstCase: WorstCaseOf(v.Strangles, spot, v.Market.DVOL, v.Account.Equity, now),
	}
	sum.Income.Coin, sum.WorstCase.Coin = coin, coin
	if n := len(v.PnL); n > 0 {
		sum.Income.UnrealisedCoin = v.PnL[n-1].Unrealised // last row = strategy total
		sum.Income.UnrealisedUSD = sum.Income.UnrealisedCoin * spot
	}
	if n := len(trades); n > 0 {
		la := lastActionOf(trades[n-1])
		sum.LastAction = &la
	}
	return sum
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
	}
	return h
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
		inc.Months[i].RealisedCoin += t.PnL
		inc.Months[i].RealisedUSD += t.PnLUSD
		inc.Months[i].Closes++
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
	wc := WorstCase{Model: WorstCaseModel, Sigmas: WorstCaseSigmas, DVOL: dvol}
	switch {
	case spot <= 0:
		wc.Reason = "no spot price"
		return wc
	case dvol <= 0:
		wc.Reason = "no DVOL reading"
		return wc
	}
	move := WorstCaseSigmas * dvol / 100 / math.Sqrt(365)
	wc.Available, wc.MovePct = true, move*100
	wc.Down = shock(strangles, spot, spot*(1-move), dvol, equity, now)
	wc.Up = shock(strangles, spot, spot*(1+move), dvol, equity, now)
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
	return la
}
