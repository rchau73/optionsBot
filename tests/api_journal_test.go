package tests

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/orders"
)

// GET /api/journal (#417): the journal's last days read from orders.log,
// without pnl lines, repeats collapsed, trades kept, bounded.

// journalFixture writes lines to a temp orders.log and opens it as main does:
// replay, logger, restore — so the ring's seqs and the file's agree.
func journalFixture(t *testing.T, lines []string) (*orders.Logger, http.Handler) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "orders.log")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rp, err := orders.ReplayFile(path, orders.RecentEvents)
	if err != nil {
		t.Fatal(err)
	}
	logger, err := orders.NewLogger(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logger.Close() })
	logger.Restore(rp)
	return logger, api.New(staticView{sampleView()}, logger).Handler()
}

func getJournal(t *testing.T, h http.Handler, path string) api.JournalWindow {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s → %d %s", path, rec.Code, rec.Body.String())
	}
	var w api.JournalWindow
	if err := json.Unmarshal(rec.Body.Bytes(), &w); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return w
}

func jline(at time.Time, event, rest string) string {
	if rest != "" {
		rest = "," + rest
	}
	return fmt.Sprintf(`{"timestamp":%q,"event":%q%s}`, at.UTC().Format(time.RFC3339Nano), event, rest)
}

func TestAPI_Journal(t *testing.T) {
	now := time.Now()
	day := 24 * time.Hour
	put := `"instrument":"BTC-27NOV26-60000-P","option_type":"put"`
	var lines []string
	add := func(at time.Time, event, rest string) { lines = append(lines, jline(at, event, rest)) }

	add(now.Add(-9*day), orders.EventClosed, put+`,"close_reason":"roi_target","qty":1`) // outside 7 days
	add(now.Add(-6*day), orders.EventFilled, put+`,"trigger_reason":"entry","direction":"sell","qty":1,"fill_price":0.02`)
	lines = append(lines, `{"timestamp":"torn`) // a torn write: skipped and not numbered
	add(now.Add(-6*day+30*time.Minute), orders.EventSkipped, put+`,"skip_reason":"repair_held: put stopped out, cooldown 72h"`)
	add(now.Add(-6*day+time.Hour), orders.EventClosed, put+`,"close_reason":"stop_loss","direction":"buy","qty":1,"fill_price":0.04`)
	// A repair attempted three times at drifting prices, re-priced and cancelled each time.
	for i := 0; i < 3; i++ {
		at := now.Add(-5*day + time.Duration(i)*time.Hour)
		add(at, orders.EventSubmitted, put+fmt.Sprintf(`,"trigger_reason":"repair","direction":"sell","qty":1,"limit_price":0.0%d`, i+1))
		add(at.Add(time.Minute), orders.EventAmended, put+fmt.Sprintf(`,"trigger_reason":"repair","direction":"sell","limit_price":0.0%d,"previous_price":0.05`, i+1))
		add(at.Add(2*time.Minute), orders.EventCancelled, put+`,"trigger_reason":"repair","direction":"sell","filled_qty":0`)
	}
	// Two identical fills: each changes the position, both kept.
	for i := 0; i < 2; i++ {
		add(now.Add(-3*day+time.Duration(i)*time.Minute), orders.EventFilled, put+`,"trigger_reason":"repair","direction":"sell","qty":0.5,"fill_price":0.03`)
	}
	add(now.Add(-2*day), orders.EventClosed, put+`,"close_reason":"manual_close","direction":"buy","qty":1,"actor":{"user":"admin1","role":"admin"}`)
	// 300 identical gex_wait skips in a day, P&L lines among them.
	gexFirst := now.Add(-day)
	for i := 0; i < 300; i++ {
		at := gexFirst.Add(time.Duration(i) * 4 * time.Minute)
		add(at, orders.EventSkipped, fmt.Sprintf(`"skip_reason":"gex_wait: snapshot %ds old","slot":{"dte":25,"delta":0.16}`, 300+i))
		add(at.Add(time.Second), orders.EventPnL, `"realised_pnl":0.1`)
	}
	gexLast := gexFirst.Add(299 * 4 * time.Minute)

	logger, h := journalFixture(t, lines)
	logger.LogSkipped("no_expiry", orders.EventContext{}) // a live line after the restore

	w := getJournal(t, h, "/api/journal")
	if w.Days != 7 || w.Truncated {
		t.Errorf("days %d truncated %v, want 7 false", w.Days, w.Truncated)
	}
	count := map[string]int{}
	var gex, stop, ownClose *api.JournalItem
	for i := range w.Items {
		it := &w.Items[i]
		count[it.Event]++
		switch {
		case it.Event == orders.EventPnL:
			t.Errorf("a pnl line was read: %+v", it)
		case it.Reason == "roi_target":
			t.Errorf("a close 9 days old is in the 7-day window")
		case it.Reason == "gex_wait":
			gex = it
		case it.Reason == "stop_loss":
			stop = it
		case it.Reason == "manual_close":
			ownClose = it
		}
		if it.Plain == "" {
			t.Errorf("item without plain: %+v", it)
		}
	}

	t.Run("Scenario: 300 identical gex_wait skips are one item with count, first and last", func(t *testing.T) {
		if gex == nil || gex.Count != 300 {
			t.Fatalf("gex_wait item = %+v, want count 300", gex)
		}
		if gex.First.Sub(gexFirst).Abs() > time.Millisecond {
			t.Errorf("first %v, want %v", gex.First, gexFirst)
		}
		if gex.Last.Sub(gexLast).Abs() > time.Millisecond || gex.Seq <= gex.FirstSeq {
			t.Errorf("last %v seq %d first_seq %d, want last %v", gex.Last, gex.Seq, gex.FirstSeq, gexLast)
		}
		if gex.Slot == nil || gex.Slot.DTE != 25 {
			t.Errorf("slot = %+v", gex.Slot)
		}
	})

	t.Run("a repair attempted three times: one item per event kind", func(t *testing.T) {
		for _, ev := range []string{orders.EventSubmitted, orders.EventAmended, orders.EventCancelled} {
			if count[ev] != 1 {
				t.Errorf("%s items = %d, want 1", ev, count[ev])
			}
		}
		for _, it := range w.Items {
			if it.Event == orders.EventSubmitted && (it.Count != 3 || it.Side != "sell" || it.OptionType != "put") {
				t.Errorf("submitted = %+v, want count 3, sell put", it)
			}
		}
	})

	t.Run("trades and manual actions are never collapsed", func(t *testing.T) {
		if count[orders.EventFilled] != 3 || count[orders.EventClosed] != 2 {
			t.Errorf("filled %d closed %d, want 3 and 2", count[orders.EventFilled], count[orders.EventClosed])
		}
		if ownClose == nil || ownClose.Count != 1 {
			t.Errorf("manual close = %+v", ownClose)
		}
	})

	t.Run("Scenario: a close 6 days ago and the decisions around it are there, citable by the ring's seq", func(t *testing.T) {
		if stop == nil {
			t.Fatal("the stop-loss close 6 days ago is missing")
		}
		var ringSeq uint64
		for _, e := range logger.Recent(0, 0) {
			if e.Event == orders.EventClosed && strings.Contains(string(e.Data), "stop_loss") {
				ringSeq = e.Seq
			}
		}
		if stop.Seq != ringSeq {
			t.Errorf("close seq %d, the ring's %d", stop.Seq, ringSeq)
		}
		if count[orders.EventSkipped] != 3 { // repair_held, gex_wait, the live no_expiry
			t.Errorf("skipped items = %d, want 3", count[orders.EventSkipped])
		}
		last := w.Items[len(w.Items)-1]
		if last.Reason != "no_expiry" || last.Seq != logger.Recent(0, 0)[len(logger.Recent(0, 0))-1].Seq {
			t.Errorf("newest item = %+v, want the live no_expiry with its seq", last)
		}
	})

	t.Run("the window", func(t *testing.T) {
		w1 := getJournal(t, h, "/api/journal?days=1")
		for _, it := range w1.Items {
			if it.Reason == "stop_loss" {
				t.Error("days=1 holds a close 6 days old")
			}
		}
		if len(w1.Items) != 2 { // gex_wait and the live skip
			t.Errorf("days=1 → %d items, want 2", len(w1.Items))
		}
		for _, bad := range []string{"?days=0", "?days=8", "?days=x"} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/journal"+bad, nil))
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s → %d, want 400", bad, rec.Code)
			}
		}
	})
}

func TestAPI_JournalBound(t *testing.T) {
	now := time.Now()
	var lines []string
	n := api.MaxJournalItems + 50
	for i := 0; i < n; i++ {
		// Distinct instruments: nothing collapses.
		lines = append(lines, jline(now.Add(-time.Hour+time.Duration(i)*time.Millisecond), orders.EventSkipped,
			fmt.Sprintf(`"instrument":"BTC-27NOV26-%d-P","skip_reason":"liquidity_cap"`, 10000+i)))
	}
	_, h := journalFixture(t, lines)
	w := getJournal(t, h, "/api/journal")
	if !w.Truncated || len(w.Items) != api.MaxJournalItems {
		t.Fatalf("truncated %v items %d, want true %d", w.Truncated, len(w.Items), api.MaxJournalItems)
	}
	if last := w.Items[len(w.Items)-1]; last.Seq != uint64(n) {
		t.Errorf("newest item seq %d, want %d (the newest are kept)", last.Seq, n)
	}
}

func TestAPI_JournalWithoutFile(t *testing.T) {
	h := api.New(staticView{sampleView()}, orders.NewWriterLogger(io.Discard, 0)).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/journal", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no journal file → %d, want 503", rec.Code)
	}
}
