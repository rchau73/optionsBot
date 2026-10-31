package evidence

import (
	"fmt"
	"math"
	"strings"
	"time"

	"optionsbot/internal/history"
	"optionsbot/internal/orders"
)

// The account reconciliation section (#566): the change of the account's
// margin balance over the week (the P&L history's lines, #231) next to the
// P&L the bot booked in its journal over the same span, net of fees, and the
// gap between the two. A gap over ReconcileTolerancePct of the starting
// balance is flagged with the causes it can have. Measured on the account,
// never a forecast. A live Deribit account shows percentages only.

// ReconcileLabel names the section.
const ReconcileLabel = "account reconciliation (measured on the account, not a forecast)"

// ReconcileTolerancePct is the largest gap, in % of the starting margin
// balance, left unflagged.
const ReconcileTolerancePct = 1.0

// ReconcileCauses are what a flagged gap can stand for, in plain words.
var ReconcileCauses = []string{
	"a deposit or withdrawal on the exchange",
	"unrealised P&L of open legs, in the account's balance but not yet booked",
	"funding or fees the journal did not record",
	"a position the bot's book missed",
}

// reconcileCauseUSD is added when the balance is in USD (cross collateral).
const reconcileCauseUSD = "the USD value of the collateral as prices move"

// Reconciliation is the section. The amount fields are nil for a live
// account; the percentages (of EquityStart) are set whenever Known.
type Reconciliation struct {
	Label        string     `json:"label"`
	Source       string     `json:"source"`
	Account      string     `json:"account"` // testnet | live
	AmountsShown bool       `json:"amounts_shown"`
	Known        bool       `json:"known"`
	Note         string     `json:"note,omitempty"` // why not known
	Unit         string     `json:"unit,omitempty"` // the balance's unit: the coin, or USD under cross collateral
	Start        *time.Time `json:"start,omitempty"`
	End          *time.Time `json:"end,omitempty"`
	EquityPoints int        `json:"equity_points"` // history points in the week carrying the margin balance
	EquityStart  *float64   `json:"equity_start,omitempty"`
	EquityEnd    *float64   `json:"equity_end,omitempty"`
	Change       *float64   `json:"change,omitempty"` // EquityEnd − EquityStart
	Booked       *float64   `json:"booked,omitempty"` // Σ journal close P&L in (Start, End], net of fees, in Unit
	Gap          *float64   `json:"gap,omitempty"`    // Change − Booked
	ChangePct    float64    `json:"change_pct"`
	BookedPct    float64    `json:"booked_pct"`
	GapPct       float64    `json:"gap_pct"`
	Closes       int        `json:"closes"` // journal closes booked in the span
	TolerancePct float64    `json:"tolerance_pct"`
	Flagged      bool       `json:"flagged"`
	Causes       []string   `json:"causes"`       // possible causes, when flagged
	MissingDays  []string   `json:"missing_days"` // UTC days of the week without a balance point
}

