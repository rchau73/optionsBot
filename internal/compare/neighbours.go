package compare

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"optionsbot/internal/config"
)

// The fragility check of a proposed setting (#495): the proposed config is
// re-run with only that setting moved −20 %, −10 %, +10 % and +20 %, so a
// value that works only at one exact number shows as a cliff between
// neighbours. Backtest and replay only (the stress is one run of the
// proposed value); nothing here applies a setting.

// NeighbourSteps are the moves around the proposed value, in % of it.
var NeighbourSteps = []float64{-20, -10, 10, 20}

// CliffPP is the change, in percentage points of equity, of the worst week
// or the max drawdown between two adjacent values that flags a cliff. Why 1:
// a first round value, large against the reports' rounding (0.01 pp) and
// small against what a manager would accept from a 10 % nudge of one setting
// (the IM limits are 20–50 % of the balance, a stop costs twice a premium);
// revisit once neighbour runs on recorded books show the usual spread.
const CliffPP = 1.0

// Neighbours is the fragility check of one setting of the proposed config.
type Neighbours struct {
	Key      string  `json:"key"` // the YAML key, as in Diffs
	Title    string  `json:"title"`
	Unit     string  `json:"unit,omitempty"`
	Proposed float64 `json:"proposed"`
	Range    string  `json:"range"` // the catalogue's range
	// CliffThresholdPP is CliffPP, written into the report the analysis reads.
	CliffThresholdPP float64 `json:"cliff_threshold_pp"`
	// StressNote says the stress is not re-run for the neighbours.
	StressNote string `json:"stress_note"`
	// Rows are the four neighbours and the proposed value (step 0), by value.
	Rows   []NeighbourRow `json:"rows"`
	Cliffs []Cliff        `json:"cliffs"`
}

// StressNotNeighboured is Neighbours.StressNote.
const StressNotNeighboured = "stress is one run of the proposed value (section Stress shocks), not re-run for the neighbours"

// NeighbourRow is one value of the setting. Skipped says why it did not run
// (outside the catalogue's range, refused by Validate); Note says a value was
// clipped to the range.
type NeighbourRow struct {
	StepPct  float64          `json:"step_pct"` // 0 = the proposed value
	Value    float64          `json:"value"`
	Note     string           `json:"note,omitempty"`
	Skipped  string           `json:"skipped,omitempty"`
	Backtest NeighbourFigures `json:"backtest"` // fees in USD
	Replay   NeighbourFigures `json:"replay"`   // fees in the coin
}

// NeighbourFigures are one source's figures for one value; Ran is false
// with Note saying why.
type NeighbourFigures struct {
	Ran            bool    `json:"ran"`
	Note           string  `json:"note,omitempty"`
	ReturnPct      float64 `json:"return_pct"`
	WorstWeekPct   float64 `json:"worst_week_pct"` // ≤ 0
	MaxDrawdownPct float64 `json:"max_drawdown_pct"`
	Fees           float64 `json:"fees"`
	Trades         int     `json:"trades"`
}

// Cliff is a sharp change between two adjacent values that both ran.
type Cliff struct {
	Source string  `json:"source"` // backtest or replay
	Figure string  `json:"figure"` // worst_week_pct, max_drawdown_pct or return_pct
	From   float64 `json:"from_value"`
	To     float64 `json:"to_value"`
	Before float64 `json:"before"` // the figure at From
	After  float64 `json:"after"`  // and at To
	Detail string  `json:"detail"`
}

// Cliff figures.
const (
	FigureWorstWeek = "worst_week_pct"
	FigureDrawdown  = "max_drawdown_pct"
	FigureReturn    = "return_pct"
)

var indexRe = regexp.MustCompile(`\[\d+\]`)

// TunableFor is the catalogue entry of a Diffs key: list indexes match "[]".
func TunableFor(key string) (config.Tunable, bool) {
	want := indexRe.ReplaceAllString(key, "[]")
	for _, t := range config.Tunables() {
		if t.Key == want {
			return t, true
		}
	}
	return config.Tunable{}, false
}

// PlanNeighbours checks that key is a number setting of proposed and lists
// its values: the four neighbours and the proposed one, with the reason any
// is skipped. Nothing is run.
func PlanNeighbours(proposed *config.Config, key string) (Neighbours, error) {
	t, ok := TunableFor(key)
	if !ok {
		return Neighbours{}, fmt.Errorf("neighbours %q: not a setting of the catalogue (docs/parameters.md)", key)
	}
	if t.Kind != config.KindNumber && t.Kind != config.KindInteger {
		return Neighbours{}, fmt.Errorf("neighbours %q: a %s setting has no neighbours, only numbers do", key, t.Kind)
	}
	raw, ok := settings(proposed)[key]
	if !ok {
		return Neighbours{}, fmt.Errorf("neighbours %q: the proposed config has no such key (write it as in the Differences, e.g. dte_delta_matrix[0].deltas[0])", key)
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return Neighbours{}, fmt.Errorf("neighbours %q: value %q is not a number", key, raw)
	}
	return Neighbours{Key: key, Title: t.Title, Unit: t.Unit, Proposed: v, Range: rangeOf(t),
		CliffThresholdPP: CliffPP, StressNote: StressNotNeighboured, Rows: NeighbourValues(v, t)}, nil
}

