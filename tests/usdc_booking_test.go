package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/history"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/perf"
	"optionsbot/internal/strategy"
)

// A SOL/XRP bot trades USDC-settled linear options (#832): prices, premium,
// fees, P&L and the premium floor are USDC, orders are whole contracts, and
// the journal names the unit. BTC/ETH stay as they were.

// newLinearFixture turns the strategy fixture into a coin's USDC bot: its
// call and put renamed "<COIN>_USDC-<date>-<strike>-C/P", settled in USDC,
// contractSize coin per contract, quoted at bid/ask USDC, a 5,000 USDC
// account. imPerUnit is the fake's margin per 0.1 coin per leg pair.
func newLinearFixture(t *testing.T, coin string, spot, callStrike, putStrike float64, strikeName func(float64) string,
	contractSize, bid, ask, imPerUnit float64) *strategyFixture {
	t.Helper()
	f := newStrategyFixture(t)
	label := strings.ToUpper(f.expiry.Format("2Jan06"))
	f.cfg.Underlying = coin
	f.call = fmt.Sprintf("%s_USDC-%s-%s-C", coin, label, strikeName(callStrike))
	f.put = fmt.Sprintf("%s_USDC-%s-%s-P", coin, label, strikeName(putStrike))
	inst := func(name, typ string, strike, delta float64) *marketdata.Instrument {
		return &marketdata.Instrument{
			Name: name, Underlying: coin, Settlement: "USDC", ContractSize: contractSize,
			Strike: strike, Expiry: f.expiry, OptionType: typ, TickSize: 0.0001, MinTradeAmount: 0.1,
			Bid: bid, Ask: ask, Mid: (bid + ask) / 2, UnderlyingPrice: spot,
			Greeks: marketdata.Greeks{Delta: delta}, UpdatedAt: time.Now(),
		}
	}
	f.market.price = spot
	f.market.instruments = map[string]*marketdata.Instrument{
		f.call: inst(f.call, "call", callStrike, 0.16),
		f.put:  inst(f.put, "put", putStrike, -0.16),
	}
	f.exch.summary = orders.AccountSummary{Currency: "USDC", Equity: 5000, AvailableFunds: 5000, MarginBalance: 5000}
	f.exch.imPerLot = imPerUnit
	return f
}

func wholeStrike(k float64) string { return fmt.Sprintf("%g", k) }

// XRP's strikes carry a "d" for the decimal point: 2.2 → "2d2".
func xrpStrike(k float64) string { return strings.Replace(fmt.Sprintf("%g", k), ".", "d", 1) }

// Scenario: a SOL strangle is booked in USDC.
func TestUSDC_SOLStrangleIsBookedInUSDC(t *testing.T) {
	// One 10-SOL contract a leg fits the 1,000 USDC slot share 1.5 times.
	f := newLinearFixture(t, "SOL", 200, 220, 180, wholeStrike, 10, 1.10, 1.30, 1000/(1.5*100))
	f.startRun()

	eventually(t, 2*time.Second, "both legs submitted", func() bool { return len(f.exch.sells()) == 2 })
	for _, o := range f.exch.sells() {
		if o.Qty != 10 {
			t.Errorf("%s qty = %v, want one 10-SOL contract", o.Instrument, o.Qty)
		}
	}
	f.exch.fill(f.exch.orderIDFor(f.call, 0), 10, 1.20)
	f.exch.fill(f.exch.orderIDFor(f.put, 0), 10, 1.10)
	eventually(t, 2*time.Second, "strangle active", func() bool { return len(f.state.AllStrangles()) == 1 })
	var premium float64
	for _, p := range f.state.AllPositions() {
		premium += p.PremiumReceived
	}
	if math.Abs(premium-23.0) > 1e-9 {
		t.Errorf("premium received = %v USDC, want 23.0 (10 × 1.20 + 10 × 1.10)", premium)
	}

	// Fees follow the linear formula in USDC: 0.03 % of the 200 USDC index per
	// SOL (0.06), capped at 12.5 % of the price.
	callFee := orders.LinearOptionFee(200, 1.20, 10)
	if math.Abs(callFee-0.6) > 1e-12 {
		t.Errorf("call fee = %v USDC, want 0.6", callFee)
	}

	// The journal's P&L and _usd fields are USDC, with no conversion.
	var buf bytes.Buffer
	journal := orders.NewWriterLogger(&buf, 0)
	open := &orders.Position{Instrument: f.call, OptionType: "call", Qty: 10, PremiumReceived: 12, Fees: callFee,
		EntryTime: time.Now().Add(-48 * time.Hour), UnderlyingPrice: 200}
	closeFee := orders.LinearOptionFee(200, 0.50, 10)
	journal.LogOpen(open, orders.Fill{OrderID: "o-1", FillPrice: 1.20, Qty: 10, Fee: callFee}, orders.TriggerEntry,
		orders.EventContext{Market: orders.MarketSnapshot{Spot: 200}})
	journal.LogClose(open, orders.Fill{OrderID: "o-2", FillPrice: 0.50, Qty: 10, Fee: closeFee}, orders.TriggerRolloutROI, orders.TypeLimit,
		orders.EventContext{Market: orders.MarketSnapshot{Spot: 200}})
	lines := decodeJournal(t, &buf)
	if len(lines) != 2 {
		t.Fatalf("%d journal lines, want open and close", len(lines))
	}
	for _, l := range lines {
		if l.Unit != "USDC" {
			t.Errorf("%s line unit = %q, want USDC", l.Event, l.Unit)
		}
	}
	closed := lines[1]
	wantPnL := 12 - 0.50*10 - callFee - closeFee // 5.8 USDC
	if math.Abs(closed.PnL-wantPnL) > 1e-9 || math.Abs(closed.PnLUSD-wantPnL) > 1e-9 {
		t.Errorf("close pnl = %v, pnl_usd = %v; want both %v USDC (no × spot)", closed.PnL, closed.PnLUSD, wantPnL)
	}
}

