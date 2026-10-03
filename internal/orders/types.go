package orders

import "time"

// Trigger reason constants.
const (
	TriggerEntry             = "entry"
	TriggerRollout19DTE      = "rollout_19dte"
	TriggerRolloutDelta      = "rollout_delta_drift"
	TriggerRolloutROI        = "rollout_roi"
	TriggerStopLoss200Pct    = "stop_loss_200pct"
	TriggerGammaClose        = "gamma_close"
	TriggerKillSwitch        = "kill_switch"
	TriggerReconciled        = "reconciled"         // position loaded from exchange on startup
	TriggerTimeout           = "order_timeout"      // limit order cancelled after fill-timeout elapsed
	TriggerRebalanceDownsize = "rebalance_downsize" // startup: position exceeds current budget — partial close
)

// Order direction and type constants.
const (
	DirectionBuy  = "buy"
	DirectionSell = "sell"
	TypeLimit     = "limit"
	TypeMarket    = "market"
)

// Position represents an open short options leg.
type Position struct {
	ID              string
	Instrument      string
	Underlying      string
	Strike          float64
	Expiry          time.Time
	OptionType      string
	Qty             float64
	EntryPrice      float64 // option premium received per unit (BTC)
	UnderlyingPrice float64 // spot price of BTC/ETH at time of entry
	EntryTime       time.Time
	PremiumReceived float64 // credit received (positive)
	CurrentMid      float64
	CurrentGreeks   Greeks
	LimitOrderID    string // pending limit close, if any
	LimitPrice      float64
}

type Greeks struct {
	Delta float64 `json:"delta"`
	Gamma float64 `json:"gamma"`
	Theta float64 `json:"theta"`
	Vega  float64 `json:"vega"`
	Rho   float64 `json:"rho"`
	IV    float64 `json:"iv"`
}

// DTE returns whole calendar days to expiry from the current wall clock.
func (p *Position) DTE() int { return p.DTEAt(time.Now()) }

// DTEAt returns whole calendar days to expiry as of now (0 once expired).
// The backtest passes its simulated date.
func (p *Position) DTEAt(now time.Time) int {
	d := p.Expiry.Sub(now).Hours() / 24
	if d < 0 {
		return 0
	}
	return int(d)
}

// MtMPnL returns mark-to-market PnL: premium received minus current close cost.
func (p *Position) MtMPnL() float64 {
	return p.PremiumReceived - p.CurrentMid*p.Qty
}

// ROIPct returns (premium_received - close_cost) / premium_received.
func (p *Position) ROIPct() float64 {
	if p.PremiumReceived == 0 {
		return 0
	}
	return (p.PremiumReceived - p.CurrentMid*p.Qty) / p.PremiumReceived
}

// LossPct returns current loss as a multiple of premium received (positive = loss).
func (p *Position) LossPct() float64 {
	if p.PremiumReceived == 0 {
		return 0
	}
	loss := p.CurrentMid*p.Qty - p.PremiumReceived
	return loss / p.PremiumReceived
}

// Strangle groups a call and put leg for a given expiry target.
type Strangle struct {
	ID         string
	TargetDTE  int
	EntryDelta float64 // the delta target used when this strangle was opened
	CallLeg    *Position
	PutLeg     *Position
	OpenedAt   time.Time
}

// MarginInfo holds the margin impact returned by private/get_margins.
type MarginInfo struct {
	InitialMargin     float64 `json:"initial_margin"`
	MaintenanceMargin float64 `json:"maintenance_margin"`
}

// OrderStateInfo holds the fill status returned by private/get_order_state.
type OrderStateInfo struct {
	OrderID      string  `json:"order_id"`
	State        string  `json:"order_state"` // open | filled | cancelled | rejected
	FilledAmount float64 `json:"filled_amount"`
	AvgPrice     float64 `json:"average_price"`
}

// AccountSummary holds the key account metrics returned by private/get_account_summary.
type AccountSummary struct {
	Currency          string  `json:"currency"`
	Equity            float64 `json:"equity"`
	Balance           float64 `json:"balance"`
	AvailableFunds    float64 `json:"available_funds"`
	InitialMargin     float64 `json:"initial_margin"`
	MaintenanceMargin float64 `json:"maintenance_margin"`
	MarginBalance     float64 `json:"margin_balance"`
	DeltaTotal        float64 `json:"delta_total"`
	OptionsPL         float64 `json:"options_pl"`
	OptionsValue      float64 `json:"options_value"`
}

// RawPosition is the per-position record returned by private/get_positions.
type RawPosition struct {
	InstrumentName string  `json:"instrument_name"`
	Size           float64 `json:"size"`
	Direction      string  `json:"direction"`
	AveragePrice   float64 `json:"average_price"`
	MarkPrice      float64 `json:"mark_price"`
	IndexPrice     float64 `json:"index_price"` // underlying spot at snapshot time
	Delta          float64 `json:"delta"`
	Gamma          float64 `json:"gamma"`
	Theta          float64 `json:"theta"`
	Vega           float64 `json:"vega"`
	Rho            float64 `json:"rho"`
	Kind           string  `json:"kind"`
	FloatingPnL    float64 `json:"floating_profit_loss"`
}

// Fill represents an order execution result.
type Fill struct {
	OrderID   string
	FillPrice float64
	Qty       float64
	Timestamp time.Time
}

// DailyClose holds the closing price for a single UTC day.
type DailyClose struct {
	Date  time.Time
	Close float64
}

// Order is an outbound order request.
type Order struct {
	Instrument string
	Direction  string
	OrderType  string
	Qty        float64
	LimitPrice float64
	TickSize   float64 // per-instrument tick size; 0 falls back to default 0.0001
	// TimeInForce is Deribit's time_in_force; empty means good_til_cancelled.
	// TimeInForceIOC fills what it can immediately and cancels the rest, so the
	// returned Fill is final and the caller never has to track a resting order.
	TimeInForce   string
	TriggerReason string
	// Label tags the order on Deribit with its strategy and slot (max 64
	// characters), so exchange-side fills can be attributed per strategy.
	Label string
}

// MaxLabelLen is Deribit's limit for an order label.
const MaxLabelLen = 64

// TimeInForceIOC is Deribit's immediate_or_cancel time in force.
const TimeInForceIOC = "immediate_or_cancel"
