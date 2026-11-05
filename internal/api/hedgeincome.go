package api

import (
	"math"
	"time"

	"optionsbot/internal/hedge"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// The futures hedge in the investor's figures (#665). The experiment moves the
// margin balance through the perpetual, so the worst case and the income count
// it: the worst case adds its inverse P&L at each moved price, and the income
// gets a "Futures hedge" source per month.
//
// The journal is the history (hedge_fill and hedge_closed lines): the realised
// P&L is rebuilt from the fills with the benchmark's own book (hedge.PerpBook).
// A line journals Deribit's running funding figure, so a line's funding is the
// change since the line before, and the open position's latest reading books
// the rest in the current month. A perpetual that goes flat starts again at 0.

// perpFlatUSD is the size under which the perpetual counts as closed: the
// strategy's own test.
const perpFlatUSD = 0.5

// HedgeTradeReason marks a futures hedge flow given to IncomeStartOf as a
// trade, so it counts in the result and not among the closes.
const HedgeTradeReason = "futures_hedge"

// PerpFillSource is the journal's record of the perpetual's trades (orders.Logger).
type PerpFillSource interface {
	PerpFills() []orders.PerpFill
}

// FuturesHedge is what the summary and the income need of the experiment:
// the journal's fills, oldest first, and the perpetual open now (size signed).
type FuturesHedge struct {
	Fills       []orders.PerpFill
	SizeUSD     float64
	AvgPrice    float64
	FundingCoin float64
}

// HedgeFlow is what one moment added to the hedge's result, in coin.
type HedgeFlow struct {
	At           time.Time
	Spot         float64 // USD, the price the flow is valued at
	Fills        int
	RealisedCoin float64
	FeesCoin     float64
	FundingCoin  float64
}

// NetCoin is the flow's result: realised − fees + funding.
func (f HedgeFlow) NetCoin() float64 { return f.RealisedCoin - f.FeesCoin + f.FundingCoin }

// Flows walks the fills through the perpetual's book, oldest first. The
// last flow, at now and at spot, is the funding of the open position since
// the last fill.
func (h FuturesHedge) Flows(spot float64, now time.Time) []HedgeFlow {
	var flows []HedgeFlow
	var book hedge.PerpBook
	reading := 0.0
	for _, f := range h.Fills {
		price := f.Price
		if price <= 0 {
			price = f.Spot
		}
		usd := f.FilledUSD
		if f.Side == hedge.SideSell {
			usd = -usd
		}
		before := book.Realised
		book.Book(usd, price, f.FeeCoin)
		flows = append(flows, HedgeFlow{At: f.At, Spot: price, Fills: 1, RealisedCoin: book.Realised - before,
			FeesCoin: f.FeeCoin, FundingCoin: f.FundingCoin - reading})
		reading = f.FundingCoin
		if math.Abs(book.SizeUSD) < perpFlatUSD {
			reading = 0
		}
	}
	if math.Abs(h.SizeUSD) >= perpFlatUSD && h.FundingCoin != reading {
		flows = append(flows, HedgeFlow{At: now, Spot: spot, FundingCoin: h.FundingCoin - reading})
	}
	return flows
}

// IncomeWithHedge adds the flows to their months (flows outside the months
// shown are left out, as closes are): each month's "Futures hedge" source
// and its total. No flows leaves inc as it is.
func IncomeWithHedge(inc IncomeSummary, flows []HedgeFlow) IncomeSummary {
	if len(flows) == 0 {
		return inc
	}
	months := make([]MonthIncome, len(inc.Months))
	copy(months, inc.Months)
	inc.Months = months
	idx := map[string]int{}
	for i, m := range months {
		idx[m.Month] = i
	}
	for _, f := range flows {
		i, ok := idx[f.At.UTC().Format("2006-01")]
		if !ok {
			continue
		}
		m := &months[i]
		net := f.NetCoin()
		m.HedgeFills += f.Fills
		m.HedgeRealisedCoin += f.RealisedCoin
		m.HedgeFeesCoin += f.FeesCoin
		m.HedgeFundingCoin += f.FundingCoin
		m.HedgeCoin += net
		m.HedgeUSD += net * f.Spot
		m.RealisedCoin += net
		m.RealisedUSD += net * f.Spot
	}
	if n := len(months); n > 0 {
		inc.ThisMonth = months[n-1]
	}
	inc.Basis += "; futures hedge: journal fills, fees and funding"
	return inc
}

// HedgeTrades are the flows as closes for the "% of start" view, which
// measures any close-kind trade in its period.
func HedgeTrades(flows []HedgeFlow) []orders.Trade {
	out := make([]orders.Trade, 0, len(flows))
	for _, f := range flows {
		out = append(out, orders.Trade{At: f.At, Kind: "close", Reason: HedgeTradeReason, PnL: f.NetCoin()})
	}
	return out
}

// WorstCaseWithPerp adds an open perpetual (size signed, USD) to each move of
// wc and keeps the options alone in wc.Hedge. No perpetual, or none with an
// average price, leaves wc as it is: options only, and its model says so.
func WorstCaseWithPerp(wc WorstCase, sizeUSD, avgPrice, equity float64) WorstCase {
	if !wc.Available || math.Abs(sizeUSD) < perpFlatUSD || avgPrice <= 0 {
		return wc
	}
	wc.Model = WorstCaseHedgedModel
	wc.Hedge = &WorstCaseHedge{SizeUSD: sizeUSD, AvgPrice: avgPrice, OptionsDown: wc.Down, OptionsUp: wc.Up}
	wc.Down = withPerp(wc.Down, sizeUSD, avgPrice, equity)
	wc.Up = withPerp(wc.Up, sizeUSD, avgPrice, equity)
	scenarios := make([]Scenario, len(wc.Scenarios))
	for i, sc := range wc.Scenarios {
		gain := hedge.PerpPnL(sizeUSD, avgPrice, sc.Spot)
		sc.ResultCoin += gain
		sc.ResultUSD += gain * sc.Spot
		if equity > 0 {
			sc.PctOfEquity = ptr(sc.ResultCoin / equity * 100)
		}
		scenarios[i] = sc
	}
	wc.Scenarios = scenarios
	return wc
}

// withPerp takes the perpetual's gain at the moved spot off a shock's loss.
func withPerp(ms MoveShock, sizeUSD, avgPrice, equity float64) MoveShock {
	gain := hedge.PerpPnL(sizeUSD, avgPrice, ms.Spot)
	ms.LossCoin -= gain
	ms.LossUSD -= gain * ms.Spot
	if equity > 0 {
		ms.PctOfEquity = ptr(ms.LossCoin / equity * 100)
	}
	return ms
}

// BuildHedgedSummary is BuildSummary with the futures hedge counted.
func BuildHedgedSummary(v strategy.View, trades []orders.Trade, h FuturesHedge, now time.Time) Summary {
	sum := BuildSummary(v, trades, now)
	sum.Income = IncomeWithHedge(sum.Income, h.Flows(v.Market.Spot, now))
	sum.WorstCase = WorstCaseWithPerp(sum.WorstCase, h.SizeUSD, h.AvgPrice, v.Account.Equity)
	return sum
}
