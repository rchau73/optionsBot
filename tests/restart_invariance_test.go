package tests

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/gex"
	"optionsbot/internal/history"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// #220: a restart on an unchanged exchange book must not trade by itself.
// Each past startup bug has its own targeted test (#123 startup band, #147
// regime seed, #156 shared strikes and slots, held stops and sheds); this one
// restarts a book that has all of them at once, through the restore paths
// main uses: the journal replayed from orders.log (RestorePnL, RestoreStops
// — stops, shed anchors and opening slots), the regime history file and a
// fresh GEX snapshot.
//
// The book, two slots and three expiries:
//   - slot 25, expiry +25 d: a strangle whose put was stopped out 10 h ago
//     (held by repair_cooldown_hours);
//   - slot 50, expiry +50 d: the #156 shape — 95000-C 2.1 backs two
//     strangles, puts 1.8 and 0.5, call 0.2: three even strangles after the
//     split (1.6, 0.5, 0.2);
//   - slot 50, expiry +45 d: a strangle whose put was shed by GEX an hour ago
//     at flip 120,000 (anchored: spot 100,000 is under it).
//
// IM is 20.5 % of the margin balance, limit 20 %: inside the startup band.

const (
	restartCycles  = 5 // more than the position check's two-cycle confirmation
	restartBalance = 10.0
)

type restartLeg struct {
	name   string
	typ    string
	strike float64
	expiry time.Time
	size   float64 // exchange short size
	delta  float64
}

type restartBook struct {
	slot25, slot50 orders.SlotRef
	stopCall       string // E1's open call; its put was stopped out
	stopPut        string
	shedCall       string // E3's open call; its put was shed
	shedPut        string
	legs           []restartLeg
	journal        string // orders.log
	regimes        string // regime_history.jsonl
}

