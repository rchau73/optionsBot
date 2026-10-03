package strategy

import (
	"context"

	"optionsbot/internal/gex"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

// The strategy talks to the outside world only through the small interfaces
// below, defined here where they are consumed. *orders.Executor,
// *marketdata.Manager, *orders.Logger, *hedge.Reporter and *gex.Manager
// satisfy them in production; tests substitute fakes.

// OrderPlacer creates and cancels orders.
type OrderPlacer interface {
	Submit(ctx context.Context, order orders.Order) (orders.Fill, error)
	Cancel(ctx context.Context, orderID string) error
	CancelAllOrders(ctx context.Context, currency string) error
}

// OrderTracker follows resting limit orders until they fill.
type OrderTracker interface {
	GetOrderState(ctx context.Context, orderID string) (orders.OrderStateInfo, error)
	AmendOrder(ctx context.Context, orderID string, qty, price float64) error
}

// AccountReader reads account, position and margin data.
type AccountReader interface {
	GetAccountSummary(ctx context.Context, currency string) (orders.AccountSummary, error)
	GetPositions(ctx context.Context, currency string) ([]orders.RawPosition, error)
	GetMargins(ctx context.Context, instrument string, amount, price float64) (orders.MarginInfo, error)
	GetDailyCloses(ctx context.Context, instrument string, days int) ([]orders.DailyClose, error)
}

// Exchange is everything the strategy needs from Deribit.
type Exchange interface {
	OrderPlacer
	OrderTracker
	AccountReader
}

// MarketData provides the latest option chain snapshot and index price.
type MarketData interface {
	UnderlyingPrice() float64
	IVPercentile() float64
	GetInstrument(name string) (*marketdata.Instrument, bool)
	AllInstruments() []*marketdata.Instrument
}

// TradeJournal records every order event to the audit log (orders.log).
type TradeJournal interface {
	LogOpen(pos *orders.Position, fill orders.Fill, ivPercentile, spreadAlertThreshold float64, mkt orders.MarketContext, gexCtx orders.GEXContext)
	LogClose(pos *orders.Position, fill orders.Fill, ivPercentile float64, trigger string, mkt orders.MarketContext, gexCtx orders.GEXContext)
	LogSubmit(r orders.PendingOrderRecord, mkt orders.MarketContext, gexCtx orders.GEXContext)
	LogCancelled(r orders.PendingOrderRecord, mkt orders.MarketContext, gexCtx orders.GEXContext)
	LogReconciled(pos *orders.Position, ivPercentile float64, mkt orders.MarketContext, gexCtx orders.GEXContext)
}

// HedgeReporter writes a hedge suggestion when net delta is too large.
// It never places orders.
type HedgeReporter interface {
	MaybeReport(netDelta, underlyingPrice float64, suggestedInst string)
}

// GEXSource provides the latest market-wide gamma exposure snapshot.
type GEXSource interface {
	Snapshot() *gex.Snapshot
}

// Deps groups the collaborators a Strategy needs.
type Deps struct {
	Market   MarketData
	Exchange Exchange
	// State is the in-memory book of positions and strangles. It is a plain
	// struct with no I/O, so tests use the real one rather than a fake.
	State   *orders.StateManager
	Journal TradeJournal
	Hedge   HedgeReporter
	GEX     GEXSource // optional: nil until a GEX manager is wired
}
