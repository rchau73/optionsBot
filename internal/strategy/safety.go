package strategy

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"optionsbot/internal/orders"
)

// Guards added after the 2026-10-06 night on testnet, when the book and the
// exchange disagreed and the bot churned for hours (buy back at the ask,
// re-sell at the bid), shrank every position and left the account long:
//
//   - a buy-back never exceeds the short the exchange actually holds, so a
//     wrong book can never flip a position long (capToExchange);
//   - a long on an instrument the bot trades is reported and sold at the bid
//     (flattenLongs) — the strategy only holds shorts, and a hidden long would
//     silently absorb every repair sold into it;
//   - a slot that buys back and re-sells the same exposure churn_max_round_trips
//     times within churn_window_minutes is paused for that window (churn
//     breaker): no entries, repairs, upsizes or leg balancing; exits still run.

// exchangeSnapshot is the exchange's option positions from this cycle's
// position check: shorts and longs by instrument, both as positive sizes.
type exchangeSnapshot struct {
	short map[string]float64
	long  map[string]float64
}

// capToExchange limits a buy-back to the short the exchange holds on the
// instrument. Without a snapshot this cycle (the read failed) the qty passes
// unchanged: a stop-loss must never wait on it.
func (s *Strategy) capToExchange(instrument string, qty float64) float64 {
	if s.exchSnap == nil {
		return qty
	}
	held := s.exchSnap.short[instrument]
	if qty > held+qtyEpsilon {
		slog.Error("buy back capped at the exchange's short: the book holds more than the exchange",
			"instrument", instrument, "requested_qty", qty, "exchange_short", held)
		return math.Max(held, 0)
	}
	return qty
}

// spendExchangeShort lowers the snapshot after a buy-back fills, so a second
// close of the same instrument in this cycle is capped correctly.
func (s *Strategy) spendExchangeShort(instrument string, filled float64) {
	if s.exchSnap != nil {
		s.exchSnap.short[instrument] = math.Max(s.exchSnap.short[instrument]-filled, 0)
	}
}

// managedInstruments are the instruments the bot trades now or traded since
// it started: book positions, working orders and past buy-backs. A long on
// any other instrument (e.g. a hedge placed by hand) is not the bot's.
func (s *Strategy) managedInstruments() map[string]bool {
	out := map[string]bool{}
	for _, p := range s.state.AllPositions() {
		out[p.Instrument] = true
	}
	for _, ps := range s.pendingSnapshot() {
		for _, l := range ps.legs() {
			out[l.instrument] = true
		}
	}
	for name := range s.boughtBack {
		out[name] = true
	}
	return out
}

// flattenLongs sells, at the bid, any long the exchange shows on an
// instrument the bot trades. Closing a long reduces risk, so it runs even
// while new risk is frozen. Unfilled remainders are retried next cycle.
func (s *Strategy) flattenLongs(ctx context.Context) {
	if s.exchSnap == nil {
		return
	}
	managed := s.managedInstruments()
	for name, size := range s.exchSnap.long {
		if !managed[name] || s.instrumentUnconfirmed(name) {
			continue
		}
		inst, ok := s.md.GetInstrument(name)
		if !ok || inst.Bid <= 0 {
			slog.Error("long position on a traded instrument: no bid to sell into, retrying next cycle",
				"instrument", name, "long_qty", size)
			continue
		}
		price := orders.RoundToStep(inst.Bid, inst.EffectiveTick(inst.Bid))
		slog.Error("long position on a traded instrument: the strategy only holds shorts — selling it at the bid",
			"instrument", name, "long_qty", size, "bid", price)
		label := s.uniqueOrderLabel(nil)
		fill, err := s.exch.Submit(ctx, orders.Order{
			Instrument: name, Direction: orders.DirectionSell, OrderType: orders.TypeLimit,
			Qty: size, LimitPrice: price, TickSize: inst.EffectiveTick(price),
			TimeInForce: orders.TimeInForceIOC, TriggerReason: orders.TriggerCloseLong, Label: label,
		})
		if err != nil {
			if orders.MaybePlaced(err) {
				s.noteUnconfirmed(label, name, nil, err)
			}
			slog.Error("long position: sell failed, retrying next cycle", "instrument", name, "err", err)
			continue
		}
		s.journal.LogSubmit(orders.PendingOrderRecord{
			OrderID: fill.OrderID, Instrument: name, OptionType: inst.OptionType,
			Direction: orders.DirectionSell, OrderType: orders.TypeLimit, TriggerReason: orders.TriggerCloseLong,
			Qty: size, LimitPrice: price, Greeks: toOrderGreeks(inst),
		}, s.eventContext(inst, nil))
		s.exchSnap.long[name] = math.Max(size-fill.Qty, 0)
		slog.Info("long position: sold", "instrument", name, "filled_qty", fill.Qty, "price", fill.FillPrice,
			"remaining_long", s.exchSnap.long[name])
	}
}

// churnLog is a slot's recent buy-backs and sell fills.
type churnLog struct {
	buys, sells []time.Time
}

// noteChurn records a buy-back (buy=true) or a sell fill for slot and trips
// the breaker when both reach churn_max_round_trips within the window.
func (s *Strategy) noteChurn(slot *orders.SlotRef, buy bool, now time.Time) {
	if slot == nil || s.cfg.ChurnMaxRoundTrips <= 0 {
		return
	}
	key := makeSlotKey(slot.DTE, slot.Delta)
	window := time.Duration(s.cfg.ChurnWindowMinutes) * time.Minute
	c := s.churn[key]
	if c == nil {
		c = &churnLog{}
		s.churn[key] = c
	}
	if buy {
		c.buys = append(c.buys, now)
	} else {
		c.sells = append(c.sells, now)
	}
	c.buys, c.sells = within(c.buys, now, window), within(c.sells, now, window)
	if !ChurnTripped(len(c.buys), len(c.sells), s.cfg.ChurnMaxRoundTrips) || s.churnPaused(key, now) {
		return
	}
	s.churnUntil[key] = now.Add(window)
	detail := fmt.Sprintf("%d buy-backs and %d sells within %s: no entries, repairs, upsizes or leg balancing until %s",
		len(c.buys), len(c.sells), window, s.churnUntil[key].Format(time.RFC3339))
	slog.Error("churn breaker: slot paused", "target_dte", slot.DTE, "entry_delta", slot.Delta, "detail", detail)
	s.noteSkip(slot.DTE, slot.Delta, SkipChurnPaused, detail)
	c.buys, c.sells = nil, nil
}

// ChurnTripped reports whether a slot has bought back and re-sold max times.
func ChurnTripped(buys, sells, max int) bool {
	return max > 0 && buys >= max && sells >= max
}

// churnPaused reports whether the churn breaker holds slot key now.
func (s *Strategy) churnPaused(key slotKey, now time.Time) bool {
	return now.Before(s.churnUntil[key])
}

// within keeps the times no older than window.
func within(ts []time.Time, now time.Time, window time.Duration) []time.Time {
	out := ts[:0]
	for _, t := range ts {
		if now.Sub(t) <= window {
			out = append(out, t)
		}
	}
	return out
}
