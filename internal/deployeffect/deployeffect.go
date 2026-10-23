// Package deployeffect measures what each bot startup did to the book in
// its first minutes (#219, E1: "no restart or deploy changes the book by
// itself"). It reads the decision journal (orders.log) and, when given, the
// deploy record (<envs>/history/deploys.tsv); it never calls Deribit or the
// monitor, and it reports counts and lots only, never an amount.
//
// A startup is a burst of startup `reconciled` lines (their detail starts
// with "strangle ", reconcile.go; a position adopted mid-run has no detail)
// or, for a restart into a flat book that journals no such line, a deploy
// with no burst within DeployMatch of it. The deploy nearest a burst names
// its commit.
//
// Every order the journal shows in the Window after a startup (its submit,
// or its close for a market or IOC buy-back, which journals no submit) is
// classified; the startup takes its worst order's class:
//
//   - neutral: no order, or only entries into slots that held no strangle
//     at the restart (the slot was vacant before it too; the entry passed
//     every gate, so the reason it was vacant has cleared).
//   - expected: an order that would have been sent without the restart —
//     an exit rule (stop-loss, DTE roll, delta exit, delta drift,
//     take-profit) that strategy.EvaluateLeg says applied to the leg on its
//     mark at the restart (the reconciled line's, else the instrument's last
//     journaled mark before it); an exit of a position opened after the
//     restart; a repair whose leg was not held at the restart (its last
//     close was a roll, or a held close older than the repair cooldown); a
//     rebalance after a confirmed limit change or a person's rebalance in
//     the same window; the MM cut (Deribit's account figure), a manual close
//     or the kill switch (a person).
//   - restart-made: everything else — a rebalance with no confirmed limit
//     change, leg balancing (rebalance_legs), close_long, an entry into a
//     slot that held a strangle, a repair of a leg closed by a stop, delta
//     exit, manual close, MM cut or GEX shed inside the repair cooldown, an
//     exit of a position held at the restart whose rule did not apply (or
//     could not be checked: no mark) at the restart, a GEX shed of such a
//     position (its anchors and confirmations are not in the journal), and
//     any trigger this package does not know.
//
// Amends and cancels are counted by trigger but belong to the order they
// change, so they do not classify a startup on their own.
package deployeffect

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// Classes of a startup and of each order in its window.
const (
	Neutral     = "neutral"
	Expected    = "expected"
	RestartMade = "restart-made"
)

// classRank orders the classes: a startup takes its worst order's class.
var classRank = map[string]int{Neutral: 0, Expected: 1, RestartMade: 2}

const (
	// Window is how long after a startup its orders are counted (the
	// issue's 30 minutes); the next startup ends it earlier.
	Window = 30 * time.Minute
	// burstGap separates two startups: startup reconciled lines further
	// apart than this belong to different startups.
	burstGap = 2 * time.Minute
	// DeployMatch is how far a deploy record may be from a startup burst
	// to name its commit (deploy.sh records after its health check).
	DeployMatch = 10 * time.Minute
	// expiryHour is Deribit's settlement time (08:00 UTC) on the expiry
	// date an option name carries.
	expiryHour = 8 * time.Hour
)

// Line is one journal line, the fields the report reads.
type Line struct {
	At              time.Time
	Event           string
	Instrument      string
	Slot            *orders.SlotRef
	OrderID         string
	Trigger         string
	Detail          string
	Change          string // risk_limit lines
	Qty             float64
	Delta           float64 // the option's delta at the event
	Mid             float64 // the instrument's mid in the market snapshot (0 = no quote)
	FillPrice       float64 // reconciled: the average entry price
	PremiumReceived float64
}

// Deploy is one line of deploys.tsv: time, environment, commit and an
// optional "rollback" marker.
type Deploy struct {
	At       time.Time
	Commit   string
	Rollback bool
}

// Rules are the exit and repair settings the classification checks
// against, from the bot's config.
type Rules struct {
	RolloutDTE          int
	DeltaDriftThreshold float64
	ROITakeProfit       float64
	StopLossMultiplier  float64
	DeltaExitThreshold  float64
	RepairCooldown      time.Duration
}