// BuildReconciliation is the pure section over the window w: the first and
// last history point in w carrying a margin balance, and the journal's
// closes after the first up to the last. Under a USD balance each close is
// valued at its own spot (the journal's pnl_usd, else the history's spot).
func BuildReconciliation(points []history.Point, trades []orders.Trade, w Window, account, historySrc, journalSrc string) Reconciliation {
	r := Reconciliation{Label: ReconcileLabel, Account: accountOf(account), TolerancePct: ReconcileTolerancePct,
		Causes: []string{}, MissingDays: []string{},
		Source: historySrc + " (margin balance on each line) and " + journalSrc + " (closes, net of fees)"}
	r.AmountsShown = r.Account != AccountLive

	var in []history.Point
	var times []time.Time
	for _, p := range points {
		if p.MarginBalance != nil && !p.T.Before(w.From) && p.T.Before(w.To) {
			in = append(in, p)
			times = append(times, p.T)
		}
	}
	r.EquityPoints = len(in)
	if days := missingDays(w, times); days != nil {
		r.MissingDays = days
	}
	if len(in) < 2 {
		r.Note = fmt.Sprintf("not enough history: %d margin-balance point(s) in the week, two are needed", len(in))
		return r
	}
	a, z := in[0], in[len(in)-1]
	if a.Unit != z.Unit {
		r.Note = fmt.Sprintf("the balance's unit changed in the week (%s → %s): cross collateral was switched", a.Unit, z.Unit)
		return r
	}
	start, end := *a.MarginBalance, *z.MarginBalance
	if start <= 0 {
		r.Note = "the margin balance at the start is not positive: no percentage of it"
		return r
	}
	usd := strings.EqualFold(a.Unit, "USD")
	booked := 0.0
	for _, t := range trades {
		if t.Kind != "close" || !t.At.After(a.T) || t.At.After(z.T) {
			continue
		}
		r.Closes++
		if !usd {
			booked += t.PnL
		} else if t.PnLUSD != 0 || t.PnL == 0 {
			booked += t.PnLUSD
		} else {
			booked += t.PnL * spotAt(points, t.At)
		}
	}
	change := end - start
	gap := change - booked
	r.Known, r.Unit = true, a.Unit
	r.Start, r.End = &a.T, &z.T
	r.ChangePct, r.BookedPct, r.GapPct = 100*change/start, 100*booked/start, 100*gap/start
	if math.Abs(r.GapPct) > ReconcileTolerancePct {
		r.Flagged = true
		r.Causes = append(r.Causes, ReconcileCauses...)
		if usd {
			r.Causes = append(r.Causes, reconcileCauseUSD)
		}
	}
	if r.AmountsShown {
		r.EquityStart, r.EquityEnd, r.Change, r.Booked, r.Gap = ptr(start), ptr(end), ptr(change), ptr(booked), ptr(gap)
	}
	return r
}

// spotAt is the spot of the last history point at or before t (the first
// point with a spot when none is before it); 0 without any.
func spotAt(points []history.Point, t time.Time) float64 {
	spot := 0.0
	for _, p := range points {
		if p.Spot <= 0 {
			continue
		}
		if p.T.After(t) {
			if spot == 0 {
				spot = p.Spot
			}
			break
		}
		spot = p.Spot
	}
	return spot
}

// writeReconciliation writes the section.
func writeReconciliation(p func(string, ...any), r Reconciliation) {
	p("## Account reconciliation\n\n")
	p("Measured on the account, not a forecast: the change of Deribit's margin balance between the first and last P&L history line of the week that carries it, "+
		"next to the P&L the journal booked between those two lines (closes, net of fees). Gap = balance change − booked P&L. "+
		"A gap over %.1f %% of the starting balance is flagged. A live Deribit account shows percentages only. Source: %s.\n\n", r.TolerancePct, r.Source)
	if len(r.MissingDays) == 0 {
		p("- days without a balance point: none\n")
	} else {
		p("- days without a balance point: **%d** — %s\n", len(r.MissingDays), strings.Join(r.MissingDays, ", "))
	}
	if !r.Known {
		p("- **%s**. No gap figure.\n\n", strings.ToUpper(r.Note[:1])+r.Note[1:])
		return
	}
	amt := func(v float64) string {
		if strings.EqualFold(r.Unit, "USD") {
			return fmt.Sprintf("%+.0f USD", v)
		}
		return fmt.Sprintf("%+.4f %s", v, r.Unit)
	}
	p("- %s → %s UTC, %d balance points, %d close(s) booked\n", r.Start.UTC().Format("Jan 02 15:04"), r.End.UTC().Format("Jan 02 15:04"), r.EquityPoints, r.Closes)
	if r.AmountsShown {
		p("- margin balance %s → %s: change %s (%+.2f %%)\n", strings.TrimPrefix(amt(*r.EquityStart), "+"), strings.TrimPrefix(amt(*r.EquityEnd), "+"), amt(*r.Change), r.ChangePct)
		p("- booked P&L, net of fees: %s (%+.2f %%)\n", amt(*r.Booked), r.BookedPct)
		p("- **gap %s = %+.2f %% of the starting balance**", amt(*r.Gap), r.GapPct)
	} else {
		p("- margin balance change: %+.2f %% · booked P&L, net of fees: %+.2f %%\n", r.ChangePct, r.BookedPct)
		p("- **gap %+.2f %% of the starting balance**", r.GapPct)
	}
	if !r.Flagged {
		p(" — within the %.1f %% tolerance\n\n", r.TolerancePct)
		return
	}
	p(" — **flagged: over the %.1f %% tolerance**. The account moved by more than the bot booked. Possible causes: %s.\n\n",
		r.TolerancePct, strings.Join(r.Causes, "; "))
}
