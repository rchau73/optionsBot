package assistantreview

import (
	"sort"
	"strings"
	"time"

	"optionsbot/internal/helpkb"
)

// Outcomes an answer line records (frontend/lib/assistant/service.js).
// Anything else is counted as OutcomeOther.
const (
	OutcomeAnswered   = "answered"
	OutcomeDontKnow   = "dont_know"
	OutcomeRefused    = "refused"
	OutcomeNoCitation = "no_citation" // the guard found no citation: "I don't know" shown
	OutcomeRejected   = "rejected"    // the guard rejected the answer: the fallback reply shown
	OutcomeModelError = "model_error"
	OutcomeNeedsInfo  = "needs_info"
	OutcomeDraft      = "draft"
	OutcomeOther      = "other"
)

// Outcomes is the report's order of the answer kinds.
var Outcomes = []string{OutcomeAnswered, OutcomeDontKnow, OutcomeRefused, OutcomeNoCitation,
	OutcomeRejected, OutcomeModelError, OutcomeNeedsInfo, OutcomeDraft, OutcomeOther}

// GuardReasons are the answer guard's rejection reasons
// (internal/assistant/guard); a verdict line reads "<reason>: <detail>".
var GuardReasons = []string{"no_citation", "unknown_citation", "ungrounded_number", "advice", "forecast", "points_to_document", "language"}

// GapCoverage is the share of a question's weight a passage must hold to be
// "close" to it: half of what the assistant needs to answer from it
// (helpkb.DefaultNoAnswer). Under it the question is "no passage close" — a
// new section or glossary term, not a better passage.
const GapCoverage = helpkb.DefaultNoAnswer / 2

// NoPassage names the gap group of questions no passage is close to.
const NoPassage = "no passage close"

// MinResponses is how many "Helped"/"Didn't help" answers a helped share
// needs (#579): under it the share is null and the report says "too few
// responses" — a share of three opinions says nothing.
const MinResponses = 10

// Report is one week of the assistant's answers. Private: it holds the
// questions and answers.
type Report struct {
	Week    string    `json:"week"`
	From    time.Time `json:"from"`
	To      time.Time `json:"to"` // exclusive
	Skipped int       `json:"skipped_lines"`
	Answers int       `json:"answers"`
	// ByOutcome counts the week's answers by kind, every kind of Outcomes listed.
	ByOutcome []KindCount `json:"by_outcome"`
	// Reasons counts the answers whose verdict hit each guard reason (an
	// answer counts once per reason).
	Reasons     []ReasonCount `json:"guard_reasons"`
	Flagged     int           `json:"flagged"`
	FlagPct     *float64      `json:"flag_pct"`     // flagged ÷ answers; null without answers
	AnsweredPct *float64      `json:"answered_pct"` // answered ÷ answers
	// Feedback is the week's "Helped"/"Didn't help" answers (#579): the
	// person's opinion, never a measure of correctness.
	Feedback
	// Review lists every flagged and every dont_know answer of the week, every
	// answer of an earlier week flagged during this one (Late) and the rows of
	// earlier weeks not closed before this one (Carried). Overdue rows first.
	Review []ReviewItem `json:"review"`
	// Gaps groups the review's dont_know and flagged questions by the help
	// passage nearest each; the NoPassage group last. Carried rows are left out:
	// they were grouped in their own week.
	Gaps []Gap `json:"gaps"`
	// ClosuresSkipped counts the closure lines that could not be used.
	ClosuresSkipped int `json:"closures_skipped_lines"`
	// goldenDone holds the questions (questionKey) of the answers closed as
	// golden_case, which Candidates never offers again.
	goldenDone map[string]bool
}

// KindCount is one answer kind: how many, how many flagged, and its feedback.
type KindCount struct {
	Kind    string   `json:"kind"`
	Answers int      `json:"answers"`
	Flagged int      `json:"flagged"`
	FlagPct *float64 `json:"flag_pct"`
	Feedback
}

// Feedback counts answers by the last "Helped"/"Didn't help" choice of the
// person who asked (#579; the choice can be changed once, the last line wins).
type Feedback struct {
	Helped     int `json:"helped"`
	NotHelped  int `json:"not_helped"`
	NoFeedback int `json:"no_feedback"`
	// HelpedPct = helped ÷ (helped + not helped); null under MinResponses responses.
	HelpedPct *float64 `json:"helped_pct"`
	// ResponsePct = (helped + not helped) ÷ answers; null without answers.
	ResponsePct *float64 `json:"response_pct"`
}

// Responses is how many answers got a "Helped" or a "Didn't help".
func (f Feedback) Responses() int { return f.Helped + f.NotHelped }

// count adds one answer with its choice (nil: none).
func (f *Feedback) count(helped *bool) {
	switch {
	case helped == nil:
		f.NoFeedback++
	case *helped:
		f.Helped++
	default:
		f.NotHelped++
	}
}