// newRestartBook sets the fixture's slots, config, instruments, exchange
// positions and margin, and writes the journal and regime history a bot that
// ran this book would have left behind.
func newRestartBook(t *testing.T, f *strategyFixture) *restartBook {
	t.Helper()
	now := time.Now().UTC()
	day := now.Truncate(24 * time.Hour)
	exp := func(days int) time.Time { return day.AddDate(0, 0, days).Add(8 * time.Hour) }
	label := func(e time.Time) string { return strings.ToUpper(e.Format("2Jan06")) }
	e1, e2, e3 := exp(25), exp(50), exp(45)
	name := func(e time.Time, strike float64, cp string) string {
		return fmt.Sprintf("BTC-%s-%.0f-%s", label(e), strike, cp)
	}

	f.cfg.DTEDeltaMatrix = []config.DTEDeltaEntry{{DTE: 25, Deltas: []float64{0.16}}, {DTE: 50, Deltas: []float64{0.16}}}
	f.cfg.RepairCooldownHours = 72
	f.cfg.RebalanceStartupBandPct = config.FixedRebalanceStartupBandPct
	f.cfg.GammaFlipBufferSD = 1.5
	f.cfg.GEXShedConfirmSnapshots = config.FixedGEXConfirmSnapshots
	f.cfg.GEXRepairConfirmSnapshots = config.FixedGEXConfirmSnapshots
	f.cfg.GEXRepairReleaseHours = config.FixedGEXRepairReleaseHours
	f.cfg.IVBandConfirmDays = config.FixedIVBandConfirmDays
	f.cfg.DeltaExitThreshold = 0.30

	b := &restartBook{
		slot25:   orders.SlotRef{DTE: 25, Delta: 0.16},
		slot50:   orders.SlotRef{DTE: 50, Delta: 0.16},
		stopCall: name(e1, 115000, "C"), stopPut: name(e1, 88000, "P"),
		shedCall: name(e3, 118000, "C"), shedPut: name(e3, 85000, "P"),
		journal: filepath.Join(t.TempDir(), "orders.log"),
		regimes: filepath.Join(t.TempDir(), "regime_history.jsonl"),
	}
	c95, c93 := name(e2, 95000, "C"), name(e2, 93000, "C")
	p72, p73 := name(e2, 72000, "P"), name(e2, 73000, "P")
	b.legs = []restartLeg{
		{b.stopCall, "call", 115000, e1, 0.3, 0.16},
		{c95, "call", 95000, e2, 2.1, 0.16},
		{c93, "call", 93000, e2, 0.2, 0.19},
		{p72, "put", 72000, e2, 1.8, -0.15},
		{p73, "put", 73000, e2, 0.5, -0.17},
		{b.shedCall, "call", 118000, e3, 0.4, 0.16},
	}

	// Market data: a live quote at mark 0.02 for every held leg (and the two
	// missing puts), so no exit rule applies.
	f.market.mu.Lock()
	template := *f.market.instruments[f.call]
	f.market.instruments = map[string]*marketdata.Instrument{}
	f.market.mu.Unlock()
	addInst := func(l restartLeg) {
		inst := template
		inst.Name, inst.OptionType, inst.Strike, inst.Expiry = l.name, l.typ, l.strike, l.expiry
		inst.Greeks.Delta = l.delta
		f.market.mu.Lock()
		f.market.instruments[l.name] = &inst
		f.market.mu.Unlock()
	}
	for _, l := range b.legs {
		addInst(l)
	}
	addInst(restartLeg{b.stopPut, "put", 88000, e1, 0, -0.16})
	addInst(restartLeg{b.shedPut, "put", 85000, e3, 0, -0.16})

	f.exch.positions = nil
	for _, l := range b.legs {
		f.exch.positions = append(f.exch.positions, orders.RawPosition{InstrumentName: l.name, Size: -l.size, Direction: "sell",
			AveragePrice: 0.02, MarkPrice: 0.02, Delta: l.delta * l.size})
	}
	f.exch.summary.MarginBalance, f.exch.summary.Equity = restartBalance, restartBalance
	f.exch.setMargin(0.205*restartBalance, 0.1*restartBalance) // IM 20.5 % of a 20 % limit: inside the band

	// Live GEX: positive regime, fresh. Spot 100,000 is under the shed's
	// anchor (120,000 + buffer), so the shed put stays anchored.
	f.gex = fixedGEX{&gex.Snapshot{ComputedAt: now, Regime: "POSITIVE", Spot: 100000, GammaFlip: 90000, GammaFlipFound: true}}

	// The journal the running bot wrote: every leg opened by its slot five
	// days ago, the E1 put stopped out 10 h ago, the E3 put shed 1 h ago.
	l, err := orders.NewLogger(b.journal, 0)
	if err != nil {
		t.Fatal(err)
	}
	at := func(d time.Duration) orders.MarketSnapshot {
		return orders.MarketSnapshot{AsOf: now.Add(-d), Spot: 100000, DVOL: 55}
	}
	open := func(inst, typ string, qty float64, slot orders.SlotRef) {
		s := slot
		p := &orders.Position{Instrument: inst, OptionType: typ, Qty: qty, PremiumReceived: 0.02 * qty}
		l.LogOpen(p, orders.Fill{Qty: qty, FillPrice: 0.02}, orders.TriggerEntry,
			orders.EventContext{StrategyID: strategy.DefaultStrategyID, Slot: &s, Market: at(5 * 24 * time.Hour)})
	}
	open(b.stopCall, "call", 0.3, b.slot25)
	open(b.stopPut, "put", 0.3, b.slot25)
	for _, s := range []struct {
		call, put string
		qty       float64
	}{{c95, p72, 1.6}, {c95, p73, 0.5}, {c93, p72, 0.2}} {
		open(s.call, "call", s.qty, b.slot50)
		open(s.put, "put", s.qty, b.slot50)
	}
	open(b.shedCall, "call", 0.4, b.slot50)
	open(b.shedPut, "put", 0.4, b.slot50)

	stopSlot, shedSlot := b.slot25, b.slot50
	stopped := &orders.Position{Instrument: b.stopPut, OptionType: "put", Qty: 0.3, PremiumReceived: 0.006}
	l.LogClose(stopped, orders.Fill{Qty: 0.3, FillPrice: 0.05}, orders.TriggerStopLoss200Pct, orders.TypeMarket,
		orders.EventContext{StrategyID: strategy.DefaultStrategyID, Slot: &stopSlot, Detail: "stop-loss", Market: at(10 * time.Hour)})
	shedMkt := at(time.Hour)
	shedMkt.GammaFlip, shedMkt.FlipBufferPct, shedMkt.GEXRegime = 120000, 4.3, "NEGATIVE/ACCELERATION"
	shed := &orders.Position{Instrument: b.shedPut, OptionType: "put", Qty: 0.4, PremiumReceived: 0.008}
	l.LogClose(shed, orders.Fill{Qty: 0.4, FillPrice: 0.03}, orders.TriggerGammaClose, orders.TypeMarket,
		orders.EventContext{StrategyID: strategy.DefaultStrategyID, Slot: &shedSlot, Detail: "GEX shed (sma bear)", Market: shedMkt})
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}

	// Regime history: two confirmed positive closes (not a brand-new bot).
	rs, err := history.OpenRegimes(b.regimes)
	if err != nil {
		t.Fatal(err)
	}
	rs.Record(day.AddDate(0, 0, -2), "POSITIVE")
	rs.Record(day.AddDate(0, 0, -1), "POSITIVE")
	if err := rs.Close(); err != nil {
		t.Fatal(err)
	}
	return b
}

