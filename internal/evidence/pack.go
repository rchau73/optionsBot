package evidence

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The evidence pack (#700): one document joining what the other reports
// already wrote — the track record, the trend, and the latest weekly report's
// scoreboard, replay vs live and hedge trial — for a sceptical reader or
// counsel. It copies figures and never recomputes one; a source that is
// missing reads "not reported". Every figure stands next to its source file,
// its window and its caveat, the risks in the same table at the same size,
// and every value in one format, so a loss reads like a gain. A live account
// shows percentages, ratios and counts only.

// PackLabel heads every pack: counsel approves any external use (legal brief,
// performance claims).
const PackLabel = "internal, not for external use before counsel approves (legal brief)"

// The caveats next to every figure. The ticket's words are for testnet; a
// live account's figures are not testnet, so its caveat says live instead.
const (
	PackCaveat     = "testnet, past result, not a forecast"
	PackCaveatLive = "live account, past result, not a forecast"
)

// The replay-vs-live labels every section carries (else NotReported).
const (
	PackComparable    = "comparable"
	PackPartial       = "partial"
	PackNotComparable = "not comparable"
)

// The pack's units of a value that is not a coin amount.
const (
	packPct   = "%"
	packPP    = "pp"
	packRatio = "ratio"
	packCount = "count"
	packHours = "h"
)

const packUnknown = "unknown"

// PackRiskName names a risk's row in the Markdown table.
const PackRiskName = "Risk"

// PackFigure is one row. Value is the source's number, unrounded; nil when
// Display is a status or says why there is none. Display is what is printed.
type PackFigure struct {
	Name    string   `json:"name"`
	Value   *float64 `json:"value"`
	Unit    string   `json:"unit,omitempty"`
	Display string   `json:"display"`
	Sample  *Sample  `json:"sample,omitempty"` // how much data stands behind it (#772); nil only on a row without a number
	Source  string   `json:"source"`
	Window  string   `json:"window"`
	Caveat  string   `json:"caveat"`
}

// PackSection is one block: its figures and its risks.
type PackSection struct {
	Title        string       `json:"title"`
	Caveat       string       `json:"caveat"`
	ReplayVsLive string       `json:"replay_vs_live"`
	Figures      []PackFigure `json:"figures"`
	Risks        []string     `json:"risks"`
}

// Pack is the document, its sections in reading order.
type Pack struct {
	Label        string        `json:"label"`
	Env          string        `json:"env"`
	Bot          string        `json:"bot"`
	Account      string        `json:"account"` // testnet | live
	AmountsShown bool          `json:"amounts_shown"`
	Sections     []PackSection `json:"sections"`
	Unreadable   []string      `json:"unreadable,omitempty"` // files that did not parse
}

// PackInputs are the reports read, already loaded; nil = no such report.
// Paths are as printed. EnvsDir, when set, is taken out of the sources' notes.
type PackInputs struct {
	Env, Bot, Account string
	Track             *TrackRecord
	TrackPath         string
	Trend             *Trend
	TrendPath         string
	Latest            *Report // the latest weekly report
	LatestPath        string
	Unreadable        []string
	EnvsDir           string
}

// packRows collects a section's rows, all with the same source and caveat
// and, unless a row brings its own, the same sample (nil = none known).
type packRows struct {
	source, window, caveat string
	sample                 *Sample
	figures                []PackFigure
}

func (r *packRows) add(f PackFigure) {
	f.Source, f.Caveat = r.source, r.caveat
	if f.Window == "" {
		f.Window = r.window
	}
	if f.Sample == nil {
		f.Sample = r.sample
	}
	r.figures = append(r.figures, f)
}

func (r *packRows) value(name string, v float64, unit string) {
	r.add(PackFigure{Name: name, Value: ptr(v), Unit: unit, Display: packDisplay(v, unit)})
}

func (r *packRows) text(name, text string) {
	r.add(PackFigure{Name: name, Display: text})
}

// valueOr is the value when known, else "unknown".
func (r *packRows) valueOr(name string, v float64, unit string, known bool) {
	if known {
		r.value(name, v, unit)
	} else {
		r.text(name, packUnknown)
	}
}

// packDisplay is one format for a gain and a loss: sign, number, unit.
func packDisplay(v float64, unit string) string {
	switch unit {
	case packPct:
		return fmt.Sprintf("%+.2f %%", v)
	case packPP:
		return fmt.Sprintf("%+.2f pp", v)
	case packRatio:
		return fmt.Sprintf("%.2f", v)
	case packCount:
		return fmt.Sprintf("%.0f", v)
	case packHours:
		return fmt.Sprintf("%.1f h", v)
	}
	return fmt.Sprintf("%+.4f %s", v, unit) // a coin
}

// BuildPack is the pure pack: the same inputs give the same JSON.
func BuildPack(in PackInputs) Pack {
	p := Pack{Label: PackLabel, Env: in.Env, Bot: in.Bot, Account: packAccount(in), Unreadable: in.Unreadable}
	p.AmountsShown = p.Account != AccountLive
	caveat := PackCaveat
	if !p.AmountsShown {
		caveat = PackCaveatLive
	}
	label := packRVLLabel(in.Latest)
	section := func(title string, r packRows, risks ...string) {
		p.Sections = append(p.Sections, PackSection{Title: title, Caveat: caveat, ReplayVsLive: label, Figures: r.figures, Risks: risks})
	}
	risks := []string{
		"A short strangle can lose far more than the premium it collects: the worst week and the max drawdown are what this window showed, not a limit on a loss.",
		"The net return in coin and the return of holding the coin in USD are different units: read holding against the strategy in USD.",
	}
	if p.AmountsShown {
		risks = append(risks, "Testnet prices and fills are not mainnet's: the same rules on a live account can give a different result.")
	}
	section("Headline", packHeadline(in, p.AmountsShown, caveat), append(risks, InceptionNote)...)
	section("Twelve-week scoreboard", packScoreboard(in, caveat), "A window of twelve weeks is short: one sharp move can change the status.")
	section("Replay vs live", packReplayVsLive(in, p.AmountsShown, caveat), packUnmodelled(in.Latest))
	section("Weeks missing", packWeeksMissing(in, caveat),
		"A week with no report counts in no weekly figure, and nothing is filled in; the week in progress has no report until it ends.")
	section("Futures hedge trial", packHedge(in, p.AmountsShown, caveat), HedgeTrialLabel)
	return p
}

// packAccount is live when the clone or any report read says so: an amount
// must not reach the pack because one of them is out of date.
func packAccount(in PackInputs) string {
	accounts := []string{in.Account}
	if in.Track != nil {
		accounts = append(accounts, in.Track.Account)
	}
	if in.Latest != nil {
		accounts = append(accounts, in.Latest.RVL.Account, in.Latest.Hedge.Account)
	}
	for _, a := range accounts {
		if a == AccountLive {
			return AccountLive
		}
	}
	return AccountTestnet
}

// packRVLLabel is the latest report's replay-vs-live state in one word.
func packRVLLabel(r *Report) string {
	switch {
	case r == nil || r.RVL.Label == "":
		return NotReported
	case r.Replay.Partial != "":
		return PackPartial
	case r.RVL.Comparable:
		return PackComparable
	}
	return PackNotComparable
}

// packWeeks are the week rows and counts: the track record's whole span,
// else the trend's. Nil rows = neither was read.
func packWeeks(in PackInputs) (rows []WeekRow, c Counts, source string) {
	if in.Track != nil && len(in.Track.Weeks) > 0 {
		return in.Track.Weeks, in.Track.Counts, in.TrackPath
	}
	if in.Trend != nil && len(in.Trend.Weeks) > 0 {
		return in.Trend.Weeks, in.Trend.Counts, in.TrendPath
	}
	return nil, Counts{}, ""
}

func packDays(from, to time.Time) string {
	return from.UTC().Format(time.DateOnly) + " → " + to.UTC().Format(time.DateOnly) + " UTC"
}

func packMinutes(from, to time.Time) string {
	const layout = "2006-01-02 15:04"
	return from.UTC().Format(layout) + " → " + to.UTC().Format(layout) + " UTC"
}

// packHeadline is the whole span, read from the track record, with the weeks
// it covers first.
func packHeadline(in PackInputs, amounts bool, caveat string) packRows {
	r := packRows{source: in.TrackPath, window: NotReported, caveat: caveat}
	weeks, c, weeksSource := packWeeks(in)
	if len(weeks) > 0 {
		r.window = weeks[0].Week + " → " + weeks[len(weeks)-1].Week
		r.source = weeksSource
	}
	if in.Track != nil && in.Track.Known {
		r.window = packDays(in.Track.From, in.Track.To)
	}
	if weeks != nil {
		r.sample = ptrTo(WeeksSample(c.Reported, nil, ""))
		r.add(PackFigure{Name: "Weeks covered", Value: ptr(float64(c.Reported)), Unit: packCount,
			Display: fmt.Sprintf("%d of %d ISO weeks have a report", c.Reported, c.Reported+c.NotReported)})
		r.value("Weeks with a loss", float64(c.Lost), packCount)
		r.add(PackFigure{Name: "Weeks beating holding the coin", Value: ptr(float64(c.BeatHold)), Unit: packCount,
			Display: fmt.Sprintf("%d of %d with the excess known", c.BeatHold, c.HoldKnown)})
	} else {
		r.text("Weeks covered", NotReported)
	}
	if in.Track == nil || !in.Track.Known {
		r.sample = nil
		for _, name := range []string{"Net return", "Holding the coin", "Worst 7 days", "Max drawdown"} {
			r.text(name, NotReported)
		}
		if weeks != nil {
			r.sample = ptrTo(WeeksSample(c.Reported, intPtr(c.TailEvents), TailEventsName))
		}
		packWorstISOWeek(&r, c)
		return r
	}
	r.source = in.TrackPath
	s, unit := in.Track.Perf, strings.ToUpper(in.Bot)
	known := s.EquityKnown
	// The results rest on the days of history and the tail events in them;
	// holding the coin on the days only.
	r.sample = ptrTo(SpanSample(s.DaysCovered, intPtr(c.TailEvents), TailEventsName))
	if amounts {
		r.value("Net return in coin", s.NetPnL, unit)
	}
	r.valueOr("Net return, % of the starting equity, in coin", s.ReturnPct, packPct, known)
	hold := packRows{source: r.source, window: r.window, caveat: caveat, sample: ptrTo(SpanSample(s.DaysCovered, nil, ""))}
	hold.valueOr("Holding the coin over the same window, in USD", s.HoldReturnUSDPct, packPct, s.HoldKnown)
	r.figures = append(r.figures, hold.figures...)
	r.valueOr("The strategy in USD", s.ReturnUSDPct, packPct, known && s.HoldKnown)
	r.valueOr("Excess over holding the coin", s.ExcessReturnPct, packPP, known && s.HoldKnown)
	if s.WorstWeekKnown {
		worst := packRows{source: r.source, window: packMinutes(s.WorstWeekStart, s.WorstWeekEnd), caveat: caveat, sample: r.sample}
		if amounts {
			worst.value("Worst 7 days in coin", s.WorstWeek, unit)
		}
		worst.valueOr("Worst 7 days, % of the starting equity", s.WorstWeekPct, packPct, known)
		r.figures = append(r.figures, worst.figures...)
	} else {
		r.text("Worst 7 days", "the history does not span 7 days")
	}
	packWorstISOWeek(&r, c)
	if amounts {
		r.value("Max drawdown in coin", s.MaxDrawdown, unit)
	}
	r.valueOr("Max drawdown, % of the starting equity", s.MaxDrawdownPct, packPct, known)
	return r
}

func packWorstISOWeek(r *packRows, c Counts) {
	if c.WorstWeek == "" {
		return
	}
	f := PackFigure{Name: "Worst ISO week, % of that week's starting equity", Display: packUnknown, Window: c.WorstWeek}
	if c.WorstWeekPct != nil {
		f.Value, f.Unit, f.Display = ptr(*c.WorstWeekPct), packPct, packDisplay(*c.WorstWeekPct, packPct)
	}
	r.add(f)
}

func packScoreboard(in PackInputs, caveat string) packRows {
	r := packRows{source: in.LatestPath + " (scoreboard)", window: NotReported, caveat: caveat}
	if in.Latest == nil || in.Latest.Score.Label == "" {
		r.source = in.LatestPath
		r.text("Status", NotReported)
		return r
	}
	s := in.Latest.Score
	r.window = in.Latest.Week
	if s.Status == "" {
		r.text("Status", "not computed: "+s.Note)
		return r
	}
	if s.FirstWeek != "" {
		r.window = s.FirstWeek + " → " + s.LastWeek
	}
	r.sample = ptrTo(WeeksSample(s.WeeksCounted, nil, ""))
	r.text("Status", s.Status)
	r.add(PackFigure{Name: "Weeks counted", Value: ptr(float64(s.WeeksCounted)), Unit: packCount,
		Display: fmt.Sprintf("%d of %d", s.WeeksCounted, s.WeeksNeeded)})
	ratio := func(name string, v *float64) {
		if v != nil {
			r.value(name, *v, packRatio)
		} else {
			r.text(name, "no ratio")
		}
	}
	ratio("Engine ratio (weekly Sharpe)", s.EngineRatio)
	ratio("Holding the coin ratio (weekly Sharpe)", s.HoldRatio)
	missing := "none"
	if len(s.MissingWeeks) > 0 {
		missing = strings.Join(s.MissingWeeks, ", ")
	}
	r.text("Weeks missing from the window", missing)
	return r
}

func packReplayVsLive(in PackInputs, amounts bool, caveat string) packRows {
	r := packRows{source: in.LatestPath + " (replay_vs_live)", window: NotReported, caveat: caveat}
	label := packRVLLabel(in.Latest)
	if label == NotReported {
		r.source = in.LatestPath
		r.text("Replay vs live", NotReported)
		return r
	}
	rep, s := in.Latest, in.Latest.RVL
	r.window = rep.Week
	r.sample = ptrTo(rvlSample(*rep))
	r.text("Replay vs live", label)
	if rep.Replay.Partial != "" {
		r.text("Why partial", packPlain(in, rep.Replay.Partial))
	}
	if !s.Comparable {
		r.text("Why not comparable", packPlain(in, s.Note))
	}
	if s.MissingHours != nil {
		r.value("Market books missing", *s.MissingHours, packHours)
	}
	if !s.Comparable {
		return r
	}
	unit := strings.ToUpper(in.Bot)
	if amounts && s.Replay.Net != nil && s.Live.Net != nil {
		r.value("Replay result in coin", *s.Replay.Net, unit)
		r.value("Live result in coin", *s.Live.Net, unit)
	}
	r.value("Replay result, % of the starting equity", s.Replay.NetPct, packPct)
	r.value("Live result, % of the starting equity", s.Live.NetPct, packPct)
	r.value("Replay worst day, % of the starting equity", s.Replay.WorstDayPct, packPct)
	r.value("Live worst day, % of the starting equity", s.Live.WorstDayPct, packPct)
	if amounts && s.GapCoin != nil {
		r.value("Gap (replay − live) in coin", *s.GapCoin, unit)
	}
	if s.GapEquityPP != nil {
		r.value("Gap (replay − live), of the starting equity", *s.GapEquityPP, packPP)
	}
	if s.GapPct != nil {
		r.value("Gap, % of the live result", *s.GapPct, packPct)
	}
	return r
}

// packUnmodelled is the replay section's risk: what the replay leaves out.
func packUnmodelled(rep *Report) string {
	if rep == nil || len(rep.RVL.Unmodelled) == 0 {
		return "A replay runs the rules on recorded books: it is a model of the week, not what the account did."
	}
	return "The replay does not model: " + strings.Join(rep.RVL.Unmodelled, "; ") + "."
}

func packWeeksMissing(in PackInputs, caveat string) packRows {
	rows, _, source := packWeeks(in)
	r := packRows{source: source, window: NotReported, caveat: caveat}
	if rows == nil {
		r.source = in.TrackPath
		r.text("Weeks missing", NotReported)
		return r
	}
	r.window = rows[0].Week + " → " + rows[len(rows)-1].Week
	for _, w := range rows {
		if !w.Reported {
			r.add(PackFigure{Name: w.Week, Display: NotReported, Window: w.Week})
		}
	}
	if len(r.figures) == 0 {
		r.text("Weeks missing", "none")
	}
	return r
}

func packHedge(in PackInputs, amounts bool, caveat string) packRows {
	r := packRows{source: in.LatestPath + " (hedge_trial)", window: NotReported, caveat: caveat}
	if in.Latest == nil || in.Latest.Hedge.Label == "" {
		r.source = in.LatestPath
		r.text("Status", NotReported)
		return r
	}
	h := in.Latest.Hedge
	r.window = in.Latest.Week
	if h.Since != nil && h.ReadAt != nil {
		r.window = packMinutes(*h.Since, *h.ReadAt)
	}
	r.sample = hedgeSample(h)
	r.text("Status", h.Status)
	if h.Note != "" {
		r.text("Why no figure", packPlain(in, h.Note))
	}
	if h.Mode != "" {
		r.text("Mode when read", h.Mode)
	}
	count := func(name string, v *int) {
		if v != nil {
			r.value(name, float64(*v), packCount)
		}
	}
	count("Days measured", h.Days)
	count("Days still missing", h.DaysMissing)
	count("Real trades", h.RealTrades)
	count("Paper trades", h.PaperTrades)
	count("Counterfactual legs", h.Legs)
	if amounts && h.Advantage != nil {
		unit := strings.ToUpper(in.Bot)
		r.value("Advantage of the hedge over the shed", *h.Advantage, unit)
		if h.HedgePnL != nil {
			r.value("Hedge P&L, net of fees", *h.HedgePnL, unit)
		}
		if h.WorstDay != nil {
			r.add(PackFigure{Name: "Worst day of shed decisions", Value: ptr(h.WorstDay.Value), Unit: unit,
				Display: packDisplay(h.WorstDay.Value, unit), Window: h.WorstDay.Date})
		}
	}
	return r
}

// packPlain takes the machine's folders out of a source's note.
func packPlain(in PackInputs, note string) string {
	if in.EnvsDir == "" {
		return note
	}
	return strings.ReplaceAll(note, in.EnvsDir+string(filepath.Separator), "")
}

// PackOutPaths are the pack's files: <root>/data/evidence/pack/<bot>.md and
// .json.
func PackOutPaths(root, bot string) (md, js string) {
	dir := filepath.Join(root, "data", "evidence", "pack")
	return filepath.Join(dir, bot+".md"), filepath.Join(dir, bot+".json")
}

// LoadPack reads what the pack joins from the clone: the track record, the
// trend and the latest weekly report. A file that is absent stays nil, one
// that does not parse is also listed. Printed paths are relative to the clone.
func LoadPack(env, bot, root string, notes io.Writer) PackInputs {
	in := PackInputs{Env: env, Bot: bot, Account: DeribitAccount(root), EnvsDir: filepath.Dir(root)}
	rel := func(path string) string {
		if p, err := filepath.Rel(root, path); err == nil {
			return p
		}
		return path
	}
	_, trackJS := TrackOutPaths(root, bot)
	_, trendJS := TrendOutPaths(root, bot)
	in.TrackPath, in.TrendPath = rel(trackJS), rel(trendJS)
	var bad bool
	if in.Track, bad = readPackFile[TrackRecord](trackJS); bad {
		in.Unreadable = append(in.Unreadable, in.TrackPath)
	}
	if in.Trend, bad = readPackFile[Trend](trendJS); bad {
		in.Unreadable = append(in.Unreadable, in.TrendPath)
	}
	reports, unreadable, err := ReadWeekly(root, bot)
	if err != nil {
		fmt.Fprintf(notes, "%s %s: weekly reports not read: %v\n", env, bot, err)
	}
	for _, path := range unreadable {
		in.Unreadable = append(in.Unreadable, rel(path))
	}
	in.LatestPath = rel(WeeklyGlob(root, bot))
	if ws := byWeek(reports); len(ws) > 0 {
		last := ws[len(ws)-1]
		in.Latest = &last
		_, js := OutPaths(root, last.Week, bot)
		in.LatestPath = rel(js)
	}
	for _, path := range in.Unreadable {
		fmt.Fprintf(notes, "%s %s: not read: %s\n", env, bot, path)
	}
	return in
}

// readPackFile reads one JSON report: nil when absent, bad when unreadable.
func readPackFile[T any](path string) (v *T, bad bool) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false
	}
	v = new(T)
	if err == nil {
		err = json.Unmarshal(data, v)
	}
	if err != nil {
		return nil, true
	}
	return v, false
}

// WritePack writes the pack as Markdown and JSON.
func WritePack(p Pack, md, js string) error {
	return writeBoth(md, js, p, func(w io.Writer) error { return WritePackMarkdown(w, p) })
}
