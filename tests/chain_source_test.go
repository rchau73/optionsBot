package tests

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/exchange/deribit"
	"optionsbot/internal/gateway"
	"optionsbot/internal/marketdata"
)

// Scenario (#710): same requests, same order — method, parameters and lane
// sent to the gateway through the chain source are the ones the manager sent.
func TestChainSource_SameRequestsSameOrder(t *testing.T) {
	gw := newFakeFeedGateway()
	gw.results["public/get_instruments"] = instrumentsJSON(time.Now(), 24)
	gw.results["public/get_volatility_index_data"] = `{"data":[]}`
	cfg := newMarketCfg()
	before := time.Now()
	startManager(t, gw)

	if len(gw.calls) != 2 {
		t.Fatalf("want 2 requests, got %+v", gw.calls)
	}
	chain := feedCall{"public/get_instruments", map[string]any{
		"currency": "BTC", "kind": "option", "expired": false,
	}, gateway.PriorityLow}
	if !reflect.DeepEqual(gw.calls[0], chain) {
		t.Errorf("first request = %+v, want %+v", gw.calls[0], chain)
	}

	vol := gw.calls[1]
	params, ok := vol.params.(map[string]any)
	if !ok {
		t.Fatalf("volatility params: %T", vol.params)
	}
	end, _ := params["end_timestamp"].(int64)
	if end < before.UnixMilli() {
		t.Errorf("end_timestamp %d is before the start of the run", end)
	}
	days := cfg.IVPercentileWindow + marketdata.DVOLHistoryExtraDays
	want := feedCall{"public/get_volatility_index_data", map[string]any{
		"currency":        "BTC",
		"start_timestamp": time.UnixMilli(end).AddDate(0, 0, -days-1).UnixMilli(),
		"end_timestamp":   end,
		"resolution":      "1D",
	}, gateway.PriorityLow}
	if !reflect.DeepEqual(vol, want) {
		t.Errorf("second request = %+v, want %+v", vol, want)
	}
}

// Scenario (#710): subscription channels are unchanged — exact strings, in
// the same order, at start and from Track.
func TestChainSource_SubscriptionChannelsAreUnchanged(t *testing.T) {
	gw := newFakeFeedGateway()
	gw.results["public/get_instruments"] = instrumentsJSON(time.Now(), 17, 24)
	m := startManager(t, gw)

	want := []string{
		"deribit_volatility_index.btc_usd",
		"deribit_price_index.btc_usd",
		"ticker.BTC-D24-C.100ms",
		"ticker.BTC-D24-P.100ms",
	}
	if got := gw.subscriptions(); !reflect.DeepEqual(got, want) {
		t.Errorf("start channels = %q, want %q", got, want)
	}

	if err := m.Track(context.Background(), []string{"BTC-D17-P"}); err != nil {
		t.Fatal(err)
	}
	want = append(want, "ticker.BTC-D17-P.100ms")
	if got := gw.subscriptions(); !reflect.DeepEqual(got, want) {
		t.Errorf("channels after Track = %q, want %q", got, want)
	}
}

func TestChainSource_ChannelNames(t *testing.T) {
	s := deribit.NewChainSource(newFakeFeedGateway())
	if got := s.VolIndexChannel("ETH"); got != "deribit_volatility_index.eth_usd" {
		t.Errorf("volatility channel: %q", got)
	}
	if got := s.IndexChannel("ETH"); got != "deribit_price_index.eth_usd" {
		t.Errorf("index channel: %q", got)
	}
	if got := s.TickerChannel("BTC-4OCT26-60000-P"); got != "ticker.BTC-4OCT26-60000-P.100ms" {
		t.Errorf("ticker channel: %q", got)
	}

	for _, c := range []struct {
		channel string
		kind    marketdata.ChannelKind
		name    string // "" = not a parsable ticker channel
	}{
		{"deribit_volatility_index.btc_usd", marketdata.ChannelVolIndex, ""},
		{"deribit_price_index.btc_usd", marketdata.ChannelIndex, ""},
		{"ticker.BTC-4OCT26-60000-P.100ms", marketdata.ChannelTicker, "BTC-4OCT26-60000-P"},
		{"ticker.BTC-4OCT26-60000-P.raw", marketdata.ChannelTicker, "BTC-4OCT26-60000-P"},
		{"ticker.BTC-A.B.100ms", marketdata.ChannelTicker, "BTC-A.B"}, // the interval is the last part
		{"ticker.BTC", marketdata.ChannelTicker, ""},                  // no interval
		{"ticker..100ms", marketdata.ChannelTicker, ""},               // no instrument
		{"heartbeat", marketdata.ChannelOther, ""},
		{"", marketdata.ChannelOther, ""},
	} {
		if got := s.ChannelKind(c.channel); got != c.kind {
			t.Errorf("ChannelKind(%q) = %v, want %v", c.channel, got, c.kind)
		}
		name, ok := s.TickerInstrument(c.channel)
		if ok != (c.name != "") || name != c.name {
			t.Errorf("TickerInstrument(%q) = %q, %v, want %q", c.channel, name, ok, c.name)
		}
	}
}

