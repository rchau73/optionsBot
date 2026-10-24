// Package exchange names what the bot asks of an exchange, so that a second
// exchange can be added behind the same boundary without touching the
// strategy (E8, #181). It holds interfaces only: today's Deribit types
// satisfy them as they are (see deribit.go), and nothing in the bot depends
// on this package yet — the strategy keeps its own consumer-defined
// interfaces in internal/strategy/deps.go, which tests/exchange_test.go
// checks are the same method sets as the ones below.
//
// The data types (orders.Order, orders.AccountSummary, marketdata.Instrument,
// …) are the bot's own and stay where they are; an adapter for another
// exchange converts to them. docs/architecture.md "Exchange adapters" maps
// every method to today's Deribit call and marks what is Deribit-specific.
package exchange

import (
	"context"
	"time"

	"optionsbot/internal/gateway"
	"optionsbot/internal/gex"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

// OrderPlacer creates and cancels orders.
type OrderPlacer interface {
	// Submit places a buy or sell (limit, market, IOC) and returns what filled.
	Submit(ctx context.Context, order orders.Order) (orders.Fill, error)
	Cancel(ctx context.Context, orderID string) error
	// CancelAllOrders cancels every open order of one currency (startup, kill switch).
	CancelAllOrders(ctx context.Context, currency string) error
	// CancelByLabel cancels the open orders carrying label; returns how many.
	// It resolves a submit whose outcome is unknown (orders.MaybePlaced). An
	// exchange without it needs another way to find an order by client ID.
	CancelByLabel(ctx context.Context, currency, label string) (int, error)
}

// OrderTracker follows resting limit orders until they fill.
type OrderTracker interface {
	GetOrderState(ctx context.Context, orderID string) (orders.OrderStateInfo, error)
	// AmendOrder edits a resting order's size and price in place.
	AmendOrder(ctx context.Context, orderID string, qty, price float64) error
	// OrderFee is the total fee charged for an order's fills (coin).
	OrderFee(ctx context.Context, orderID string) (float64, error)
}

// AccountReader reads positions, the account's margin and the daily closes
// the trend uses.
type AccountReader interface {
	GetAccountSummary(ctx context.Context, currency string) (orders.AccountSummary, error)
	GetPositions(ctx context.Context, currency string) ([]orders.RawPosition, error)
	GetMargins(ctx context.Context, instrument string, amount, price float64) (orders.MarginInfo, error)
	GetDailyCloses(ctx context.Context, instrument string, days int) ([]orders.DailyClose, error)
}

// PortfolioSimulator prices hypothetical positions in the exchange's own
// margin model before an order is sent (entry and top-up sizing, the size
// cap). Deribit-specific (private/simulate_portfolio); an exchange without
// it needs a local margin model, which the bot does not have.
type PortfolioSimulator interface {
	// SimulatePortfolio returns the account summary with positions
	// (instrument → coin size, negative = short) added to the book.
	SimulatePortfolio(ctx context.Context, currency string, positions map[string]float64) (orders.AccountSummary, error)
	// SimulateAlone is the margin of positions on their own (no book offsets).
	SimulateAlone(ctx context.Context, currency string, positions map[string]float64) (orders.AccountSummary, error)
}

// Trading is everything the strategy asks of an exchange account.
type Trading interface {
	OrderPlacer
	OrderTracker
	AccountReader
	PortfolioSimulator
}

// OptionChain is the live option chain and the underlying's index price.
type OptionChain interface {
	UnderlyingPrice() float64
	GetInstrument(name string) (*marketdata.Instrument, bool)
	AllInstruments() []*marketdata.Instrument
	// Track subscribes to the tickers of instruments that have none yet.
	Track(ctx context.Context, instruments []string) error
}

// VolIndex is the exchange's implied-volatility index and its percentile —
// Deribit's DVOL. It drives the margin bands and the flip buffer; an exchange
// without one needs a substitute (e.g. Deribit's DVOL read over a public
// connection, as GEX already reads mainnet).
type VolIndex interface {
	DVOL() float64
	IVPercentile() float64
	// DVOLDaily returns recent daily closes and today, with percentiles.
	DVOLDaily() ([]marketdata.DayIV, marketdata.DayIV)
}

// MarketData is everything the strategy reads from the market.
type MarketData interface {
	OptionChain
	VolIndex
}

// OpenInterest is the option book's open interest per instrument (the
// liquidity cap and journal snapshots). It comes from a book summary of the
// real market, never a test environment.
type OpenInterest interface {
	OpenInterest() *gex.OISnapshot
}

// Transport is the one connection all of an exchange's I/O goes through:
// request/reply, subscriptions and their pushes, and failure reporting. It is
// where rate-limit pools, priorities, retries and the circuit breaker live.
type Transport interface {
	Call(ctx context.Context, method string, params any, priority int) (gateway.JSONRPCResponse, error)
	Subscribe(ctx context.Context, channels []string) error
	Notifications() <-chan gateway.JSONRPCResponse
	// Fatal reports that the connection gave up reconnecting.
	Fatal() <-chan error
	ConnStats() gateway.ConnStats
}

// OptionNameParser reads an exchange's option instrument name.
type OptionNameParser func(name string) (underlying string, expiry time.Time, strike float64, optType string, err error)

// RateLimitPool reports whether a request method draws from the exchange's
// order (matching-engine) pool rather than the general one.
type RateLimitPool func(method string) bool

// FeeModel is the exchange's option fee for qty contracts (the backtest and
// stress tools; the live bot books the fees the exchange reports).
type FeeModel func(feePerContract, optionPrice, qty float64) float64
