// Package compare runs the same evidence for two configs — the backtest
// (internal/backtest), the bot's rules replayed on the recorded mainnet books
// (internal/replay) and the standard stress shocks of the captured book
// (cmd/stress) — and puts the figures side by side (#186), after the
// settings that differ, key by key. It adds no rule: both runs read the same
// data with the same fixed values (config.Load applies config.ApplyFixed).
// RunWindows (#301) repeats the backtest and the replay once per UTC week or
// day of the span and summarises how often the proposal wins and where it
// loses; the stress stays one run.
//
// The figures are modelled, not a forecast: every report carries Disclaimer.
package compare

import (
	"context"
	"time"

	"optionsbot/internal/backtest"
	"optionsbot/internal/config"
	"optionsbot/internal/evidence"
	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
	"optionsbot/internal/replay"
)

// Disclaimer heads every report.
const Disclaimer = "Backtested/replayed, not a promise: modelled fills, prices and margin on past or scripted data. " +
	"Past or simulated results do not predict future results."

// BacktestEquityUSD is the backtest's starting equity, as `bot --mode=backtest`.
const BacktestEquityUSD = 100_000

// Inputs are everything one comparison reads. Base and Proposed are loaded
// with config.Load; the data is read once and given to both runs.
type Inputs struct {
	BasePath, ProposedPath string
	Base, Proposed         *config.Config
	From, To               time.Time // first and last UTC day, both included

	CSV string // backtest data (cmd/gendata format); "" = no backtest

	Market       string               // recorded books' folder; "" = no replay
	Snaps        []recorder.Snapshot  // books up to To's end (earlier ones warm up GEX, DVOL, regime)
	GEX          []recorder.GEXRecord // minute GEX records (nil = hourly)
	Closes, DVOL []orders.DailyClose  // daily history before the recording (market.json)
	Equity       float64              // replay's starting margin balance, coin; ≤ 0 = unknown
	EquitySource string

	StressDir                  string // captured book (scripts/stress_inputs.sh); "" = no stress
	StressBookAt               time.Time
	BaseStress, ProposedStress []evidence.StressOutcome // one per standard shock (RunStress)
	StressNote                 string                   // why the stress did not run, when it did not
}

// Report is the comparison. Backtest amounts are USD (the backtest's
// units); replay and stress amounts are in the coin.
type Report struct {
	Disclaimer string    `json:"disclaimer"`
	Underlying string    `json:"underlying"`
	Base       string    `json:"base"`
	Proposed   string    `json:"proposed"`
	From       time.Time `json:"from"`
	To         time.Time `json:"to"` // last day, included
	Diffs      []Diff    `json:"diffs"`
	Backtest   Backtest  `json:"backtest"`
	Replay     Replay    `json:"replay"`
	Stress     Stress    `json:"stress"`
	// Windows is the comparison per week or day (-windows); nil without it.
	Windows *WindowReport `json:"windows,omitempty"`
	// Neighbours is the fragility check of one proposed setting
	// (-neighbours); nil without it.
	Neighbours *Neighbours `json:"neighbours,omitempty"`
}

// Backtest is both configs through the daily backtest.
type Backtest struct {
	Ran       bool        `json:"ran"`
	Note      string      `json:"note,omitempty"`
	Source    string      `json:"source"`
	EquityUSD float64     `json:"equity_usd"`
	Base      BacktestRun `json:"base"`
	Proposed  BacktestRun `json:"proposed"`
}

// BacktestRun is one config's backtest, in USD.
type BacktestRun struct {
	Error          string  `json:"error,omitempty"`
	NetUSD         float64 `json:"net_usd"` // equity end − start (realised + open)
	ReturnPct      float64 `json:"return_pct"`
	RealisedUSD    float64 `json:"realised_usd"` // closed trades, net of fees
	FeesUSD        float64 `json:"fees_usd"`
	WorstWeekUSD   float64 `json:"worst_week_usd"` // ≤ 0
	WorstWeekPct   float64 `json:"worst_week_pct"`
	MaxDrawdownUSD float64 `json:"max_drawdown_usd"`
	MaxDrawdownPct float64 `json:"max_drawdown_pct"`
	Trades         int     `json:"trades"`
	Stops          int     `json:"stops"`
	DeltaExits     int     `json:"delta_exits"`
	Sheds          int     `json:"sheds"` // GEX closes
	Rolls          int     `json:"rolls"` // DTE rolls, delta drifts, take-profits
	Sharpe         float64 `json:"sharpe"`
}

