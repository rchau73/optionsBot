package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"optionsbot/internal/gateway"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// The plain-words catalogue (#201): one sentence per journal event, written
// for a holder new to options — what happened, why (the rule in everyday
// words) and what follows when that is known. Facts only: never a forecast,
// never advice. /api/events and /api/trades serve it as "plain", and the
// investor summary's last action (#177) reads the same catalogue.
//
// A journal value the catalogue does not know gets a generic sentence (or,
// for a trade, its rule name in brackets) and one WARN per value per process,
// so a new journal action never breaks the API. tests/api_explain_test.go
// reads the journal's constants from the code: a new one without a sentence
// fails CI.

// GenericPlain begins the sentence of an event the catalogue does not know.
const GenericPlain = "The bot recorded a step that has no plain-words explanation yet"

var (
	stopMultipleRe = regexp.MustCompile(`≥ ([0-9.]+)×`)
	dteRe          = regexp.MustCompile(`(\d+) days to expiry`)
)

// openWords explains a fill that opened (sold) a leg, by trigger.
var openWords = map[string]string{
	orders.TriggerEntry:           "Sold the %s to open a new strangle and collect its premium (entry)",
	orders.TriggerRepair:          "Sold the %s again to complete its strangle (repair)",
	orders.TriggerRebalanceUpsize: "Sold more of the %s to bring the slot up to its share of the margin limit (rebalance)",
}

// closeWords explains a buy-back, by the journal's close_reason label
// (orders.CloseReasonLabel). %[1]s is the leg (put, call); stop_loss and
// dte_rollout read their numbers from the detail (closeSentence).
var closeWords = map[string]string{
	"stop_loss": "Bought back the %[1]s: it lost %[2]s its premium (stop-loss). It was bought back to limit the loss; " +
		"the %[1]s will not be sold again on that side until its cooldown ends",
	"dte_rollout": "Bought back the %[1]s: only %[2]s days to expiry left, so the strangle closes before expiry (time roll). " +
		"The slot then opens a new strangle on a later expiry",
	"delta_exit": "Bought back the %[1]s: the price moved close to its strike, so the risk grew too fast (delta exit). " +
		"Like a stop-loss, it will not be sold again on that side until its cooldown ends",
	"delta_drift": "Bought back the %[1]s: little premium was left to earn; it is sold again nearer the price (delta drift)",
	"roi_target": "Bought back the %[1]s: most of its premium was already earned (take-profit). " +
		"The %[1]s is sold again to complete the strangle",
	"gamma_regime": "Bought back the %[1]s: market makers' positioning pointed to a strong move against it (GEX shed). " +
		"It is sold again once that signal has passed",
	"kill_switch": "Bought back the %[1]s: the kill switch closed every position. The bot stays idle until it is restarted",
	orders.TriggerManualClose: "Bought back the %[1]s by hand from the monitor (manual close). " +
		"Like a stop-loss, it will not be sold again on that side until its cooldown ends",
	orders.TriggerRebalanceDownsize: "Bought back part of the %[1]s: margin in use was above its limit (rebalance). " +
		"The rest stays open",
	orders.TriggerMarginMM: "Bought back part of the %[1]s: maintenance margin reached its safety limit (margin cut). " +
		"Nothing new is sold until it is lower",
	orders.TriggerRebalanceLegs: "Bought back the extra %[1]s so the call and the put are the same size (leg balance)",
}

