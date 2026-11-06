package strategy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/hedge"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
)

// The futures hedge experiment (#657). Off by default and switched from the
// monitor (data/hedge_mode.json). While it is on:
//   - the GEX shed closes nothing: each leg it would have bought back is
//     journaled as a shed_hedged skip and kept (entries and repairs still
//     skip the shed side: Action is unchanged), unless the hedge cannot trade:
//     then the shed works as with the mode off (hedge.Health, #664);
//   - every cycle the book's net delta (options + the coin's perpetual) is
//     traded back to zero with the perpetual once it reaches the band
//     (hedge.HedgeOrder), at most FixedFuturesHedgeMaxPerHour orders an hour;
//   - the stop-loss, delta exit, MM cut, DTE roll and take-profit still close legs.
// The kill switch closes the bot's own perpetual too, the ledger's size at
// most (#667). Switching off closes the perpetual at market first
// (reduce-only); the shed works again from the next cycle. A benchmark
// (hedge.Ledger) compares the two approaches from the first switch on: see
// internal/hedge/benchmark.go.

// FuturesTrader is what the futures hedge asks of the exchange beyond the
// options (orders go through Exchange.Submit). Optional: nil leaves the
// experiment unavailable.
type FuturesTrader interface {
	GetFuturePositions(ctx context.Context, currency string) ([]orders.RawPosition, error)
	GetInstrumentSpec(ctx context.Context, name string) (orders.InstrumentSpec, error)
	GetTicker(ctx context.Context, name string) (orders.Ticker, error)
}

// HedgeJournal is the journal's futures-hedge record (orders.Logger has it).
// A journal without it logs the records to the bot log only.
type HedgeJournal interface {
	LogHedge(r orders.HedgeRecord)
}

// SkipShedHedged: the GEX shed would have bought this leg back; the futures
// hedge keeps it (journaled once per leg).
const SkipShedHedged = "shed_hedged"

// hedgeSaveEvery is how often the ledger's follow-up prices are saved when
// nothing else changed (a trade or a switch saves at once).
const hedgeSaveEvery = 5 * time.Minute

// futuresHedge is the experiment's loop-owned state.
type futuresHedge struct {
	path string // "" → the mode cannot be switched on (no file to keep it)
	mode hedge.ModeState
	spec orders.InstrumentSpec // the perpetual's contract (read once)
	// sent / paperSent: the real and paper hedge orders of the last hour.
	sent, paperSent []time.Time
	rateNoted       bool            // the current rate-limit wait is journaled
	unpricedNoted   bool            // the current wait for a leg's greeks is logged
	shedNoted       map[string]bool // positions already journaled shed_hedged
	perp            orders.RawPosition
	perpAt          time.Time // when perp was read (zero: never)
	band            float64   // the last cycle's band (coin)
	shedSide        GammaAction
	health          hedge.Health  // can the real hedge trade? (#664)
	recheck         time.Duration // how often a halted hedge looks again
	unavailNoted    bool          // the current hedge_unavailable episode is journaled
	awayNoted       bool          // the current hedge_price_away wait is journaled
	dirty           bool
	savedAt         time.Time
}

// loadHedgeMode reads the mode file at startup. A file that cannot be read
// keeps the experiment OFF: it must never switch on by accident.
func (s *Strategy) loadHedgeMode(path string) {
	s.fh.path = path
	s.fh.shedNoted = map[string]bool{}
	if path == "" {
		return
	}
	st, err := hedge.LoadMode(path)
	if err != nil {
		slog.Error("futures hedge: mode file unreadable — the experiment stays OFF", "path", path, "err", err)
		return
	}
	if st.Enabled && s.cfg.IsLive() {
		slog.Error("futures hedge: the mode file says ON on a live account — the experiment is testnet-only and stays OFF", "path", path)
		st.Enabled = false
	}
	s.fh.mode = st
	if st.Enabled {
		slog.Warn("futures hedge experiment is ON (from the mode file)", "since", st.Since, "by", st.By.User)
	}
}

func (s *Strategy) perpName() string { return s.suggestedHedgeInst() }

// hedgeJournal records r in the journal (or the bot log when the journal
// has no hedge record).
func (s *Strategy) hedgeJournal(r orders.HedgeRecord) {
	ectx := s.eventContext(nil, nil)
	r.StrategyID, r.Market, r.Portfolio = s.strategyID(), ectx.Market, ectx.Portfolio
	if r.Instrument == "" {
		r.Instrument = s.perpName()
	}
	if hj, ok := s.journal.(HedgeJournal); ok {
		hj.LogHedge(r)
		return
	}
	slog.Info("futures hedge", "change", r.Change, "detail", r.Detail, "side", r.Side, "amount_usd", r.AmountUSD, "filled_usd", r.FilledUSD)
}

