package evidence

import (
	"sort"
	"time"

	"optionsbot/internal/history"
	"optionsbot/internal/perf"
)

// The track record (#279) and the evidence trend (#323) join the weekly
// reports up: one row per ISO week, read from data/evidence/<week>/<bot>.json
// as written — a week without a report is marked, never recomputed — and,
// for the track record, the whole span of the P&L history through
// perf.Summarize. Both are pure: the same files give the same JSON.

// TrackDisclaimer heads every track record and trend (the spirit of
// compare.Disclaimer).
const TrackDisclaimer = "Testnet and replayed results, not a promise of future returns"

// InceptionNote says what "since inception" means: --reset-history archives
// the P&L history, so the span starts at the current file's first line.
const InceptionNote = "since inception = since the first line of the current P&L history; `--reset-history` archives older lines to data/archive/, which this report does not read"

// Cell notes of a week row.
const (
	NotReported = "not reported" // no weekly report for the week
	NotMeasured = "not measured" // the report predates the field
)

// WeekRow is one ISO week. Percentages are of the margin balance at that
// week's start (the weekly report's Equity); nil = unknown, and NotMeasured
// names the fields its report does not carry at all (an older format).
// Amounts (coin) are omitted from the trend.
type WeekRow struct {
	Week     string    `json:"week"`
	From     time.Time `json:"from"`
	Reported bool      `json:"reported"`
	Note     string    `json:"note,omitempty"` // NotReported

	EquityKnown bool     `json:"equity_known"`
	Net         *float64 `json:"net,omitempty"`          // coin, the P&L history's net (perf)
	NetPct      *float64 `json:"net_pct"`                // in coin, % of equity
	NetUSDPct   *float64 `json:"net_usd_pct"`            // the strategy in USD
	HoldPct     *float64 `json:"hold_usd_pct"`           // holding the coin, USD
	ExcessPP    *float64 `json:"excess_pp"`              // NetUSDPct − HoldPct
	MaxDD       *float64 `json:"max_drawdown,omitempty"` // coin
	MaxDDPct    *float64 `json:"max_drawdown_pct"`
	ROMLivePct  *float64 `json:"rom_live_pct"`
	ROMReplPct  *float64 `json:"rom_replay_pct"`
	Gap         *float64 `json:"gap,omitempty"` // replay − live, coin
	GapPct      *float64 `json:"gap_pct"`       // of equity
	UtilPct     *float64 `json:"utilisation_pct"`
	MissingDays []string `json:"missing_days"` // UTC days of the week with no P&L history point
	NotMeasured []string `json:"not_measured,omitempty"`
}

// Counts are facts over the reported weeks: no forecast.
type Counts struct {
	Reported      int      `json:"weeks_reported"`
	NotReported   int      `json:"weeks_not_reported"`
	BeatHold      int      `json:"weeks_beating_hold"`    // excess over holding > 0
	HoldKnown     int      `json:"weeks_with_hold_known"` // the base of BeatHold
	Lost          int      `json:"weeks_lost"`            // net < 0 (coin, or % when amounts are omitted)
	LongestLosing int      `json:"longest_losing_streak"` // consecutive lost weeks; a week not reported ends a streak
	WorstWeek     string   `json:"worst_week,omitempty"`  // the reported week with the lowest net
	WorstWeekPct  *float64 `json:"worst_week_pct"`        // its net % (nil = equity unknown)
}

// TrackRecord is the whole life of one bot's P&L history.
type TrackRecord struct {
	Disclaimer string    `json:"disclaimer"`
	Inception  string    `json:"inception"`
	Env        string    `json:"env"`
	Bot        string    `json:"bot"`
	Account    string    `json:"account"` // live | testnet (the env's .env DERIBIT_ENV)
	History    string    `json:"history"`
	Weekly     string    `json:"weekly_source"`
	Known      bool      `json:"known"` // the history has a point
	From       time.Time `json:"from"`  // first history point
	To         time.Time `json:"to"`    // last history point

	Equity       float64 `json:"equity"` // coin, at the start; 0 = unknown
	EquitySource string  `json:"equity_source"`

	Perf        perf.Summary             `json:"perf"`
	ROM         perf.MarginReturn        `json:"return_on_margin"`
	Util        history.UtilisationStats `json:"utilisation"`
	MissingDays []string                 `json:"missing_days"`

	Weeks      []WeekRow `json:"weeks"`
	Counts     Counts    `json:"counts"`
	Unreadable []string  `json:"unreadable,omitempty"` // weekly files that did not parse
}

