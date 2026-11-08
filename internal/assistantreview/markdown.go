package assistantreview

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Markdown writes the private report for a person: counts, the review list
// with questions and answers, and the gap groups.
func Markdown(w io.Writer, r Report) error {
	var b strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	p("# Assistant quality review — %s\n\n", r.Week)
	p("%s to %s (UTC), from `data/monitor/assistant.jsonl` (#438). **Private:** this file holds the questions and answers; only the counts of `<week>.summary.json` may be shared.\n\n",
		r.From.Format(time.DateOnly), r.To.AddDate(0, 0, -1).Format(time.DateOnly))
	p("- Answers: %d — answered %s, flagged %d (%s)\n", r.Answers, fmtPct(r.AnsweredPct), r.Flagged, fmtPct(r.FlagPct))
	p("- Did it help? (the person's opinion, not a measure of correctness): helped %d, didn't help %d, no feedback %d — helped share %s, response rate %s\n",
		r.Helped, r.NotHelped, r.NoFeedback, helpedShare(r.Feedback), fmtPct(r.ResponsePct))
	s := Summary(r)
	p("- Review list: %d rows — closed %d (help fix %d, golden case %d, answer was right %d), open %d, overdue %d\n",
		s.FlaggedTotal, s.Closed, s.ClosedByHow.HelpFix, s.ClosedByHow.GoldenCase, s.ClosedByHow.AnswerWasRight, s.Open, s.Overdue)
	if r.Skipped > 0 {
		p("- Unreadable log lines skipped: %d\n", r.Skipped)
	}
	if r.ClosuresSkipped > 0 {
		p("- Unreadable closure lines skipped: %d (their answers show as open)\n", r.ClosuresSkipped)
	}
	p("\n## Answers by kind\n\n| kind | answers | flagged | flag rate |\n|---|---|---|---|\n")
	for _, k := range r.ByOutcome {
		p("| %s | %d | %d | %s |\n", k.Kind, k.Answers, k.Flagged, fmtPct(k.FlagPct))
	}
	p("\n## Did it help?\n\nThe last \"Helped\" or \"Didn't help\" of the person who asked (#579): their opinion, never a measure of correctness. Helped share = helped ÷ (helped + didn't help), shown from %d responses; response rate = (helped + didn't help) ÷ answers.\n\n", MinResponses)
	p("| kind | answers | helped | didn't help | no feedback | helped share | response rate |\n|---|---|---|---|---|---|---|\n")
	for _, k := range r.ByOutcome {
		p("| %s | %d | %d | %d | %d | %s | %s |\n", k.Kind, k.Answers, k.Helped, k.NotHelped, k.NoFeedback, helpedShare(k.Feedback), fmtPct(k.ResponsePct))
	}
	p("\n## Guard rejections\n\n| reason | answers |\n|---|---|\n")
	for _, g := range r.Reasons {
		p("| %s | %d |\n", g.Reason, g.Answers)
	}

	p("\n## Review list\n\nEvery flagged and every `dont_know` answer, overdue rows first (open more than %d days), with the unclosed rows of earlier weeks carried. Each ends within %d days as a help-source fix, a golden-set case or a recorded \"answer was right\": `go run ./cmd/assistantreview close -id <answer id> -how %s -ref \"...\"`.\n\n",
		OverdueDays, OverdueDays, strings.Join(Hows, "|"))
	if len(r.Review) == 0 {
		p("Nothing to review.\n")
	}
	for _, it := range r.Review {
		tags := it.Kind
		if it.Flagged {
			tags += ", flagged"
		}
		if it.Late {
			tags += ", asked before the week"
		}
		if it.Carried {
			tags += ", carried"
		}
		p("### %s — %s (%s) — %s\n\n", it.Time.UTC().Format("2006-01-02 15:04"), it.ID, tags, it.status())
		if it.Closure != nil {
			p("- Closed: %s, %s, ref %s\n", it.Closure.ClosedAt.UTC().Format(time.DateOnly), it.Closure.How, oneLine(it.Closure.Ref))
		}
		p("- **Question:** %s\n", oneLine(it.Question))
		p("- **Answer shown:** %s\n", oneLine(it.Answer))
		p("- Sources given: %s\n", list(it.Sources))
		p("- Cited: %s\n", list(it.Citations))
		if len(it.Verdict) > 0 {
			p("- Guard: %s\n", strings.Join(it.Verdict, "; "))
		}
		for _, n := range it.Notes {
			p("- Flag note: %s\n", oneLine(n))
		}
		p("\n")
	}

	p("## Gap candidates\n\nThe review's questions grouped by the nearest help passage (`helpkb.Search`, at least %.0f %% of the question's weight). A passage named: improve it or add a glossary term; %q: a new section.\n\n", 100*GapCoverage, NoPassage)
	if len(r.Gaps) == 0 {
		p("None.\n")
	}
	for _, g := range r.Gaps {
		head := "`" + g.Source + "`"
		if g.Source == NoPassage {
			head = NoPassage
		} else if g.Title != "" {
			head += " — " + g.Title
		}
		p("### %s\n\n", head)
		for _, q := range g.Questions {
			p("- %s (%s, %s, coverage %.0f %%)\n", oneLine(q.Question), q.ID, q.Kind, 100*q.Coverage)
		}
		p("\n")
	}
	p("Golden-set candidates: `%s.golden_candidates.jsonl` (category, expected behaviour and sources empty; nothing is added to `golden.jsonl`).\n", r.Week)
	_, err := io.WriteString(w, b.String())
	return err
}

func fmtPct(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%.1f %%", *v)
}

// helpedShare is the helped share, or "too few responses" under MinResponses.
func helpedShare(f Feedback) string {
	if f.HelpedPct == nil {
		return fmt.Sprintf("too few responses (%d of %d)", f.Responses(), MinResponses)
	}
	return fmtPct(f.HelpedPct)
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "—"
	}
	return s
}

func list(xs []string) string {
	if len(xs) == 0 {
		return "none"
	}
	return "`" + strings.Join(xs, "`, `") + "`"
}
