package evidence

import (
	"fmt"
	"math"
	"strings"
	"time"

	"optionsbot/internal/deployeffect"
	"optionsbot/internal/idlemargin"
	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
	"optionsbot/internal/replay"
)

// The "replay vs live" section (#488): the replay of the week next to what
// the journal shows, side by side — result in coin and USD, fees, trades and
// worst day — and the gap in coin and as a percentage of the live result.
// It reuses the Replay and Gap sections' figures (no second replay). A week
// whose books do not cover it is listed as not comparable, with its missing
// hours, and no gap is computed. Causes are listed only when the journal
// shows them, never guessed. A live Deribit account shows percentages only.

// ReplayVsLiveLabel names the section.
const ReplayVsLiveLabel = "replay vs live"

// Accounts (Inputs.Account, DeribitAccount).
const (
	AccountTestnet = "testnet"
	AccountLive    = "live"
)

// ReplayVsLive is the section. Amount fields (coin, USD) are nil for a live
// account; the percentages (of the starting equity) are always set when the
// week is comparable.
type ReplayVsLive struct {
	Label        string    `json:"label"`
	Account      string    `json:"account"` // testnet | live
	AmountsShown bool      `json:"amounts_shown"`
	Comparable   bool      `json:"comparable"`
	Note         string    `json:"note,omitempty"` // why not comparable
	MissingHours *float64  `json:"missing_hours,omitempty"`
	Gaps         []LongGap `json:"gaps"` // the market books' gaps in the week
	Source       string    `json:"source"`
	SpotUSD      *float64  `json:"spot_usd,omitempty"` // the spot the USD figures use (the replay's last book)
	Replay       RVLSide   `json:"replay"`
	Live         RVLSide   `json:"live"`
	GapCoin      *float64  `json:"gap,omitempty"`     // replay − live, coin
	GapUSD       *float64  `json:"gap_usd,omitempty"` // at SpotUSD
	GapEquityPP  *float64  `json:"gap_equity_pp,omitempty"`
	GapPct       *float64  `json:"gap_pct_of_live,omitempty"` // gap ÷ |live|; nil when live is 0
	Causes       []string  `json:"causes"`                    // what the journal shows in the week
	Unmodelled   []string  `json:"unmodelled"`
}

// RVLSide is one side's week.
type RVLSide struct {
	Net          *float64 `json:"net,omitempty"` // coin, net of fees
	NetUSD       *float64 `json:"net_usd,omitempty"`
	Fees         *float64 `json:"fees,omitempty"`
	WorstDay     *float64 `json:"worst_day,omitempty"`
	NetPct       float64  `json:"net_pct"` // of the starting equity
	FeesPct      float64  `json:"fees_pct"`
	WorstDayPct  float64  `json:"worst_day_pct"`
	WorstDayDate string   `json:"worst_day_date,omitempty"`
	Trades       int      `json:"trades"` // filled legs (opens and closes)
}

// buildReplayVsLive assembles the section from the report's Replay, Gap and
// Coverage and the replay's days. Pure.
func buildReplayVsLive(in Inputs, r Report, days []replay.Day) ReplayVsLive {
	s := ReplayVsLive{Label: ReplayVsLiveLabel, Account: accountOf(in.Account), Gaps: []LongGap{}, Causes: []string{},
		Source: "the Replay and Gap sections (" + in.Market + "; " + in.Journal + " marked at the recorded mids)"}
	s.AmountsShown = s.Account != AccountLive
	c := r.Coverage
	if c.Known {
		for _, d := range c.Datasets {
			if d.Dataset == "market" && d.Status == CoverageRecorded {
				h := d.MissingHours
				s.MissingHours = &h
				for _, g := range d.Gaps {
					s.Gaps = append(s.Gaps, LongGap{Dataset: d.Dataset, From: g.From, To: g.To, Hours: g.Hours})
				}
			}
		}
	}
	if s.Note = notComparable(r); s.Note != "" {
		return s
	}
	s.Comparable, s.Unmodelled = true, r.Gap.Unmodelled

	eq := in.Equity // > 0: the replay ran
	pct := func(v float64) float64 { return 100 * v / eq }
	rp, g := r.Replay, r.Gap
	window := snapsIn(in.Snaps, rp.Start, rp.End)
	spot := 0.0
	if len(window) > 0 {
		spot = window[len(window)-1].Spot()
	}

	rWorst, rDate := replayWorstDay(days)
	lWorst, lDate := liveWorstDay(in.Trades, window, days, rp.Start, rp.End)
	liveFees := closeFees(in.Trades, rp.Start, rp.End)
	s.Replay = RVLSide{NetPct: pct(rp.Net), FeesPct: pct(rp.Fees), WorstDayPct: pct(rWorst), WorstDayDate: rDate, Trades: rp.Trades}
	s.Live = RVLSide{NetPct: pct(g.Live.Net), FeesPct: pct(liveFees), WorstDayPct: pct(lWorst), WorstDayDate: lDate,
		Trades: g.Live.Opens + g.Live.Closes}
	gapPP := pct(g.Gap)
	s.GapEquityPP = &gapPP
	if g.Live.Net != 0 {
		v := 100 * g.Gap / math.Abs(g.Live.Net)
		s.GapPct = &v
	}
	if s.AmountsShown {
		s.Replay.Net, s.Replay.Fees, s.Replay.WorstDay = ptr(rp.Net), ptr(rp.Fees), ptr(rWorst)
		s.Live.Net, s.Live.Fees, s.Live.WorstDay = ptr(g.Live.Net), ptr(liveFees), ptr(lWorst)
		s.GapCoin = ptr(g.Gap)
		if spot > 0 {
			s.SpotUSD = ptr(spot)
			s.Replay.NetUSD, s.Live.NetUSD, s.GapUSD = ptr(rp.Net*spot), ptr(g.Live.Net*spot), ptr(g.Gap*spot)
		}
	}
	s.Causes = journalCauses(in, r)
	return s
}

