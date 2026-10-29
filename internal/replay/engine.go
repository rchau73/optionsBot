package replay

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/gex"
	"optionsbot/internal/history"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
	"optionsbot/internal/risk"
	"optionsbot/internal/strategy"
)

// Settings of one replay.
type Settings struct {
	Cfg      *config.Config // the bot's settings (config.Load, then the rule under test)
	From, To time.Time      // trading window; snapshots before From only warm up GEX, DVOL and the regime
	Equity   float64        // margin balance at From, in the coin
	// Closes and DVOL are daily values from before the recording
	// (market.json of scripts/stress_inputs.sh): the SMA trend needs 21+
	// closes and the IV percentile 20+ days, more than the recording holds.
	Closes []orders.DailyClose
	DVOL   []orders.DailyClose // Close = that day's DVOL
	// FlipWindow is the bias's median-flip window; 0 = the bot's. Live it
	// spans ~60 per-minute snapshots; on hourly snapshots it is raised to two
	// snapshot intervals so the median still holds against a moving flip.
	FlipWindow time.Duration
	// GEX are the minute GEX records (recorder.ReadGEXDir, #122); nil = the
	// GEX side is rebuilt from each hourly book. Where they exist the replay
	// decides at each record, as the bot does once a minute: regime, flip,
	// bias, shed confirmation and repair anchors at that pace, spot from the
	// record; option prices, greeks and fills stay those of the last hourly
	// book (held, not interpolated), so an exit fires on the last book's mark
	// and fills at its ask. An hour without records falls back to its book.
	GEX   []recorder.GEXRecord
	Book  []StartLeg                      // the book at From (from the journal); empty = flat
	Trace func(at time.Time, line string) // every action, as it happens (nil = none)
}

// StartLeg is one short leg of the starting book.
type StartLeg struct {
	Slot       config.StrangleSlot
	Instrument string
	Qty        float64
	Price      float64 // premium received per option
}

// Day is one UTC day of the replay, at its last snapshot.
type Day struct {
	Date                time.Time
	Spot, DVOL          float64
	Regime              string // live regime at the day's last snapshot
	ConfirmedNegative   bool
	Direction           int     // the trend the rules used (+1 bull/up, −1 bear/down)
	LimitPct, IMPct     float64 // IM limit and use at the day's end (% of margin balance)
	Equity, PnL, Fees   float64 // margin balance at the end; the day's change; fees paid that day
	Trades, Sheds, Reps int     // orders filled, GEX sheds, repairs
	Actions             []string
}

// Result sums up a replay.
type Result struct {
	Start, End        time.Time
	Snapshots         int     // decisions (one per hourly book or minute GEX record)
	GEXMinutes        int     // of them, decisions on a minute GEX record
	Equity0, Equity   float64 // margin balance at the start and end (coin)
	Net               float64 // Equity − Equity0: realised + unrealised, net of fees
	Realised, Fees    float64
	OpenAtEnd         int // legs still open (their P&L is in Net, at the last mid)
	Trades            int // orders filled (each leg is one)
	Entries, Repairs  int
	Sheds, Stops      int
	DeltaExits, Rolls int // rolls: DTE rolls, take-profits and delta drifts (legs)
	MMCuts, Unfilled  int // Unfilled: orders that found an empty side of the book and waited
	LiquidityCapped   int // entry legs cut (or blocked) by the liquidity cap
	// The liquidity-cap study (#487, liqcap.go).
	LiquidityEntriesCut int     // entries the cap sized down or blocked
	LiquidityLotsCut    float64 // lots the cap took off those entries, summed over their legs
	AvgUseOfLimitPct    float64 // IM as % of the limit, averaged over the decisions
	EntryCycles         int     // decisions with entries allowed (not frozen, no MM breach, no deferred stop, fresh GEX)
	UnderTargetCycles   int     // of them, IM under strategy.UnderUseTargetPct % of the limit
	MaxDrawdown         float64
	MaxIMPct, AvgIMPct  float64
	Days                []Day
}

type strangle struct {
	id        int
	slot      config.StrangleSlot
	call, put *orders.Position
}

func (s *strangle) leg(typ string) *orders.Position {
	if typ == "call" {
		return s.call
	}
	return s.put
}