// orderPurpose says why an order was sent, by its trigger: it ends the
// sentences of submitted, amended and cancelled orders.
var orderPurpose = map[string]string{
	orders.TriggerEntry:             "to open a new strangle and collect its premium",
	orders.TriggerRepair:            "to complete its strangle again",
	orders.TriggerRebalanceUpsize:   "to bring the slot up to its share of the margin limit",
	orders.TriggerCloseLong:         "because the bot held it long, and the strategy only sells options",
	orders.TriggerRollout19DTE:      "because it is close to expiry",
	orders.TriggerRolloutDelta:      "because little premium is left to earn",
	orders.TriggerRolloutROI:        "because most of its premium is already earned",
	orders.TriggerStopLoss200Pct:    "to limit a loss",
	orders.TriggerDeltaExit:         "because the price moved close to its strike",
	orders.TriggerGammaClose:        "because market makers' positioning points to a strong move against it",
	orders.TriggerKillSwitch:        "because the kill switch closes every position",
	orders.TriggerManualClose:       "because a person asked for it from the monitor",
	orders.TriggerReconciled:        "as a position found on the exchange at startup",
	orders.TriggerTimeout:           "after an earlier order did not fill in time",
	orders.TriggerRebalanceDownsize: "because margin in use is above its limit",
	orders.TriggerMarginMM:          "because maintenance margin reached its safety limit",
	orders.TriggerRebalanceLegs:     "so the call and the put are the same size",
	orders.TriggerRiskFrozen:        "while new risk is paused",
	orders.TriggerRegimeSide:        "while that side is not sold: market makers amplify moves and the trend runs toward it",
}

// cancelWhy says why the bot cancelled a working sell order, by the trigger
// its cancel was journaled with.
var cancelWhy = map[string]string{
	orders.TriggerTimeout:    "it did not fill within the time allowed",
	orders.TriggerRiskFrozen: "new risk was paused (a margin-limit change waits for confirmation, or maintenance margin is too high)",
	orders.TriggerGammaClose: "market makers' positioning turned against that side",
	orders.TriggerKillSwitch: "the kill switch stopped all trading",
	orders.TriggerRegimeSide: "market makers amplify moves now and the trend turned toward that side, so it is not sold",
}

// skipWords explains a journaled skip, by its reason word.
var skipWords = map[string]string{
	strategy.SkipMarginLimit: "No new strangle in this slot: the margin it would use does not fit under the margin limit. " +
		"The slot is tried again each cycle.",
	strategy.SkipMarginUnknown: "No new strangle: the exchange's margin figures are missing or out of date, " +
		"so the bot adds no risk until they come back.",
	strategy.SkipRiskFrozen: "No new strangle: new risk is paused while a change in volatility or in market makers' positioning " +
		"waits to be confirmed on daily closes. Exits and repairs continue.",
	// The sentence of a known hold is picked from the detail (repairHeldWords);
	// this one covers every hold, for a line whose detail names none.
	strategy.SkipRepairHeld: "A missing leg is not sold again yet: a rule still holds that side back " +
		"(a cooldown after a stop-loss or a close by hand, a GEX shed whose signal has not passed, " +
		"or the trend under a negative gamma regime). It is sold again once that rule lets it go.",
	strategy.SkipNoExpiry: "No new strangle in this slot: the exchange lists no expiry date in the slot's range. " +
		"It waits for one.",
	strategy.SkipNoFreeExpiry: "No new strangle in this slot: every expiry date in its range is already used by another slot, " +
		"and each slot keeps its own date. It waits for a free one.",
	strategy.SkipNoStrike: "No new option sold in this slot: no strike price was close enough to the slot's target risk (delta) " +
		"with a real market — a buyer and a seller, and a price in line with the options near the money. " +
		"It tries again next cycle.",
	strategy.SkipEntryRejected: "No new strangle in this slot: the order could not be sent (premium too small, size under the minimum, " +
		"or an exchange error). It tries again next cycle.",
	strategy.SkipChurnPaused: "This slot is paused: it bought back and sold again too many times within an hour. " +
		"It resumes when the pause ends; exits still run.",
	strategy.SkipStopDeferred: "A stop-loss waits a few minutes: the option's order book is too thin (the asking price is far above the mid), " +
		"so buying at market would cost too much. It fires once the spread is normal, or at market when the wait ends; " +
		"nothing new is sold meanwhile.",
	strategy.SkipRegimeSide: "One side is not sold: market makers' positioning favours strong moves and the price trend runs toward that side, " +
		"so the bot sells only the other side for now.",
	strategy.SkipGEXWait: "No new risk yet: the market makers' positioning data (GEX) is missing or out of date, " +
		"so the bot waits for a fresh reading.",
	strategy.SkipBiasWarmup: "A close of one side was held back: that signal is trusted only once a full hour of data supports it " +
		"(after a restart or a data gap). New sales of that side wait meanwhile.",
	strategy.SkipEntryWaitTopup: "No new strangle yet: a rebalance order is still working, and the bot sizes one new position at a time " +
		"so the two do not add up past the margin limit.",
	strategy.SkipRebalanceWaitEntry: "The rebalance waits: an entry order is still working, and the bot sizes one new position at a time " +
		"so the two do not add up past the margin limit.",
	strategy.SkipLiquidityCap: "The order was made smaller: it is kept to a small share of the contracts open on each strike, " +
		"so it can fill without moving the price. Under one contract nothing is sent.",
	strategy.SkipManualCloseRefused: "A manual close from the monitor was refused: it is allowed only on a side the bot no longer sells " +
		"(a negative gamma regime with the trend moving toward that side). Nothing was bought.",
}

