package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// GET /api/journal?days=N&held=0|1 (#417): the journal's decisions of the
// last N days, read from orders.log (never the in-memory ring, which holds
// only the last RecentEvents), without the periodic pnl lines
// (/api/pnl/history serves them), each with its plain-words sentence. A
// notice or an attempt the bot repeats — the same event, reason, instrument
// or slot and side every cycle, at another time and with drifting numbers —
// is one item with its count and first and last time; what changes the book
// (an open, a close, a fill, a person's action) is always its own item, and a
// fill or a close carries the market its journal line recorded (#427). With
// held=1 the answer also holds every older event of each instrument the bot
// holds now, however old (#427: a leg opened 40 days ago still has its
// entry). The assistant reads it.

// JournalSource reads orders.log back from its end (orders.Logger).
type JournalSource interface {
	JournalBack(from time.Time, fn func(e orders.RecentEvent) bool) error
}

const (
	defaultJournalDays = 7
	maxJournalDays     = 7
	// MaxJournalItems bounds one /api/journal answer after collapsing (and,
	// apart, the held instruments' older history); the newest items are kept
	// and the answer says truncated.
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
	// Market is what the journal line of a fill or a close recorded (#427);
	// nil for other events and for a line without a market.
	Market *JournalMarket `json:"market,omitempty"`
}

// JournalMarket is the market a fill or a close was journaled with, as the
// line recorded it: nothing recomputed.
type JournalMarket struct {
	Spot         float64 `json:"spot"`          // index, USD
	DVOL         float64 `json:"dvol"`          // %
	IVPercentile float64 `json:"iv_percentile"` // 0–100
	GEXRegime    string  `json:"gex_regime,omitempty"`
}

// JournalWindow is the /api/journal answer.
type JournalWindow struct {
	AsOf      time.Time     `json:"as_of"`
	Days      int           `json:"days"`
	From      time.Time     `json:"from"`
	Events    int           `json:"events"` // decisions read (pnl lines not counted)
	Items     []JournalItem `json:"items"`  // oldest first, by last occurrence
	Truncated bool          `json:"truncated"`
	// Held lists the instruments held now whose older events are in Items
	// too (held=1); empty otherwise.
	Held []string `json:"held,omitempty"`
}

// journalFacts is the part of a line the collapse reads beyond journalLine.
type journalFacts struct {
	journalLine
	Slot   *orders.SlotRef        `json:"slot"`
	Market *orders.MarketSnapshot `json:"market"`
}

// tradeMarket is the market a fill or a close recorded; nil for other events.
func tradeMarket(event string, l journalFacts) *JournalMarket {
	if (event != orders.EventFilled && event != orders.EventClosed) || l.Market == nil || l.Market.Spot <= 0 {
		return nil
	}
	m := l.Market
	return &JournalMarket{Spot: m.Spot, DVOL: m.DVOL, IVPercentile: m.IVPercentile, GEXRegime: m.GEXRegime}
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
	// only, when set, keeps the events of these instruments older than
	// before, and nothing else (the held history, #427).
	only   map[string]bool
	before time.Time
}

// skips says whether the held-history pass passes over a line without
// decoding it: one inside the window (the window's pass reads it), or one
// that does not name a held instrument.
func (c *journalCollector) skips(e orders.RecentEvent) bool {
	if c.only == nil {
		return false
	}
	if !e.At.Before(c.before) {
		return true
	}
	for name := range c.only {
		if bytes.Contains(e.Data, []byte(name)) {
			return false
		}
	}
	return true
}

// add takes one event; false once the bound is reached (read no further).
func (c *journalCollector) add(e orders.RecentEvent) bool {
	if e.Event == orders.EventPnL || c.skips(e) {
		return true
	}
	var l journalFacts
	if len(e.Data) > 0 {
		_ = json.Unmarshal(e.Data, &l) // an odd line still gets the generic sentence below
	}
	if c.only != nil && !c.only[l.Instrument] {
		return true // the name was in another field (a detail)
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
		Count: 1, FirstSeq: e.Seq, First: e.At, Last: e.At, Market: tradeMarket(e.Event, l),
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

// ReadHeldJournal collapses every event of the held instruments older than
// from, however old (#427): the whole journal is read back, and a line is
// decoded only when it names one of them. The same collapse and bound as
// ReadJournal, apart from the window's items.
func ReadHeldJournal(src JournalSource, held []string, from time.Time) (items []JournalItem, events int, truncated bool, err error) {
	if len(held) == 0 {
		return nil, 0, false, nil
	}
	only := make(map[string]bool, len(held))
	for _, name := range held {
		only[name] = true
	}
	c := &journalCollector{byKey: map[string]int{}, only: only, before: from}
	if err := src.JournalBack(time.Time{}, c.add); err != nil {
		return nil, 0, false, err
	}
	return c.oldestFirst(), c.events, c.truncated, nil
}

// heldInstruments are the instruments of the legs the bot holds now, sorted.
func heldInstruments(v strategy.View) []string {
	seen := map[string]bool{}
	var out []string
	for _, st := range v.Strangles {
		for _, leg := range st.Legs {
			if leg.Instrument != "" && !seen[leg.Instrument] {
				seen[leg.Instrument] = true
				out = append(out, leg.Instrument)
			}
		}
	}
	sort.Strings(out)
	return out
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
	withHeld := false
	switch r.URL.Query().Get("held") {
	case "", "0":
	case "1":
		withHeld = true
	default:
		writeError(w, http.StatusBadRequest, "held must be 0 or 1")
		return
	}
	src, ok := s.events.(JournalSource)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "journal not available")
		return
	}
	now := time.Now()
	from := now.Add(-time.Duration(days) * 24 * time.Hour)
	items, events, truncated, err := ReadJournal(src, from)
	var held []string
	if err == nil && withHeld {
		held = heldInstruments(s.strategy.View())
		var older []JournalItem
		var n int
		var cut bool
		older, n, cut, err = ReadHeldJournal(src, held, from)
		items, events, truncated = append(older, items...), events+n, truncated || cut
	}
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, orders.ErrNoJournalFile) {
			code = http.StatusServiceUnavailable
		}
		slog.Warn("monitor API: journal read failed", "days", days, "held", withHeld, "err", err)
		writeError(w, code, "journal not available")
		return
	}
	if truncated {
		slog.Warn("monitor API: journal answer truncated", "days", days, "held", len(held), "items", len(items), "max", MaxJournalItems)
	}
	writeJSON(w, JournalWindow{AsOf: now, Days: days, From: from, Events: events, Items: items, Truncated: truncated, Held: held})
}