func (s *strangle) set(typ string, p *orders.Position) {
	if typ == "call" {
		s.call = p
	} else {
		s.put = p
	}
}

func (s *strangle) legs() []*orders.Position {
	var out []*orders.Position
	for _, p := range []*orders.Position{s.call, s.put} {
		if p != nil {
			out = append(out, p)
		}
	}
	return out
}

// snapSource serves the GEX snapshot of the current decision to the gamma monitor.
type snapSource struct{ snap *gex.Snapshot }

func (s *snapSource) Snapshot() *gex.Snapshot { return s.snap }

type engine struct {
	set     Settings
	cfg     *config.Config
	lot     float64
	gamma   *strategy.GammaMonitor
	src     *snapSource
	dvol    *marketdata.DVOLTracker
	regimes *history.RegimeStore
	policy  risk.Config

	book       []*strangle
	nextID     int
	stopped    map[string]time.Time // strangle/side → when a stop, delta exit or MM cut closed it
	anchors    map[string]*strategy.ShedAnchor
	deferSince map[string]time.Time // position → when its stop was first deferred

	now  time.Time // the snapshot being replayed
	cash float64   // margin balance + what buying every short back at its mid costs
	peak float64
	imN  int
	res  Result
	day  *Day
}

func key(s *strangle, typ string) string { return fmt.Sprintf("%d/%s", s.id, typ) }

// Run replays snaps (oldest first) under set and returns the result.
func Run(snaps []recorder.Snapshot, set Settings) (Result, error) {
	cfg := set.Cfg
	if cfg == nil || len(cfg.Slots()) == 0 {
		return Result{}, fmt.Errorf("replay: no slots configured")
	}
	if set.Equity <= 0 {
		return Result{}, fmt.Errorf("replay: starting equity must be positive")
	}
	e := &engine{set: set, cfg: cfg, lot: 0.1, src: &snapSource{},
		dvol:    marketdata.NewDVOLTracker(cfg.IVPercentileWindow),
		policy:  cfg.RiskPolicy(true),
		stopped: map[string]time.Time{}, anchors: map[string]*strategy.ShedAnchor{}, deferSince: map[string]time.Time{},
		cash: set.Equity}
	if !strings.EqualFold(cfg.Underlying, "BTC") {
		e.lot = 1 // Deribit's minimum: 0.1 BTC, 1 ETH
	}
	e.regimes, _ = history.OpenRegimes("") // in memory; cannot fail
	e.gamma = strategy.NewGammaMonitor(cfg.GammaTrendLookbackDays, cfg.SwingPivotN)
	e.gamma.SetGEXSource(e.src)
	e.gamma.SetFlipBuffer(cfg.GammaFlipBufferSD, e.dvol.Current)
	e.gamma.SetShedConfirm(cfg.GEXShedConfirmSnapshots)
	e.gamma.SetSMA9Band(cfg.SMA9BandSD)
	e.gamma.SetDirection(cfg.GEXDirection)
	e.gamma.SetBiasFlipWindow(e.flipWindow(snaps, set.GEX))
	var seed []orders.DailyClose
	for _, c := range set.Closes {
		if len(snaps) == 0 || c.Date.Before(snaps[0].At.UTC().Truncate(24*time.Hour)) {
			seed = append(seed, c)
		}
	}
	e.gamma.SeedDailyCloses(seed)
	for _, d := range set.DVOL {
		if len(snaps) == 0 || d.Date.Before(snaps[0].At) {
			e.dvol.Record(d.Date, d.Close)
		}
	}

	started := false
	for _, t := range ticks(snaps, set.GEX) {
		if !set.To.IsZero() && t.at.After(set.To) {
			break
		}
		if !started && !t.at.Before(set.From) {
			started = true
			e.res.Start = t.at
			e.openBook(snaps[t.book])
		}
		e.step(t.at, snaps[t.book], t.gex, started)
	}
	if !started {
		return Result{}, fmt.Errorf("replay: no snapshot in the window from %s", set.From.Format(time.DateOnly))
	}
	e.closeDay()
	e.res.Equity0 = set.Equity
	e.res.Equity = e.res.Days[len(e.res.Days)-1].Equity
	e.res.Net = e.res.Equity - e.res.Equity0
	if e.imN > 0 {
		e.res.AvgIMPct /= float64(e.imN)
		e.res.AvgUseOfLimitPct /= float64(e.imN)
	}
	for _, s := range e.book {
		e.res.OpenAtEnd += len(s.legs())
	}
	return e.res, nil
}

