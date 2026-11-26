package tests

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/hedge"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// The futures hedge on a linear USDC perpetual (#836): a SOL/XRP book's
// delta is held in the coin with <COIN>_USDC-PERPETUAL, sized in the coin,
// and the hedge is on at a testnet bot's first start.

const solPerp = "SOL_USDC-PERPETUAL"

// newSOLHedgeFixture holds 20 SOL of a call at Δ callDelta and 20 SOL of a
// put at Δ putDelta on SOL_USDC (spot 75, each leg's mark 4.04 USDC), with a
// 3,000 USDC margin balance: band = 5 % × 3,000 ÷ 75 = 2 SOL. The perpetual's
// contract is 0.1 SOL. mode: "on" / "off" write the mode file, "" leaves none
// (a first start).
func newSOLHedgeFixture(t *testing.T, callDelta, putDelta float64, mode string) (*strategyFixture, *fakeFutures) {
	t.Helper()
	f := newStrategyFixture(t)
	f.cfg.Underlying = "SOL"
	label := strings.ToUpper(f.expiry.Format("2Jan06"))
	f.call, f.put = "SOL_USDC-"+label+"-80-C", "SOL_USDC-"+label+"-70-P"
	inst := func(name, typ string, strike, delta float64) *marketdata.Instrument {
		return &marketdata.Instrument{
			Name: name, Underlying: "SOL", Settlement: "USDC", ContractSize: 10, Strike: strike, Expiry: f.expiry, OptionType: typ,
			TickSize: 0.01, MinTradeAmount: 10,
			Bid: 4.0, Ask: 4.08, Mid: 4.04, UnderlyingPrice: 75,
			Greeks: marketdata.Greeks{Delta: delta}, UpdatedAt: time.Now(),
		}
	}
	f.market.price = 75
	f.market.instruments = map[string]*marketdata.Instrument{
		f.call: inst(f.call, "call", 80, callDelta),
		f.put:  inst(f.put, "put", 70, putDelta),
	}
	f.exch.summary = orders.AccountSummary{Currency: "USDC", Equity: 3000, AvailableFunds: 3000, MarginBalance: 3000}
	f.exch.perpSuffix = "_USDC-PERPETUAL"
	f.withOpenStrangle(20, 4.04)
	for i, d := range []float64{callDelta, putDelta} {
		f.exch.positions[i].MarkPrice, f.exch.positions[i].Delta = 4.04, d
	}
	ff := &fakeFutures{ex: f.exch, bid: 74.9, ask: 75.1, index: 75, contract: 0.1}
	f.futures = ff
	f.hedgeAt = filepath.Join(t.TempDir(), "hedge_mode.json")
	if mode != "" {
		if err := hedge.SaveMode(f.hedgeAt, hedge.ModeState{Enabled: mode == "on", Since: time.Now(), By: hedge.Actor{User: "tester", Role: "admin"}}); err != nil {
			t.Fatal(err)
		}
	}
	f.exch.onSubmit = func(o orders.Order, id string) orders.Fill {
		switch {
		case o.Instrument == solPerp && o.OrderType == orders.TypeMarket:
			return orders.Fill{OrderID: id, Qty: o.Qty, FillPrice: 75}
		case o.Instrument == solPerp:
			return orders.Fill{OrderID: id, Qty: o.Qty, FillPrice: o.LimitPrice}
		case o.Direction == orders.DirectionBuy:
			return orders.Fill{OrderID: id, Qty: o.Qty, FillPrice: 0.01}
		}
		return orders.Fill{OrderID: id}
	}
	return f, ff
}

func (f *fakeExchange) solPerpOrders() []orders.Order {
	return f.ordersWhere(func(o orders.Order) bool { return o.Instrument == solPerp })
}

