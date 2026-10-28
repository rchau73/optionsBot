package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"optionsbot/internal/orders"
)

// GET /api/journal?days=N (#417): the journal's decisions of the last N days,
// read from orders.log (never the in-memory ring, which holds only the last
// RecentEvents), without the periodic pnl lines (/api/pnl/history serves
// them), each with its plain-words sentence. A notice or an attempt the bot
// repeats — the same event, reason, instrument or slot and side every cycle,
// at another time and with drifting numbers — is one item with its count and
// first and last time; what changes the book (an open, a close, a fill, a
// person's action) is always its own item. The assistant reads it.

// JournalSource reads orders.log back from its end (orders.Logger).
type JournalSource interface {
	JournalBack(from time.Time, fn func(e orders.RecentEvent) bool) error
}

const (
	defaultJournalDays = 7
	maxJournalDays     = 7
	// MaxJournalItems bounds one /api/journal answer after collapsing; the
	// newest items are kept and the answer says truncated.
	MaxJournalItems = 2000
)

// JournalItem is one fact of /api/journal: an event, or a run of the same
// one collapsed (Count > 1). Seq, At and the numbers in Plain are the last
// occurrence's; FirstSeq and First the first's.
type JournalItem struct {
	Seq        uint64          `json:"seq"`
	At         time.Time       `json:"at"`
	Event      string          `json:"event"`
	Reason     string          `json:"reason,omitempty"` // close, skip or trigger reason, or the risk change
	Instrument string          `json:"instrument,omitempty"`
	Slot       *orders.SlotRef `json:"slot,omitempty"`
	OptionType string          `json:"option_type,omitempty"` // call | put
	Side       string          `json:"side,omitempty"`        // buy | sell
	Plain      string          `json:"plain"`
	Count      int             `json:"count"`
	FirstSeq   uint64          `json:"first_seq"`
	First      time.Time       `json:"first"`
	Last       time.Time       `json:"last"`
}

// JournalWindow is the /api/journal answer.
type JournalWindow struct {
	AsOf      time.Time     `json:"as_of"`
	Days      int           `json:"days"`
	From      time.Time     `json:"from"`
	Events    int           `json:"events"` // decisions read (pnl lines not counted)
	Items     []JournalItem `json:"items"`  // oldest first, by last occurrence
	Truncated bool          `json:"truncated"`
}

// journalFacts is the part of a line the collapse reads beyond journalLine.
type journalFacts struct {
	journalLine
	Slot *orders.SlotRef `json:"slot"`
}

// digits are the numbers that only drift between repeats (prices, IM %, sizes).
var digits = regexp.MustCompile(`[0-9]+(?:[.,][0-9]+)*`)

// alwaysOwn says whether an event changes the book or was asked by a person:
// never collapsed with another.
func alwaysOwn(event string, l journalFacts) bool {
	return event == orders.EventFilled || event == orders.EventClosed ||
		l.Actor != nil || l.Change == orders.RiskRebalanceManual || l.FilledQty > 0
}

// reasonOf is the rule word of a line: its close reason, skip word, trigger
// or risk change.
func reasonOf(l journalFacts) string {
	switch {
	case l.CloseReason != "":
		return l.CloseReason
	case l.SkipReason != "":
		return skipWord(l.SkipReason)
	case l.TriggerReason != "":
		return l.TriggerReason
	}
	return l.Change
}

// journalCollector collapses events read newest first into items.
type journalCollector struct {
	items     []JournalItem // newest first while collecting
	byKey     map[string]int
	events    int
	truncated bool
}

// add takes one event; false once the bound is reached (read no further).
func (c *journalCollector) add(e orders.RecentEvent) bool {
	if e.Event == orders.EventPnL {
		return true
	}
	var l journalFacts
	if len(e.Data) > 0 {
		_ = json.Unmarshal(e.Data, &l) // an odd line still gets the generic sentence below
	}
	plain, unknown := ExplainEvent(e)
	warnUnknown(unknown)
	reason := reasonOf(l)
	slot := ""
	if l.Slot != nil {
		slot = fmt.Sprintf("%d/%.2f", l.Slot.DTE, l.Slot.Delta)
	}
	key := ""
	if !alwaysOwn(e.Event, l) {
		// The plain sentence without its numbers tells holds of one skip word
		// apart (repair_held: stopped, GEX shed, regime side, top-up).
		key = fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s", e.Event, reason, l.Instrument, slot, l.OptionType, l.Direction,
			digits.ReplaceAllString(plain, "#"))
		if i, ok := c.byKey[key]; ok {
			it := &c.items[i]
			it.Count++
			it.FirstSeq, it.First = e.Seq, e.At
			c.events++
			return true
		}
	}
	if len(c.items) == MaxJournalItems {
		c.truncated = true
		return false
	}
	c.events++
	if key != "" {
		c.byKey[key] = len(c.items)
	}
	c.items = append(c.items, JournalItem{
		Seq: e.Seq, At: e.At, Event: e.Event, Reason: reason, Instrument: l.Instrument, Slot: l.Slot,
		OptionType: l.OptionType, Side: l.Direction, Plain: plain,
		Count: 1, FirstSeq: e.Seq, First: e.At, Last: e.At,
	})
	return true
}

// oldestFirst returns the items by last occurrence, oldest first.
func (c *journalCollector) oldestFirst() []JournalItem {
	out := make([]JournalItem, len(c.items))
	for i, it := range c.items {
		out[len(c.items)-1-i] = it
	}
	return out
}

// ReadJournal collapses the journal's events since from (#417).
func ReadJournal(src JournalSource, from time.Time) (items []JournalItem, events int, truncated bool, err error) {
	c := &journalCollector{byKey: map[string]int{}}
	if err := src.JournalBack(from, c.add); err != nil {
		return nil, 0, false, err
	}
	return c.oldestFirst(), c.events, c.truncated, nil
}

func (s *Server) journal(w http.ResponseWriter, r *http.Request) {
	days := defaultJournalDays
	if q := r.URL.Query().Get("days"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 1 || n > maxJournalDays {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("days must be 1 to %d", maxJournalDays))
			return
		}
		days = n
	}
	src, ok := s.events.(JournalSource)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "journal not available")
		return
	}
	now := time.Now()
	from := now.Add(-time.Duration(days) * 24 * time.Hour)
	items, events, truncated, err := ReadJournal(src, from)
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, orders.ErrNoJournalFile) {
			code = http.StatusServiceUnavailable
		}
		slog.Warn("monitor API: journal read failed", "days", days, "err", err)
		writeError(w, code, "journal not available")
		return
	}
	if truncated {
		slog.Warn("monitor API: journal answer truncated", "days", days, "items", len(items), "max", MaxJournalItems)
	}
	writeJSON(w, JournalWindow{AsOf: now, Days: days, From: from, Events: events, Items: items, Truncated: truncated})
}