// flipWindow is the bias's median window for these snapshots (see Settings):
// on minute records the bot's own window holds; on hourly books alone it is
// raised to two book intervals.
func (e *engine) flipWindow(snaps []recorder.Snapshot, recs []recorder.GEXRecord) time.Duration {
	w := e.set.FlipWindow
	if w == 0 {
		w = time.Duration(max(e.cfg.GEXBiasFlipWindowMinutes, 0)) * time.Minute
	}
	if w <= 0 || len(recs) > 0 || len(snaps) < 2 {
		return w
	}
	step := snaps[1].At.Sub(snaps[0].At)
	return max(w, 2*step)
}

// openBook books the starting legs at From, grouped by slot and expiry.
// Equity is the margin balance, which already counts the book at its mid, so
// the cash behind it is Equity plus that value.
func (e *engine) openBook(snap recorder.Snapshot) {
	quotes := snap.ByName()
	for _, l := range e.set.Book {
		_, expiry, strike, typ, err := marketdata.ParseOptionName(l.Instrument)
		if err != nil {
			continue
		}
		expiry = expiry.Add(marketdata.SettlementHourUTC)
		var st *strangle
		for _, s := range e.book {
			if s.slot == l.Slot && s.leg(typ) == nil && len(s.legs()) > 0 && s.legs()[0].Expiry.Equal(expiry) {
				st = s
			}
		}
		if st == nil {
			e.nextID++
			st = &strangle{id: e.nextID, slot: l.Slot}
			e.book = append(e.book, st)
		}
		p := &orders.Position{ID: fmt.Sprintf("%s#start", l.Instrument), Instrument: l.Instrument, Strike: strike,
			Expiry: expiry, OptionType: typ, Side: orders.DirectionSell, Qty: l.Qty, EntryPrice: l.Price,
			EntryTime: snap.At, PremiumReceived: l.Price * l.Qty, CurrentMid: l.Price}
		if q, ok := quotes[l.Instrument]; ok {
			p.CurrentMid, p.CurrentGreeks.Delta, p.CurrentGreeks.IV, p.MarkLive = q.LiveMid(), q.Delta, q.IV, true
		}
		st.set(typ, p)
		e.cash += p.CurrentMid * p.Qty
	}
}

