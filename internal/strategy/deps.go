package strategy

import (
	"time"

	"optionsbot/internal/exchange"
	"optionsbot/internal/gex"
	"optionsbot/internal/history"
	"optionsbot/internal/orders"
)

// The strategy talks to the outside world only through the small interfaces
// below. What it asks of the exchange — orders, account, margin simulation,
// market data, open interest — is the exchange boundary in internal/exchange
// (#182); Deribit's *orders.Executor, *marketdata.Manager and *gex.Manager
// are its only adapter. The rest (journal, hedge report, histories) are the
// bot's own and defined here where they are consumed. Tests substitute fakes.

// Exchange is everything the strategy asks of an exchange account.
type Exchange = exchange.Trading

// MarketData provides the latest option chain snapshot, index price and
// volatility index.
type MarketData = exchange.MarketData

// TradeJournal records every order event, with its market snapshot, and the
// periodic P&L to the audit log (orders.log).
type TradeJournal interface {
	LogSubmit(r orders.PendingOrderRecord, ctx orders.EventContext)
	LogAmend(r orders.PendingOrderRecord, previousPrice float64, ctx orders.EventContext)
	LogCancelled(r orders.PendingOrderRecord, ctx orders.EventContext)
	LogOpen(pos *orders.Position, fill orders.Fill, trigger string, ctx orders.EventContext)
	LogClose(pos *orders.Position, fill orders.Fill, trigger, orderType string, ctx orders.EventContext)
	LogReconciled(pos *orders.Position, ctx orders.EventContext)
	LogSkipped(reason string, ctx orders.EventContext)
	LogRisk(r orders.RiskRecord)
	LogTrend(r orders.TrendRecord)
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
type OISource = exchange.OpenInterest

// PnLRecorder persists the strategy's total P&L over time (for charts).
// realised is cumulative since the journal began (restored at startup);
// margin is the policy's last margin view, nil when there is none.
type PnLRecorder interface {
	RecordPnL(t time.Time, realised, unrealised, spot float64, margin *history.Margin)
}

// RegimeHistory keeps the gamma regime at each UTC daily close, so a regime
// change can be confirmed across restarts.
type RegimeHistory interface {
	Record(t time.Time, regime string)
	// Seed starts an empty history at the live regime (a brand-new bot, #147).
	Seed(now time.Time, regime string, days int) bool
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
	// Directions is optional: records the SMA trend next to the GEX bias
	// whenever either (or the shed signal) changes.
	Directions DirectionRecorder
}

// DirectionRecorder keeps the direction sources' history (never read by trading).
type DirectionRecorder interface {
	Record(history.DirectionRecord)
}