// riskWords explains a margin-policy line, by its change.
var riskWords = map[string]func(l journalLine) string{
	orders.RiskFrozen: func(journalLine) string {
		return "New risk is paused: a change in volatility or in market makers' positioning would lower the margin limit, " +
			"and it waits to be confirmed on daily closes. Exits, rolls and repairs continue."
	},
	orders.RiskUnfrozen: func(journalLine) string {
		return "New risk is allowed again: the change that paused it was confirmed or went away."
	},
	orders.RiskLimitChanged: func(l journalLine) string {
		return fmt.Sprintf("The margin limit changed to %s of the margin balance after a confirmed change in volatility "+
			"or market positioning. The positions are resized toward it.", pct(l.LimitIMPct))
	},
	orders.RiskRebalance: func(journalLine) string {
		return "The positions are resized so the margin in use matches the limit: slots under their share get more, " +
			"slots over it are bought back in part."
	},
	orders.RiskRebalanceInBand: func(journalLine) string {
		return "After a restart the positions were left as they are: the margin in use is already close to its limit."
	},
	orders.RiskRegimeSeeded: func(journalLine) string {
		return "First start with no history: the bot took today's market positioning as confirmed, so it trades from the start."
	},
	orders.RiskRebalanceRetry: func(journalLine) string {
		return "A rebalance order did not fill in full. The bot tries again after a short wait."
	},
	orders.RiskRebalanceManual: func(l journalLine) string {
		who := "A person"
		if l.Actor != nil && l.Actor.User != "" {
			who = l.Actor.User
		}
		return who + " asked for a rebalance from the monitor: the positions are resized toward the margin limit."
	},
	orders.RiskMMBreach: func(journalLine) string {
		return "Maintenance margin reached its safety limit: positions are bought back at market now, " +
			"and nothing new is sold until it is lower."
	},
	orders.RiskIMOverLimit: func(l journalLine) string {
		return fmt.Sprintf("Margin in use (%s) is above its limit (%s) with no rebalance due, for example after a transfer "+
			"or a price move. This is an alert only: the bot sends no order for it.", pct(l.IMPct), pct(l.LimitIMPct))
	},
	orders.RiskIMOverLimitDone: func(journalLine) string {
		return "Margin in use is back at or under its limit; the alert ends."
	},
	orders.RiskIMUnderUse: func(l journalLine) string {
		return fmt.Sprintf("Margin in use (%s) has stayed under 60 %% of its limit (%s) for hours while new trades were allowed%s. "+
			"This is an alert only: the bot sends no order for it.", pct(l.IMPct), pct(l.LimitIMPct), underUseCause(l.Cause))
	},
	orders.RiskIMUnderUseDone: func(journalLine) string {
		return "The unused-margin alert ends: margin in use is back at 60 % of its limit or more, or new trades are no longer allowed."
	},
}

