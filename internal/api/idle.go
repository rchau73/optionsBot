package api

import (
	"encoding/json"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// Why the bot is not trading (#781, epic E3): /api/summary's `idle` names the
// decision behind an idle book — nothing held, or new risk blocked — from
// the journal's latest skip or freeze line, so the Overview can say it in one
// sentence with a Why?. Only reads the view and the journal's recent lines;
// it carries no amount and no secret.

const (
	// IdleHalted: the kill switch fired; the bot stays idle until restarted.
	IdleHalted = "halted"
	// IdleFrozen is the reason of a journaled freeze (a risk_limit line with
	// change "frozen"): the same word as the entry's skip.
	IdleFrozen = strategy.SkipRiskFrozen
)

// Idle is why an idle bot is not trading. Reason is a strategy.Skip* word,
// IdleHalted, or empty when no journal line says why (the monitor then
// says "the bot is waiting"). Seq is the journal line's sequence number
// (0 = none), Plain its sentence (ExplainEvent), Detail its numbers.
type Idle struct {
	Reason  string     `json:"reason"`
	Blocked bool       `json:"blocked"` // entries are not allowed this cycle
	Held    bool       `json:"held"`    // the book holds a strangle
	Seq     uint64     `json:"seq"`
	At      *time.Time `json:"at"`
	Plain   string     `json:"plain,omitempty"`
	Detail  string     `json:"detail,omitempty"`
}

// notIdleSkips are skips about closes or the monitor, never why nothing is
// sold: a refused manual close, a shed kept by the hedge, a shed held back.
var notIdleSkips = map[string]bool{
	strategy.SkipManualCloseRefused: true,
	strategy.SkipShedHedged:         true,
	strategy.SkipBiasWarmup:         true,
}

// blockingReasons are the reasons that block every entry (open.go's
// maybeOpenStrangles, before any slot is tried): while entries are not
// allowed only one of them explains it.
var blockingReasons = map[string]bool{
	strategy.SkipRiskFrozen:    true,
	strategy.SkipGEXWait:       true,
	strategy.SkipStopDeferred:  true,
	strategy.SkipMarginLimit:   true,
	strategy.SkipMarginUnknown: true,
}

// IdleOf is the idle note of a bot, nil while it holds a strangle (or
// works an entry) with entries allowed. The reason is the latest skip or
// freeze line journaled after the latest open or close; while entries are
// blocked only a blocking reason counts, and a freeze with no such line
// takes its reason from the view. events: the journal's recent lines,
// oldest first. Pure.
func IdleOf(v strategy.View, events []orders.RecentEvent) *Idle {
	held := len(v.Strangles) > 0
	blocked := !v.Risk.EntriesAllowed
	if v.Halted {
		return &Idle{Reason: IdleHalted, Blocked: true, Held: held}
	}
	if !blocked && (held || len(v.Pending) > 0) {
		return nil
	}
	idle := &Idle{Blocked: blocked, Held: held}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Event == orders.EventFilled || e.Event == orders.EventClosed {
			break // a line before the latest trade no longer explains the book
		}
		reason, detail, ok := idleLine(e)
		if !ok || (blocked && !blockingReasons[reason]) {
			continue
		}
		at := e.At
		plain, _ := ExplainEvent(e)
		idle.Reason, idle.Seq, idle.At, idle.Plain, idle.Detail = reason, e.Seq, &at, plain, detail
		return idle
	}
	if v.Risk.Status.Frozen {
		idle.Reason, idle.Detail = IdleFrozen, v.Risk.Status.FreezeReason
	}
	return idle
}

// idleLine reads a skip or a freeze line: its reason word and detail.
func idleLine(e orders.RecentEvent) (reason, detail string, ok bool) {
	var l journalLine
	if json.Unmarshal(e.Data, &l) != nil {
		return "", "", false
	}
	switch e.Event {
	case orders.EventSkipped:
		word := skipWord(l.SkipReason)
		if word == "" || notIdleSkips[word] {
			return "", "", false
		}
		return word, skipDetail(l), true
	case orders.EventRisk:
		if l.Change == orders.RiskFrozen {
			return IdleFrozen, l.Detail, true
		}
	}
	return "", "", false
}

// skipDetail is a skip's numbers: its detail field, else what follows the
// reason word in skip_reason ("risk_frozen: DVOL band 2 → 1").
func skipDetail(l journalLine) string {
	if l.Detail != "" {
		return l.Detail
	}
	rest := l.SkipReason[len(skipWord(l.SkipReason)):]
	for len(rest) > 0 && (rest[0] == ':' || rest[0] == ' ') {
		rest = rest[1:]
	}
	return rest
}
