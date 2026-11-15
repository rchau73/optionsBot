package tests

import (
	"math"
	"testing"
	"time"

	"optionsbot/internal/marketdata"
)

// #773: the strategy reads an instrument's expiry, strike and type from the
// chain, or — for a name the chain does not hold — from its market data's
// InstrumentFromName, never by parsing the name itself. These pin the
// fallback paths to the fields the parser gave before the change.

func TestInstrumentFromName(t *testing.T) {
	cases := map[string]struct {
		name string
		want marketdata.Instrument
		bad  bool
	}{
		"call": {name: "BTC-27NOV26-110000-C", want: marketdata.Instrument{Name: "BTC-27NOV26-110000-C", Underlying: "BTC",
			Expiry: time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC), Strike: 110000, OptionType: "call"}},
		"daily put, one-digit day": {name: "ETH-4OCT26-2500-P", want: marketdata.Instrument{Name: "ETH-4OCT26-2500-P", Underlying: "ETH",
			Expiry: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC), Strike: 2500, OptionType: "put"}},
		"fractional strike": {name: "XRP_USDC-27NOV26-2d5-C", want: marketdata.Instrument{Name: "XRP_USDC-27NOV26-2d5-C", Underlying: "XRP_USDC",
			Expiry: time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC), Strike: 2.5, OptionType: "call"}},
		"perpetual":    {name: "BTC-PERPETUAL", bad: true},
		"unknown type": {name: "BTC-27NOV26-110000-X", bad: true},
		"empty":        {name: "", bad: true},
	}
	for label, c := range cases {
		got, err := marketdata.InstrumentFromName(c.name)
		if c.bad {
			if err == nil {
				t.Errorf("%s: want an error, got %+v", label, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if got.Name != c.want.Name || got.Underlying != c.want.Underlying || !got.Expiry.Equal(c.want.Expiry) ||
			got.Strike != c.want.Strike || got.OptionType != c.want.OptionType || got.HasQuote() {
			t.Errorf("%s: got %+v, want %+v", label, got, c.want)
		}
		// The same fields as the parser the research tools keep.
		und, exp, strike, typ, _ := marketdata.ParseOptionName(c.name)
		if und != got.Underlying || !exp.Equal(got.Expiry) || strike != got.Strike || typ != got.OptionType {
			t.Errorf("%s: differs from ParseOptionName", label)
		}
	}
}

// Scenario (#773): orphan adoption is unchanged — an exchange short whose
// instrument is not in the chain is adopted with the expiry, strike and type
// its name gives.
func TestStrategy_AdoptedShortOutsideTheChainCarriesItsNameFields(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.market.mu.Lock()
	delete(f.market.instruments, f.put) // e.g. an expiry the chain no longer lists
	f.market.mu.Unlock()
	f.startRun()

	eventually(t, 2*time.Second, "both shorts in the book", func() bool { return len(f.state.AllPositions()) == 2 })
	p := f.position(f.put)
	want := f.expiry.Truncate(24 * time.Hour) // the name's date at 00:00 UTC
	if p == nil || p.Underlying != "BTC" || p.Strike != 90000 || p.OptionType != "put" || !p.Expiry.Equal(want) {
		t.Fatalf("adopted put = %+v, want BTC 90000 put expiring %s", p, want)
	}
	if c := f.position(f.call); c == nil || !c.Expiry.Equal(f.expiry) || c.Strike != 110000 {
		t.Errorf("the call in the chain keeps the chain's fields: %+v", c)
	}
}

// Scenario (#773): the pending booking is unchanged — an entry leg that fills
// after its instrument left the chain is booked with its name's strike and
// expiry.
func TestStrategy_EntryFillOutsideTheChainCarriesItsNameFields(t *testing.T) {
	f := newStrategyFixture(t)
	f.startRun()
	eventually(t, 2*time.Second, "both legs submitted", func() bool { return len(f.exch.sells()) == 2 })

	f.market.mu.Lock()
	delete(f.market.instruments, f.put)
	f.market.mu.Unlock()
	f.exch.fill(f.exch.orderIDFor(f.call, 0), 0.1, 0.021)
	f.exch.fill(f.exch.orderIDFor(f.put, 0), 0.1, 0.021)

	eventually(t, 2*time.Second, "put fill booked", func() bool { return f.position(f.put) != nil })
	if c := f.position(f.call); c == nil || !c.Expiry.Equal(f.expiry) || c.Strike != 110000 {
		t.Errorf("the call in the chain keeps the chain's fields: %+v", c)
	}
	p := f.position(f.put)
	want := f.expiry.Truncate(24 * time.Hour)
	if p.Strike != 90000 || p.OptionType != "put" || !p.Expiry.Equal(want) || math.Abs(p.Qty-0.1) > 1e-9 {
		t.Errorf("booked put = %+v, want 0.1 of the 90000 put expiring %s", p, want)
	}
}
