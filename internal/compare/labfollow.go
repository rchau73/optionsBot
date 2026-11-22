package compare

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"optionsbot/internal/evidence"
	"optionsbot/internal/history"
	"optionsbot/internal/idlemargin"
	"optionsbot/internal/orders"
	"optionsbot/internal/perf"
	"optionsbot/internal/replay"
)

// The lab follow-up (#855, epic E9, cmd/labfollow): once a manager's
// recorded change (COMPARE_DIR/applied/<run id>.json) has gone live, the
// run's backtested return, worst week, drawdown and fees beside what the bot
// realised over the days after it (perf.Summarize over the P&L history, the
// journal's close fees), and the gap in percentage points. The day the change
// reached the bot is given by a person (-live-from) and may be kept in
// applied/<run id>.live. Percentages and counts only: no setting value,
// amount, name or account detail. Read-only, except the .live file the
// person asks for; nothing here writes a config, edits a record or calls a
// bot or the exchange.

// LabFollowDays is the default window: the runner's 7 days.
const LabFollowDays = 7

// LabRealisedLabel is the label of every realised figure (the backtested
// ones carry LabLabel).
const LabRealisedLabel = "realised, one short window"

// LabFollowNotEvidence is the line every follow-up ends with.
const LabFollowNotEvidence = "A single week is not evidence: one short window of live trading can differ from the backtest by chance alone; judge the lab after several recorded changes."

// The follow-up's states.
const (
	LabFollowCompared = "compared"
	LabFollowTooEarly = "too early to judge"
	LabFollowNoData   = "no data"
)

// LabLiveFromSuffix names the file beside a record that holds its live-from day.
const LabLiveFromSuffix = ".live"

// LabFollowInput is everything BuildLabFollow needs, read by ReadLabFollow.
type LabFollowInput struct {
	RunID        string
	Record       AppliedRecord
	Report       *Report // nil when the run's report.json cannot be read
	ReportNote   string  // why Report is nil
	LiveFrom     time.Time
	Days         int
	Now          time.Time
	Points       []history.Point // the bot's P&L history
	Trades       []orders.Trade  // the journal's opens and closes
	Equity       float64         // coin, the basis of the realised percentages (≤ 0: unknown)
	EquitySource string          // where Equity came from, never the amount
}

// LabFollowSetting is one changed setting, without its values.
type LabFollowSetting struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

// LabFollowFigure is one figure, backtested beside realised, in % of equity.
// A nil value is not known; its note says why. Gap = realised − backtested,
// in percentage points, only when the window is over and both are known.
type LabFollowFigure struct {
	Name            string   `json:"name"`
	Backtested      *float64 `json:"backtested_pct"`
	BacktestedLabel string   `json:"backtested_label"`
	Realised        *float64 `json:"realised_pct"`
	RealisedLabel   string   `json:"realised_label"`
	RealisedNote    string   `json:"realised_note,omitempty"`
	GapPP           *float64 `json:"gap_pp"`
	Worse           bool     `json:"worse"` // a worst week or drawdown deeper than the backtest's
}

// LabFollow is the follow-up of one recorded change.
type LabFollow struct {
	RunID        string             `json:"run_id"`
	Underlying   string             `json:"underlying"`
	Config       string             `json:"config"`
	Settings     []LabFollowSetting `json:"settings"`
	LiveFrom     string             `json:"live_from"` // YYYY-MM-DD
	Days         int                `json:"days"`
	From         time.Time          `json:"from"`
	To           time.Time          `json:"to"` // excluded
	DaysPassed   int                `json:"days_passed"`
	State        string             `json:"state"`
	StateText    string             `json:"state_text"`
	BacktestNote string             `json:"backtest_note,omitempty"` // why the backtested figures are missing
	BacktestDays int                `json:"backtest_days"`           // the run's span; return and fees are scaled from it to Days
	Points       int                `json:"points"`                  // P&L history points in the window
	Closes       int                `json:"closes"`                  // journal closes in the window
	DaysCovered  int                `json:"days_covered"`
	EquityKnown  bool               `json:"equity_known"`
	EquitySource string             `json:"equity_source"`
	Figures      []LabFollowFigure  `json:"figures"`
	Flags        []string           `json:"flags"`
	NotEvidence  string             `json:"not_evidence"`
}

// ParseLiveFrom reads a YYYY-MM-DD day as UTC midnight.
func ParseLiveFrom(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, fmt.Errorf("live-from %q: want YYYY-MM-DD", s)
	}
	return t, nil
}