// NeighbourValues are proposed moved by each NeighbourStep, rounded to the
// setting's step and clipped to its catalogue range, plus proposed itself
// (step 0), in step order. A value equal to proposed or to an earlier
// neighbour once rounded or clipped is skipped, with the reason.
func NeighbourValues(proposed float64, t config.Tunable) []NeighbourRow {
	var rows []NeighbourRow
	seen := []float64{proposed}
	for _, step := range NeighbourSteps {
		v := roundTo(proposed*(1+step/100), t)
		row := NeighbourRow{StepPct: step, Value: v}
		var clip string
		switch {
		case t.Min != nil && v < *t.Min:
			v, clip = *t.Min, fmt.Sprintf("below the catalogue's minimum %s", fmtValue(*t.Min))
		case t.Max != nil && v > *t.Max:
			v, clip = *t.Max, fmt.Sprintf("above the catalogue's maximum %s", fmtValue(*t.Max))
		}
		row.Value = v
		if clip != "" {
			row.Note = clip + ": clipped to " + fmtValue(v)
		}
		for _, s := range seen {
			if math.Abs(s-v) < 1e-12 {
				if clip != "" {
					row.Skipped = fmt.Sprintf("outside the valid range (%s): clipped to %s, a value already in the table", clip, fmtValue(v))
				} else {
					row.Skipped = fmt.Sprintf("%+g %% of %s rounds to %s, a value already in the table", step, fmtValue(proposed), fmtValue(v))
				}
				row.Note = ""
				break
			}
		}
		if row.Skipped == "" {
			seen = append(seen, v)
		}
		rows = append(rows, row)
	}
	// The proposed value sits between −10 % and +10 %.
	at := len(rows) / 2
	rows = append(rows[:at], append([]NeighbourRow{{StepPct: 0, Value: proposed}}, rows[at:]...)...)
	return rows
}

// roundTo rounds v to the setting's step (whole numbers for an integer, six
// decimals when the catalogue gives no step).
func roundTo(v float64, t config.Tunable) float64 {
	step := t.Step
	if t.Kind == config.KindInteger && step < 1 {
		step = 1
	}
	if step <= 0 {
		step = 1e-6
	}
	r := math.Round(v/step) * step
	return math.Round(r*1e9) / 1e9 // drop float noise (0.1 × 3)
}

func rangeOf(t config.Tunable) string {
	switch {
	case t.Min != nil && t.Max != nil:
		return fmtValue(*t.Min) + " to " + fmtValue(*t.Max)
	case t.Min != nil:
		return "at least " + fmtValue(*t.Min)
	case t.Max != nil:
		return "at most " + fmtValue(*t.Max)
	}
	return "no bound of its own (Validate's cross rules still apply)"
}

