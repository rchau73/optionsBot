package evidence

import (
	"fmt"
	"strings"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/hedge"
	"optionsbot/internal/history"
)

// The futures hedge trial section (#678): how the hedge experiment (#657) is
// doing against the GEX shed, read from the benchmark's ledger through
// hedge.Summary and never recomputed. The ledger is cumulative and keeps no
// daily series, so the section covers the days since the ledger began, as
// the ledger stood when read, and its worst day is the worst day of shed
// decisions. Testnet and shadow-based, not a result: under
// HedgeTrialMinDays it shows no figure, and a live account shows days and
// counts only.

// HedgeTrialLabel names the section.
const HedgeTrialLabel = "futures hedge trial (testnet figures, a short shadow-based comparison, not a result)"

// HedgeTrialMinDays is how many days the ledger needs before any figure shows.
const HedgeTrialMinDays = 7

// The section's states.
const (
	HedgeTrialNotRead    = "not read"        // the mode file cannot be read
	HedgeTrialNotStarted = "not started"     // nobody has switched the hedge on
	HedgeTrialTooFew     = "not enough days" // under HedgeTrialMinDays
	HedgeTrialNoSpot     = "no spot"         // no P&L history spot to mark the ledger at
	HedgeTrialMeasured   = "measured"
)

// The mode now.
const (
	HedgeModeHedge = "hedge"
	HedgeModeShed  = "shed"
)

// HedgeInputs are the section's inputs (LoadHedge).
type HedgeInputs struct {
	Path  string          // the mode file
	State hedge.ModeState // its contents; the zero state = no file
	Err   string          // why the file was not read
	Now   time.Time       // when it was read: the days are counted up to here
	// Events are the journal's hedge lines since the ledger began
	// (ReadHedgeEvents); nil when not read, EventsErr says why.
	Events    *HedgeEvents
	EventsErr string
}

// HedgeWorstDay is the UTC day whose shed decisions are worth least to the hedge.
type HedgeWorstDay struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"` // Σ Counterfactual.Value of that day's decisions
	Legs  int     `json:"legs"`
}

// HedgeTrial is the section. Amounts are coin and nil unless Status is
// measured on a testnet account; counts are nil under HedgeTrialMinDays.
type HedgeTrial struct {
	Label        string     `json:"label"`
	Source       string     `json:"source"`
	Account      string     `json:"account"` // testnet | live
	AmountsShown bool       `json:"amounts_shown"`
	Status       string     `json:"status"`
	Note         string     `json:"note,omitempty"` // why there are no figures
	Mode         string     `json:"mode,omitempty"` // hedge | shed, now
	Since        *time.Time `json:"since,omitempty"`
	ReadAt       *time.Time `json:"read_at,omitempty"`
	Days         *int       `json:"days,omitempty"`         // whole days measured
	DaysMissing  *int       `json:"days_missing,omitempty"` // to HedgeTrialMinDays

	RealTrades  *int   `json:"real_trades,omitempty"`
	Unavailable *int   `json:"hedge_unavailable,omitempty"` // journal episodes; nil when not read
	RateLimited *int   `json:"hedge_rate_limited,omitempty"`
	EventsNote  string `json:"events_note,omitempty"` // why the journal's hedge events were not read
	PaperTrades *int   `json:"paper_trades,omitempty"`
	Legs        *int   `json:"counterfactuals,omitempty"`
	LegsOpen    *int   `json:"counterfactuals_open,omitempty"`

	Advantage   *float64       `json:"advantage,omitempty"` // > 0: the hedge did better than the shed
	HedgePnL    *float64       `json:"hedge_pnl,omitempty"` // real + paper, net of fees
	RealPnL     *float64       `json:"real_pnl,omitempty"`
	PaperPnL    *float64       `json:"paper_pnl,omitempty"`
	LegsValue   *float64       `json:"counterfactuals_value,omitempty"`
	Fees        *float64       `json:"fees,omitempty"` // real + paper
	RealFees    *float64       `json:"real_fees,omitempty"`
	PaperFees   *float64       `json:"paper_fees,omitempty"`
	WorstDay    *HedgeWorstDay `json:"worst_day,omitempty"`
	Spot        *float64       `json:"spot_usd,omitempty"` // the price the open parts are marked at
	Benchmark   string         `json:"benchmark_note,omitempty"`
	PricingNote string         `json:"pricing_note,omitempty"`

	// Verdict is docs/hedge-trial.md's rule over the figures above (#780):
	// nil when not started, not read, without a spot or on a live account.
	Verdict *HedgeVerdict `json:"verdict,omitempty"`
}

// LoadHedge reads the mode file; no file is the zero state, the trial not started.
func LoadHedge(path string, now time.Time) HedgeInputs {
	in := HedgeInputs{Path: path, Now: now}
	st, err := hedge.LoadMode(path)
	if err != nil {
		in.Err = err.Error()
		return in
	}
	in.State = st
	return in
}