// readPerp reads the bot's perpetual position from the exchange (zero size
// when there is none). Every hedge decision starts from it.
func (s *Strategy) readPerp(ctx context.Context) (orders.RawPosition, error) {
	rows, err := s.futures.GetFuturePositions(ctx, s.cfg.Underlying)
	if err != nil {
		return orders.RawPosition{}, err
	}
	out := orders.RawPosition{InstrumentName: s.perpName()}
	for _, r := range rows {
		if r.InstrumentName == s.perpName() {
			out = r
			out.Size = signedSize(r)
		}
	}
	s.fh.perp, s.fh.perpAt = out, time.Now()
	return out, nil
}

// signedSize is a futures position's size in USD, negative when short
// (Deribit signs it; the direction makes sure).
func signedSize(r orders.RawPosition) float64 {
	if r.Direction == orders.DirectionSell && r.Size > 0 {
		return -r.Size
	}
	return r.Size
}

// perpSpec is the perpetual's contract, read once from Deribit. A contract
// size it cannot read stops the hedge: amounts are never guessed.
func (s *Strategy) perpSpec(ctx context.Context) (orders.InstrumentSpec, error) {
	if s.fh.spec.ContractSize > 0 {
		return s.fh.spec, nil
	}
	spec, err := s.futures.GetInstrumentSpec(ctx, s.perpName())
	if err != nil {
		return orders.InstrumentSpec{}, fmt.Errorf("perpetual contract: %w", err)
	}
	if spec.ContractSize <= 0 {
		return orders.InstrumentSpec{}, fmt.Errorf("perpetual contract: %s has no contract size", s.perpName())
	}
	s.fh.spec = spec
	slog.Info("futures hedge: perpetual contract", "instrument", s.perpName(),
		"contract_usd", spec.ContractSize, "min_amount", spec.MinTradeAmount, "tick", spec.TickSize)
	return spec, nil
}

// hedgeBand is the |net delta| (coin) the hedge tolerates, from the cycle's
// margin balance; 0 (no hedge) when the margin is unknown.
func hedgeBand(m marginState, underlying string, spot float64) float64 {
	if m.err != nil || m.usage.MarginBalance <= 0 {
		return 0
	}
	return hedge.Band(m.usage.MarginBalance, m.usage.Unit != "" && m.usage.Unit != underlying, spot, config.FixedFuturesHedgeBandPct)
}

// perpMark is the price the perpetual is valued at: its mark, else spot.
func perpMark(p orders.RawPosition, spot float64) float64 {
	if p.MarkPrice > 0 {
		return p.MarkPrice
	}
	return spot
}

// stepFuturesHedge runs the cycle's hedge: the real one while the mode is
// on, the paper one (benchmark) while it is off. Then it follows the shed
// counterfactuals and saves the ledger.
func (s *Strategy) stepFuturesHedge(ctx context.Context, m marginState, now time.Time) {
	if s.futures == nil || s.flattening {
		return
	}
	spot := s.md.UnderlyingPrice()
	if spot <= 0 {
		return
	}
	if s.fh.mode.Enabled {
		s.realHedge(ctx, m, spot, now)
		s.noteHedgeHealth()
	} else if s.fh.path != "" {
		s.paperHedge(ctx, m, spot, now)
	}
	s.followCounterfactuals(now)
	s.saveHedge(now, false)
}

// coinDelta is the options' coin delta for the hedge. While a leg has no
// greeks yet it would count as its mark alone, so there is no reading and no
// hedge, real or paper, that cycle (warned once per episode).
func (s *Strategy) coinDelta() (float64, bool) {
	legs := s.state.UnpricedLegs()
	if len(legs) > 0 {
		if !s.fh.unpricedNoted {
			s.fh.unpricedNoted = true
			slog.Warn("futures hedge: a leg has no greeks yet — no hedge until it does", "legs", legs)
		}
		return 0, false
	}
	s.fh.unpricedNoted = false
	return s.state.TotalCoinDelta(), true
}

