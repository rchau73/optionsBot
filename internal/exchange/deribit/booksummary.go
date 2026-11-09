// Package deribit holds Deribit's calls behind the exchange boundary (E8,
// docs/exchange-adapter.md §4; step 6 grows it into the adapter). It imports
// the connection's parts and marketdata's types, never internal/exchange, so
// gex and recorder can use it: internal/exchange names gex.Manager.
package deribit

import (
	"context"
	"encoding/json"
	"fmt"

	"optionsbot/internal/exchange/transport"
	"optionsbot/internal/gateway"
)

// SummaryRow is one instrument of public/get_book_summary_by_currency.
type SummaryRow struct {
	InstrumentName  string  `json:"instrument_name"`
	OpenInterest    float64 `json:"open_interest"`
	UnderlyingPrice float64 `json:"underlying_price"` // that expiry's future
	MarkIV          float64 `json:"mark_iv"`          // percent, e.g. 45.2
	// Prices in the coin (null on an empty side → 0); recorded by
	// internal/recorder, unused by the GEX itself.
	BidPrice  float64 `json:"bid_price"`
	AskPrice  float64 `json:"ask_price"`
	MidPrice  float64 `json:"mid_price"`
	MarkPrice float64 `json:"mark_price"`
}

// BookReader reads book summaries over one connection (the one the caller
// wires: the mainnet public gateway for GEX and the recorder).
type BookReader struct {
	gw transport.Caller
}

// NewBookReader reads through gw.
func NewBookReader(gw transport.Caller) *BookReader { return &BookReader{gw: gw} }

// BookSummary returns every option of currency (a coin, or "USDC" for the
// USDC-settled options), on the low-priority lane: the non-matching pool.
func (r *BookReader) BookSummary(ctx context.Context, currency string) ([]SummaryRow, error) {
	resp, err := r.gw.Call(ctx, "public/get_book_summary_by_currency", map[string]any{
		"currency": currency,
		"kind":     "option",
	}, gateway.PriorityLow)
	if err != nil {
		return nil, err
	}
	var rows []SummaryRow
	if err := json.Unmarshal(resp.Result, &rows); err != nil {
		return nil, fmt.Errorf("decode %s book summary: %w", currency, err)
	}
	return rows, nil
}
