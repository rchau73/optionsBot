package tests

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/gateway"
	"optionsbot/internal/gex"
	"optionsbot/internal/history"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// These tests drive the real Strategy.Run loop against in-memory fakes of the
// exchange and market data, and assert on the orders it sends and the book it
// keeps. They cover the order-path safety properties end to end.

// ── Fake exchange ────────────────────────────────────────────────────────────

type fakeExchange struct {
	mu sync.Mutex

	submitted   []orders.Order
	cancelled   []string
	cancelAll   []string
	orderStates map[string]orders.OrderStateInfo
	positions   []orders.RawPosition
	summary     orders.AccountSummary
	summaryErr  error
	imPerLot    float64 // IM of one strangle lot (both legs) in simulate_portfolio
	mmRatio     float64 // MM as a share of IM in simulations
	simErr      error
	simCalls    []map[string]float64
	nextID      int
	dailyCloses []orders.DailyClose
	amended     []string

	// onSubmit decides each fill. Default: sells rest unfilled on the book,
	// buys fill completely at once.
	onSubmit func(o orders.Order, id string) orders.Fill

	// Like a real exchange, positions follow fills (byID/applied track how
	// much of each order is already in positions).
	byID    map[string]orders.Order
	applied map[string]float64

	// loseReply makes Submit put the order on the book but answer with a
	// lost connection — the reply never reached the bot.
	loseReply       func(o orders.Order) bool
	cancelledLabels []string
	cancelLabelErr  error
}

// applyFillLocked moves positions by a fill of qty on o. Caller holds mu.
func (f *fakeExchange) applyFillLocked(o orders.Order, id string, qty, price float64) {
	if f.byID == nil {
		f.byID, f.applied = map[string]orders.Order{}, map[string]float64{}
	}
	f.byID[id] = o
	if qty <= 0 {
		return
	}
	f.applied[id] += qty
	signed := qty
	if o.Direction == orders.DirectionSell {
		signed = -qty
	}
	for i := range f.positions {
		p := &f.positions[i]
		if p.InstrumentName != o.Instrument {
			continue
		}
		if (p.Size < 0) == (signed < 0) { // adding to the position: average the price
			p.AveragePrice = (p.AveragePrice*math.Abs(p.Size) + price*qty) / (math.Abs(p.Size) + qty)
		}
		p.Size += signed
		switch {
		case math.Abs(p.Size) < 1e-9:
			f.positions = append(f.positions[:i], f.positions[i+1:]...)
		case p.Size < 0:
			p.Direction = orders.DirectionSell
		default:
			p.Direction = orders.DirectionBuy
		}
		return
	}
	dir := orders.DirectionBuy
	if signed < 0 {
		dir = orders.DirectionSell
	}
	f.positions = append(f.positions, orders.RawPosition{InstrumentName: o.Instrument, Size: signed, Direction: dir, AveragePrice: price, MarkPrice: price})
}

func (f *fakeExchange) CancelByLabel(_ context.Context, _ string, label string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cancelLabelErr != nil {
		return 0, f.cancelLabelErr
	}
	f.cancelledLabels = append(f.cancelledLabels, label)
	n := 0
	for id, o := range f.byID {
		if st := f.orderStates[id]; o.Label == label && st.State == "open" {
			st.State = "cancelled"
			f.orderStates[id] = st
			n++
		}
	}
	return n, nil
}

func newFakeExchange() *fakeExchange {
	return &fakeExchange{
		orderStates: map[string]orders.OrderStateInfo{},
		summary:     orders.AccountSummary{Currency: "BTC", Equity: 10, AvailableFunds: 10, MarginBalance: 10},
		imPerLot:    0.01,
		mmRatio:     0.7,
	}
}

func (f *fakeExchange) Submit(_ context.Context, o orders.Order) (orders.Fill, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := fmt.Sprintf("o-%d", f.nextID)
	f.submitted = append(f.submitted, o)

	if f.loseReply != nil && f.loseReply(o) {
		f.applyFillLocked(o, id, 0, 0)
		f.orderStates[id] = orders.OrderStateInfo{OrderID: id, State: "open"}
		return orders.Fill{}, fmt.Errorf("private/sell: %w", gateway.ErrConnectionLost)
	}
	var fill orders.Fill
	switch {
	case f.onSubmit != nil:
		fill = f.onSubmit(o, id)
	case o.Direction == orders.DirectionBuy:
		fill = orders.Fill{OrderID: id, Qty: o.Qty, FillPrice: 0.01}
	default:
		fill = orders.Fill{OrderID: id}
	}
	fill.OrderID = id
	state := "open"
	if fill.Qty >= o.Qty {
		state = "filled"
	}
	f.orderStates[id] = orders.OrderStateInfo{OrderID: id, State: state, FilledAmount: fill.Qty, AvgPrice: fill.FillPrice}
	f.applyFillLocked(o, id, fill.Qty, fill.FillPrice)
	return fill, nil
}

func (f *fakeExchange) Cancel(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled = append(f.cancelled, id)
	st := f.orderStates[id]
	if st.State == "open" {
		st.State = "cancelled"
		f.orderStates[id] = st
	}
	return nil
}

func (f *fakeExchange) CancelAllOrders(_ context.Context, currency string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelAll = append(f.cancelAll, currency)
	return nil
}

func (f *fakeExchange) GetOrderState(_ context.Context, id string) (orders.OrderStateInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.orderStates[id], nil
}

func (f *fakeExchange) AmendOrder(_ context.Context, id string, _ float64, price float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.amended = append(f.amended, fmt.Sprintf("%s@%g", id, price))
	return nil
}

func (f *fakeExchange) GetAccountSummary(context.Context, string) (orders.AccountSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.summary, f.summaryErr
}

