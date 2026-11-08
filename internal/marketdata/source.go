package marketdata

import (
	"context"
	"time"
)

// ChannelKind says what a subscription push carries.
type ChannelKind int

const (
	ChannelOther    ChannelKind = iota // not a channel the manager handles
	ChannelVolIndex                    // the volatility index
	ChannelIndex                       // the underlying's price index
	ChannelTicker                      // one instrument's quotes and greeks
)

// VolClose is one daily close of the volatility index.
type VolClose struct {
	At    time.Time
	Close float64
}

// ChainSource is what the manager asks of an exchange: the option chain, the
// volatility history and the names of its channels. It is declared here so
// that marketdata never imports internal/exchange; the implementation is
// deribit.ChainSource.
type ChainSource interface {
	// Instruments lists the live options of underlying, without quotes.
	Instruments(ctx context.Context, underlying string) ([]*Instrument, error)
	// VolHistory returns the daily closes of the volatility index in [from, to].
	VolHistory(ctx context.Context, underlying string, from, to time.Time) ([]VolClose, error)

	VolIndexChannel(underlying string) string
	IndexChannel(underlying string) string
	TickerChannel(instrument string) string
	// TickerInstrument extracts the instrument from a ticker channel.
	TickerInstrument(channel string) (string, bool)
	// ChannelKind classifies a push's channel.
	ChannelKind(channel string) ChannelKind
}
