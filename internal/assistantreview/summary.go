package assistantreview

// Shareable is what may leave the private report — the weekly comment on the
// E10 epic (scripts/agents/usage_week.py): counts and percentages only. Its
// only strings are answer kinds and guard reasons from the fixed lists
// (Outcomes, GuardReasons); no question, answer, user name, id or amount.
// tests/assistantreview_test.go fails on any other field.
type Shareable struct {
	Answers     int      `json:"answers"`
	Skipped     int      `json:"skipped_lines"`
	Flagged     int      `json:"flagged"`
	FlagPct     *float64 `json:"flag_pct"`
	AnsweredPct *float64 `json:"answered_pct"`
	// Feedback is the week's "Helped"/"Didn't help" counts and shares (#579):
	// the person's opinion, not a measure of correctness.
	Feedback
	// ByOutcome and Reasons are the report's, every kind and reason listed.
	ByOutcome []KindCount   `json:"by_outcome"`
	Reasons   []ReasonCount `json:"guard_reasons"`
	// ReviewItems is how many answers a person must look at; LateFlags those
	// of them asked in an earlier week.
	ReviewItems int `json:"review_items"`
	LateFlags   int `json:"late_flags"`
	// GapGroups is how many passages the gap questions were grouped under;
	// NoPassageClose how many questions no passage is close to.
	GapGroups      int `json:"gap_groups"`
	NoPassageClose int `json:"no_passage_close"`
	// FlaggedTotal is the rows of the review list, flagged and dont_know, late
	// and carried (#704) = Closed + Open; Overdue those of Open past OverdueDays.
	FlaggedTotal int         `json:"flagged_total"`
	Closed       int         `json:"closed"`
	Open         int         `json:"open"`
	Overdue      int         `json:"overdue"`
	ClosedByHow  ClosedByHow `json:"closed_by_how"`
}

// ClosedByHow counts the closed rows by each of Hows.
type ClosedByHow struct {
	HelpFix        int `json:"help_fix"`
	GoldenCase     int `json:"golden_case"`
	AnswerWasRight int `json:"answer_was_right"`
}

func (c *ClosedByHow) count(how string) {
	switch how {
	case HowHelpFix:
		c.HelpFix++
	case HowGoldenCase:
		c.GoldenCase++
	case HowAnswerWasRight:
		c.AnswerWasRight++
	}
}

// Summary is the shareable part of a report.
func Summary(r Report) Shareable {
	s := Shareable{
		Answers: r.Answers, Skipped: r.Skipped, Flagged: r.Flagged,
		FlagPct: r.FlagPct, AnsweredPct: r.AnsweredPct, Feedback: r.Feedback,
		ByOutcome: append([]KindCount(nil), r.ByOutcome...),
		Reasons:   append([]ReasonCount(nil), r.Reasons...),
	}
	for _, it := range r.Review {
		s.ReviewItems++
		if it.Late {
			s.LateFlags++
		}
		s.FlaggedTotal++
		if it.Closure != nil {
			s.Closed++
			s.ClosedByHow.count(it.Closure.How)
		} else {
			s.Open++
		}
		if it.Overdue {
			s.Overdue++
		}
	}
	for _, g := range r.Gaps {
		if g.Source == NoPassage {
			s.NoPassageClose += len(g.Questions)
		} else {
			s.GapGroups++
		}
	}
	return s
}
