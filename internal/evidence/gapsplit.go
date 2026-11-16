package evidence

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"optionsbot/internal/orders"
	"optionsbot/internal/recorder"
	"optionsbot/internal/replay"
)

// The gap split (#802): the Gap section's replay − live, over the replay's
// window, cut into named causes from what the report already reads — the
// journal's fills and fees for live, the replay's own fills on the recorded
// books (replay.Result.Fills), no second replay. Every part is in the Gap's
// sign (a positive part made the replay better than live):
//
//   - fill price: legs both sides filled (same instrument and kind, within
//     FillMatchWindow): the replay's price against live's, × the quantity both
//     filled — a sale higher, or a buy-back lower, than live adds to the gap;
//   - fees: live's fees in its result (the journal's close fees) − the
//     replay's (every fill's);
//   - trades one side made: what is left of a leg once matched, valued from its
//     fill to the window's last recorded mid of its instrument, each listed
//     with the journal's reason (live) or the replay's rule;
//   - marks: each side's open book at the window's two ends as that side
//     values it (live: the recorded mid, its premium without a quote; the
//     replay: its own last mark) against the recorded mids;
//   - unexplained: the gap minus those four, never hidden.
//
// With every fill matched the four parts add to the gap exactly; whatever
// the journal and the replay book differently (a close's premium share, the
// perpetual hedge, an expiry) shows as unexplained. The exchange's marks are
// a line outside the sum: the Gap's live side is marked at the recorded mids.

// GapSplitLabel names the section.
const GapSplitLabel = "gap split"

// FillMatchWindow is how far apart a live and a replay fill of the same leg
// may be and still count as the same trade: one recorded-book interval (the
// replay decides on hourly books between GEX minutes).
const FillMatchWindow = time.Hour

// GapExplainedPct is the proposed bar (an ASSUMPTION a person confirms,
// docs/agents/evidence.md "Gap split"): a week's gap is explained when the
// unexplained line is within this % of the gap.
const GapExplainedPct = 20.0

// LegsName names the gap split's sample events.
const LegsName = "legs"

// The parts, in the order printed.
const (
	PartFillPrice   = "fill price"
	PartFees        = "fees"
	PartOneSided    = "trades one side made"
	PartMarks       = "marks"
	PartUnexplained = "unexplained"
)

// GapSplit is the section. Amounts (coin) are nil for a live account.
type GapSplit struct {
	Label        string         `json:"label"`
	Known        bool           `json:"known"`
	Note         string         `json:"note,omitempty"`    // why there are no figures
	Partial      string         `json:"partial,omitempty"` // as Gap.Partial
	Account      string         `json:"account"`
	AmountsShown bool           `json:"amounts_shown"`
	Source       string         `json:"source"`
	Gap          *float64       `json:"gap,omitempty"` // coin, = Gap.Gap
	GapEquityPP  float64        `json:"gap_equity_pp"`
	Parts        []GapPart      `json:"parts"` // PartFillPrice … PartUnexplained
	OneSided     []OneSidedLeg  `json:"one_sided"`
	Exchange     *ExchangeMarks `json:"exchange_marks,omitempty"` // outside the sum
	// Explained is |unexplained| ≤ GapExplainedPct % of |gap|.
	Explained    bool    `json:"explained"`
	ExplainedPct float64 `json:"explained_bar_pct"`
}

// GapPart is one line of the split.
type GapPart struct {
	Part     string   `json:"part"`
	Coin     *float64 `json:"coin,omitempty"`
	EquityPP float64  `json:"equity_pp"`         // of the starting equity
	GapPct   *float64 `json:"gap_pct,omitempty"` // of the gap (signed, the parts add to 100); nil when the gap is 0
	Legs     int      `json:"legs"`              // the legs behind it
	Sample   Sample   `json:"sample"`            // the replay's window and those legs
	Note     string   `json:"note,omitempty"`    // what it counts
}

// OneSidedLeg is a leg (or what is left of one) only one side filled.
type OneSidedLeg struct {
	Side       string    `json:"side"` // live | replay
	At         time.Time `json:"at"`
	Instrument string    `json:"instrument"`
	Kind       string    `json:"kind"` // open | close
	Qty        *float64  `json:"qty,omitempty"`
	Price      *float64  `json:"price,omitempty"`
	Coin       *float64  `json:"coin,omitempty"` // its share of the gap
	EquityPP   float64   `json:"equity_pp"`
	Reason     string    `json:"reason"` // the journal's reason, or the replay's rule
}