// realHedge trades the perpetual so the book's net delta is back inside the band.
func (s *Strategy) realHedge(ctx context.Context, m marginState, spot float64, now time.Time) {
	perp, err := s.readPerp(ctx)
	if err != nil {
		slog.Warn("futures hedge: cannot read the perpetual position — no hedge this cycle", "err", err)
		s.fh.health.NotFilled("the perpetual position cannot be read")
		return
	}
	s.adoptPerpDrift(perp, spot)
	spec, err := s.perpSpec(ctx)
	if err != nil {
		slog.Warn("futures hedge: no contract size — no hedge this cycle", "err", err)
		s.fh.health.NotFilled("the perpetual's contract size cannot be read")
		return
	}
	band := hedgeBand(m, s.cfg.Underlying, spot)
	s.fh.band = band
	if band <= 0 {
		return // margin unknown: nothing to size the band from
	}
	optDelta, ok := s.coinDelta()
	if !ok {
		return
	}
	perpDelta := hedge.PerpDelta(perp.Size, perpMark(perp, spot))
	usd, side := hedge.HedgeOrder(optDelta, perpDelta, band, spot, spec.ContractSize)
	if s.fh.health.Waiting(now) {
		return // halted: nothing until the re-check
	}
	if usd == 0 && !s.fh.health.Halted() {
		s.fh.rateNoted, s.fh.awayNoted = false, false
		s.fh.health.Holding()
		return
	}
	if usd != 0 && hedge.RateLimited(s.fh.sent, now, config.FixedFuturesHedgeMaxPerHour) {
		s.fh.health.NotFilled(fmt.Sprintf("%d hedge orders went out in the last hour: the next one waits", config.FixedFuturesHedgeMaxPerHour))
		if !s.fh.rateNoted {
			s.fh.rateNoted = true
			s.hedgeJournal(orders.HedgeRecord{Change: orders.HedgeRateLimited, Side: side, AmountUSD: usd,
				OptionsDelta: optDelta, PerpDelta: perpDelta, BandCoin: band,
				Detail: fmt.Sprintf("hedge due (net delta %+.4f, band %.4f) but %d orders were sent in the last hour: it waits",
					optDelta+perpDelta, band, config.FixedFuturesHedgeMaxPerHour)})
		}
		return
	}
	s.fh.rateNoted = false
	tk, err := s.futures.GetTicker(ctx, s.perpName())
	if err != nil {
		slog.Warn("futures hedge: no perpetual quote — no hedge this cycle", "err", err)
		s.fh.health.NotFilled("the perpetual's quote cannot be read")
		return
	}
	if hedge.InstrumentClosed(tk.State) {
		s.haltHedge(fmt.Sprintf("Deribit shows %s as %s, not open", s.perpName(), tk.State), now)
		return
	}
	if usd == 0 {
		s.fh.health.Resume() // the re-check found it open, and nothing is due
		return
	}
	price := tk.BestBid
	if side == hedge.SideBuy {
		price = tk.BestAsk
	}
	if price <= 0 {
		slog.Warn("futures hedge: the perpetual has no "+map[string]string{hedge.SideBuy: "ask", hedge.SideSell: "bid"}[side]+" — no hedge this cycle",
			"bid", tk.BestBid, "ask", tk.BestAsk)
		s.fh.health.NotFilled("the perpetual has no price to trade at")
		return
	}
	index := tk.Index
	if index <= 0 {
		index = spot
	}
	if s.hedgePricing().Blocks(price, index, config.FixedFuturesHedgeMaxIndexGapPct) {
		s.hedgePriceAway(side, usd, price, index, optDelta, perpDelta, band)
		return
	}
	s.fh.awayNoted = false
	order := orders.Order{
		Instrument: s.perpName(), Direction: side, OrderType: orders.TypeLimit, TimeInForce: orders.TimeInForceIOC,
		Qty: usd, LimitPrice: price, TickSize: spec.TickSize, TriggerReason: orders.TriggerFuturesHedge,
		Label: s.hedgeLabel(),
	}
	detail := fmt.Sprintf("net delta %+.4f %s (options %+.4f, perpetual %+.4f) ≥ band %.4f: %s %.0f USD of %s at %.2f (IOC)",
		optDelta+perpDelta, s.cfg.Underlying, optDelta, perpDelta, band, side, usd, s.perpName(), price)
	s.fh.sent = append(pruneHour(s.fh.sent, now), now)
	fill, err := s.exch.Submit(ctx, order)
	if err != nil {
		s.hedgeOrderFailed(ctx, order, err, detail)
		s.noteHedgeRejection(err, now)
		return
	}
	s.fh.health.Resume() // the exchange took the order: whatever halted the hedge has cleared
	if fill.Qty > 0 {
		s.fh.health.Holding()
	} else {
		s.fh.health.NotFilled("the last hedge orders found nothing to fill")
	}
	booked := s.bookPerpFill(side, fill, price, index)
	s.saveHedge(now, true)
	s.hedgeJournal(orders.HedgeRecord{Change: orders.HedgeFill, Side: side, AmountUSD: usd, FilledUSD: fill.Qty,
		Price: fill.FillPrice, LedgerPrice: booked, FeeCoin: fill.Fee, FundingCoin: perp.RealizedFunding, OrderID: fill.OrderID,
		OptionsDelta: optDelta, PerpDelta: perpDelta, BandCoin: band, Detail: detail})
	slog.Info("futures hedge", "side", side, "amount_usd", usd, "filled_usd", fill.Qty, "price", fill.FillPrice,
		"options_delta", optDelta, "perp_delta", perpDelta, "band", band)
}

// hedgePricing is what the benchmark books the real perpetual at: the index
// on testnet, the exchange's own prices on live.
func (s *Strategy) hedgePricing() hedge.Pricing {
	if s.cfg.IsLive() {
		return hedge.PricingExchange
	}
	return hedge.PricingIndex
}