// CheckLiveFrom refuses a live-from day in the future or before the day the
// change was recorded: the change cannot have reached the bot then.
func CheckLiveFrom(rec AppliedRecord, liveFrom, now time.Time) error {
	if liveFrom.After(utcDay(now)) {
		return fmt.Errorf("live-from %s is in the future", liveFrom.Format("2006-01-02"))
	}
	at, err := time.Parse(time.RFC3339, rec.RecordedAt)
	if err != nil {
		return fmt.Errorf("the record's recorded_at %q cannot be read", rec.RecordedAt)
	}
	if liveFrom.Before(utcDay(at)) {
		return fmt.Errorf("live-from %s is before the change was recorded (%s)", liveFrom.Format("2006-01-02"), utcDay(at).Format("2006-01-02"))
	}
	return nil
}

func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// BuildLabFollow is the follow-up of in: pure, now passed in.
func BuildLabFollow(in LabFollowInput) LabFollow {
	days := in.Days
	if days < 1 {
		days = LabFollowDays
	}
	from := utcDay(in.LiveFrom)
	f := LabFollow{RunID: in.RunID, Underlying: in.Record.Account.Underlying, Config: in.Record.Account.Config,
		Settings: []LabFollowSetting{}, LiveFrom: from.Format("2006-01-02"), Days: days, From: from, To: from.AddDate(0, 0, days),
		EquitySource: in.EquitySource, EquityKnown: in.Equity > 0, Figures: []LabFollowFigure{}, Flags: []string{}, NotEvidence: LabFollowNotEvidence}
	for _, c := range in.Record.Changes {
		f.Settings = append(f.Settings, LabFollowSetting{Key: c.Key, Title: c.Title})
	}
	if passed := in.Now.Sub(from); passed > 0 {
		f.DaysPassed = int(passed / (24 * time.Hour))
	}

	bt := labBacktestFigures(in, days, &f)
	s := perf.Summarize(in.Points, in.Equity, f.From, f.To.Add(-time.Nanosecond))
	f.Points, f.DaysCovered = s.Points, s.DaysCovered
	fees := 0.0
	for _, t := range in.Trades {
		if t.Kind == "close" && !t.At.Before(f.From) && t.At.Before(f.To) {
			f.Closes++
			fees += t.Fees
		}
	}
	switch {
	case in.Now.Before(f.To):
		f.State = LabFollowTooEarly
		f.StateText = fmt.Sprintf("too early to judge: %d of %d whole UTC days have passed since %s", min(f.DaysPassed, days), days, f.LiveFrom)
	case s.Points == 0:
		f.State = LabFollowNoData
		f.StateText = "no data: the P&L history has no point in the window"
	default:
		f.State = LabFollowCompared
		f.StateText = fmt.Sprintf("compared: %d P&L points on %d of %d days, %d closes", s.Points, s.DaysCovered, days, f.Closes)
	}

	var rl [4]*float64
	realisedNote := [4]string{}
	if f.State == LabFollowCompared && f.EquityKnown {
		rl[0], rl[2], rl[3] = ptr(s.ReturnPct), ptr(s.MaxDrawdownPct), ptr(fees/in.Equity*100)
		switch {
		case s.WorstWeekKnown:
			rl[1] = ptr(s.WorstWeekPct)
		case days <= 7:
			rl[1] = ptr(s.ReturnPct)
			realisedNote[1] = "the window is its own worst week: its return"
		default:
			realisedNote[1] = "the history does not span a week"
		}
	}
	for i := range realisedNote {
		switch {
		case f.State != LabFollowCompared:
			realisedNote[i] = f.State
		case !f.EquityKnown:
			realisedNote[i] = "equity unknown"
		}
	}
	for i, name := range []string{"Return", "Worst week", "Largest drawdown", "Fees"} {
		fig := LabFollowFigure{Name: name, Backtested: bt[i], BacktestedLabel: LabLabel, Realised: rl[i],
			RealisedLabel: LabRealisedLabel, RealisedNote: realisedNote[i]}
		if fig.Backtested != nil && fig.Realised != nil {
			fig.GapPP = ptr(*fig.Realised - *fig.Backtested)
			if (i == 1 || i == 2) && *fig.Realised < *fig.Backtested {
				fig.Worse = true
				f.Flags = append(f.Flags, fmt.Sprintf("Worse than the backtest: the realised %s (%s %%) was deeper than the backtested one (%s %%).",
					strings.ToLower(name), pctText(*fig.Realised), pctText(*fig.Backtested)))
			}
		}
		f.Figures = append(f.Figures, fig)
	}
	return f
}

