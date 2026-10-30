package tests

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/orders"
	"optionsbot/internal/risk"
	"optionsbot/internal/strategy"
)

// journalConsts reads the string constants of the Go files in dir whose name
// starts with prefix (e.g. "Skip" in internal/strategy), name → value: the
// catalogue test follows the code, so a new journal value fails CI until it
// has a sentence.
func journalConsts(t *testing.T, dir, prefix string) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no Go files in %s: %v", dir, err)
	}
	out := map[string]string{}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if !strings.HasPrefix(name.Name, prefix) || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					v, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("%s: %v", name.Name, err)
					}
					out[name.Name] = v
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("no %s* constants in %s", prefix, dir)
	}
	return out
}

func event(name string, data map[string]any) orders.RecentEvent {
	raw, _ := json.Marshal(data)
	return orders.RecentEvent{Event: name, Data: raw}
}

// eventSamples is one representative line per journal event; a new event
// constant needs a sample here (and a sentence in internal/api/explain.go).
var eventSamples = map[string]map[string]any{
	orders.EventSubmitted:  {"trigger_reason": orders.TriggerEntry, "direction": "sell", "option_type": "put", "instrument": "BTC-27NOV26-60000-P", "qty": 1.5},
	orders.EventAmended:    {"trigger_reason": orders.TriggerEntry, "direction": "sell", "option_type": "put", "limit_price": 0.01, "previous_price": 0.012},
	orders.EventCancelled:  {"trigger_reason": orders.TriggerTimeout, "direction": "sell", "option_type": "call", "qty": 2, "filled_qty": 0.5},
	orders.EventFilled:     {"trigger_reason": orders.TriggerEntry, "option_type": "call"},
	orders.EventClosed:     {"close_reason": "stop_loss", "option_type": "put"},
	orders.EventReconciled: {"trigger_reason": orders.TriggerReconciled, "option_type": "put", "instrument": "BTC-27NOV26-60000-P"},
	orders.EventSkipped:    {"skip_reason": "no_expiry"},
	orders.EventPnL:        {"realised": 0.01},
	orders.EventRisk:       {"change": orders.RiskFrozen},
	orders.EventTrend:      {"from": "bull", "to": "bear", "reason": "price < swing low"},
	orders.EventConfig:     {"fingerprint": "0123456789ab", "changes": []map[string]any{{"key": "repair_cooldown_hours", "from": 72, "to": 48}}},
	orders.EventKeyScope:   {"scopes": []string{"account:read", "trade:read_write"}, "can_move_funds": false, "status": "trade_only"},
}

// openTriggers and nonCloseTriggers sort the trigger constants: every other
// trigger is a close and needs a close sentence.
var (
	openTriggers     = []string{orders.TriggerEntry, orders.TriggerRepair, orders.TriggerRebalanceUpsize}
	nonCloseTriggers = map[string]bool{
		orders.TriggerEntry: true, orders.TriggerRepair: true, orders.TriggerRebalanceUpsize: true,
		orders.TriggerCloseLong: true, orders.TriggerReconciled: true, orders.TriggerTimeout: true, orders.TriggerRiskFrozen: true,
		orders.TriggerRegimeSide: true,
	}
)