// step runs one decision at `at` through the bot's cycle (strategy.evaluate):
// marks → GEX → shed → exit rules → margin policy → repairs → entries. The
// prices are snap's; the GEX side, spot and DVOL are rec's when there is one
// (a minute record), else rebuilt from snap.
func (e *engine) step(at time.Time, snap recorder.Snapshot, rec *recorder.GEXRecord, trading bool) {
	spot, dvol := snap.Spot(), snap.DVOL
	var gexErr error
	if rec != nil {
		if rec.Spot > 0 {
			spot = rec.Spot
		}
		if rec.DVOL > 0 {
			dvol = rec.DVOL
		}
		e.src.snap = rec.Snapshot()
	} else {
		var g gex.Snapshot
		g, _, gexErr = gex.Build(snap.SummaryRows(), snap.At, gex.Params{Underlying: e.cfg.Underlying,
			StrikeRangePct: e.cfg.GEXStrikeRangePct, Method: e.cfg.GEXMethod})
		if gexErr == nil {
			e.src.snap = &g
		}
	}
	if spot <= 0 {
		return
	}
	e.now = at
	if dvol > 0 {
		e.dvol.Record(at, dvol)
	}
	quotes := snap.ByName()
	e.gamma.PushPriceAt(at, spot)
	dec := e.gamma.Evaluate()
	st := e.riskStatus(at, dec)
	if !trading {
		return
	}
	if e.day != nil && !at.UTC().Truncate(24*time.Hour).Equal(e.day.Date) {
		e.closeDay()
	}
	if e.day == nil {
		e.day = &Day{Date: at.UTC().Truncate(24 * time.Hour)}
	}
	e.res.Snapshots++
	if rec != nil {
		e.res.GEXMinutes++
	}
	e.res.End = at
	e.refreshMarks(at, quotes, spot)

	if dec.Shed != strategy.GammaActionNone {
		e.shed(at, dec, quotes)
	}
	stopsDeferred := e.exits(at, quotes)
	u := e.usage(spot)
	mmBreach := u.MMPct() >= st.MaxMMPct
	if mmBreach {
		e.cutForMM(at, u, st, quotes)
	}
	// No new risk without a fresh GEX snapshot (gexready.go): the side to
	// avoid is unknown.
	entriesAllowed := !mmBreach && !stopsDeferred && gexErr == nil && !st.Frozen
	if !mmBreach && !stopsDeferred && gexErr == nil {
		e.repairs(at, snap, dec, st, quotes)
		if entriesAllowed {
			e.entries(at, snap, dec, st, quotes, spot)
		}
	}

	u = e.usage(spot)
	e.res.MaxIMPct = math.Max(e.res.MaxIMPct, u.IMPct())
	e.res.AvgIMPct += u.IMPct()
	use := 0.0
	if st.LimitIMPct > 0 {
		use = u.IMPct() / st.LimitIMPct * 100
	}
	e.res.AvgUseOfLimitPct += use
	if entriesAllowed {
		e.res.EntryCycles++
		if use < strategy.UnderUseTargetPct {
			e.res.UnderTargetCycles++
		}
	}
	e.imN++
	eq := u.MarginBalance
	e.peak = math.Max(e.peak, eq)
	e.res.MaxDrawdown = math.Max(e.res.MaxDrawdown, e.peak-eq)
	e.day.Spot, e.day.DVOL, e.day.Regime = spot, e.dvol.Current(), dec.Regime
	e.day.ConfirmedNegative, e.day.Direction = st.RegimeNegative, dec.TrendDir
	e.day.LimitPct, e.day.IMPct, e.day.Equity = st.LimitIMPct, u.IMPct(), eq
}

func (e *engine) closeDay() {
	if e.day == nil {
		return
	}
	prev := e.set.Equity
	if n := len(e.res.Days); n > 0 {
		prev = e.res.Days[n-1].Equity
	}
	e.day.PnL = e.day.Equity - prev
	e.res.Days = append(e.res.Days, *e.day)
	e.day = nil
}

func (e *engine) act(at time.Time, format string, a ...any) {
	line := fmt.Sprintf(format, a...)
	e.day.Actions = append(e.day.Actions, line)
	if e.set.Trace != nil {
		e.set.Trace(at, line)
	}
}

// riskStatus is the margin policy at at, as limits.go builds it: DVOL and
// regime daily closes (today excluded) and the live observation.
func (e *engine) riskStatus(at time.Time, dec strategy.GammaDecision) risk.Status {
	known := dec.Regime != "" && dec.Regime != "UNKNOWN"
	if known {
		e.regimes.Record(at, dec.Regime)
	}
	today := at.UTC().Truncate(24 * time.Hour)
	byDay := map[time.Time]*risk.Day{}
	get := func(t time.Time) *risk.Day {
		if byDay[t] == nil {
			byDay[t] = &risk.Day{Date: t}
		}
		return byDay[t]
	}
	ivCloses, ivToday := e.dvol.Daily()
	for _, c := range ivCloses {
		if c.Day.Before(today) {
			d := get(c.Day)
			d.IVPct, d.IVKnown = c.Percentile, c.Known
		}
	}
	for _, r := range e.regimes.Daily() {
		if r.Day.Before(today) {
			d := get(r.Day)
			d.Negative, d.RegimeKnown = strings.HasPrefix(r.Regime, "NEGATIVE"), true
		}
	}
	closes := make([]risk.Day, 0, len(byDay))
	for _, d := range byDay {
		closes = append(closes, *d)
	}
	sort.Slice(closes, func(i, j int) bool { return closes[i].Date.Before(closes[j].Date) })
	return risk.Evaluate(e.policy, closes, risk.Day{Date: today, IVPct: ivToday.Percentile, IVKnown: ivToday.Known,
		Negative: strings.HasPrefix(dec.Regime, "NEGATIVE"), RegimeKnown: known})
}