// Replay is both configs on the recorded books, each from a flat book.
type Replay struct {
	Ran          bool      `json:"ran"`
	Note         string    `json:"note,omitempty"`
	Source       string    `json:"source"`
	Equity       float64   `json:"equity"` // coin, at the start
	EquitySource string    `json:"equity_source"`
	Base         ReplayRun `json:"base"`
	Proposed     ReplayRun `json:"proposed"`
}

// ReplayRun is one config's replay, in the coin.
type ReplayRun struct {
	Error        string    `json:"error,omitempty"`
	Start        time.Time `json:"start"`
	End          time.Time `json:"end"`
	Decisions    int       `json:"decisions"`
	Net          float64   `json:"net"` // realised + unrealised, net of fees
	ReturnPct    float64   `json:"return_pct"`
	Realised     float64   `json:"realised"`
	Fees         float64   `json:"fees"`
	WorstWeek    float64   `json:"worst_week"` // ≤ 0
	WorstWeekPct float64   `json:"worst_week_pct"`
	MaxDrawdown  float64   `json:"max_drawdown"`
	Trades       int       `json:"trades"`
	Entries      int       `json:"entries"`
	Repairs      int       `json:"repairs"`
	Sheds        int       `json:"sheds"`
	Stops        int       `json:"stops"`
	DeltaExits   int       `json:"delta_exits"`
	Rolls        int       `json:"rolls"`
	MMCuts       int       `json:"mm_cuts"`
	MaxIMPct     float64   `json:"max_im_pct"`
	AvgIMPct     float64   `json:"avg_im_pct"`
}

// Stress is both configs through the standard shocks on the captured book.
type Stress struct {
	Ran      bool                     `json:"ran"`
	Note     string                   `json:"note,omitempty"`
	Source   string                   `json:"source"`
	BookAt   *time.Time               `json:"book_at,omitempty"`
	Base     []evidence.StressOutcome `json:"base"`
	Proposed []evidence.StressOutcome `json:"proposed"`
	// NotModelled are the differing settings cmd/stress does not see
	// (evidence.StressModels): they cannot change a stress figure.
	NotModelled []string `json:"not_modelled,omitempty"`
}

// Run builds the report: the diff, then both backtests and both replays (the
// stress outcomes come in in, from RunStress). It reads in.CSV; nothing else.
func Run(in Inputs) Report {
	r := Report{Disclaimer: Disclaimer, Base: in.BasePath, Proposed: in.ProposedPath, From: in.From, To: in.To}
	if in.Base == nil || in.Proposed == nil {
		note := "both configs are needed"
		r.Backtest.Note, r.Replay.Note, r.Stress.Note = note, note, note
		return r
	}
	r.Underlying = in.Base.Underlying
	r.Diffs = DiffConfigs(in.Base, in.Proposed)
	r.Backtest = runBacktests(in)
	r.Replay = runReplays(in)
	r.Stress = buildStress(in)
	r.Stress.NotModelled = notModelledByStress(r.Diffs, in.Base, in.Proposed)
	return r
}

const noBacktestData = "no backtest data (-csv)"

func runBacktests(in Inputs) Backtest {
	b := Backtest{Source: in.CSV, EquityUSD: BacktestEquityUSD}
	if in.CSV == "" {
		b.Note = noBacktestData
		return b
	}
	b.Base, b.Proposed = runBacktest(in.Base, in.CSV, in.From, in.To), runBacktest(in.Proposed, in.CSV, in.From, in.To)
	b.Ran = b.Base.Error == "" || b.Proposed.Error == ""
	return b
}

func runBacktest(cfg *config.Config, csv string, from, to time.Time) BacktestRun {
	feed, err := backtest.NewHistoricalFeed(csv, from, to, cfg.IVPercentileWindow)
	if err != nil {
		return BacktestRun{Error: err.Error()}
	}
	exec := backtest.NewSimExecutor(cfg.Backtest, BacktestEquityUSD)
	engine := backtest.NewEngine(cfg, feed, exec)
	s, err := engine.Run(context.Background())
	if err != nil {
		return BacktestRun{Error: err.Error()}
	}
	snaps := engine.Snapshots()
	r := BacktestRun{RealisedUSD: s.TotalPnLUSD, FeesUSD: s.TotalCommissionUSD,
		MaxDrawdownUSD: s.MaxDrawdownUSD, MaxDrawdownPct: s.MaxDrawdownPct, Trades: s.TotalTrades,
		Stops: s.StopLossTriggers, DeltaExits: s.DeltaExits, Sheds: s.GammaCloseTriggers,
		Rolls: s.Rollout19DTE + s.RolloutDeltaDrift + s.RolloutROI, Sharpe: s.SharpeRatio}
	if len(snaps) > 0 {
		r.NetUSD = snaps[len(snaps)-1].EquityUSD - BacktestEquityUSD
	}
	r.ReturnPct = r.NetUSD / BacktestEquityUSD * 100
	r.WorstWeekUSD, r.WorstWeekPct = backtest.WorstWeek(snaps)
	return r
}

