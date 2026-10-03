// Command monitor-demo serves the real monitor API backed by a simulated
// strategy, so the frontend can be developed and demoed without a running bot
// or exchange credentials. It never connects to Deribit.
//
//	go run ./cmd/monitor-demo                     # BTC on :8081
//	go run ./cmd/monitor-demo -underlying ETH -addr 127.0.0.1:8082
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"optionsbot/internal/account"
	"optionsbot/internal/api"
	"optionsbot/internal/history"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8081", "listen address")
	underlying := flag.String("underlying", "BTC", "BTC or ETH")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	sim := newSimulation(*underlying)
	hist, err := demoHistory(*underlying, sim.spot)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer hist.Close()
	sim.history = hist
	go sim.run(ctx)

	slog.Info("monitor demo: simulated data, no exchange connection", "underlying", *underlying, "addr", *addr)
	if err := api.New(sim, sim.journal, api.WithPnLHistory(hist), api.WithAccount(sim)).ListenAndServe(ctx, *addr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// simulation fakes a running strategy: spot follows a random walk, option
// marks follow spot, and now and then a leg rolls, a stop fires or an entry
// is skipped — each written to a real journal so /api/events looks real.
type simulation struct {
	mu         sync.Mutex
	underlying string
	spot       float64
	dvol       float64
	legs       []*simLeg
	realised   float64
	closed     int
	journal    *orders.Logger
	history    *history.Store
	started    time.Time
}

// demoHistory opens a throwaway history file pre-filled with 30 days of a
// synthetic P&L walk (one point every 10 minutes), so every chart range shows data.
func demoHistory(underlying string, spot float64) (*history.Store, error) {
	path := filepath.Join(os.TempDir(), "optionsbot-monitor-demo-"+underlying+".jsonl")
	os.Remove(path) // fresh history on every demo start
	h, err := history.Open(path, 366*24*time.Hour, time.Now())
	if err != nil {
		return nil, err
	}
	realised, unrealised := 0.0, 0.0
	for t := time.Now().AddDate(0, 0, -30); t.Before(time.Now()); t = t.Add(10 * time.Minute) {
		realised += 0.00004 + rand.NormFloat64()*0.0002
		unrealised = 0.6*unrealised + rand.NormFloat64()*0.002
		h.RecordPnL(t, realised, unrealised, spot)
	}
	// Reopen so later points continue from this level (it becomes the base).
	if err := h.Close(); err != nil {
		return nil, err
	}
	return history.Open(path, 366*24*time.Hour, time.Now())
}

type simLeg struct {
	strangle string
	slot     orders.SlotRef
	typ      string
	strike   float64
	expiry   time.Time
	qty      float64
	entry    float64
	mark     float64
}

func newSimulation(underlying string) *simulation {
	spot := 100000.0
	if underlying == "ETH" {
		spot = 3500
	}
	s := &simulation{underlying: underlying, spot: spot, dvol: 52, journal: orders.NewWriterLogger(io.Discard, 0.05), started: time.Now()}
	for i, slot := range []orders.SlotRef{{DTE: 25, Delta: 0.16}, {DTE: 45, Delta: 0.16}, {DTE: 60, Delta: 0.18}} {
		exp := time.Now().AddDate(0, 0, slot.DTE).Truncate(24 * time.Hour).Add(8 * time.Hour)
		id := fmt.Sprintf("st-%d", i+1)
		k := spot * (0.06 + float64(slot.DTE)/600)
		s.legs = append(s.legs,
			&simLeg{strangle: id, slot: slot, typ: "call", strike: roundStrike(spot+k, spot), expiry: exp, qty: 0.3, entry: 0.012, mark: 0.012},
			&simLeg{strangle: id, slot: slot, typ: "put", strike: roundStrike(spot-k, spot), expiry: exp, qty: 0.3, entry: 0.014, mark: 0.014},
		)
	}
	return s
}

func roundStrike(k, spot float64) float64 {
	step := 1000.0
	if spot < 10000 {
		step = 50
	}
	return math.Round(k/step) * step
}

func (s *simulation) run(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.step()
		}
	}
}