// refreshMarks copies each held leg's recorded quote onto it (refreshMarks):
// a leg without one keeps its last mark, not live. A leg past its expiry
// settles at its intrinsic value.
func (e *engine) refreshMarks(at time.Time, quotes map[string]recorder.Quote, spot float64) {
	for _, s := range e.book {
		for _, p := range s.legs() {
			if !at.Before(p.Expiry) {
				v := inversePrice(p.OptionType, spot, p.Strike, 0, 0)
				e.close(at, s, p, p.Qty, v, 0, "expired")
				continue
			}
			q, ok := quotes[p.Instrument]
			if !ok {
				p.MarkLive = false
				continue
			}
			p.CurrentMid, p.CurrentGreeks.Delta, p.CurrentGreeks.IV, p.MarkLive = q.LiveMid(), q.Delta, q.IV, true
		}
	}
	e.prune()
}

// shed buys back every leg of the side GEX sheds, anchoring it (shed.go).
func (e *engine) shed(at time.Time, dec strategy.GammaDecision, quotes map[string]recorder.Quote) {
	typ := "put"
	if dec.Shed == strategy.GammaActionCloseCalls {
		typ = "call"
	}
	for _, s := range e.book {
		p := s.leg(typ)
		if p == nil {
			continue
		}
		if e.buyBack(at, s, p, p.Qty, quotes, "GEX shed") {
			e.anchors[key(s, typ)] = &strategy.ShedAnchor{At: at, Flip: dec.GammaFlip, BufferPct: dec.FlipBufferPct}
			e.res.Sheds++
			e.day.Sheds++
		}
	}
	e.prune()
}

// exits runs EvaluateLeg on every leg; it reports whether a stop waits for
// the spread (no new risk then).
func (e *engine) exits(at time.Time, quotes map[string]recorder.Quote) (stopsDeferred bool) {
	c := e.cfg
	for _, s := range e.book {
		for _, p := range s.legs() {
			if p.Qty <= 0 {
				continue
			}
			d := strategy.EvaluateLeg(p, at, c.RolloutDTE, c.DeltaDriftThreshold, c.ROITakeProfit, c.StopLossMultiplier, c.DeltaExitThreshold)
			if d.Action != strategy.ActionStopLoss {
				delete(e.deferSince, p.ID)
			}
			switch d.Action {
			case strategy.ActionStopLoss:
				q := quotes[p.Instrument]
				if reason := strategy.StopDeferReason(q.Bid, q.Ask, e.deferSince[p.ID], at, c.StopSpreadGuardPct,
					time.Duration(c.StopSpreadMaxWaitMin)*time.Minute); reason != "" {
					if e.deferSince[p.ID].IsZero() {
						e.deferSince[p.ID] = at
						e.act(at, "stop deferred %s: %s", p.Instrument, reason)
					}
					stopsDeferred = true
					continue
				}
				delete(e.deferSince, p.ID)
				if e.buyBack(at, s, p, p.Qty, quotes, "STOP") {
					e.stopped[key(s, p.OptionType)] = at
					e.res.Stops++
				}
			case strategy.ActionDeltaExit:
				if e.buyBack(at, s, p, p.Qty, quotes, "DELTA EXIT") {
					e.stopped[key(s, p.OptionType)] = at
					e.res.DeltaExits++
				}
			case strategy.ActionRollNextMonth:
				for _, l := range s.legs() {
					if e.buyBack(at, s, l, l.Qty, quotes, "DTE roll") {
						e.res.Rolls++
					}
				}
			case strategy.ActionRollSameLeg:
				what := map[string]string{orders.TriggerRolloutDelta: "drift roll", orders.TriggerRolloutROI: "take-profit"}[d.Reason]
				if e.buyBack(at, s, p, p.Qty, quotes, what) {
					e.res.Rolls++
				}
			}
		}
	}
	e.prune()
	return stopsDeferred
}

// cutForMM buys back each short down to MMKeepQty at once (reduceForMM).
func (e *engine) cutForMM(at time.Time, u risk.Usage, st risk.Status, quotes map[string]recorder.Quote) {
	for _, s := range e.book {
		for _, p := range s.legs() {
			keep := strategy.MMKeepQty(p.Qty, e.lot, u.MMPct(), st.MaxMMPct)
			if p.Qty-keep < 1e-9 {
				continue
			}
			if e.buyBack(at, s, p, p.Qty-keep, quotes, fmt.Sprintf("MM cut (MM %.0f%%)", u.MMPct())) {
				e.res.MMCuts++
				if keep < 1e-9 {
					e.stopped[key(s, p.OptionType)] = at
				}
			}
		}
	}
	e.prune()
}