// labBacktestFigures are the run's proposed backtest in % of its equity:
// return, worst week, largest drawdown (≤ 0) and fees, the return and fees
// scaled linearly from the run's span to the window's days so the gap
// compares like with like. Missing ones are nil, the reason in f.
func labBacktestFigures(in LabFollowInput, days int, f *LabFollow) [4]*float64 {
	var out [4]*float64
	r := in.Report
	switch {
	case r == nil:
		f.BacktestNote = "the run's report cannot be read: " + in.ReportNote
		return out
	case !r.Backtest.Ran:
		f.BacktestNote = "the run has no backtest: " + dashed(r.Backtest.Note)
		return out
	case r.Backtest.Proposed.Error != "":
		f.BacktestNote = "the run's backtest failed"
		return out
	case r.Backtest.EquityUSD <= 0:
		f.BacktestNote = "the run's backtest has no starting equity"
		return out
	}
	span := int(r.To.Sub(r.From)/(24*time.Hour)) + 1 // To is included
	if span < 1 {
		span = 1
	}
	f.BacktestDays = span
	scale := float64(days) / float64(span)
	p := r.Backtest.Proposed
	out[0] = ptr(p.ReturnPct * scale)
	out[1] = ptr(p.WorstWeekPct)
	out[2] = ptr(-math.Abs(p.MaxDrawdownPct)) // the backtest reports its depth, perf a fall
	out[3] = ptr(p.FeesUSD / r.Backtest.EquityUSD * 100 * scale)
	return out
}

func ptr(x float64) *float64 {
	r := math.Round(x*100) / 100
	return &r
}

// pctText is a percentage with two decimals and a real minus sign.
func pctText(x float64) string {
	return strings.Replace(fmt.Sprintf("%.2f", x), "-", "−", 1)
}

func figText(x *float64, note string) string {
	if x == nil {
		return dashed(note)
	}
	return pctText(*x) + " %"
}