// What must not change: a BTC close has no unit and its pnl_usd is pnl × spot.
func TestUSDC_BTCCloseKeepsCoinAndSpotConversion(t *testing.T) {
	var buf bytes.Buffer
	journal := orders.NewWriterLogger(&buf, 0)
	pos := &orders.Position{Instrument: "BTC-25DEC26-110000-C", OptionType: "call", Qty: 0.1, PremiumReceived: 0.002,
		EntryTime: time.Now().Add(-24 * time.Hour)}
	journal.LogClose(pos, orders.Fill{OrderID: "o-1", FillPrice: 0.01, Qty: 0.1}, orders.TriggerRolloutROI, orders.TypeLimit,
		orders.EventContext{Market: orders.MarketSnapshot{Spot: 100000}})
	l := decodeJournal(t, &buf)[0]
	if l.Unit != "" || strings.Contains(buf.String(), `"unit"`) {
		t.Errorf("a BTC line must carry no unit: %s", buf.String())
	}
	if math.Abs(l.PnLUSD-l.PnL*100000) > 1e-6 {
		t.Errorf("pnl_usd = %v, want pnl %v × spot 100000", l.PnLUSD, l.PnL)
	}
}

// Scenario: the premium floor is in USDC.
func TestUSDC_PremiumFloorIsInUSDC(t *testing.T) {
	f := newLinearFixture(t, "SOL", 200, 220, 180, wholeStrike, 10, 0.4, 0.6, 1000/(1.5*100))
	f.cfg.MinPremiumBTC = 0.5
	f.cfg.EntryPriceFloor = strategy.PriceFloorBid // the step-down can reach the 0.4 bid
	f.startRun()

	eventually(t, 2*time.Second, "entry skipped below the floor", func() bool {
		for _, s := range f.journal.events(orders.EventSkipped) {
			if strings.HasPrefix(s.reason, strategy.SkipEntryRejected) &&
				strings.Contains(s.reason, "0.400000 USDC below floor 0.500000 USDC") {
				return true
			}
		}
		return false
	})
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("%d orders sent under the floor, want none", n)
	}
}

// What must not change: a BTC floor message names no unit.
func TestUSDC_BTCPremiumFloorTextUnchanged(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.MinPremiumBTC = 0.05 // above the fixture's 0.021 ask
	f.startRun()
	eventually(t, 2*time.Second, "entry skipped below the floor", func() bool {
		for _, s := range f.journal.events(orders.EventSkipped) {
			if strings.Contains(s.reason, "premium 0.021000 below floor 0.050000 — skipping") {
				return true
			}
		}
		return false
	})
}

// Scenario: XRP orders are whole contracts.
func TestUSDC_XRPOrdersAreWholeContracts(t *testing.T) {
	// The 1,000 USDC slot share fits 2,700 XRP; the order is 2 contracts.
	f := newLinearFixture(t, "XRP", 2, 2.2, 1.8, xrpStrike, 1000, 0.019, 0.021, 1000*0.1/2700)
	f.startRun()

	eventually(t, 2*time.Second, "both legs submitted", func() bool { return len(f.exch.sells()) == 2 })
	for _, o := range f.exch.sells() {
		if o.Qty != 2000 {
			t.Errorf("%s qty = %v, want 2,000 XRP (2 whole 1,000-XRP contracts of 2,700)", o.Instrument, o.Qty)
		}
	}
}

