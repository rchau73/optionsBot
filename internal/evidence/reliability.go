package evidence

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"optionsbot/internal/deployeffect"
	"optionsbot/internal/history"
	"optionsbot/internal/idlemargin"
	"optionsbot/internal/orders"
)

// The E1 reliability section (#361): E1's two outcome measures, every week.
// Margin use against the target (history.Utilisation, the figure already in
// Util — not recomputed), why approved margin sat idle (internal/idlemargin)
// and what each startup sent in its first 30 minutes (internal/deployeffect).
// Percentages, hours, counts and lots only: never an amount.

// E1TargetPct is E1's margin-use target: mean IM ÷ limit while entries were
// allowed (PRD E1, #174).
const E1TargetPct = 60.0

// Reliability is the E1 section.
type Reliability struct {
	TargetPct float64    `json:"target_pct"`
	Margin    E1Margin   `json:"margin_use"`
	Idle      E1Idle     `json:"idle_margin"`
	Restarts  E1Restarts `json:"restarts"`
}

// E1Margin is the week's margin use against the target.
type E1Margin struct {
	Known         bool     `json:"known"`
	Note          string   `json:"note,omitempty"` // why it is not measured
	Source        string   `json:"source"`
	MeanPct       *float64 `json:"mean_pct"`      // IM ÷ limit while entries were allowed, %
	AllowedShare  *float64 `json:"allowed_share"` // 0–1 of the measured time
	MeasuredHours float64  `json:"measured_hours"`
	TargetMet     *bool    `json:"target_met"` // nil when not measured
	MissingDays   []string `json:"missing_days"`
}

// E1Cause is one cause of idle slot time.
type E1Cause struct {
	Cause string  `json:"cause"` // a skip reason, frozen or no_skip_logged
	Hours float64 `json:"hours"`
	Share float64 `json:"share"` // of the idle hours measured, 0–1
}

// E1Book is the unused approved margin in one book state.
type E1Book struct {
	State       string   `json:"state"`
	Hours       float64  `json:"hours"`
	UnusedShare *float64 `json:"unused_share"` // 0–1, nil without unused margin
}

// E1Idle is the slots' time by row (idlemargin.Attribute summed over slots).
type E1Idle struct {
	Known            bool      `json:"known"`
	Note             string    `json:"note,omitempty"`
	Source           string    `json:"source"`
	Slots            int       `json:"slots"`
	SlotHours        float64   `json:"slot_hours"` // every slot's time in the week
	OccupiedHours    float64   `json:"occupied_hours"`
	IdleHours        float64   `json:"idle_hours"` // vacant and measured
	NotMeasuredHours float64   `json:"not_measured_hours"`
	Causes           []E1Cause `json:"causes"` // most hours first; shares sum to 1
	Book             []E1Book  `json:"book"`   // idlemargin.BookStates order
}

// E1Startup is one startup in the week.
type E1Startup struct {
	At       time.Time `json:"at"`
	Commit   string    `json:"commit,omitempty"`
	Source   string    `json:"source"` // journal | deploy
	Class    string    `json:"class"`  // neutral | expected | restart-made
	Orders   int       `json:"orders"`
	Lots     float64   `json:"lots"`              // every order of its window
	MadeLots float64   `json:"restart_made_lots"` // its restart-made orders only
}

// E1Restarts is every startup of the week, by class.
type E1Restarts struct {
	Known       bool           `json:"known"`
	Note        string         `json:"note,omitempty"`
	Source      string         `json:"source"`
	DeployNote  string         `json:"deploy_note,omitempty"` // deploys.tsv not read: commits unnamed
	Startups    int            `json:"startups"`
	ByClass     map[string]int `json:"by_class"`
	RestartMade int            `json:"restart_made"`
	MadeLots    float64        `json:"restart_made_lots"`
	List        []E1Startup    `json:"list"`
}