func (f *fakeExchange) GetPositions(context.Context, string) ([]orders.RawPosition, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]orders.RawPosition(nil), f.positions...), nil
}

func (f *fakeExchange) GetMargins(context.Context, string, float64, float64) (orders.MarginInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return orders.MarginInfo{InitialMargin: f.imPerLot}, nil
}

// SimulatePortfolio models margin as linear in lots: each short lot (0.1)
// adds half of imPerLot per leg; a buy (positive size) frees the same.
func (f *fakeExchange) SimulatePortfolio(_ context.Context, currency string, positions map[string]float64) (orders.AccountSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.simCalls = append(f.simCalls, positions)
	if f.simErr != nil {
		return orders.AccountSummary{}, f.simErr
	}
	out := f.summary
	for _, size := range positions {
		dIM := -size / 0.1 * f.imPerLot / 2
		out.InitialMargin += dIM
		out.MaintenanceMargin += dIM * f.mmRatio
	}
	return out, nil
}

func (f *fakeExchange) setMargin(im, mm float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.summary.InitialMargin, f.summary.MaintenanceMargin = im, mm
}

func (f *fakeExchange) GetDailyCloses(context.Context, string, int) ([]orders.DailyClose, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dailyCloses == nil {
		return nil, errors.New("no history in tests")
	}
	return f.dailyCloses, nil
}

// fill marks a resting order as filled (positions follow).
func (f *fakeExchange) fill(id string, qty, price float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.orderStates[id] = orders.OrderStateInfo{OrderID: id, State: "filled", FilledAmount: qty, AvgPrice: price}
	if o, ok := f.byID[id]; ok {
		f.applyFillLocked(o, id, qty-f.applied[id], price)
	}
}

// orders returns a copy of the submitted orders matching keep.
func (f *fakeExchange) ordersWhere(keep func(orders.Order) bool) []orders.Order {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []orders.Order
	for _, o := range f.submitted {
		if keep(o) {
			out = append(out, o)
		}
	}
	return out
}

func (f *fakeExchange) sells() []orders.Order {
	return f.ordersWhere(func(o orders.Order) bool { return o.Direction == orders.DirectionSell })
}

func (f *fakeExchange) buys() []orders.Order {
	return f.ordersWhere(func(o orders.Order) bool { return o.Direction == orders.DirectionBuy })
}

// orderIDFor returns the ID of the n-th (0-based) submitted order on instrument.
func (f *fakeExchange) orderIDFor(instrument string, n int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	seen := 0
	for i, o := range f.submitted {
		if o.Instrument == instrument {
			if seen == n {
				return fmt.Sprintf("o-%d", i+1)
			}
			seen++
		}
	}
	return ""
}

// ── Fake market data, journal, hedge ─────────────────────────────────────────

type fakeMarket struct {
	mu          sync.Mutex
	tracked     []string // instruments passed to Track
	price       float64
	instruments map[string]*marketdata.Instrument
	dvolCloses  []marketdata.DayIV
	dvolToday   marketdata.DayIV
}

func (m *fakeMarket) Track(_ context.Context, names []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tracked = append(m.tracked, names...)
	return nil
}

func (m *fakeMarket) DVOLDaily() ([]marketdata.DayIV, marketdata.DayIV) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]marketdata.DayIV(nil), m.dvolCloses...), m.dvolToday
}

// setIVHistory gives the market daily IV percentiles: closes for the days
// before today (oldest first) and today's live value.
func (m *fakeMarket) setIVHistory(closes []float64, today float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	day := time.Now().UTC().Truncate(24 * time.Hour)
	m.dvolCloses = nil
	for i, pct := range closes {
		d := day.AddDate(0, 0, i-len(closes))
		m.dvolCloses = append(m.dvolCloses, marketdata.DayIV{Day: d, Percentile: pct, Known: true})
	}
	m.dvolToday = marketdata.DayIV{Day: day, Percentile: today, Known: true}
}

func (m *fakeMarket) UnderlyingPrice() float64 { m.mu.Lock(); defer m.mu.Unlock(); return m.price }
func (m *fakeMarket) DVOL() float64            { return 55 }
func (m *fakeMarket) IVPercentile() float64    { return 50 }

func (m *fakeMarket) GetInstrument(name string) (*marketdata.Instrument, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, ok := m.instruments[name]
	if !ok {
		return nil, false
	}
	cp := *inst
	return &cp, true
}

func (m *fakeMarket) AllInstruments() []*marketdata.Instrument {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*marketdata.Instrument, 0, len(m.instruments))
	for _, inst := range m.instruments {
		cp := *inst
		out = append(out, &cp)
	}
	return out
}

// setQuote changes an instrument's bid/ask (mid follows).
func (m *fakeMarket) setQuote(name string, bid, ask float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	inst := m.instruments[name]
	inst.Bid, inst.Ask, inst.Mid = bid, ask, (bid+ask)/2
}

// recordingJournal keeps every journal entry so tests can assert on them.
type recordingJournal struct {
	mu      sync.Mutex
	entries []journalEntry
	pnl     []orders.PnLRecord
	risk    []orders.RiskRecord
}

func (j *recordingJournal) LogRisk(r orders.RiskRecord) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.risk = append(j.risk, r)
}

// riskChanges returns the journaled margin-policy records with this change.
func (j *recordingJournal) riskChanges(change string) []orders.RiskRecord {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []orders.RiskRecord
	for _, r := range j.risk {
		if r.Change == change {
			out = append(out, r)
		}
	}
	return out
}

type journalEntry struct {
	event      string
	instrument string
	trigger    string
	orderType  string
	reason     string
	ctx        orders.EventContext
}

func (j *recordingJournal) add(e journalEntry) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.entries = append(j.entries, e)
}

