package exchange

// Capabilities says what an exchange can do, as data (docs/exchange-adapter.md
// §3). Nothing reads it yet: it is code, not a setting, and each fallback for
// a missing capability is a person's decision (§5).
type Capabilities struct {
	MarginSimulator     bool // portfolio margin simulation before an order
	VolIndex            bool // an implied-volatility index like DVOL
	CancelByLabel       bool // cancel/look up an order by client order ID
	AmendInPlace        bool // edit price and size of a resting order
	CancelAllByCurrency bool // cancel every order of one underlying
	PortfolioMargin     bool // margin offsets across the book
	InverseSettlement   bool // premiums and P&L in the coin, not USD(C)
	MainnetOpenInterest bool // real open interest on the same venue
}

// DeribitCapabilities is Deribit's value: it has them all. tests/exchange_test.go
// pins it, so a change is deliberate.
var DeribitCapabilities = Capabilities{
	MarginSimulator:     true,
	VolIndex:            true,
	CancelByLabel:       true,
	AmendInPlace:        true,
	CancelAllByCurrency: true,
	PortfolioMargin:     true,
	InverseSettlement:   true,
	MainnetOpenInterest: true,
}