// buildReliability assembles the section; util is the report's Util (the
// week's history.Utilisation) and missing its history days.
func buildReliability(in Inputs, util history.UtilisationStats, missing []string) Reliability {
	return Reliability{
		TargetPct: E1TargetPct,
		Margin:    buildE1Margin(in, util, missing),
		Idle:      buildE1Idle(in),
		Restarts:  buildE1Restarts(in),
	}
}

func buildE1Margin(in Inputs, u history.UtilisationStats, missing []string) E1Margin {
	m := E1Margin{Source: in.History + " (`history.Utilisation`)", MeanPct: u.MeanPct, AllowedShare: u.AllowedShare,
		MeasuredHours: u.MeasuredHours, MissingDays: missing}
	switch {
	case in.Points == nil:
		m.Note = "P&L history not read"
	case u.MeasuredHours == 0:
		m.Note = "no P&L history line with margin data in the week"
	case u.MeanPct == nil:
		m.Note = "entries were never allowed in the measured time"
	default:
		m.Known = true
		met := *u.MeanPct >= E1TargetPct
		m.TargetMet = &met
	}
	return m
}

func buildE1Idle(in Inputs) E1Idle {
	d := E1Idle{Source: in.Journal + " and " + in.History + " (`internal/idlemargin`)"}
	switch {
	case in.IdleEvents == nil:
		d.Note = "journal not read"
		if in.JournalNote != "" {
			d.Note += ": " + in.JournalNote
		}
		return d
	case in.Points == nil:
		d.Note = "P&L history not read"
		return d
	}
	var slots []orders.SlotRef
	if in.Cfg != nil {
		for _, s := range in.Cfg.Slots() {
			slots = append(slots, orders.SlotRef{DTE: s.TargetDTE, Delta: s.EntryDelta})
		}
	}
	rep := idlemargin.Attribute(in.IdleEvents, in.Points, slots, in.Window.From, in.Window.To)
	d.Slots = len(rep.Slots)
	for row, h := range rep.Total.Hours {
		d.SlotHours += h
		switch row {
		case idlemargin.RowOccupied, idlemargin.RowOccupiedCapped:
			d.OccupiedHours += h
		case idlemargin.RowNotMeasured:
			d.NotMeasuredHours += h
		default:
			d.IdleHours += h
			d.Causes = append(d.Causes, E1Cause{Cause: row, Hours: h})
		}
	}
	for i := range d.Causes {
		d.Causes[i].Share = d.Causes[i].Hours / d.IdleHours
	}
	sort.Slice(d.Causes, func(i, j int) bool {
		if d.Causes[i].Hours != d.Causes[j].Hours {
			return d.Causes[i].Hours > d.Causes[j].Hours
		}
		return d.Causes[i].Cause < d.Causes[j].Cause
	})
	for _, s := range idlemargin.BookStates {
		if bs := rep.Book[s]; bs != nil {
			d.Book = append(d.Book, E1Book{State: s, Hours: bs.Hours, UnusedShare: bs.UnusedShare})
		}
	}
	if d.SlotHours-d.NotMeasuredHours <= 0 {
		d.Note = "no measured time: no P&L history line with margin data in the week"
		return d
	}
	d.Known = true
	return d
}

func buildE1Restarts(in Inputs) E1Restarts {
	r := E1Restarts{Source: in.Journal + " (startup `reconciled` lines) and " + in.DeploysPath + " (`internal/deployeffect`, first 30 min)",
		ByClass: map[string]int{}, DeployNote: in.DeploysNote}
	switch {
	case in.DeployLines == nil:
		r.Note = "journal not read"
		if in.JournalNote != "" {
			r.Note += ": " + in.JournalNote
		}
		return r
	case in.Cfg == nil:
		r.Note = "no config: the exit rules and repair cooldown classify the orders"
		return r
	}
	rep := deployeffect.Analyze(in.DeployLines, in.Deploys, deployeffect.RulesFrom(in.Cfg), in.Window.From, in.Window.To)
	r.Known, r.Startups, r.ByClass = true, len(rep.Startups), rep.ByClass
	r.RestartMade = rep.ByClass[deployeffect.RestartMade]
	for _, st := range rep.Startups {
		e := E1Startup{At: st.At, Commit: st.Commit, Source: st.Source, Class: st.Class, Orders: len(st.Orders)}
		for _, o := range st.Orders {
			e.Lots += o.Lots
			if o.Class == deployeffect.RestartMade {
				e.MadeLots += o.Lots
			}
		}
		r.MadeLots += e.MadeLots
		r.List = append(r.List, e)
	}
	return r
}

