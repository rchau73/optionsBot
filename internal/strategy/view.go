package strategy

import (
	"sort"
	"sync"
	"time"

	"optionsbot/internal/orders"
)

// View is a read-only picture of the strategy for the monitor API. It is
// assembled from snapshots and values the decision loop publishes, so
// building it never touches the exchange and never races the loop.
type View struct {
	AsOf        time.Time `json:"as_of"`
	StrategyID  string    `json:"strategy_id"`
	Underlying  string    `json:"underlying"`
	Environment string    `json:"environment"`
	Halted      bool      `json:"halted"` // kill switch has fired
	LoopAt      time.Time `json:"loop_at"`

	Market  orders.MarketSnapshot `json:"market"`
	Trend   string                `json:"trend"`
	Account AccountView           `json:"account"`
	Greeks  orders.MarketContext  `json:"greeks"` // net portfolio greeks (short-signed)

	Strangles []StrangleView `json:"strangles"`
	Pending   []PendingView  `json:"pending"`
	PnL       []PnLView      `json:"pnl"` // per slot, last entry = strategy total
}

// AccountView is the last account summary the decision loop fetched.
type AccountView struct {
	Equity        float64   `json:"equity"`
	MarginUsed    float64   `json:"margin_used"`
	MarginAllowed float64   `json:"margin_allowed"`
	AsOf          time.Time `json:"as_of"`
}

// StrangleView is one strangle and its open legs.
type StrangleView struct {
	ID       string         `json:"id"`
	Slot     orders.SlotRef `json:"slot"`
	OpenedAt time.Time      `json:"opened_at"`
	Legs     []LegView      `json:"legs"`
}

// LegView is one open short option.
type LegView struct {
	PositionID      string        `json:"position_id"`
	Instrument      string        `json:"instrument"`
	OptionType      string        `json:"option_type"`
	Strike          float64       `json:"strike"`
	Expiry          time.Time     `json:"expiry"`
	DTE             float64       `json:"dte"`
	Qty             float64       `json:"qty"`
	EntryPrice      float64       `json:"entry_price"`
	Mark            float64       `json:"mark"`
	PremiumReceived float64       `json:"premium_received"`
	UnrealisedPnL   float64       `json:"unrealised_pnl"`
	ROIPct          float64       `json:"roi_pct"`        // share of premium captured, %
	LossMultiple    float64       `json:"loss_multiple"`  // loss ÷ premium; stop-loss fires at stop_loss_multiplier
	StopLossMark    float64       `json:"stop_loss_mark"` // mark price at which the stop-loss fires
	Moneyness       string        `json:"moneyness"`
	DistancePct     float64       `json:"distance_to_strike_pct"`
	Greeks          orders.Greeks `json:"greeks"`
}

// PendingView is an entry or repair order still working on the book.
type PendingView struct {
	ID          string           `json:"id"`
	Slot        orders.SlotRef   `json:"slot"`
	Repair      bool             `json:"repair"`
	SubmittedAt time.Time        `json:"submitted_at"`
	Adjustments int              `json:"adjustments"`
	Legs        []PendingLegView `json:"legs"`
}

// PendingLegView is one working order.
type PendingLegView struct {
	OrderID    string  `json:"order_id"`
	Instrument string  `json:"instrument"`
	OptionType string  `json:"option_type"`
	Qty        float64 `json:"qty"`
	FilledQty  float64 `json:"filled_qty"`
	LimitPrice float64 `json:"limit_price"`
	Done       bool    `json:"done"`
}

// PnLView is PnLLine with JSON names; Slot is nil for the strategy total.
type PnLView struct {
	Slot       *orders.SlotRef `json:"slot"`
	Realised   float64         `json:"realised"`
	Unrealised float64         `json:"unrealised"`
	Total      float64         `json:"total"`
	OpenLegs   int             `json:"open_legs"`
	ClosedLegs int             `json:"closed_legs"`
}

// published holds what only the decision loop may compute (it mutates
// pending legs and the gamma monitor without locks); the loop copies it here
// at the end of every cycle for readers on other goroutines.
type published struct {
	mu      sync.RWMutex
	at      time.Time
	trend   string
	pending []PendingView
	account AccountView
	halted  bool
}

// publish copies the loop-owned state for readers. Call on the Run goroutine.
func (s *Strategy) publish() {
	pending := make([]PendingView, 0)
	for _, ps := range s.pendingSnapshot() {
		pv := PendingView{
			ID: ps.id, Slot: *ps.slot(), Repair: ps.repairStrangleID != "",
			SubmittedAt: ps.submittedAt, Adjustments: ps.adjustments,
		}
		for _, l := range ps.legs() {
			pv.Legs = append(pv.Legs, PendingLegView{
				OrderID: l.orderID, Instrument: l.instrument, OptionType: l.optionType,
				Qty: l.qty, FilledQty: l.filledQty, LimitPrice: l.limitPrice, Done: l.done,
			})
		}
		pending = append(pending, pv)
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].SubmittedAt.Before(pending[j].SubmittedAt) })
	trend := s.gamma.Trend()

	s.pub.mu.Lock()
	defer s.pub.mu.Unlock()
	s.pub.at = time.Now()
	s.pub.trend = trend
	s.pub.pending = pending
}