// catalogueSentences explains every journal value the code writes and
// returns each sentence by the case it came from, failing on a generic one.
func catalogueSentences(t *testing.T) map[string]string {
	t.Helper()
	got := map[string]string{}
	known := func(what, plain, unknown string) {
		t.Helper()
		if unknown != "" || strings.HasPrefix(plain, api.GenericPlain) || plain == "" {
			t.Errorf("%s: no plain sentence (unknown %q): %q", what, unknown, plain)
		}
		got[what] = plain
	}
	explain := func(what string, e orders.RecentEvent) {
		plain, unknown := api.ExplainEvent(e)
		known(what, plain, unknown)
	}

	for name, ev := range journalConsts(t, "../internal/orders", "Event") {
		sample, ok := eventSamples[ev]
		if !ok {
			t.Errorf("%s (%q): no sample in eventSamples — add one and its sentence", name, ev)
			continue
		}
		explain("event "+ev, event(ev, sample))
	}
	triggers := journalConsts(t, "../internal/orders", "Trigger")
	for _, tr := range triggers {
		for _, dir := range []string{"sell", "buy"} {
			explain("submitted "+dir+" "+tr, event(orders.EventSubmitted, map[string]any{"trigger_reason": tr, "direction": dir, "option_type": "put"}))
			explain("amended "+dir+" "+tr, event(orders.EventAmended, map[string]any{"trigger_reason": tr, "direction": dir, "option_type": "put"}))
			explain("cancelled "+dir+" "+tr, event(orders.EventCancelled, map[string]any{"trigger_reason": tr, "direction": dir, "option_type": "put"}))
		}
		if !nonCloseTriggers[tr] {
			label := orders.CloseReasonLabel(tr)
			explain("closed "+label, event(orders.EventClosed, map[string]any{"close_reason": label, "option_type": "call"}))
			plain, unknown := api.ExplainTrade(orders.Trade{Kind: "close", OptionType: "call", Reason: label})
			known("trade close "+label, plain, unknown)
		}
	}
	for _, tr := range openTriggers {
		explain("filled "+tr, event(orders.EventFilled, map[string]any{"trigger_reason": tr, "option_type": "put"}))
		plain, unknown := api.ExplainTrade(orders.Trade{Kind: "open", OptionType: "put", Reason: tr})
		known("trade open "+tr, plain, unknown)
	}
	for _, ch := range journalConsts(t, "../internal/orders", "Risk") {
		explain("risk "+ch, event(orders.EventRisk, map[string]any{"change": ch, "limit_im_pct": 20, "im_pct": 24}))
	}
	for _, sk := range journalConsts(t, "../internal/strategy", "Skip") {
		explain("skip "+sk, event(orders.EventSkipped, map[string]any{"skip_reason": sk + ": the numbers"}))
		explain("skip "+sk+" on an instrument", event(orders.EventSkipped, map[string]any{"skip_reason": sk + " BTC-27NOV26-60000-P: the numbers"}))
		explain("skip "+sk+" alone", event(orders.EventSkipped, map[string]any{"skip_reason": sk}))
	}
	for _, from := range []string{"bull", "bear", "neutral"} {
		explain("trend from "+from, event(orders.EventTrend, map[string]any{"from": from, "to": "neutral"}))
	}
	return got
}

// Scenario: every journal action has a sentence.
func TestAPIExplain_EveryJournalActionHasASentence(t *testing.T) {
	if n := len(catalogueSentences(t)); n < 100 {
		t.Errorf("only %d cases explained", n)
	}
}

// A skip journaled with a literal reason would bypass the Skip* constants the
// catalogue test reads: every LogSkipped call must start from a constant.
func TestAPIExplain_SkipReasonsAreConstants(t *testing.T) {
	literal := regexp.MustCompile(`LogSkipped\("`)
	files, _ := filepath.Glob("../internal/strategy/*.go")
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if literal.Match(src) {
			t.Errorf("%s: LogSkipped with a literal reason; use a Skip* constant", f)
		}
	}
}

// Scenario: a stop-loss close is explained.
func TestAPIExplain_StopLossCloseIsExplained(t *testing.T) {
	logger := orders.NewWriterLogger(io.Discard, 0)
	pos := &orders.Position{Instrument: "BTC-27NOV26-60000-P", OptionType: "put", Qty: 1, EntryPrice: 0.01,
		PremiumReceived: 0.01, EntryTime: time.Now().Add(-48 * time.Hour)}
	logger.LogClose(pos, orders.Fill{OrderID: "o1", FillPrice: 0.03, Qty: 1}, orders.TriggerStopLoss200Pct, orders.TypeMarket,
		orders.EventContext{Detail: "stop-loss: loss 2.05× the premium ≥ 2.00×"})
	h := api.New(staticView{sampleView()}, logger).Handler()

	want := []string{"Bought back the put", "to limit the loss", "will not be sold again on that side until its cooldown ends"}
	code, body := getJSON(t, h, "/api/events")
	if code != http.StatusOK {
		t.Fatalf("/api/events → %d", code)
	}
	ev := body["events"].([]any)[0].(map[string]any)
	plain, _ := ev["plain"].(string)
	for _, w := range want {
		if !strings.Contains(plain, w) {
			t.Errorf("events plain %q lacks %q", plain, w)
		}
	}
	if ev["event"] != orders.EventClosed || ev["data"].(map[string]any)["close_reason"] != "stop_loss" {
		t.Errorf("existing fields changed: %v", ev)
	}

	_, body = getJSON(t, h, "/api/trades")
	tr := body["trades"].([]any)[0].(map[string]any)
	if tr["plain"] != plain || tr["reason"] != "stop_loss" || tr["instrument"] != "BTC-27NOV26-60000-P" {
		t.Errorf("trade = %v, want plain %q and its fields", tr, plain)
	}
}