// restartRun is one boot of the bot: what main does at startup, on the
// fixture's exchange and market, then Strategy.Run for restartCycles cycles.
type restartRun struct {
	strat *strategy.Strategy
	state *orders.StateManager
}

// bootAndRun replays the journal, opens the regime history and runs a new
// Strategy for restartCycles decision cycles, then stops it. restoreStops
// false skips RestoreStops (the guard scenario).
func bootAndRun(t *testing.T, f *strategyFixture, b *restartBook, restoreStops bool) restartRun {
	t.Helper()
	replay, err := orders.ReplayFile(b.journal, orders.RecentEvents)
	if err != nil {
		t.Fatal(err)
	}
	logger, err := orders.NewLogger(b.journal, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	logger.Restore(replay)
	regimes, err := history.OpenRegimes(b.regimes)
	if err != nil {
		t.Fatal(err)
	}
	defer regimes.Close()

	state := orders.NewStateManager()
	strat := strategy.New(f.cfg, strategy.Deps{
		Market: f.market, Exchange: f.exch, State: state, Journal: logger, Hedge: nopHedge{},
		GEX: f.gex, Regimes: regimes,
	})
	strat.RestorePnL(replay.Realised, replay.Closed)
	if restoreStops {
		strat.RestoreStops(replay.Trades)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- strat.Run(ctx) }()

	seen := map[time.Time]bool{}
	eventually(t, 5*time.Second, fmt.Sprintf("%d decision cycles", restartCycles), func() bool {
		if at := strat.View().LoopAt; !at.IsZero() {
			seen[at] = true
		}
		return len(seen) >= restartCycles
	})
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Strategy.Run did not stop after cancel")
	}
	return restartRun{strat: strat, state: state}
}

// sideEffects lists every private call with a side effect the exchange saw,
// other than the startup cancel_all_by_currency.
func sideEffects(f *strategyFixture, wantCancelAll int) []string {
	f.exch.mu.Lock()
	defer f.exch.mu.Unlock()
	var out []string
	for _, o := range f.exch.submitted {
		out = append(out, fmt.Sprintf("%s %s %g (%s)", o.Direction, o.Instrument, o.Qty, o.TriggerReason))
	}
	for _, a := range f.exch.amended {
		out = append(out, "edit "+a)
	}
	for _, id := range f.exch.cancelled {
		out = append(out, "cancel "+id)
	}
	for _, l := range f.exch.cancelledLabels {
		out = append(out, "cancel_by_label "+l)
	}
	if n := len(f.exch.cancelAll); n != wantCancelAll {
		out = append(out, fmt.Sprintf("cancel_all_by_currency × %d, want %d (one per startup)", n, wantCancelAll))
	}
	return out
}

// strangleSlots maps each strangle (its legs and sizes) to its slot.
func strangleSlots(state *orders.StateManager) map[string]int {
	out := map[string]int{}
	for _, st := range state.AllStrangles() {
		out[strangleKey(st)] = st.TargetDTE
	}
	return out
}

func strangleKey(st *orders.Strangle) string {
	leg := func(p *orders.Position) string {
		if p == nil {
			return "-"
		}
		return fmt.Sprintf("%s %.1f", p.Instrument, p.Qty)
	}
	return leg(st.CallLeg) + " / " + leg(st.PutLeg)
}

