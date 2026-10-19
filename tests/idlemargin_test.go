package tests

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/history"
	"optionsbot/internal/idlemargin"
	"optionsbot/internal/orders"
)

var (
	imDay  = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	imSlot = orders.SlotRef{DTE: 30, Delta: 0.15}
	imSlo2 = orders.SlotRef{DTE: 45, Delta: 0.10}
)

const imCall = "BTC-27NOV26-90000-C"
const imPut = "BTC-27NOV26-60000-P"

func at(h, m int) time.Time {
	return imDay.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
}

// imPoints writes a history point every 60 s in [from, to), with entries
// allowed unless allowed says otherwise, IM at im % of a 20 % limit.
func imPoints(from, to time.Time, im float64, allowed func(time.Time) bool) []history.Point {
	var out []history.Point
	for t := from; t.Before(to); t = t.Add(time.Minute) {
		a := allowed == nil || allowed(t)
		imPct, limit := im*0.2, 20.0
		out = append(out, history.Point{T: t, Realised: 123456.789, Total: 98765.4321, IMPct: &imPct, IMLimitPct: &limit, EntriesAllowed: &a})
	}
	return out
}

func skip(t time.Time, slot orders.SlotRef, reason string) idlemargin.Event {
	return idlemargin.Event{At: t, Event: orders.EventSkipped, Slot: &slot, Reason: reason}
}

func fill(t time.Time, slot orders.SlotRef, inst string, qty float64) idlemargin.Event {
	return idlemargin.Event{At: t, Event: orders.EventFilled, Slot: &slot, Instrument: inst, Qty: qty, OrderID: "o-" + inst}
}

func closeEv(t time.Time, slot orders.SlotRef, inst string, qty float64) idlemargin.Event {
	return idlemargin.Event{At: t, Event: orders.EventClosed, Slot: &slot, Instrument: inst, Qty: qty}
}