// causeWords puts the im_under_use cause in everyday words (orders.Idle*;
// a vacant slot's skip reason is named as it was journaled).
var causeWords = map[string]string{
	orders.IdleSlotEmpty:        "a slot is empty",
	orders.IdleNoSkip:           "a slot is empty with no reason recorded yet",
	orders.IdleRebalanceWaiting: "every slot is taken and a rebalance top-up waits to try again",
	orders.IdleSizedSmall:       "every slot is taken but one was sized down to what the market can trade",
	orders.IdleAllFull:          "every slot is taken at its normal size",
	orders.IdleNotMeasured:      "the cause was not measured",
}

func underUseCause(cause string) string {
	switch {
	case cause == "":
		return ""
	case causeWords[cause] != "":
		return "; cause: " + causeWords[cause]
	}
	return "; cause: a slot is empty, last skipped for " + strings.ReplaceAll(cause, "_", " ")
}

// trendWords and trendWhy put the SMA trend and its reason in everyday words.
var (
	trendWords = map[string]string{"bull": "rising", "bear": "falling", "neutral": "flat"}
	trendWhy   = map[string]string{
		strategy.TrendReasonNoSwing:   "no clear high or low yet",
		strategy.TrendReasonBreakout:  "the price rose above its last high",
		strategy.TrendReasonBullSMA:   "the short average is above the long one and the price above both",
		strategy.TrendReasonBreakdown: "the price fell below its last low",
		strategy.TrendReasonBearSMA:   "the short average is below the long one and the price below both",
		strategy.TrendReasonInside:    "the price is between its last high and low, with no clear direction",
	}
)

// journalLine is the part of an orders.log line the catalogue reads; one
// struct for every event type (fields of other events stay empty).
type journalLine struct {
	Instrument    string        `json:"instrument"`
	OptionType    string        `json:"option_type"`
	Direction     string        `json:"direction"` // buy | sell (orders); sma | bias (trend_changed)
	TriggerReason string        `json:"trigger_reason"`
	SkipReason    string        `json:"skip_reason"`
	CloseReason   string        `json:"close_reason"`
	Detail        string        `json:"detail"`
	Qty           float64       `json:"qty"`
	FilledQty     float64       `json:"filled_qty"`
	LimitPrice    float64       `json:"limit_price"`
	PreviousPrice float64       `json:"previous_price"`
	Actor         *orders.Actor `json:"actor"`
	Change        string        `json:"change"` // risk_limit
	LimitIMPct    float64       `json:"limit_im_pct"`
	IMPct         float64       `json:"im_pct"`
	Cause         string        `json:"cause"` // im_under_use
	From          string        `json:"from"`  // trend_changed
	To            string        `json:"to"`
	Reason        string        `json:"reason"`
	First         bool          `json:"first"` // config_loaded
	Unchanged     bool          `json:"unchanged"`
	Changes       []struct {
		Key string `json:"key"`
	} `json:"changes"`
	Status string `json:"status"` // key_scope
}

// keyScopeWords says what a key_scope line (#538) recorded, by its status.
var keyScopeWords = map[string]string{
	gateway.KeyScopeTradeOnly: "The bot checked its exchange key at startup: it can trade but cannot withdraw or transfer funds.",
	gateway.KeyScopeCanMoveFunds: "The bot checked its exchange key at startup: the key can also move funds out of the account. " +
		"The policy refuses withdrawal rights, so the key should be replaced by a trade-only one; the bot keeps trading meanwhile.",
	gateway.KeyScopeUnknown: "The bot could not read what its exchange key may do at startup, so whether it can move funds is unknown.",
}