// Scenario: a SOL book is hedged in SOL — the pure rule.
func TestLinearHedge_SOLBookIsHedgedInSOL_Rule(t *testing.T) {
	st := orders.NewStateManager()
	for _, p := range []*orders.Position{
		{ID: "c", Instrument: "SOL_USDC-25DEC26-80-C", OptionType: "call", Qty: 20, CurrentMid: 4.04, CurrentGreeks: orders.Greeks{Delta: 0.16}},
		{ID: "p", Instrument: "SOL_USDC-25DEC26-70-P", OptionType: "put", Qty: 20, CurrentMid: 4.04, CurrentGreeks: orders.Greeks{Delta: -0.30}},
	} {
		st.AddPosition(p)
	}
	if got := st.TotalNetDelta(); math.Abs(got-2.8) > 1e-9 {
		t.Fatalf("the book's option delta = %v SOL, want +2.8", got)
	}
	linear := hedge.Perp{Linear: true}
	band := hedge.Band(3000, true, 75, 5) // 3,000 USDC ÷ 75 × 5 % = 2 SOL
	amount, side := linear.Order(st.TotalNetDelta(), linear.Delta(0, 75), band, 75, 0.1)
	if side != hedge.SideSell || amount != 2.8 {
		t.Fatalf("hedge = %s %v SOL, want sell 2.8 SOL (28 contracts of 0.1)", side, amount)
	}
	if amount, _ := linear.Order(2.8, 0, band, 75, 1); amount != 3 {
		t.Errorf("1-SOL contracts: %v, want 3 (whole contracts)", amount)
	}
	if amount, _ := linear.Order(2.8, -1.0, band, 75, 0.1); amount != 0 {
		t.Errorf("net 1.8 SOL inside the 2 SOL band: %v, want nothing", amount)
	}
	if amount, side := linear.Order(-2.8, 0, band, 75, 0.1); side != hedge.SideBuy || amount != 2.8 {
		t.Errorf("short book: %s %v, want buy 2.8", side, amount)
	}
	if amount, _ := linear.Order(2.8, 0, band, 75, 0); amount != 0 {
		t.Errorf("no contract size: %v, want nothing (never guessed)", amount)
	}
	if amount, _ := linear.Order(2.8, 0, band, 0, 0.1); amount != 0 {
		t.Errorf("no spot: %v, want nothing", amount)
	}
	if got := linear.Delta(-2.8, 75); got != -2.8 {
		t.Errorf("a linear perpetual's delta is its size in the coin: %v", got)
	}
}

// Scenario: a SOL book is hedged in SOL — end to end, through Strategy.Run.
func TestLinearHedge_SOLBookIsHedgedInSOL(t *testing.T) {
	f, _ := newSOLHedgeFixture(t, 0.16, -0.30, "on")
	f.startRun()
	eventually(t, 2*time.Second, "perpetual sold", func() bool { return len(f.exch.solPerpOrders()) >= 1 })
	o := f.exch.solPerpOrders()[0]
	if o.Direction != orders.DirectionSell || o.Qty != 2.8 || o.TimeInForce != orders.TimeInForceIOC || o.LimitPrice != 74.9 {
		t.Fatalf("hedge order %+v, want sell 2.8 SOL of %s IOC at the bid 74.9", o, solPerp)
	}
	time.Sleep(150 * time.Millisecond)
	if n := len(f.exch.solPerpOrders()); n != 1 {
		t.Errorf("hedged: %d perpetual orders, want 1", n)
	}
	fills := f.journal.hedgeChanges(orders.HedgeFill)
	if len(fills) != 1 || fills[0].FilledUSD != 2.8 || fills[0].AmountUnit != "SOL" || fills[0].Unit != "USDC" ||
		math.Abs(fills[0].OptionsDelta-2.8) > 1e-9 || math.Abs(fills[0].BandCoin-2) > 1e-9 {
		t.Fatalf("one hedge_fill in SOL (options +2.8, band 2), got %+v", fills)
	}
	if !strings.Contains(fills[0].Detail, "sell 2.8 SOL of SOL_USDC-PERPETUAL") {
		t.Errorf("detail in SOL: %q", fills[0].Detail)
	}
	st, _ := hedge.LoadMode(f.hedgeAt)
	if st.Ledger.Real.SizeUSD != -2.8 || !st.Ledger.Real.Linear || st.Ledger.Real.AvgPrice != 75 {
		t.Errorf("the ledger holds −2.8 SOL at the index 75: %+v", st.Ledger.Real)
	}
	if v := f.strat.HedgeState(); math.Abs(v.NetDelta) > 1e-9 || math.Abs(v.PerpDeltaCoin+2.8) > 1e-9 {
		t.Errorf("the view: net %v, perpetual %v SOL; want 0 and −2.8", v.NetDelta, v.PerpDeltaCoin)
	}
}