// BuildHedgeTrial is the pure section as of in.Now. A bot nobody switched on
// has not started, whatever its paper hedge has booked (the mode's Since is
// set by the first switch and never cleared). The price is the P&L history's
// last spot at in.Now.
func BuildHedgeTrial(in HedgeInputs, points []history.Point, account string) HedgeTrial {
	t := HedgeTrial{Label: HedgeTrialLabel, Source: in.Path + " (hedge.Summary)", Account: accountOf(account)}
	t.AmountsShown = t.Account != AccountLive
	l := in.State.Ledger
	switch {
	case in.Err != "":
		t.Status, t.Note = HedgeTrialNotRead, in.Err
		return t
	case in.State.Since.IsZero() || l.Since.IsZero():
		t.Status, t.Note = HedgeTrialNotStarted, "the hedge has never been switched on"
		return t
	}
	t.Mode = HedgeModeShed
	if in.State.Enabled {
		t.Mode = HedgeModeHedge
	}
	since, readAt := l.Since, in.Now
	t.Since, t.ReadAt = &since, &readAt
	days := max(0, int(in.Now.Sub(l.Since)/(24*time.Hour)))
	t.Days = &days
	if days < HedgeTrialMinDays {
		missing := HedgeTrialMinDays - days
		t.Status, t.DaysMissing = HedgeTrialTooFew, &missing
		t.Note = fmt.Sprintf("%d day(s) still missing of the %d this comparison needs", missing, HedgeTrialMinDays)
		t.Verdict = hedgeTooFewVerdict(missing)
		return t
	}
	t.Status = HedgeTrialMeasured
	open := 0
	for _, c := range l.Legs {
		if !c.Done {
			open++
		}
	}
	legs := len(l.Legs)
	t.RealTrades, t.PaperTrades, t.Legs, t.LegsOpen = &l.Real.Trades, &l.Virtual.Trades, &legs, &open
	if ev := in.Events; ev != nil {
		t.Unavailable, t.RateLimited = ptrTo(ev.Unavailable), ptrTo(ev.RateLimited)
	} else {
		t.EventsNote = in.EventsErr
		if t.EventsNote == "" {
			t.EventsNote = "the journal was not read"
		}
	}
	if !t.AmountsShown {
		return t
	}
	spot := spotAt(points, in.Now)
	if spot <= 0 {
		t.Status, t.Note = HedgeTrialNoSpot, "no spot in the P&L history: the ledger cannot be marked"
		return t
	}
	// A testnet ledger is booked at the index and leaves funding out, so
	// neither needs a reading of the perpetual.
	s := hedge.PricingIndex.Summarise(l, 0, spot, spot)
	t.Advantage, t.HedgePnL, t.RealPnL, t.PaperPnL = ptr(s.AdvantageCoin), ptr(s.HedgeCoin), ptr(s.RealCoin), ptr(s.VirtualCoin)
	t.LegsValue, t.Spot = ptr(s.LegsCoin), ptr(spot)
	t.Fees, t.RealFees, t.PaperFees = ptr(l.Real.Fees+l.Virtual.Fees), ptr(l.Real.Fees), ptr(l.Virtual.Fees)
	t.WorstDay = hedgeWorstDay(l.Legs)
	t.Benchmark, t.PricingNote = s.Note, s.PricingNote
	t.Verdict = hedgeVerdict(t)
	return t
}

// hedgeWorstDay is the UTC day of shed decisions with the lowest total value
// (the earliest on a tie); nil without a decision.
func hedgeWorstDay(legs []hedge.Counterfactual) *HedgeWorstDay {
	byDate := map[string]*HedgeWorstDay{}
	for _, c := range legs {
		date := c.At.UTC().Format(time.DateOnly)
		d := byDate[date]
		if d == nil {
			d = &HedgeWorstDay{Date: date}
			byDate[date] = d
		}
		d.Value += c.Value()
		d.Legs++
	}
	var worst *HedgeWorstDay
	for _, d := range byDate {
		if worst == nil || d.Value < worst.Value || (d.Value == worst.Value && d.Date < worst.Date) {
			worst = d
		}
	}
	return worst
}

