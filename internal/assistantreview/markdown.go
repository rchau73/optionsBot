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
	if r.Skipped > 0 {
		p("- Unreadable log lines skipped: %d\n", r.Skipped)
	}
	p("\n## Answers by kind\n\n| kind | answers | flagged | flag rate |\n|---|---|---|---|\n")
	for _, k := range r.ByOutcome {
		p("| %s | %d | %d | %s |\n", k.Kind, k.Answers, k.Flagged, fmtPct(k.FlagPct))
	}
	p("\n## Guard rejections\n\n| reason | answers |\n|---|---|\n")
	for _, g := range r.Reasons {
		p("| %s | %d |\n", g.Reason, g.Answers)
	}

	p("\n## Review list\n\nEvery flagged and every `dont_know` answer. Each ends as a help-source fix, a golden-set case or a recorded \"answer was right\".\n\n")
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
		p("### %s — %s (%s)\n\n", it.Time.UTC().Format("2006-01-02 15:04"), it.ID, tags)
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
