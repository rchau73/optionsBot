package api

import (
	"sort"
	"strings"

	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// CatalogueEntry is one sentence of the plain-words catalogue (explain.go) as
// the help index (#312, internal/helpkb) cites it: Key names the journal value
// ("closed:stop_loss", "skipped:gex_wait"), Text is the sentence the API
// serves for it.
type CatalogueEntry struct {
	Key  string
	Text string
}

// placeholderFigure stands for a figure a sentence takes from its journal
// line (a margin percentage): the catalogue lists the rule, not one line's
// numbers.
const placeholderFigure = "N %"

// Catalogue lists every sentence of the plain-words catalogue whose words do
// not depend on one journal line, rendered through the same functions
// /api/events and /api/trades use, sorted by key. The leg is the generic
// "option", the stop-loss multiple the default rule ("twice"), the days of a
// time roll "few" (ExplainTrade's own defaults) and the margin figures
// "N %". Amended orders and trend changes are left out: their sentences are
// built from a line's prices and directions.
func Catalogue() []CatalogueEntry {
	var out []CatalogueEntry
	add := func(key, text string) { out = append(out, CatalogueEntry{Key: key, Text: text}) }

	for reason := range openWords {
		plain, _ := ExplainTrade(orders.Trade{Kind: "open", Reason: reason})
		add(orders.EventFilled+":"+reason, plain)
	}
	for reason := range closeWords {
		plain, _ := ExplainTrade(orders.Trade{Kind: "close", Reason: reason})
		add(orders.EventClosed+":"+reason, plain)
	}
	// Submitted orders: the sells, and the buy-backs that rest at the mid
	// first (the three rolls, #239) — explainSubmit's buy sentence describes
	// those; the other buy-backs cross at once.
	for trigger, direction := range map[string]string{
		orders.TriggerEntry:           orders.DirectionSell,
		orders.TriggerRepair:          orders.DirectionSell,
		orders.TriggerRebalanceUpsize: orders.DirectionSell,
		orders.TriggerCloseLong:       orders.DirectionSell,
		orders.TriggerRollout19DTE:    orders.DirectionBuy,
		orders.TriggerRolloutDelta:    orders.DirectionBuy,
		orders.TriggerRolloutROI:      orders.DirectionBuy,
	} {
		plain, _ := explainSubmit(journalLine{TriggerReason: trigger, Direction: direction})
		add(orders.EventSubmitted+":"+trigger, plain)
	}
	for trigger := range cancelWhy {
		plain, _ := explainCancel(journalLine{TriggerReason: trigger})
		add(orders.EventCancelled+":"+trigger, plain)
	}
	resting, _ := explainCancel(journalLine{TriggerReason: orders.TriggerRolloutROI, Direction: orders.DirectionBuy})
	add(orders.EventCancelled+":resting_close", resting)
	for word, text := range skipWords {
		add(orders.EventSkipped+":"+word, text)
	}
	// The holds repairHeldWords tells apart, each by a detail that names it.
	for hold, detail := range map[string]string{
		"stopped":        "put stopped out",
		"stopped_top_up": "top-up held: put stopped out",
		"gex_shed":       "put shed by GEX",
		"regime_side":    "no new puts",
	} {
		add(orders.EventSkipped+":"+strategy.SkipRepairHeld+":"+hold, repairHeldWords(detail))
	}
	for change, f := range riskWords {
		add(orders.EventRisk+":"+change, strings.ReplaceAll(f(journalLine{}), pct(0), placeholderFigure))
	}
	reconciled, _ := ExplainEvent(orders.RecentEvent{Event: orders.EventReconciled})
	add(orders.EventReconciled, reconciled)
	pnl, _ := ExplainEvent(orders.RecentEvent{Event: orders.EventPnL})
	add(orders.EventPnL, pnl)
	add(orders.EventConfig+":first", explainConfig(journalLine{First: true}))
	add(orders.EventConfig+":unchanged", explainConfig(journalLine{Unchanged: true}))
	for status, text := range keyScopeWords {
		add(orders.EventKeyScope+":"+status, text)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
