package strategy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/risk"
)

// Manual close from the monitor. The regime-side rule (regimeside.go) only
// stops new shorts: a leg sold before the block, or in a moment the trend
// read neutral (2026-10-09 08:39 BTC: puts re-sold by repair for two
// minutes of neutral), stays open. A person may close such a leg by hand —
// and only such a leg: every other close belongs to the rules.

// ManualCloseBlockReason says why a short leg of optType may not be closed by
// hand now, or "" when it may: only while the regime-side rule blocks new
// shorts of that type (a confirmed negative gamma regime, puts in a bear
// trend, calls in a bull trend).
func ManualCloseBlockReason(optType string, trend int, st risk.Status) string {
	if RegimeSideBlockReason(optType, trend, st) != "" {
		return ""
	}
	if !st.RegimeUsed || !st.RegimeNegative {
		return "manual close is allowed only while the regime-side block is on: the confirmed gamma regime is not negative"
	}
	return fmt.Sprintf("manual close is allowed only while the regime-side block is on: the confirmed regime is negative but the trend is %s, so the bot still sells %ss",
		trendWord(trend), optType)
}

func trendWord(trend int) string {
	switch {
	case trend > 0:
		return "bull"
	case trend < 0:
		return "bear"
	}
	return "neutral"
}

// trendDirOf maps GammaMonitor.Trend's label to TrendDir.
func trendDirOf(label string) int {
	switch label {
	case "bull":
		return 1
	case "bear":
		return -1
	}
	return 0
}

// ManualCloseView tells the monitor whether a leg can be closed by hand now
// and what follows if it is.
type ManualCloseView struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"` // why it is allowed (the active block) or why not
	// After is what the bot does once the leg is closed. Rebuilt is true when
	// the close would be undone: the strangle's last leg frees its slot, and
	// the next cycle enters a new strangle there.
	After   string `json:"after,omitempty"`
	Rebuilt bool   `json:"rebuilt"`
}

// SetManualClose turns manual closes on (the API holds the token), or off
// with the reason the monitor shows on every leg.
func (s *Strategy) SetManualClose(enabled bool, offReason string) {
	s.pub.mu.Lock()
	defer s.pub.mu.Unlock()
	s.pub.manualOn, s.pub.manualOff = enabled, offReason
}

// manualCloseView is the view of one leg; lastLeg when it is its strangle's
// only open leg. Reads published state only.
func (s *Strategy) manualCloseView(pos *orders.Position, lastLeg bool, pub *published) ManualCloseView {
	switch {
	case !pub.manualOn:
		return ManualCloseView{Reason: pub.manualOff}
	case pub.halted:
		return ManualCloseView{Reason: "the kill switch has fired: the bot is idle"}
	case pos.Side != "" && pos.Side != orders.DirectionSell:
		return ManualCloseView{Reason: "only short legs can be closed by hand"}
	}
	trend := trendDirOf(pub.trend)
	if why := ManualCloseBlockReason(pos.OptionType, trend, pub.risk.Status); why != "" {
		return ManualCloseView{Reason: why}
	}
	v := ManualCloseView{Allowed: true, Reason: RegimeSideBlockReason(pos.OptionType, trend, pub.risk.Status)}
	if lastLeg {
		v.Rebuilt = true
		v.After = "this is the strangle's last leg: its slot becomes vacant and the next cycle enters a new strangle there (the other side only, while the block lasts)"
	} else {
		v.After = fmt.Sprintf("repair is held as after a stop-out: no re-sell while the confirmed regime is negative or entries are frozen, and not before %s (%dh cooldown)",
			time.Now().Add(time.Duration(s.cfg.RepairCooldownHours)*time.Hour).UTC().Format("Jan 2 15:04 UTC"), s.cfg.RepairCooldownHours)
	}
	return v
}

// ManualCloseResult is the outcome for one position.
type ManualCloseResult struct {
	PositionID string  `json:"position_id"`
	Instrument string  `json:"instrument,omitempty"`
	Qty        float64 `json:"qty"`    // asked
	Filled     float64 `json:"filled"` // bought back
	Error      string  `json:"error,omitempty"`
}

type manualReq struct {
	ids      []string
	by       string
	deadline time.Time // not started after this: the caller has given up
	reply    chan []ManualCloseResult
}

