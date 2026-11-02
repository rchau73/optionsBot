// Package evidence builds the weekly evidence report of one bot (#176): what
// it earned live, net of fees, next to what the bot's rules earn on the
// recorded mainnet books over the same week (internal/replay), the gap
// between the two, the standard stress shocks of its book (cmd/stress) and
// simply holding the coin (internal/perf). Every section names its source,
// and a missing input is reported — the days it lacks, or why a section did
// not run — never filled in.
//
// Build is pure: the caller (cmd/evidence) reads the files and runs the
// stress tool, and the same inputs always give the same report.
package evidence

import (
	"sort"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/deployeffect"
	"optionsbot/internal/history"
	"optionsbot/internal/idlemargin"
	"optionsbot/internal/orders"
	"optionsbot/internal/perf"
	"optionsbot/internal/recorder"
	"optionsbot/internal/replay"
)

// Unmodelled are the bot's behaviours the replay does not reproduce (README
// "Replay of recorded books"), listed with every gap.
var Unmodelled = []string{
	"rebalance top-ups", "leg balancing", "churn breaker",
	"stop and shed holds restored from the journal", "the bias's restart warm-up",
	"Deribit's margin (modelled: scan ±16 % spot, IV −20/+30 %)",
	"testnet fills (the replay fills at mainnet's recorded prices)",
	"decisions every recorded hour or GEX minute (live: every 40 s)",
}

// Inputs are everything one report reads, already loaded. A nil slice is a
// missing input: its section says so.
type Inputs struct {
	Env, Bot string
	Account  string // the env's DERIBIT_ENV: live | testnet ("" = testnet); live shows % only (#488)
	Window   Window
	Cfg      *config.Config // the bot's settings (replay)
	Config   string         // its path

	Trades      []orders.Trade // the journal's opens and closes
	Journal     string
	JournalNote string // why the journal was not read, when it was not

	// The E1 section's journal reads (#361); nil = not read.
	IdleEvents  []idlemargin.Event    // idlemargin.ReadJournalFile
	DeployLines []deployeffect.Line   // deployeffect.ReadJournalFile
	Deploys     []deployeffect.Deploy // deploys.tsv, this env's lines
	DeploysPath string
	DeploysNote string // why deploys.tsv was not read

	Points  []history.Point // P&L history, ordered by time
	History string

	Snaps   []recorder.Snapshot // recorded books up to the window's end (earlier ones warm up the replay)
	Market  string
	GEX     []recorder.GEXRecord // minute GEX records (nil = hourly replay)
	GEXDir  string
	Closes  []orders.DailyClose // daily closes and DVOL before the recording (market.json)
	DVOL    []orders.DailyClose
	MarketJ string

	Equity       float64 // margin balance at the window start, coin; ≤ 0 = unknown
	EquitySource string

	Stress       []StressOutcome // one per standard shock (nil = not run)
	StressDir    string
	StressBookAt time.Time // when the stressed book was captured (zero = unknown)
	StressNote   string    // why the stress did not run, when it did not

	// Every recorded dataset's record times over the week, for the coverage
	// section (#362); nil = not read.
	Recorded []RecordedSet

	// The bot's earlier weekly reports (ReadWeekly), for the twelve-week
	// scoreboard (#537); nil = none read.
	Earlier      []Report
	WeeklySource string
}

// Report is the weekly evidence of one bot. Amounts are coin unless named
// USD; it stays in data/ (gitignored).
type Report struct {
	Env       string         `json:"env"`
	Bot       string         `json:"bot"`
	Week      string         `json:"week"`
	From      time.Time      `json:"from"`
	To        time.Time      `json:"to"` // exclusive
	Live      Live           `json:"live"`
	Bridge    Bridge         `json:"bridge"`
	Recon     Reconciliation `json:"reconciliation"`
	Replay    Replay         `json:"replay"`
	Gap       Gap            `json:"gap"`
	Stress    Stress         `json:"stress"`
	Hold      Hold           `json:"hold"`
	Util      Util           `json:"utilisation"`
	ROM       ROM            `json:"return_on_margin"`
	Missing   Missing        `json:"missing"`
	E1        Reliability    `json:"e1_reliability"`
	Coverage  Coverage       `json:"coverage"`
	RVL       ReplayVsLive   `json:"replay_vs_live"`
	Score     Scoreboard     `json:"scoreboard"`
	Equity    float64        `json:"equity"` // coin, at the start (0 = unknown)
	EquitySrc string         `json:"equity_source"`
}