// fixedEvents serves a fixed list of journal events.
type fixedEvents []orders.RecentEvent

func (f fixedEvents) Recent(uint64, int) []orders.RecentEvent { return f }
func (f fixedEvents) EventCounts() map[string]int             { return map[string]int{} }

// Scenario: unknown action.
func TestAPIExplain_UnknownAction(t *testing.T) {
	src := fixedEvents{
		{Seq: 1, Event: "brand_new_action", At: time.Now(), Data: json.RawMessage(`{"event":"brand_new_action"}`)},
		{Seq: 2, Event: orders.EventSkipped, At: time.Now(), Data: json.RawMessage(`{"skip_reason":"brand_new_skip: x"}`)},
		{Seq: 3, Event: orders.EventRisk, At: time.Now(), Data: json.RawMessage(`{"change":"brand_new_change"}`)},
		{Seq: 4, Event: orders.EventSubmitted, At: time.Now(), Data: json.RawMessage(`{"trigger_reason":"brand_new_trigger"}`)},
	}
	h := api.New(staticView{sampleView()}, src).Handler()
	for range 2 { // a second read logs no new WARN and still answers
		code, body := getJSON(t, h, "/api/events")
		if code != http.StatusOK {
			t.Fatalf("/api/events → %d, want 200", code)
		}
		events := body["events"].([]any)
		if len(events) != len(src) {
			t.Fatalf("%d events, want %d", len(events), len(src))
		}
		for _, e := range events {
			m := e.(map[string]any)
			if p, _ := m["plain"].(string); !strings.HasPrefix(p, api.GenericPlain) {
				t.Errorf("seq %v: plain %q, want the generic sentence", m["seq"], p)
			}
		}
	}
	if plain, unknown := api.ExplainTrade(orders.Trade{Kind: "close", OptionType: "call", Reason: "something_new"}); unknown == "" ||
		plain != "Bought back the call (something_new)" {
		t.Errorf("unknown close → %q (%q)", plain, unknown)
	}
}

// Scenario: no advice.
func TestAPIExplain_NoAdvice(t *testing.T) {
	banned := []string{"should", "recommend", "advise", "suggest", "expect", "forecast", "predict", "guarantee",
		"will earn", "will make", "will gain", "will return", "consider", "you ", "your ", "best time", "opportunit"}
	sentences := catalogueSentences(t)
	keys := make([]string, 0, len(sentences))
	for k := range sentences {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		low := strings.ToLower(sentences[k])
		for _, b := range banned {
			if strings.Contains(low, b) {
				t.Errorf("%s: %q contains %q", k, sentences[k], b)
			}
		}
	}
}