// ErrManualCloseBusy: the decision loop did not take the request in time.
var ErrManualCloseBusy = errors.New("the decision loop did not take the request in time; nothing was sent")

// manualPickup is how long a request may wait for the loop to take it.
const manualPickup = 25 * time.Second

// ManualClose asks the decision loop to buy back the given positions at
// market. Safe from any goroutine. The loop re-checks each position against
// ManualCloseBlockReason; a request it has not taken within manualPickup is
// dropped unexecuted, so a caller that gave up never closes later.
func (s *Strategy) ManualClose(ctx context.Context, ids []string, by string) ([]ManualCloseResult, error) {
	s.pub.mu.RLock()
	on, off, halted := s.pub.manualOn, s.pub.manualOff, s.pub.halted
	s.pub.mu.RUnlock()
	switch {
	case !on:
		return nil, errors.New(off)
	case halted:
		return nil, errors.New("the kill switch has fired: the bot is idle")
	case len(ids) == 0:
		return nil, errors.New("no position given")
	}
	req := manualReq{ids: ids, by: by, deadline: time.Now().Add(manualPickup), reply: make(chan []ManualCloseResult, 1)}
	select {
	case s.manualCh <- req:
	default:
		return nil, errors.New("another manual close is waiting; try again")
	}
	pickup := time.NewTimer(manualPickup + 5*time.Second)
	defer pickup.Stop()
	for {
		select {
		case res := <-req.reply:
			if res == nil {
				return nil, ErrManualCloseBusy
			}
			return res, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-pickup.C:
			// Taken but still running (a market order is seconds): keep
			// waiting on ctx; not taken: it will be dropped at its deadline.
			pickup.Reset(time.Hour)
		}
	}
}

// runManualClose executes a request on the Run goroutine.
func (s *Strategy) runManualClose(ctx context.Context, req manualReq) {
	if time.Now().After(req.deadline) {
		slog.Warn("manual close dropped: taken after its deadline", "positions", req.ids, "by", req.by)
		req.reply <- nil
		return
	}
	defer s.publish()
	rs := s.riskView().Status
	trend := trendDirOf(s.gamma.Trend())
	out := make([]ManualCloseResult, 0, len(req.ids))
	for _, id := range req.ids {
		r := ManualCloseResult{PositionID: id}
		pos, ok := s.state.GetPosition(id)
		if !ok {
			r.Error = "not in the book (closed already?)"
			out = append(out, r)
			continue
		}
		r.Instrument, r.Qty = pos.Instrument, pos.Qty
		if why := ManualCloseBlockReason(pos.OptionType, trend, rs); why != "" {
			r.Error = why
			s.journal.LogSkipped("manual_close_refused "+pos.Instrument+": "+why, s.instrumentContext(pos.Instrument, s.slotOf(pos.ID)))
			out = append(out, r)
			continue
		}
		block := RegimeSideBlockReason(pos.OptionType, trend, rs)
		key, inStrangle := s.stopKeyOf(pos)
		at := time.Now()
		if inStrangle {
			s.stopped[key] = at // repair holds it like a stop-out (RepairBlockReason)
		}
		detail := fmt.Sprintf("manual close from the monitor (%s): %s; repair held as after a stop-out", req.by, block)
		slog.Warn("manual close", "instrument", pos.Instrument, "qty", pos.Qty, "by", req.by, "block", block)
		filled, err := s.buyToClose(ctx, pos, pos.Qty, orders.TriggerManualClose, 0, detail)
		r.Filled = filled
		if err != nil {
			r.Error = err.Error()
		}
		if filled <= qtyEpsilon && inStrangle && s.stopped[key].Equal(at) {
			delete(s.stopped, key) // nothing closed: the leg is still there, nothing to hold
		}
		out = append(out, r)
	}
	req.reply <- out
}

// stopKeyOf is the stopped key of pos's leg in its strangle.
func (s *Strategy) stopKeyOf(pos *orders.Position) (string, bool) {
	for _, st := range s.state.AllStrangles() {
		if (st.CallLeg != nil && st.CallLeg.ID == pos.ID) || (st.PutLeg != nil && st.PutLeg.ID == pos.ID) {
			return stopKey(st.ID, pos.OptionType), true
		}
	}
	return "", false
}