// hedgeKeepsShed reports whether the futures hedge replaces the GEX shed this
// cycle: the mode is on and the hedge can trade (#664). A hedge that cannot
// would leave the book with neither.
func (s *Strategy) hedgeKeepsShed() bool {
	return s.fh.mode.Enabled && s.fh.health.NotHolding(config.FixedFuturesHedgeUnfilledCycles) == ""
}

// endKeptLeg ends the benchmark's kept leg the shed now buys back: from here
// it is followed as a real shed, and a later keep starts a new one.
func (s *Strategy) endKeptLeg(pos *orders.Position) {
	if s.fh.mode.Ledger.EndKept(pos.Instrument) {
		s.fh.dirty = true
	}
	delete(s.fh.shedNoted, pos.ID)
}

// noteHedgeRejection sorts a failed hedge order: a reason retrying cannot cure
// halts the hedge; any other rejection gives the shed back until a fill; an
// unknown outcome is one more cycle without a fill.
func (s *Strategy) noteHedgeRejection(err error, now time.Time) {
	switch why := orders.LastingRejection(err); {
	case why != "":
		s.haltHedge(why, now)
	case orders.MaybePlaced(err):
		s.fh.health.NotFilled("the last hedge order's outcome is unknown")
	default:
		s.fh.health.Rejected(err.Error())
	}
}

// haltHedge stops the hedge's orders until the re-check, journaled once per reason.
func (s *Strategy) haltHedge(reason string, now time.Time) {
	if !s.fh.health.Halt(reason, now, s.fh.recheck) {
		return
	}
	slog.Error("futures hedge halted", "reason", reason, "recheck_at", s.fh.health.RecheckAt)
	s.hedgeJournal(orders.HedgeRecord{Change: orders.HedgeHalted,
		Detail: fmt.Sprintf("%s: no hedge orders; looks again every %s", reason, s.fh.recheck)})
}

// hedgePriceAway holds a due order the price guard stops, journaled once per wait.
func (s *Strategy) hedgePriceAway(side string, usd, price, index, optDelta, perpDelta, band float64) {
	s.fh.health.NotFilled("the perpetual's price is too far from the index")
	if s.fh.awayNoted {
		return
	}
	s.fh.awayNoted = true
	s.hedgeJournal(orders.HedgeRecord{Change: orders.HedgePriceAway, Side: side, AmountUSD: usd, Price: price,
		OptionsDelta: optDelta, PerpDelta: perpDelta, BandCoin: band,
		Detail: fmt.Sprintf("%s %.0f USD due, but %.2f is %.2f %% from the index %.2f (limit %d %%): it waits",
			side, usd, price, (price/index-1)*100, index, config.FixedFuturesHedgeMaxIndexGapPct)})
}

// noteHedgeHealth journals once when the hedge stops holding the delta (the
// shed is back from the next cycle), and once when it holds it again.
func (s *Strategy) noteHedgeHealth() {
	why := s.fh.health.NotHolding(config.FixedFuturesHedgeUnfilledCycles)
	switch {
	case why != "" && !s.fh.unavailNoted:
		s.fh.unavailNoted = true
		slog.Warn("futures hedge unavailable: the GEX shed is back", "reason", why)
		s.hedgeJournal(orders.HedgeRecord{Change: orders.HedgeUnavailable, OptionsDelta: s.state.TotalCoinDelta(),
			Detail: why + ": the GEX shed works as with the mode off until the hedge can trade"})
	case why == "" && s.fh.unavailNoted:
		s.fh.unavailNoted = false
		slog.Info("futures hedge available again")
		s.hedgeJournal(orders.HedgeRecord{Change: orders.HedgeResumed, OptionsDelta: s.state.TotalCoinDelta(),
			Detail: "the hedge can trade again: the GEX shed keeps its legs"})
	}
}

// hedgeLabel is a unique label for a perpetual order.
func (s *Strategy) hedgeLabel() string {
	return s.strategyID() + ":hedge:" + s.state.NextID("o")
}

// hedgeOrderFailed journals a failed hedge order. An order whose outcome is
// unknown is cancelled by label (an IOC cannot rest, but the label makes
// sure); the next cycle reads the position again before anything is sent.
func (s *Strategy) hedgeOrderFailed(ctx context.Context, order orders.Order, err error, detail string) {
	why := "rejected: " + err.Error()
	if orders.MaybePlaced(err) {
		if n, cerr := s.exch.CancelByLabel(ctx, s.cfg.Underlying, order.Label); cerr != nil {
			why = fmt.Sprintf("outcome unknown (%v); cancel by label failed (%v): the next cycle reads the position", err, cerr)
		} else {
			why = fmt.Sprintf("outcome unknown (%v); %d cancelled by label: the next cycle reads the position", err, n)
		}
	}
	slog.Error("futures hedge order failed", "label", order.Label, "err", err)
	s.hedgeJournal(orders.HedgeRecord{Change: orders.HedgeFailed, Side: order.Direction, AmountUSD: order.Qty,
		Detail: detail + " — " + why})
}