// LabFollowMarkdown is one follow-up as a Markdown subsection.
func LabFollowMarkdown(f LabFollow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### Run %s\n\n", f.RunID)
	keys := make([]string, len(f.Settings))
	for i, s := range f.Settings {
		keys[i] = fmt.Sprintf("`%s` (%s)", s.Key, dashed(s.Title))
	}
	fmt.Fprintf(&b, "%s (%s). Settings changed: %s.\n\n", f.Underlying, f.Config, strings.Join(keys, ", "))
	fmt.Fprintf(&b, "Live from %s; window %s – %s UTC (%d days).\n\n", f.LiveFrom, f.From.Format("2006-01-02"),
		f.To.AddDate(0, 0, -1).Format("2006-01-02"), f.Days)
	fmt.Fprintf(&b, "State: %s.\n\n", f.StateText)
	b.WriteString("| Figure (% of equity) | Backtested, not a forecast | Realised, one short window | Gap (points) |\n|---|---|---|---|\n")
	for _, fig := range f.Figures {
		gap := "–"
		if fig.GapPP != nil {
			gap = strings.Replace(fmt.Sprintf("%+.2f", *fig.GapPP), "-", "−", 1)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", fig.Name, figText(fig.Backtested, "not known"), figText(fig.Realised, fig.RealisedNote), gap)
	}
	b.WriteString("\n")
	if f.BacktestNote != "" {
		fmt.Fprintf(&b, "Backtested figures missing: %s.\n\n", f.BacktestNote)
	} else {
		fmt.Fprintf(&b, "Backtested return and fees are scaled from the run's %d days to the window's %d; worst week and drawdown are the run's own. Backtested figures are of the backtest's starting equity.\n\n", f.BacktestDays, f.Days)
	}
	equity := "unknown"
	if f.EquityKnown {
		equity = f.EquitySource
	}
	fmt.Fprintf(&b, "Realised figures are of the account's equity at the start (%s).\n\n", equity)
	for _, fl := range f.Flags {
		fmt.Fprintf(&b, "- %s\n", fl)
	}
	if len(f.Flags) > 0 {
		b.WriteString("\n")
	}
	b.WriteString(f.NotEvidence + "\n")
	return b.String()
}

// LabNoFollowUps is the section of a lab with no live-from day on file.
const LabNoFollowUps = "no recorded change has a live-from day on file"

// LabFollowSectionMarkdown is the "Lab follow-up" section: each follow-up,
// then the records that could not be followed with their reason.
func LabFollowSectionMarkdown(fs []LabFollow, refused map[string]string) string {
	var b strings.Builder
	b.WriteString("## Lab follow-up\n\n")
	if len(fs) == 0 && len(refused) == 0 {
		b.WriteString(LabNoFollowUps + ".\n")
		return b.String()
	}
	for i, f := range fs {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(LabFollowMarkdown(f))
	}
	ids := make([]string, 0, len(refused))
	for id := range refused {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) > 0 {
		b.WriteString("\nNot followed:\n\n")
	}
	for _, id := range ids {
		fmt.Fprintf(&b, "- run %s: %s\n", id, refused[id])
	}
	return b.String()
}

// LabFollowOptions are where ReadLabFollow reads.
type LabFollowOptions struct {
	CompareDir string  // COMPARE_DIR: applied/<id>.json, <id>/report.json
	Root       string  // the environment's clone: data/<bot>/orders.log and pnl_history.jsonl, data/stress/<bot>/account.json
	Journal    string  // overrides the journal under Root
	History    string  // overrides the P&L history under Root
	Equity     float64 // coin; > 0 overrides the run's and account.json's
}

// ReadAppliedRecord reads applied/<id>.json.
func ReadAppliedRecord(compareDir, id string) (AppliedRecord, error) {
	var rec AppliedRecord
	if !runIDRe.MatchString(id) {
		return rec, fmt.Errorf("run id %q: want a folder time like 20261012T093000Z", id)
	}
	raw, err := os.ReadFile(filepath.Join(compareDir, "applied", id+".json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return rec, fmt.Errorf("run %s has no recorded change (applied/%s.json)", id, id)
		}
		return rec, fmt.Errorf("read the record: %w", err)
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		return rec, fmt.Errorf("the record cannot be read: %w", err)
	}
	if rec.RunID != id {
		return rec, fmt.Errorf("the record is for run %q, not %q", rec.RunID, id)
	}
	return rec, nil
}

// ReadLabFollow reads the inputs of one follow-up. An error is bad input (no
// record, a live-from day refused by CheckLiveFrom); a journal or history that
// cannot be read is an empty one, so the follow-up says "no data".
func ReadLabFollow(o LabFollowOptions, id string, liveFrom time.Time, days int, now time.Time) (LabFollowInput, error) {
	rec, err := ReadAppliedRecord(o.CompareDir, id)
	if err != nil {
		return LabFollowInput{}, err
	}
	if err := CheckLiveFrom(rec, liveFrom, now); err != nil {
		return LabFollowInput{}, err
	}
	in := LabFollowInput{RunID: id, Record: rec, LiveFrom: liveFrom, Days: days, Now: now}
	if in.Report, err = readRunReport(o.CompareDir, id); err != nil {
		in.ReportNote = err.Error()
	}
	bot := strings.ToLower(rec.Account.Underlying)
	p := evidence.BotPaths(o.Root, o.Root, bot)
	if o.Journal != "" {
		p.Journal = o.Journal
	}
	if o.History != "" {
		p.History = o.History
	}
	if rp, err := orders.ReplayFile(p.Journal, 0); err == nil {
		in.Trades = rp.Trades
	}
	if pts, _, err := idlemargin.ReadHistoryFile(p.History); err == nil {
		in.Points = pts
	}
	switch acct := filepath.Join(p.Stress, "account.json"); {
	case o.Equity > 0:
		in.Equity, in.EquitySource = o.Equity, "-equity"
	case in.Report != nil && in.Report.Replay.Equity > 0:
		in.Equity, in.EquitySource = in.Report.Replay.Equity, "the run's replay equity, the same basis as its report"
	default:
		in.Equity, in.EquitySource = replay.ReadEquity(acct, bot), "margin balance in data/stress/"+bot+"/account.json"
	}
	return in, nil
}

// LiveFromPath is applied/<id>.live.
func LiveFromPath(compareDir, id string) string {
	return filepath.Join(compareDir, "applied", id+LabLiveFromSuffix)
}

// ReadLiveFrom reads the live-from day kept for id; ok is false when none is.
func ReadLiveFrom(compareDir, id string) (t time.Time, ok bool, err error) {
	raw, err := os.ReadFile(LiveFromPath(compareDir, id))
	if errors.Is(err, os.ErrNotExist) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	t, err = ParseLiveFrom(string(raw))
	return t, err == nil, err
}

// RecordLiveFrom keeps the live-from day of id in applied/<id>.live (one line).
func RecordLiveFrom(compareDir, id string, liveFrom time.Time) error {
	return os.WriteFile(LiveFromPath(compareDir, id), []byte(liveFrom.Format("2006-01-02")+"\n"), 0o600)
}

// LiveFromIDs are the run ids with a live-from day on file, sorted.
func LiveFromIDs(compareDir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(compareDir, "applied"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if id, ok := strings.CutSuffix(e.Name(), LabLiveFromSuffix); ok && runIDRe.MatchString(id) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}