// Kill switch on a SOL bot: its perpetual is closed in SOL, reduce-only.
func TestLinearHedge_KillSwitchClosesThePerpetualInSOL(t *testing.T) {
	f, _ := newSOLHedgeFixture(t, 0.16, -0.30, "on")
	f.startRun()
	eventually(t, 2*time.Second, "perpetual sold", func() bool { return len(f.exch.solPerpOrders()) >= 1 })
	f.strat.KillSwitch()
	eventually(t, 3*time.Second, "perpetual closed", func() bool { return len(f.exch.solPerpOrders()) >= 2 })
	c := f.exch.solPerpOrders()[1]
	if c.Direction != orders.DirectionBuy || c.Qty != 2.8 || c.OrderType != orders.TypeMarket || !c.ReduceOnly || c.TriggerReason != orders.TriggerKillSwitch {
		t.Fatalf("kill switch close %+v, want buy 2.8 SOL at market, reduce-only", c)
	}
	if left := f.journal.hedgeChanges(orders.HedgeLeftOpen); len(left) != 0 {
		t.Errorf("nothing beyond the bot's ledger: %+v", left)
	}
}

// Scenario: the hedge is on at first start.
func TestLinearHedge_OnAtFirstStart(t *testing.T) {
	f, _ := newSOLHedgeFixture(t, 0.16, -0.30, "")
	f.startRun()
	eventually(t, 2*time.Second, "hedge_mode_changed journaled", func() bool {
		return len(f.journal.hedgeChanges(orders.HedgeModeChanged)) == 1
	})
	ch := f.journal.hedgeChanges(orders.HedgeModeChanged)[0]
	if ch.Enabled == nil || !*ch.Enabled || ch.Actor == nil || *ch.Actor != strategy.FirstStartActor || !strings.Contains(ch.Detail, "first start") {
		t.Fatalf("first start: %+v", ch)
	}
	st, err := hedge.LoadMode(f.hedgeAt)
	if err != nil || !st.Enabled || st.By.User != strategy.FirstStartActor.User {
		t.Fatalf("the mode file says ON: %+v, %v", st, err)
	}
	if !f.strat.HedgeState().Enabled {
		t.Error("the view says ON")
	}
	eventually(t, 2*time.Second, "the hedge trades", func() bool { return len(f.exch.solPerpOrders()) >= 1 })
}

// Negative: a SOL bot whose hedge a person switched off stays off at restart.
func TestLinearHedge_SwitchedOffStaysOff(t *testing.T) {
	f, _ := newSOLHedgeFixture(t, 0.16, -0.30, "off")
	f.startRun()
	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 2 })
	time.Sleep(150 * time.Millisecond)
	if n := len(f.exch.solPerpOrders()); n != 0 || f.strat.HedgeState().Enabled {
		t.Errorf("switched off: %d perpetual orders, enabled %v", n, f.strat.HedgeState().Enabled)
	}
	if n := len(f.journal.hedgeChanges(orders.HedgeModeChanged)); n != 0 {
		t.Errorf("no mode change at a restart: %d", n)
	}
	eventually(t, 2*time.Second, "paper hedge in SOL", func() bool {
		st, _ := hedge.LoadMode(f.hedgeAt)
		return st.Ledger.Virtual.SizeUSD == -2.8 && st.Ledger.Virtual.Linear
	})
}

// Negative: never on at first start on a live account.
func TestLinearHedge_NotOnAtFirstStartLive(t *testing.T) {
	f, _ := newSOLHedgeFixture(t, 0.16, -0.30, "")
	f.cfg.Environment = "live"
	f.startRun()
	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 2 })
	if f.strat.HedgeState().Enabled {
		t.Error("live: the experiment is testnet-only")
	}
	if _, err := os.Stat(f.hedgeAt); !os.IsNotExist(err) {
		t.Errorf("live: no mode file written (%v)", err)
	}
}

