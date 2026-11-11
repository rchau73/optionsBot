package deribit

import (
	"context"
	"encoding/json"
	"fmt"

	"optionsbot/internal/account"
	"optionsbot/internal/exchange/transport"
	"optionsbot/internal/gateway"
)

// Compile-time check: this is the account poller's source.
var _ account.Source = (*AccountSource)(nil)

// AccountSource reads the account's summaries and positions for the
// monitor's account view (account.Poller, #746). Every call is read-only, on
// the low-priority lane: the non-matching pool.
type AccountSource struct {
	gw transport.Caller
}

// NewAccountSource reads through gw.
func NewAccountSource(gw transport.Caller) *AccountSource { return &AccountSource{gw: gw} }

// Summaries returns every currency's summary in one call
// (private/get_account_summaries, extended).
func (s *AccountSource) Summaries(ctx context.Context) ([]account.Summary, error) {
	resp, err := s.gw.Call(ctx, "private/get_account_summaries", map[string]any{"extended": true}, gateway.PriorityLow)
	if err != nil {
		return nil, fmt.Errorf("get_account_summaries: %w", err)
	}
	var result struct {
		Summaries []account.Summary `json:"summaries"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("decode account summaries: %w", err)
	}
	return result.Summaries, nil
}

// Summary returns one currency's summary (private/get_account_summary,
// extended).
func (s *AccountSource) Summary(ctx context.Context, currency string) (account.Summary, error) {
	resp, err := s.gw.Call(ctx, "private/get_account_summary", map[string]any{"currency": currency, "extended": true}, gateway.PriorityLow)
	if err != nil {
		return account.Summary{}, err
	}
	var sum account.Summary
	if err := json.Unmarshal(resp.Result, &sum); err != nil {
		return account.Summary{}, err
	}
	return sum, nil
}

// Positions returns every position of currency, all kinds
// (private/get_positions).
func (s *AccountSource) Positions(ctx context.Context, currency string) ([]account.PositionRow, error) {
	resp, err := s.gw.Call(ctx, "private/get_positions", map[string]any{"currency": currency}, gateway.PriorityLow)
	if err != nil {
		return nil, fmt.Errorf("get_positions %s: %v", currency, err)
	}
	var rows []account.PositionRow
	if err := json.Unmarshal(resp.Result, &rows); err != nil {
		return nil, fmt.Errorf("decode positions %s: %v", currency, err)
	}
	return rows, nil
}