// notComparable says why the week cannot be compared ("" when it can): the
// replay did not run, the coverage was not measured, or the market books
// left the week short (under ReplayFullCoveragePct of the hours, or a gap
// over LongGapHours).
func notComparable(r Report) string {
	c := r.Coverage
	switch {
	case !r.Replay.Ran:
		return "the replay did not run: " + r.Replay.Note
	case !c.Known:
		return "the recorded history was not measured: " + c.Note
	case c.MarketPct == nil:
		return "the market books were not recorded or not read"
	case *c.MarketPct < c.FullPct:
		return fmt.Sprintf("the market books cover %.1f %% of the week's hours, under %.0f %%", *c.MarketPct, c.FullPct)
	}
	for _, g := range c.LongGaps {
		if g.Dataset == "market" {
			return fmt.Sprintf("the market books have a gap over %.0f h (%s from %s UTC)", c.LongGapHours, hoursText(g.Hours), g.From.Format("2006-01-02 15:04"))
		}
	}
	return ""
}

// journalCauses are the gap's likely causes the journal shows in the week:
// entries frozen (the idle-margin section's frozen time), rebalance orders
// sent by a startup and closes a person asked for. Nothing else is named.
func journalCauses(in Inputs, r Report) []string {
	out := []string{}
	for _, c := range r.E1.Idle.Causes {
		if c.Cause == idlemargin.RowFrozen && c.Hours > 0 {
			out = append(out, fmt.Sprintf("entries frozen %.1f slot-hours (journal `risk_limit` lines)", c.Hours))
		}
	}
	if in.DeployLines != nil && in.Cfg != nil {
		rep := deployeffect.Analyze(in.DeployLines, in.Deploys, deployeffect.RulesFrom(in.Cfg), in.Window.From, in.Window.To)
		startups, lots := 0, 0.0
		for _, st := range rep.Startups {
			sent := false
			for _, o := range st.Orders {
				if o.Trigger == orders.TriggerRebalanceUpsize || o.Trigger == orders.TriggerRebalanceDownsize {
					sent, lots = true, lots+o.Lots
				}
			}
			if sent {
				startups++
			}
		}
		if startups > 0 {
			out = append(out, fmt.Sprintf("restart rebalance: %d startup(s) sent rebalance orders in their first 30 min (%.4g lots)", startups, lots))
		}
	}
	manual := 0
	for _, t := range in.Trades {
		if t.Actor != nil && !t.At.Before(in.Window.From) && t.At.Before(in.Window.To) {
			manual++
		}
	}
	if manual > 0 {
		out = append(out, fmt.Sprintf("manual action: %d close(s) asked from the monitor", manual))
	}
	return out
}

// replayWorstDay is the replay's day with the lowest P&L.
func replayWorstDay(days []replay.Day) (float64, string) {
	worst, date := 0.0, ""
	for i, d := range days {
		if i == 0 || d.PnL < worst {
			worst, date = d.PnL, d.Date.Format(time.DateOnly)
		}
	}
	return worst, date
}