// explainKeyScope says what a key_scope line recorded; an unreadable status
// is the unknown sentence, never the trade-only one.
func explainKeyScope(l journalLine) string {
	if s, ok := keyScopeWords[l.Status]; ok {
		return s
	}
	return keyScopeWords[gateway.KeyScopeUnknown]
}

// explainConfig says what a config_loaded line (#256) recorded.
func explainConfig(l journalLine) string {
	switch {
	case l.First:
		return "The bot recorded the settings it trades with; later starts will say what changed."
	case l.Unchanged || len(l.Changes) == 0:
		return "The bot restarted with the same settings as before."
	}
	keys := make([]string, len(l.Changes))
	for i, c := range l.Changes {
		keys[i] = c.Key
	}
	return fmt.Sprintf("The bot started with changed settings (%s); results from here on come from these settings.", strings.Join(keys, ", "))
}

// ExplainEvent says in plain words what a journal event means. unknown names
// the journal value the catalogue does not know ("skip_reason=foo"), empty
// when it knew it; plain is then the generic sentence.
func ExplainEvent(e orders.RecentEvent) (plain, unknown string) {
	var l journalLine
	if len(e.Data) > 0 {
		if err := json.Unmarshal(e.Data, &l); err != nil {
			return generic("event=" + e.Event)
		}
	}
	switch e.Event {
	case orders.EventSubmitted:
		return explainSubmit(l)
	case orders.EventAmended:
		return explainAmend(l)
	case orders.EventCancelled:
		return explainCancel(l)
	case orders.EventFilled:
		return ExplainTrade(orders.Trade{Kind: "open", OptionType: l.OptionType, Reason: l.TriggerReason, Detail: l.Detail})
	case orders.EventClosed:
		return ExplainTrade(orders.Trade{Kind: "close", OptionType: l.OptionType, Reason: l.CloseReason, Detail: l.Detail})
	case orders.EventReconciled:
		return fmt.Sprintf("Found a short %s on the exchange at startup and took it into the book; "+
			"the bot manages it like a position it sold itself.", legName(l)), ""
	case orders.EventSkipped:
		word := skipWord(l.SkipReason)
		if word == strategy.SkipRepairHeld {
			if s := repairHeldWords(l.SkipReason); s != "" {
				return s, ""
			}
		}
		if s, ok := skipWords[word]; ok {
			return s, ""
		}
		return generic("skip_reason=" + word)
	case orders.EventRisk:
		if f, ok := riskWords[l.Change]; ok {
			return f(l), ""
		}
		return generic("change=" + l.Change)
	case orders.EventTrend:
		return explainTrend(l), ""
	case orders.EventPnL:
		return "A periodic profit-and-loss snapshot; nothing was traded.", ""
	case orders.EventConfig:
		return explainConfig(l), ""
	case orders.EventKeyScope:
		return explainKeyScope(l), ""
	}
	return generic("event=" + e.Event)
}

// ExplainTrade says why a trade (an open or a close of the journal) happened.
// An unknown reason keeps its rule name in brackets ("Bought back the call
// (something_new)") and is named in unknown.
func ExplainTrade(t orders.Trade) (plain, unknown string) {
	leg := legOf(t.OptionType)
	if t.Kind == "open" {
		if f, ok := openWords[t.Reason]; ok {
			return fmt.Sprintf(f, leg), ""
		}
		return fmt.Sprintf("Sold the %s (%s)", leg, t.Reason), "open_reason=" + t.Reason
	}
	f, ok := closeWords[t.Reason]
	if !ok {
		return fmt.Sprintf("Bought back the %s (%s)", leg, t.Reason), "close_reason=" + t.Reason
	}
	switch t.Reason {
	case "stop_loss":
		return fmt.Sprintf(f, leg, timesWord(t.Detail)), ""
	case "dte_rollout":
		days := "few"
		if m := dteRe.FindStringSubmatch(t.Detail); m != nil {
			days = m[1]
		}
		return fmt.Sprintf(f, leg, days), ""
	}
	return fmt.Sprintf(f, leg), ""
}