func (s *simulation) step() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spot *= 1 + rand.NormFloat64()*0.0015
	s.dvol = math.Max(30, s.dvol+rand.NormFloat64()*0.2)
	for _, l := range s.legs {
		moneyness := (s.spot - l.strike) / s.spot
		if l.typ == "put" {
			moneyness = -moneyness
		}
		l.mark = math.Max(0.0005, l.entry*math.Exp(moneyness*12)*(1+rand.NormFloat64()*0.01))
	}
	if s.history != nil {
		unrealised := 0.0
		for _, l := range s.legs {
			unrealised += l.entry*l.qty - l.mark*l.qty
		}
		s.history.RecordPnL(time.Now(), s.realised, unrealised, s.spot)
	}
	switch r := rand.Float64(); {
	case r < 0.04:
		s.rollRandomLeg("rollout_roi")
	case r < 0.06:
		s.journal.LogSkipped("no_expiry", s.ctx(nil, &orders.SlotRef{DTE: 25, Delta: 0.16}))
	case r < 0.10:
		l := s.legs[rand.Intn(len(s.legs))]
		s.journal.LogSubmit(orders.PendingOrderRecord{OrderID: fmt.Sprintf("o-%d", rand.Intn(1e6)), Instrument: s.name(l),
			OptionType: l.typ, Direction: orders.DirectionSell, OrderType: orders.TypeLimit, TriggerReason: orders.TriggerEntry,
			Qty: l.qty, LimitPrice: l.mark}, s.ctx(l, &l.slot))
	}
}

func (s *simulation) rollRandomLeg(reason string) {
	l := s.legs[rand.Intn(len(s.legs))]
	pos := s.position(l)
	fill := orders.Fill{OrderID: fmt.Sprintf("o-%d", rand.Intn(1e6)), FillPrice: l.mark, Qty: l.qty, Timestamp: time.Now()}
	s.journal.LogClose(pos, fill, reason, orders.TypeLimit, s.ctx(l, &l.slot))
	s.realised += pos.PremiumReceived - l.mark*l.qty
	s.closed++
	l.entry, l.mark = 0.012, 0.012 // re-sold at a fresh premium
	s.journal.LogOpen(s.position(l), orders.Fill{FillPrice: l.entry, Qty: l.qty, Timestamp: time.Now()}, s.ctx(l, &l.slot))
}

func (s *simulation) name(l *simLeg) string {
	c := "C"
	if l.typ == "put" {
		c = "P"
	}
	return fmt.Sprintf("%s-%s-%.0f-%s", s.underlying, l.expiry.Format("2Jan06"), l.strike, c)
}

func (s *simulation) position(l *simLeg) *orders.Position {
	return &orders.Position{
		ID: s.name(l), Instrument: s.name(l), OptionType: l.typ, Strike: l.strike, Expiry: l.expiry,
		Qty: l.qty, EntryPrice: l.entry, PremiumReceived: l.entry * l.qty, CurrentMid: l.mark,
		UnderlyingPrice: s.spot, EntryTime: s.started, CurrentGreeks: s.greeks(l),
	}
}

func (s *simulation) greeks(l *simLeg) orders.Greeks {
	d := 0.16 + (s.spot-l.strike)/s.spot*2
	if l.typ == "put" {
		d = -0.16 + (s.spot-l.strike)/s.spot*2
	}
	return orders.Greeks{Delta: d, Gamma: 0.00002, Theta: -15, Vega: 40, IV: s.dvol / 100}
}

func (s *simulation) ctx(l *simLeg, slot *orders.SlotRef) orders.EventContext {
	m := orders.MarketSnapshot{AsOf: time.Now(), Spot: s.spot, DVOL: s.dvol, IVPercentile: 55, GEXRegime: "POSITIVE/PINNING"}
	if l != nil {
		m.Strike = l.strike
		m.Moneyness, m.DistanceToStrikePct = strategy.Moneyness(l.typ, s.spot, l.strike)
		m.Mid = l.mark
		m.StrikeOI = 800 + rand.Float64()*1500
		m.StrikeOIRank = 1 + rand.Intn(8)
	}
	return orders.EventContext{StrategyID: strategy.DefaultStrategyID, Slot: slot, Market: m}
}

