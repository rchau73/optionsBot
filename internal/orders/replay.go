package orders

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"time"
)

// The journal (orders.log) is the bot's history: every decision, fill and
// close is one JSON line, appended and never rewritten. On startup it is
// replayed so what the monitor shows — realised P&L, closes, event counts,
// the activity feed and the trade history — continues across restarts
// instead of starting again from zero.

// Trade is one open (an entry or repair fill) or close (a buy-back with its
// realised P&L) from the journal. Coin amounts are in the underlying.
type Trade struct {
	Seq        uint64    `json:"seq"` // the journal event's sequence number
	At         time.Time `json:"at"`
	Kind       string    `json:"kind"` // open | close
	StrategyID string    `json:"strategy_id,omitempty"`
	Slot       *SlotRef  `json:"slot,omitempty"`
	Instrument string    `json:"instrument"`
	OptionType string    `json:"option_type"`
	Qty        float64   `json:"qty"`
	Price      float64   `json:"price"`
	Reason     string    `json:"reason"`           // entry, or the close reason (stop_loss, roi_target, …)
	Detail     string    `json:"detail,omitempty"` // why, with the numbers
	// Per-option greeks at that moment (long-holder view, as Deribit quotes).
	Delta   float64 `json:"delta"`
	Gamma   float64 `json:"gamma"`
	Theta   float64 `json:"theta"`
	Vega    float64 `json:"vega"`
	Premium float64 `json:"premium"`
	// Closes only.
	PnL      float64 `json:"pnl,omitempty"`
	PnLUSD   float64 `json:"pnl_usd,omitempty"`
	ROIPct   float64 `json:"roi_pct,omitempty"`
	HoldDays int     `json:"hold_days,omitempty"`
	// Market at that moment.
	Spot      float64 `json:"spot"`
	DVOL      float64 `json:"dvol"`
	IVPct     float64 `json:"iv_percentile"`
	Moneyness string  `json:"moneyness,omitempty"`
	GEXRegime string  `json:"gex_regime,omitempty"`
}

// tradeOf turns a filled or closed journal record into a Trade.
func tradeOf(seq uint64, rec OrderLog) (Trade, bool) {
	t := Trade{
		Seq: seq, At: rec.Timestamp, StrategyID: rec.StrategyID, Slot: rec.Slot,
		Instrument: rec.Instrument, OptionType: rec.OptionType, Qty: rec.Qty, Price: rec.FillPrice,
		Premium: rec.PremiumReceived, Spot: rec.Market.Spot, DVOL: rec.Market.DVOL, IVPct: rec.Market.IVPercentile,
		Moneyness: rec.Market.Moneyness, GEXRegime: rec.Market.GEXRegime,
		Detail: rec.Detail, Delta: rec.Delta, Gamma: rec.Gamma, Theta: rec.Theta, Vega: rec.Vega,
	}
	switch rec.Event {
	case EventFilled:
		t.Kind, t.Reason = "open", rec.TriggerReason
	case EventClosed:
		t.Kind, t.Reason = "close", rec.CloseReason
		if t.Reason == "" {
			t.Reason = rec.TriggerReason
		}
		t.PnL, t.PnLUSD, t.ROIPct, t.HoldDays = rec.PnL, rec.PnLUSD, rec.ROIPct, rec.HoldDays
	default:
		return Trade{}, false
	}
	return t, true
}

// Replay is what a journal holds that the bot keeps in memory.
type Replay struct {
	Events   uint64              // journal events read (the next live event is Events+1)
	Counts   map[string]int      // events per type
	Recent   []RecentEvent       // the last keepRecent events, oldest first
	Trades   []Trade             // every open and close, oldest first
	Realised map[SlotRef]float64 // realised P&L per slot (zero SlotRef = unknown slot)
	Closed   map[SlotRef]int     // closes per slot
	FirstAt  time.Time           // time of the first event (zero for an empty journal)
	Skipped  int                 // unreadable lines (e.g. a torn write), ignored
}

// SlotKey normalises a slot for map lookups (delta compared in hundredths),
// so a journal slot and a configured slot land on the same key.
func SlotKey(slot *SlotRef) SlotRef {
	if slot == nil {
		return SlotRef{}
	}
	return SlotRef{DTE: slot.DTE, Delta: math.Round(slot.Delta*100) / 100}
}

var (
	eventKey     = []byte(`"event":"`)
	timestampKey = []byte(`"timestamp":"`)
)

// field reads a top-level string field without decoding the whole line.
func field(line, key []byte) string {
	i := bytes.Index(line, key)
	if i < 0 {
		return ""
	}
	rest := line[i+len(key):]
	j := bytes.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	return string(rest[:j])
}

// ReplayJournal reads journal lines from r. Only fills and closes are fully
// decoded; other lines are counted and kept for the activity feed, so a
// long journal replays quickly.
func ReplayJournal(r io.Reader, keepRecent int) (Replay, error) {
	rp := Replay{Counts: map[string]int{}, Realised: map[SlotRef]float64{}, Closed: map[SlotRef]int{}}
	ring := newRecentRing(keepRecent)
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			rp.add(ring, line)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return rp, fmt.Errorf("read journal: %w", err)
		}
	}
	rp.Recent = ring.since(0, 0)
	return rp, nil
}

// add counts one line. Most lines (submits, skips, P&L ticks) only need
// their event and time, read without decoding; fills and closes — the P&L —
// are decoded in full, and a torn one is skipped rather than half-counted.
func (rp *Replay) add(ring *recentRing, line []byte) {
	event := field(line, eventKey)
	if event == "" || line[0] != '{' || line[len(line)-1] != '}' {
		rp.Skipped++
		return
	}
	var rec OrderLog
	trade := event == EventFilled || event == EventClosed
	if trade {
		if err := json.Unmarshal(line, &rec); err != nil {
			rp.Skipped++
			return
		}
	}
	at, _ := time.Parse(time.RFC3339Nano, field(line, timestampKey))
	rp.Events++
	rp.Counts[event]++
	if rp.FirstAt.IsZero() && !at.IsZero() {
		rp.FirstAt = at
	}
	ring.addAt(event, line, rp.Events, at)
	if !trade {
		return
	}
	if t, ok := tradeOf(rp.Events, rec); ok {
		rp.Trades = append(rp.Trades, t)
	}
	if event == EventClosed {
		k := SlotKey(rec.Slot)
		rp.Realised[k] += rec.PnL
		rp.Closed[k]++
	}
}

// ReplayFile replays the journal at path; a missing file is an empty history.
func ReplayFile(path string, keepRecent int) (Replay, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Replay{Counts: map[string]int{}, Realised: map[SlotRef]float64{}, Closed: map[SlotRef]int{}}, nil
	}
	if err != nil {
		return Replay{}, fmt.Errorf("open journal %s: %w", path, err)
	}
	defer f.Close()
	rp, err := ReplayJournal(f, keepRecent)
	if rp.Skipped > 0 {
		slog.Warn("journal replay: skipped unreadable lines", "path", path, "lines", rp.Skipped)
	}
	return rp, err
}