// Scenario: BTC/ETH hedge unchanged — a BTC bot with no mode file stays OFF
// (no first-start switch), and the inverse rules equal today's functions.
func TestLinearHedge_BTCUnchanged(t *testing.T) {
	f, _ := newHedgeFixture(t, 0.06, -0.2905, false)
	f.startRun()
	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 2 })
	time.Sleep(100 * time.Millisecond)
	if f.strat.HedgeState().Enabled || len(f.exch.perpOrders()) != 0 || len(f.journal.hedgeChanges(orders.HedgeModeChanged)) != 0 {
		t.Fatal("a BTC bot's first start leaves the hedge OFF")
	}
	if st, _ := hedge.LoadMode(f.hedgeAt); st.Ledger.Real.Linear || st.Ledger.Virtual.Linear {
		t.Errorf("a BTC ledger is not linear: %+v", st.Ledger)
	}
	inverse := hedge.Perp{}
	for _, c := range []struct{ opt, perp, band, spot, contract float64 }{
		{0.2305, 0, 0.05785, 100000, 10}, {-0.10, 0, 0.05785, 100000, 10}, {0.03, 0, 0.05785, 100000, 10}, {0.5, -0.2, 0.1, 3000, 1},
	} {
		wa, ws := hedge.HedgeOrder(c.opt, c.perp, c.band, c.spot, c.contract)
		if ga, gs := inverse.Order(c.opt, c.perp, c.band, c.spot, c.contract); ga != wa || gs != ws {
			t.Errorf("inverse order %+v: %v %s, want %v %s", c, ga, gs, wa, ws)
		}
	}
	if inverse.Delta(23050, 100000) != hedge.PerpDelta(23050, 100000) || inverse.PnL(-1000, 100000, 90000) != hedge.PerpPnL(-1000, 100000, 90000) ||
		inverse.Fee(1000, 100000, 0.0005) != hedge.TakerFee(1000, 100000, 0.0005) || inverse.Dust(10) != 0.5 {
		t.Error("the inverse perpetual's rules are today's")
	}
}

// The linear ledger: P&L in USDC, averaged by size.
func TestLinearHedge_LedgerInUSDC(t *testing.T) {
	b := hedge.PerpBook{Linear: true}
	b.Book(-2, 70, 0)
	b.Book(-2, 80, 0) // short 4 SOL at 75
	if b.SizeUSD != -4 || b.AvgPrice != 75 {
		t.Fatalf("after two sells: %+v, want −4 at 75", b)
	}
	if got := b.PnL(70); got != 20 {
		t.Errorf("short 4 SOL from 75 marked at 70: %v USDC, want +20", got)
	}
	b.Book(1, 65, 0.1) // buy back 1 at 65: +10, fee 0.1
	if b.Realised != 10 || b.Fees != 0.1 || b.SizeUSD != -3 {
		t.Errorf("after the buy-back: %+v", b)
	}
	p := hedge.Perp{Linear: true}
	if fee := p.Fee(-2, 75, 0.0005); math.Abs(fee-0.075) > 1e-12 {
		t.Errorf("linear taker fee = %v USDC, want 0.075", fee)
	}
	s := hedge.Ledger{Real: b}.Summarise(0, 70)
	if s.Unit != "USDC" || s.AdvantageUSD != s.AdvantageCoin {
		t.Errorf("a USDC ledger's figures are dollars: %+v", s)
	}
	if (hedge.Ledger{}).Summarise(0, 70).Unit != "" {
		t.Error("an inverse ledger names no unit")
	}
	// The monitor's view shows it in the coin at the mark.
	if c := s.InCoin(70); c.Unit != "" || math.Abs(c.AdvantageCoin-s.AdvantageCoin/70) > 1e-12 || c.AdvantageUSD != s.AdvantageUSD || math.Abs(c.RealCoin-s.RealCoin/70) > 1e-12 {
		t.Errorf("in coin at 70: %+v from %+v", c, s)
	}
	if inv := (hedge.Ledger{Real: hedge.PerpBook{SizeUSD: -100, AvgPrice: 100000}}).Summarise(0, 90000); inv.InCoin(90000).AdvantageCoin != inv.AdvantageCoin || inv.InCoin(90000).RealCoin != inv.RealCoin {
		t.Error("an inverse summary is already in coin")
	}
}

