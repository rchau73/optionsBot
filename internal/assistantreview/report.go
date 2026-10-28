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
var GuardReasons = []string{"no_citation", "unknown_citation", "ungrounded_number", "advice", "forecast"}

// GapCoverage is the share of a question's weight a passage must hold to be
// "close" to it: half of what the assistant needs to answer from it
// (helpkb.DefaultNoAnswer). Under it the question is "no passage close" — a
// new section or glossary term, not a better passage.
const GapCoverage = helpkb.DefaultNoAnswer / 2

// NoPassage names the gap group of questions no passage is close to.
const NoPassage = "no passage close"

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
	// Review lists every flagged and every dont_know answer of the week, and
	// every answer of an earlier week flagged during this one (Late).
	Review []ReviewItem `json:"review"`
	// Gaps groups the review's dont_know and flagged questions by the help
	// passage nearest each; the NoPassage group last.
	Gaps []Gap `json:"gaps"`
}

// KindCount is one answer kind: how many, how many flagged.
type KindCount struct {
	Kind    string   `json:"kind"`
	Answers int      `json:"answers"`
	Flagged int      `json:"flagged"`
	FlagPct *float64 `json:"flag_pct"`
}

// ReasonCount is one guard reason and the answers that hit it.
type ReasonCount struct {
	Reason  string `json:"reason"`
	Answers int    `json:"answers"`
}

// ReviewItem is one answer a person must look at.
type ReviewItem struct {
	ID        string    `json:"id"`
	Time      time.Time `json:"time"`
	Kind      string    `json:"kind"`
	Bot       string    `json:"bot,omitempty"`
	Flagged   bool      `json:"flagged"`
	Late      bool      `json:"late,omitempty"` // asked before the week, flagged in it
	Question  string    `json:"question"`
	Answer    string    `json:"answer"`
	Sources   []string  `json:"sources"`
	Citations []string  `json:"citations"`
	Verdict   []string  `json:"verdict,omitempty"`
	Notes     []string  `json:"flag_notes,omitempty"`
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
// too (Late), so no flag waits for a week that was already reported. index
// may be nil: every gap question is then NoPassage.
func Build(lg Log, from, to time.Time, label string, index *helpkb.Index) Report {
	r := Report{Week: label, From: from, To: to, Skipped: lg.Skipped}
	in := func(t time.Time) bool { return !t.Before(from) && t.Before(to) }

	answers := map[string]Entry{}
	var order []string
	notes := map[string][]string{}
	flaggedIn := map[string]bool{} // flagged by a flag line placed in the week
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
		r.ByOutcome = append(r.ByOutcome, *c)
	}
	for _, reason := range GuardReasons {
		r.Reasons = append(r.Reasons, ReasonCount{Reason: reason, Answers: reasons[reason]})
	}
	r.FlagPct = pct(r.Flagged, r.Answers)
	r.AnsweredPct = pct(kinds[OutcomeAnswered].Answers, r.Answers)
	sort.SliceStable(r.Review, func(i, j int) bool { return r.Review[i].Time.Before(r.Review[j].Time) })
	r.Gaps = gaps(r.Review, index)
	return r
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
		if strings.TrimSpace(it.Question) == "" {
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