func (j *recordingJournal) LogSubmit(r orders.PendingOrderRecord, ctx orders.EventContext) {
	j.add(journalEntry{event: orders.EventSubmitted, instrument: r.Instrument, trigger: r.TriggerReason, ctx: ctx})
}
func (j *recordingJournal) LogAmend(r orders.PendingOrderRecord, _ float64, ctx orders.EventContext) {
	j.add(journalEntry{event: orders.EventAmended, instrument: r.Instrument, ctx: ctx})
}
func (j *recordingJournal) LogCancelled(r orders.PendingOrderRecord, ctx orders.EventContext) {
	j.add(journalEntry{event: orders.EventCancelled, instrument: r.Instrument, ctx: ctx})
}
func (j *recordingJournal) LogOpen(pos *orders.Position, _ orders.Fill, ctx orders.EventContext) {
	j.add(journalEntry{event: orders.EventFilled, instrument: pos.Instrument, ctx: ctx})
}
func (j *recordingJournal) LogClose(pos *orders.Position, _ orders.Fill, trigger, orderType string, ctx orders.EventContext) {
	j.add(journalEntry{event: orders.EventClosed, instrument: pos.Instrument, trigger: trigger, orderType: orderType, ctx: ctx})
}
func (j *recordingJournal) LogReconciled(pos *orders.Position, ctx orders.EventContext) {
	j.add(journalEntry{event: orders.EventReconciled, instrument: pos.Instrument, ctx: ctx})
}
func (j *recordingJournal) LogSkipped(reason string, ctx orders.EventContext) {
	j.add(journalEntry{event: orders.EventSkipped, reason: reason, ctx: ctx})
}
func (j *recordingJournal) LogPnL(p orders.PnLRecord) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.pnl = append(j.pnl, p)
}

// events returns a copy of the entries with the given event name.
func (j *recordingJournal) events(event string) []journalEntry {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []journalEntry
	for _, e := range j.entries {
		if e.event == event {
			out = append(out, e)
		}
	}
	return out
}

type nopHedge struct{}

func (nopHedge) MaybeReport(float64, float64, string) {}

type fixedGEX struct{ snap *gex.Snapshot }

func (g fixedGEX) Snapshot() *gex.Snapshot { return g.snap }

// ── Fixture ──────────────────────────────────────────────────────────────────

const (
	testCall = "BTC-%s-110000-C"
	testPut  = "BTC-%s-90000-P"
)

type strategyFixture struct {
	cfg      *config.Config
	exch     *fakeExchange
	market   *fakeMarket
	state    *orders.StateManager
	strat    *strategy.Strategy
	call     string
	put      string
	expiry   time.Time
	gex      strategy.GEXSource     // optional
	regimes  strategy.RegimeHistory // optional; set with withConfirmedRegime
	oi       strategy.OISource      // optional
	journal  *recordingJournal
	logger   *orders.Logger // when set, used as the journal instead of the recorder
	cancel   context.CancelFunc
	runErr   chan error
	startRun func()
}

// newStrategyFixture builds one (45 DTE, 0.16 delta) slot with a matching
// OTM call and put. Call startRun to launch Strategy.Run.
func newStrategyFixture(t *testing.T) *strategyFixture {
	t.Helper()
	expiry := time.Now().UTC().AddDate(0, 0, 45).Truncate(24 * time.Hour).Add(8 * time.Hour)
	label := strings.ToUpper(expiry.Format("2Jan06"))
	f := &strategyFixture{
		cfg: &config.Config{
			Underlying:             "BTC",
			DTEDeltaMatrix:         []config.DTEDeltaEntry{{DTE: 45, Deltas: []float64{0.16}}},
			EntryDelta:             0.16,
			RolloutDTE:             19,
			DeltaDriftThreshold:    0.05,
			ROITakeProfit:          0.5,
			StopLossMultiplier:     2.0,
			GammaTrendLookbackDays: 21,
			SwingPivotN:            2,
			EvalIntervalMS:         10,
			MaxDTEDeviation:        10,
			DeltaSlippage:          0.05,
			MinTradeAmount:         0.1,
			OrderFillTimeoutSec:    60,
			OrderSlippagePct:       0.5,
			OrderMaxAdjustments:    3,
		},
		exch:    newFakeExchange(),
		state:   orders.NewStateManager(),
		journal: &recordingJournal{},
		call:    fmt.Sprintf(testCall, label),
		put:     fmt.Sprintf(testPut, label),
		expiry:  expiry,
		runErr:  make(chan error, 1),
	}
	inst := func(name, typ string, strike, delta float64) *marketdata.Instrument {
		return &marketdata.Instrument{
			Name: name, Underlying: "BTC", Strike: strike, Expiry: expiry, OptionType: typ,
			TickSize: 0.0001, MinTradeAmount: 0.1,
			Bid: 0.019, Ask: 0.021, Mid: 0.02, UnderlyingPrice: 100000,
			Greeks: marketdata.Greeks{Delta: delta}, UpdatedAt: time.Now(), // has a live quote
		}
	}
	f.market = &fakeMarket{price: 100000, instruments: map[string]*marketdata.Instrument{
		f.call: inst(f.call, "call", 110000, 0.16),
		f.put:  inst(f.put, "put", 90000, -0.16),
	}}
	// DVOL history as the live bot seeds it at startup: low IV percentile,
	// so the confirmed IM limit is the lowest band, 20 % of margin balance.
	f.market.setIVHistory([]float64{10, 10, 10}, 10)
	f.startRun = func() {
		var journal strategy.TradeJournal = f.journal
		if f.logger != nil {
			journal = f.logger
		}
		f.strat = strategy.New(f.cfg, strategy.Deps{
			Market: f.market, Exchange: f.exch, State: f.state,
			Journal: journal, Hedge: nopHedge{}, GEX: f.gex, OI: f.oi, Regimes: f.regimes,
		})
		ctx, cancel := context.WithCancel(context.Background())
		f.cancel = cancel
		go func() { f.runErr <- f.strat.Run(ctx) }()
		t.Cleanup(func() {
			cancel()
			select {
			case <-f.runErr:
			case <-time.After(3 * time.Second):
				t.Error("Strategy.Run did not stop after cancel")
			}
		})
	}
	return f
}

