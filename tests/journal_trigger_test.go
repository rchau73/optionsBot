package tests

import (
	"testing"
	"time"

	"optionsbot/internal/orders"
)

// Repair and rebalance sells go through the entry path but are journaled with
// their own reason, so the journal and the monitor can tell them apart.

// triggers returns the journaled trigger of every event of kind.
func (j *recordingJournal) triggers(event string) []string {
	var out []string
	for _, e := range j.events(event) {
		out = append(out, e.trigger)
	}
	return out
}

func TestJournalTrigger(t *testing.T) {
	cases := []struct {
		name  string
		setup func(f *strategyFixture)
		want  string
	}{
		{"fresh entry", func(f *strategyFixture) {}, orders.TriggerEntry},
		{"repair of a missing leg", func(f *strategyFixture) { f.withLoneCall(0.1) }, orders.TriggerRepair},
		{"rebalance complement", func(f *strategyFixture) {
			f.withOpenStrangle(0.1, 0.02)
			f.market.setIVHistory([]float64{10, 10, 80, 80}, 80) // 50 % band fits 2 lots
		}, orders.TriggerRebalanceUpsize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newStrategyFixture(t)
			tc.setup(f)
			f.exch.onSubmit = func(o orders.Order, _ string) orders.Fill { // everything fills at once
				return orders.Fill{Qty: o.Qty, FillPrice: 0.02}
			}
			f.startRun()

			eventually(t, 2*time.Second, "sell filled", func() bool { return len(f.journal.events(orders.EventFilled)) > 0 })
			for _, ev := range []string{orders.EventSubmitted, orders.EventFilled} {
				for _, got := range f.journal.triggers(ev) {
					if got != tc.want {
						t.Errorf("%s trigger = %q, want %q", ev, got, tc.want)
					}
				}
			}
		})
	}
}
