// Package shadow keeps the record of every missing leg the bot's rules hold
// back (#676): the leg it would have sold, followed at its mark until the
// real rules would sell it or the exit rules would close it, for the
// full-strangle study (#677). It only records: no order, no rule and nothing
// the trading decisions read.
package shadow

import (
	"math"
	"time"

	"optionsbot/internal/hedge"
	"optionsbot/internal/orders"
)

// Line types of shadow_legs.jsonl.
const (
	EventOpen   = "open"   // a held leg starts being followed
	EventUpdate = "update" // its state, every UpdateEvery or when its hold changes
	EventClose  = "close"  // its outcome
	EventNoLeg  = "no_leg" // a held leg with no strike to follow: counted, not followed
)

// Why a shadow leg ends. The first three are the exit rules, the same that
// close a real leg; a leg they end is not opened again while the hold lasts.
const (
	ReasonStopLoss = "stop_loss"
	ReasonDTERoll  = "dte_roll"
	ReasonDelta    = "delta_exit"
	// ReasonReleased: the real rules let the leg be sold; the real leg takes over.
	ReasonReleased = "released"
	// ReasonStrangleClosed: the open leg is gone too, nothing is held any more.
	ReasonStrangleClosed = "strangle_closed"
)

// UpdateEvery is how often an open leg's state is written. It is followed
// every cycle; a line per cycle would add nothing but size.
const UpdateEvery = 5 * time.Minute

// LegKey names a held leg across restarts: the open leg's instrument and the
// missing side. Strangle and position IDs are new after every reconcile.
func LegKey(openLeg, side string) string { return openLeg + ":" + side }

// Leg is one shadow leg: what would have been sold and what it did since.
// Prices are per unit in coin; coin deltas are the perpetual's, as the futures
// hedge counts them.
type Leg struct {
	ID         string         `json:"id"`
	Key        string         `json:"key"`
	Rule       string         `json:"rule"`    // the rule that held the leg when it was opened
	Reason     string         `json:"reason"`  // that rule's words
	HeldBy     string         `json:"held_by"` // the rule holding it now
	StrangleID string         `json:"strangle_id"`
	Slot       orders.SlotRef `json:"slot"`
	OpenLeg    string         `json:"open_leg"`
	OptionType string         `json:"option_type"`
	Instrument string         `json:"instrument,omitempty"`
	Strike     float64        `json:"strike,omitempty"`
	Expiry     time.Time      `json:"expiry"`
	Qty        float64        `json:"qty"`
	OpenedAt   time.Time      `json:"opened_at"`
	DTE        int            `json:"dte"`

	Premium  float64       `json:"premium"` // the modelled fill: sold at the bid
	Bid      float64       `json:"bid"`
	Ask      float64       `json:"ask"`
	Mark     float64       `json:"mark"`
	MarkLive bool          `json:"mark_live"`
	Greeks   orders.Greeks `json:"greeks"`

	// The extra perpetual the hedge would have needed: PerpDelta holds the leg's
	// coin delta at zero, Cycles is how many cycles were followed. PerpPnLCoin
	// and PerpFeesCoin model that perpetual re-traded every cycle at spot.
	PerpDelta    float64 `json:"perp_delta"`
	PerpDeltaAvg float64 `json:"perp_delta_avg"` // mean |PerpDelta|
	PerpDeltaMax float64 `json:"perp_delta_max"` // largest |PerpDelta|
	Cycles       int     `json:"cycles"`
	PerpPnLCoin  float64 `json:"perp_pnl_coin"`
	PerpFeesCoin float64 `json:"perp_fees_coin"`
	Spot         float64 `json:"spot"` // the last spot followed

	PnLCoin float64 `json:"pnl_coin"` // premium less the mark, or less the exit once closed

	ExitReason string     `json:"exit_reason,omitempty"`
	ExitDetail string     `json:"exit_detail,omitempty"`
	ExitPrice  float64    `json:"exit_price,omitempty"`
	ClosedAt   *time.Time `json:"closed_at,omitempty"`
	DaysHeld   float64    `json:"days_held,omitempty"`
	PnLUSD     float64    `json:"pnl_usd,omitempty"`
	// NetPnLCoin is PnLCoin with the perpetual's P&L and fees, on close.
	NetPnLCoin float64 `json:"net_pnl_coin,omitempty"`
}

