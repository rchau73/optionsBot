package recorder

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"optionsbot/internal/gateway"
	"optionsbot/internal/gex"
)

// rpcCaller is the slice of the gateway LinearBook uses.
type rpcCaller interface {
	Call(ctx context.Context, method string, params any, priority int) (gateway.JSONRPCResponse, error)
}

// Fetcher is a Source that fetches its summary on demand: Run calls
// FetchSummary before each snapshot. (The GEX manager refreshes itself and
// is not one: an extra GEX refresh would count as an extra snapshot.)
type Fetcher interface {
	FetchSummary(ctx context.Context) error
}

// LinearBook is the mainnet book summary of the underlying's USDC-settled
// options (e.g. BTC_USDC-27NOV26-96000-C, prices in USDC), recorded beside
// the coin-settled market to compare their liquidity (2026-10-13: tighter
// quotes on calm days, but under 0.5 % of the open interest). One extra
// public call per snapshot; trading never reads it.
type LinearBook struct {
	gw     rpcCaller
	prefix string

	mu   sync.Mutex
	rows []gex.SummaryRow
	at   time.Time
}

// NewLinearBook records underlying's options settled in USDC.
func NewLinearBook(gw rpcCaller, underlying string) *LinearBook {
	return &LinearBook{gw: gw, prefix: underlying + "_USDC-"}
}

// FetchSummary reads the USDC options' book summary and keeps the
// underlying's rows.
func (b *LinearBook) FetchSummary(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := b.gw.Call(ctx, "public/get_book_summary_by_currency", map[string]any{
		"currency": "USDC",
		"kind":     "option",
	}, gateway.PriorityLow)
	if err != nil {
		return err
	}
	var all []gex.SummaryRow
	if err := json.Unmarshal(resp.Result, &all); err != nil {
		return fmt.Errorf("decode USDC book summary: %w", err)
	}
	rows := make([]gex.SummaryRow, 0, len(all))
	for _, r := range all {
		if strings.HasPrefix(r.InstrumentName, b.prefix) {
			rows = append(rows, r)
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rows, b.at = rows, time.Now()
	return nil
}

// LatestSummary returns the last fetched rows and when they were fetched.
func (b *LinearBook) LatestSummary() ([]gex.SummaryRow, time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rows, b.at
}