func slotHours(t *testing.T, r idlemargin.Report, slot orders.SlotRef) map[string]float64 {
	t.Helper()
	for _, s := range r.Slots {
		if s.Slot == slot {
			return s.Hours
		}
	}
	t.Fatalf("slot %+v not in report %+v", slot, r.Slots)
	return nil
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestIdleMargin_Attribute(t *testing.T) {
	allDay := imPoints(at(8, 0), at(16, 0), 50, nil)
	cases := []struct {
		name     string
		events   []idlemargin.Event
		points   []history.Point
		slots    []orders.SlotRef
		from, to time.Time
		want     map[orders.SlotRef]map[string]float64
	}{
		{
			name:   "a slot skipped then opened",
			events: []idlemargin.Event{skip(at(10, 0), imSlot, "no_free_expiry"), fill(at(13, 0), imSlot, imCall, 1), fill(at(13, 0), imSlot, imPut, 1)},
			points: allDay, from: at(9, 0), to: at(15, 0),
			want: map[orders.SlotRef]map[string]float64{imSlot: {idlemargin.RowNoSkip: 1, "no_free_expiry": 3, idlemargin.RowOccupied: 2}},
		},
		{
			name:   "the bot was down",
			events: []idlemargin.Event{skip(at(9, 0), imSlot, "gex_wait")},
			// points stop after 10:45 (that one stands 15 min) and resume at 13:00
			points: append(imPoints(at(8, 0), at(10, 46), 50, nil), imPoints(at(13, 0), at(16, 0), 50, nil)...),
			from:   at(9, 0), to: at(15, 0),
			want: map[orders.SlotRef]map[string]float64{imSlot: {"gex_wait": 4, idlemargin.RowNotMeasured: 2}},
		},
		{
			name:   "frozen time",
			events: []idlemargin.Event{skip(at(8, 30), imSlot, "no_expiry"), fill(at(8, 0), imSlo2, imCall, 1)},
			points: imPoints(at(8, 0), at(16, 0), 50, func(t time.Time) bool { return t.Before(at(10, 0)) || !t.Before(at(14, 0)) }),
			from:   at(9, 0), to: at(15, 0),
			want: map[orders.SlotRef]map[string]float64{
				imSlot: {"no_expiry": 2, idlemargin.RowFrozen: 4},
				imSlo2: {idlemargin.RowOccupied: 6},
			},
		},
		{
			name: "the window cut at both ends",
			events: []idlemargin.Event{
				skip(at(8, 0), imSlot, "margin_limit"), fill(at(10, 0), imSlot, imCall, 2),
				closeEv(at(12, 0), imSlot, imCall, 1), // partial: still occupied
				closeEv(at(14, 0), imSlot, imCall, 1), // last leg: vacant, no skip yet
				skip(at(14, 30), imSlot, "risk_frozen"), fill(at(16, 0), imSlot, imPut, 1),
			},
			points: imPoints(at(7, 0), at(18, 0), 50, nil), from: at(9, 0), to: at(15, 0),
			want: map[orders.SlotRef]map[string]float64{imSlot: {"margin_limit": 1, idlemargin.RowOccupied: 4, idlemargin.RowNoSkip: 0.5, "risk_frozen": 0.5}},
		},
		{
			name:   "an unknown reason kept as its own row",
			events: []idlemargin.Event{skip(at(9, 0), imSlot, "brand_new_reason")},
			points: allDay, from: at(9, 0), to: at(11, 0),
			want: map[orders.SlotRef]map[string]float64{imSlot: {"brand_new_reason": 2}},
		},
		{
			name: "a pending entry occupies until cancelled unfilled",
			events: []idlemargin.Event{
				{At: at(9, 0), Event: orders.EventSubmitted, Slot: &imSlot, OrderID: "e1", Trigger: orders.TriggerEntry},
				{At: at(9, 2), Event: orders.EventCancelled, Slot: &imSlot, OrderID: "e1"},
				{At: at(9, 30), Event: orders.EventSubmitted, Slot: &imSlot, OrderID: "e2", Trigger: orders.TriggerEntry},
				// a cancel with a fill is followed by the fill: the slot stays occupied
				{At: at(9, 31), Event: orders.EventCancelled, Slot: &imSlot, OrderID: "e2", FilledQty: 1},
				{At: at(9, 32), Event: orders.EventFilled, Slot: &imSlot, OrderID: "e2", Instrument: imPut, Qty: 1},
			},
			points: allDay, from: at(9, 0), to: at(11, 0),
			want: map[orders.SlotRef]map[string]float64{imSlot: {idlemargin.RowOccupied: 2.0/60 + 1.5, idlemargin.RowNoSkip: 28.0 / 60}},
		},
		{
			name:   "a pending order the bot never settled ages out",
			events: []idlemargin.Event{{At: at(9, 0), Event: orders.EventSubmitted, Slot: &imSlot, OrderID: "e1", Trigger: orders.TriggerEntry}},
			points: allDay, from: at(9, 0), to: at(11, 0),
			want: map[orders.SlotRef]map[string]float64{imSlot: {idlemargin.RowOccupied: 0.25, idlemargin.RowNoSkip: 1.75}},
		},
		{
			name: "a leg held to settlement frees its slot at 08:00 UTC",
			events: []idlemargin.Event{
				{At: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), Event: orders.EventReconciled, Slot: &imSlot, Instrument: "BTC-15OCT26-60000-P", Qty: 1},
			},
			points: allDay, from: at(7, 0), to: at(10, 0),
			want: map[orders.SlotRef]map[string]float64{imSlot: {idlemargin.RowNotMeasured: 1, idlemargin.RowOccupied: 0, idlemargin.RowNoSkip: 2}},
		},
		{
			name:   "a close journaled on another slot takes the leg off the slot that holds it",
			events: []idlemargin.Event{fill(at(8, 0), imSlot, imCall, 1), closeEv(at(10, 0), imSlo2, imCall, 1)},
			points: allDay, from: at(9, 0), to: at(11, 0),
			want: map[orders.SlotRef]map[string]float64{imSlot: {idlemargin.RowOccupied: 1, idlemargin.RowNoSkip: 1}, imSlo2: {idlemargin.RowNoSkip: 2}},
		},
		{
			name:   "an entry sent after a liquidity cap is occupied but sized small",
			events: []idlemargin.Event{skip(at(9, 0), imSlot, "liquidity_cap"), fill(at(9, 0), imSlot, imCall, 1)},
			points: allDay, from: at(9, 0), to: at(11, 0),
			want: map[orders.SlotRef]map[string]float64{imSlot: {idlemargin.RowOccupiedCapped: 2}},
		},
		{
			name:   "only the configured slots",
			events: []idlemargin.Event{skip(at(9, 0), imSlot, "no_expiry"), skip(at(9, 0), imSlo2, "no_strike")},
			points: allDay, slots: []orders.SlotRef{imSlo2}, from: at(9, 0), to: at(10, 0),
			want: map[orders.SlotRef]map[string]float64{imSlo2: {"no_strike": 1}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := idlemargin.Attribute(tc.events, tc.points, tc.slots, tc.from, tc.to)
			if len(r.Slots) != len(tc.want) {
				t.Fatalf("slots = %+v, want %d", r.Slots, len(tc.want))
			}
			window := tc.to.Sub(tc.from).Hours()
			for slot, want := range tc.want {
				got := slotHours(t, r, slot)
				var sum float64
				for _, v := range got {
					sum += v
				}
				if !approx(sum, window) {
					t.Errorf("slot %+v: hours sum to %.4f, want the window %.1f (%v)", slot, sum, window, got)
				}
				for row, h := range want {
					if !approx(got[row], h) {
						t.Errorf("slot %+v row %s = %.4f h, want %.4f (all %v)", slot, row, got[row], h, got)
					}
				}
				for row, h := range got {
					if _, ok := want[row]; !ok {
						t.Errorf("slot %+v: unexpected row %s = %.4f h", slot, row, h)
					}
				}
			}
		})
	}
}

