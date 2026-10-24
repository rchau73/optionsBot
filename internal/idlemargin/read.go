package idlemargin

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"optionsbot/internal/history"
	"optionsbot/internal/orders"
)

// line holds the journal fields the attribution reads; orders.OrderLog and
// orders.RiskRecord lines both decode into it.
type line struct {
	Timestamp     time.Time       `json:"timestamp"`
	Event         string          `json:"event"`
	Slot          *orders.SlotRef `json:"slot"`
	Instrument    string          `json:"instrument"`
	Qty           float64         `json:"qty"`
	FilledQty     float64         `json:"filled_qty"`
	OrderID       string          `json:"order_id"`
	TriggerReason string          `json:"trigger_reason"`
	SkipReason    string          `json:"skip_reason"`
	Change        string          `json:"change"`
}

// wanted are the journal events the attribution uses; the rest (P&L ticks,
// amends, trend changes) are not decoded.
var wanted = map[string]bool{
	orders.EventSkipped: true, orders.EventSubmitted: true, orders.EventCancelled: true,
	orders.EventFilled: true, orders.EventClosed: true, orders.EventReconciled: true, orders.EventRisk: true,
}

// SkipReason is the reason word of a journaled skip ("no_free_expiry: …" →
// "no_free_expiry").
func SkipReason(text string) string {
	reason, _, _ := strings.Cut(text, ":")
	return strings.TrimSpace(reason)
}

// ReadJournal reads the events the attribution needs from a journal
// (orders.log), ordered by time. Unreadable lines (a torn write) are counted
// and skipped, like orders.ReplayJournal does.
func ReadJournal(r io.Reader) (events []Event, skipped int, err error) {
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		raw, rerr := br.ReadBytes('\n')
		raw = bytes.TrimSpace(raw)
		if len(raw) > 0 {
			if e, ok, bad := decode(raw); bad {
				skipped++
			} else if ok {
				events = append(events, e)
			}
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			return events, skipped, fmt.Errorf("read journal: %w", rerr)
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })
	return events, skipped, nil
}

// decode turns one line into an Event; ok is false for an event it does
// not use, bad for a line that does not parse.
func decode(raw []byte) (e Event, ok, bad bool) {
	var head struct {
		Event string `json:"event"`
	}
	// Most lines are P&L ticks: check the event before decoding everything.
	if bytes.Contains(raw, []byte(`"event":"pnl"`)) {
		return Event{}, false, false
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return Event{}, false, true
	}
	if !wanted[head.Event] {
		return Event{}, false, false
	}
	var l line
	if err := json.Unmarshal(raw, &l); err != nil || l.Timestamp.IsZero() {
		return Event{}, false, true
	}
	e = Event{
		At: l.Timestamp, Event: l.Event, Slot: l.Slot, Instrument: l.Instrument, Qty: l.Qty,
		FilledQty: l.FilledQty, OrderID: l.OrderID, Trigger: l.TriggerReason,
	}
	switch l.Event {
	case orders.EventSkipped:
		e.Reason = SkipReason(l.SkipReason)
	case orders.EventRisk:
		e.Reason = l.Change
	}
	return e, true, false
}

// ReadJournalFile reads the journal at path.
func ReadJournalFile(path string) ([]Event, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("open journal %s: %w", path, err)
	}
	defer f.Close()
	return ReadJournal(f)
}

// ReadHistoryFile reads every point of a P&L history file
// (pnl_history.jsonl), ordered by time, without opening it for writing
// (history.Open would). Unreadable lines are counted and skipped.
func ReadHistoryFile(path string) (points []history.Point, skipped int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("open pnl history %s: %w", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var p history.Point
		if err := json.Unmarshal(sc.Bytes(), &p); err != nil || p.T.IsZero() {
			skipped++
			continue
		}
		points = append(points, p)
	}
	if err := sc.Err(); err != nil {
		return points, skipped, fmt.Errorf("read pnl history %s: %w", path, err)
	}
	sort.SliceStable(points, func(i, j int) bool { return points[i].T.Before(points[j].T) })
	return points, skipped, nil
}