// bookPerpFill adds a real perpetual fill to the benchmark's ledger, at the
// price its pricing says (index: the index then), and returns that price.
func (s *Strategy) bookPerpFill(side string, fill orders.Fill, price, index float64) float64 {
	if fill.Qty <= 0 {
		return 0
	}
	px := fill.FillPrice
	if px <= 0 {
		px = price
	}
	px = s.hedgePricing().Book(px, index)
	usd := fill.Qty
	if side == hedge.SideSell {
		usd = -usd
	}
	s.fh.mode.Ledger.Real.Book(usd, px, fill.Fee)
	s.fh.dirty = true
	return px
}

// adoptPerpDrift matches the ledger's real perpetual to the exchange: a
// difference (an unknown order that filled, a hand trade) is booked at the
// mark (index pricing: the index), so the benchmark follows what is really held.
func (s *Strategy) adoptPerpDrift(perp orders.RawPosition, spot float64) {
	diff := perp.Size - s.fh.mode.Ledger.Real.SizeUSD
	if math.Abs(diff) < 0.5 {
		return
	}
	slog.Warn("futures hedge: the perpetual position differs from the ledger — booked at the mark",
		"exchange_usd", perp.Size, "ledger_usd", s.fh.mode.Ledger.Real.SizeUSD)
	s.fh.mode.Ledger.Real.Book(diff, s.hedgePricing().Book(perpMark(perp, spot), spot), 0)
	s.fh.dirty = true
}

// paperHedge runs the same rule on paper while the mode is off (the
// benchmark's hedge side): the paper position trades at the index price.
func (s *Strategy) paperHedge(ctx context.Context, m marginState, spot float64, now time.Time) {
	spec, err := s.perpSpec(ctx)
	if err != nil {
		return
	}
	band := hedgeBand(m, s.cfg.Underlying, spot)
	if band <= 0 {
		return // margin unknown: nothing to size the band from
	}
	optDelta, ok := s.coinDelta()
	if !ok {
		return
	}
	v := &s.fh.mode.Ledger.Virtual
	usd, side := hedge.HedgeOrder(optDelta, hedge.PerpDelta(v.SizeUSD, spot), band, spot, spec.ContractSize)
	if usd == 0 || hedge.RateLimited(s.fh.paperSent, now, config.FixedFuturesHedgeMaxPerHour) {
		return
	}
	if s.fh.mode.Ledger.Since.IsZero() {
		s.fh.mode.Ledger.Since = now
	}
	s.fh.paperSent = append(pruneHour(s.fh.paperSent, now), now)
	if side == hedge.SideSell {
		usd = -usd
	}
	v.Trade(usd, spot, config.FixedFuturesHedgeFeeRate)
	s.fh.dirty = true
}

func pruneHour(ts []time.Time, now time.Time) []time.Time {
	out := ts[:0]
	for _, t := range ts {
		if now.Sub(t) < time.Hour {
			out = append(out, t)
		}
	}
	return out
}

// shedPrice is what a shed buy-back of pos pays per unit: the ask, else its mark.
func (s *Strategy) shedPrice(pos *orders.Position) float64 {
	if inst, ok := s.md.GetInstrument(pos.Instrument); ok && inst.HasQuote() && inst.Ask > 0 {
		return inst.Ask
	}
	return pos.CurrentMid
}

// noteShedHedged journals once per leg that the shed would have bought it
// back, and records the counterfactual (mode ON). shedNoted is in memory and
// a restart may give the leg a new ID, so the persisted ledger decides how
// much of the instrument is still unfollowed: only that part is new.
func (s *Strategy) noteShedHedged(pos *orders.Position, detail string, now time.Time) {
	if s.fh.shedNoted[pos.ID] {
		return
	}
	s.fh.shedNoted[pos.ID] = true
	qty := math.Min(pos.Qty, s.heldQty(pos.Instrument)-s.fh.mode.Ledger.KeptQty(pos.Instrument))
	if qty <= qtyEpsilon {
		return
	}
	price := s.shedPrice(pos)
	s.addCounterfactual(hedge.CounterHedged, pos, qty, price, now)
	jctx := s.instrumentContext(pos.Instrument, s.slotOf(pos.ID))
	jctx.Detail = fmt.Sprintf("%s — kept: the futures hedge holds the delta; the shed would have bought %.4g at %.4f", detail, qty, price)
	s.journal.LogSkipped(SkipShedHedged+" "+pos.Instrument, jctx)
}

// heldQty is the book's total on one instrument (several positions may share it).
func (s *Strategy) heldQty(instrument string) float64 {
	total := 0.0
	for _, p := range s.state.AllPositions() {
		if p.Instrument == instrument {
			total += p.Qty
		}
	}
	return total
}