// liveWorstDay is the live day with the lowest P&L, each of the replay's
// days marked as the gap is (replay.ActualPnL) from the book nearest its
// start to the book nearest its end, inside the replay's window.
func liveWorstDay(trades []orders.Trade, window []recorder.Snapshot, days []replay.Day, start, end time.Time) (float64, string) {
	worst, date := 0.0, ""
	for _, d := range days {
		from, to := utcDay(d.Date), utcDay(d.Date).AddDate(0, 0, 1)
		if from.Before(start) {
			from = start
		}
		if to.After(end) {
			to = end
		}
		day := snapsIn(window, from, to)
		if len(day) == 0 {
			continue
		}
		net := replay.ActualPnL(trades, window, from, to).Net
		if date == "" || net < worst {
			worst, date = net, d.Date.Format(time.DateOnly)
		}
	}
	return worst, date
}

// closeFees are the journal's close fees in [from, to].
func closeFees(trades []orders.Trade, from, to time.Time) float64 {
	f := 0.0
	for _, t := range trades {
		if t.Kind == "close" && !t.At.Before(from) && !t.At.After(to) {
			f += t.Fees
		}
	}
	return f
}

func snapsIn(snaps []recorder.Snapshot, from, to time.Time) []recorder.Snapshot {
	var out []recorder.Snapshot
	for _, s := range snaps {
		if !s.At.Before(from) && !s.At.After(to) {
			out = append(out, s)
		}
	}
	return out
}

func accountOf(a string) string {
	if a == AccountLive {
		return AccountLive
	}
	return AccountTestnet
}

// writeReplayVsLive writes the section.
func writeReplayVsLive(p func(string, ...any), s ReplayVsLive, coin string) {
	p("## Replay vs live\n\n")
	p("Method: the replay of the week (Replay section) next to the journal's result over the replay's window, marked at the recorded mids (Gap section); "+
		"both net of fees. Trades count filled legs, opens and closes. A day's live P&L runs from the book nearest its start to the book nearest its end. "+
		"USD at the replay's last recorded spot. The week is not comparable when the market books cover under %.0f %% of its hours or have a gap over %.0f h: "+
		"then no gap is computed. Causes are listed only when the journal shows them. A live Deribit account shows percentages only. Source: %s.\n\n",
		ReplayFullCoveragePct, LongGapHours, s.Source)
	if s.MissingHours != nil {
		p("- market books missing: %s", hoursText(*s.MissingHours))
		for _, g := range s.Gaps {
			p(" · %s → %s UTC (%s)", g.From.Format("2006-01-02 15:04"), g.To.Format("2006-01-02 15:04"), hoursText(g.Hours))
		}
		p("\n")
	}
	if !s.Comparable {
		p("- **Not comparable**: %s. No gap computed.\n\n", s.Note)
		return
	}
	if !s.AmountsShown {
		p("Live account: percentages of the starting equity only.\n\n")
		p("| | Result %% | Fees %% | Trades | Worst day %% |\n|---|---|---|---|---|\n")
		side := func(name string, v RVLSide) {
			p("| %s | %+.2f | %.2f | %d | %+.2f (%s) |\n", name, v.NetPct, v.FeesPct, v.Trades, v.WorstDayPct, v.WorstDayDate)
		}
		side("replay", s.Replay)
		side("live", s.Live)
	} else {
		p("| | Result %s | Result USD | Fees %s | Trades | Worst day %s |\n|---|---|---|---|---|---|\n", coin, coin, coin)
		side := func(name string, v RVLSide) {
			usd := "unknown"
			if v.NetUSD != nil {
				usd = fmt.Sprintf("%+.0f", *v.NetUSD)
			}
			p("| %s | %+.4f (%+.2f %%) | %s | %.4f | %d | %+.4f (%s) |\n", name, *v.Net, v.NetPct, usd, *v.Fees, v.Trades, *v.WorstDay, v.WorstDayDate)
		}
		side("replay", s.Replay)
		side("live", s.Live)
	}
	p("\n- **gap (replay − live)**:")
	if s.GapCoin != nil {
		p(" %+.4f %s", *s.GapCoin, coin)
		if s.GapUSD != nil {
			p(" ($%+.0f)", *s.GapUSD)
		}
		p(" ·")
	}
	p(" %+.2f pp of equity", *s.GapEquityPP)
	if s.GapPct != nil {
		p(" · **%+.1f %% of the live result**\n", *s.GapPct)
	} else {
		p(" · the live result is 0: no percentage of it\n")
	}
	if len(s.Causes) == 0 {
		p("- likely causes in the journal: none shown\n")
	} else {
		p("- likely causes in the journal: %s\n", strings.Join(s.Causes, "; "))
	}
	p("- not modelled by the replay: %s\n\n", strings.Join(s.Unmodelled, "; "))
}
