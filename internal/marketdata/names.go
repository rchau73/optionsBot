package marketdata

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SettlementHourUTC is when Deribit options expire: 08:00 UTC on the expiry date.
const SettlementHourUTC = 8 * time.Hour

// ParseOptionName splits a Deribit option name such as BTC-27JUN25-70000-C.
// underlying is the name's first part as written ("SOL_USDC" for a linear
// option, as the research tools key their books); InstrumentFromName splits
// it into the coin and the settlement currency.
// expiry is the expiry date at 00:00 UTC; add SettlementHourUTC for the
// exact settlement time.
func ParseOptionName(name string) (underlying string, expiry time.Time, strike float64, optType string, err error) {
	parts := strings.Split(name, "-")
	if len(parts) != 4 {
		return "", time.Time{}, 0, "", fmt.Errorf("expected 4 parts in %q", name)
	}
	underlying = parts[0]
	expiry, err = time.Parse("2Jan06", parts[1]) // day has 1 or 2 digits: 4OCT26, 27NOV26
	if err != nil {
		return "", time.Time{}, 0, "", fmt.Errorf("parse expiry %q: %w", parts[1], err)
	}
	// A fractional strike is written with a "d" for the decimal point
	// (XRP_USDC-27NOV26-2d5-C is the 2.5 strike).
	strike, err = strconv.ParseFloat(strings.Replace(parts[2], "d", ".", 1), 64)
	if err != nil {
		return "", time.Time{}, 0, "", fmt.Errorf("parse strike %q: %w", parts[2], err)
	}
	switch parts[3] {
	case "C":
		optType = "call"
	case "P":
		optType = "put"
	default:
		return "", time.Time{}, 0, "", fmt.Errorf("unknown option type %q", parts[3])
	}
	return underlying, expiry, strike, optType, nil
}

// LinearSettlement is the currency of Deribit's linear options: their names
// start "<COIN>_USDC-" and they are listed under currency=USDC.
const LinearSettlement = "USDC"

// linearCoins are the underlyings Deribit lists only as USDC-settled options
// (#838: the SOL and XRP bots). BTC and ETH trade their inverse options.
var linearCoins = map[string]bool{"SOL": true, "XRP": true}

// IsLinear reports whether the bot trades underlying's USDC-settled options.
func IsLinear(underlying string) bool { return linearCoins[strings.ToUpper(underlying)] }

// SettlementCurrency is the currency underlying's options settle in and are
// listed under: the coin itself for BTC and ETH (returned as given, so their
// requests stay byte-identical), USDC for SOL and XRP.
func SettlementCurrency(underlying string) string {
	if IsLinear(underlying) {
		return LinearSettlement
	}
	return underlying
}

// OptionPrefix is how every option name of a linear underlying starts,
// e.g. "SOL_USDC-": it picks the coin's options out of the USDC list.
func OptionPrefix(underlying string) string {
	return strings.ToUpper(underlying) + "_" + LinearSettlement + "-"
}

// SplitUnderlying reads an option name's first part: "SOL_USDC" is SOL
// settled in USDC, "BTC" is BTC settled in BTC.
func SplitUnderlying(part string) (coin, settlement string) {
	if coin, settlement, ok := strings.Cut(part, "_"); ok {
		return coin, settlement
	}
	return part, part
}

// InstrumentFromName is an instrument's fixed fields — underlying, settlement
// currency, expiry, strike, option type — read from its Deribit name, for a
// name the chain does not hold (a journal record, an expired leg). Quotes and
// greeks stay zero; so does the contract size, which only the chain gives.
// The strategy reaches it only through Manager.InstrumentFromName (#773), so
// it never parses a name itself.
func InstrumentFromName(name string) (Instrument, error) {
	part, expiry, strike, optType, err := ParseOptionName(name)
	if err != nil {
		return Instrument{}, err
	}
	underlying, settlement := SplitUnderlying(part)
	return Instrument{Name: name, Underlying: underlying, Settlement: settlement, Expiry: expiry, Strike: strike, OptionType: optType}, nil
}

// InstrumentFromName is the package's InstrumentFromName as part of the
// manager's option chain (exchange.OptionChain).
func (m *Manager) InstrumentFromName(name string) (Instrument, error) {
	return InstrumentFromName(name)
}