// recordAccount stores the latest account summary for the view.
func (s *Strategy) recordAccount(equity, marginUsed float64) {
	s.pub.mu.Lock()
	defer s.pub.mu.Unlock()
	s.pub.account = AccountView{
		Equity: equity, MarginUsed: marginUsed,
		MarginAllowed: s.marginGuard.AllowedMargin(equity), AsOf: time.Now(),
	}
}

func (s *Strategy) setHalted() {
	s.pub.mu.Lock()
	defer s.pub.mu.Unlock()
	s.pub.halted = true
}

// View returns the current read-only picture of the strategy. Safe to call
// from any goroutine; it never calls the exchange.
func (s *Strategy) View() View {
	s.pub.mu.RLock()
	pub := published{at: s.pub.at, trend: s.pub.trend, pending: s.pub.pending, account: s.pub.account, halted: s.pub.halted}
	s.pub.mu.RUnlock()

	now := time.Now()
	v := View{
		AsOf:        now,
		StrategyID:  s.strategyID(),
		Underlying:  s.cfg.Underlying,
		Environment: s.cfg.Environment,
		Halted:      pub.halted,
		LoopAt:      pub.at,
		Market: BuildMarketSnapshot(SnapshotInput{
			Now: now, Spot: s.md.UnderlyingPrice(), DVOL: s.md.DVOL(), IVPercentile: s.md.IVPercentile(),
			GEX: s.gamma.CurrentGEXSnapshot(),
		}),
		Trend:   pub.trend,
		Account: pub.account,
		Greeks: orders.MarketContext{
			Trend:    pub.trend,
			NetDelta: s.state.TotalNetDelta(),
			NetGamma: s.state.NetGamma(),
			NetVega:  s.state.NetVega(),
			NetTheta: s.state.NetTheta(),
		},
		Strangles: s.strangleViews(now),
		Pending:   pub.pending,
	}
	if v.Pending == nil {
		v.Pending = []PendingView{}
	}
	for _, l := range s.PnLReport() {
		v.PnL = append(v.PnL, PnLView{
			Slot: l.Slot, Realised: l.Realised, Unrealised: l.Unrealised,
			Total: l.Realised + l.Unrealised, OpenLegs: l.OpenLegs, ClosedLegs: l.ClosedLegs,
		})
	}
	return v
}

func (s *Strategy) strangleViews(now time.Time) []StrangleView {
	spot := s.md.UnderlyingPrice()
	out := make([]StrangleView, 0)
	for _, st := range s.state.AllStrangles() {
		sv := StrangleView{ID: st.ID, Slot: orders.SlotRef{DTE: st.TargetDTE, Delta: st.EntryDelta}, OpenedAt: st.OpenedAt}
		for _, leg := range []*orders.Position{st.CallLeg, st.PutLeg} {
			if pos := s.livePosition(leg); pos != nil {
				sv.Legs = append(sv.Legs, s.legView(pos, spot, now))
			}
		}
		if len(sv.Legs) > 0 {
			out = append(out, sv)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Slot.DTE != out[j].Slot.DTE {
			return out[i].Slot.DTE < out[j].Slot.DTE
		}
		return out[i].Slot.Delta < out[j].Slot.Delta
	})
	return out
}

func (s *Strategy) legView(pos *orders.Position, spot float64, now time.Time) LegView {
	lv := LegView{
		PositionID: pos.ID, Instrument: pos.Instrument, OptionType: pos.OptionType,
		Strike: pos.Strike, Expiry: pos.Expiry, DTE: maxf(pos.Expiry.Sub(now).Hours()/24, 0),
		Qty: pos.Qty, EntryPrice: pos.EntryPrice, Mark: pos.CurrentMid,
		PremiumReceived: pos.PremiumReceived, UnrealisedPnL: pos.MtMPnL(),
		ROIPct: pos.ROIPct() * 100, LossMultiple: pos.LossPct(), Greeks: pos.CurrentGreeks,
	}
	if pos.Qty > 0 {
		// LossPct = (mark × qty − premium) / premium reaches the multiplier at this mark.
		lv.StopLossMark = pos.PremiumReceived * (1 + s.cfg.StopLossMultiplier) / pos.Qty
	}
	if spot > 0 && pos.Strike > 0 {
		lv.Moneyness, lv.DistancePct = Moneyness(pos.OptionType, spot, pos.Strike)
	}
	return lv
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
