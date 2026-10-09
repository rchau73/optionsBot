package tests

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/orders"
	"optionsbot/internal/risk"
	"optionsbot/internal/strategy"
)

// A person may close a leg from the monitor only while the regime-side rule
// keeps the bot from selling that side (2026-10-09: BTC's puts, re-sold by
// repair during two minutes of neutral trend, stayed open in a bear trend
// under a confirmed negative regime). Every other close belongs to the rules.

func TestManualCloseBlockReason(t *testing.T) {
	neg := risk.Status{RegimeUsed: true, RegimeKnown: true, RegimeNegative: true}
	pos := risk.Status{RegimeUsed: true, RegimeKnown: true}
	cases := []struct {
		typ     string
		trend   int
		st      risk.Status
		allowed bool
		why     string
	}{
		{"put", -1, neg, true, ""},
		{"call", 1, neg, true, ""},
		{"call", -1, neg, false, "trend is bear, so the bot still sells calls"},
		{"put", 0, neg, false, "trend is neutral"},
		{"put", -1, pos, false, "regime is not negative"},
	}
	for _, c := range cases {
		got := strategy.ManualCloseBlockReason(c.typ, c.trend, c.st)
		if (got == "") != c.allowed || !strings.Contains(got, c.why) {
			t.Errorf("%s trend %d negative %v: %q, want allowed=%v containing %q", c.typ, c.trend, c.st.RegimeNegative, got, c.allowed, c.why)
		}
	}
}

func legView(t *testing.T, s *strategy.Strategy, instrument string) (strategy.LegView, bool) {
	t.Helper()
	for _, st := range s.View().Strangles {
		for _, l := range st.Legs {
			if l.Instrument == instrument {
				return l, true
			}
		}
	}
	return strategy.LegView{}, false
}

func TestStrategy_ManualCloseBuysBackABlockedPutAndHoldsItsRepair(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.withBearTrendAboveFlip()
	f.startRun()
	f.strat.SetManualClose(true, "")

	eventually(t, 2*time.Second, "the put is closeable", func() bool {
		l, ok := legView(t, f.strat, f.put)
		return ok && l.ManualClose.Allowed
	})
	put, _ := legView(t, f.strat, f.put)
	if put.ManualClose.Rebuilt || !strings.Contains(put.ManualClose.After, "repair is held") || !strings.Contains(put.ManualClose.Reason, "no new puts") {
		t.Errorf("put view = %+v", put.ManualClose)
	}
	if call, _ := legView(t, f.strat, f.call); call.ManualClose.Allowed {
		t.Errorf("the call is not blocked: no manual close, got %+v", call.ManualClose)
	}

	res, err := f.strat.ManualClose(context.Background(), []string{put.PositionID}, "test")
	if err != nil || len(res) != 1 || res[0].Error != "" || res[0].Filled < 0.1-1e-9 {
		t.Fatalf("ManualClose = %+v, %v", res, err)
	}
	var market int
	for _, o := range f.exch.buys() {
		if o.Instrument == f.put && o.OrderType == orders.TypeMarket {
			market++
		}
	}
	if market != 1 {
		t.Errorf("one market buy-back of the put, got %d", market)
	}
	closes := f.journal.events(orders.EventClosed)
	if len(closes) != 1 || closes[0].trigger != orders.TriggerManualClose || !strings.Contains(closes[0].ctx.Detail, "manual close from the monitor") {
		t.Errorf("journal closes = %+v", closes)
	}
	// Held like a stop-out: once the trend leaves bear the regime-side block
	// lifts (as at 08:39), but the confirmed negative regime still holds it.
	f.market.mu.Lock()
	f.market.price = 106000 // above SMA9, below SMA21: neutral
	f.market.mu.Unlock()
	eventually(t, 2*time.Second, "trend neutral", func() bool { return f.strat.View().Trend == "neutral" })
	eventually(t, 2*time.Second, "repair held by the stop record", func() bool {
		return f.skips(strategy.SkipRepairHeld+": put stopped out, confirmed negative gamma regime") > 0
	})
	if n := putSells(f); n != 0 {
		t.Errorf("the closed put must not be repaired, got %d sells", n)
	}
}

