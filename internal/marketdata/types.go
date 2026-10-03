package marketdata

import "time"

// Greeks holds per-instrument option greeks.
type Greeks struct {
	Delta float64 `json:"delta"`
	Gamma float64 `json:"gamma"`
	Theta float64 `json:"theta"`
	Vega  float64 `json:"vega"`
	Rho   float64 `json:"rho"`
	IV    float64 `json:"iv"`
}

// TickSizeStep describes a price-dependent tick size tier as returned by Deribit.
type TickSizeStep struct {
	AbovePrice float64
	TickSize   float64
}

// Instrument captures a full snapshot of an options instrument.
type Instrument struct {
	Name            string
	Underlying      string
	Strike          float64
	Expiry          time.Time
	OptionType      string // "call" or "put"
	TickSize        float64
	TickSizeSteps   []TickSizeStep // price-dependent ticks; takes precedence over TickSize
	MinTradeAmount  float64        // exchange-enforced minimum order size (e.g. 0.1 BTC for BTC options)
	Bid             float64
	Ask             float64
	Mid             float64
	UnderlyingPrice float64
	Greeks          Greeks
	DVOLIndex       float64
	IVPercentile    float64
	UpdatedAt       time.Time // last ticker update; zero until the first one
}

// EffectiveTick returns the tick size that applies at the given price,
// using the instrument's tick_size_steps from Deribit if available.
func (i *Instrument) EffectiveTick(price float64) float64 {
	if len(i.TickSizeSteps) > 0 {
		tick := i.TickSizeSteps[0].TickSize
		for _, step := range i.TickSizeSteps {
			if price >= step.AbovePrice {
				tick = step.TickSize
			}
		}
		return tick
	}
	if i.TickSize > 0 {
		return i.TickSize
	}
	return 0.0001
}

// Tick is one row of historical option data replayed by the backtest feed.
type Tick struct {
	Timestamp       time.Time
	Instrument      string
	Underlying      string
	UnderlyingPrice float64
	Strike          float64
	Expiry          time.Time
	OptionType      string
	Bid             float64
	Ask             float64
	Mid             float64
	Greeks          Greeks
	DVOLIndex       float64
	IVPercentile    float64
}

// HasQuote reports whether a ticker has ever updated the instrument. Until
// then its prices and greeks are zero and must not be used as a mark.
func (i *Instrument) HasQuote() bool { return !i.UpdatedAt.IsZero() }