// bookProblems checks the restarted book against the exchange and run 1:
// every exchange short is in the book, every book position in exactly one
// strangle, every strangle in its run-1 slot, and the stopped and shed puts
// still held.
func bookProblems(f *strategyFixture, b *restartBook, r restartRun, slots map[string]int) []string {
	var out []string
	held := map[string]float64{}
	inStrangle := map[string]int{} // position ID → strangles holding it
	for _, st := range r.state.AllStrangles() {
		for _, p := range []*orders.Position{st.CallLeg, st.PutLeg} {
			if p != nil {
				inStrangle[p.ID]++
			}
		}
	}
	for _, p := range r.state.AllPositions() {
		held[p.Instrument] += p.Qty
		if n := inStrangle[p.ID]; n != 1 {
			out = append(out, fmt.Sprintf("position %s %s %.1f is in %d strangles", p.ID, p.Instrument, p.Qty, n))
		}
	}
	for _, l := range b.legs {
		if math.Abs(held[l.name]-l.size) > 1e-9 {
			out = append(out, fmt.Sprintf("%s: book %.1f, exchange %.1f", l.name, held[l.name], l.size))
		}
	}
	got := strangleSlots(r.state)
	for k, dte := range slots {
		if got[k] != dte {
			out = append(out, fmt.Sprintf("strangle %s: slot %d, was %d", k, got[k], dte))
		}
	}
	if len(got) != len(slots) {
		out = append(out, fmt.Sprintf("%d strangles, was %d", len(got), len(slots)))
	}

	rules := map[string]string{}
	for _, h := range r.strat.View().HeldLegs {
		rules[h.OpenLeg] = h.Rule
	}
	if rules[b.stopCall] != strategy.HoldStopped {
		out = append(out, fmt.Sprintf("stopped put: hold %q, want %q", rules[b.stopCall], strategy.HoldStopped))
	}
	if rules[b.shedCall] != strategy.HoldGEXShed {
		out = append(out, fmt.Sprintf("shed put: hold %q, want %q", rules[b.shedCall], strategy.HoldGEXShed))
	}
	return out
}

// Scenario: restart on an unchanged book.
func TestRestartInvariance_RestartOnAnUnchangedBook(t *testing.T) {
	f := newStrategyFixture(t)
	b := newRestartBook(t, f)

	run1 := bootAndRun(t, f, b, true)
	if fx := sideEffects(f, 1); len(fx) > 0 {
		t.Fatalf("run 1 traded on its own book:\n  %s", strings.Join(fx, "\n  "))
	}
	slots := strangleSlots(run1.state)
	want := map[string]int{
		b.stopCall + " 0.3 / -": 25,
		b.shedCall + " 0.4 / -": 50,
	}
	for k, dte := range want {
		if slots[k] != dte {
			t.Errorf("run 1: strangle %s in slot %d, want %d (all: %v)", k, slots[k], dte, sortedKeys(slots))
		}
	}
	if len(slots) != 5 {
		t.Errorf("run 1: %d strangles, want 5 (the #156 split gives three on +50 d): %v", len(slots), sortedKeys(slots))
	}
	if p := bookProblems(f, b, run1, slots); len(p) > 0 {
		t.Fatalf("run 1 book:\n  %s", strings.Join(p, "\n  "))
	}

	run2 := bootAndRun(t, f, b, true)
	if fx := sideEffects(f, 2); len(fx) > 0 {
		t.Errorf("the restart traded:\n  %s", strings.Join(fx, "\n  "))
	}
	if p := bookProblems(f, b, run2, slots); len(p) > 0 {
		t.Errorf("the restarted book:\n  %s", strings.Join(p, "\n  "))
	}
}

// Scenario: the test guards each restore path. Without RestoreStops the
// stop record, the shed anchor and the journal's slots are lost: the checks
// above must catch it (the restart re-sells the held puts).
func TestRestartInvariance_GuardsRestoreStops(t *testing.T) {
	f := newStrategyFixture(t)
	b := newRestartBook(t, f)

	run1 := bootAndRun(t, f, b, true)
	slots := strangleSlots(run1.state)
	run2 := bootAndRun(t, f, b, false)
	fx := sideEffects(f, 2)
	problems := bookProblems(f, b, run2, slots)
	if len(fx) == 0 && len(problems) == 0 {
		t.Fatal("a restart without RestoreStops passed the restart checks: the test no longer guards that path")
	}
	sold := map[string]bool{}
	for _, o := range f.exch.sells() {
		sold[o.Instrument] = true
	}
	if !sold[b.stopPut] || !sold[b.shedPut] {
		t.Errorf("without RestoreStops the stopped and shed puts are re-sold; sells: %v", fx)
	}
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, fmt.Sprintf("%s → %d", k, v))
	}
	sort.Strings(out)
	return out
}
