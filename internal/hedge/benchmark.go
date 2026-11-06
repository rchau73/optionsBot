package hedge

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"
)

// Benchmark: hedge vs shed, from the day the experiment starts (#657). Only
// one approach trades the account at a time, so the other is a shadow on the
// same cycles:
//   - Mode ON (real hedge): each leg the shed would have bought back is a
//     Counterfactual at the ask it would have paid; holding it is worth
//     (that price − its later price) × qty. The real perpetual's P&L is added.
//   - Mode OFF (real shed): each shed leg is a Counterfactual at its buy-back
//     price, followed at its mark until its DTE roll; a paper hedge runs the
//     same rule on the same book at the index price with the taker fee.
//
// Advantage (coin) = hedge P&L (real + paper) + Σ counterfactual values.
// Positive: the hedge did better than the shed.

// Counterfactual modes.
const (
	CounterHedged = "hedged" // mode ON: the leg was kept; the shed would have bought it
	CounterShed   = "shed"   // mode OFF: the leg was bought back; holding would have kept it
)

// Counterfactual is one shed decision and what holding the leg instead is worth.
type Counterfactual struct {
	At         time.Time `json:"at"`
	Mode       string    `json:"mode"`
	Instrument string    `json:"instrument"`
	OptionType string    `json:"option_type"`
	Qty        float64   `json:"qty"`
	ShedPrice  float64   `json:"shed_price"` // what the buy-back paid (or would have), per unit, coin
	LastPrice  float64   `json:"last_price"` // the leg's latest mark (or its final one)
	LastAt     time.Time `json:"last_at"`
	Done       bool      `json:"done"` // followed to its end (rolled, closed or expired)
}

// Value is what holding the leg instead of shedding it is worth (coin):
// positive when the option got cheaper after the shed. Nothing until the
// leg has a later price (or has expired).
func (c Counterfactual) Value() float64 {
	if c.LastPrice <= 0 && !c.Done {
		return 0
	}
	return (c.ShedPrice - c.LastPrice) * c.Qty
}

// PerpBook is an inverse perpetual position kept from its trades: the real
// hedge from its fills, the paper one (mode OFF) at the index price.
type PerpBook struct {
	SizeUSD  float64 `json:"size_usd"` // signed
	AvgPrice float64 `json:"avg_price"`
	Realised float64 `json:"realised_coin"`
	Fees     float64 `json:"fees_coin"`
	Trades   int     `json:"trades"`
}

// Trade books a paper trade of usd (signed: + buy) at price with the taker fee rate.
func (v *PerpBook) Trade(usd, price, feeRate float64) {
	v.Book(usd, price, TakerFee(usd, price, feeRate))
}

// Book books a trade of usd (signed: + buy) at price that cost fee (coin).
func (v *PerpBook) Book(usd, price, fee float64) {
	if usd == 0 || price <= 0 {
		return
	}
	v.Trades++
	v.Fees += fee
	switch {
	case v.SizeUSD == 0 || (v.SizeUSD > 0) == (usd > 0): // open or add: average in coin terms
		coin := math.Abs(v.SizeUSD)/v.avgOr(price) + math.Abs(usd)/price
		v.SizeUSD += usd
		v.AvgPrice = math.Abs(v.SizeUSD) / coin
	default: // reduce (and maybe flip)
		closed := math.Min(math.Abs(usd), math.Abs(v.SizeUSD))
		sign := math.Copysign(1, v.SizeUSD)
		v.Realised += PerpPnL(sign*closed, v.AvgPrice, price)
		v.SizeUSD += usd
		switch {
		case math.Abs(v.SizeUSD) < 1e-9:
			v.SizeUSD, v.AvgPrice = 0, 0
		case (v.SizeUSD > 0) != (sign > 0): // flipped: the rest opens at price
			v.AvgPrice = price
		}
	}
}

func (v *PerpBook) avgOr(price float64) float64 {
	if v.AvgPrice > 0 {
		return v.AvgPrice
	}
	return price
}

// PnL is the position's P&L net of fees, the open part marked at mark.
func (v PerpBook) PnL(mark float64) float64 {
	return v.Realised + PerpPnL(v.SizeUSD, v.AvgPrice, mark) - v.Fees
}

// Pricing is what the benchmark books the real perpetual at (#664).
type Pricing string

const (
	// PricingIndex (testnet): its perpetual trades about 3.5 % from the index,
	// a gap mainnet does not have, so fills and marks are booked at the index
	// and funding is left out. Orders still go out, to test the order path.
	PricingIndex Pricing = "index"
	// PricingExchange (live): the exchange's own prices, funding included.
	PricingExchange Pricing = "exchange"
)

// Note says in plain words what the benchmark is priced at.
func (p Pricing) Note() string {
	if p == PricingIndex {
		return "benchmark priced at the index (testnet): fills and marks at the index price, funding left out"
	}
	return "benchmark priced at the exchange's own fills and marks, funding included"
}

// Book is the price the ledger books at, for a trade at price while the index
// is at index.
func (p Pricing) Book(price, index float64) float64 {
	if p == PricingIndex && index > 0 {
		return index
	}
	return price
}

// Blocks reports an order the price guard stops: price more than maxPct %
// from the index. Only on the exchange's prices: at the index there is no gap
// to book. Without an index price there is nothing to compare.
func (p Pricing) Blocks(price, index, maxPct float64) bool {
	return p == PricingExchange && index > 0 && math.Abs(price-index)*100 > maxPct*index
}