// Live is what the bot did: the journal's closes in the window (realised,
// net of fees) and the P&L history's figures (perf.Summarize).
type Live struct {
	Source     string                `json:"source"`
	Opens      int                   `json:"opens"`
	Closes     int                   `json:"closes"`
	Realised   float64               `json:"realised"` // Σ close P&L, net of fees
	Fees       float64               `json:"fees"`     // Σ close fees (opening share + closing fee), already in Realised
	ByReason   map[string]ReasonStat `json:"by_reason"`
	Unrealised *float64              `json:"unrealised"` // the history's last point in the window; nil without one
	Perf       perf.Summary          `json:"perf"`
	PerfSource string                `json:"perf_source"`
}

// ReasonStat is the closes of one reason.
type ReasonStat struct {
	Closes int     `json:"closes"`
	PnL    float64 `json:"pnl"`
}

// Replay is the bot's rules on the recorded books over the week, starting
// from the journal's book at the window start.
type Replay struct {
	Ran        bool   `json:"ran"`
	Note       string `json:"note,omitempty"`    // why it did not run
	Partial    string `json:"partial,omitempty"` // why it stands for part of the week only (Coverage)
	Source     string `json:"source"`
	ConfigPath string `json:"config"`
	Direction  string `json:"direction"`
	BooksRead  int    `json:"books_read"`          // recorded books up to the week's end (earlier ones warm up)
	GEXInWeek  int    `json:"gex_records_in_week"` // minute GEX records inside the week
	StartLegs  int    `json:"start_legs"`          // legs of the journal's book it started from
	// The replay's figures (replay.Result).
	Start           time.Time `json:"start"`
	End             time.Time `json:"end"`
	Snapshots       int       `json:"decisions"`
	GEXMinutes      int       `json:"gex_minute_decisions"`
	Net             float64   `json:"net"` // realised + unrealised, net of fees
	Realised        float64   `json:"realised"`
	Fees            float64   `json:"fees"`
	Trades          int       `json:"trades"`
	Entries         int       `json:"entries"`
	Repairs         int       `json:"repairs"`
	Sheds           int       `json:"sheds"`
	Stops           int       `json:"stops"`
	DeltaExits      int       `json:"delta_exits"`
	Rolls           int       `json:"rolls"`
	MMCuts          int       `json:"mm_cuts"`
	Unfilled        int       `json:"unfilled"`
	LiquidityCapped int       `json:"liquidity_capped"`
	OpenAtEnd       int       `json:"open_at_end"`
	MaxDrawdown     float64   `json:"max_drawdown"`
	MaxIMPct        float64   `json:"max_im_pct"`
	AvgIMPct        float64   `json:"avg_im_pct"`
}

// Gap is replay − live over the replay's window, both marked at the
// recorded mids (replay.ActualPnL), with what the replay does not model.
type Gap struct {
	Known      bool          `json:"known"`
	Partial    string        `json:"partial,omitempty"` // as Replay.Partial
	Source     string        `json:"source"`
	Live       replay.Actual `json:"live"`
	Replay     float64       `json:"replay"`
	Gap        float64       `json:"gap"` // Replay − Live.Net
	Unmodelled []string      `json:"unmodelled"`
}

// Stress is the standard shocks on the captured book.
type Stress struct {
	Ran      bool            `json:"ran"`
	Note     string          `json:"note,omitempty"`
	Source   string          `json:"source"`
	BookAt   *time.Time      `json:"book_at,omitempty"`
	Stale    bool            `json:"stale"` // the book was not captured within a day of the week's end
	Outcomes []StressOutcome `json:"outcomes"`
}

// Hold is the hold-the-coin benchmark over the week (perf).
type Hold struct {
	Known            bool    `json:"known"`
	Source           string  `json:"source"`
	SpotStart        float64 `json:"spot_start"`
	SpotEnd          float64 `json:"spot_end"`
	HoldReturnUSDPct float64 `json:"hold_return_usd_pct"`
	// The strategy's USD return and its excess over holding; need the equity.
	StrategyKnown   bool    `json:"strategy_known"`
	ReturnUSDPct    float64 `json:"return_usd_pct"`
	ExcessReturnPct float64 `json:"excess_return_pct"`
}

// Util is the margin utilisation of the week (history.Utilisation, #174).
type Util struct {
	Source string                   `json:"source"`
	Stats  history.UtilisationStats `json:"stats"`
}

// ROM is the north star (#278): net return per unit of initial margin
// used, live (P&L history) and, when the replay ran, the replay's own.
type ROM struct {
	Label        string             `json:"label"` // perf.MarginApprox
	LiveSource   string             `json:"live_source"`
	Live         perf.MarginReturn  `json:"live"`
	ReplaySource string             `json:"replay_source"`
	Replay       *perf.MarginReturn `json:"replay"` // nil when the replay did not run
}

