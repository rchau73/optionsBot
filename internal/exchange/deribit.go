package exchange

import (
	"optionsbot/internal/gateway"
	"optionsbot/internal/gex"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

// Deribit is the exchange's key in data that names an exchange (the account
// registry, internal/accounts, #585).
const Deribit = "deribit"

// Names are the exchanges the bot has an adapter for.
var Names = []string{Deribit}

// Compile-time checks that today's Deribit implementation satisfies every
// interface of this package. A change to either side that breaks the fit
// fails the build here.
var (
	_ OrderPlacer        = (*orders.Executor)(nil)
	_ OrderTracker       = (*orders.Executor)(nil)
	_ AccountReader      = (*orders.Executor)(nil)
	_ PortfolioSimulator = (*orders.Executor)(nil)
	_ Trading            = (*orders.Executor)(nil)

	_ OptionChain = (*marketdata.Manager)(nil)
	_ VolIndex    = (*marketdata.Manager)(nil)
	_ MarketData  = (*marketdata.Manager)(nil)

	_ OpenInterest = (*gex.Manager)(nil)

	_ Transport = (*gateway.Gateway)(nil)

	_ OptionNameParser = marketdata.ParseOptionName
	_ RateLimitPool    = gateway.IsMatchingEngine
	_ FeeModel         = orders.OptionFee
)
