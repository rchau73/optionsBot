package evidence

import (
	"fmt"
	"sort"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/history"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
	"optionsbot/internal/replay"
)

// The direction trial scorecard (#245): the two GEX direction sources side
// by side for one week — live, each on the environment that runs it
// (stable's BTC on bias, mvp's BTC on sma since #168), and replayed on the
// same recorded books from the same starting book, once per source. The
// report lists facts only, as percentages of equity — never a coin or USD
// amount (a bot may run live) — and no recommendation: a person decides.
//
// BuildDirection is pure; LoadDirection reads the files.

// Row labels: a reader must never mistake the live rows for a controlled test.
const (
	LiveRowLabel   = "live — different accounts and books: not a controlled test"
	ReplayRowLabel = "replay — model"
)

// The gex_direction values (config.Validate).
const (
	DirectionBias = "bias"
	DirectionSMA  = "sma"
)

// DirectionSources are the two sources the replay runs, in report order.
var DirectionSources = []string{DirectionBias, DirectionSMA}

// DirectionEnv is one environment's inputs.
type DirectionEnv struct {
	Env       string // stable | mvp
	Direction string // its bot's gex_direction (from Config)
	Config    string

	Trades  []orders.Trade
	Journal string

	Directions []history.DirectionRecord // oldest first
	DirHistory string

	Equity       float64 // margin balance, coin; ≤ 0 = unknown (never written to the report)
	EquitySource string
}

// DirectionInputs are everything the scorecard reads, already loaded.
type DirectionInputs struct {
	Bot    string
	Window Window
	Envs   []DirectionEnv
	// Base is the environment whose journal's book at the week start, equity
	// and config every replay starts from (stable: it records the books).
	Base   string
	Cfg    *config.Config // Base's config; each replay changes only gex_direction
	Snaps  []recorder.Snapshot
	Market string
	GEX    []recorder.GEXRecord
	GEXDir string
	Closes []orders.DailyClose
	DVOL   []orders.DailyClose
}

// DirectionReport is the scorecard. Every figure is a count, hours or a
// percentage of equity.
type DirectionReport struct {
	Bot     string            `json:"bot"`
	Week    string            `json:"week"`
	From    time.Time         `json:"from"`
	To      time.Time         `json:"to"` // exclusive
	Live    []DirectionLive   `json:"live"`
	Replay  []DirectionReplay `json:"replay"`
	Missing DirectionMissing  `json:"missing"`
	Units   string            `json:"units"`
}

// DirectionMissing are the UTC days of the week without recorded books or
// minute GEX records (the replay uses the other days; none is filled in).
type DirectionMissing struct {
	BookDays []string `json:"book_days"`
	GEXDays  []string `json:"gex_days"`
}

// DirectionLive is one environment's week, live.
type DirectionLive struct {
	Source  string   `json:"source"` // the gex_direction it runs
	Env     string   `json:"env"`
	Label   string   `json:"label"`
	Files   []string `json:"files"`
	Sheds   int      `json:"sheds"`        // GEX-shed closes in the week
	Repairs int      `json:"shed_repairs"` // repairs re-selling a side a GEX shed closed
	Stops   int      `json:"stop_losses"`
	// Time the source in use spent shedding each side (confirmed shed), and
	// what the other source would have shed live (shadow, unconfirmed).
	Shed   SideHours `json:"shed_hours"`
	Shadow SideHours `json:"shadow_hours"`
	// Percentages of the env's equity; nil = equity unknown.
	EquityKnown  bool               `json:"equity_known"`
	EquitySource string             `json:"equity_source"`
	RealisedPct  *float64           `json:"realised_net_pct"` // the week's closes, net of fees
	BySlotPct    map[string]float64 `json:"realised_net_pct_by_slot"`
	MarkedPct    *float64           `json:"marked_net_pct"`       // realised + change of unrealised at the recorded mids, over the replay's window
	ReplayPct    *float64           `json:"replay_net_pct"`       // the same source's replay
	GapPP        *float64           `json:"live_minus_replay_pp"` // MarkedPct − ReplayPct
	GapSource    string             `json:"gap_source,omitempty"`
}

// SideHours is time spent shedding calls and puts, and how much of the
// week the direction history covered (before its first record the state
// is unknown, never assumed).
type SideHours struct {
	Source  string  `json:"source"`
	Calls   float64 `json:"calls"`
	Puts    float64 `json:"puts"`
	Covered float64 `json:"covered"` // hours of the week the history describes
}