// The chain's empty fields get Deribit's defaults; a set field is kept.
func TestChainSource_InstrumentsFillDefaults(t *testing.T) {
	gw := newFakeFeedGateway()
	gw.results["public/get_instruments"] = `[
		{"instrument_name":"BTC-A","strike":100000,"expiration_timestamp":1700000000000,"option_type":"put"},
		{"instrument_name":"BTC-B","tick_size":0.0005,"min_trade_amount":1}
	]`
	got, err := deribit.NewChainSource(gw).Instruments(context.Background(), "BTC")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 instruments, got %d", len(got))
	}
	a := got[0]
	if a.Name != "BTC-A" || a.Underlying != "BTC" || a.Strike != 100000 || a.OptionType != "put" ||
		!a.Expiry.Equal(time.UnixMilli(1700000000000)) || a.TickSize != 0.0001 || a.MinTradeAmount != 0.1 {
		t.Errorf("first instrument: %+v", a)
	}
	if a.HasQuote() {
		t.Error("an instrument from the chain has no quote yet")
	}
	if b := got[1]; b.TickSize != 0.0005 || b.MinTradeAmount != 1 {
		t.Errorf("set fields must be kept: %+v", b)
	}
}

// A candle too short to hold a close is skipped; the others keep their day.
func TestChainSource_VolHistorySkipsCandlesWithoutAClose(t *testing.T) {
	gw := newFakeFeedGateway()
	gw.results["public/get_volatility_index_data"] = `{"data":[[1700000000000,1,2,3],[1700086400000,1,2,3,55.5],[]]}`
	got, err := deribit.NewChainSource(gw).VolHistory(context.Background(), "BTC", time.UnixMilli(1), time.UnixMilli(2))
	if err != nil {
		t.Fatal(err)
	}
	want := []marketdata.VolClose{{At: time.UnixMilli(1700086400000), Close: 55.5}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("closes = %+v, want %+v", got, want)
	}
}

func TestChainSource_ReadFailures(t *testing.T) {
	s := func(gw *fakeFeedGateway) *deribit.ChainSource { return deribit.NewChainSource(gw) }
	reads := map[string]func(gw *fakeFeedGateway) error{
		"public/get_instruments": func(gw *fakeFeedGateway) error {
			_, err := s(gw).Instruments(context.Background(), "BTC")
			return err
		},
		"public/get_volatility_index_data": func(gw *fakeFeedGateway) error {
			_, err := s(gw).VolHistory(context.Background(), "BTC", time.UnixMilli(1), time.UnixMilli(2))
			return err
		},
	}
	for method, read := range reads {
		refused := newFakeFeedGateway()
		boom := errors.New("boom")
		refused.errs[method] = boom
		if err := read(refused); !errors.Is(err, boom) {
			t.Errorf("%s refused: err = %v, want the gateway's", method, err)
		}

		garbled := newFakeFeedGateway()
		garbled.results[method] = `not json`
		if err := read(garbled); err == nil || !strings.Contains(err.Error(), "decode") {
			t.Errorf("%s garbled: err = %v, want a decode error", method, err)
		}
	}
}