// noteShedCounterfactual records a real shed (mode OFF) for the benchmark.
func (s *Strategy) noteShedCounterfactual(pos *orders.Position, filled, price float64, now time.Time) {
	if filled <= qtyEpsilon || s.futures == nil || s.fh.path == "" {
		return
	}
	s.addCounterfactual(hedge.CounterShed, pos, filled, price, now)
}

func (s *Strategy) addCounterfactual(mode string, pos *orders.Position, qty, price float64, now time.Time) {
	if s.fh.mode.Ledger.Since.IsZero() {
		s.fh.mode.Ledger.Since = now
	}
	s.fh.mode.Ledger.AddLeg(hedge.Counterfactual{At: now, Mode: mode, Instrument: pos.Instrument, OptionType: pos.OptionType,
		Qty: qty, ShedPrice: price, LastPrice: price, LastAt: now})
	s.fh.dirty = true
}

// followCounterfactuals marks each open counterfactual at its leg's mid. A
// leg ends at its DTE roll, at expiry, or (mode hedged) when the bot closes
// the kept leg by another rule — the last mark seen stays its value.
func (s *Strategy) followCounterfactuals(now time.Time) {
	held := map[string]bool{}
	for _, p := range s.state.AllPositions() {
		held[p.Instrument] = true
	}
	for id := range s.fh.shedNoted {
		if _, ok := s.state.GetPosition(id); !ok {
			delete(s.fh.shedNoted, id)
		}
	}
	legs := s.fh.mode.Ledger.Legs
	for i := range legs {
		c := &legs[i]
		if c.Done {
			continue
		}
		if inst, ok := s.md.GetInstrument(c.Instrument); ok && inst.HasQuote() {
			c.LastPrice, c.LastAt = inst.Mid, now
			s.fh.dirty = true
		}
		_, expiry, _, _, err := marketdata.ParseOptionName(c.Instrument)
		switch {
		case err == nil && (now.After(expiry) || marketdata.DaysToExpiry(expiry, now) <= s.cfg.RolloutDTE):
			c.Done = true // the roll (or expiry) would have closed it either way
		case c.Mode == hedge.CounterHedged && !held[c.Instrument]:
			c.Done = true // the kept leg was closed by another rule
		}
		if c.Done {
			s.fh.dirty = true
		}
	}
}

// counterfactualInstruments are the instruments the benchmark still follows
// (trackPositions keeps their tickers).
func (s *Strategy) counterfactualInstruments() []string {
	var out []string
	for _, c := range s.fh.mode.Ledger.Legs {
		if !c.Done {
			out = append(out, c.Instrument)
		}
	}
	return out
}

// saveHedge writes the mode file when it changed (at most every
// hedgeSaveEvery for follow-up prices; force for trades and switches).
func (s *Strategy) saveHedge(now time.Time, force bool) error {
	if s.fh.path == "" || !s.fh.dirty {
		return nil
	}
	if !force && now.Sub(s.fh.savedAt) < hedgeSaveEvery {
		return nil
	}
	if err := hedge.SaveMode(s.fh.path, s.fh.mode); err != nil {
		slog.Error("futures hedge: cannot save the mode file", "path", s.fh.path, "err", err)
		return err
	}
	s.fh.dirty, s.fh.savedAt = false, now
	return nil
}

// ── Closing the perpetual ────────────────────────────────────────────────────