// PlainReason says why a trade happened in words a holder new to options
// reads, from its journal reason (and the numbers in its detail).
func PlainReason(t orders.Trade) string {
	plain, _ := ExplainTrade(t)
	return plain
}

func explainSubmit(l journalLine) (string, string) {
	why, ok := orderPurpose[l.TriggerReason]
	if !ok {
		return generic("trigger_reason=" + l.TriggerReason)
	}
	size := ""
	if l.Qty > 0 {
		size = qty(l.Qty) + " of "
	}
	s := fmt.Sprintf("Sent an order to %s %sthe %s %s.", verbOf(l), size, legName(l), why)
	switch {
	case l.TriggerReason == orders.TriggerCloseLong:
		s += " It sells at the best bid."
	case l.Direction == orders.DirectionBuy:
		s += " It rests at the mid price for a while; if the rule still applies when that time ends, the rest is bought at the asking price."
	default:
		s += " It rests on the order book; if it does not fill, its price steps down and it is cancelled after the fill timeout."
	}
	return s, ""
}

func explainAmend(l journalLine) (string, string) {
	why, ok := orderPurpose[l.TriggerReason]
	if !ok {
		return generic("trigger_reason=" + l.TriggerReason)
	}
	move := "Lowered"
	if l.LimitPrice > l.PreviousPrice {
		move = "Raised"
	}
	return fmt.Sprintf("%s the price of the order to %s the %s from %s to %s, so it is more likely to fill (sent %s).",
		move, verbOf(l), legName(l), price(l.PreviousPrice), price(l.LimitPrice), why), ""
}

func explainCancel(l journalLine) (string, string) {
	filled := " Nothing had filled."
	if l.FilledQty > 0 {
		filled = fmt.Sprintf(" %s of %s had filled and is kept.", qty(l.FilledQty), qty(l.Qty))
	}
	if l.Direction == orders.DirectionBuy {
		if _, ok := orderPurpose[l.TriggerReason]; !ok {
			return generic("trigger_reason=" + l.TriggerReason)
		}
		return fmt.Sprintf("Cancelled the resting order to buy back the %s.%s If the rule that sent it still applies, "+
			"the rest is bought at the asking price.", legName(l), filled), ""
	}
	why, ok := cancelWhy[l.TriggerReason]
	if !ok {
		purpose, known := orderPurpose[l.TriggerReason]
		if !known {
			return generic("trigger_reason=" + l.TriggerReason)
		}
		why = "it was sent " + purpose
	}
	s := fmt.Sprintf("Cancelled the order to sell the %s: %s.%s", legName(l), why, filled)
	if l.TriggerReason == orders.TriggerTimeout {
		s += " If only one side of a strangle filled, a repair sells the other side later."
	}
	return s, ""
}

func explainTrend(l journalLine) string {
	s := fmt.Sprintf("The price trend changed from %s to %s", trendWord(l.From), trendWord(l.To))
	if why, ok := trendWhy[l.Reason]; ok {
		s += ": " + why
	}
	return s + ". When market makers' positioning favours strong moves, the bot does not sell the side the trend moves toward."
}

// PlainEvent is a journal event as /api/events serves it: its fields, plus
// the plain-words sentence.
type PlainEvent struct {
	orders.RecentEvent
	Plain string `json:"plain"`
}

// PlainTrade is a trade as /api/trades serves it: its fields, plus the
// plain-words sentence.
type PlainTrade struct {
	orders.Trade
	Plain string `json:"plain"`
}

func plainEvents(events []orders.RecentEvent) []PlainEvent {
	out := make([]PlainEvent, 0, len(events))
	for _, e := range events {
		plain, unknown := ExplainEvent(e)
		warnUnknown(unknown)
		out = append(out, PlainEvent{RecentEvent: e, Plain: plain})
	}
	return out
}

