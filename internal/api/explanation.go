package api

import (
	"encoding/json"
	"sort"
	"time"

	"optionsbot/internal/orders"
)

// "Why did the bot do that" (#314): GET /api/explain?seq=<event seq> (or
// ?trade=<trade seq>) answers with the facts the journal recorded about one
// event — its plain sentence, the rule and its numbers, the market at that
// moment and the same instrument's or slot's events around it. It reads only
// the in-memory recent events /api/events serves: an event that has left
// that window is a 404, never a reconstruction. The assistant may explain a
// decision from this and nothing else.

// RelatedWindow is how far before and after an event another event of the
// same instrument or slot counts as related.
const RelatedWindow = 30 * time.Minute

// NotInRecentHistory is the 404 reason for an event no longer in memory.
const NotInRecentHistory = "not in recent history"

// ExplainMarket is the market the journal recorded with an event. Zero
// values mean "not recorded" (as in orders.MarketSnapshot).
type ExplainMarket struct {
	AsOf          time.Time `json:"as_of,omitzero"`
	Spot          float64   `json:"spot"`
	DVOL          float64   `json:"dvol"`
	IVPercentile  float64   `json:"iv_percentile"`
	GEXRegime     string    `json:"gex_regime,omitempty"`
	GammaFlip     float64   `json:"gamma_flip,omitempty"`
	FlipBufferPct float64   `json:"flip_buffer_pct,omitempty"`
	Trend         string    `json:"trend,omitempty"`     // the direction the rules used
	SMATrend      string    `json:"sma_trend,omitempty"` // both direction sources
	Bias          string    `json:"gex_bias,omitempty"`
	Delta         float64   `json:"delta,omitempty"` // the option's delta at the event
	Bid           float64   `json:"bid,omitempty"`
	Ask           float64   `json:"ask,omitempty"`
	Mid           float64   `json:"mid,omitempty"`
}

// Explanation is the answer of /api/explain.
type Explanation struct {
	Event   PlainEvent     `json:"event"`
	Rule    string         `json:"rule"`   // the trigger, close reason, skip reason word or risk change
	Detail  string         `json:"detail"` // the rule and its numbers, as journaled
	Market  *ExplainMarket `json:"market"` // null when the line records no market
	Related []PlainEvent   `json:"related"`
	Source  string         `json:"source"` // always "journal"
}

// factsLine is the part of a journal line the explanation reads.
type factsLine struct {
	Instrument    string                 `json:"instrument"`
	Slot          *orders.SlotRef        `json:"slot"`
	TriggerReason string                 `json:"trigger_reason"`
	CloseReason   string                 `json:"close_reason"`
	SkipReason    string                 `json:"skip_reason"`
	Change        string                 `json:"change"` // risk_limit
	To            string                 `json:"to"`     // trend_changed
	Detail        string                 `json:"detail"`
	Market        *orders.MarketSnapshot `json:"market"`
	Portfolio     *orders.MarketContext  `json:"portfolio"`
	// risk_limit lines carry their market flat.
	DVOL         float64 `json:"dvol"`
	IVPercentile float64 `json:"iv_percentile"`
	Regime       string  `json:"regime"`
}

func factsOf(e orders.RecentEvent) factsLine {
	var l factsLine
	if len(e.Data) > 0 {
		_ = json.Unmarshal(e.Data, &l) // an unreadable line explains with what it has
	}
	return l
}

// BuildExplanation gathers what the journal recorded about event: its plain
// sentence, rule, detail and market, and the events of window (the recent
// events in memory) on the same instrument or slot within ±RelatedWindow,
// oldest first. Pure: it reads only what it is given.
func BuildExplanation(event orders.RecentEvent, window []orders.RecentEvent) Explanation {
	l := factsOf(event)
	plain, unknown := ExplainEvent(event)
	warnUnknown(unknown)
	return Explanation{
		Event:   PlainEvent{RecentEvent: event, Plain: plain},
		Rule:    ruleOf(event.Event, l),
		Detail:  detailOf(event.Event, l),
		Market:  marketOf(event.Event, l),
		Related: plainEvents(relatedTo(event, l, window)),
		Source:  "journal",
	}
}

// ruleOf names the rule behind an event in the journal's own words.
func ruleOf(event string, l factsLine) string {
	switch event {
	case orders.EventClosed:
		return l.CloseReason
	case orders.EventSkipped:
		return skipWord(l.SkipReason)
	case orders.EventRisk:
		return l.Change
	case orders.EventTrend:
		return l.To
	}
	return l.TriggerReason
}

// detailOf is the journaled detail; a skip without one keeps its numbers in
// the skip reason itself ("no_free_expiry: …").
func detailOf(event string, l factsLine) string {
	if l.Detail == "" && event == orders.EventSkipped {
		return l.SkipReason
	}
	return l.Detail
}

func marketOf(event string, l factsLine) *ExplainMarket {
	if event == orders.EventRisk {
		return &ExplainMarket{DVOL: l.DVOL, IVPercentile: l.IVPercentile, GEXRegime: l.Regime}
	}
	if l.Market == nil {
		return nil
	}
	m := l.Market
	out := &ExplainMarket{
		AsOf: m.AsOf, Spot: m.Spot, DVOL: m.DVOL, IVPercentile: m.IVPercentile,
		GEXRegime: m.GEXRegime, GammaFlip: m.GammaFlip, FlipBufferPct: m.FlipBufferPct,
		Delta: m.Delta, Bid: m.Bid, Ask: m.Ask, Mid: m.Mid,
	}
	if p := l.Portfolio; p != nil {
		out.Trend, out.SMATrend, out.Bias = p.Trend, p.SMATrend, p.Bias
	}
	return out
}

// relatedTo is every other event of window on event's instrument or slot
// within ±RelatedWindow, oldest first; none when event names neither.
func relatedTo(event orders.RecentEvent, l factsLine, window []orders.RecentEvent) []orders.RecentEvent {
	out := []orders.RecentEvent{}
	if l.Instrument == "" && l.Slot == nil {
		return out
	}
	for _, e := range window {
		if e.Seq == event.Seq || e.At.Sub(event.At).Abs() > RelatedWindow {
			continue
		}
		o := factsOf(e)
		sameInstrument := l.Instrument != "" && o.Instrument == l.Instrument
		sameSlot := l.Slot != nil && o.Slot != nil && *o.Slot == *l.Slot
		if sameInstrument || sameSlot {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}