// The linear order grid is the larger of the contract size and the minimum
// trade amount (review of #901); the inverse one stays the contract size.
func TestLinearHedge_OrderStepIsTheMinimumTrade(t *testing.T) {
	linear, inverse := hedge.Perp{Linear: true}, hedge.Perp{}
	for _, c := range []struct {
		name                string
		p                   hedge.Perp
		contract, min, want float64
	}{
		{"linear, minimum above the contract", linear, 0.01, 1, 1},
		{"linear, contract above the minimum", linear, 1, 0.1, 1},
		{"linear, equal", linear, 0.1, 0.1, 0.1},
		{"linear, no minimum", linear, 0.1, 0, 0.1},
		{"inverse: the contract, whatever the minimum", inverse, 10, 20, 10},
	} {
		if got := c.p.Step(c.contract, c.min); got != c.want {
			t.Errorf("%s: step %v, want %v", c.name, got, c.want)
		}
	}
}

// End to end: a perpetual whose minimum trade (1 SOL) is above its contract
// (0.01 SOL) is traded in whole minimums: 2.8 SOL due → 3 SOL sent.
func TestLinearHedge_OrderInWholeMinimums(t *testing.T) {
	f, ff := newSOLHedgeFixture(t, 0.16, -0.30, "on")
	ff.contract, ff.minTrade = 0.01, 1
	f.startRun()
	eventually(t, 2*time.Second, "perpetual sold", func() bool { return len(f.exch.solPerpOrders()) >= 1 })
	if o := f.exch.solPerpOrders()[0]; o.Qty != 3 || o.Direction != orders.DirectionSell {
		t.Fatalf("hedge order %+v, want sell 3 SOL (whole 1-SOL minimums)", o)
	}
}

// The monitor's hedge figures of a SOL bot (review of #901): the size in SOL
// apart, perp_usd and contract_usd in USD, the book scenarios with the
// perpetual's linear P&L, and the switch-off preview's fee and delta in SOL.
func TestLinearHedge_MonitorFiguresInCoinAndUSD(t *testing.T) {
	f, _ := newSOLHedgeFixture(t, 0.16, -0.30, "on")
	f.startRun()
	eventually(t, 2*time.Second, "the view holds the perpetual", func() bool { return f.strat.HedgeState().PerpSize == -2.8 })
	v := f.strat.HedgeState()
	at := v.PerpAvg
	if at <= 0 {
		at = v.PerpMark
	}
	if v.SizeUnit != "SOL" || math.Abs(v.PerpUSD-(-2.8*at)) > 1e-9 || math.Abs(v.ContractUSD-0.1*at) > 1e-9 {
		t.Fatalf("view: size %v %s, perp_usd %v, contract_usd %v; want −2.8 SOL, %v USD, %v USD", v.PerpSize, v.SizeUnit, v.PerpUSD, v.ContractUSD, -2.8*at, 0.1*at)
	}
	if n := math.Abs(v.PerpUSD) / v.ContractUSD; math.Abs(n-28) > 1e-9 {
		t.Errorf("contracts from the view = %v, want 28", n)
	}
	if v.Benchmark.Unit != "" {
		t.Errorf("the view's benchmark is in the coin, as the monitor labels it: unit %q", v.Benchmark.Unit)
	}
	if len(v.BookScenarios) != len(hedge.ScenarioMoves) {
		t.Fatalf("book scenarios: %+v", v.BookScenarios)
	}
	for _, sc := range v.BookScenarios {
		perpUSDC := -2.8 * (sc.Spot - 75) // short 2.8 SOL from spot
		if math.Abs(sc.BookUSD-(sc.OptionsUSD+perpUSDC)) > 1e-9 || math.Abs(sc.BookCoin-(sc.OptionsCoin+perpUSDC/sc.Spot)) > 1e-12 {
			t.Errorf("move %v %%: book %v USD, want options %v + perpetual %v", sc.MovePct, sc.BookUSD, sc.OptionsUSD, perpUSDC)
		}
	}
	p, err := f.strat.HedgePreview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fee := 2.8 * p.PerpMark * 0.0005
	if p.SizeUnit != "SOL" || p.PerpSize != -2.8 || p.PerpDeltaCoin != -2.8 || math.Abs(p.DeltaNow) > 1e-9 || math.Abs(p.DeltaAfter-2.8) > 1e-9 ||
		math.Abs(p.CloseFeeUSD-fee) > 1e-9 || math.Abs(p.CloseFeeCoin-fee/p.PerpMark) > 1e-12 {
		t.Errorf("preview %+v: want −2.8 SOL, delta 0 → +2.8, fee %v USDC", p.Preview, fee)
	}
}

