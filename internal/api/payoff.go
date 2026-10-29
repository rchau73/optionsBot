package api

import (
	"math"
	"sort"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// The payoff chart of each open strangle (#391): what it pays at expiry and
// what it would give if bought back now, for every price of the coin, with
// the strikes, break-evens, exit lines, stop-loss levels and its delta. The
// math stays here; the monitor only draws. Served at /api/strangles/payoff
// from the strategy's view — never a Deribit call.

const (
	// PayoffModel names the model behind the "today" curve, the exit lines
	// and the stop levels.
	PayoffModel = "Black–Scholes on each leg's IV, zero rate"
	// PayoffGridPoints is how many evenly spaced prices the curves have
	// (the spot, strikes and break-evens are added to them).
	PayoffGridPoints = 121
	// PayoffRangeSigmas: the price axis reaches this many daily σ beyond the
	// outer break-evens (and never cuts off the spot or a strike).
	PayoffRangeSigmas = 3.0
	// payoffFallbackDailySigma is the daily σ (fraction of spot) used for the
	// axis only when neither DVOL nor any leg's IV is known.
	payoffFallbackDailySigma = 0.03
)

// PayoffReport is the /api/strangles/payoff payload.
type PayoffReport struct {
	AsOf       time.Time `json:"as_of"`
	Underlying string    `json:"underlying"`
	Spot       float64   `json:"spot"` // USD
	DVOL       float64   `json:"dvol"`
	Model      string    `json:"model"`
	// DeltaExitThreshold and StopLossMultiple are the rules behind the exit
	// lines and the stop levels.
	DeltaExitThreshold float64 `json:"delta_exit_threshold"`
	StopLossMultiple   float64 `json:"stop_loss_multiple"`
	// BotNetDelta is the bot's net delta from the view (coin, short-signed),
	// for comparison with each strangle's.
	BotNetDelta           float64          `json:"bot_net_delta"`
	BotNetDeltaUSDPer1Pct float64          `json:"bot_net_delta_usd_per_1pct"`
	Strangles             []StranglePayoff `json:"strangles"` // by slot DTE, then expiry
}

// StranglePayoff is one strangle's chart.
type StranglePayoff struct {
	StrangleID string         `json:"strangle_id"`
	Slot       orders.SlotRef `json:"slot"`
	Expiry     time.Time      `json:"expiry"`
	DTE        float64        `json:"dte"` // days, fractional
	Spot       float64        `json:"spot"`
	Legs       []PayoffLeg    `json:"legs"`    // short legs: put first
	Missing    []MissingLeg   `json:"missing"` // the side not held, and why
	// PremiumCoin is what the legs were paid (the most the strangle can earn);
	// UnrealisedCoin the book's P&L now (net of opening fees); EarnedPct the
	// share of the premium earned so far (null without a premium).
	PremiumCoin    float64  `json:"premium_coin"`
	UnrealisedCoin float64  `json:"unrealised_coin"`
	UnrealisedUSD  float64  `json:"unrealised_usd"`
	EarnedPct      *float64 `json:"earned_pct"`
	// Net delta of the strangle (short-signed): coin, and USD per +1 % move.
	NetDeltaCoin       float64 `json:"net_delta_coin"`
	NetDeltaUSDPer1Pct float64 `json:"net_delta_usd_per_1pct"`
	// Break-evens of the payoff at expiry (null when that side cannot lose
	// the whole premium, or is not held).
	BreakEvenLow  *float64 `json:"break_even_low"`
	BreakEvenHigh *float64 `json:"break_even_high"`
	// Today: whether the today curve, exit lines and stop levels are
	// computed; TodayReason says why not.
	Today       bool          `json:"today"`
	TodayReason string        `json:"today_reason,omitempty"`
	Points      []PayoffPoint `json:"points"` // ascending price
}

// PayoffLeg is one short leg of the chart.
type PayoffLeg struct {
	PositionID     string  `json:"position_id"`
	Instrument     string  `json:"instrument"`
	OptionType     string  `json:"option_type"`
	Strike         float64 `json:"strike"`
	Qty            float64 `json:"qty"`
	PremiumCoin    float64 `json:"premium_coin"`
	Mark           float64 `json:"mark"`  // coin per option
	IV             float64 `json:"iv"`    // fraction, 0 = unknown
	Delta          float64 `json:"delta"` // per option, as Deribit reports it
	UnrealisedCoin float64 `json:"unrealised_coin"`
	// PositionDelta is the leg's delta in coin, short-signed (−Δ × qty).
	PositionDelta     float64 `json:"position_delta"`
	PositionDeltaUSD1 float64 `json:"position_delta_usd_per_1pct"`
	StopLossMark      float64 `json:"stop_loss_mark"`
	// ExitLine: the price where |Δ| reaches delta_exit_threshold today
	// (ExitLinePrice, as How it works); StopLevel: the price where the leg's
	// mark reaches stop_loss_mark on the same model. Null when not computable.
	ExitLine  *float64 `json:"exit_line"`
	StopLevel *float64 `json:"stop_level"`
}

// MissingLeg is the side a one-legged strangle does not hold.
type MissingLeg struct {
	OptionType string `json:"option_type"`
	Rule       string `json:"rule"`   // held_legs' Hold* rule; "" = repair may sell it now
	Reason     string `json:"reason"` // held_legs' reason, or why it is unknown
}

// PayoffPoint is one price of the curves, in coin. TodayPnL is null when the
// today curve is not computed (Today false).
type PayoffPoint struct {
	Price     float64  `json:"price"`
	ExpiryPnL float64  `json:"expiry_pnl"`
	TodayPnL  *float64 `json:"today_pnl"`
}

// PayoffOf is the pure core of /api/strangles/payoff.
func PayoffOf(v strategy.View, now time.Time) PayoffReport {
	spot := v.Market.Spot
	rep := PayoffReport{
		AsOf: now, Underlying: v.Underlying, Spot: spot, DVOL: v.Market.DVOL, Model: PayoffModel,
		DeltaExitThreshold: v.DeltaExitThreshold, StopLossMultiple: v.StopLossMultiple,
		BotNetDelta: v.Greeks.NetDelta, BotNetDeltaUSDPer1Pct: v.Greeks.NetDelta * spot / 100,
		Strangles: []StranglePayoff{},
	}
	for _, st := range v.Strangles {
		if sp, ok := stranglePayoff(st, v, now); ok {
			rep.Strangles = append(rep.Strangles, sp)
		}
	}
	sort.SliceStable(rep.Strangles, func(i, j int) bool {
		a, b := rep.Strangles[i], rep.Strangles[j]
		if a.Slot.DTE != b.Slot.DTE {
			return a.Slot.DTE < b.Slot.DTE
		}
		return a.Expiry.Before(b.Expiry)
	})
	return rep
}

// stranglePayoff charts one strangle; false when it holds no short leg.
func stranglePayoff(st strategy.StrangleView, v strategy.View, now time.Time) (StranglePayoff, bool) {
	spot := v.Market.Spot
	sp := StranglePayoff{StrangleID: st.ID, Slot: st.Slot, Spot: spot, Legs: []PayoffLeg{}, Missing: []MissingLeg{}, Points: []PayoffPoint{}}
	var put, call *PayoffLeg
	for _, l := range st.Legs {
		if l.Side == orders.DirectionBuy || l.Qty <= 0 || l.Strike <= 0 {
			continue // the chart is of the short strangle
		}
		leg := PayoffLeg{
			PositionID: l.PositionID, Instrument: l.Instrument, OptionType: l.OptionType, Strike: l.Strike, Qty: l.Qty,
			PremiumCoin: l.PremiumReceived, Mark: l.Mark, IV: l.Greeks.IV, Delta: l.Greeks.Delta,
			UnrealisedCoin: l.UnrealisedPnL, PositionDelta: -l.Greeks.Delta * l.Qty, StopLossMark: l.StopLossMark,
		}
		leg.PositionDeltaUSD1 = leg.PositionDelta * spot / 100
		sp.Legs = append(sp.Legs, leg)
		if sp.Expiry.IsZero() || l.Expiry.Before(sp.Expiry) {
			sp.Expiry = l.Expiry
		}
	}
	if len(sp.Legs) == 0 {
		return sp, false
	}
	sort.SliceStable(sp.Legs, func(i, j int) bool { return sp.Legs[i].OptionType == "put" && sp.Legs[j].OptionType != "put" })
	for i := range sp.Legs {
		l := &sp.Legs[i]
		sp.PremiumCoin += l.PremiumCoin
		sp.UnrealisedCoin += l.UnrealisedCoin
		sp.NetDeltaCoin += l.PositionDelta
		switch l.OptionType {
		case "put":
			put = l
		case "call":
			call = l
		}
	}
	sp.UnrealisedUSD = sp.UnrealisedCoin * spot
	sp.NetDeltaUSDPer1Pct = sp.NetDeltaCoin * spot / 100
	sp.DTE = math.Max(sp.Expiry.Sub(now).Hours()/24, 0)
	if sp.PremiumCoin > 0 {
		pct := sp.UnrealisedCoin / sp.PremiumCoin * 100
		sp.EarnedPct = &pct
	}
	sp.Missing = missingLegs(st.ID, put, call, v.HeldLegs)

	var putK, putQ, callK, callQ float64
	if put != nil {
		putK, putQ = put.Strike, put.Qty
	}
	if call != nil {
		callK, callQ = call.Strike, call.Qty
	}
	sp.BreakEvenLow, sp.BreakEvenHigh = BreakEvens(putK, putQ, callK, callQ, sp.PremiumCoin)

	years := sp.Expiry.Sub(now).Hours() / 24 / 365
	sp.TodayReason = todayReason(sp.Legs, spot, years)
	sp.Today = sp.TodayReason == ""
	if sp.Today {
		for i := range sp.Legs {
			setLegLines(&sp.Legs[i], spot, years, v.DeltaExitThreshold)
		}
	}
	sp.Points = payoffPoints(sp, years, dailySigma(v.Market.DVOL, sp.Legs))
	return sp, true
}

// missingLegs names the side a one-legged strangle lacks, with held_legs'
// rule and reason for it.
func missingLegs(id string, put, call *PayoffLeg, held []strategy.HeldLegView) []MissingLeg {
	out := []MissingLeg{}
	for _, side := range []string{"put", "call"} {
		if (side == "put" && put != nil) || (side == "call" && call != nil) {
			continue
		}
		m := MissingLeg{OptionType: side, Reason: "unknown: no hold reported for it"}
		for _, h := range held {
			if h.StrangleID == id && h.Missing == side {
				m.Rule, m.Reason = h.Rule, h.Reason
				break
			}
		}
		out = append(out, m)
	}
	return out
}

// todayReason is why the today curve cannot be drawn; "" when it can.
func todayReason(legs []PayoffLeg, spot, years float64) string {
	switch {
	case spot <= 0:
		return "unknown: no spot price"
	case years <= 0:
		return "unknown: the strangle is at expiry"
	}
	for _, l := range legs {
		if l.IV <= 0 {
			return "unknown: no implied volatility for " + l.Instrument
		}
	}
	return ""
}

// setLegLines sets a leg's exit line and stop level (callers checked IV,
// spot and time left).
func setLegLines(l *PayoffLeg, spot, years, deltaExit float64) {
	if deltaExit > 0 && deltaExit < 1 {
		p := ExitLinePrice(l.OptionType, l.Strike, years, l.IV, deltaExit)
		l.ExitLine = &p
	}
	if l.StopLossMark > 0 {
		l.StopLevel = stopLevel(*l, spot, years)
	}
}

// optionCoin is one option's Black–Scholes value in coin at price s.
func optionCoin(optType string, s, strike, years, iv float64) float64 {
	return BlackScholesUSD(optType, s, strike, years, iv) / s
}

// legMarkAt is the leg's modelled mark (coin per option) at price s: the
// model's change from spot added to today's mark, so at spot it is the mark.
func legMarkAt(l PayoffLeg, s, spot, years float64) float64 {
	return l.Mark + optionCoin(l.OptionType, s, l.Strike, years, l.IV) - optionCoin(l.OptionType, spot, l.Strike, years, l.IV)
}

// legTodayAt is the leg's P&L (coin) if bought back now at price s: the
// book's unrealised P&L, moved by the model's change in its value.
func legTodayAt(l PayoffLeg, s, spot, years float64) float64 {
	return l.UnrealisedCoin - l.Qty*(legMarkAt(l, s, spot, years)-l.Mark)
}

// legExpiryAt is the leg's coin-settled payoff at expiry at price s
// (premium − qty × intrinsic ÷ s, as How it works' payoff).
func legExpiryAt(l PayoffLeg, s float64) float64 {
	return l.PremiumCoin - l.Qty*BlackScholesUSD(l.OptionType, s, l.Strike, 0, 0)/s
}

// stopLevel solves legMarkAt = stop_loss_mark by bisection: a call's value
// in coin rises with the price, a put's falls. Nil when the level lies
// beyond a tenth or ten times the spot.
func stopLevel(l PayoffLeg, spot, years float64) *float64 {
	f := func(s float64) float64 { return legMarkAt(l, s, spot, years) - l.StopLossMark }
	lo, hi := spot/10, spot*10
	flo, fhi := f(lo), f(hi)
	if flo*fhi > 0 {
		return nil
	}
	for i := 0; i < 100 && hi-lo > spot*1e-9; i++ {
		mid := (lo + hi) / 2
		if fm := f(mid); (fm > 0) == (flo > 0) {
			lo, flo = mid, fm
		} else {
			hi = mid
		}
	}
	p := (lo + hi) / 2
	return &p
}

// dailySigma is one daily σ as a fraction of spot: from DVOL, else the
// legs' mean IV, else a fallback (for the axis range only).
func dailySigma(dvol float64, legs []PayoffLeg) float64 {
	annual := dvol / 100
	if annual <= 0 {
		var sum float64
		var n int
		for _, l := range legs {
			if l.IV > 0 {
				sum += l.IV
				n++
			}
		}
		if n == 0 {
			return payoffFallbackDailySigma
		}
		annual = sum / float64(n)
	}
	return annual / math.Sqrt(365)
}

// payoffPoints is the curves over the price axis: PayoffGridPoints evenly
// spaced prices plus the spot, strikes and break-evens, ascending.
func payoffPoints(sp StranglePayoff, years, sigma float64) []PayoffPoint {
	inner := []float64{}
	add := func(p *float64) {
		if p != nil && *p > 0 {
			inner = append(inner, *p)
		}
	}
	if sp.Spot > 0 {
		inner = append(inner, sp.Spot)
	}
	near := func(p *float64) { // a line far out does not stretch the axis
		if p != nil && sp.Spot > 0 && *p >= sp.Spot/2 && *p <= sp.Spot*2 {
			add(p)
		}
	}
	for _, l := range sp.Legs {
		inner = append(inner, l.Strike)
		near(l.ExitLine)
		near(l.StopLevel)
	}
	add(sp.BreakEvenLow)
	add(sp.BreakEvenHigh)
	lo, hi := inner[0], inner[0]
	for _, p := range inner {
		lo, hi = math.Min(lo, p), math.Max(hi, p)
	}
	lo *= math.Max(1-PayoffRangeSigmas*sigma, 0.05)
	hi *= 1 + PayoffRangeSigmas*sigma

	prices := append([]float64{}, inner...)
	for i := range PayoffGridPoints {
		prices = append(prices, lo+(hi-lo)*float64(i)/float64(PayoffGridPoints-1))
	}
	sort.Float64s(prices)
	out := make([]PayoffPoint, 0, len(prices))
	for i, s := range prices {
		if i > 0 && s-prices[i-1] < 1e-9*hi {
			continue // the spot or a strike on a grid price
		}
		pt := PayoffPoint{Price: s}
		var today float64
		for _, l := range sp.Legs {
			pt.ExpiryPnL += legExpiryAt(l, s)
			if sp.Today {
				today += legTodayAt(l, s, sp.Spot, years)
			}
		}
		if sp.Today {
			pt.TodayPnL = &today
		}
		out = append(out, pt)
	}
	return out
}