func TestUSDC_OrderStep(t *testing.T) {
	for _, tc := range []struct {
		name string
		inst marketdata.Instrument
		want float64
	}{
		{"BTC: the exchange minimum, contract size ignored", marketdata.Instrument{Settlement: "BTC", ContractSize: 1, MinTradeAmount: 0.1}, 0.1},
		{"XRP: one 1,000-XRP contract", marketdata.Instrument{Settlement: "USDC", ContractSize: 1000, MinTradeAmount: 1}, 1000},
		{"SOL: one 10-SOL contract", marketdata.Instrument{Settlement: "USDC", ContractSize: 10, MinTradeAmount: 0.1}, 10},
		{"USDC without a contract size (read from a name): the minimum", marketdata.Instrument{Settlement: "USDC", MinTradeAmount: 0.1}, 0.1},
	} {
		if got := tc.inst.OrderStep(); got != tc.want {
			t.Errorf("%s: OrderStep = %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := orders.FloorToStep(2700, 1000); got != 2000 {
		t.Errorf("2,700 XRP on a 1,000 grid = %v, want 2,000", got)
	}
}

// Deribit's option fee, pinned: 0.03 % of the underlying per unit, capped at
// 12.5 % of the option price; the linear one in USDC on the USDC index.
func TestUSDC_LinearOptionFee(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		index, price, qty, want float64
	}{
		{"SOL: 0.03 % of 200 per SOL", 200, 1.20, 10, 0.6},
		{"SOL: capped at 12.5 % of a 0.4 price", 200, 0.40, 10, 0.5},
		{"XRP: 0.03 % of 2 per XRP", 2, 0.02, 2000, 1.2},
		{"XRP: capped at 12.5 % of a 0.002 price", 2, 0.002, 2000, 0.5},
	} {
		if got := orders.LinearOptionFee(tc.index, tc.price, tc.qty); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s: fee = %v USDC, want %v", tc.name, got, tc.want)
		}
	}
	if orders.OptionFeeRate != 0.0003 || orders.OptionFeeCap != 0.125 {
		t.Errorf("fee terms = %v, %v; Deribit's are 0.0003 and 0.125", orders.OptionFeeRate, orders.OptionFeeCap)
	}
}

func TestUSDC_SettlementOfAName(t *testing.T) {
	for name, want := range map[string]string{
		"SOL_USDC-25DEC26-80-C":  "USDC",
		"XRP_USDC-25DEC26-2d2-P": "USDC",
		"BTC-25DEC26-110000-C":   "BTC",
		"ETH-25DEC26-3000-P":     "ETH",
	} {
		if got := marketdata.SettlementOf(name); got != want {
			t.Errorf("SettlementOf(%s) = %q, want %q", name, got, want)
		}
	}
	if marketdata.USDPerUnit("USDC", 150) != 1 || marketdata.USDPerUnit("BTC", 100000) != 100000 {
		t.Error("USDPerUnit: USDC is the dollar, a coin is worth spot")
	}
}

func TestUSDC_ConfigNamesThePremiumUnit(t *testing.T) {
	for coin, unit := range map[string]string{"SOL": "USDC", "XRP": "USDC", "BTC": "BTC", "ETH": "ETH"} {
		c := &config.Config{Underlying: coin}
		if got := c.PremiumUnit(); got != unit {
			t.Errorf("%s: PremiumUnit = %q, want %q", coin, got, unit)
		}
	}
	cfg := validConfigForUnit(t, "SOL")
	cfg.MinPremiumBTC = -0.5
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "min_premium -0.5 USDC") {
		t.Errorf("Validate = %v, want min_premium refused in USDC", err)
	}
}

// validConfigForUnit is the shipped BTC config moved to coin.
func validConfigForUnit(t *testing.T, coin string) *config.Config {
	t.Helper()
	cfg, err := config.Load("../config_btc.yaml")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Underlying = coin
	return cfg
}

// The P&L history and the performance summary of a USDC bot read its unit:
// the USD figures equal the USDC ones, the coin's spot is the hold benchmark.
func TestUSDC_PerformanceReadsTheUnit(t *testing.T) {
	h, err := history.Open(t.TempDir()+"/pnl.jsonl", 30*24*time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	h.SetPnLUnit(history.LinearPnLUnit)
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	h.RecordPnL(t0, 0, 0, 200, nil)
	h.RecordPnL(t0.Add(24*time.Hour), 50, 10, 220, nil)
	pts := h.Points(t0.Add(-time.Hour), t0.Add(48*time.Hour))
	if last := pts[len(pts)-1]; last.PnLUnit != "USDC" || last.TotalUSD != 60 || last.RealisedUSD != 50 {
		t.Errorf("point = %+v, want pnl_unit USDC and total_usd 60 (no × spot)", last)
	}
	s := perf.Summarize(pts, 5000, t0, t0.Add(24*time.Hour))
	if s.Unit != "USDC" || s.NetPnLUSD != 60 {
		t.Errorf("summary unit %q, net_usd %v; want USDC and 60", s.Unit, s.NetPnLUSD)
	}
	if math.Abs(s.ReturnUSDPct-1.2) > 1e-9 || math.Abs(s.HoldReturnUSDPct-10) > 1e-9 {
		t.Errorf("return %v %%, hold %v %%; want 1.2 (60 of 5,000 USDC) and 10 (SOL 200 → 220)", s.ReturnUSDPct, s.HoldReturnUSDPct)
	}
}

func decodeJournal(t *testing.T, buf *bytes.Buffer) []orders.OrderLog {
	t.Helper()
	var out []orders.OrderLog
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var l orders.OrderLog
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			t.Fatalf("journal line %q: %v", line, err)
		}
		out = append(out, l)
	}
	return out
}