// shares fills the two percentages from the counts.
func (f *Feedback) shares() {
	if f.Responses() >= MinResponses {
		f.HelpedPct = pct(f.Helped, f.Responses())
	}
	f.ResponsePct = pct(f.Responses(), f.Responses()+f.NoFeedback)
}

// ReasonCount is one guard reason and the answers that hit it.
type ReasonCount struct {
	Reason  string `json:"reason"`
	Answers int    `json:"answers"`
}

// ReviewItem is one answer a person must look at.
type ReviewItem struct {
	ID      string    `json:"id"`
	Time    time.Time `json:"time"`
	Kind    string    `json:"kind"`
	Bot     string    `json:"bot,omitempty"`
	Flagged bool      `json:"flagged"`
	Late    bool      `json:"late,omitempty"`    // asked before the week, flagged in it
	Carried bool      `json:"carried,omitempty"` // on an earlier week's list, not closed before this week
	// Closure is how the row was closed; OpenDays, on a row with none, is the
	// whole days since the first flag, or since the answer for an unflagged
	// dont_know. Overdue = OpenDays over OverdueDays.
	Closure   *Closure `json:"closure,omitempty"`
	OpenDays  *int     `json:"open_days,omitempty"`
	Overdue   bool     `json:"overdue,omitempty"`
	Question  string   `json:"question"`
	Answer    string   `json:"answer"`
	Sources   []string `json:"sources"`
	Citations []string `json:"citations"`
	Verdict   []string `json:"verdict,omitempty"`
	Notes     []string `json:"flag_notes,omitempty"`
}

// Gap is the questions nearest one help passage.
type Gap struct {
	Source    string        `json:"source"` // the passage's citation, or NoPassage
	Title     string        `json:"title,omitempty"`
	Questions []GapQuestion `json:"questions"`
}

// GapQuestion is one question of a gap group.
type GapQuestion struct {
	ID       string  `json:"id"`
	Kind     string  `json:"kind"`
	Flagged  bool    `json:"flagged"`
	Question string  `json:"question"`
	Coverage float64 `json:"coverage"` // share of the question's weight in the passage
}

// Build reports the answers asked in [from, to). Flags are read from the
// whole log, so an answer of the week flagged later counts as flagged; a flag
// placed in the week on an earlier answer puts that answer in the review list
// too (Late), so no flag waits for a week that was already reported. Rows of
// earlier weeks stay on the list until the week after their closure (Carried),
// so a closure shows once and an open row is never lost. now only dates the open
// rows: pure, the caller passes it. index may be nil: every gap question is then
// NoPassage.
func Build(lg Log, closures Closures, from, to, now time.Time, label string, index *helpkb.Index) Report {
	r := Report{Week: label, From: from, To: to, Skipped: lg.Skipped, ClosuresSkipped: closures.Skipped, goldenDone: map[string]bool{}}
	in := func(t time.Time) bool { return !t.Before(from) && t.Before(to) }

	answers := map[string]Entry{}
	var order []string
	notes := map[string][]string{}
	flaggedIn := map[string]bool{}      // flagged by a flag line placed in the week
	firstFlag := map[string]time.Time{} // the earliest flag per answer, from the whole log
	helped := map[string]*bool{}        // the last feedback choice per answer, from the whole log
	for _, e := range lg.Entries {
		switch e.Kind {
		case KindAnswer:
			if _, seen := answers[e.ID]; !seen {
				order = append(order, e.ID)
			}
			answers[e.ID] = e
		case KindFlag:
			if note := strings.TrimSpace(e.Note); note != "" {
				notes[e.ID] = append(notes[e.ID], note)
			} else if _, ok := notes[e.ID]; !ok {
				notes[e.ID] = nil
			}
			if in(e.TS) {
				flaggedIn[e.ID] = true
			}
			if t, ok := firstFlag[e.ID]; !ok || e.TS.Before(t) {
				firstFlag[e.ID] = e.TS
			}
		case KindFeedback:
			if e.Helped != nil {
				helped[e.ID] = e.Helped
			}
		}
	}

	kinds := map[string]*KindCount{}
	for _, k := range Outcomes {
		kinds[k] = &KindCount{Kind: k}
	}
	reasons := map[string]int{}
	for _, id := range order {
		a := answers[id]
		_, flagged := notes[id]
		if !in(a.TS) {
			if flagged && flaggedIn[id] && a.TS.Before(from) {
				r.Review = append(r.Review, reviewItem(a, true, notes[id], true))
			} else if carries(a, firstFlag, closures, from) {
				it := reviewItem(a, flagged, notes[id], false)
				it.Carried = true
				r.Review = append(r.Review, it)
			}
			continue
		}
		kind := kindOf(a.Outcome)
		r.Answers++
		kinds[kind].Answers++
		if flagged {
			r.Flagged++
			kinds[kind].Flagged++
		}
		r.Feedback.count(helped[id])
		kinds[kind].Feedback.count(helped[id])
		for _, reason := range verdictReasons(a.Verdict) {
			reasons[reason]++
		}
		if flagged || kind == OutcomeDontKnow {
			r.Review = append(r.Review, reviewItem(a, flagged, notes[id], false))
		}
	}

	for _, k := range Outcomes {
		c := kinds[k]
		c.FlagPct = pct(c.Flagged, c.Answers)
		c.Feedback.shares()
		r.ByOutcome = append(r.ByOutcome, *c)
	}
	for _, reason := range GuardReasons {
		r.Reasons = append(r.Reasons, ReasonCount{Reason: reason, Answers: reasons[reason]})
	}
	r.FlagPct = pct(r.Flagged, r.Answers)
	r.Feedback.shares()
	r.AnsweredPct = pct(kinds[OutcomeAnswered].Answers, r.Answers)
	for i := range r.Review {
		r.Review[i].track(closures, firstFlag, now)
	}
	sort.SliceStable(r.Review, func(i, j int) bool {
		a, b := r.Review[i], r.Review[j]
		if a.Overdue != b.Overdue {
			return a.Overdue
		}
		return a.Time.Before(b.Time)
	})
	r.Gaps = gaps(r.Review, index)
	for id, c := range closures.ByID {
		if a, ok := answers[id]; ok && c.How == HowGoldenCase {
			r.goldenDone[questionKey(a.Question)] = true
		}
	}
	return r
}