// View implements api.StrategySource with the simulated state.
func (s *simulation) View() strategy.View {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	v := strategy.View{
		AsOf: now, StrategyID: strategy.DefaultStrategyID, Underlying: s.underlying, Environment: "testnet (demo)",
		LoopAt: now, Trend: "neutral",
		Market:  orders.MarketSnapshot{AsOf: now, Spot: s.spot, DVOL: s.dvol, IVPercentile: 55, GEXRegime: "POSITIVE/PINNING", GammaFlip: s.spot * 0.96},
		Account: strategy.AccountView{Equity: 2.5, MarginUsed: 0.42, MarginAllowed: 3.5, AsOf: now},
		Pending: []strategy.PendingView{},
	}
	bySlot := map[string]*strategy.StrangleView{}
	var unrealised, delta float64
	for _, l := range s.legs {
		st, ok := bySlot[l.strangle]
		if !ok {
			st = &strategy.StrangleView{ID: l.strangle, Slot: l.slot, OpenedAt: s.started}
			bySlot[l.strangle] = st
		}
		pos := s.position(l)
		mon, dist := strategy.Moneyness(l.typ, s.spot, l.strike)
		st.Legs = append(st.Legs, strategy.LegView{
			PositionID: pos.ID, Instrument: pos.Instrument, OptionType: l.typ, Strike: l.strike, Expiry: l.expiry,
			DTE: l.expiry.Sub(now).Hours() / 24, Qty: l.qty, EntryPrice: l.entry, Mark: l.mark,
			Bid: l.mark * 0.97, Ask: l.mark * 1.03, MarkSource: "live", MarkAsOf: now,
			PremiumReceived: pos.PremiumReceived, UnrealisedPnL: pos.MtMPnL(), ROIPct: pos.ROIPct() * 100,
			LossMultiple: pos.LossPct(), StopLossMark: pos.PremiumReceived * 3 / l.qty,
			Moneyness: mon, DistancePct: dist, Greeks: pos.CurrentGreeks,
		})
		unrealised += pos.MtMPnL()
		delta -= pos.CurrentGreeks.Delta * l.qty
	}
	for _, id := range []string{"st-1", "st-2", "st-3"} {
		v.Strangles = append(v.Strangles, *bySlot[id])
	}
	v.Greeks = orders.MarketContext{NetDelta: delta, NetGamma: -0.00012, NetVega: -72, NetTheta: 27}
	v.PnL = []strategy.PnLView{{Realised: s.realised, Unrealised: unrealised, Total: s.realised + unrealised, OpenLegs: len(s.legs), ClosedLegs: s.closed}}
	return v
}

// Status implements api.AccountSource with a simulated cross-collateral
// portfolio-margin account whose margin moves with spot.
func (s *simulation) Status() account.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	usage := 0.18 + (100000-s.spot)/100000*2 // margin rises as spot falls
	btc := account.Summary{
		Currency: "BTC", Balance: 1.25, Equity: 1.27, MarginBalance: 1.27, AvailableFunds: 1.27 * (1 - usage),
		AvailableWithdrawalFunds: 1.0, InitialMargin: 1.27 * usage, MaintenanceMargin: 1.27 * usage * 0.7,
		ProjectedInitialMargin: 1.27 * usage * 0.9, ProjectedMaintenanceMargin: 1.27 * usage * 0.63,
		MarginModel: "cross_pm", PortfolioMarginingEnabled: true, CrossCollateralEnabled: true,
		TotalEquityUSD: 1.27*s.spot + 9.5*3500 + 25000, TotalMarginBalanceUSD: 1.27*s.spot + 9.5*3500 + 25000,
		TotalInitialMarginUSD: 1.27 * usage * s.spot, TotalMaintenanceMarginUSD: 1.27 * usage * 0.7 * s.spot,
	}
	eth := account.Summary{Currency: "ETH", Balance: 9.5, Equity: 9.5, MarginBalance: 9.5, AvailableFunds: 9.1, AvailableWithdrawalFunds: 9.1,
		InitialMargin: 0.4, MaintenanceMargin: 0.28, MarginModel: "cross_pm", CrossCollateralEnabled: true}
	usdc := account.Summary{Currency: "USDC", Balance: 25000, Equity: 25000, MarginBalance: 25000, AvailableFunds: 25000, AvailableWithdrawalFunds: 25000,
		MarginModel: "cross_pm", CrossCollateralEnabled: true}
	return account.Status{Snapshot: account.Build([]account.Summary{btc, eth, usdc}, "demo", time.Now())}
}