// ExchangeMarks compares live's open book at the window's end at the
// recorded mids (the Gap's figure) with the exchange's marks (the P&L
// history's unrealised at its last point in the window).
type ExchangeMarks struct {
	At            time.Time `json:"at"` // the history point
	RecordedMids  *float64  `json:"recorded_mids,omitempty"`
	ExchangeMarks *float64  `json:"exchange_marks,omitempty"`
	DiffCoin      *float64  `json:"diff,omitempty"` // exchange − recorded
	DiffEquityPP  float64   `json:"diff_equity_pp"`
	Legs          int       `json:"legs"`
	Sample        Sample    `json:"sample"`
}

// splitFill is one fill of either side, with what is left unmatched.
type splitFill struct {
	at         time.Time
	inst, kind string
	qty, price float64
	left       float64
	why        string
}

// buildGapSplit is the section, from the report's Gap and the replay's
// result (nil when it did not run). Pure.
func buildGapSplit(in Inputs, r Report, res *replay.Result) GapSplit {
	s := GapSplit{Label: GapSplitLabel, Account: accountOf(in.Account), Parts: []GapPart{}, OneSided: []OneSidedLeg{},
		ExplainedPct: GapExplainedPct,
		Source:       in.Journal + " (fills and fees) and the replay's fills on " + in.Market + ", marked at the recorded mids"}
	s.AmountsShown = s.Account != AccountLive
	if !r.Gap.Known || res == nil {
		note := r.Replay.Note
		if note == "" {
			note = "no gap"
		}
		s.Note = "the replay did not run: " + note
		return s
	}
	window := snapsIn(in.Snaps, res.Start, res.End)
	if len(window) == 0 {
		s.Note = "no recorded book in the replay's window"
		return s
	}
	s.Known, s.Partial = true, r.Gap.Partial
	first, last := window[0], window[len(window)-1]
	eq := in.Equity // > 0: the replay ran
	gap := r.Gap.Gap

	live := liveFills(in.Trades, first.At, last.At)
	rep := make([]*splitFill, 0, len(res.Fills))
	repFees := 0.0
	for _, f := range res.Fills {
		rep = append(rep, &splitFill{at: f.At, inst: f.Instrument, kind: f.Kind, qty: f.Qty, price: f.Price, left: f.Qty, why: f.Why})
		repFees += f.Fee
	}
	liveFees := 0.0
	for _, t := range in.Trades {
		if t.Kind == "close" && !t.At.Before(first.At) && !t.At.After(last.At) {
			liveFees += t.Fees
		}
	}

	fill, matched := matchFills(live, rep)
	fees := liveFees - repFees

	// The reference mids: each instrument's at the window's last book (else
	// the latest book quoting it, else 0) and at its first.
	insts := map[string]bool{}
	for _, f := range append(append([]*splitFill{}, live...), rep...) {
		insts[f.inst] = true
	}
	liveStart, liveEnd := replay.BookAt(in.Trades, first.At), replay.BookAt(in.Trades, last.At)
	for _, l := range append(append([]replay.StartLeg{}, liveStart...), liveEnd...) {
		insts[l.Instrument] = true
	}
	for _, l := range append(append([]replay.BookLeg{}, res.StartBook...), res.EndBook...) {
		insts[l.Instrument] = true
	}
	m0, m1 := refMids(window, insts, false), refMids(window, insts, true)

	oneSided, nOne := 0.0, 0
	add := func(side string, f *splitFill, sign float64) {
		if f.left <= 1e-9 {
			return
		}
		dir := 1.0 // a sale: premium in, the leg's value at the end out
		if f.kind == "close" {
			dir = -1
		}
		v := sign * dir * (f.price - m1[f.inst]) * f.left
		oneSided += v
		nOne++
		leg := OneSidedLeg{Side: side, At: f.at, Instrument: f.inst, Kind: f.kind, EquityPP: 100 * v / eq, Reason: f.why}
		if s.AmountsShown {
			leg.Qty, leg.Price, leg.Coin = ptr(f.left), ptr(f.price), ptr(v)
		}
		s.OneSided = append(s.OneSided, leg)
	}
	for _, f := range rep {
		add("replay", f, 1)
	}
	for _, f := range live {
		add("live", f, -1)
	}
	sort.SliceStable(s.OneSided, func(i, j int) bool {
		return math.Abs(s.OneSided[i].EquityPP) > math.Abs(s.OneSided[j].EquityPP)
	})

	// Marks: Net = flows − fees + V(start) − V(end), V = the shorts' value.
	q0, q1 := first.ByName(), last.ByName()
	liveValue := func(legs []replay.StartLeg, quotes map[string]recorder.Quote, ref map[string]float64) float64 {
		d := 0.0
		for _, l := range legs {
			mid := l.Price // ActualPnL: no quote → its premium
			if q, ok := quotes[l.Instrument]; ok {
				mid = q.LiveMid()
			}
			d += (mid - ref[l.Instrument]) * l.Qty
		}
		return d
	}
	repValue := func(legs []replay.BookLeg, ref map[string]float64) float64 {
		d := 0.0
		for _, l := range legs {
			d += (l.Mid - ref[l.Instrument]) * l.Qty
		}
		return d
	}
	marks := (repValue(res.StartBook, m0) - repValue(res.EndBook, m1)) -
		(liveValue(liveStart, q0, m0) - liveValue(liveEnd, q1, m1))
	markLegs := len(res.StartBook) + len(res.EndBook) + len(liveStart) + len(liveEnd)

	unexplained := gap - fill - fees - oneSided - marks
	days := int(math.Round(res.End.Sub(res.Start).Hours() / 24))
	part := func(name string, v float64, legs int, note string) GapPart {
		p := GapPart{Part: name, EquityPP: 100 * v / eq, Legs: legs, Sample: SpanSample(days, intPtr(legs), LegsName), Note: note}
		if s.AmountsShown {
			p.Coin = ptr(v)
		}
		if gap != 0 {
			p.GapPct = ptr(100 * v / gap)
		}
		return p
	}
	allLegs := len(live) + len(rep)
	s.Parts = []GapPart{
		part(PartFillPrice, fill, matched, "legs both sides filled within FillMatchWindow: the replay's price against live's"),
		part(PartFees, fees, len(live)+len(rep), "live's close fees − the replay's fees"),
		part(PartOneSided, oneSided, nOne, "legs one side filled, to the window's last recorded mid"),
		part(PartMarks, marks, markLegs, "open books at the window's ends: each side's own marks against the recorded mids"),
		part(PartUnexplained, unexplained, allLegs, "the gap minus the four parts above"),
	}
	s.GapEquityPP = 100 * gap / eq
	if s.AmountsShown {
		s.Gap = ptr(gap)
	}
	s.Explained = math.Abs(unexplained) <= GapExplainedPct/100*math.Abs(gap)+1e-12
	s.Exchange = exchangeMarks(in, r, res, eq, days, len(liveEnd), s.AmountsShown)
	return s
}