// closePerp closes the perpetual at market (reduce-only), up to attempts
// times, and returns what it realised (coin, net of fees; funding not
// included) and the size left of what owned gives (USD, signed). owned says
// how much of the exchange's position this close may touch. reason is the
// order's trigger.
func (s *Strategy) closePerp(ctx context.Context, reason, why string, attempts int, owned func(exchangeUSD float64) float64) (realised, left float64, err error) {
	before := s.fh.mode.Ledger.Real.Realised - s.fh.mode.Ledger.Real.Fees
	beyond := 0.0 // what the exchange holds past owned: never touched
	for i := 0; i < attempts; i++ {
		perp, rerr := s.readPerp(ctx)
		if rerr != nil {
			err = fmt.Errorf("read the perpetual position: %w", rerr)
			continue
		}
		spot := s.md.UnderlyingPrice()
		held := perp.Size
		perp.Size = owned(held)
		beyond = held - perp.Size
		s.adoptPerpDrift(perp, spot)
		left = perp.Size
		if math.Abs(perp.Size) < 0.5 {
			err = nil
			break
		}
		side := hedge.SideBuy
		if perp.Size > 0 {
			side = hedge.SideSell
		}
		order := orders.Order{Instrument: s.perpName(), Direction: side, OrderType: orders.TypeMarket,
			Qty: math.Abs(perp.Size), TriggerReason: reason, Label: s.hedgeLabel(), ReduceOnly: true}
		fill, serr := s.exch.Submit(ctx, order)
		if serr != nil {
			s.hedgeOrderFailed(ctx, order, serr, why)
			err = serr
			continue
		}
		booked := s.bookPerpFill(side, fill, perpMark(perp, spot), spot)
		left = perp.Size + map[string]float64{hedge.SideBuy: 1, hedge.SideSell: -1}[side]*fill.Qty
		s.hedgeJournal(orders.HedgeRecord{Change: orders.HedgeClosed, Side: side, AmountUSD: order.Qty, FilledUSD: fill.Qty,
			Price: fill.FillPrice, LedgerPrice: booked, FeeCoin: fill.Fee, FundingCoin: perp.RealizedFunding, OrderID: fill.OrderID, OptionsDelta: s.state.TotalCoinDelta(),
			PerpDelta:    hedge.PerpDelta(perp.Size, perpMark(perp, spot)),
			RealisedCoin: s.fh.mode.Ledger.Real.Realised - s.fh.mode.Ledger.Real.Fees - before,
			Detail:       fmt.Sprintf("%s: %s %.0f USD of %s at market (reduce-only), filled %.0f", why, side, order.Qty, s.perpName(), fill.Qty)})
		err = nil
		if math.Abs(left) < 0.5 {
			break
		}
	}
	if err == nil && math.Abs(left) >= 0.5 {
		err = fmt.Errorf("%.0f USD of %s still open", left, s.perpName())
	}
	s.fh.perp.Size = left + beyond // the view shows what is left, not the read before the close
	s.saveHedge(time.Now(), true)
	return s.fh.mode.Ledger.Real.Realised - s.fh.mode.Ledger.Real.Fees - before, left, err
}

// wholePerp is a close of the whole position (the switch-off).
func wholePerp(exchangeUSD float64) float64 { return exchangeUSD }

// ownPerp is the part of the exchange's position that is the bot's: the
// ledger's, and nothing on a live account with the mode off, where the
// experiment cannot have traded whatever an old mode file says.
func (s *Strategy) ownPerp(exchangeUSD float64) float64 {
	if s.cfg.IsLive() && !s.fh.mode.Enabled {
		return 0
	}
	return hedge.OwnPerp(s.fh.mode.Ledger.Real.SizeUSD, exchangeUSD)
}

// flattenPerp is the kill switch's part: the bot's own perpetual is closed
// at market and checked flat. Anything beyond it (a position a person holds
// on purpose, #667) is left alone and journaled once.
func (s *Strategy) flattenPerp(ctx context.Context) {
	if s.futures == nil {
		return
	}
	ledger := s.fh.mode.Ledger.Real.SizeUSD
	_, left, err := s.closePerp(ctx, orders.TriggerKillSwitch, "kill switch: closing the bot's perpetual", killSwitchAttempts, s.ownPerp)
	switch {
	case err != nil:
		slog.Error("kill switch: the bot's perpetual is not flat — close it manually", "instrument", s.perpName(), "left_usd", left, "err", err)
	default:
		slog.Warn("kill switch: none of the bot's perpetual left", "instrument", s.perpName())
	}
	if beyond := s.fh.perp.Size - left; math.Abs(beyond) >= 0.5 {
		slog.Warn("kill switch: a perpetual that is not the bot's stays open", "instrument", s.perpName(), "left_usd", beyond, "ledger_usd", ledger)
		s.hedgeJournal(orders.HedgeRecord{Change: orders.HedgeLeftOpen, AmountUSD: math.Abs(beyond),
			OptionsDelta: s.state.TotalCoinDelta(), PerpDelta: hedge.PerpDelta(beyond, perpMark(s.fh.perp, s.md.UnderlyingPrice())),
			Detail: fmt.Sprintf("kill switch: %+.0f USD of %s left open: beyond the %+.0f USD the bot's ledger held, so not the bot's to close", beyond, s.perpName(), ledger)})
	}
}

// ── Switching (the monitor) ──────────────────────────────────────────────────

// ErrHedgeBusy: the decision loop did not take the switch in time.
var ErrHedgeBusy = errors.New("the decision loop did not take the request in time; nothing was changed")

// HedgeSwitchResult is the outcome of a switch.
type HedgeSwitchResult struct {
	Enabled      bool    `json:"enabled"`
	ClosedUSD    float64 `json:"closed_usd,omitempty"`    // the perpetual closed (switch off)
	RealisedCoin float64 `json:"realised_coin,omitempty"` // what that close realised, net of fees
	Detail       string  `json:"detail"`
}

type hedgeReq struct {
	enable   bool
	by       orders.Actor
	deadline time.Time
	reply    chan hedgeReply
}

type hedgeReply struct {
	res HedgeSwitchResult
	err error
}