func runReplays(in Inputs) Replay {
	rp := Replay{Source: in.Market, Equity: in.Equity, EquitySource: in.EquitySource, Note: replayNote(in)}
	if rp.Note != "" {
		return rp
	}
	rp.Base, rp.Proposed = runReplay(in, in.Base), runReplay(in, in.Proposed)
	rp.Ran = rp.Base.Error == "" || rp.Proposed.Error == ""
	return rp
}

// replayNote says why the replay cannot run on in; "" = it can.
func replayNote(in Inputs) string {
	switch {
	case in.Market == "":
		return "no recorded books (-replay)"
	case !hasBook(in.Snaps, in.From, in.To):
		return "no recorded book between the two days (" + in.Market + ")"
	case in.Equity <= 0:
		return "no starting margin balance (-equity or account.json): the replay sizes on it"
	}
	return ""
}

func runReplay(in Inputs, cfg *config.Config) ReplayRun {
	res, err := replay.Run(in.Snaps, replay.Settings{Cfg: cfg, From: in.From, To: lastInstant(in.To), Equity: in.Equity,
		Closes: in.Closes, DVOL: in.DVOL, GEX: in.GEX})
	if err != nil {
		return ReplayRun{Error: err.Error()}
	}
	r := ReplayRun{Start: res.Start, End: res.End, Decisions: res.Snapshots, Net: res.Net, Realised: res.Realised,
		Fees: res.Fees, MaxDrawdown: res.MaxDrawdown, Trades: res.Trades, Entries: res.Entries, Repairs: res.Repairs,
		Sheds: res.Sheds, Stops: res.Stops, DeltaExits: res.DeltaExits, Rolls: res.Rolls, MMCuts: res.MMCuts,
		MaxIMPct: res.MaxIMPct, AvgIMPct: res.AvgIMPct}
	if res.Equity0 > 0 {
		r.ReturnPct = res.Net / res.Equity0 * 100
	}
	// The backtest's worst week on the replay's daily equity (coin), from
	// the starting balance.
	curve := []backtest.PortfolioSnapshot{{Date: res.Start, EquityUSD: res.Equity0}}
	for _, d := range res.Days {
		curve = append(curve, backtest.PortfolioSnapshot{Date: d.Date, EquityUSD: d.Equity})
	}
	r.WorstWeek, r.WorstWeekPct = backtest.WorstWeek(curve)
	return r
}

func buildStress(in Inputs) Stress {
	s := Stress{Source: in.StressDir + " (scripts/stress_inputs.sh) through cmd/stress", Base: in.BaseStress, Proposed: in.ProposedStress}
	if !in.StressBookAt.IsZero() {
		at := in.StressBookAt
		s.BookAt = &at
	}
	for _, o := range append(append([]evidence.StressOutcome{}, in.BaseStress...), in.ProposedStress...) {
		if o.Error == "" {
			s.Ran = true
		}
	}
	if len(in.BaseStress)+len(in.ProposedStress) == 0 {
		s.Note = in.StressNote
		if s.Note == "" {
			s.Note = "not run"
		}
	}
	return s
}

// notModelledByStress are the keys of diffs that cmd/stress does not see for
// both configs.
func notModelledByStress(diffs []Diff, base, proposed *config.Config) []string {
	var out []string
	for _, d := range diffs {
		if !evidence.StressModels(d.Key, base, proposed) {
			out = append(out, d.Key)
		}
	}
	return out
}

func hasBook(snaps []recorder.Snapshot, from, to time.Time) bool {
	for _, s := range snaps {
		if !s.At.Before(from) && !s.At.After(lastInstant(to)) {
			return true
		}
	}
	return false
}

// lastInstant is the last moment of day's UTC day.
func lastInstant(day time.Time) time.Time {
	return day.Truncate(24 * time.Hour).Add(24*time.Hour - time.Nanosecond)
}
