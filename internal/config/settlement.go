package config

import "strings"

// LinearSettlement is the currency of Deribit's linear options: their names
// start "<COIN>_USDC-" and they are listed under currency=USDC.
const LinearSettlement = "USDC"

// linearCoins are the underlyings Deribit lists only as USDC-settled options
// (#838: the SOL and XRP bots). BTC and ETH trade their inverse options.
var linearCoins = map[string]bool{"SOL": true, "XRP": true}

// IsLinear reports whether the bot trades underlying's USDC-settled options.
func IsLinear(underlying string) bool { return linearCoins[strings.ToUpper(underlying)] }

// SettlementCurrency is the currency underlying's options settle in, the unit
// of their prices, premiums, fees and P&L: the coin itself for BTC and ETH
// (returned as given), USDC for SOL and XRP.
func SettlementCurrency(underlying string) string {
	if IsLinear(underlying) {
		return LinearSettlement
	}
	return underlying
}

// PremiumUnit is min_premium's unit (#832): the settlement currency, so a
// SOL bot's floor of 0.5 is 0.5 USDC and a BTC bot's 0.0005 is 0.0005 BTC.
func (c *Config) PremiumUnit() string { return SettlementCurrency(c.Underlying) }