// carries says whether an answer asked before the week stays on the list: it was
// on an earlier list (a dont_know, or flagged before the week) and was not closed
// before the week. One closed since then stays once, to show how.
func carries(a Entry, firstFlag map[string]time.Time, closures Closures, from time.Time) bool {
	flaggedAt, flagged := firstFlag[a.ID]
	listed := kindOf(a.Outcome) == OutcomeDontKnow || (flagged && flaggedAt.Before(from))
	if !a.TS.Before(from) || !listed {
		return false
	}
	c, closed := closures.ByID[a.ID]
	return !closed || !c.ClosedAt.Before(from)
}

// track fills the row's closure, or the days it has been open.
func (it *ReviewItem) track(closures Closures, firstFlag map[string]time.Time, now time.Time) {
	if c, closed := closures.ByID[it.ID]; closed {
		it.Closure = &c
		return
	}
	since := it.Time
	if t, flagged := firstFlag[it.ID]; flagged {
		since = t
	}
	days := max(0, int(now.Sub(since)/(24*time.Hour)))
	it.OpenDays = &days
	it.Overdue = days > OverdueDays
}

func reviewItem(a Entry, flagged bool, notes []string, late bool) ReviewItem {
	return ReviewItem{
		ID: a.ID, Time: a.TS, Kind: kindOf(a.Outcome), Bot: a.Bot, Flagged: flagged, Late: late,
		Question: a.Question, Answer: a.Answer, Sources: a.Sources, Citations: a.Citations,
		Verdict: a.Verdict, Notes: notes,
	}
}

// kindOf is the outcome as one of Outcomes.
func kindOf(outcome string) string {
	for _, k := range Outcomes {
		if outcome == k && k != OutcomeOther {
			return k
		}
	}
	return OutcomeOther
}

// verdictReasons are the guard reasons a verdict names, each once.
func verdictReasons(verdict []string) []string {
	var out []string
	for _, v := range verdict {
		reason, _, _ := strings.Cut(v, ":")
		reason = strings.TrimSpace(reason)
		for _, g := range GuardReasons {
			if reason == g && !contains(out, g) {
				out = append(out, g)
			}
		}
	}
	return out
}

// gaps groups the review's questions under the passage nearest each (the
// best helpkb.Search hit holding at least GapCoverage of the question), in
// order of group size, NoPassage last.
func gaps(items []ReviewItem, index *helpkb.Index) []Gap {
	groups := map[string]*Gap{}
	var keys []string
	for _, it := range items {
		if it.Carried || strings.TrimSpace(it.Question) == "" {
			continue
		}
		key, title, coverage := NoPassage, "", 0.0
		if index != nil {
			if hits := index.Search(it.Question, 1); len(hits) > 0 {
				coverage = hits[0].Coverage
				if coverage >= GapCoverage {
					key, title = hits[0].Source, hits[0].Title
				}
			}
		}
		g, ok := groups[key]
		if !ok {
			g = &Gap{Source: key, Title: title}
			groups[key] = g
			keys = append(keys, key)
		}
		g.Questions = append(g.Questions, GapQuestion{ID: it.ID, Kind: it.Kind, Flagged: it.Flagged, Question: it.Question, Coverage: coverage})
	}
	sort.SliceStable(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if (a == NoPassage) != (b == NoPassage) {
			return b == NoPassage
		}
		return len(groups[a].Questions) > len(groups[b].Questions)
	})
	out := make([]Gap, 0, len(keys))
	for _, k := range keys {
		out = append(out, *groups[k])
	}
	return out
}

func pct(n, of int) *float64 {
	if of == 0 {
		return nil
	}
	v := 100 * float64(n) / float64(of)
	return &v
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