func fmtValue(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// RunNeighbours runs the backtest and the replay of every planned neighbour
// that is not skipped — the proposed config file with only nb.Key changed,
// through config.Load (a value Validate refuses is skipped with its error) —
// takes the proposed value's figures from r, and flags the cliffs.
func RunNeighbours(in Inputs, r Report, nb Neighbours) Neighbours {
	rows := append([]NeighbourRow(nil), nb.Rows...)
	for i := range rows {
		row := &rows[i]
		if row.Skipped != "" {
			continue
		}
		if row.StepPct == 0 {
			row.Backtest, row.Replay = backtestFigures(r.Backtest.Note, r.Backtest.Proposed), replayFigures(r.Replay, r.Replay.Proposed)
			continue
		}
		cfg, err := loadWith(in.ProposedPath, nb.Key, row.Value)
		if err != nil {
			row.Skipped = "refused by Validate: " + err.Error()
			continue
		}
		row.Backtest, row.Replay = neighbourBacktest(in, cfg), neighbourReplay(in, cfg)
	}
	nb.Rows = rows
	nb.Cliffs = FindCliffs(rows, nb.CliffThresholdPP)
	return nb
}

func neighbourBacktest(in Inputs, cfg *config.Config) NeighbourFigures {
	if in.CSV == "" {
		return NeighbourFigures{Note: noBacktestData}
	}
	return backtestFigures("", runBacktest(cfg, in.CSV, in.From, in.To))
}

func neighbourReplay(in Inputs, cfg *config.Config) NeighbourFigures {
	rp := Replay{Equity: in.Equity, Note: replayNote(in)}
	if rp.Note != "" {
		return NeighbourFigures{Note: rp.Note}
	}
	return replayFigures(rp, runReplay(in, cfg))
}

func backtestFigures(note string, b BacktestRun) NeighbourFigures {
	switch {
	case note != "":
		return NeighbourFigures{Note: note}
	case b.Error != "":
		return NeighbourFigures{Note: b.Error}
	}
	return NeighbourFigures{Ran: true, ReturnPct: b.ReturnPct, WorstWeekPct: b.WorstWeekPct, MaxDrawdownPct: b.MaxDrawdownPct,
		Fees: b.FeesUSD, Trades: b.Trades}
}

func replayFigures(rp Replay, run ReplayRun) NeighbourFigures {
	switch {
	case rp.Note != "":
		return NeighbourFigures{Note: rp.Note}
	case run.Error != "":
		return NeighbourFigures{Note: run.Error}
	}
	f := NeighbourFigures{Ran: true, ReturnPct: run.ReturnPct, WorstWeekPct: run.WorstWeekPct, Fees: run.Fees, Trades: run.Trades}
	if rp.Equity > 0 {
		f.MaxDrawdownPct = run.MaxDrawdown / rp.Equity * 100
	}
	return f
}

// FindCliffs compares, per source, each value with the next one that ran:
// a worst week or drawdown that moves by more than thresholdPP, or a return
// whose sign flips, is a cliff.
func FindCliffs(rows []NeighbourRow, thresholdPP float64) []Cliff {
	var out []Cliff
	sources := []struct {
		name string
		of   func(NeighbourRow) NeighbourFigures
	}{
		{"backtest", func(r NeighbourRow) NeighbourFigures { return r.Backtest }},
		{"replay", func(r NeighbourRow) NeighbourFigures { return r.Replay }},
	}
	for _, s := range sources {
		var prev *NeighbourRow
		for i := range rows {
			r := &rows[i]
			if r.Skipped != "" || !s.of(*r).Ran {
				continue
			}
			if prev != nil {
				out = append(out, cliffsBetween(s.name, *prev, *r, s.of(*prev), s.of(*r), thresholdPP)...)
			}
			prev = r
		}
	}
	return out
}

func cliffsBetween(source string, a, b NeighbourRow, fa, fb NeighbourFigures, thresholdPP float64) []Cliff {
	var out []Cliff
	add := func(figure, why string, before, after float64) {
		out = append(out, Cliff{Source: source, Figure: figure, From: a.Value, To: b.Value, Before: before, After: after,
			Detail: fmt.Sprintf("%s %s %s from %+.2f %% at %s to %+.2f %% at %s", source, strings.ReplaceAll(figure, "_", " "), why,
				before, fmtValue(a.Value), after, fmtValue(b.Value))})
	}
	moved := fmt.Sprintf("moves by more than %g pp", thresholdPP)
	if math.Abs(fb.WorstWeekPct-fa.WorstWeekPct) > thresholdPP {
		add(FigureWorstWeek, moved, fa.WorstWeekPct, fb.WorstWeekPct)
	}
	if math.Abs(fb.MaxDrawdownPct-fa.MaxDrawdownPct) > thresholdPP {
		add(FigureDrawdown, moved, fa.MaxDrawdownPct, fb.MaxDrawdownPct)
	}
	if sign(fa.ReturnPct)*sign(fb.ReturnPct) < 0 {
		add(FigureReturn, "flips sign", fa.ReturnPct, fb.ReturnPct)
	}
	return out
}

// loadWith loads the config file at path with key set to value, through
// config.Load (defaults, fixed values, Validate) on a temporary copy.
func loadWith(path, key string, value float64) (*config.Config, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc any
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	if doc, err = setPath(doc, key, value); err != nil {
		return nil, err
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, err
	}
	f, err := os.CreateTemp("", "neighbour-*.yaml")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(out); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return config.Load(f.Name())
}

var segRe = regexp.MustCompile(`^([a-z0-9_]+)((?:\[\d+\])*)$`)

// setPath sets key ("a.b", "list[1].deltas[0]") in a decoded YAML document.
// A missing map key is added; a missing list element is an error.
func setPath(doc any, key string, value float64) (any, error) {
	head, rest, nested := strings.Cut(key, ".")
	m := segRe.FindStringSubmatch(head)
	if m == nil {
		return nil, fmt.Errorf("key %q: cannot read %q", key, head)
	}
	obj, ok := doc.(map[string]any)
	if doc == nil {
		obj, ok = map[string]any{}, true
	}
	if !ok {
		return nil, fmt.Errorf("key %q: %q is not a section", key, head)
	}
	var idx []int
	for _, s := range indexRe.FindAllString(m[2], -1) {
		n, _ := strconv.Atoi(strings.Trim(s, "[]"))
		idx = append(idx, n)
	}
	set := func(cur any) (any, error) {
		if nested {
			return setPath(cur, rest, value)
		}
		return value, nil
	}
	child, err := setIndexed(obj[m[1]], idx, set, key)
	if err != nil {
		return nil, err
	}
	obj[m[1]] = child
	return obj, nil
}

func setIndexed(cur any, idx []int, set func(any) (any, error), key string) (any, error) {
	if len(idx) == 0 {
		return set(cur)
	}
	list, ok := cur.([]any)
	if !ok || idx[0] >= len(list) {
		return nil, fmt.Errorf("key %q: no element %d in the proposed file", key, idx[0])
	}
	v, err := setIndexed(list[idx[0]], idx[1:], set, key)
	if err != nil {
		return nil, err
	}
	list[idx[0]] = v
	return list, nil
}