// liveFills are the journal's filled legs in [from, to] (ActualPnL's window).
func liveFills(trades []orders.Trade, from, to time.Time) []*splitFill {
	var out []*splitFill
	for _, t := range trades {
		if t.Qty <= 0 || (t.Kind != "open" && t.Kind != "close") || t.At.Before(from) || t.At.After(to) {
			continue
		}
		why := t.Reason
		if why == "" {
			why = t.Kind
		}
		out = append(out, &splitFill{at: t.At, inst: t.Instrument, kind: t.Kind, qty: t.Qty, price: t.Price, left: t.Qty, why: why})
	}
	return out
}

// matchFills pairs each live fill with the replay's fills of the same
// instrument and kind within FillMatchWindow, nearest first, and returns
// the fill-price part and the pairs made. Matched quantities leave `left`.
func matchFills(live, rep []*splitFill) (part float64, pairs int) {
	for _, l := range live {
		for l.left > 1e-9 {
			var best *splitFill
			for _, f := range rep {
				d := absDur(f.at.Sub(l.at))
				if f.inst != l.inst || f.kind != l.kind || f.left <= 1e-9 || d > FillMatchWindow {
					continue
				}
				if best == nil || d < absDur(best.at.Sub(l.at)) {
					best = f
				}
			}
			if best == nil {
				break
			}
			q := math.Min(l.left, best.left)
			diff := best.price - l.price // a sale: the replay received more
			if l.kind == "close" {
				diff = -diff // a buy-back: the replay paid less
			}
			part += diff * q
			l.left -= q
			best.left -= q
			pairs++
		}
	}
	return part, pairs
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// refMids is each instrument's recorded mid at the window's last book
// (fromEnd) or its first, else at the nearest book inside the window that
// quotes it, else 0.
func refMids(window []recorder.Snapshot, insts map[string]bool, fromEnd bool) map[string]float64 {
	out := map[string]float64{}
	for i := range window {
		snap := window[i]
		if fromEnd {
			snap = window[len(window)-1-i]
		}
		quotes := snap.ByName()
		for inst := range insts {
			if _, done := out[inst]; done {
				continue
			}
			if q, ok := quotes[inst]; ok {
				out[inst] = q.LiveMid()
			}
		}
		if len(out) == len(insts) {
			break
		}
	}
	return out
}

// exchangeMarks is live's open book at the window's end at the recorded mids
// against the exchange's marks; nil without a history point in the window.
func exchangeMarks(in Inputs, r Report, res *replay.Result, eq float64, days, legs int, amounts bool) *ExchangeMarks {
	for i := len(in.Points) - 1; i >= 0; i-- {
		p := in.Points[i]
		if p.T.Before(res.Start) || p.T.After(res.End) {
			continue
		}
		d := p.Unrealised - r.Gap.Live.Unrealised1
		x := &ExchangeMarks{At: p.T, DiffEquityPP: 100 * d / eq, Legs: legs, Sample: SpanSample(days, intPtr(legs), LegsName)}
		if amounts {
			x.RecordedMids, x.ExchangeMarks, x.DiffCoin = ptr(r.Gap.Live.Unrealised1), ptr(p.Unrealised), ptr(d)
		}
		return x
	}
	return nil
}

// gapSplitRows is how many one-sided legs the Markdown lists (the JSON has all).
const gapSplitRows = 20

// writeGapSplit writes the section.
func writeGapSplit(p func(string, ...any), s GapSplit, coin string) {
	p("## Gap split (replay − live)\n\n")
	if s.Partial != "" {
		p("**%s** (see Recorded-history coverage).\n\n", s.Partial)
	}
	p("Method: the Gap section's replay − live cut into its causes, each in the gap's sign (positive = the replay did better). "+
		"Fill price: legs both sides filled (same instrument and kind within %.0f h), the replay's price against live's. Fees: live's close fees − the replay's. "+
		"Trades one side made: legs only one side filled, valued to the window's last recorded mid. Marks: the open books at the window's ends, each side's own marks against the recorded mids. "+
		"Unexplained: the gap minus the four, always shown. The gap counts as explained when the unexplained line is within %.0f %% of it (a proposed bar). "+
		"Every line names the legs behind it. A live Deribit account shows percentages only. Source: %s.\n\n",
		FillMatchWindow.Hours(), s.ExplainedPct, s.Source)
	if !s.Known {
		p("- **Not available**: %s. No figures.\n\n", s.Note)
		return
	}
	if s.AmountsShown {
		p("| Part | %s | pp of equity | %% of the gap | Sample |\n|---|---|---|---|---|\n", coin)
	} else {
		p("Live account: percentages only.\n\n| Part | pp of equity | %% of the gap | Sample |\n|---|---|---|---|\n")
	}
	for _, pt := range s.Parts {
		share := "the gap is 0"
		if pt.GapPct != nil {
			share = fmt.Sprintf("%+.1f %%", *pt.GapPct)
		}
		name := pt.Part
		if pt.Part == PartUnexplained {
			name = "**unexplained**"
		}
		if s.AmountsShown {
			p("| %s | %+.4f | %+.2f | %s | %s |\n", name, *pt.Coin, pt.EquityPP, share, pt.Sample.Text())
		} else {
			p("| %s | %+.2f | %s | %s |\n", name, pt.EquityPP, share, pt.Sample.Text())
		}
	}
	if s.AmountsShown {
		p("| gap | %+.4f | %+.2f | 100 %% | |\n\n", *s.Gap, s.GapEquityPP)
	} else {
		p("| gap | %+.2f | 100 %% | |\n\n", s.GapEquityPP)
	}
	if s.Explained {
		p("- explained: the unexplained line is within %.0f %% of the gap\n", s.ExplainedPct)
	} else {
		p("- **not explained**: the unexplained line is over %.0f %% of the gap\n", s.ExplainedPct)
	}
	if len(s.OneSided) == 0 {
		p("- trades one side made: none\n")
	} else {
		p("- trades one side made (%d, largest first):\n", len(s.OneSided))
		for i, l := range s.OneSided {
			if i == gapSplitRows {
				p("  - … and %d more (the JSON lists all)\n", len(s.OneSided)-gapSplitRows)
				break
			}
			p("  - %s only, %s UTC: %s %s", l.Side, l.At.UTC().Format("2006-01-02 15:04"), l.Kind, l.Instrument)
			if l.Qty != nil {
				p(" ×%.4g at %.4f (%+.4f %s)", *l.Qty, *l.Price, *l.Coin, coin)
			}
			p(" · %+.2f pp · reason: %s\n", l.EquityPP, strings.ReplaceAll(l.Reason, "\n", " "))
		}
	}
	if x := s.Exchange; x != nil {
		p("- outside the sum: live's open book at the exchange's marks (P&L history, %s UTC) against the recorded mids:", x.At.UTC().Format("2006-01-02 15:04"))
		if x.DiffCoin != nil {
			p(" %+.4f vs %+.4f, %+.4f %s ·", *x.ExchangeMarks, *x.RecordedMids, *x.DiffCoin, coin)
		}
		p(" %+.2f pp of equity · %s\n", x.DiffEquityPP, x.Sample.Text())
	} else {
		p("- outside the sum: the exchange's marks: no P&L history point in the replay's window\n")
	}
	p("\n")
}