// DirectionReplay is one source replayed.
type DirectionReplay struct {
	Source     string    `json:"source"`
	Label      string    `json:"label"`
	Files      []string  `json:"files"`
	Ran        bool      `json:"ran"`
	Note       string    `json:"note,omitempty"`
	StartLegs  int       `json:"start_legs"`
	BooksRead  int       `json:"books_read"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Decisions  int       `json:"decisions"`
	GEXMinutes int       `json:"gex_minute_decisions"`
	Sheds      int       `json:"sheds"`
	Repairs    int       `json:"repairs"` // every repair (the replay does not split shed repairs)
	Stops      int       `json:"stops"`
	NetPct     *float64  `json:"net_pct"` // net of fees, % of the starting equity
	MaxDDPct   *float64  `json:"max_drawdown_pct"`
	MaxIMPct   float64   `json:"max_im_pct"`
}

// DirectionReplaySettings are the replay settings of one source: Base's
// config with only gex_direction changed (a copy: the config is not
// touched), Base's journal book at the week start, its equity and the
// recorded books — the same for every source, so the market path is too.
func DirectionReplaySettings(in DirectionInputs, source string) (replay.Settings, error) {
	base, ok := in.base()
	switch {
	case !ok:
		return replay.Settings{}, fmt.Errorf("no %s environment: the replay starts from its book", in.Base)
	case in.Cfg == nil:
		return replay.Settings{}, fmt.Errorf("no config: the replay runs %s's settings", in.Base)
	case base.Equity <= 0:
		return replay.Settings{}, fmt.Errorf("no starting margin balance for %s (account.json): the replay sizes on it", in.Base)
	}
	cfg := *in.Cfg
	cfg.GEXDirection = source
	w := in.Window
	return replay.Settings{Cfg: &cfg, From: w.From, To: w.Last(), Equity: base.Equity,
		Closes: in.Closes, DVOL: in.DVOL, GEX: in.GEX, Book: replay.BookAt(base.Trades, w.From)}, nil
}

func (in DirectionInputs) base() (DirectionEnv, bool) {
	for _, e := range in.Envs {
		if e.Env == in.Base {
			return e, true
		}
	}
	return DirectionEnv{}, false
}

// BuildDirection assembles the scorecard from in.
func BuildDirection(in DirectionInputs) DirectionReport {
	w := in.Window
	r := DirectionReport{Bot: in.Bot, Week: w.Label, From: w.From, To: w.To,
		Units:   "percentages of each environment's equity, counts and hours only (a bot may run live: no amounts)",
		Missing: DirectionMissing{BookDays: missingDays(w, bookTimes(in.Snaps)), GEXDays: missingDays(w, gexTimes(in.GEX))}}

	replayNet := map[string]*float64{}
	var window []recorder.Snapshot // the books of the replays' window (the same for both)
	var start, end time.Time
	for _, src := range DirectionSources {
		row, res, ok := runDirectionReplay(in, src)
		if ok {
			replayNet[src] = row.NetPct
			start, end = res.Start, res.End
		}
		r.Replay = append(r.Replay, row)
	}
	if !start.IsZero() {
		for _, s := range in.Snaps {
			if !s.At.Before(start) && !s.At.After(end) {
				window = append(window, s)
			}
		}
	}
	for _, e := range in.Envs {
		row := buildDirectionLive(e, w)
		if row.EquityKnown && len(window) > 0 {
			a := replay.ActualPnL(e.Trades, window, start, end)
			row.MarkedPct = pct(a.Net, e.Equity)
			row.GapSource = e.Journal + " marked at " + in.Market + " mids, " + start.Format("Jan 02 15:04") + " → " + end.Format("Jan 02 15:04") + " UTC"
			if rp := replayNet[row.Source]; rp != nil {
				row.ReplayPct = rp
				g := *row.MarkedPct - *rp
				row.GapPP = &g
			}
		}
		r.Live = append(r.Live, row)
	}
	return r
}

func runDirectionReplay(in DirectionInputs, src string) (DirectionReplay, replay.Result, bool) {
	row := DirectionReplay{Source: src, Label: ReplayRowLabel, BooksRead: len(in.Snaps)}
	if base, ok := in.base(); ok {
		row.Files = []string{in.Market, base.Config + " (gex_direction: " + src + ")", base.Journal + " (book at the week start)"}
	} else {
		row.Files = []string{in.Market}
	}
	if !hasBookIn(in.Snaps, in.Window) {
		row.Note = "no recorded book in the week (" + in.Market + ")"
		return row, replay.Result{}, false
	}
	set, err := DirectionReplaySettings(in, src)
	if err != nil {
		row.Note = err.Error()
		return row, replay.Result{}, false
	}
	res, err := replay.Run(in.Snaps, set)
	if err != nil {
		row.Note = err.Error()
		return row, replay.Result{}, false
	}
	row.Ran, row.StartLegs = true, len(set.Book)
	row.Start, row.End, row.Decisions, row.GEXMinutes = res.Start, res.End, res.Snapshots, res.GEXMinutes
	row.Sheds, row.Repairs, row.Stops = res.Sheds, res.Repairs, res.Stops
	row.NetPct, row.MaxDDPct, row.MaxIMPct = pct(res.Net, set.Equity), pct(res.MaxDrawdown, set.Equity), res.MaxIMPct
	return row, res, true
}

var (
	shedCloses = reasonSet(orders.TriggerGammaClose)
	stopCloses = reasonSet(orders.TriggerStopLoss200Pct)
	trimCloses = reasonSet(orders.TriggerRebalanceLegs, orders.TriggerRebalanceDownsize) // the leg stays open
)

// reasonSet is triggers and the close_reason labels the journal records
// for them (as strategy.closeReasons).
func reasonSet(triggers ...string) map[string]bool {
	out := map[string]bool{}
	for _, t := range triggers {
		out[t], out[orders.CloseReasonLabel(t)] = true, true
	}
	return out
}

func buildDirectionLive(e DirectionEnv, w Window) DirectionLive {
	row := DirectionLive{Source: e.Direction, Env: e.Env, Label: LiveRowLabel,
		Files:     []string{e.Journal, e.DirHistory, e.Config},
		BySlotPct: map[string]float64{}, EquitySource: e.EquitySource, EquityKnown: e.Equity > 0}
	type side struct {
		expiry time.Time
		typ    string
	}
	shed := map[side]bool{} // the side's latest close was a GEX shed, not re-sold since
	realised, bySlot := 0.0, map[string]float64{}
	for _, t := range e.Trades {
		in := !t.At.Before(w.From) && t.At.Before(w.To)
		_, expiry, _, typ, err := marketdata.ParseOptionName(t.Instrument)
		s := side{expiry, typ}
		switch t.Kind {
		case "close":
			if err == nil && !trimCloses[t.Reason] {
				shed[s] = shedCloses[t.Reason]
			}
			if !in {
				continue
			}
			realised += t.PnL
			bySlot[slotLabel(t.Slot)] += t.PnL
			if shedCloses[t.Reason] {
				row.Sheds++
			}
			if stopCloses[t.Reason] {
				row.Stops++
			}
		case "open":
			if err == nil && t.Reason == orders.TriggerRepair {
				if in && shed[s] {
					row.Repairs++
				}
				shed[s] = false
			}
		}
	}
	if row.EquityKnown {
		row.RealisedPct = pct(realised, e.Equity)
		for k, v := range bySlot {
			row.BySlotPct[k] = *pct(v, e.Equity)
		}
	}
	row.Shed, row.Shadow = ShedHours(e.Directions, e.Direction, w)
	return row
}

func slotLabel(s *orders.SlotRef) string {
	if s == nil {
		return "unknown slot"
	}
	k := orders.SlotKey(s)
	return fmt.Sprintf("%dd/%.2fΔ", k.DTE, k.Delta)
}

// ShedHours integrates a direction history over w: each record's state
// lasts until the next record (they are written on change) or the week's
// end. used is the confirmed shed of the source in use (named inUse when
// the history is empty), shadow the other source's live action. Time before
// the history's first record is not covered.
func ShedHours(recs []history.DirectionRecord, inUse string, w Window) (used, shadow SideHours) {
	used.Source, shadow.Source = inUse, other(inUse)
	recs = append([]history.DirectionRecord(nil), recs...)
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].At.Before(recs[j].At) })
	for i, r := range recs {
		from, to := r.At, w.To
		if i+1 < len(recs) {
			to = recs[i+1].At
		}
		if from.Before(w.From) {
			from = w.From
		}
		if to.After(w.To) {
			to = w.To
		}
		if !to.After(from) {
			continue
		}
		h := to.Sub(from).Hours()
		if r.Direction != "" {
			used.Source, shadow.Source = r.Direction, other(r.Direction)
		}
		used.Covered += h
		shadow.Covered += h
		used.add(r.Shed, h)
		shadow.add(r.OtherAction, h)
	}
	return used, shadow
}

func (s *SideHours) add(action string, h float64) {
	switch action {
	case "close_calls":
		s.Calls += h
	case "close_puts":
		s.Puts += h
	}
}

func other(source string) string {
	switch source {
	case DirectionBias:
		return DirectionSMA
	case DirectionSMA:
		return DirectionBias
	}
	return ""
}

func pct(amount, equity float64) *float64 {
	if equity <= 0 {
		return nil
	}
	v := amount / equity * 100
	return &v
}