func TestIdleMargin_ConsistentWithUtilisation(t *testing.T) {
	// IM at 30 % of the limit while a slot is vacant, 80 % once it is full;
	// frozen for an hour and down for two.
	var points []history.Point
	points = append(points, imPoints(at(8, 0), at(11, 0), 30, nil)...)
	points = append(points, imPoints(at(11, 0), at(12, 0), 30, func(time.Time) bool { return false })...)
	points = append(points, imPoints(at(14, 0), at(18, 0), 80, nil)...)
	events := []idlemargin.Event{
		skip(at(8, 0), imSlot, "no_free_expiry"),
		fill(at(14, 0), imSlot, imCall, 1), fill(at(14, 0), imSlot, imPut, 1),
		{At: at(16, 0), Event: orders.EventRisk, Reason: orders.RiskRebalanceRetry},
		{At: at(17, 0), Event: orders.EventRisk, Reason: orders.RiskRebalance},
	}
	from, to := at(9, 0), at(18, 0)
	r := idlemargin.Attribute(events, points, nil, from, to)
	want := history.Utilisation(points, from, to)
	if r.Utilisation.MeanPct == nil || want.MeanPct == nil || !approx(*r.Utilisation.MeanPct, *want.MeanPct) {
		t.Fatalf("utilisation = %+v, want history.Utilisation %+v", r.Utilisation, want)
	}

	var hours, weighted float64
	for _, name := range idlemargin.BookStates {
		bs := r.Book[name]
		hours += bs.Hours
		if bs.MeanPct != nil {
			weighted += *bs.MeanPct * bs.Hours
		}
	}
	if !approx(hours, want.AllowedHours) || !approx(weighted/hours, *want.MeanPct) {
		t.Errorf("book states: %.4f h at %.4f %%, want %.4f h at %.4f %%", hours, weighted/hours, want.AllowedHours, *want.MeanPct)
	}
	checks := map[string]float64{"slot_empty": 2, "rebalance_waiting": 1, "all_full": 3}
	for name, h := range checks {
		if !approx(r.Book[name].Hours, h) {
			t.Errorf("book %s = %.4f h, want %.1f", name, r.Book[name].Hours, h)
		}
	}
	// unused: empty 2 h × 0.7, waiting 1 h × 0.2, full 3 h × 0.2 → 1.4 / 2.2
	if s := r.Book["slot_empty"].UnusedShare; s == nil || !approx(*s, 1.4/2.2) {
		t.Errorf("slot_empty unused share = %v, want %.4f", s, 1.4/2.2)
	}
	// the last frozen point (11:59) stands until 12:14
	if got := slotHours(t, r, imSlot); !approx(got[idlemargin.RowFrozen], 1+14.0/60) || !approx(got[idlemargin.RowNotMeasured], 2-14.0/60) {
		t.Errorf("slot rows = %v, want 1 h 14 frozen and 1 h 46 not measured", got)
	}
}