// repairs re-sells the missing leg of each one-legged strangle when the
// bot's repair gate (holds.go repairGate) lets it.
func (e *engine) repairs(at time.Time, snap recorder.Snapshot, dec strategy.GammaDecision, st risk.Status, quotes map[string]recorder.Quote) {
	c := e.cfg
	bias := c.GEXDirection == strategy.DirectionBias
	if bias { // every anchor counts every snapshot (repairIncompleteStrangles)
		for k, a := range e.anchors {
			side := k[strings.IndexByte(k, '/')+1:]
			points := (side == "put" && dec.BiasDir < 0) || (side == "call" && dec.BiasDir > 0)
			a.ObserveBias(!points, dec.SnapshotAt)
		}
	}
	var insts []*marketdata.Instrument
	for _, s := range e.book {
		if s.call != nil && s.put != nil {
			continue
		}
		present, missing := s.call, "put"
		if present == nil {
			present, missing = s.put, "call"
		}
		if present == nil || dec.Sheds(missing) || present.DTEAt(at) <= c.RolloutDTE ||
			strategy.RegimeSideBlockReason(missing, dec.TrendDir, st) != "" {
			continue
		}
		k := key(s, missing)
		if a, ok := e.anchors[k]; ok {
			if bias {
				if strategy.BiasRepairBlockReason(a, missing, at, time.Duration(c.GEXBiasRepairCooldownHours)*time.Hour, c.GEXRepairConfirmSnapshots) != "" {
					continue
				}
			} else {
				a.Observe(dec.Spot, dec.SnapshotAt)
				if strategy.ShedRepairBlockReason(a, at, dec.NonNegativeSince, c.GEXRepairConfirmSnapshots,
					time.Duration(c.GEXRepairReleaseHours)*time.Hour) != "" {
					continue
				}
			}
		}
		if stoppedAt, ok := e.stopped[k]; ok {
			if strategy.RepairBlockReason(stoppedAt, at, time.Duration(c.RepairCooldownHours)*time.Hour, st) != "" {
				continue
			}
		}
		if insts == nil {
			insts = snap.Instruments(e.lot)
		}
		in, err := strategy.SelectStrike(insts, present.Expiry, missing, s.slot.EntryDelta, c.DeltaSlippage)
		if err != nil {
			continue
		}
		q := quotes[in.Name]
		f := SellFill(q, c.RepairPriceFloor)
		if !f.OK {
			e.res.Unfilled++
			continue
		}
		if c.MinPremiumBTC > 0 && f.Price < c.MinPremiumBTC {
			continue
		}
		e.sell(at, s, q, present.Qty, f.Price, "repair")
		delete(e.anchors, k)
		delete(e.stopped, k)
		e.res.Repairs++
		e.day.Reps++
	}
}