// withOpenStrangle makes the exchange report an existing short strangle,
// which reconcile loads on startup. The margin per lot is set so the IM
// limit (lowest band, 20 %: no DVOL history in the fixture) fits exactly this
// size, so the startup rebalance leaves it alone.
func (f *strategyFixture) withOpenStrangle(qty, avgPrice float64) {
	lots := math.Round(qty / 0.1)
	share := 20.0 / 100 * f.exch.summary.MarginBalance / float64(len(f.cfg.Slots()))
	f.exch.imPerLot = share / lots * 0.999
	f.exch.positions = []orders.RawPosition{
		{InstrumentName: f.call, Size: -qty, Direction: "sell", AveragePrice: avgPrice, MarkPrice: 0.02, Delta: 0.16},
		{InstrumentName: f.put, Size: -qty, Direction: "sell", AveragePrice: avgPrice, MarkPrice: 0.02, Delta: -0.16},
	}
}

func (f *strategyFixture) position(instrument string) *orders.Position {
	for _, p := range f.state.AllPositions() {
		if p.Instrument == instrument {
			return p
		}
	}
	return nil
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestStrategy_OpensStrangleAndActivatesOnFill(t *testing.T) {
	f := newStrategyFixture(t)
	f.startRun()

	eventually(t, 2*time.Second, "both legs submitted", func() bool { return len(f.exch.sells()) == 2 })
	if n := len(f.state.AllPositions()); n != 0 {
		t.Fatalf("resting orders must not be booked as positions yet, got %d", n)
	}

	f.exch.fill(f.exch.orderIDFor(f.call, 0), 0.1, 0.021)
	f.exch.fill(f.exch.orderIDFor(f.put, 0), 0.1, 0.021)

	eventually(t, 2*time.Second, "strangle active", func() bool { return len(f.state.AllStrangles()) == 1 })
	call := f.position(f.call)
	if call == nil || call.Qty != 0.1 || math.Abs(call.PremiumReceived-0.0021) > 1e-12 {
		t.Errorf("call position = %+v", call)
	}
	time.Sleep(50 * time.Millisecond)
	if n := len(f.exch.sells()); n != 2 {
		t.Errorf("an occupied slot must not be re-entered: %d sells", n)
	}
}

func TestStrategy_EntrySubmitsAtMaxOfMidAndAsk(t *testing.T) {
	f := newStrategyFixture(t)
	f.market.setQuote(f.call, 0, 0.03) // no bid: mid 0.015, ask 0.03
	f.startRun()

	eventually(t, 2*time.Second, "entry submitted", func() bool { return len(f.exch.sells()) == 2 })
	for _, o := range f.exch.sells() {
		if o.Instrument == f.call && o.LimitPrice != 0.03 {
			t.Errorf("call limit = %v, want ask 0.03 when bid is 0", o.LimitPrice)
		}
	}
}

func TestStrategy_PremiumFloorBlocksBothLegs(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.MinPremiumBTC = 0.05 // both legs quote 0.021
	f.startRun()

	time.Sleep(100 * time.Millisecond)
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("no leg may be sold below the premium floor, got %d sells", n)
	}
}

// When an entry times out with only one leg filled, the filled leg must stay
// tracked (it is a live short on the exchange) and the other is cancelled.
func TestStrategy_EntryTimeoutKeepsFilledLeg(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.OrderFillTimeoutSec = 1
	f.startRun()

	eventually(t, 2*time.Second, "both legs submitted", func() bool { return len(f.exch.sells()) == 2 })
	f.exch.fill(f.exch.orderIDFor(f.call, 0), 0.1, 0.021)

	eventually(t, 3*time.Second, "filled call booked after timeout", func() bool { return f.position(f.call) != nil })
	putOrder := f.exch.orderIDFor(f.put, 0)
	f.exch.mu.Lock()
	cancelled := strings.Contains(strings.Join(f.exch.cancelled, ","), putOrder)
	f.exch.mu.Unlock()
	if !cancelled {
		t.Errorf("unfilled put order %s should be cancelled", putOrder)
	}
	if n := len(f.state.AllStrangles()); n != 1 {
		t.Errorf("filled leg should form a one-legged strangle for repair, got %d strangles", n)
	}
}

// A partially filled entry leg is booked at the filled size, not the requested one.
func TestStrategy_PartialEntryFillBooksFilledQty(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.OrderFillTimeoutSec = 1
	f.exch.imPerLot = 0.001 // budget allows several lots
	f.startRun()

	eventually(t, 2*time.Second, "entry submitted", func() bool { return len(f.exch.sells()) == 2 })
	requested := f.exch.sells()[0].Qty
	if requested < 0.3 {
		t.Fatalf("test needs a multi-lot entry, got %v", requested)
	}
	callID := f.exch.orderIDFor(f.call, 0)
	f.exch.mu.Lock()
	f.exch.orderStates[callID] = orders.OrderStateInfo{OrderID: callID, State: "open", FilledAmount: 0.1, AvgPrice: 0.021}
	f.exch.mu.Unlock()

	eventually(t, 3*time.Second, "partial call booked", func() bool { return f.position(f.call) != nil })
	if q := f.position(f.call).Qty; q != 0.1 {
		t.Errorf("booked qty = %v, want the filled 0.1 (requested %v)", q, requested)
	}
}