// The preview's rules, pure: linear in coin and USDC, inverse unchanged.
func TestLinearHedge_PreviewRule(t *testing.T) {
	in := hedge.PreviewInput{Spot: 75, OptionsDelta: 2.8, ScenarioDelta: 2.8, PerpUSD: -2.8, PerpAvg: 75, PerpMark: 70, FeeRate: 0.0005, Funding: 0.7}
	inv := hedge.BuildPreview(in)
	in.Kind = hedge.Perp{Linear: true}
	lin := hedge.BuildPreview(in)
	if lin.PerpUSD != -210 || lin.PerpSize != -2.8 || lin.PerpDeltaCoin != -2.8 || math.Abs(lin.UnrealisedUSD-14) > 1e-9 ||
		math.Abs(lin.UnrealisedCoin-0.2) > 1e-9 || math.Abs(lin.CloseFeeUSD-0.098) > 1e-12 || math.Abs(lin.FundingCoin-0.01) > 1e-12 {
		t.Errorf("linear preview %+v: short 2.8 SOL from 75 at 70 = +14 USDC (0.2 SOL), fee 0.098 USDC, funding 0.01 SOL", lin)
	}
	if inv.PerpSize != 0 || inv.PerpUSD != -2.8 || inv.PerpDeltaCoin != hedge.PerpDelta(-2.8, 70) || inv.CloseFeeCoin != hedge.TakerFee(-2.8, 70, 0.0005) {
		t.Errorf("inverse preview changed: %+v", inv)
	}
	if got, want := (hedge.Perp{}).BookScenarios(100000, 0.1, 0, -1000), hedge.BookScenarios(100000, 0.1, 0, -1000); len(got) != len(want) || got[0] != want[0] || got[3] != want[3] {
		t.Errorf("inverse book scenarios changed: %+v vs %+v", got, want)
	}
}

// The executor reads a linear coin's futures from currency=USDC, its own rows only.
func TestLinearHedge_FuturePositionsFromUSDC(t *testing.T) {
	rows := `[{"instrument_name":"SOL_USDC-PERPETUAL","size":-2.8,"direction":"sell"},{"instrument_name":"XRP_USDC-PERPETUAL","size":100,"direction":"buy"},{"instrument_name":"BTC_USDC-PERPETUAL","size":0.01,"direction":"buy"}]`
	fc := &fakeCaller{replies: []fakeReply{{result: rows}, {result: `[{"instrument_name":"BTC-PERPETUAL","size":-100,"direction":"sell"}]`}}}
	ex := orders.NewExecutor(fc)
	got, err := ex.GetFuturePositions(context.Background(), "SOL")
	if err != nil || len(got) != 1 || got[0].InstrumentName != solPerp || got[0].Size != -2.8 {
		t.Fatalf("SOL futures = %+v, %v; want SOL_USDC-PERPETUAL only", got, err)
	}
	if c := fc.calls[0]; c.method != "private/get_positions" || c.params["currency"] != "USDC" || c.params["kind"] != "future" {
		t.Errorf("SOL request %+v, want currency=USDC kind=future", c)
	}
	btc, err := ex.GetFuturePositions(context.Background(), "BTC")
	if err != nil || len(btc) != 1 || fc.calls[1].params["currency"] != "BTC" {
		t.Errorf("BTC request unchanged: %+v %+v %v", fc.calls[1], btc, err)
	}
}