// entries opens each vacant slot (maybeOpenStrangles): its expiry
// (SelectSlotExpiry), a strike pair (SelectStrikePair), the sides the GEX
// signal and the regime-side rule allow, sized by the slot share and capped.
func (e *engine) entries(at time.Time, snap recorder.Snapshot, dec strategy.GammaDecision, st risk.Status, quotes map[string]recorder.Quote, spot float64) {
	c := e.cfg
	slots := c.Slots()
	occupied := map[config.StrangleSlot]bool{}
	held := map[time.Time]bool{}
	for _, s := range e.book {
		occupied[s.slot] = true
		held[s.legs()[0].Expiry] = true
	}
	vacant := 0
	for _, sl := range slots {
		if !occupied[sl] {
			vacant++
		}
	}
	if vacant == 0 {
		return
	}
	insts := snap.Instruments(e.lot)
	for _, sl := range slots {
		if occupied[sl] {
			continue
		}
		exp, _ := strategy.SelectSlotExpiry(insts, at, sl.TargetDTE, c.MaxDTEDeviation, c.RolloutDTE, c.ExpiryStretch, held)
		if exp.IsZero() {
			continue
		}
		ci, pi, err := strategy.SelectStrikePair(insts, exp, sl.EntryDelta, c.DeltaSlippage)
		if err != nil {
			continue
		}
		var sides []entrySide
		for _, in := range []*marketdata.Instrument{ci, pi} {
			if dec.Sheds(in.OptionType) || strategy.RegimeSideBlockReason(in.OptionType, dec.TrendDir, st) != "" {
				continue
			}
			q := quotes[in.Name]
			// the premium floor at the lowest price the step-down reaches
			if c.MinPremiumBTC > 0 && q.LiveMid() < c.MinPremiumBTC {
				sides = nil
				break
			}
			sides = append(sides, entrySide{q, SellFill(q, c.EntryPriceFloor)})
		}
		if len(sides) == 0 {
			continue
		}
		lots := e.size(sides, spot, st, vacant, len(slots))
		// The liquidity cap (strategy/liquidity.go): at most a share of each
		// sold strike's recorded open interest.
		marginLots := lots
		for _, sd := range sides {
			if most := strategy.LiquidityCapQty(sd.q.OpenInterest, c.LiquidityCapOIPct, e.lot) / e.lot; most < float64(lots) {
				lots = int(math.Floor(most + 1e-9))
				e.res.LiquidityCapped++
			}
		}
		if lots < marginLots {
			e.res.LiquidityEntriesCut++
			e.res.LiquidityLotsCut += float64((marginLots - max(lots, 0)) * len(sides))
		}
		if lots < 1 {
			continue
		}
		e.nextID++
		s := &strangle{id: e.nextID, slot: sl}
		var names []string
		for _, sd := range sides {
			if !sd.fill.OK {
				e.res.Unfilled++
				continue
			}
			e.sellInto(at, s, sd.q, float64(lots)*e.lot, sd.fill.Price)
			names = append(names, sd.q.Instrument)
		}
		if len(s.legs()) == 0 {
			continue
		}
		e.book = append(e.book, s)
		occupied[sl], held[exp] = true, true
		vacant--
		e.res.Entries++
		e.act(at, "entry %dd ×%d: %s", sl.TargetDTE, lots, strings.Join(names, " + "))
	}
}

// size is the entry's lots: EntryShare of the margin, the IM of one more
// lot, the size cap against the strangle's own margin (CapLots), then the
// post-trade IM and MM checked against the limits, shrinking up to 3 times.
func (e *engine) size(sides []entrySide, spot float64, st risk.Status, vacant, nSlots int) int {
	u := e.usage(spot)
	slotShare := strategy.SlotShare(st.LimitIMPct, u.MarginBalance, nSlots)
	share := strategy.EntryShare(u.Headroom(st.LimitIMPct), slotShare, vacant)
	if share <= 0 {
		return 0
	}
	extra := func(lots int) []marginLeg {
		var out []marginLeg
		for _, sd := range sides {
			out = append(out, marginLeg{sd.q.OptionType, sd.q.Strike, float64(lots) * e.lot, years(sd.q.Expiry, e.now), sd.q.IV})
		}
		return out
	}
	book := e.marginLegs()
	perLot := scanIM(append(book, extra(1)...), spot) - u.IM
	lots := strategy.EntryLots(share, perLot)
	normal := strategy.TargetLots(slotShare, scanIM(extra(1), spot))
	lots, _ = strategy.CapLots(lots, normal, e.cfg.MaxLegSizeMultiple)
	for attempt := 0; attempt < 3 && lots >= 1; attempt++ {
		im := scanIM(append(e.marginLegs(), extra(lots)...), spot)
		post := risk.Usage{IM: im, MM: im * MMRatio, MarginBalance: u.MarginBalance}
		if risk.Fits(post, st.LimitIMPct, st.MaxMMPct) {
			return lots
		}
		over := math.Max(post.IMPct()/st.LimitIMPct, post.MMPct()/st.MaxMMPct)
		lots = min(lots-1, int(math.Floor(float64(lots)/over)))
	}
	return 0
}

// entrySide is one leg an entry will sell: its quote and its fill.
type entrySide struct {
	q    recorder.Quote
	fill Fill
}

func years(expiry, now time.Time) float64 { return expiry.Sub(now).Hours() / 24 / 365 }