// Trend is the week-over-week table of the weekly reports, percentages only
// (a bot may run live): from the first report to the last, every ISO week
// between listed, those without a file as not reported.
type Trend struct {
	Disclaimer string    `json:"disclaimer"`
	Env        string    `json:"env"`
	Bot        string    `json:"bot"`
	Weekly     string    `json:"weekly_source"`
	Weeks      []WeekRow `json:"weeks"`
	Counts     Counts    `json:"counts"`
	Unreadable []string  `json:"unreadable,omitempty"`
}

// TrackInputs are what TrackRecord reads, already loaded.
type TrackInputs struct {
	Env, Bot, Account string
	Points            []history.Point // the P&L history, any order
	History           string
	Weekly            []Report // the weekly reports found, any order
	WeeklySource      string
	Unreadable        []string
}

// BuildTrackRecord is the pure track record. The span is the first to the
// last history point; the equity at its start is the earliest weekly
// report's that carries one (named in EquitySource), else unknown.
func BuildTrackRecord(in TrackInputs) TrackRecord {
	t := TrackRecord{Disclaimer: TrackDisclaimer, Inception: InceptionNote, Env: in.Env, Bot: in.Bot,
		Account: in.Account, History: in.History, Weekly: in.WeeklySource, Unreadable: in.Unreadable,
		MissingDays: []string{}, Weeks: []WeekRow{}}
	weekly := byWeek(in.Weekly)
	t.EquitySource = "no weekly report carries a starting margin balance"
	for _, r := range weekly {
		if r.Equity > 0 {
			t.Equity = r.Equity
			t.EquitySource = "the " + r.Week + " weekly report (" + r.EquitySrc + ")"
			break
		}
	}
	pts := append([]history.Point(nil), in.Points...)
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].T.Before(pts[j].T) })
	if len(pts) == 0 {
		t.Perf = perf.Summarize(nil, t.Equity, time.Time{}, time.Time{})
		t.ROM = perf.MarginReturn{Equity: t.Equity, Reason: "no P&L history point"}
		t.Counts = countWeeks(nil, true)
		return t
	}
	t.Known = true
	t.From, t.To = pts[0].T.UTC(), pts[len(pts)-1].T.UTC()
	t.Perf = perf.Summarize(pts, t.Equity, t.From, t.To)
	t.ROM = perf.ReturnOnMargin(pts, t.Equity, t.From, t.To)
	t.Util = history.Utilisation(pts, t.From, t.To)
	t.MissingDays = dates(t.Perf.MissingDays)

	reports := map[string]Report{}
	for _, r := range weekly {
		reports[r.Week] = r
	}
	for monday := isoMonday(t.From); !monday.After(t.To); monday = monday.AddDate(0, 0, 7) {
		t.Weeks = append(t.Weeks, rowFor(monday, reports))
	}
	t.Counts = countWeeks(t.Weeks, true)
	return t
}

// BuildTrend is the pure trend over the weekly reports: one row per ISO week
// from the first report to the last, amounts left out.
func BuildTrend(env, bot, source string, weekly []Report, unreadable []string) Trend {
	tr := Trend{Disclaimer: TrackDisclaimer, Env: env, Bot: bot, Weekly: source, Weeks: []WeekRow{}, Unreadable: unreadable}
	ws := byWeek(weekly)
	if len(ws) > 0 {
		reports := map[string]Report{}
		for _, r := range ws {
			reports[r.Week] = r
		}
		last := ws[len(ws)-1].From
		for monday := isoMonday(ws[0].From); !monday.After(last); monday = monday.AddDate(0, 0, 7) {
			row := rowFor(monday, reports)
			row.Net, row.MaxDD, row.Gap = nil, nil, nil
			tr.Weeks = append(tr.Weeks, row)
		}
	}
	tr.Counts = countWeeks(tr.Weeks, false)
	return tr
}

