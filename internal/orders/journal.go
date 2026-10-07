package orders

import "time"

// Journal event names (the "event" field of every orders.log line).
const (
	EventSubmitted  = "submitted"
	EventAmended    = "amended"
	EventCancelled  = "cancelled"
	EventFilled     = "filled"
	EventClosed     = "closed"
	EventReconciled = "reconciled"
	EventSkipped    = "skipped"
	EventPnL        = "pnl"
	EventRisk       = "risk_limit"
)

// Risk changes journaled as EventRisk.
const (
	RiskFrozen         = "frozen"          // a DVOL band or gamma regime change was seen; new risk blocked
	RiskUnfrozen       = "unfrozen"        // the change reverted before confirmation, or was confirmed
	RiskLimitChanged   = "limit_changed"   // a confirmed change moved the IM limit
	RiskRebalance      = "rebalance"       // the book is being resized toward the IM limit
	RiskRebalanceRetry = "rebalance_retry" // a complement fell short: the rebalance runs again after a cooldown
	RiskMMBreach       = "mm_breach"       // maintenance margin above max_mm_pct: reducing now
)

// RiskRecord is one margin-policy decision with the figures behind it.
// Margin percentages are of Deribit's margin balance, in Unit (USD under
// cross collateral, otherwise the underlying).
type RiskRecord struct {
	Timestamp    time.Time `json:"timestamp"`
	Event        string    `json:"event"` // always "risk_limit"
	StrategyID   string    `json:"strategy_id"`
	Change       string    `json:"change"`
	Detail       string    `json:"detail"`
	LimitIMPct   float64   `json:"limit_im_pct"`
	BandLimitPct float64   `json:"band_limit_pct"`
	MaxMMPct     float64   `json:"max_mm_pct"`
	IMPct        float64   `json:"im_pct"`
	MMPct        float64   `json:"mm_pct"`
	Unit         string    `json:"unit,omitempty"`
	DVOL         float64   `json:"dvol"`
	IVPercentile float64   `json:"iv_percentile"`
	Regime       string    `json:"regime"`
	Frozen       bool      `json:"frozen"`
}

// SlotRef identifies the (DTE, delta) strategy slot a record belongs to.
type SlotRef struct {
	DTE   int     `json:"dte"`
	Delta float64 `json:"delta"`
}

// MarketContext is the portfolio state at the moment of an event.
type MarketContext struct {
	Trend    string  `json:"trend,omitempty"` // bull | bear | neutral
	NetDelta float64 `json:"net_delta"`
	NetGamma float64 `json:"net_gamma"`
	NetVega  float64 `json:"net_vega"`
	NetTheta float64 `json:"net_theta"`
}

// MarketSnapshot records the market conditions at the moment of a decision,
// so every P&L number can later be explained by the context it was made in.
// Prices of inverse options are in the underlying (BTC/ETH); "_usd" fields
// are dollars. Zero-valued optional fields mean "not known at that moment".
type MarketSnapshot struct {
	AsOf time.Time `json:"as_of"`
	Spot float64   `json:"spot"` // index price, USD

	// Volatility
	DVOL         float64 `json:"dvol"`          // Deribit volatility index, %
	IVPercentile float64 `json:"iv_percentile"` // today's DVOL vs the configured window, 0–100
	OptionIV     float64 `json:"option_iv"`     // this option's mark IV, fraction (0.55 = 55%)
	ATMIV        float64 `json:"atm_iv,omitempty"`
	Skew         float64 `json:"skew,omitempty"` // option IV − ATM IV of the same expiry

	// Moneyness
	Strike              float64 `json:"strike,omitempty"`
	DTE                 float64 `json:"dte,omitempty"`          // days, fractional
	Moneyness           string  `json:"moneyness,omitempty"`    // ITM | ATM | OTM
	DistanceToStrikePct float64 `json:"distance_to_strike_pct"` // positive = OTM, negative = ITM
	DistanceToStrikeSD  float64 `json:"distance_to_strike_sd"`  // same, in standard deviations of the expected move
	Delta               float64 `json:"delta"`

	// Option value, all in the underlying coin
	Bid       float64 `json:"bid"`
	Ask       float64 `json:"ask"`
	Mid       float64 `json:"mid"`
	SpreadPct float64 `json:"spread_pct"` // (ask − bid) / mid × 100
	Intrinsic float64 `json:"intrinsic"`
	Extrinsic float64 `json:"extrinsic"`

	// Open interest (from the GEX book-summary poll)
	InstrumentOI  float64   `json:"instrument_oi,omitempty"`
	StrikeOI      float64   `json:"strike_oi,omitempty"` // calls + puts at this strike and expiry
	ExpiryOI      float64   `json:"expiry_oi,omitempty"`
	StrikeOIRank  int       `json:"strike_oi_rank,omitempty"` // 1 = largest OI strike of the expiry
	MaxPainStrike float64   `json:"max_pain_strike,omitempty"`
	OIAsOf        time.Time `json:"oi_as_of,omitempty"`

	// Dealer positioning
	GEXRegime     string  `json:"gex_regime,omitempty"`
	GammaFlip     float64 `json:"gamma_flip,omitempty"`
	SpotToFlipPct float64 `json:"spot_to_flip_pct,omitempty"` // (spot − flip) / flip × 100
}