func (e *engine) marginLegs() []marginLeg {
	var out []marginLeg
	for _, s := range e.book {
		for _, p := range s.legs() {
			out = append(out, marginLeg{p.OptionType, p.Strike, p.Qty, years(p.Expiry, e.now), p.CurrentGreeks.IV})
		}
	}
	return out
}

// usage is the book's margin now (MODEL, see scanIM) against its margin
// balance: cash less what buying every short back at its mid would cost.
func (e *engine) usage(spot float64) risk.Usage {
	im := scanIM(e.marginLegs(), spot)
	value := 0.0
	for _, s := range e.book {
		for _, p := range s.legs() {
			value += p.CurrentMid * p.Qty
		}
	}
	return risk.Usage{IM: im, MM: im * MMRatio, MarginBalance: e.cash - value, Unit: e.cfg.Underlying}
}

// sell books a repair onto s.
func (e *engine) sell(at time.Time, s *strangle, q recorder.Quote, qty, price float64, why string) {
	e.sellInto(at, s, q, qty, price)
	e.act(at, "%s: sell %s ×%.1f at %.4f", why, q.Instrument, qty, price)
}

func (e *engine) sellInto(at time.Time, s *strangle, q recorder.Quote, qty, price float64) {
	fee := Fee(price, qty)
	p := &orders.Position{ID: fmt.Sprintf("%s#%d", q.Instrument, at.Unix()), Instrument: q.Instrument, Strike: q.Strike,
		Expiry: q.Expiry, OptionType: q.OptionType, Side: orders.DirectionSell, Qty: qty, EntryPrice: price,
		UnderlyingPrice: q.UnderlyingPrice, EntryTime: at, PremiumReceived: price * qty, Fees: fee,
		CurrentMid: q.LiveMid(), CurrentGreeks: orders.Greeks{Delta: q.Delta, IV: q.IV}, MarkLive: true}
	s.set(q.OptionType, p)
	e.cash += price*qty - fee
	e.res.Fees += fee
	e.day.Fees += fee
	e.res.Trades++
	e.day.Trades++
}

// buyBack buys qty of p back at the ask; false when the ask side is empty
// (the leg stays, the rule re-fires next hour).
func (e *engine) buyBack(at time.Time, s *strangle, p *orders.Position, qty float64, quotes map[string]recorder.Quote, why string) bool {
	f := BuyFill(quotes[p.Instrument])
	if !f.OK {
		e.res.Unfilled++
		return false
	}
	fee := Fee(f.Price, qty)
	pnl := e.close(at, s, p, qty, f.Price, fee, why)
	e.act(at, "%s: buy %s ×%.1f at %.4f (%+.4f)", why, p.Instrument, qty, f.Price, pnl)
	e.res.Trades++
	e.day.Trades++
	return true
}

// close books qty of p closed at price with fee: realised P&L net of the
// leg's share of its opening fees (orders.ClosedNetPnL).
func (e *engine) close(at time.Time, s *strangle, p *orders.Position, qty, price, fee float64, why string) float64 {
	qty = math.Min(qty, p.Qty)
	share := qty / p.Qty
	part := &orders.Position{Qty: qty, PremiumReceived: p.PremiumReceived * share, Fees: p.Fees * share}
	pnl := orders.ClosedNetPnL(part, orders.Fill{FillPrice: price, Qty: qty, Fee: fee})
	e.cash -= price*qty + fee
	e.res.Realised += pnl
	e.res.Fees += fee
	e.day.Fees += fee
	p.Qty -= qty
	p.PremiumReceived -= part.PremiumReceived
	p.Fees -= part.Fees
	if why == "expired" {
		e.act(at, "expired %s (%+.4f)", p.Instrument, pnl)
	}
	return pnl
}

// prune drops closed legs and strangles with no leg left (the slot is free).
func (e *engine) prune() {
	keep := e.book[:0]
	for _, s := range e.book {
		for _, typ := range []string{"call", "put"} {
			if p := s.leg(typ); p != nil && p.Qty < 1e-9 {
				s.set(typ, nil)
			}
		}
		if len(s.legs()) > 0 {
			keep = append(keep, s)
			continue
		}
		for _, typ := range []string{"call", "put"} { // the slot re-enters: its records go
			delete(e.anchors, key(s, typ))
			delete(e.stopped, key(s, typ))
		}
	}
	e.book = keep
}
