package evidence

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"optionsbot/internal/history"
	"optionsbot/internal/orders"
)

// The attribution section (#581): where the week's booked result came from.
// Each close of the week splits into the premium of the part closed, its
// buy-back cost (fill price × quantity, by exit reason) and its fees (opening
// share + closing fee). The parts are measured from the journal line, not
// derived from its P&L, so a close that does not add up shows as unexplained,
// with its size, and the rows always sum to the report's realised result.
// A live Deribit account shows percentages only.

// AttributionLabel names the section.
const AttributionLabel = "where the week's booked result came from"

// AttributionTolerancePct is the largest unexplained part, in % of the
// starting equity, left unflagged.
const AttributionTolerancePct = 1.0

// The rows that are not an exit reason.
const (
	AttrPremium     = "premium_sold"
	AttrOther       = "other"
	AttrFees        = "fees"
	AttrUnexplained = "unexplained"
	AttrNet         = "net"
)

// attrZero is the size under which a coin figure is float noise.
const attrZero = 1e-12

// attrExits are the buy-back rows, by the journal's close reason. Any other
// reason (GEX shed, downsize, leg balancing, manual, kill switch) goes in
// AttrOther and is named in OtherReasons.
var attrExits = []struct{ reason, name string }{
	{orders.CloseReasonLabel(orders.TriggerStopLoss200Pct), "buy-back: stop-loss"},
	{orders.CloseReasonLabel(orders.TriggerDeltaExit), "buy-back: delta exit"},
	{orders.CloseReasonLabel(orders.TriggerRollout19DTE), "buy-back: DTE roll"},
	{orders.CloseReasonLabel(orders.TriggerRolloutDelta), "buy-back: delta drift"},
	{orders.CloseReasonLabel(orders.TriggerRolloutROI), "buy-back: take-profit"},
	{orders.CloseReasonLabel(orders.TriggerMarginMM), "buy-back: MM cut"},
	{AttrOther, "buy-back: other"},
}

// AttributionRow is one figure. Income is positive, a cost negative. Coin and
// USD are nil for a live account (USD also without a spot); Pct, of the
// starting equity, is nil when the equity is unknown.
type AttributionRow struct {
	Key    string   `json:"key"`
	Name   string   `json:"name"`
	Closes int      `json:"closes"`
	Pct    *float64 `json:"pct"`
	Coin   *float64 `json:"coin,omitempty"`
	USD    *float64 `json:"usd,omitempty"`
}

// Attribution is the section. Rows sum to Net, which is Live.Realised.
type Attribution struct {
	Label        string           `json:"label"`
	Source       string           `json:"source"`
	Account      string           `json:"account"` // testnet | live
	AmountsShown bool             `json:"amounts_shown"`
	Known        bool             `json:"known"`
	Note         string           `json:"note,omitempty"` // why not known
	Closes       int              `json:"closes"`
	Rows         []AttributionRow `json:"rows"`
	Net          AttributionRow   `json:"net"`
	OtherReasons []string         `json:"other_reasons"`
	StopSharePct *float64         `json:"stop_share_pct"` // % of the buy-back cost from stop-losses; nil without a cost
	TolerancePct float64          `json:"tolerance_pct"`
	Flagged      bool             `json:"flagged"` // unexplained over the tolerance
	// The week's sales, outside the sum: a leg's premium and opening fees
	// are booked when it closes, often in a later week.
	Sold     AttributionRow `json:"sold"`
	OpenFees AttributionRow `json:"open_fees"`
}

// attrSum adds up one row's coin and USD figures. rate ≤ 0 (no spot) leaves
// the USD unknown for the whole section, so a part is never silently missing.
type attrSum struct {
	coin, usd float64
	closes    int
}

func (s *attrSum) add(coin, rate float64) {
	s.coin += coin
	s.usd += coin * rate
	s.closes++
}

// attrRate is the USD per coin of a trade: its own spot, else what its USD
// P&L implies, else the history's spot.
func attrRate(t orders.Trade, points []history.Point) float64 {
	if t.Spot > 0 {
		return t.Spot
	}
	if t.PnL != 0 && t.PnLUSD != 0 {
		return t.PnLUSD / t.PnL
	}
	return spotAt(points, t.At)
}