// EventContext is what every journal entry records besides the order itself.
type EventContext struct {
	StrategyID string
	// Detail says why, with the numbers (e.g. "stop-loss: loss 2.3× the
	// premium ≥ 2×"); journaled as "detail".
	Detail    string
	Slot      *SlotRef
	Market    MarketSnapshot
	Portfolio MarketContext
}

// PendingOrderRecord describes an order at submission, amendment or cancel.
type PendingOrderRecord struct {
	OrderID       string
	Instrument    string
	OptionType    string // "call" or "put"
	Direction     string
	OrderType     string
	TriggerReason string
	Qty           float64
	LimitPrice    float64
	Greeks        Greeks
}

// OrderLog is one line of orders.log.
type OrderLog struct {
	Timestamp     time.Time `json:"timestamp"`
	Event         string    `json:"event"`
	Status        string    `json:"status"` // same as event; kept for existing log filters
	StrategyID    string    `json:"strategy_id,omitempty"`
	Slot          *SlotRef  `json:"slot,omitempty"`
	OrderID       string    `json:"order_id,omitempty"`
	Instrument    string    `json:"instrument,omitempty"`
	OptionType    string    `json:"option_type,omitempty"`
	Direction     string    `json:"direction,omitempty"`
	OrderType     string    `json:"order_type,omitempty"`
	TriggerReason string    `json:"trigger_reason,omitempty"`
	SkipReason    string    `json:"skip_reason,omitempty"`
	Detail        string    `json:"detail,omitempty"` // why, with the numbers
	Qty           float64   `json:"qty,omitempty"`
	LimitPrice    float64   `json:"limit_price,omitempty"`
	PreviousPrice float64   `json:"previous_price,omitempty"` // amendments
	FillPrice     float64   `json:"fill_price,omitempty"`

	// Option greeks at the event (long-holder perspective, per contract)
	Delta float64 `json:"delta"`
	Gamma float64 `json:"gamma"`
	Theta float64 `json:"theta"`
	Vega  float64 `json:"vega"`
	Rho   float64 `json:"rho"`

	Market    MarketSnapshot `json:"market"`
	Portfolio MarketContext  `json:"portfolio"`

	// Closing records only
	CloseReason     string  `json:"close_reason,omitempty"`
	PremiumReceived float64 `json:"premium_received,omitempty"` // coin
	CloseCost       float64 `json:"close_cost,omitempty"`       // coin
	PnL             float64 `json:"pnl,omitempty"`              // coin, net of fees
	Fee             float64 `json:"fee,omitempty"`              // coin: what this fill was charged
	Fees            float64 `json:"fees,omitempty"`             // coin, closes: opening share + closing fee
	PnLUSD          float64 `json:"pnl_usd,omitempty"`          // PnL × spot at close
	PnLUSDFmt       string  `json:"pnl_usd_fmt,omitempty"`      // e.g. "-1,234.56"
	ROIPct          float64 `json:"roi_pct,omitempty"`
	ROIPctFmt       string  `json:"roi_pct_fmt,omitempty"` // e.g. "-7.1429%"
	HoldDays        int     `json:"hold_days,omitempty"`
	ROIAnnualized   float64 `json:"roi_annualized,omitempty"`
}

// PnLRecord is a periodic P&L line for one strategy slot, or for the whole
// strategy when Slot is nil. Realised P&L counts closes since the process
// started; the full history can be rebuilt from the "closed" records.
type PnLRecord struct {
	Timestamp     time.Time `json:"timestamp"`
	Event         string    `json:"event"` // always "pnl"
	StrategyID    string    `json:"strategy_id"`
	Slot          *SlotRef  `json:"slot,omitempty"`
	Realised      float64   `json:"realised"`   // coin
	Unrealised    float64   `json:"unrealised"` // coin, open legs marked to mid
	Total         float64   `json:"total"`      // coin
	RealisedUSD   float64   `json:"realised_usd"`
	UnrealisedUSD float64   `json:"unrealised_usd"`
	TotalUSD      float64   `json:"total_usd"`
	Spot          float64   `json:"spot"` // conversion price for the USD fields
	OpenLegs      int       `json:"open_legs"`
	ClosedLegs    int       `json:"closed_legs"`
}
