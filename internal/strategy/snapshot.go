package strategy

import (
	"fmt"
	"math"
	"sort"
	"time"

	"optionsbot/internal/gex"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

// DefaultStrategyID names the short-strangle strategy in journals and order
// labels when the config does not set strategy_id.
const DefaultStrategyID = "short-strangle"

// atmBandPct is how close (as a fraction of spot) a strike must be to count as ATM.
const atmBandPct = 0.01

// SnapshotInput is plain data, so BuildMarketSnapshot stays a pure function.
type SnapshotInput struct {
	Now          time.Time
	Spot         float64 // index price, USD
	DVOL         float64
	IVPercentile float64
	Instrument   *marketdata.Instrument   // nil for slot-level events (e.g. a skipped entry)
	Chain        []*marketdata.Instrument // option chain, for ATM IV and open interest by strike
	OI           *gex.OISnapshot          // may be nil (before the first GEX refresh)
	GEX          *gex.Snapshot            // may be nil
}

// BuildMarketSnapshot captures the market conditions around one instrument (or
// just the market, when Instrument is nil) at in.Now.
func BuildMarketSnapshot(in SnapshotInput) orders.MarketSnapshot {
	snap := orders.MarketSnapshot{
		AsOf:         in.Now,
		Spot:         in.Spot,
		DVOL:         in.DVOL,
		IVPercentile: in.IVPercentile,
	}
	if g := in.GEX; g != nil {
		snap.GEXRegime = g.Regime
		if g.GammaFlipFound && g.GammaFlip > 0 {
			snap.GammaFlip = g.GammaFlip
			if in.Spot > 0 {
				snap.SpotToFlipPct = (in.Spot - g.GammaFlip) / g.GammaFlip * 100
			}
		}
	}

	inst := in.Instrument
	if inst == nil {
		return snap
	}
	spot := in.Spot
	if spot <= 0 {
		spot = inst.UnderlyingPrice
		snap.Spot = spot
	}

	snap.Strike = inst.Strike
	snap.Delta = inst.Greeks.Delta
	snap.OptionIV = inst.Greeks.IV
	snap.Bid, snap.Ask, snap.Mid = inst.Bid, inst.Ask, inst.Mid
	if inst.Bid > 0 && inst.Ask > 0 && inst.Mid > 0 {
		snap.SpreadPct = (inst.Ask - inst.Bid) / inst.Mid * 100
	}
	snap.DTE = math.Max(inst.Expiry.Sub(in.Now).Hours()/24, 0)

	if spot > 0 && inst.Strike > 0 {
		snap.Moneyness, snap.DistanceToStrikePct = Moneyness(inst.OptionType, spot, inst.Strike)
		snap.DistanceToStrikeSD = distanceInSD(inst.OptionType, spot, inst.Strike, inst.Greeks.IV, snap.DTE)
		snap.Intrinsic = IntrinsicCoin(inst.OptionType, spot, inst.Strike)
		if inst.Mid > 0 {
			snap.Extrinsic = inst.Mid - snap.Intrinsic
		}
	}

	expiryChain := sameExpiry(in.Chain, inst.Expiry)
	if atm := atmIV(expiryChain, inst.OptionType, spot); atm > 0 {
		snap.ATMIV = atm
		if inst.Greeks.IV > 0 {
			snap.Skew = inst.Greeks.IV - atm
		}
	}

	if in.OI != nil {
		snap.OIAsOf = in.OI.AsOf
		snap.InstrumentOI = in.OI.ByInstrument[inst.Name]
		rows := StrikeOpenInterest(expiryChain, in.OI)
		for i, r := range rows {
			snap.ExpiryOI += r.CallOI + r.PutOI
			if r.Strike == inst.Strike {
				snap.StrikeOI = r.CallOI + r.PutOI
				snap.StrikeOIRank = i + 1
			}
		}
		snap.MaxPainStrike = MaxPainStrike(rows)
	}
	return snap
}

// Moneyness labels an option ITM, ATM or OTM and returns its distance to the
// strike in % of spot: positive when out of the money, negative when in it.
func Moneyness(optionType string, spot, strike float64) (string, float64) {
	dist := (strike - spot) / spot // call is OTM when strike > spot
	if optionType == "put" {
		dist = -dist
	}
	switch {
	case math.Abs(strike-spot)/spot < atmBandPct:
		return "ATM", dist * 100
	case dist > 0:
		return "OTM", dist * 100
	default:
		return "ITM", dist * 100
	}
}

// distanceInSD expresses the out-of-the-money distance in standard deviations
// of the move implied by iv over dteDays (log-normal), the way a desk compares
// strikes across volatility regimes. Returns 0 when iv or time is unknown.
func distanceInSD(optionType string, spot, strike, iv, dteDays float64) float64 {
	t := dteDays / 365
	if iv <= 0 || t <= 0 {
		return 0
	}
	d := math.Log(strike / spot)
	if optionType == "put" {
		d = -d
	}
	return d / (iv * math.Sqrt(t))
}

// IntrinsicCoin is the intrinsic value of an inverse option in the underlying
// coin: the USD payoff divided by spot, comparable to the option's coin price.
func IntrinsicCoin(optionType string, spot, strike float64) float64 {
	if spot <= 0 {
		return 0
	}
	if optionType == "call" {
		return math.Max(spot-strike, 0) / spot
	}
	return math.Max(strike-spot, 0) / spot
}

// StrikeOI is the open interest at one strike of one expiry.
type StrikeOI struct {
	Strike float64
	CallOI float64
	PutOI  float64
}

// StrikeOpenInterest sums open interest by strike for one expiry's chain,
// sorted by total OI, largest first (rank 1 = most open interest).
func StrikeOpenInterest(expiryChain []*marketdata.Instrument, oi *gex.OISnapshot) []StrikeOI {
	byStrike := map[float64]*StrikeOI{}
	for _, inst := range expiryChain {
		v := oi.ByInstrument[inst.Name]
		if v <= 0 {
			continue
		}
		row, ok := byStrike[inst.Strike]
		if !ok {
			row = &StrikeOI{Strike: inst.Strike}
			byStrike[inst.Strike] = row
		}
		if inst.OptionType == "call" {
			row.CallOI += v
		} else {
			row.PutOI += v
		}
	}
	rows := make([]StrikeOI, 0, len(byStrike))
	for _, r := range byStrike {
		rows = append(rows, *r)
	}
	sort.Slice(rows, func(i, j int) bool {
		ti, tj := rows[i].CallOI+rows[i].PutOI, rows[j].CallOI+rows[j].PutOI
		if ti != tj {
			return ti > tj
		}
		return rows[i].Strike < rows[j].Strike
	})
	return rows
}

// MaxPainStrike returns the settlement price (among listed strikes) at which
// option holders' total payout would be smallest — where open interest says
// sellers would like expiry to land. Returns 0 without open interest.
func MaxPainStrike(rows []StrikeOI) float64 {
	best, bestPain := 0.0, math.MaxFloat64
	for _, settle := range rows {
		pain := 0.0
		for _, r := range rows {
			pain += r.CallOI*math.Max(settle.Strike-r.Strike, 0) + r.PutOI*math.Max(r.Strike-settle.Strike, 0)
		}
		if pain < bestPain || (pain == bestPain && settle.Strike < best) {
			best, bestPain = settle.Strike, pain
		}
	}
	return best
}

func sameExpiry(chain []*marketdata.Instrument, expiry time.Time) []*marketdata.Instrument {
	var out []*marketdata.Instrument
	for _, inst := range chain {
		if inst.Expiry.Equal(expiry) {
			out = append(out, inst)
		}
	}
	return out
}

// atmIV returns the mark IV of the same-type option whose strike is closest to spot.
func atmIV(expiryChain []*marketdata.Instrument, optionType string, spot float64) float64 {
	best, bestDist := 0.0, math.MaxFloat64
	for _, inst := range expiryChain {
		if inst.OptionType != optionType || inst.Greeks.IV <= 0 {
			continue
		}
		if d := math.Abs(inst.Strike - spot); d < bestDist {
			best, bestDist = inst.Greeks.IV, d
		}
	}
	return best
}

// ── Strategy helpers ─────────────────────────────────────────────────────────

func (s *Strategy) strategyID() string {
	if s.cfg.StrategyID != "" {
		return s.cfg.StrategyID
	}
	return DefaultStrategyID
}

// eventContext builds the journal context for an event on inst (nil for
// slot-level events) in slot (nil when unknown).
func (s *Strategy) eventContext(inst *marketdata.Instrument, slot *orders.SlotRef) orders.EventContext {
	in := SnapshotInput{
		Now:          time.Now(),
		Spot:         s.md.UnderlyingPrice(),
		DVOL:         s.md.DVOL(),
		IVPercentile: s.md.IVPercentile(),
		Instrument:   inst,
		GEX:          s.gamma.CurrentGEXSnapshot(),
	}
	if inst != nil {
		in.Chain = s.md.AllInstruments()
		if s.oi != nil {
			in.OI = s.oi.OpenInterest()
		}
	}
	return orders.EventContext{
		StrategyID: s.strategyID(),
		Slot:       slot,
		Market:     BuildMarketSnapshot(in),
		Portfolio:  s.marketContext(),
	}
}

// instrumentContext is eventContext for an instrument known only by name.
func (s *Strategy) instrumentContext(name string, slot *orders.SlotRef) orders.EventContext {
	inst, ok := s.md.GetInstrument(name)
	if !ok {
		inst = nil
	}
	return s.eventContext(inst, slot)
}

func slotRef(dte int, delta float64) *orders.SlotRef {
	return &orders.SlotRef{DTE: dte, Delta: delta}
}

// slotOf returns the slot of the strangle holding position posID, or nil.
func (s *Strategy) slotOf(posID string) *orders.SlotRef {
	for _, st := range s.state.AllStrangles() {
		if (st.CallLeg != nil && st.CallLeg.ID == posID) || (st.PutLeg != nil && st.PutLeg.ID == posID) {
			return slotRef(st.TargetDTE, st.EntryDelta)
		}
	}
	return nil
}

// orderLabel tags exchange orders with strategy and slot, e.g.
// "short-strangle:45d:0.16".
func (s *Strategy) orderLabel(slot *orders.SlotRef) string {
	if slot == nil {
		return s.strategyID()
	}
	return fmt.Sprintf("%s:%dd:%.2f", s.strategyID(), slot.DTE, slot.Delta)
}

// uniqueOrderLabel is orderLabel plus a per-order suffix, so one order can be
// found (and cancelled) by its label alone when its submit outcome is unknown.
// Deribit allows 64 characters; the executor truncates beyond that.
func (s *Strategy) uniqueOrderLabel(slot *orders.SlotRef) string {
	return s.orderLabel(slot) + ":" + s.state.NextID("o")
}
