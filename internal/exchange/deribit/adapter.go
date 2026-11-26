package deribit

import (
	"optionsbot/internal/config"
	"optionsbot/internal/gateway"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

// Adapter is Deribit as one exchange (#761, docs/exchange-adapter.md §4 step
// 6): today's connection, order executor and market-data manager composed in
// one value. It adds no behaviour: every method is the embedded type's own,
// so the requests, their order, retries and rate-limit pools are unchanged.
// internal/exchange/deribit.go checks that it satisfies exchange.Exchange
// (this package cannot import internal/exchange, which imports gex).
//
// The GEX manager is not part of it: its open interest comes from mainnet,
// on testnet over a second, public-only connection.
type Adapter struct {
	*gateway.Gateway    // the connection: Connect, Close, Fatal, ConnStats
	*orders.Executor    // orders, positions, margin, its simulation, daily closes, futures
	*marketdata.Manager // the option chain, the index and DVOL
}

// NewAdapter composes the exchange over gw, which the caller connects and
// closes. Building it sends nothing: the manager sends its first requests in
// Start, the executor on its first call.
func NewAdapter(cfg *config.Config, gw *gateway.Gateway) *Adapter {
	return &Adapter{
		Gateway:  gw,
		Executor: orders.NewExecutor(gw),
		Manager:  marketdata.New(cfg, gw, NewChainSource(gw)),
	}
}
