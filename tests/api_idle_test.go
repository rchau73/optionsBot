package tests

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/orders"
	"optionsbot/internal/risk"
	"optionsbot/internal/strategy"
)

// #781: /api/summary's idle note — why a bot that holds nothing, or may not
// open anything new, is not trading, from the journal's latest skip or
// freeze line after its latest trade.

func idleEvent(seq uint64, event string, line map[string]any) orders.RecentEvent {
	b, _ := json.Marshal(line)
	return orders.RecentEvent{Seq: seq, Event: event, At: time.Date(2026, 11, 14, 10, 0, int(seq), 0, time.UTC), Data: b}
}

func idleSkip(seq uint64, reason string) orders.RecentEvent {
	return idleEvent(seq, orders.EventSkipped, map[string]any{"skip_reason": reason})
}

func TestAPIIdle_IdleOf(t *testing.T) {
	held := summaryView(time.Now()).Strangles
	allowed := strategy.RiskView{EntriesAllowed: true}
	frozen := strategy.RiskView{Status: risk.Status{Frozen: true, FreezeReason: "DVOL band 2 → 1 (1/2 closes)"}}
	gexWait := strategy.RiskView{}
	freezeLine := idleEvent(4, orders.EventRisk, map[string]any{"change": orders.RiskFrozen, "detail": "regime NEGATIVE (1/2 closes)"})
	fill := idleEvent(3, orders.EventFilled, map[string]any{"trigger_reason": "entry"})

	cases := []struct {
		name       string
		view       strategy.View
		events     []orders.RecentEvent
		wantNil    bool
		wantReason string
		wantSeq    uint64
		wantDetail string
	}{
		{name: "open strangles and entries allowed: no idle note", view: strategy.View{Strangles: held, Risk: allowed},
			events: []orders.RecentEvent{idleSkip(1, "no_free_expiry: every expiry up to 40 DTE is held")}, wantNil: true},
		{name: "an entry working with entries allowed: not idle", view: strategy.View{Pending: []strategy.PendingView{{ID: "p"}}, Risk: allowed}, wantNil: true},
		{name: "nothing held, the latest skip says why", view: strategy.View{Risk: allowed},
			events:     []orders.RecentEvent{idleSkip(1, "gex_wait: no snapshot"), idleSkip(2, "no_free_expiry: every expiry up to 40 DTE is held")},
			wantReason: strategy.SkipNoFreeExpiry, wantSeq: 2, wantDetail: "every expiry up to 40 DTE is held"},
		{name: "nothing held and frozen: the freeze line", view: strategy.View{Risk: frozen},
			events:     []orders.RecentEvent{idleSkip(1, "no_expiry"), fill, freezeLine},
			wantReason: strategy.SkipRiskFrozen, wantSeq: 4, wantDetail: "regime NEGATIVE (1/2 closes)"},
		{name: "a skip before the latest trade no longer explains the book", view: strategy.View{Risk: allowed},
			events: []orders.RecentEvent{idleSkip(1, "no_expiry"), fill}, wantReason: ""},
		{name: "held and frozen with no line since the trade: the view's freeze", view: strategy.View{Strangles: held, Risk: frozen},
			events: []orders.RecentEvent{freezeLine, fill}, wantReason: strategy.SkipRiskFrozen, wantDetail: "DVOL band 2 → 1 (1/2 closes)"},
		{name: "blocked: only a reason that blocks every entry counts", view: strategy.View{Strangles: held, Risk: gexWait},
			events:     []orders.RecentEvent{idleSkip(1, "gex_wait: snapshot 6m old"), idleSkip(2, "repair_held: put stopped out")},
			wantReason: strategy.SkipGEXWait, wantSeq: 1, wantDetail: "snapshot 6m old"},
		{name: "a refused manual close is not why nothing is sold", view: strategy.View{Risk: allowed},
			events: []orders.RecentEvent{idleSkip(1, "no_strike: none"), idleSkip(2, "manual_close_refused: put")}, wantReason: strategy.SkipNoStrike, wantSeq: 1, wantDetail: "none"},
		{name: "kill switch: halted", view: strategy.View{Halted: true, Strangles: held, Risk: allowed}, wantReason: api.IdleHalted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := api.IdleOf(c.view, c.events)
			if c.wantNil {
				if got != nil {
					t.Fatalf("idle %+v, want none", got)
				}
				return
			}
			if got == nil {
				t.Fatal("no idle note")
			}
			if got.Reason != c.wantReason || got.Seq != c.wantSeq || got.Detail != c.wantDetail {
				t.Errorf("idle %+v, want reason %q seq %d detail %q", got, c.wantReason, c.wantSeq, c.wantDetail)
			}
			if got.Seq > 0 && (got.Plain == "" || strings.HasPrefix(got.Plain, api.GenericPlain) || got.At == nil) {
				t.Errorf("a journal line's note needs its sentence and time: %+v", got)
			}
		})
	}
}

// Scenario: a bot with no open strangle whose latest decision is a freeze
// skip — /api/summary names it, with the journal line to explain.
func TestAPIIdle_SummaryCarriesTheFreeze(t *testing.T) {
	journal := orders.NewWriterLogger(io.Discard, 0.05)
	journal.LogSkipped("risk_frozen: DVOL band 2 → 1", orders.EventContext{})
	v := strategy.View{Underlying: "BTC", Risk: strategy.RiskView{Status: risk.Status{Frozen: true}}}
	code, body := getJSON(t, api.New(staticView{v}, journal).Handler(), "/api/summary")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	idle, ok := body["idle"].(map[string]any)
	if !ok || idle["reason"] != "risk_frozen" || idle["blocked"] != true || idle["held"] != false || idle["seq"].(float64) < 1 || idle["detail"] != "DVOL band 2 → 1" {
		t.Errorf("idle %v", body["idle"])
	}

	// Scenario: open strangles and entries allowed — no idle note.
	v = summaryView(time.Now())
	v.Risk.EntriesAllowed = true
	_, body = getJSON(t, api.New(staticView{v}, journal).Handler(), "/api/summary")
	if body["idle"] != nil {
		t.Errorf("idle %v, want null", body["idle"])
	}
}