// Regression: a take-profit roll used to reopen the leg twice (once directly,
// once via repair). Now it closes once and repair reopens exactly once.
func TestStrategy_RolloutReopensLegExactlyOnce(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.market.setQuote(f.call, 0.004, 0.006) // call mid 0.005 → 75% of premium captured
	f.startRun()

	eventually(t, 2*time.Second, "call bought back", func() bool { return len(f.exch.buys()) == 1 })
	buy := f.exch.buys()[0]
	if buy.Instrument != f.call || buy.OrderType != orders.TypeLimit || buy.TimeInForce != orders.TimeInForceIOC || buy.LimitPrice != 0.006 {
		t.Errorf("rollout close should be an IOC limit at the ask, got %+v", buy)
	}

	eventually(t, 2*time.Second, "call reopened", func() bool { return len(f.exch.sells()) >= 1 })
	time.Sleep(100 * time.Millisecond) // many more cycles
	if n := len(f.exch.sells()); n != 1 {
		t.Errorf("rolled leg must be reopened exactly once, got %d sells", n)
	}
	if o := f.exch.sells()[0]; o.Instrument != f.call || o.Qty != 0.1 {
		t.Errorf("reopen should sell the call at the original size, got %+v", o)
	}
}

func TestStrategy_RolloutCloseThatMissesKeepsPosition(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.market.setQuote(f.call, 0.004, 0.006)
	f.exch.onSubmit = func(orders.Order, string) orders.Fill { return orders.Fill{} } // IOC never fills
	f.startRun()

	eventually(t, 2*time.Second, "close attempted", func() bool { return len(f.exch.buys()) >= 1 })
	time.Sleep(50 * time.Millisecond)
	if f.position(f.call) == nil {
		t.Fatal("an unfilled close must leave the position tracked")
	}
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("nothing may be reopened before the close fills, got %d sells", n)
	}
}

func TestStrategy_StopLossPartialFillKeepsRemainder(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.3, 0.01)
	f.market.setQuote(f.call, 0.039, 0.041) // mid 0.04 = 4× premium → stop-loss
	var mu sync.Mutex
	attempts := 0
	f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill {
		if o.Direction != orders.DirectionBuy {
			return orders.Fill{}
		}
		mu.Lock()
		defer mu.Unlock()
		attempts++
		if attempts == 1 {
			return orders.Fill{Qty: 0.1, FillPrice: 0.04} // thin book: one lot
		}
		return orders.Fill{Qty: o.Qty, FillPrice: 0.04}
	}
	f.startRun()

	callBuys := func() []orders.Order {
		return f.exch.ordersWhere(func(o orders.Order) bool {
			return o.Direction == orders.DirectionBuy && o.Instrument == f.call
		})
	}
	eventually(t, 2*time.Second, "call fully closed", func() bool {
		return len(callBuys()) >= 2 && f.position(f.call) == nil
	})
	buys := callBuys()
	if len(buys) < 2 || buys[0].Qty != 0.3 || math.Abs(buys[1].Qty-0.2) > 1e-9 {
		t.Fatalf("want 0.3 then the 0.2 remainder, got %+v", buys)
	}
	if buys[0].OrderType != orders.TypeMarket || buys[0].TriggerReason != orders.TriggerStopLoss200Pct {
		t.Errorf("stop-loss must be a market order, got %+v", buys[0])
	}
}

func TestStrategy_ReconcileLoadsPositionsAndCancelsOnlyOwnCurrency(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.startRun()

	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 2 })
	f.exch.mu.Lock()
	cancelAll := append([]string(nil), f.exch.cancelAll...)
	f.exch.mu.Unlock()
	if len(cancelAll) == 0 || cancelAll[0] != "BTC" {
		t.Errorf("startup should cancel stale BTC orders only, got %v", cancelAll)
	}
	strangles := f.state.AllStrangles()
	if len(strangles) != 1 || strangles[0].CallLeg == nil || strangles[0].PutLeg == nil {
		t.Fatalf("call and put should be regrouped into one strangle, got %+v", strangles)
	}
	if strangles[0].TargetDTE != 45 {
		t.Errorf("strangle should be matched to the 45 DTE slot, got %d", strangles[0].TargetDTE)
	}
}

func TestStrategy_RebalanceDownsizesInWholeLots(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.55, 0.02) // 0.55 is not on the 0.1 grid
	f.exch.imPerLot = 100          // now the limit fits only the minimum lot (0.1)
	f.exch.setMargin(3, 1)         // IM 30 % of margin balance > 20 % limit
	f.startRun()

	eventually(t, 2*time.Second, "both legs downsized", func() bool { return len(f.exch.buys()) == 2 })
	for _, b := range f.exch.buys() {
		if math.Abs(b.Qty-0.4) > 1e-9 || b.TriggerReason != orders.TriggerRebalanceDownsize {
			t.Errorf("downsize should buy back 0.4 (whole lots above the 0.1 target), got %+v", b)
		}
	}
	eventually(t, time.Second, "book updated", func() bool {
		p := f.position(f.call)
		return p != nil && math.Abs(p.Qty-0.15) < 1e-9
	})
}