// writeReliability writes the E1 section.
func writeReliability(p func(string, ...any), e Reliability) {
	p("## E1 reliability\n\n")
	m := e.Margin
	p("**Margin use** (target %.0f %% of the IM limit while entries are allowed). Source: %s.\n\n", e.TargetPct, m.Source)
	if !m.Known {
		p("- not measured: %s\n", m.Note)
	} else {
		met := "**not met**"
		if *m.TargetMet {
			met = "**met**"
		}
		p("- mean IM ÷ limit while entries were allowed: **%.1f %%** — target %s", *m.MeanPct, met)
		if m.AllowedShare != nil {
			p(" · entries allowed %.0f %% of %.1f measured hours", *m.AllowedShare*100, m.MeasuredHours)
		}
		p("\n")
	}
	if len(m.MissingDays) > 0 {
		p("- not measured on %d day(s) with no P&L history line: %s\n", len(m.MissingDays), strings.Join(m.MissingDays, ", "))
	}
	p("\n")

	d := e.Idle
	p("**Why margin sat idle** (slot time by cause). Source: %s.\n\n", d.Source)
	if !d.Known {
		p("- not measured: %s\n\n", d.Note)
	} else {
		pct := func(h float64) float64 { return 100 * h / d.SlotHours }
		p("- %d slot(s), %.1f slot-hours: occupied %.1f h (%.0f %%) · idle %.1f h (%.0f %%) · not measured %.1f h (%.0f %%, bot down or no margin data)\n",
			d.Slots, d.SlotHours, d.OccupiedHours, pct(d.OccupiedHours), d.IdleHours, pct(d.IdleHours), d.NotMeasuredHours, pct(d.NotMeasuredHours))
		if len(d.Causes) == 0 {
			p("- no idle time measured\n")
		} else {
			p("- idle time by cause:")
			for _, c := range d.Causes {
				p(" %s %.0f %% (%.1f h);", c.Cause, c.Share*100, c.Hours)
			}
			p("\n")
		}
		var parts []string
		for _, b := range d.Book {
			if b.UnusedShare != nil {
				parts = append(parts, fmt.Sprintf("%s %.0f %%", b.State, *b.UnusedShare*100))
			}
		}
		if len(parts) > 0 {
			p("- unused approved margin by book state: %s\n", strings.Join(parts, " · "))
		}
		p("\n")
	}

	r := e.Restarts
	p("**Restarts and deploys** (what each startup sent in its first 30 minutes). Source: %s.\n\n", r.Source)
	if !r.Known {
		p("- not measured: %s\n\n", r.Note)
		return
	}
	if r.DeployNote != "" {
		p("- %s\n", r.DeployNote)
	}
	p("- %d startup(s): neutral %d · expected %d · **restart-made %d** (%.4g lots)\n",
		r.Startups, r.ByClass[deployeffect.Neutral], r.ByClass[deployeffect.Expected], r.RestartMade, r.MadeLots)
	for _, s := range r.List {
		commit := s.Commit
		if commit == "" {
			commit = "commit unknown"
		}
		p("  - %s UTC (%s, %s): %s · %d order(s), %.4g lots", s.At.UTC().Format("2006-01-02 15:04"), commit, s.Source, s.Class, s.Orders, s.Lots)
		if s.MadeLots > 0 {
			p(", %.4g restart-made", s.MadeLots)
		}
		p("\n")
	}
	p("\n")
}
