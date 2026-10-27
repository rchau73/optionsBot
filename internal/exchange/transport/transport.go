// Package transport names the request/reply and subscription parts of an
// exchange connection (E8, #182). It lives apart from internal/exchange
// because that package names the market-data types (marketdata.Instrument),
// so marketdata itself could not import it; this one imports the gateway's
// wire types only. exchange.Transport is built from these interfaces, and
// gateway.Gateway is today's only implementation.
package transport

import (
	"context"

	"optionsbot/internal/gateway"
)

// Caller sends one request and returns its reply. priority is the gateway's
// lane (gateway.PriorityHigh is drained first).
type Caller interface {
	Call(ctx context.Context, method string, params any, priority int) (gateway.JSONRPCResponse, error)
}

// Streamer subscribes to channels and delivers their pushes.
type Streamer interface {
	Subscribe(ctx context.Context, channels []string) error
	Notifications() <-chan gateway.JSONRPCResponse
}

// Compile-time check: the Deribit gateway is the implementation.
var (
	_ Caller   = (*gateway.Gateway)(nil)
	_ Streamer = (*gateway.Gateway)(nil)
)