func TestStrategy_ManualCloseRefusedWithoutTheBlock(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.startRun()
	f.strat.SetManualClose(true, "")

	eventually(t, 2*time.Second, "positions loaded", func() bool { _, ok := legView(t, f.strat, f.put); return ok })
	put, _ := legView(t, f.strat, f.put)
	if put.ManualClose.Allowed || !strings.Contains(put.ManualClose.Reason, "only while the regime-side block is on") {
		t.Errorf("put view = %+v", put.ManualClose)
	}
	res, err := f.strat.ManualClose(context.Background(), []string{put.PositionID}, "test")
	if err != nil || len(res) != 1 || res[0].Error == "" || res[0].Filled != 0 {
		t.Fatalf("ManualClose = %+v, %v: must be refused on the loop too", res, err)
	}
	if n := len(f.exch.buys()); n != 0 {
		t.Errorf("nothing bought back, got %d buys", n)
	}
}

func TestStrategy_ManualCloseOff(t *testing.T) {
	f := newStrategyFixture(t)
	f.withOpenStrangle(0.1, 0.02)
	f.withBearTrendAboveFlip()
	f.startRun()
	f.strat.SetManualClose(false, "manual close is testnet-only")

	eventually(t, 2*time.Second, "positions loaded", func() bool { _, ok := legView(t, f.strat, f.put); return ok })
	if put, _ := legView(t, f.strat, f.put); put.ManualClose.Allowed || put.ManualClose.Reason != "manual close is testnet-only" {
		t.Errorf("put view = %+v", put.ManualClose)
	}
	if _, err := f.strat.ManualClose(context.Background(), []string{"x"}, "test"); err == nil {
		t.Error("ManualClose must fail while off")
	}
}

type fakeCloser struct {
	ids []string
	err error
}

func (c *fakeCloser) ManualClose(_ context.Context, ids []string, _ string) ([]strategy.ManualCloseResult, error) {
	c.ids = ids
	if c.err != nil {
		return nil, c.err
	}
	return []strategy.ManualCloseResult{{PositionID: ids[0], Qty: 1, Filled: 1}}, nil
}

func TestAPI_ManualCloseNeedsTheToken(t *testing.T) {
	logger := orders.NewWriterLogger(io.Discard, 0)
	post := func(h http.Handler, auth, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/positions/close", strings.NewReader(body))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	off := api.New(staticView{sampleView()}, logger, api.WithManualClose(&fakeCloser{}, "")).Handler()
	if rec := post(off, "Bearer ", `{"position_ids":["p1"]}`); rec.Code == http.StatusOK {
		t.Errorf("no token configured: the route must not exist, got %d", rec.Code)
	}

	c := &fakeCloser{}
	h := api.New(staticView{sampleView()}, logger, api.WithManualClose(c, "s3cret")).Handler()
	if rec := post(h, "Bearer wrong", `{"position_ids":["p1"]}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("bad token: %d", rec.Code)
	}
	if rec := post(h, "", `{"position_ids":["p1"]}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d", rec.Code)
	}
	if rec := post(h, "Bearer s3cret", `{"position_ids":[]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("no ids: %d", rec.Code)
	}
	if c.ids != nil {
		t.Fatalf("nothing may reach the strategy before auth and validation, got %v", c.ids)
	}
	rec := post(h, "Bearer s3cret", `{"position_ids":["p1"]}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"filled":1`) || len(c.ids) != 1 {
		t.Errorf("close: %d %s", rec.Code, rec.Body)
	}
	c.err = errors.New("the kill switch has fired: the bot is idle")
	if rec := post(h, "Bearer s3cret", `{"position_ids":["p1"]}`); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "kill switch") {
		t.Errorf("refused: %d %s", rec.Code, rec.Body)
	}
}