// writeHedgeTrial writes the section; unit is the coin.
func writeHedgeTrial(p func(string, ...any), t HedgeTrial, unit string) {
	p("## Futures hedge trial\n\n")
	if t.Account == AccountLive {
		p("Days and counts only: the hedge experiment is testnet-only and a live account shows no amount. Source: %s.\n\n", t.Source)
	} else {
		p("Testnet figures from a short, shadow-based comparison, not a result: only one approach trades the account at a time, the other is followed on paper. Source: %s.\n\n", t.Source)
	}
	switch t.Status {
	case HedgeTrialNotRead:
		p("**Not read**: %s. No figure.\n\n", t.Note)
		return
	case HedgeTrialNotStarted:
		p("**The trial has not started**: %s. No figure.\n\n", t.Note)
		return
	}
	p("- mode now: **%s** · ledger since %s UTC, read %s UTC\n", t.Mode, t.Since.UTC().Format("2006-01-02 15:04"), t.ReadAt.UTC().Format("2006-01-02 15:04"))
	switch t.Status {
	case HedgeTrialTooFew:
		p("- **%s**: %s. No figure yet.\n\n", HedgeTrialTooFew, t.Note)
		writeHedgeVerdict(p, t, unit)
		return
	case HedgeTrialNoSpot:
		p("- **%s**. No figure.\n\n", strings.ToUpper(t.Note[:1])+t.Note[1:])
		return
	}
	p("- every figure runs from the ledger's start to when it was read, not over this week alone\n\n")
	days := *t.Days
	p("| Figure | Value | Days measured |\n|---|---|---|\n")
	if t.AmountsShown {
		p("| Advantage: hedge P&L + counterfactual legs (positive: the hedge did better than the shed) | **%+.4f %s** | %d |\n", *t.Advantage, unit, days)
		p("| Hedge P&L, net of fees (real + paper) | %+.4f %s | %d |\n", *t.HedgePnL, unit, days)
		p("| Real perpetual, net of fees | %+.4f %s | %d |\n", *t.RealPnL, unit, days)
		p("| Paper hedge, net of fees | %+.4f %s | %d |\n", *t.PaperPnL, unit, days)
		p("| Counterfactual legs: what holding each shed leg was worth | %+.4f %s | %d |\n", *t.LegsValue, unit, days)
		p("| Fees (real %.4f + paper %.4f), already in the P&L | %.4f %s | %d |\n", *t.RealFees, *t.PaperFees, *t.Fees, unit, days)
		if w := t.WorstDay; w != nil {
			p("| Worst day of shed decisions (%s, %d leg(s)); the ledger keeps no daily hedge P&L | %+.4f %s | %d |\n", w.Date, w.Legs, w.Value, unit, days)
		} else {
			p("| Worst day of shed decisions | none: no shed decision | %d |\n", days)
		}
	}
	p("| Real trades | %d | %d |\n", *t.RealTrades, days)
	p("| Paper trades | %d | %d |\n", *t.PaperTrades, days)
	p("| Counterfactual legs still open | %d of %d | %d |\n", *t.LegsOpen, *t.Legs, days)
	p("| Real hedge orders an hour, mean (cap %d) | %.2f | %d |\n", config.FixedFuturesHedgeMaxPerHour, float64(*t.RealTrades)/float64(days*24), days)
	if t.Unavailable != nil {
		p("| Hedges that waited for the hourly cap (hedge_rate_limited) | %d | %d |\n", *t.RateLimited, days)
		p("| Episodes with the hedge unavailable, the shed back (hedge_unavailable) | %d | %d |\n\n", *t.Unavailable, days)
	} else {
		p("| Hedge events of the journal (hedge_unavailable, hedge_rate_limited) | not read: %s | %d |\n\n", t.EventsNote, days)
	}
	if t.AmountsShown {
		p("%s Open parts marked at the P&L history's last spot, %.0f USD; %s.\n\n", t.Benchmark, *t.Spot, t.PricingNote)
	}
	writeHedgeVerdict(p, t, unit)
}

// writeHedgeVerdict writes the verdict's one line with the figures it used;
// a live account (no amount to judge) gets none.
func writeHedgeVerdict(p func(string, ...any), t HedgeTrial, unit string) {
	v := t.Verdict
	if v == nil {
		p("No verdict: a live account shows no amount to judge (%s).\n\n", HedgeTrialRule)
		return
	}
	used := fmt.Sprintf("%d of the %d days needed", *t.Days, HedgeTrialMinDays)
	if t.Advantage != nil {
		events := "hedge events not read"
		if t.Unavailable != nil {
			events = fmt.Sprintf("%d hedge_unavailable, %d hedge_rate_limited", *t.Unavailable, *t.RateLimited)
		}
		worst := "no shed decision"
		if t.WorstDay != nil {
			worst = fmt.Sprintf("%+.4f %s", t.WorstDay.Value, unit)
		}
		used = fmt.Sprintf("advantage %+.4f %s, fees %.4f %s, worst day %s, %s, %d days",
			*t.Advantage, unit, *t.Fees, unit, worst, events, *t.Days)
	}
	p("**Verdict (%s): %s**: %s. Figures used: %s. A verdict changes no hedge mode, setting or config: a person decides.\n\n",
		v.Rule, v.Outcome, v.Why, used)
}