func TestIdleMargin_NoAmounts(t *testing.T) {
	points := imPoints(at(8, 0), at(16, 0), 50, nil)
	events := []idlemargin.Event{skip(at(9, 0), imSlot, "no_free_expiry"), fill(at(12, 0), imSlot, imCall, 1.2345)}
	var buf bytes.Buffer
	if err := idlemargin.Format(&buf, "btc", idlemargin.Attribute(events, points, nil, at(9, 0), at(15, 0))); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	t.Log("\n" + out)
	for _, amount := range []string{"123456", "98765", "1.2345", "USD", "BTC-"} {
		if strings.Contains(out, amount) {
			t.Errorf("output holds an amount (%q):\n%s", amount, out)
		}
	}
	for _, want := range []string{"no_free_expiry", "occupied", "not_measured", "slot_empty", "50.0 %"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestIdleMargin_ReadJournal(t *testing.T) {
	journal := strings.Join([]string{
		`{"timestamp":"2026-10-15T10:00:00Z","event":"skipped","status":"skipped","slot":{"dte":30,"delta":0.15},"skip_reason":"no_free_expiry: 27NOV26 held by slot 45/0.10"}`,
		`{"timestamp":"2026-10-15T10:00:30Z","event":"pnl","realised":1.5}`,
		`{"timestamp":"2026-10-15T09:00:00Z","event":"risk_limit","change":"rebalance_retry","detail":"x"}`,
		`{"timestamp":"2026-10-15T10:01:00Z","event":"cancelled","slot":{"dte":30,"delta":0.15},"order_id":"e1","filled_qty":0.5}`,
		`{"timestamp":"2026-10-15T10:02:00Z","event":"filled","slot":{"dte":30,"delta":0.15},"order_id":"e1","trigger_reason":"entry","instrument":"` + imCall + `","qty":0.5}`,
		`{"timestamp":"2026-10-15T10:03:00Z","event":"filled","slot":{"dt`, // torn write
	}, "\n")
	events, skipped, err := idlemargin.ReadJournal(strings.NewReader(journal))
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 || len(events) != 4 {
		t.Fatalf("events = %+v, skipped %d; want 4 events and 1 skipped", events, skipped)
	}
	if events[0].Event != orders.EventRisk || events[0].Reason != orders.RiskRebalanceRetry {
		t.Errorf("first event = %+v, want the risk line (ordered by time)", events[0])
	}
	if e := events[1]; e.Reason != "no_free_expiry" || e.Slot == nil || e.Slot.DTE != 30 {
		t.Errorf("skip = %+v, want reason no_free_expiry on slot 30", e)
	}
	if e := events[2]; e.FilledQty != 0.5 || e.OrderID != "e1" {
		t.Errorf("cancel = %+v", e)
	}
	if e := events[3]; e.Instrument != imCall || e.Qty != 0.5 || e.Trigger != orders.TriggerEntry {
		t.Errorf("fill = %+v", e)
	}
}