// Missing lists, per input, the UTC days of the window it has nothing for.
type Missing struct {
	HistoryDays []string `json:"history_days"` // no P&L history point
	BookDays    []string `json:"book_days"`    // no recorded book
	GEXDays     []string `json:"gex_days"`     // no minute GEX record
}

// staleBook is how far from the week's end a stressed book may be captured.
const staleBook = 24 * time.Hour

// Build assembles the report from in.
func Build(in Inputs) Report {
	w := in.Window
	r := Report{Env: in.Env, Bot: in.Bot, Week: w.Label, From: w.From, To: w.To, EquitySrc: in.EquitySource}
	if in.Equity > 0 {
		r.Equity = in.Equity
	}
	r.Live = buildLive(in)
	r.Bridge = BuildBridge(in.Points, w.From, w.To, in.History)
	r.Recon = BuildReconciliation(in.Points, in.Trades, w, in.Account, in.History, in.Journal)
	r.Hold = buildHold(in, r.Live.Perf)
	r.Score = buildScoreboard(in, r.Hold)
	r.Util = Util{Source: in.History, Stats: history.Utilisation(in.Points, w.From, w.To)}
	r.Missing = Missing{
		HistoryDays: dates(r.Live.Perf.MissingDays),
		BookDays:    missingDays(w, bookTimes(in.Snaps)),
		GEXDays:     missingDays(w, gexTimes(in.GEX)),
	}
	r.Coverage = buildCoverage(in.Recorded, w)
	var days []replay.Day
	r.Replay, r.Gap, days = buildReplay(in)
	if note := r.Coverage.partialNote(); note != "" && r.Replay.Ran {
		r.Replay.Partial, r.Gap.Partial = note, note
	}
	r.Stress = buildStress(in)
	r.ROM = buildROM(in, r.Replay)
	r.E1 = buildReliability(in, r.Util.Stats, r.Missing.HistoryDays)
	r.RVL = buildReplayVsLive(in, r, days)
	return r
}

func buildLive(in Inputs) Live {
	w := in.Window
	l := Live{Source: in.Journal + " (opens and closes)", ByReason: map[string]ReasonStat{}, PerfSource: in.History}
	for _, t := range in.Trades {
		if t.At.Before(w.From) || !t.At.Before(w.To) {
			continue
		}
		switch t.Kind {
		case "open":
			l.Opens++
		case "close":
			l.Closes++
			l.Realised += t.PnL
			l.Fees += t.Fees
			s := l.ByReason[t.Reason]
			s.Closes++
			s.PnL += t.PnL
			l.ByReason[t.Reason] = s
		}
	}
	l.Perf = perf.Summarize(in.Points, in.Equity, w.From, w.Last())
	for i := len(in.Points) - 1; i >= 0; i-- {
		if p := in.Points[i]; !p.T.Before(w.From) && p.T.Before(w.To) {
			u := p.Unrealised
			l.Unrealised = &u
			break
		}
	}
	return l
}

func buildHold(in Inputs, s perf.Summary) Hold {
	return Hold{Known: s.HoldKnown, Source: in.History + " (spot at the first and last point)",
		SpotStart: s.SpotStart, SpotEnd: s.SpotEnd, HoldReturnUSDPct: s.HoldReturnUSDPct,
		StrategyKnown: s.HoldKnown && s.EquityKnown, ReturnUSDPct: s.ReturnUSDPct, ExcessReturnPct: s.ExcessReturnPct}
}