// Position is the leg as the exit rules read a real one (strategy.EvaluateLeg).
func (l *Leg) Position() *orders.Position {
	return &orders.Position{
		ID: l.ID, Instrument: l.Instrument, Strike: l.Strike, Expiry: l.Expiry, OptionType: l.OptionType,
		Side: orders.DirectionSell, Qty: l.Qty, EntryPrice: l.Premium, PremiumReceived: l.Premium * l.Qty,
		EntryTime: l.OpenedAt, CurrentMid: l.Mark, CurrentGreeks: l.Greeks, MarkLive: l.MarkLive,
	}
}

// Follow moves the leg to this cycle's quote: the modelled perpetual earns the
// spot's move, then is re-traded to hold the leg's new coin delta. A short
// leg's coin delta is −qty × (delta − mark); the perpetual holds the opposite.
func (l *Leg) Follow(bid, ask, mark float64, greeks orders.Greeks, spot, feeRate float64) {
	l.accruePerp(spot)
	l.Bid, l.Ask, l.Mark, l.Greeks, l.MarkLive = bid, ask, mark, greeks, true
	l.PnLCoin = (l.Premium - mark) * l.Qty
	want := (greeks.Delta - mark) * l.Qty
	l.PerpFeesCoin += hedge.TakerFee((want-l.PerpDelta)*spot, spot, feeRate)
	l.PerpDelta = want
	abs := math.Abs(want)
	l.Cycles++
	l.PerpDeltaAvg += (abs - l.PerpDeltaAvg) / float64(l.Cycles)
	l.PerpDeltaMax = math.Max(l.PerpDeltaMax, abs)
}

// accruePerp books the modelled perpetual's P&L since the last spot.
func (l *Leg) accruePerp(spot float64) {
	if l.Spot > 0 && spot > 0 {
		l.PerpPnLCoin += hedge.PerpPnL(l.PerpDelta*l.Spot, l.Spot, spot)
	}
	if spot > 0 {
		l.Spot = spot
	}
}

// Finish records the leg's outcome at exitPrice: the option bought back and
// the modelled perpetual closed.
func (l *Leg) Finish(reason, detail string, exitPrice, spot, feeRate float64, at time.Time) {
	l.accruePerp(spot)
	l.PerpFeesCoin += hedge.TakerFee(l.PerpDelta*l.Spot, l.Spot, feeRate)
	l.ExitReason, l.ExitDetail, l.ExitPrice = reason, detail, exitPrice
	l.ClosedAt = &at
	l.DaysHeld = at.Sub(l.OpenedAt).Hours() / 24
	l.PnLCoin = (l.Premium - exitPrice) * l.Qty
	l.PnLUSD = l.PnLCoin * l.Spot
	l.NetPnLCoin = l.PnLCoin + l.PerpPnLCoin - l.PerpFeesCoin
}

// BookDelta is the book's net coin delta when a line was written.
type BookDelta struct {
	Options float64 `json:"options"`
	Perp    float64 `json:"perp"`
	Net     float64 `json:"net"`
}

// Record is one line of shadow_legs.jsonl: the leg's whole state, so the last
// line of a leg is all a restart needs. Open and close lines add the market.
type Record struct {
	Event string    `json:"event"`
	At    time.Time `json:"at"`
	Leg
	Market    *orders.MarketSnapshot `json:"market,omitempty"`
	Portfolio *orders.MarketContext  `json:"portfolio,omitempty"`
	Book      *BookDelta             `json:"book_delta,omitempty"`
}