// Summarise totals the ledger on this pricing: funding and mark as Deribit
// reports them, index the index price now.
func (p Pricing) Summarise(l Ledger, funding, mark, index float64) Summary {
	if p == PricingIndex {
		funding, mark = 0, p.Book(mark, index)
	}
	s := l.Summarise(funding, mark)
	s.Pricing, s.PricingNote = string(p), p.Note()
	return s
}

// Ledger is the benchmark's state, kept in the mode file so a restart (every
// deploy) does not reset it.
type Ledger struct {
	Since time.Time `json:"since"`
	// Real is the real perpetual from its fills (mode ON, and its closes);
	// Virtual the paper hedge while the mode is OFF.
	Real    PerpBook         `json:"real"`
	Virtual PerpBook         `json:"virtual"`
	Legs    []Counterfactual `json:"legs"`
}

// MaxLegs bounds the ledger's leg list (the oldest finished leg goes first).
const MaxLegs = 500

// AddLeg records a shed decision.
func (l *Ledger) AddLeg(c Counterfactual) {
	l.Legs = append(l.Legs, c)
	if len(l.Legs) <= MaxLegs {
		return
	}
	for i, old := range l.Legs {
		if old.Done {
			l.Legs = append(l.Legs[:i], l.Legs[i+1:]...)
			return
		}
	}
	l.Legs = l.Legs[1:]
}

// KeptQty is the quantity of instrument the ledger already follows as kept
// legs (mode ON). It survives a restart, so a leg is not counted twice.
func (l Ledger) KeptQty(instrument string) float64 {
	total := 0.0
	for _, c := range l.Legs {
		if c.Mode == CounterHedged && !c.Done && c.Instrument == instrument {
			total += c.Qty
		}
	}
	return total
}

// EndKept ends the kept legs of instrument, as the shed buys it back after
// all, and reports whether there were any.
func (l *Ledger) EndKept(instrument string) bool {
	ended := false
	for i := range l.Legs {
		if c := &l.Legs[i]; c.Mode == CounterHedged && !c.Done && c.Instrument == instrument {
			c.Done, ended = true, true
		}
	}
	return ended
}

// Summary is the benchmark as the monitor shows it.
type Summary struct {
	Since         time.Time        `json:"since"`
	AdvantageCoin float64          `json:"advantage_coin"` // > 0: the hedge did better than the shed
	AdvantageUSD  float64          `json:"advantage_usd"`
	HedgeCoin     float64          `json:"hedge_pnl_coin"`   // real + paper
	RealCoin      float64          `json:"real_pnl_coin"`    // the real perpetual (fills, fees, funding)
	VirtualCoin   float64          `json:"virtual_pnl_coin"` // the paper hedge (mode OFF)
	LegsCoin      float64          `json:"legs_coin"`        // Σ counterfactual values
	RealTrades    int              `json:"real_trades"`
	VirtualTrades int              `json:"virtual_trades"`
	Legs          []Counterfactual `json:"legs"`
	Note          string           `json:"note"`
	Pricing       string           `json:"pricing"`      // "index" or "exchange"
	PricingNote   string           `json:"pricing_note"` // the same in plain words
}

// BenchmarkNote says what the number is, and what it is not.
const BenchmarkNote = "Hedge P&L (real and paper, net of fees) plus what holding each shed leg was worth since the shed. " +
	"The paper hedge trades at the index price with a 0.05 % taker fee; legs are followed at their mark until their DTE roll. An estimate, not an audited result."

// Summarise totals the ledger: funding is the open perpetual's realised
// funding (coin, as Deribit reports it), mark the price for the open parts
// and for USD. The latest 50 legs are listed.
func (l Ledger) Summarise(funding, mark float64) Summary {
	s := Summary{Since: l.Since, RealTrades: l.Real.Trades, VirtualTrades: l.Virtual.Trades, Note: BenchmarkNote}
	s.VirtualCoin = l.Virtual.PnL(mark)
	s.RealCoin = l.Real.PnL(mark) + funding
	s.HedgeCoin = s.RealCoin + s.VirtualCoin
	for _, c := range l.Legs {
		s.LegsCoin += c.Value()
	}
	s.AdvantageCoin = s.HedgeCoin + s.LegsCoin
	s.AdvantageUSD = s.AdvantageCoin * mark
	start := max(0, len(l.Legs)-50)
	s.Legs = append([]Counterfactual{}, l.Legs[start:]...)
	return s
}

// ── The mode file ────────────────────────────────────────────────────────────

// Actor is who switched the mode (the monitor's signed-in person).
type Actor struct {
	User string `json:"user"`
	Role string `json:"role,omitempty"`
}

// ModeState is data/hedge_mode.json: whether the experiment is on, since
// when and by whom, and the benchmark's ledger. A missing file is OFF.
type ModeState struct {
	Enabled bool      `json:"enabled"`
	Since   time.Time `json:"since"`
	By      Actor     `json:"by"`
	Ledger  Ledger    `json:"ledger"`
}

// LoadMode reads the mode file; a missing file is the zero state (OFF).
func LoadMode(path string) (ModeState, error) {
	var st ModeState
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return ModeState{}, fmt.Errorf("hedge mode %s: %w", path, err)
	}
	return st, nil
}

// SaveMode writes the mode file atomically (temp file + rename).
func SaveMode(path string, st ModeState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