// RulesFrom takes the rules from a loaded config.
func RulesFrom(c *config.Config) Rules {
	return Rules{
		RolloutDTE:          c.RolloutDTE,
		DeltaDriftThreshold: c.DeltaDriftThreshold,
		ROITakeProfit:       c.ROITakeProfit,
		StopLossMultiplier:  c.StopLossMultiplier,
		DeltaExitThreshold:  c.DeltaExitThreshold,
		RepairCooldown:      time.Duration(c.RepairCooldownHours) * time.Hour,
	}
}

// Order is one order the bot sent in a startup's window, with its class.
type Order struct {
	At         time.Time
	OrderID    string
	Trigger    string
	Instrument string
	Lots       float64
	Class      string
	Why        string
}

// TriggerCount is how many orders (and their lots) one trigger sent, and
// the amends and cancels journaled under it.
type TriggerCount struct {
	Orders  int
	Lots    float64
	Amends  int
	Cancels int
}

// Startup is one restart and what followed it.
type Startup struct {
	At       time.Time
	Commit   string // from deploys.tsv; empty when unknown
	Rollback bool   // the deploy was deploy.sh's rollback
	Source   string // "journal" (reconciled lines) or "deploy" (deploys.tsv only)
	End      time.Time
	Class    string
	Orders   []Order
	Triggers map[string]*TriggerCount
}

// Report is every startup in [From, To) and the summary over them.
type Report struct {
	From, To time.Time
	Startups []Startup
	ByClass  map[string]int
	Triggers map[string]*TriggerCount
}

// RestartMadeShare is the share of startups classified restart-made (E1's
// metric, target 0); ok is false when there was no startup.
func (r Report) RestartMadeShare() (share float64, ok bool) {
	if len(r.Startups) == 0 {
		return 0, false
	}
	return float64(r.ByClass[RestartMade]) / float64(len(r.Startups)), true
}

// leg is a position the startup loaded from the exchange.
type leg struct {
	Line
	mark float64 // the leg's mark at the restart (0 = unknown)
}

type startup struct {
	Startup
	legs  map[string]leg // by instrument
	slots map[orders.SlotRef]bool
}

// Analyze finds the startups in [from, to) and classifies what each sent.
// lines must be ordered by time (ReadJournal does); deploys may be nil.
func Analyze(lines []Line, deploys []Deploy, rules Rules, from, to time.Time) Report {
	starts := findStartups(lines, deploys)
	rep := Report{From: from, To: to, ByClass: map[string]int{}, Triggers: map[string]*TriggerCount{}}
	for i := range starts {
		st := &starts[i]
		if st.At.Before(from) || !st.At.Before(to) {
			continue
		}
		st.End = st.At.Add(Window)
		if i+1 < len(starts) && starts[i+1].At.Before(st.End) {
			st.End = starts[i+1].At
		}
		classifyWindow(st, lines, rules)
		rep.ByClass[st.Class]++
		for trig, c := range st.Triggers {
			addCount(rep.Triggers, trig, *c)
		}
		rep.Startups = append(rep.Startups, st.Startup)
	}
	return rep
}

// isStartupReconciled reports whether a line is one of a startup's
// reconciled lines (reconciledDetail starts them with "strangle ").
func isStartupReconciled(l Line) bool {
	return l.Event == orders.EventReconciled && strings.HasPrefix(l.Detail, "strangle ")
}