func TestStrategy_KillSwitchCancelsFlattensAndStaysIdle(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.startRun()
	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 2 })

	f.strat.KillSwitch()
	f.strat.KillSwitch() // idempotent

	eventually(t, 2*time.Second, "book flattened", func() bool { return len(f.state.AllPositions()) == 0 })
	for _, b := range f.exch.buys() {
		if b.OrderType != orders.TypeMarket || b.TriggerReason != orders.TriggerKillSwitch {
			t.Errorf("kill switch must close at market, got %+v", b)
		}
	}
	f.exch.mu.Lock()
	cancels := len(f.exch.cancelAll)
	f.exch.mu.Unlock()
	if cancels < 2 { // startup reconcile + kill switch
		t.Errorf("kill switch should cancel resting orders first, cancel_all calls = %d", cancels)
	}

	select {
	case err := <-f.runErr:
		t.Fatalf("Run must stay idle after the kill switch (a restart would re-open positions), returned %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	sellsBefore := len(f.exch.sells())
	time.Sleep(50 * time.Millisecond)
	if len(f.exch.sells()) != sellsBefore {
		t.Error("no new entries after the kill switch")
	}

	f.cancel()
	select {
	case err := <-f.runErr:
		if !errors.Is(err, strategy.ErrKillSwitch) {
			t.Errorf("Run returned %v, want ErrKillSwitch", err)
		}
		f.runErr <- err // let the cleanup see Run has stopped
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on cancel")
	}
}

func TestStrategy_ForbiddenPausesTrading(t *testing.T) {
	f := newStrategyFixture(t)
	f.exch.summaryErr = fmt.Errorf("wrapped: %w", orders.ErrForbidden)
	f.startRun()

	time.Sleep(150 * time.Millisecond)
	if n := len(f.exch.sells()); n != 0 {
		t.Errorf("no orders while the API key is forbidden, got %d", n)
	}
}

// withPutSheddingRegime configures a confirmed negative-gamma regime (spot
// below the flip) and a falling market, in which the GEX gate sheds puts.
func (f *strategyFixture) withPutSheddingRegime() {
	// 30 daily closes: up to a peak, then a steady slide to today's 100k.
	start := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -30)
	for i := 0; i < 30; i++ {
		price := 110000.0 + float64(i)*1000
		if i > 10 {
			price = 120000 - float64(i-10)*1000
		}
		f.exch.dailyCloses = append(f.exch.dailyCloses, orders.DailyClose{Date: start.AddDate(0, 0, i), Close: price})
	}
	f.exch.dailyCloses[29].Close = 100000
	f.gex = fixedGEX{&gex.Snapshot{Regime: "NEGATIVE/ACCELERATION", Spot: 100000, GammaFlip: 120000, GammaFlipFound: true}}
}

func TestStrategy_GEXSheddingPutsOpensCallOnly(t *testing.T) {
	f := newStrategyFixture(t)
	f.withPutSheddingRegime()
	f.withConfirmedRegime("NEGATIVE/ACCELERATION") // otherwise entries are frozen
	f.startRun()

	eventually(t, 2*time.Second, "call submitted", func() bool { return len(f.exch.sells()) >= 1 })
	time.Sleep(50 * time.Millisecond)
	for _, o := range f.exch.sells() {
		if o.Instrument != f.call {
			t.Errorf("only the call may be sold while puts are being shed, got %s", o.Instrument)
		}
	}
}

// withConfirmedRegime records regime at the two previous daily closes, so the
// margin policy sees it as confirmed.
func (f *strategyFixture) withConfirmedRegime(regime string) {
	store, _ := history.OpenRegimes("")
	day := time.Now().UTC().Truncate(24 * time.Hour)
	store.Record(day.AddDate(0, 0, -2), regime)
	store.Record(day.AddDate(0, 0, -1), regime)
	f.regimes = store
}

// withLoneCall makes the exchange report only the call of a strangle.
func (f *strategyFixture) withLoneCall(qty float64) {
	f.withOpenStrangle(qty, 0.02)
	f.exch.positions = f.exch.positions[:1]
}

func TestStrategy_RepairReopensMissingLeg(t *testing.T) {
	f := newStrategyFixture(t)
	f.withLoneCall(0.1)
	f.startRun()

	eventually(t, 2*time.Second, "missing put sold", func() bool { return len(f.exch.sells()) == 1 })
	if o := f.exch.sells()[0]; o.Instrument != f.put || o.Qty != 0.1 {
		t.Errorf("repair should sell the put at the call's size, got %+v", o)
	}
	time.Sleep(50 * time.Millisecond)
	if n := len(f.exch.sells()); n != 1 {
		t.Errorf("a pending repair must not be re-submitted, got %d sells", n)
	}
}

func TestStrategy_RepairRespectsGEXGate(t *testing.T) {
	f := newStrategyFixture(t)
	f.withLoneCall(0.1)
	f.withPutSheddingRegime()
	f.startRun()

	eventually(t, 2*time.Second, "positions reconciled", func() bool { return len(f.state.AllPositions()) == 1 })
	time.Sleep(100 * time.Millisecond)
	for _, o := range f.exch.sells() {
		if o.Instrument == f.put {
			t.Error("repair must not sell a put while the GEX regime is shedding puts")
		}
	}
}

func TestStrategy_AmendsEntryWhenAskDrifts(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.OrderSlippagePct = 0.05
	f.startRun()

	eventually(t, 2*time.Second, "entry submitted", func() bool { return len(f.exch.sells()) == 2 })
	f.market.setQuote(f.call, 0.014, 0.016) // ask 0.021 → 0.016 (−24%)

	eventually(t, 2*time.Second, "call amended to the new ask", func() bool {
		f.exch.mu.Lock()
		defer f.exch.mu.Unlock()
		for _, a := range f.exch.amended {
			if strings.HasSuffix(a, "@0.016") {
				return true
			}
		}
		return false
	})
	time.Sleep(100 * time.Millisecond)
	f.exch.mu.Lock()
	n := len(f.exch.amended)
	f.exch.mu.Unlock()
	if n > f.cfg.OrderMaxAdjustments*2 {
		t.Errorf("amendments must stop at order_max_adjustments, got %d", n)
	}
}