// buildReplay also returns the replay's days (the replay vs live section's worst day).
func buildReplay(in Inputs) (Replay, Gap, []replay.Day) {
	w := in.Window
	rp := Replay{Source: in.Market, ConfigPath: in.Config, BooksRead: len(in.Snaps)}
	gap := Gap{Source: in.Journal + " marked at " + in.Market + " mids", Unmodelled: Unmodelled}
	if in.Cfg != nil {
		rp.Direction = in.Cfg.GEXDirection
	}
	for _, g := range in.GEX {
		if !g.ComputedAt.Before(w.From) && g.ComputedAt.Before(w.To) {
			rp.GEXInWeek++
		}
	}
	switch {
	case in.Cfg == nil:
		rp.Note = "no config: the replay runs the bot's settings"
	case !hasBookIn(in.Snaps, w):
		rp.Note = "no recorded book in the week (" + in.Market + ")"
	case in.Equity <= 0:
		rp.Note = "no starting margin balance (account.json or -equity): the replay sizes on it"
	}
	if rp.Note != "" {
		gap.Unmodelled = nil
		return rp, gap, nil
	}
	book := replay.BookAt(in.Trades, w.From)
	res, err := replay.Run(in.Snaps, replay.Settings{Cfg: in.Cfg, From: w.From, To: w.Last(), Equity: in.Equity,
		Closes: in.Closes, DVOL: in.DVOL, GEX: in.GEX, Book: book})
	if err != nil {
		rp.Note = err.Error()
		gap.Unmodelled = nil
		return rp, gap, nil
	}
	rp.Ran, rp.StartLegs = true, len(book)
	rp.Start, rp.End, rp.Snapshots, rp.GEXMinutes = res.Start, res.End, res.Snapshots, res.GEXMinutes
	rp.Net, rp.Realised, rp.Fees = res.Net, res.Realised, res.Fees
	rp.Trades, rp.Entries, rp.Repairs = res.Trades, res.Entries, res.Repairs
	rp.Sheds, rp.Stops, rp.DeltaExits, rp.Rolls, rp.MMCuts, rp.Unfilled = res.Sheds, res.Stops, res.DeltaExits, res.Rolls, res.MMCuts, res.Unfilled
	rp.MaxDrawdown, rp.MaxIMPct, rp.AvgIMPct = res.MaxDrawdown, res.MaxIMPct, res.AvgIMPct
	rp.LiquidityCapped, rp.OpenAtEnd = res.LiquidityCapped, res.OpenAtEnd

	var window []recorder.Snapshot
	for _, s := range in.Snaps {
		if !s.At.Before(res.Start) && !s.At.After(res.End) {
			window = append(window, s)
		}
	}
	gap.Known = true
	gap.Live = replay.ActualPnL(in.Trades, window, res.Start, res.End)
	gap.Replay = res.Net
	gap.Gap = res.Net - gap.Live.Net
	return rp, gap, res.Days
}

// buildROM is the return on margin, live and replayed (#278). The replay's
// average IM is its MODEL margin, averaged per decision.
func buildROM(in Inputs, rp Replay) ROM {
	m := ROM{Label: perf.MarginApprox,
		LiveSource:   in.History + " (net P&L ÷ time-weighted mean im_pct × equity)",
		Live:         perf.ReturnOnMargin(in.Points, in.Equity, in.Window.From, in.Window.Last()),
		ReplaySource: "the replay's net ÷ its average IM % (MODEL margin) × equity"}
	if rp.Ran {
		r := perf.ReturnOnMarginOf(rp.Net, rp.AvgIMPct, in.Equity, rp.End.Sub(rp.Start).Hours())
		m.Replay = &r
	}
	return m
}

func buildStress(in Inputs) Stress {
	s := Stress{Source: in.StressDir + " (scripts/stress_inputs.sh) through cmd/stress", Outcomes: in.Stress}
	if !in.StressBookAt.IsZero() {
		at := in.StressBookAt
		s.BookAt = &at
		d := in.Window.To.Sub(at)
		s.Stale = d > staleBook || d < -staleBook
	}
	if len(in.Stress) == 0 {
		s.Note = in.StressNote
		if s.Note == "" {
			s.Note = "not run"
		}
		return s
	}
	for _, o := range in.Stress {
		if o.Error == "" {
			s.Ran = true
		}
	}
	return s
}

func hasBookIn(snaps []recorder.Snapshot, w Window) bool {
	for _, s := range snaps {
		if !s.At.Before(w.From) && s.At.Before(w.To) {
			return true
		}
	}
	return false
}

func bookTimes(snaps []recorder.Snapshot) []time.Time {
	out := make([]time.Time, len(snaps))
	for i, s := range snaps {
		out[i] = s.At
	}
	return out
}

func gexTimes(recs []recorder.GEXRecord) []time.Time {
	out := make([]time.Time, len(recs))
	for i, g := range recs {
		out[i] = g.ComputedAt
	}
	return out
}

// missingDays are the UTC days of w with none of times.
func missingDays(w Window, times []time.Time) []string {
	seen := map[time.Time]bool{}
	for _, t := range times {
		seen[utcDay(t)] = true
	}
	var out []string
	for d := utcDay(w.From); d.Before(w.To); d = d.AddDate(0, 0, 1) {
		if !seen[d] {
			out = append(out, d.Format(time.DateOnly))
		}
	}
	return out
}

func dates(ts []time.Time) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Format(time.DateOnly))
	}
	return out
}

// reasons are the close reasons of l in a stable order (most P&L lost first).
func (l Live) reasons() []string {
	var out []string
	for k := range l.ByReason {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := l.ByReason[out[i]], l.ByReason[out[j]]
		if a.PnL != b.PnL {
			return a.PnL < b.PnL
		}
		return out[i] < out[j]
	})
	return out
}
