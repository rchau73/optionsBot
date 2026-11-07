package deribit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"optionsbot/internal/exchange/transport"
	"optionsbot/internal/gateway"
	"optionsbot/internal/marketdata"
)

// Channel names of Deribit's subscription pushes.
const (
	volIndexChannelPrefix = "deribit_volatility_index."
	indexChannelPrefix    = "deribit_price_index."
	tickerChannelPrefix   = "ticker."
	tickerInterval        = "100ms" // Deribit accepts only 100ms and raw
)

// Applied when public/get_instruments leaves a field empty.
const (
	defaultTickSize       = 0.0001
	defaultMinTradeAmount = 0.1 // Deribit default for BTC/ETH options
)

// Compile-time check: this is the manager's source.
var _ marketdata.ChainSource = (*ChainSource)(nil)

// ChainSource reads the option chain and the DVOL history over one connection
// and names Deribit's market-data channels, for marketdata.Manager.
type ChainSource struct {
	gw transport.Caller
}

// NewChainSource reads through gw.
func NewChainSource(gw transport.Caller) *ChainSource { return &ChainSource{gw: gw} }

// instrumentInfo is the subset of public/get_instruments we use.
type instrumentInfo struct {
	InstrumentName string  `json:"instrument_name"`
	Strike         float64 `json:"strike"`
	ExpirationTS   int64   `json:"expiration_timestamp"`
	OptionType     string  `json:"option_type"`
	TickSize       float64 `json:"tick_size"`
	MinTradeAmount float64 `json:"min_trade_amount"`
	TickSizeSteps  []struct {
		AbovePrice float64 `json:"above_price"`
		TickSize   float64 `json:"tick_size"`
	} `json:"tick_size_steps"`
}

// Instruments returns the unexpired options of underlying, on the low-priority
// lane: the non-matching pool.
func (s *ChainSource) Instruments(ctx context.Context, underlying string) ([]*marketdata.Instrument, error) {
	resp, err := s.gw.Call(ctx, "public/get_instruments", map[string]any{
		"currency": underlying,
		"kind":     "option",
		"expired":  false,
	}, gateway.PriorityLow)
	if err != nil {
		return nil, err
	}
	var infos []instrumentInfo
	if err := json.Unmarshal(resp.Result, &infos); err != nil {
		return nil, fmt.Errorf("decode instruments: %w", err)
	}
	instruments := make([]*marketdata.Instrument, len(infos))
	for i, info := range infos {
		instruments[i] = newInstrument(underlying, info)
	}
	return instruments, nil
}

func newInstrument(underlying string, info instrumentInfo) *marketdata.Instrument {
	tickSize := info.TickSize
	if tickSize <= 0 {
		tickSize = defaultTickSize
	}
	minTA := info.MinTradeAmount
	if minTA <= 0 {
		minTA = defaultMinTradeAmount
	}
	steps := make([]marketdata.TickSizeStep, len(info.TickSizeSteps))
	for i, s := range info.TickSizeSteps {
		steps[i] = marketdata.TickSizeStep{AbovePrice: s.AbovePrice, TickSize: s.TickSize}
	}
	return &marketdata.Instrument{
		Name:           info.InstrumentName,
		Underlying:     underlying,
		Strike:         info.Strike,
		Expiry:         time.UnixMilli(info.ExpirationTS),
		OptionType:     info.OptionType,
		TickSize:       tickSize,
		TickSizeSteps:  steps,
		MinTradeAmount: minTA,
	}
}

// VolHistory returns DVOL's daily closes from from to to, on the low-priority
// lane. A candle without a close is skipped.
func (s *ChainSource) VolHistory(ctx context.Context, underlying string, from, to time.Time) ([]marketdata.VolClose, error) {
	resp, err := s.gw.Call(ctx, "public/get_volatility_index_data", map[string]any{
		"currency":        underlying,
		"start_timestamp": from.UnixMilli(),
		"end_timestamp":   to.UnixMilli(),
		"resolution":      "1D",
	}, gateway.PriorityLow)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data [][]float64 `json:"data"` // [timestamp, open, high, low, close]
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("decode volatility index data: %w", err)
	}
	closes := make([]marketdata.VolClose, 0, len(result.Data))
	for _, candle := range result.Data {
		if len(candle) < 5 {
			continue
		}
		closes = append(closes, marketdata.VolClose{At: time.UnixMilli(int64(candle[0])), Close: candle[4]})
	}
	return closes, nil
}

// VolIndexChannel is DVOL's channel, e.g. "deribit_volatility_index.btc_usd".
func (s *ChainSource) VolIndexChannel(underlying string) string {
	return volIndexChannelPrefix + strings.ToLower(underlying) + "_usd"
}

// IndexChannel is the price index's channel, e.g. "deribit_price_index.btc_usd".
func (s *ChainSource) IndexChannel(underlying string) string {
	return indexChannelPrefix + strings.ToLower(underlying) + "_usd"
}

// TickerChannel is an instrument's channel: "ticker.<name>.100ms".
func (s *ChainSource) TickerChannel(instrument string) string {
	return tickerChannelPrefix + instrument + "." + tickerInterval
}

// TickerInstrument extracts the instrument from "ticker.<name>.100ms".
func (s *ChainSource) TickerInstrument(channel string) (string, bool) {
	if !strings.HasPrefix(channel, tickerChannelPrefix) {
		return "", false
	}
	rest := strings.TrimPrefix(channel, tickerChannelPrefix)
	i := strings.LastIndex(rest, ".")
	if i <= 0 {
		return "", false
	}
	return rest[:i], true
}

// ChannelKind classifies a push by its channel's prefix.
func (s *ChainSource) ChannelKind(channel string) marketdata.ChannelKind {
	switch {
	case strings.HasPrefix(channel, volIndexChannelPrefix):
		return marketdata.ChannelVolIndex
	case strings.HasPrefix(channel, indexChannelPrefix):
		return marketdata.ChannelIndex
	case strings.HasPrefix(channel, tickerChannelPrefix):
		return marketdata.ChannelTicker
	}
	return marketdata.ChannelOther
}
