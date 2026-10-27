package strategy

import (
	"time"

	"optionsbot/internal/gex"
	"optionsbot/internal/orders"
)

// CandleDays is how many completed daily candles the view publishes for the
// monitor's regime chart (#394); today's live candle comes on top.
const CandleDays = 60

// Candle is one UTC day of the perpetual's price for the monitor's regime
// chart: open, high, low and close. Today's candle is the live one — its
// close is the last price and its high and low follow the pushed prices.
type Candle struct {
	Date  string  `json:"date"` // UTC day, YYYY-MM-DD
	Open  float64 `json:"open"`
	High  float64 `json:"high"`
	Low   float64 `json:"low"`
	Close float64 `json:"close"`
}

// SMAView is the view's "sma": the SMA trend's levels and the daily candles
// they are read from (last CandleDays days + today's live candle, oldest
// first), for the monitor's regime chart (#394).
type SMAView struct {
	SMALevels
	Candles []Candle `json:"candles"`
}

// candleFrom is a seeded day as the live candle of day: a day whose open,
// high or low the history did not give is flat at its close.
func candleFrom(c orders.DailyClose, day time.Time) orders.DailyClose {
	c.Date = day
	if c.Open <= 0 || c.High <= 0 || c.Low <= 0 {
		c.Open, c.High, c.Low = c.Close, c.Close, c.Close
	}
	return c
}

// extendCandle moves the live candle to price: a new high or low, the close.
func extendCandle(c orders.DailyClose, price float64) orders.DailyClose {
	if price <= 0 {
		return c
	}
	if c.Open <= 0 {
		c.Open, c.High, c.Low = price, price, price
	}
	c.High = max(c.High, price)
	c.Low = min(c.Low, price)
	c.Close = price
	return c
}

// committedCandle is the daily close the day roll commits for day: its last
// price as Close (what the trend reads, unchanged) with the live candle's
// open, high and low when the live candle is that day's.
func committedCandle(live orders.DailyClose, day time.Time, close float64) orders.DailyClose {
	if !live.Date.Equal(day) {
		return orders.DailyClose{Date: day, Close: close}
	}
	live.Close = close
	live.High = max(live.High, close)
	live.Low = min(live.Low, close)
	return live
}

// CandlesOf is the regime chart's candles, pure: the last days completed
// daily closes plus the live candle, oldest first, one per UTC day (a later
// entry of a day replaces an earlier one — the live candle replaces a seeded
// partial of today). A day without an open, high or low is drawn flat at its
// close; a day without a close is left out.
func CandlesOf(closes []orders.DailyClose, live orders.DailyClose, days int) []Candle {
	all := closes
	if !live.Date.IsZero() && live.Close > 0 {
		all = append(append([]orders.DailyClose(nil), closes...), live)
	}
	out := make([]Candle, 0, days+1)
	for _, c := range all {
		if c.Close <= 0 {
			continue
		}
		c = candleFrom(c, c.Date)
		k := Candle{Date: c.Date.UTC().Format(time.DateOnly), Open: c.Open, High: max(c.High, c.Open, c.Close), Low: min(c.Low, c.Open, c.Close), Close: c.Close}
		if n := len(out); n > 0 && out[n-1].Date == k.Date {
			out[n-1] = k
			continue
		}
		out = append(out, k)
	}
	if limit := days + 1; len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// Candles is CandlesOf on the monitor's history: the last CandleDays days and
// today's live candle. Call on the decision loop (publish).
func (g *GammaMonitor) Candles() []Candle {
	return CandlesOf(g.dailyCloses, g.live, CandleDays)
}

// gexAsOf is when snap was computed, nil without a snapshot or a time.
func gexAsOf(snap *gex.Snapshot) *time.Time {
	if snap == nil || snap.ComputedAt.IsZero() {
		return nil
	}
	t := snap.ComputedAt
	return &t
}

// MedianFlip is the median flip of the bias window (StableBiasDir) under
// DirectionBias, 0 otherwise or without a recorded flip. Call on the decision
// loop (publish).
func (g *GammaMonitor) MedianFlip() float64 {
	if !g.usesBias() {
		return 0
	}
	return g.medianFlip()
}
