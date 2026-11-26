// Command monitor-demo serves the real monitor API backed by a simulated
// strategy, so the frontend can be developed and demoed without a running bot
// or exchange credentials. It never connects to Deribit.
//
//	go run ./cmd/monitor-demo                     # BTC on :8081
//	go run ./cmd/monitor-demo -underlying ETH -addr 127.0.0.1:8082
//	go run ./cmd/monitor-demo -scenario mixed     # a losing month among winning ones (#203)
//	go run ./cmd/monitor-demo -scenario hedged    # the futures hedge on (#827)
//
// -scenario good|mixed|drawdown|hedged shapes the investor summary for design
// and user tests: steady income, a losing month, an open loss near its stop
// with a worst case large next to the month's income, or the same book as the
// default demo with the futures hedge on — a perpetual that offsets the
// book's lean, traded now and then (journaled hedge_fill lines, so the Last
// action can be a hedge trade, and served on /api/hedge; the worst case
// counts it as the real API does). Without it the demo is as before. A
// scenario reports deployment "demo" and version "scenario <name>" in
// /api/status, so it is never mistaken for a track record.
package main

import (
	"context"
	"errors"
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
	"optionsbot/internal/config"
	"optionsbot/internal/hedge"
	"optionsbot/internal/history"
	"optionsbot/internal/orders"
	"optionsbot/internal/risk"
	"optionsbot/internal/strategy"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8081", "listen address")
	underlying := flag.String("underlying", "BTC", "BTC or ETH")
	scenario := flag.String("scenario", "", "investor-summary scenario: good | mixed | drawdown | hedged (default: the usual demo)")
	flag.Parse()
	if !validScenario(*scenario) {
		fmt.Fprintf(os.Stderr, "unknown -scenario %q: want good, mixed, drawdown or hedged\n", *scenario)
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	sim := newSimulation(*underlying, *scenario)
	hist, err := demoHistory(*underlying, sim.spot)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer hist.Close()
	sim.history = hist
	go sim.run(ctx)

	slog.Info("monitor demo: simulated data, no exchange connection", "underlying", *underlying, "addr", *addr, "scenario", *scenario)
	opts := []api.Option{api.WithPnLHistory(hist), api.WithAccount(sim)}
	if *scenario != "" {
		opts = append(opts, api.WithDeployment("demo", "scenario "+*scenario))
	}
	if *scenario == scenarioHedged {
		// No token: GET /api/hedge only, the switch is not served.
		opts = append(opts, api.WithFuturesHedge(sim, "", false))
	}
	if err := api.New(sim, sim.journal, opts...).ListenAndServe(ctx, *addr); err != nil {
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
	base       float64 // starting spot: the simulated SMAs and swings sit around it
	trend      string  // last SMA trend, so a change is journaled once
	scenario   string  // "" | good | mixed | drawdown | hedged
	steps      int     // ticks so far: the first always journals an order
	// The hedged scenario's perpetual (#827): the book of its fills, when the
	// hedge was switched on, and when it last traded.
	perp        hedge.PerpBook
	hedgeSince  time.Time
	hedgeTrades []time.Time
}

// The investor-summary scenarios (#203, #827).
const (
	scenarioGood     = "good"     // steady monthly income
	scenarioMixed    = "mixed"    // a losing month among winning ones
	scenarioDrawdown = "drawdown" // an open loss near its stop, a large worst case
	scenarioHedged   = "hedged"   // the default book with the futures hedge on
)

func validScenario(s string) bool {
	switch s {
	case "", scenarioGood, scenarioMixed, scenarioDrawdown, scenarioHedged:
		return true
	}
	return false
}

// The hedged scenario's numbers (#827).
const (
	// hedgedSpotMove is where spot starts against the price the strangles
	// were sold at: a 3 % fall, so the book leans to the puts (it gains as
	// the coin rises) and the perpetual is sold against it.
	hedgedSpotMove = 0.97
	// hedgeLeanPct: the perpetual trades when the worst case's two moves
	// differ by more than this % of the larger one, and the trade makes them
	// equal at the model's prices.
	hedgeLeanPct = 10
	// perpContractUSD is the perpetual's contract size (Deribit BTC: 10 USD).
	perpContractUSD = 10
	// perpTakerFee is Deribit's perpetual taker fee rate.
	perpTakerFee = 0.0005
	// demoEquity is the equity the view reports (coin), for % of equity.
	demoEquity = 2.5
	// hedgeBandPct and hedgeMaxPerHour are the real hedge's band and order
	// limit, shown on /api/hedge (the demo's book is small for its 2.5 coin,
	// so it trades on the lean of the worst case instead).
	hedgeBandPct    = 5
	hedgeMaxPerHour = 12
)

// drawdownMarkMultiple pins the drawdown scenario's stressed put at this
// multiple of its entry price: 2.6 of the 3× stop mark (premium + 2×
// premium lost) is within 20 % of the stop.
const drawdownMarkMultiple = 2.6

// smaLevels simulates the SMA trend's levels around the starting spot and
// reads the trend with the bot's own rule. Call with s.mu held.
func (s *simulation) smaLevels() strategy.SMALevels {
	day := func(ago int) string { return s.started.UTC().AddDate(0, 0, -ago).Format(time.DateOnly) }
	l := strategy.SMALevels{
		SMA9: s.base * 0.997, SMA21: s.base * 1.004, Price: s.spot,
		SwingHigh: s.base * 1.03, SwingHighDate: day(12),
		SwingLow: s.base * 0.985, SwingLowDate: day(8),
	}
	l.TrendDir, l.Reason = strategy.SMATrendOf(l.Price, l.SwingHigh, l.SwingLow, l.SMA9, l.SMA21)
	l.Trend = map[int]string{1: "bull", -1: "bear", 0: "neutral"}[l.TrendDir]
	return l
}

// demoCandles is CandleDays simulated daily candles around base and today's
// live candle at spot, for the regime chart.
func (s *simulation) demoCandles() []strategy.Candle {
	var closes []orders.DailyClose
	today := time.Now().UTC().Truncate(24 * time.Hour)
	prev := s.base
	for i := strategy.CandleDays; i >= 1; i-- {
		c := s.base * (1 + 0.04*math.Sin(float64(i)/7) + 0.01*math.Cos(float64(i)*1.3))
		closes = append(closes, orders.DailyClose{Date: today.AddDate(0, 0, -i), Open: prev, Close: c,
			High: math.Max(prev, c) * 1.012, Low: math.Min(prev, c) * 0.988})
		prev = c
	}
	live := orders.DailyClose{Date: today, Open: prev, Close: s.spot, High: math.Max(prev, s.spot) * 1.004, Low: math.Min(prev, s.spot) * 0.996}
	return strategy.CandlesOf(closes, live, strategy.CandleDays)
}

// journalTrendChange writes a trend_changed event when the simulated SMA
// trend changes. Call with s.mu held.
func (s *simulation) journalTrendChange() {
	l := s.smaLevels()
	prev := s.trend
	s.trend = l.Trend
	if prev == "" || prev == l.Trend {
		return
	}
	s.journal.LogTrend(orders.TrendRecord{
		StrategyID: strategy.DefaultStrategyID, Direction: strategy.DirectionSMA,
		From: prev, To: l.Trend, Reason: l.Reason, Spot: l.Price,
		Detail: fmt.Sprintf("SMA trend %s → %s: %s (demo)", prev, l.Trend, l.Reason),
		SMA9:   l.SMA9, SMA21: l.SMA21, SwingHigh: l.SwingHigh, SwingHighDate: l.SwingHighDate,
		SwingLow: l.SwingLow, SwingLowDate: l.SwingLowDate,
	})
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
	start := time.Now().AddDate(0, 0, -30)
	for t := start; t.Before(time.Now()); t = t.Add(10 * time.Minute) {
		day := t.Sub(start).Hours() / 24
		drift := 0.00004
		if day >= 12 && day < 19 { // a losing week, so /api/performance shows a drawdown
			drift = -0.0001
		}
		realised += drift + rand.NormFloat64()*0.0002
		unrealised = 0.6*unrealised + rand.NormFloat64()*0.002
		pointSpot := spot * (0.94 + 0.06*day/30) // the coin rose ~6 % over the month
		h.RecordPnL(t, realised, unrealised, pointSpot, demoMargin(underlying))
	}
	// Reopen so later points continue from this level (it becomes the base).
	if err := h.Close(); err != nil {
		return nil, err
	}
	return history.Open(path, 366*24*time.Hour, time.Now())
}

// demoMargin is a synthetic margin view: IM around half of a 20 % limit,
// entries blocked one point in ten, a margin balance of 2 coins.
func demoMargin(underlying string) *history.Margin {
	return &history.Margin{Known: true, IMPct: 8 + rand.Float64()*6, IMLimitPct: 20, MMPct: 3,
		EntriesAllowed: rand.Float64() > 0.1, MarginBalance: 2, Equity: 2, Unit: underlying}
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
	// pinned: the drawdown scenario holds this leg's mark at
	// drawdownMarkMultiple × entry, near its stop.
	pinned bool
}

func newSimulation(underlying, scenario string) *simulation {
	spot := 100000.0
	if underlying == "ETH" {
		spot = 3500
	}
	s := &simulation{underlying: underlying, spot: spot, base: spot, dvol: 52, journal: orders.NewWriterLogger(io.Discard, 0.05), started: time.Now(), scenario: scenario}
	s.journal.Restore(demoTrades(spot, scenario, time.Now()))
	qty := 0.3
	if scenario == scenarioDrawdown {
		qty = 1.5 // a book large next to its income: the worst case dwarfs the month
	}
	for i, slot := range []orders.SlotRef{{DTE: 25, Delta: 0.16}, {DTE: 45, Delta: 0.16}, {DTE: 60, Delta: 0.18}} {
		exp := time.Now().AddDate(0, 0, slot.DTE).Truncate(24 * time.Hour).Add(8 * time.Hour)
		id := fmt.Sprintf("st-%d", i+1)
		k := spot * (0.06 + float64(slot.DTE)/600)
		s.legs = append(s.legs,
			&simLeg{strangle: id, slot: slot, typ: "call", strike: roundStrike(spot+k, spot), expiry: exp, qty: qty, entry: 0.012, mark: 0.012},
			&simLeg{strangle: id, slot: slot, typ: "put", strike: roundStrike(spot-k, spot), expiry: exp, qty: qty, entry: 0.014, mark: 0.014},
		)
	}
	if scenario == scenarioDrawdown {
		put := s.legs[1] // the 25-day put
		put.pinned, put.mark = true, put.entry*drawdownMarkMultiple
	}
	if scenario == scenarioHedged {
		// The same strikes, sold before a 3 % fall; the hedge was switched on
		// an hour ago and sells the perpetual against the lean now, after the
		// last option trade (demoTrades ends an hour back).
		s.spot = spot * hedgedSpotMove
		s.hedgeSince = time.Now().Add(-time.Hour)
		s.rebalanceHedge(time.Now())
	}
	return s
}

// demoTrades is six months of simulated closes (a few a week, mostly
// take-profits, now and then a stop-loss), so /api/summary's monthly income
// and /api/trades have history from the start. The scenarios change the
// mix: good has no stop-loss, mixed and drawdown turn every close of the
// month two months back into a stop-loss (a losing month), and drawdown
// earns little this month.
func demoTrades(spot float64, scenario string, now time.Time) orders.Replay {
	rp := orders.Replay{FirstAt: now.AddDate(0, -6, 0)}
	reasons := []string{"roi_target", "roi_target", "delta_drift", "roi_target", "stop_loss", "dte_rollout"}
	if scenario == scenarioGood {
		reasons = []string{"roi_target", "roi_target", "delta_drift", "roi_target", "dte_rollout"}
	}
	u := now.UTC()
	losingMonth := time.Date(u.Year(), u.Month()-2, 1, 0, 0, 0, 0, time.UTC).Format("2006-01")
	thisMonth := u.Format("2006-01")
	for i, at := 0, rp.FirstAt; at.Before(now.Add(-time.Hour)); i, at = i+1, at.Add(40*time.Hour) {
		reason := reasons[i%len(reasons)]
		month := at.UTC().Format("2006-01")
		if (scenario == scenarioMixed || scenario == scenarioDrawdown) && month == losingMonth {
			reason = "stop_loss"
		}
		pnl := 0.002 + rand.Float64()*0.002
		if scenario == scenarioDrawdown && month == thisMonth {
			pnl = 0.0003 + rand.Float64()*0.0002 // a thin month so far
		}
		detail := ""
		if reason == "stop_loss" {
			pnl, detail = -0.006-rand.Float64()*0.004, "stop-loss: loss 2.05× the premium ≥ 2.00× (demo)"
		}
		typ := []string{"call", "put"}[i%2]
		rp.Events++
		rp.Trades = append(rp.Trades, orders.Trade{Seq: rp.Events, At: at, Kind: "close", Instrument: "demo-" + typ,
			OptionType: typ, Qty: 0.3, Reason: reason, Detail: detail, PnL: pnl, PnLUSD: pnl * spot, Spot: spot})
	}
	return rp
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
	s.journalTrendChange()
	for _, l := range s.legs {
		moneyness := (s.spot - l.strike) / s.spot
		if l.typ == "put" {
			moneyness = -moneyness
		}
		l.mark = math.Max(0.0005, l.entry*math.Exp(moneyness*12)*(1+rand.NormFloat64()*0.01))
		if l.pinned {
			l.mark = l.entry * drawdownMarkMultiple * (1 + rand.NormFloat64()*0.005)
		}
	}
	if s.history != nil {
		unrealised := 0.0
		for _, l := range s.legs {
			unrealised += l.entry*l.qty - l.mark*l.qty
		}
		s.history.RecordPnL(time.Now(), s.realised, unrealised, s.spot, demoMargin(s.underlying))
	}
	if s.scenario == scenarioHedged {
		s.rebalanceHedge(time.Now())
	}
	s.steps++
	if s.steps == 1 {
		// The first tick always sends an order, so the Activity feed is
		// never empty for long; afterwards events come at random (a minute
		// without one failed a loaded test run).
		s.submitRandomLeg()
		return
	}
	switch r := rand.Float64(); {
	case r < 0.04:
		s.rollRandomLeg("rollout_roi")
	case r < 0.06:
		s.journal.LogSkipped("no_expiry", s.ctx(nil, &orders.SlotRef{DTE: 25, Delta: 0.16}))
	case r < 0.10:
		s.submitRandomLeg()
	}
}

// submitRandomLeg journals an entry order sent for a random leg. Call with
// s.mu held.
func (s *simulation) submitRandomLeg() {
	l := s.legs[rand.Intn(len(s.legs))]
	s.journal.LogSubmit(orders.PendingOrderRecord{OrderID: fmt.Sprintf("o-%d", rand.Intn(1e6)), Instrument: s.name(l),
		OptionType: l.typ, Direction: orders.DirectionSell, OrderType: orders.TypeLimit, TriggerReason: orders.TriggerEntry,
		Qty: l.qty, LimitPrice: l.mark}, s.ctx(l, &l.slot))
}

func (s *simulation) rollRandomLeg(reason string) {
	l := s.legs[rand.Intn(len(s.legs))]
	pos := s.position(l)
	fill := orders.Fill{OrderID: fmt.Sprintf("o-%d", rand.Intn(1e6)), FillPrice: l.mark, Qty: l.qty, Timestamp: time.Now()}
	s.journal.LogClose(pos, fill, reason, orders.TypeLimit, s.ctx(l, &l.slot))
	s.realised += pos.PremiumReceived - l.mark*l.qty
	s.closed++
	l.entry, l.mark = 0.012, 0.012 // re-sold at a fresh premium
	s.journal.LogOpen(s.position(l), orders.Fill{FillPrice: l.entry, Qty: l.qty, Timestamp: time.Now()}, orders.TriggerEntry, s.ctx(l, &l.slot))
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
	now := time.Now()
	rv := s.riskView(now) // reads the account, which takes s.mu itself
	s.mu.Lock()
	defer s.mu.Unlock()
	sma := s.smaLevels()
	v := strategy.View{
		AsOf: now, StrategyID: strategy.DefaultStrategyID, Underlying: s.underlying, Environment: "testnet (demo)",
		LoopAt: now, Trend: sma.Trend, Direction: strategy.DirectionSMA, SMATrend: sma.Trend, SMA: strategy.SMAView{SMALevels: sma, Candles: s.demoCandles()},
		Market:        orders.MarketSnapshot{AsOf: now, Spot: s.spot, DVOL: s.dvol, IVPercentile: 55, GEXRegime: "POSITIVE/PINNING", GammaFlip: s.spot * 0.96},
		FlipBufferPct: strategy.FlipBufferPct(1, s.dvol),
		Account:       strategy.AccountView{Equity: 2.5, MarginUsed: 0.42, AsOf: now},
		Risk:          rv,
		Pending:       s.pending(now),
	}
	var unrealised, delta float64
	v.Strangles, unrealised, delta = s.strangleViews(now)
	v.Greeks = orders.MarketContext{NetDelta: delta, NetGamma: -0.00012, NetVega: -72, NetTheta: 27}
	v.PnL = []strategy.PnLView{{Realised: s.realised, Unrealised: unrealised, Total: s.realised + unrealised, OpenLegs: len(s.legs), ClosedLegs: s.closed}}
	return v
}

// strangleViews is the book as the view shows it, with its open P&L and net
// delta (coin). Call with s.mu held.
func (s *simulation) strangleViews(now time.Time) (out []strategy.StrangleView, unrealised, delta float64) {
	bySlot := map[string]*strategy.StrangleView{}
	for _, l := range s.legs {
		st, ok := bySlot[l.strangle]
		if !ok {
			st = &strategy.StrangleView{ID: l.strangle, Slot: l.slot, OpenedAt: s.started}
			bySlot[l.strangle] = st
		}
		pos := s.position(l)
		mon, dist := strategy.Moneyness(l.typ, s.spot, l.strike)
		st.Legs = append(st.Legs, strategy.LegView{
			PositionID: pos.ID, Instrument: pos.Instrument, OptionType: l.typ, Side: orders.DirectionSell, Strike: l.strike, Expiry: l.expiry,
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
		out = append(out, *bySlot[id])
	}
	return out, unrealised, delta
}

// perpInstrument is the hedged scenario's perpetual.
func (s *simulation) perpInstrument() string { return s.underlying + "-PERPETUAL" }

// rebalanceHedge is the hedged scenario's futures hedge (#827). The real
// hedge holds the book's coin delta within a band; the demo does the same on
// the worst case's own model: when its two one-day moves (perpetual
// included) lose amounts more than hedgeLeanPct apart, it trades the
// perpetual that makes them equal at spot — selling when the book gains as
// the coin rises — and journals a hedge_fill, so the Last action can be the
// hedge's. Call with s.mu held.
func (s *simulation) rebalanceHedge(now time.Time) {
	strangles, _, optionsDelta := s.strangleViews(now)
	wc := api.WorstCaseWithPerp(api.WorstCaseOf(strangles, s.spot, s.dvol, demoEquity, now), s.perp.SizeUSD, s.perp.AvgPrice, demoEquity)
	down, up := wc.Down.LossCoin, wc.Up.LossCoin
	if !wc.Available || math.Abs(down-up) <= hedgeLeanPct/100.0*math.Max(math.Abs(down), math.Abs(up)) {
		return
	}
	// A trade of x USD at spot gains x × (1/spot − 1/moved) at each move.
	perUSD := hedge.PerpPnL(1, s.spot, wc.Down.Spot) - hedge.PerpPnL(1, s.spot, wc.Up.Spot)
	usd := math.Round((down-up)/perUSD/perpContractUSD) * perpContractUSD
	if usd == 0 {
		return
	}
	side := hedge.SideBuy
	if usd < 0 {
		side = hedge.SideSell
	}
	fee := hedge.TakerFee(usd, s.spot, perpTakerFee)
	perpDelta := hedge.PerpDelta(s.perp.SizeUSD, s.spot)
	s.perp.Book(usd, s.spot, fee)
	s.hedgeTrades = append(s.hedgeTrades, now)
	s.journal.LogHedge(orders.HedgeRecord{
		Timestamp: now, StrategyID: strategy.DefaultStrategyID, Change: orders.HedgeFill,
		Detail: fmt.Sprintf("%s %.0f USD of %s: the worst case's moves %.5f / %.5f %s apart by more than %d %% (demo)",
			side, math.Abs(usd), s.perpInstrument(), down, up, s.underlying, hedgeLeanPct),
		Instrument: s.perpInstrument(), Side: side, AmountUSD: math.Abs(usd), FilledUSD: math.Abs(usd),
		Price: s.spot, LedgerPrice: s.spot, FeeCoin: fee, OrderID: fmt.Sprintf("o-%d", rand.Intn(1e6)),
		OptionsDelta: optionsDelta, PerpDelta: perpDelta, BandCoin: hedge.Band(demoEquity, false, s.spot, hedgeBandPct),
		Market: orders.MarketSnapshot{AsOf: now, Spot: s.spot, DVOL: s.dvol, IVPercentile: 55, GEXRegime: "POSITIVE/PINNING"},
	})
}

// HedgeState implements api.HedgeController: the hedged scenario's
// perpetual, switched on, on a test account's prices.
func (s *simulation) HedgeState() strategy.HedgeView {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _, optionsDelta := s.strangleViews(now)
	perpDelta := hedge.PerpDelta(s.perp.SizeUSD, s.spot)
	lastHour := 0
	for _, at := range s.hedgeTrades {
		if now.Sub(at) < time.Hour {
			lastHour++
		}
	}
	return strategy.HedgeView{
		Available: true, Enabled: true, Since: s.hedgeSince, By: "demo", Underlying: s.underlying,
		Instrument: s.perpInstrument(), ContractUSD: perpContractUSD,
		PerpUSD: s.perp.SizeUSD, PerpDeltaCoin: perpDelta, PerpAvg: s.perp.AvgPrice, PerpMark: s.spot, LedgerAvg: s.perp.AvgPrice,
		UnrealisedCoin: hedge.PerpPnL(s.perp.SizeUSD, s.perp.AvgPrice, s.spot), PerpAsOf: now,
		OptionsDelta: optionsDelta, NetDelta: optionsDelta + perpDelta,
		BandCoin: hedge.Band(demoEquity, false, s.spot, hedgeBandPct), BandPct: hedgeBandPct,
		OrdersLastHour: lastHour, MaxPerHour: hedgeMaxPerHour, Testnet: true,
		RealisedCoin: s.perp.Realised, FeesCoin: s.perp.Fees,
		BookScenarios: []hedge.BookScenario{},
	}
}

// HedgePreview implements api.HedgeController: the demo reads no exchange.
func (s *simulation) HedgePreview(context.Context) (strategy.HedgePreview, error) {
	return strategy.HedgePreview{}, errors.New("demo: no exchange to read")
}

// SetFuturesHedge implements api.HedgeController; the demo's switch is not
// served (no token), so this is never called.
func (s *simulation) SetFuturesHedge(context.Context, bool, orders.Actor) (strategy.HedgeSwitchResult, error) {
	return strategy.HedgeSwitchResult{}, errors.New("demo: the futures hedge cannot be switched")
}

// pending simulates one working repair order on the 45-day put, re-sent
// every five minutes, so the Working orders panel always has a row (the
// phone-width check, #309, measures that table). Call with s.mu held.
func (s *simulation) pending(now time.Time) []strategy.PendingView {
	l := s.legs[3] // st-2's put
	sent := now.Truncate(5 * time.Minute)
	return []strategy.PendingView{{
		ID: "demo-repair", Slot: l.slot, Repair: true, SubmittedAt: sent, TimeoutAt: sent.Add(5 * time.Minute),
		Legs: []strategy.PendingLegView{{OrderID: "o-demo-repair", Instrument: s.name(l), OptionType: l.typ, Qty: l.qty, LimitPrice: l.mark}},
	}}
}

// riskView simulates the margin policy mid-change: DVOL confirmed in the
// middle band (35 %), one close in the high band so far, so new entries are
// frozen and the change is 1 of 2 daily closes from confirmation.
func (s *simulation) riskView(now time.Time) strategy.RiskView {
	today := now.UTC().Truncate(24 * time.Hour)
	var closes []risk.Day
	for i, pct := range []float64{50, 52, 55, 78} {
		closes = append(closes, risk.Day{Date: today.AddDate(0, 0, i-4), IVPct: pct, IVKnown: true, RegimeKnown: true})
	}
	st := risk.Evaluate((&config.Config{}).RiskPolicy(true), closes,
		risk.Day{Date: today, IVPct: 81, IVKnown: true, RegimeKnown: true})
	acct := s.Status().Snapshot.Totals
	return strategy.RiskView{Status: st, IMPct: acct.IMPct, MMPct: acct.MMPct, Unit: "USD", MarginBalance: acct.MarginBalanceUSD, AsOf: now}
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
