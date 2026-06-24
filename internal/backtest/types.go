package backtest

import (
	"context"
	"time"

	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

// MarketFeed is satisfied by HistoricalFeed (backtest) and the live Manager.
type MarketFeed interface {
	NextTick() (*marketdata.Tick, error)
	Done() bool
}

// OrderExecutor is satisfied by SimExecutor (backtest) and the live Executor.
type OrderExecutor interface {
	Submit(ctx context.Context, order orders.Order) (orders.Fill, error)
	Cancel(ctx context.Context, orderID string) error
	AccountEquity(ctx context.Context, currency string) (float64, error)
}

// PortfolioSnapshot is written to the equity curve on each simulated day.
type PortfolioSnapshot struct {
	Date           time.Time
	EquityUSD      float64
	OpenPositions  int
	MarginUsedPct  float64
	IVPercentile   float64
	DrawdownUSD    float64
	DrawdownPct    float64
}

// TradeRecord captures a closed trade for trades.csv.
type TradeRecord struct {
	EntryDate     time.Time `json:"entry_date"`
	ExitDate      time.Time `json:"exit_date"`
	ExitReason    string    `json:"exit_reason"`
	Instrument    string    `json:"instrument"`
	OptionType    string    `json:"option_type"`
	Strike        float64   `json:"strike"`
	Expiry        time.Time `json:"expiry"`
	Qty           float64   `json:"qty"`
	EntryPrice    float64   `json:"entry_price"`
	ExitPrice     float64   `json:"exit_price"`
	PremiumRecvd  float64   `json:"premium_received"`
	CloseCost     float64   `json:"close_cost"`
	PnLUSD        float64   `json:"pnl_usd"`
	ROIPct        float64   `json:"roi_pct"`
	HoldDays      int       `json:"hold_days"`
	Commission    float64   `json:"commission"`
}