// SetFuturesHedge asks the decision loop to switch the experiment on or
// off. Safe from any goroutine; a request the loop has not taken within
// manualPickup is dropped unexecuted. Off closes the perpetual at market
// first and stays on if that fails.
func (s *Strategy) SetFuturesHedge(ctx context.Context, enable bool, by orders.Actor) (HedgeSwitchResult, error) {
	s.pub.mu.RLock()
	halted := s.pub.halted
	s.pub.mu.RUnlock()
	switch {
	case s.futures == nil || s.fh.path == "":
		return HedgeSwitchResult{}, errors.New("the futures hedge is not available on this bot")
	case halted:
		return HedgeSwitchResult{}, errors.New("the kill switch has fired: the bot is idle")
	}
	req := hedgeReq{enable: enable, by: by, deadline: time.Now().Add(manualPickup), reply: make(chan hedgeReply, 1)}
	select {
	case s.hedgeCh <- req:
	default:
		return HedgeSwitchResult{}, errors.New("another hedge switch is waiting; try again")
	}
	select {
	case r := <-req.reply:
		return r.res, r.err
	case <-ctx.Done():
		return HedgeSwitchResult{}, ctx.Err()
	}
}

// runHedgeSwitch executes a switch on the Run goroutine.
func (s *Strategy) runHedgeSwitch(ctx context.Context, req hedgeReq) {
	if time.Now().After(req.deadline) {
		req.reply <- hedgeReply{err: ErrHedgeBusy}
		return
	}
	defer s.publish()
	res, err := s.switchHedge(ctx, req.enable, req.by, time.Now())
	req.reply <- hedgeReply{res: res, err: err}
}

func (s *Strategy) switchHedge(ctx context.Context, enable bool, by orders.Actor, now time.Time) (HedgeSwitchResult, error) {
	actor := by
	if enable == s.fh.mode.Enabled {
		return HedgeSwitchResult{Enabled: enable, Detail: "unchanged: the experiment is already " + onOff(enable)}, nil
	}
	res := HedgeSwitchResult{}
	spot := s.md.UnderlyingPrice()
	if enable {
		if s.cfg.IsLive() {
			return res, errors.New("the futures hedge experiment is testnet-only")
		}
		if _, err := s.perpSpec(ctx); err != nil {
			return res, err
		}
	} else {
		before := s.fh.perp.Size
		if p, err := s.readPerp(ctx); err == nil {
			before = p.Size
		}
		realised, _, err := s.closePerp(ctx, orders.TriggerFuturesHedgeClose, "futures hedge switched off by "+actor.String(), 2, wholePerp)
		res.RealisedCoin = realised
		if err != nil {
			return res, fmt.Errorf("the perpetual could not be closed (%v): the experiment stays ON", err)
		}
		res.ClosedUSD = math.Abs(before)
	}
	prev := s.fh.mode
	s.fh.mode.Enabled, s.fh.mode.Since, s.fh.mode.By = enable, now, hedge.Actor{User: actor.User, Role: actor.Role}
	if s.fh.mode.Ledger.Since.IsZero() {
		s.fh.mode.Ledger.Since = now
	}
	s.fh.dirty = true
	if err := s.saveHedge(now, true); err != nil {
		s.fh.mode = prev // a switch that cannot be kept across a restart is not made
		return res, fmt.Errorf("cannot save the mode: %w", err)
	}
	s.fh.health, s.fh.unavailNoted, s.fh.awayNoted = hedge.Health{}, false, false
	l := &s.fh.mode.Ledger
	if enable {
		// The paper hedge was the hedge side while OFF: it ends at today's price.
		s.fh.sent, s.fh.rateNoted = nil, false
		l.Virtual.Trade(-l.Virtual.SizeUSD, spot, config.FixedFuturesHedgeFeeRate)
	} else {
		// A kept leg's holding ends here: from now the shed acts on it, and a
		// real shed is followed as its own counterfactual.
		for i := range l.Legs {
			if l.Legs[i].Mode == hedge.CounterHedged {
				l.Legs[i].Done = true
			}
		}
		clear(s.fh.shedNoted)
	}
	s.saveHedge(now, true)
	on := enable
	res.Enabled = enable
	res.Detail = fmt.Sprintf("futures hedge %s by %s", onOff(enable), actor)
	if !enable && res.ClosedUSD > 0 {
		res.Detail += fmt.Sprintf("; %.0f USD of %s closed at market, realised %+.6f %s", res.ClosedUSD, s.perpName(), res.RealisedCoin, s.cfg.Underlying)
	}
	s.hedgeJournal(orders.HedgeRecord{Change: orders.HedgeModeChanged, Actor: &actor, Enabled: &on,
		OptionsDelta: s.state.TotalCoinDelta(), PerpDelta: hedge.PerpDelta(s.fh.perp.Size, perpMark(s.fh.perp, s.md.UnderlyingPrice())),
		RealisedCoin: res.RealisedCoin, Detail: res.Detail})
	slog.Warn("futures hedge switched", "enabled", enable, "by", actor)
	return res, nil
}

func onOff(on bool) string {
	if on {
		return "ON"
	}
	return "OFF"
}