func plainTrades(trades []orders.Trade) []PlainTrade {
	out := make([]PlainTrade, 0, len(trades))
	for _, t := range trades {
		plain, unknown := ExplainTrade(t)
		warnUnknown(unknown)
		out = append(out, PlainTrade{Trade: t, Plain: plain})
	}
	return out
}

// generic is the sentence for a journal value the catalogue does not know.
func generic(key string) (string, string) {
	return GenericPlain + " (" + key + ").", key
}

var warnedUnknown sync.Map

// warnUnknown logs one WARN per unknown journal value per process.
func warnUnknown(key string) {
	if key == "" {
		return
	}
	if _, seen := warnedUnknown.LoadOrStore(key, true); !seen {
		slog.Warn("plain words: journal value not in the catalogue", "value", key)
	}
}

// skipWord is the reason word of a journaled skip: "no_free_expiry: …" and
// "stop_deferred BTC-…: …" → the part before the first ':' or space.
func skipWord(text string) string {
	if i := strings.IndexAny(text, ": "); i >= 0 {
		return text[:i]
	}
	return text
}

// repairHeldWords explains a repair_held skip by the hold its detail names
// (the reasons of strategy's repairGate and complementHold): a stopped or
// hand-closed side ("<side> stopped out, …"), a GEX shed ("shed by GEX …")
// or the regime-side rule ("… no new puts"). "" when the detail names none.
func repairHeldWords(text string) string {
	topUp := strings.Contains(text, "top-up held")
	switch {
	case strings.Contains(text, "stopped out"):
		if topUp {
			return "The rebalance adds nothing to this slot yet: one of its sides was closed by a stop-loss, " +
				"a quick exit, a margin cut or by hand, and that side waits for its cooldown and a calmer market. " +
				"The rebalance tries again later."
		}
		return "A missing leg is not sold again yet: that side was closed by a stop-loss, a quick exit, a margin cut or by hand, " +
			"so the bot waits for its cooldown and a calmer market before selling it again."
	case strings.Contains(text, "shed by GEX"):
		return "A missing leg is not sold again yet: it was bought back because market makers' positioning pointed to " +
			"a strong move against it (GEX shed). It is sold again once that signal has passed for long enough, " +
			"not on a single reading."
	case strings.Contains(text, "no new puts"), strings.Contains(text, "no new calls"):
		return "A missing leg is not sold again yet: market makers' positioning favours strong moves and the price trend " +
			"runs toward that side, so the bot does not sell it until that changes."
	}
	return ""
}

// timesWord reads the stop-loss multiple from a stop's detail ("≥ 2.00×"):
// "twice" for 2, else "N times"; "twice" (the default rule) when absent.
func timesWord(detail string) string {
	m := stopMultipleRe.FindStringSubmatch(detail)
	if m == nil {
		return "twice"
	}
	x, err := strconv.ParseFloat(m[1], 64)
	if err != nil || x == 2 {
		return "twice"
	}
	return strconv.FormatFloat(x, 'f', -1, 64) + " times"
}

func legOf(optionType string) string {
	if optionType == "" {
		return "option"
	}
	return optionType
}

// legName is the leg and its instrument ("put BTC-27NOV26-60000-P"), or the
// leg alone when the line names no instrument.
func legName(l journalLine) string {
	if l.Instrument == "" {
		return legOf(l.OptionType)
	}
	return legOf(l.OptionType) + " " + l.Instrument
}

func verbOf(l journalLine) string {
	if l.Direction == orders.DirectionBuy {
		return "buy back"
	}
	return "sell"
}

func trendWord(t string) string {
	if w, ok := trendWords[t]; ok {
		return w
	}
	return "unknown"
}

func qty(q float64) string   { return strconv.FormatFloat(q, 'f', -1, 64) }
func price(p float64) string { return strconv.FormatFloat(p, 'f', -1, 64) }
func pct(p float64) string   { return strconv.FormatFloat(p, 'f', 1, 64) + " %" }
