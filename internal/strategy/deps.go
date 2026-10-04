package strategy

import (
	"context"
	"time"

	"optionsbot/internal/gex"
	"optionsbot/internal/history"
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
	// CancelByLabel cancels the open orders carrying label; returns how many.
	CancelByLabel(ctx context.Context, currency, label string) (int, error)
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
	// SimulatePortfolio returns the account summary Deribit would report with
	// positions (instrument → coin size, negative = short) added to the book.
	SimulatePortfolio(ctx context.Context, currency string, positions map[string]float64) (orders.AccountSummary, error)
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
	DVOL() float64
	IVPercentile() float64
	// DVOLDaily returns recent daily DVOL closes and today, with percentiles.
	DVOLDaily() ([]marketdata.DayIV, marketdata.DayIV)
	GetInstrument(name string) (*marketdata.Instrument, bool)
	AllInstruments() []*marketdata.Instrument
	// Track subscribes to the tickers of instruments that have none yet.
	Track(ctx context.Context, instruments []string) error
}

// TradeJournal records every order event, with its market snapshot, and the
// periodic P&L to the audit log (orders.log).
type TradeJournal interface {
	LogSubmit(r orders.PendingOrderRecord, ctx orders.EventContext)
	LogAmend(r orders.PendingOrderRecord, previousPrice float64, ctx orders.EventContext)
	LogCancelled(r orders.PendingOrderRecord, ctx orders.EventContext)
	LogOpen(pos *orders.Position, fill orders.Fill, ctx orders.EventContext)
	LogClose(pos *orders.Position, fill orders.Fill, trigger, orderType string, ctx orders.EventContext)
	LogReconciled(pos *orders.Position, ctx orders.EventContext)
	LogSkipped(reason string, ctx orders.EventContext)
	LogRisk(r orders.RiskRecord)
	LogPnL(p orders.PnLRecord)
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

// OISource provides open interest per instrument (refreshed periodically).
type OISource interface {
	OpenInterest() *gex.OISnapshot
}

// PnLRecorder persists the strategy's total P&L over time (for charts).
// realised is counted since this process started.
type PnLRecorder interface {
	RecordPnL(t time.Time, realised, unrealised, spot float64)
}

// RegimeHistory keeps the gamma regime at each UTC daily close, so a regime
// change can be confirmed across restarts.
type RegimeHistory interface {
	Record(t time.Time, regime string)
	Daily() []history.RegimeDay
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
	GEX     GEXSource   // optional: nil until a GEX manager is wired
	OI      OISource    // optional: open interest for journal snapshots
	History PnLRecorder // optional: P&L history for the monitor chart
	// Regimes is required for the gamma-regime margin rule when GEX is set;
	// nil keeps the regime history in memory only.
	Regimes RegimeHistory
}