func TestAPIExplain_SentencesCarryTheirNumbers(t *testing.T) {
	cases := []struct {
		e    orders.RecentEvent
		want string
	}{
		{event(orders.EventSubmitted, map[string]any{"trigger_reason": orders.TriggerEntry, "direction": "sell", "option_type": "put",
			"instrument": "BTC-27NOV26-60000-P", "qty": 1.5}), "Sent an order to sell 1.5 of the put BTC-27NOV26-60000-P to open a new strangle"},
		{event(orders.EventSubmitted, map[string]any{"trigger_reason": orders.TriggerRolloutROI, "direction": "buy", "option_type": "call"}),
			"the rest is bought at the asking price"},
		{event(orders.EventAmended, map[string]any{"trigger_reason": orders.TriggerRepair, "direction": "sell", "option_type": "put",
			"limit_price": 0.01, "previous_price": 0.012}), "Lowered the price of the order to sell the put from 0.012 to 0.01"},
		{event(orders.EventCancelled, map[string]any{"trigger_reason": orders.TriggerTimeout, "direction": "sell", "option_type": "call",
			"qty": 2, "filled_qty": 0.5}), "0.5 of 2 had filled and is kept"},
		{event(orders.EventCancelled, map[string]any{"trigger_reason": orders.TriggerRiskFrozen, "direction": "sell", "option_type": "call"}),
			"new risk was paused"},
		{event(orders.EventClosed, map[string]any{"close_reason": "dte_rollout", "option_type": "call", "detail": "time roll: 3 days to expiry ≤ 3"}),
			"only 3 days to expiry left"},
		{event(orders.EventRisk, map[string]any{"change": orders.RiskIMOverLimit, "im_pct": 24.2, "limit_im_pct": 20}),
			"Margin in use (24.2 %) is above its limit (20.0 %)"},
		{event(orders.EventRisk, map[string]any{"change": orders.RiskRebalanceManual, "actor": map[string]any{"user": "alice", "role": "admin"}}),
			"alice asked for a rebalance"},
		{event(orders.EventTrend, map[string]any{"from": "bull", "to": "bear", "reason": "price < swing low"}),
			"The price trend changed from rising to falling: the price fell below its last low"},
		{event(orders.EventSkipped, map[string]any{"skip_reason": "stop_deferred BTC-27NOV26-60000-P: ask 30% over mid"}),
			"A stop-loss waits a few minutes"},
	}
	for _, c := range cases {
		plain, unknown := api.ExplainEvent(c.e)
		if unknown != "" || !strings.Contains(plain, c.want) {
			t.Errorf("%s: %q (unknown %q), want %q", c.e.Data, plain, unknown, c.want)
		}
	}
}

// A repair_held line is explained by the hold it names: the details are built
// with the strategy's own reason functions, as repairGate and complementHold
// journal them, so a reworded reason fails here.
func TestAPIExplain_RepairHeldNamesItsHold(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	calm := risk.Status{}
	negative := risk.Status{RegimeUsed: true, RegimeNegative: true}
	anchor := &strategy.ShedAnchor{At: now.Add(-time.Hour), Flip: 70000, BufferPct: 2.5}
	stopped := "put stopped out, " + strategy.RepairBlockReason(now.Add(-time.Hour), now, 72*time.Hour, calm)
	stoppedNeg := "call stopped out, " + strategy.RepairBlockReason(now.Add(-100*time.Hour), now, 72*time.Hour, negative)
	shed := "put " + strategy.ShedRepairBlockReason(anchor, now, time.Time{}, 2, 24*time.Hour)
	biasShed := "call " + strategy.BiasRepairBlockReason(anchor, "call", now, 4*time.Hour, 2)
	regimeSide := "put " + strategy.RegimeSideBlockReason("put", -1, negative)

	cases := []struct {
		name, detail, want, notWant string
	}{
		{"stopped", stopped, "closed by a stop-loss", "GEX"},
		{"stopped under a negative regime", stoppedNeg, "closed by a stop-loss", "GEX"},
		{"GEX shed", shed, "(GEX shed)", "stop-loss"},
		{"GEX shed by bias", biasShed, "(GEX shed)", "stop-loss"},
		{"regime side", regimeSide, "price trend runs toward that side", "stop-loss"},
		{"top-up held", "top-up held: " + stopped, "The rebalance adds nothing to this slot yet", "GEX"},
		{"no detail", "", "a rule still holds that side back", ""},
	}
	for _, c := range cases {
		text := strategy.SkipRepairHeld
		if c.detail != "" {
			text += ": " + c.detail
		}
		plain, unknown := api.ExplainEvent(event(orders.EventSkipped, map[string]any{"skip_reason": text}))
		if unknown != "" || !strings.Contains(plain, c.want) || (c.notWant != "" && strings.Contains(plain, c.notWant)) {
			t.Errorf("%s (%q): %q (unknown %q), want %q without %q", c.name, text, plain, unknown, c.want, c.notWant)
		}
	}
}