// byWeek keeps the reports of an ISO week (a -from/-to report is not one),
// the last given for a week, ordered by week.
func byWeek(reports []Report) []Report {
	seen := map[string]int{}
	var out []Report
	for _, r := range reports {
		w, err := ParseWeek(r.Week)
		if err != nil || w.Label != r.Week {
			continue
		}
		r.From = w.From
		if i, ok := seen[r.Week]; ok {
			out[i] = r
			continue
		}
		seen[r.Week] = len(out)
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].From.Before(out[j].From) })
	return out
}

func isoMonday(t time.Time) time.Time {
	d := utcDay(t)
	return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
}

// rowFor reads the week starting monday from its report, if any. A section
// whose source is empty was not written by that report's version.
func rowFor(monday time.Time, reports map[string]Report) WeekRow {
	label := weekLabel(monday)
	row := WeekRow{Week: label, From: monday, MissingDays: []string{}}
	r, ok := reports[label]
	if !ok {
		row.Note = NotReported
		return row
	}
	row.Reported = true
	s := r.Live.Perf
	row.EquityKnown = s.EquityKnown
	if s.Points > 0 {
		row.Net, row.MaxDD = ptr(s.NetPnL), ptr(s.MaxDrawdown)
		if s.EquityKnown {
			row.NetPct, row.MaxDDPct = ptr(s.ReturnPct), ptr(s.MaxDrawdownPct)
		}
	}
	if r.Hold.Source == "" {
		row.NotMeasured = append(row.NotMeasured, "hold")
	} else if r.Hold.Known {
		row.HoldPct = ptr(r.Hold.HoldReturnUSDPct)
		if r.Hold.StrategyKnown {
			row.NetUSDPct, row.ExcessPP = ptr(r.Hold.ReturnUSDPct), ptr(r.Hold.ExcessReturnPct)
		}
	}
	if r.ROM.Label == "" {
		row.NotMeasured = append(row.NotMeasured, "rom_live", "rom_replay")
	} else {
		if r.ROM.Live.Known {
			row.ROMLivePct = ptr(r.ROM.Live.ReturnPct)
		}
		if r.ROM.Replay != nil && r.ROM.Replay.Known {
			row.ROMReplPct = ptr(r.ROM.Replay.ReturnPct)
		}
	}
	if r.Gap.Source == "" {
		row.NotMeasured = append(row.NotMeasured, "gap")
	} else if r.Gap.Known {
		row.Gap = ptr(r.Gap.Gap)
		row.GapPct = pct(r.Gap.Gap, r.Equity)
	}
	if r.Util.Source == "" {
		row.NotMeasured = append(row.NotMeasured, "utilisation")
	} else if r.Util.Stats.MeanPct != nil {
		row.UtilPct = ptr(*r.Util.Stats.MeanPct)
	}
	if r.Missing.HistoryDays != nil {
		row.MissingDays = r.Missing.HistoryDays
	}
	return row
}

// countWeeks counts the reported rows. byCoin ranks losses by the coin net
// (the track record); without amounts (the trend) by the net %.
func countWeeks(rows []WeekRow, byCoin bool) Counts {
	c := Counts{}
	streak := 0
	var worst *float64
	for _, r := range rows {
		if !r.Reported {
			c.NotReported++
			streak = 0
			continue
		}
		c.Reported++
		if r.ExcessPP != nil {
			c.HoldKnown++
			if *r.ExcessPP > 0 {
				c.BeatHold++
			}
		}
		net := r.NetPct
		if byCoin {
			net = r.Net
		}
		if net == nil {
			streak = 0
			continue
		}
		if *net < 0 {
			c.Lost++
			streak++
			if streak > c.LongestLosing {
				c.LongestLosing = streak
			}
		} else {
			streak = 0
		}
		if worst == nil || *net < *worst {
			worst = net
			c.WorstWeek, c.WorstWeekPct = r.Week, r.NetPct
		}
	}
	return c
}

func ptr(v float64) *float64 { return &v }