func TestStrategy_GEXSheddingClosesOpenPuts(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.withPutSheddingRegime()
	f.startRun()

	eventually(t, 2*time.Second, "put closed", func() bool { return len(f.state.AllPositions()) == 1 && f.position(f.put) == nil })
	for _, b := range f.exch.buys() {
		if b.Instrument != f.put || b.TriggerReason != orders.TriggerGammaClose || b.OrderType != orders.TypeMarket {
			t.Errorf("GEX shedding must close the put at market, got %+v", b)
		}
	}
	if f.position(f.call) == nil {
		t.Error("the call leg must stay open")
	}
}

func TestStrategy_RebalanceUpsizeOpensComplementStrangle(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.exch.imPerLot /= 3 // the budget now fits three lots
	f.startRun()

	eventually(t, 2*time.Second, "complement submitted", func() bool { return len(f.exch.sells()) == 2 })
	for _, o := range f.exch.sells() {
		if math.Abs(o.Qty-0.2) > 1e-9 {
			t.Errorf("complement should add the missing 0.2, got %+v", o)
		}
	}
	if len(f.exch.buys()) != 0 {
		t.Error("an upsize must never close the existing legs")
	}
}

// A position on an instrument the market data does not know (e.g. an expiry
// outside the subscribed set) is still loaded, from its name.
func TestStrategy_ReconcileParsesUnknownInstrument(t *testing.T) {
	f := newStrategyFixture(t)
	f.exch.positions = []orders.RawPosition{
		{InstrumentName: "BTC-27DEC30-150000-C", Size: -0.1, Direction: "sell", AveragePrice: 0.01, MarkPrice: 0.01},
		{InstrumentName: "BTC-BAD", Size: -0.1, Direction: "sell"},               // unparsable: skipped
		{InstrumentName: f.put, Size: 0.1, Direction: "buy", AveragePrice: 0.01}, // long: not ours
	}
	f.exch.imPerLot = 100
	f.startRun()

	eventually(t, 2*time.Second, "position loaded", func() bool { return len(f.state.AllPositions()) >= 1 })
	time.Sleep(30 * time.Millisecond)
	p := f.position("BTC-27DEC30-150000-C")
	if p == nil || p.Strike != 150000 || p.OptionType != "call" || p.Expiry.Year() != 2030 {
		t.Fatalf("parsed position = %+v", p)
	}
	if f.position(f.put) != nil {
		t.Error("long positions are not part of the short book")
	}
}

func TestStrategy_HeartbeatAndHedgeDoNotTrade(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.MinPremiumBTC = 1 // block entries: only background activity remains
	f.withOpenStrangle(0.1, 0.02)
	f.startRun()
	eventually(t, 2*time.Second, "reconciled", func() bool { return len(f.state.AllPositions()) == 2 })
	time.Sleep(50 * time.Millisecond)
	if n := len(f.exch.sells()) + len(f.exch.buys()); n != 0 {
		t.Errorf("a quiet book must not trade, got %d orders", n)
	}
}

// ── Journal snapshots and P&L ────────────────────────────────────────────────

type fakeOI struct{ snap *gex.OISnapshot }

func (f fakeOI) OpenInterest() *gex.OISnapshot { return f.snap }

func (f *strategyFixture) withOpenInterest() {
	f.oi = fakeOI{&gex.OISnapshot{AsOf: time.Now(), ByInstrument: map[string]float64{f.call: 1200, f.put: 800}}}
}

func TestStrategy_EveryDecisionIsJournaledWithMarketSnapshot(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenInterest()
	f.startRun()

	eventually(t, 2*time.Second, "entry submitted", func() bool { return len(f.journal.events(orders.EventSubmitted)) == 2 })
	labels := map[string]bool{}
	for _, o := range f.exch.sells() {
		if !strings.HasPrefix(o.Label, "short-strangle:45d:0.16:") {
			t.Errorf("order label = %q, want strategy and slot, then a per-order id", o.Label)
		}
		labels[o.Label] = true
	}
	if len(labels) != len(f.exch.sells()) {
		t.Error("every order needs its own label, so it can be cancelled alone")
	}
	f.exch.fill(f.exch.orderIDFor(f.call, 0), 0.1, 0.021)
	f.exch.fill(f.exch.orderIDFor(f.put, 0), 0.1, 0.021)
	eventually(t, 2*time.Second, "fills journaled", func() bool { return len(f.journal.events(orders.EventFilled)) == 2 })

	for _, e := range append(f.journal.events(orders.EventSubmitted), f.journal.events(orders.EventFilled)...) {
		m := e.ctx.Market
		if e.ctx.StrategyID != strategy.DefaultStrategyID || e.ctx.Slot == nil || e.ctx.Slot.DTE != 45 || e.ctx.Slot.Delta != 0.16 {
			t.Errorf("%s %s: strategy/slot = %q %+v", e.event, e.instrument, e.ctx.StrategyID, e.ctx.Slot)
		}
		if m.Spot != 100000 || m.DVOL != 55 || m.IVPercentile != 50 || m.Moneyness != "OTM" || m.DistanceToStrikePct <= 0 {
			t.Errorf("%s %s: market snapshot = %+v", e.event, e.instrument, m)
		}
		wantRank := 1 // call strike holds 1,200 of the expiry's 2,000 OI
		if e.instrument == f.put {
			wantRank = 2
		}
		if m.InstrumentOI == 0 || m.StrikeOIRank != wantRank || m.ExpiryOI != 2000 || m.MaxPainStrike == 0 {
			t.Errorf("%s %s: open interest not captured: %+v", e.event, e.instrument, m)
		}
		if m.Mid != 0.02 || !near(m.SpreadPct, 0.002/0.02*100, 1e-9) {
			t.Errorf("%s %s: liquidity not captured: %+v", e.event, e.instrument, m)
		}
	}
}