// findStartups groups startup reconciled lines into bursts, gives each the
// leg marks at the restart, matches deploys to them and adds deploys with
// no burst as startups of their own. The result is ordered by time.
func findStartups(lines []Line, deploys []Deploy) []startup {
	var out []startup
	lastMid := map[string]float64{}
	var cur *startup
	var last time.Time
	for _, l := range lines {
		if isStartupReconciled(l) {
			if cur == nil || l.At.Sub(last) > burstGap {
				out = append(out, startup{
					Startup: Startup{At: l.At, Source: "journal"},
					legs:    map[string]leg{}, slots: map[orders.SlotRef]bool{},
				})
				cur = &out[len(out)-1]
			}
			last = l.At
			mark := l.Mid
			if mark <= 0 {
				mark = lastMid[l.Instrument]
			}
			cur.legs[l.Instrument] = leg{Line: l, mark: mark}
			if l.Slot != nil {
				cur.slots[*l.Slot] = true
			}
		}
		if l.Instrument != "" && l.Mid > 0 {
			lastMid[l.Instrument] = l.Mid
		}
	}

	used := make([]bool, len(deploys))
	for i := range out {
		best := -1
		for j, d := range deploys {
			gap := absDur(d.At.Sub(out[i].At))
			if !used[j] && gap <= DeployMatch && (best < 0 || gap < absDur(deploys[best].At.Sub(out[i].At))) {
				best = j
			}
		}
		if best >= 0 {
			used[best] = true
			out[i].Commit, out[i].Rollback = deploys[best].Commit, deploys[best].Rollback
		}
	}
	for j, d := range deploys {
		if !used[j] {
			out = append(out, startup{
				Startup: Startup{At: d.At, Commit: d.Commit, Rollback: d.Rollback, Source: "deploy"},
				legs:    map[string]leg{}, slots: map[orders.SlotRef]bool{},
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// classifyWindow collects the orders, amends and cancels in the startup's
// window and classifies each order.
func classifyWindow(st *startup, lines []Line, rules Rules) {
	st.Class = Neutral
	st.Triggers = map[string]*TriggerCount{}
	inWindow := func(l Line) bool { return !l.At.Before(st.At) && l.At.Before(st.End) }
	// A resting close journals its submit, then its fills as closes: the
	// submit is the order. A market or IOC buy-back journals closes only,
	// one per filled piece.
	submittedIDs := map[string]bool{}
	for _, l := range lines {
		if inWindow(l) && l.Event == orders.EventSubmitted && l.OrderID != "" {
			submittedIDs[l.OrderID] = true
		}
	}
	closedAt := map[string]int{} // order id → index in st.Orders, closes without a submit
	for _, l := range lines {
		if !inWindow(l) {
			continue
		}
		switch l.Event {
		case orders.EventAmended:
			count(st.Triggers, l.Trigger).Amends++
		case orders.EventCancelled:
			count(st.Triggers, l.Trigger).Cancels++
		case orders.EventSubmitted, orders.EventClosed:
			if l.Event == orders.EventClosed && l.OrderID != "" {
				if submittedIDs[l.OrderID] {
					continue
				}
				if i, ok := closedAt[l.OrderID]; ok {
					st.Orders[i].Lots += l.Qty
					continue
				}
				closedAt[l.OrderID] = len(st.Orders)
			}
			class, why := classify(l, st, lines, rules)
			st.Orders = append(st.Orders, Order{
				At: l.At, OrderID: l.OrderID, Trigger: l.Trigger, Instrument: l.Instrument,
				Lots: l.Qty, Class: class, Why: why,
			})
			if classRank[class] > classRank[st.Class] {
				st.Class = class
			}
		}
	}
	for _, o := range st.Orders {
		c := count(st.Triggers, o.Trigger)
		c.Orders++
		c.Lots += o.Lots
	}
}

// classify gives one order its class and the reason, by trigger.
func classify(o Line, st *startup, lines []Line, rules Rules) (class, why string) {
	switch o.Trigger {
	case orders.TriggerEntry:
		if o.Slot != nil && st.slots[*o.Slot] {
			return RestartMade, fmt.Sprintf("entry into slot %dd/%.2f, which held a strangle at the restart", o.Slot.DTE, o.Slot.Delta)
		}
		return Neutral, "entry into a slot vacant at the restart"
	case orders.TriggerRepair:
		return classifyRepair(o, st, lines, rules)
	case orders.TriggerRebalanceUpsize, orders.TriggerRebalanceDownsize:
		for _, l := range lines {
			if l.Event == orders.EventRisk && !l.At.Before(st.At) && !l.At.After(o.At) &&
				(l.Change == orders.RiskLimitChanged || l.Change == orders.RiskRebalanceManual) {
				return Expected, "rebalance after " + l.Change + " at " + l.At.UTC().Format("15:04:05")
			}
		}
		return RestartMade, "rebalance after the startup with no confirmed limit change"
	case orders.TriggerRebalanceLegs:
		return RestartMade, "leg balancing after the startup"
	case orders.TriggerCloseLong:
		return RestartMade, "close of a long held at the restart"
	case orders.TriggerStopLoss200Pct, orders.TriggerRollout19DTE, orders.TriggerRolloutDelta,
		orders.TriggerRolloutROI, orders.TriggerDeltaExit:
		return classifyExit(o, st, rules)
	case orders.TriggerGammaClose:
		if _, held := st.legs[o.Instrument]; !held {
			return Expected, "GEX shed of a position opened after the restart"
		}
		return RestartMade, "GEX shed of a position held at the restart (its anchor is not in the journal: check the close's detail)"
	case orders.TriggerMarginMM:
		return Expected, "MM cut: Deribit's maintenance margin over the limit"
	case orders.TriggerManualClose, orders.TriggerKillSwitch:
		return Expected, o.Trigger + ": a person's action"
	}
	return RestartMade, fmt.Sprintf("trigger %q not known to the report", o.Trigger)
}

// classifyExit checks whether the exit rule applied to the leg on its mark
// at the restart (strategy.EvaluateLeg, any closing decision).
func classifyExit(o Line, st *startup, rules Rules) (class, why string) {
	lg, held := st.legs[o.Instrument]
	if !held {
		return Expected, "exit of a position opened after the restart"
	}
	_, expiry, _, _, err := marketdata.ParseOptionName(o.Instrument)
	if err != nil {
		return RestartMade, "exit rule not checkable: " + err.Error()
	}
	pos := &orders.Position{
		Instrument:      lg.Instrument,
		Qty:             lg.Qty,
		EntryPrice:      lg.FillPrice,
		PremiumReceived: lg.PremiumReceived,
		Expiry:          expiry.Add(expiryHour),
		CurrentMid:      lg.mark,
		MarkLive:        lg.mark > 0,
		CurrentGreeks:   orders.Greeks{Delta: lg.Delta},
	}
	if pos.PremiumReceived == 0 {
		pos.PremiumReceived = pos.EntryPrice * pos.Qty
	}
	d := strategy.EvaluateLeg(pos, st.At, rules.RolloutDTE, rules.DeltaDriftThreshold,
		rules.ROITakeProfit, rules.StopLossMultiplier, rules.DeltaExitThreshold)
	if d.Action != strategy.ActionNone {
		// The rule's name only: its detail carries prices.
		return Expected, "due on the mark at the restart: " + d.Reason
	}
	if lg.mark <= 0 {
		return RestartMade, o.Trigger + " of a position held at the restart; rule not checkable: no mark at the restart"
	}
	return RestartMade, o.Trigger + " of a position held at the restart; no exit rule applied on its mark at the restart"
}

// heldCloses are the closes whose leg the bot holds before re-selling it
// (stopped.go, shed.go).
var heldCloses = map[string]bool{
	orders.TriggerStopLoss200Pct: true, orders.TriggerDeltaExit: true, orders.TriggerManualClose: true,
	orders.TriggerMarginMM: true, orders.TriggerGammaClose: true,
}

// classifyRepair looks at the last close before the restart of the
// repaired side on its expiry: a held close inside the cooldown means the
// restart re-sold a leg the bot was holding.
func classifyRepair(o Line, st *startup, lines []Line, rules Rules) (class, why string) {
	_, expiry, _, side, err := marketdata.ParseOptionName(o.Instrument)
	if err != nil {
		return RestartMade, "repair not checkable: " + err.Error()
	}
	var last *Line
	for i := range lines {
		l := &lines[i]
		if !l.At.Before(st.At) {
			break
		}
		if l.Event != orders.EventClosed {
			continue
		}
		if _, exp, _, typ, err := marketdata.ParseOptionName(l.Instrument); err == nil && typ == side && exp.Equal(expiry) {
			last = l
		}
	}
	if last == nil {
		return Expected, "repair of a leg missing at the restart with no close before it (one-legged fill)"
	}
	age := st.At.Sub(last.At)
	if heldCloses[last.Trigger] && age < rules.RepairCooldown {
		return RestartMade, fmt.Sprintf("repair of a %s leg closed by %s %.1f h before the restart (cooldown %.0f h)",
			side, last.Trigger, age.Hours(), rules.RepairCooldown.Hours())
	}
	return Expected, fmt.Sprintf("repair of a %s leg closed by %s %.1f h before the restart: not held", side, last.Trigger, age.Hours())
}

func count(m map[string]*TriggerCount, trigger string) *TriggerCount {
	if trigger == "" {
		trigger = "(none)"
	}
	c, ok := m[trigger]
	if !ok {
		c = &TriggerCount{}
		m[trigger] = c
	}
	return c
}

func addCount(m map[string]*TriggerCount, trigger string, c TriggerCount) {
	t := count(m, trigger)
	t.Orders += c.Orders
	t.Lots += c.Lots
	t.Amends += c.Amends
	t.Cancels += c.Cancels
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