// buildAttribution is the pure section over the week's trades; booked is the
// report's realised result (Live.Realised).
func buildAttribution(in Inputs, booked float64) Attribution {
	a := Attribution{Label: AttributionLabel, Account: accountOf(in.Account), TolerancePct: AttributionTolerancePct,
		Rows: []AttributionRow{}, OtherReasons: []string{},
		Source: in.Journal + " (closes: premium, fill price × quantity, fees; opens)"}
	a.AmountsShown = a.Account != AccountLive
	if in.JournalNote != "" {
		a.Note = "the journal was not read: " + in.JournalNote
		return a
	}
	a.Known = true

	var premium, fees, gap, sold, openFees attrSum
	exits := map[string]*attrSum{}
	usdKnown := true
	other := map[string]bool{}
	netUSD := 0.0
	for _, t := range in.Trades {
		if t.At.Before(in.Window.From) || !t.At.Before(in.Window.To) {
			continue
		}
		rate := attrRate(t, in.Points)
		usdKnown = usdKnown && rate > 0
		switch t.Kind {
		case "open":
			sold.add(t.Premium, rate)
			openFees.add(-t.OpenFee, rate)
		case "close":
			a.Closes++
			netUSD += t.PnL * rate
			cost := t.Price * t.Qty
			premium.add(t.Premium, rate)
			fees.add(-t.Fees, rate)
			key := orders.CloseReasonLabel(t.Reason)
			if !isAttrExit(key) {
				if key != "" {
					other[key] = true
				}
				key = AttrOther
			}
			if exits[key] == nil {
				exits[key] = &attrSum{}
			}
			exits[key].add(-cost, rate)
			if d := t.PnL - (t.Premium - cost - t.Fees); math.Abs(d) > attrZero {
				gap.add(d, rate)
			}
		}
	}

	row := func(key, name string, s attrSum) AttributionRow {
		return attrRow(key, name, s, in.Equity, a.AmountsShown, usdKnown)
	}
	a.Rows = append(a.Rows, row(AttrPremium, "premium of the legs closed", premium))
	explained, totalCost, stopCost := premium.coin+fees.coin, 0.0, 0.0
	for _, e := range attrExits {
		s := attrSum{}
		if exits[e.reason] != nil {
			s = *exits[e.reason]
		}
		a.Rows = append(a.Rows, row(e.reason, e.name, s))
		explained += s.coin
		totalCost -= s.coin
		if e.reason == orders.CloseReasonLabel(orders.TriggerStopLoss200Pct) {
			stopCost = -s.coin
		}
	}
	a.Rows = append(a.Rows, row(AttrFees, "fees (opening share + closing fee)", fees))
	gap.coin = booked - explained
	if math.Abs(gap.coin) <= attrZero {
		gap = attrSum{}
	}
	a.Rows = append(a.Rows, row(AttrUnexplained, "unexplained", gap))
	a.Net = row(AttrNet, "net: the realised result", attrSum{coin: booked, usd: netUSD, closes: a.Closes})
	a.Sold = row("sold", "premium sold on the week's opens", sold)
	a.OpenFees = row("open_fees", "their opening fees", openFees)

	for k := range other {
		a.OtherReasons = append(a.OtherReasons, k)
	}
	sort.Strings(a.OtherReasons)
	if totalCost > 0 {
		a.StopSharePct = ptr(100 * stopCost / totalCost)
	}
	if in.Equity > 0 {
		a.Flagged = math.Abs(gap.coin) > in.Equity*AttributionTolerancePct/100+attrZero
	}
	return a
}

func isAttrExit(reason string) bool {
	for _, e := range attrExits {
		if e.reason == reason {
			return true
		}
	}
	return false
}

func attrRow(key, name string, s attrSum, equity float64, amounts, usdKnown bool) AttributionRow {
	r := AttributionRow{Key: key, Name: name, Closes: s.closes}
	if equity > 0 {
		r.Pct = ptr(100 * s.coin / equity)
	}
	if amounts {
		r.Coin = ptr(s.coin)
		if usdKnown {
			r.USD = ptr(s.usd)
		}
	}
	return r
}

// writeAttribution writes the section.
func writeAttribution(p func(string, ...any), a Attribution, coin string) {
	p("## Where the result came from\n\n")
	p("How the week's realised result splits: the premium of the legs closed, what buying them back cost by exit reason, and the fees. " +
		"The figures are read from each close's journal line, so what does not add up shows as unexplained, never hidden. Percentages of the starting equity. ")
	if a.AmountsShown {
		p("USD at each close's own spot. ")
	}
	p("A live Deribit account shows percentages only. Source: %s.\n\n", a.Source)
	if !a.Known {
		p("- **Not available**: %s.\n\n", a.Note)
		return
	}
	if a.Closes == 0 {
		p("- **No closes in the week**: nothing was booked, so the rows below are zero.\n\n")
	}
	if a.AmountsShown {
		p("| | Closes | %s | USD | %% of equity |\n|---|---|---|---|---|\n", coin)
	} else {
		p("| | Closes | %% of equity |\n|---|---|---|\n")
	}
	line := func(r AttributionRow, bold bool) {
		mark := ""
		if bold {
			mark = "**"
		}
		if a.AmountsShown {
			p("| %s%s%s | %d | %+.4f | %s | %s |\n", mark, r.Name, mark, r.Closes, *r.Coin, attrUSD(r), attrPct(r))
		} else {
			p("| %s%s%s | %d | %s |\n", mark, r.Name, mark, r.Closes, attrPct(r))
		}
	}
	for _, r := range a.Rows {
		line(r, false)
	}
	line(a.Net, true)
	p("\n")
	if len(a.OtherReasons) > 0 {
		p("- other buy-backs: %s\n", strings.Join(a.OtherReasons, ", "))
	}
	if a.StopSharePct != nil {
		p("- stop-losses' share of the buy-back cost: **%.1f %%**\n", *a.StopSharePct)
	} else {
		p("- stop-losses' share of the buy-back cost: no buy-back cost in the week\n")
	}
	if a.AmountsShown {
		p("- sold in the week, outside the sum (booked when each leg closes): %d leg(s), premium %+.4f %s (%s %% of equity), opening fees %+.4f %s (%s %%)\n",
			a.Sold.Closes, *a.Sold.Coin, coin, attrPct(a.Sold), *a.OpenFees.Coin, coin, attrPct(a.OpenFees))
	} else {
		p("- sold in the week, outside the sum (booked when each leg closes): %d leg(s), premium %s %% of equity, opening fees %s %%\n",
			a.Sold.Closes, attrPct(a.Sold), attrPct(a.OpenFees))
	}
	if a.Flagged {
		p("- **flagged: unexplained over the %.1f %% tolerance**. Closes' premium, buy-back and fees do not add up to their booked P&L\n", a.TolerancePct)
	}
	p("\n")
}

func attrUSD(r AttributionRow) string {
	if r.USD == nil {
		return "unknown"
	}
	return fmt.Sprintf("%+.0f", *r.USD)
}

func attrPct(r AttributionRow) string {
	if r.Pct == nil {
		return "unknown"
	}
	return fmt.Sprintf("%+.2f", *r.Pct)
}