func TestStrategy_StopLossRealisesPnLPerSlot(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.01)
	f.market.setQuote(f.call, 0.039, 0.041) // call mid 0.04 = 4× premium → stop-loss
	f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill {
		if o.Direction == orders.DirectionBuy {
			return orders.Fill{Qty: o.Qty, FillPrice: 0.04}
		}
		return orders.Fill{}
	}
	f.startRun()

	eventually(t, 2*time.Second, "call stopped out", func() bool { return len(f.journal.events(orders.EventClosed)) >= 1 })
	closed := f.journal.events(orders.EventClosed)[0]
	if closed.trigger != orders.TriggerStopLoss200Pct || closed.orderType != orders.TypeMarket {
		t.Errorf("close journaled as %s/%s, want stop-loss/market", closed.trigger, closed.orderType)
	}
	if closed.ctx.Slot == nil || closed.ctx.Slot.DTE != 45 || closed.ctx.Market.Moneyness == "" {
		t.Errorf("close must carry its slot and snapshot: %+v", closed.ctx)
	}
	if b := f.exch.buys()[0]; !strings.HasPrefix(b.Label, "short-strangle:45d:0.16:") {
		t.Errorf("close order label = %q", b.Label)
	}

	// Premium 0.01 × 0.1 = 0.001; bought back 0.04 × 0.1 = 0.004 → realised −0.003 BTC.
	report := f.strat.PnLReport()
	slot, total := report[0], report[len(report)-1]
	if slot.Slot == nil || slot.Slot.DTE != 45 || !near(slot.Realised, -0.003, 1e-12) || slot.ClosedLegs != 1 {
		t.Errorf("slot P&L = %+v, want realised −0.003 over 1 close", slot)
	}
	if total.Slot != nil || !near(total.Realised, -0.003, 1e-12) || total.OpenLegs != 1 {
		t.Errorf("total P&L = %+v (the put stays open)", total)
	}
	// Unrealised on the open put: premium 0.001 − mid 0.02 × 0.1 = −0.001.
	if !near(total.Unrealised, -0.001, 1e-12) {
		t.Errorf("unrealised = %v, want −0.001", total.Unrealised)
	}
}

func TestStrategy_ReconciledPositionsAreJournaledWithSlot(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.startRun()

	eventually(t, 2*time.Second, "reconcile journaled", func() bool { return len(f.journal.events(orders.EventReconciled)) == 2 })
	for _, e := range f.journal.events(orders.EventReconciled) {
		if e.ctx.Slot == nil || e.ctx.Slot.DTE != 45 || e.ctx.Market.DVOL != 55 {
			t.Errorf("reconciled %s: %+v", e.instrument, e.ctx)
		}
	}
}

// A slot that cannot be entered is journaled once per reason, not every cycle.
func TestStrategy_SkippedEntryIsJournaledOncePerReason(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.MinPremiumBTC = 1 // every leg is below the floor
	f.startRun()

	eventually(t, 2*time.Second, "skip journaled", func() bool { return len(f.journal.events(orders.EventSkipped)) >= 1 })
	time.Sleep(100 * time.Millisecond) // ~10 more cycles
	skips := f.journal.events(orders.EventSkipped)
	if len(skips) != 1 {
		t.Fatalf("skip journaled %d times, want once", len(skips))
	}
	s := skips[0]
	if !strings.HasPrefix(s.reason, strategy.SkipEntryRejected) || s.ctx.Slot == nil || s.ctx.Market.DVOL != 55 || s.ctx.Market.Spot != 100000 {
		t.Errorf("skip = %+v", s)
	}
}

func TestStrategy_PeriodicPnLLinesPerSlotAndTotal(t *testing.T) {
	f := newStrategyFixture(t)
	f.cfg.ReportIntervalSec = 1
	f.withOpenStrangle(0.1, 0.02)
	f.startRun()

	eventually(t, 3*time.Second, "pnl journaled", func() bool {
		f.journal.mu.Lock()
		defer f.journal.mu.Unlock()
		return len(f.journal.pnl) >= 2
	})
	f.journal.mu.Lock()
	lines := append([]orders.PnLRecord(nil), f.journal.pnl[:2]...)
	f.journal.mu.Unlock()

	slot, total := lines[0], lines[1]
	if slot.Slot == nil || slot.Slot.DTE != 45 || total.Slot != nil {
		t.Fatalf("want one slot line then the total, got %+v / %+v", slot, total)
	}
	// Two legs: premium 0.002 each, mid 0.02 × 0.1 = 0.002 → unrealised 0.
	if total.OpenLegs != 2 || !near(total.Unrealised, 0, 1e-12) || total.Spot != 100000 || total.StrategyID != strategy.DefaultStrategyID {
		t.Errorf("total = %+v", total)
	}
	if !near(total.TotalUSD, total.Total*total.Spot, 1e-9) {
		t.Errorf("USD must use the recorded spot: %+v", total)
	}
}

// A persistent GEX action is announced once, not on every cycle.
func TestGammaMonitor_AnnouncesRegimeChangesOnce(t *testing.T) {
	var buf strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&lockedWriter{w: &buf}, nil)))
	defer slog.SetDefault(prev)

	f := newStrategyFixture(t)
	f.withPutSheddingRegime()
	f.startRun()
	time.Sleep(200 * time.Millisecond) // ~20 cycles in the same regime

	f.cancel()
	<-f.runErr
	f.runErr <- nil
	if n := strings.Count(buf.String(), `"msg":"gex_regime_trigger"`); n != 1 {
		t.Errorf("gex_regime_trigger logged %d times over many cycles, want 1", n)
	}
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
