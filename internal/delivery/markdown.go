package delivery

import (
	"fmt"
	"strings"
	"time"
)

// SummaryMarkdown is a week's four numbers per environment: what dora.sh
// posts on the E6 epic.
func SummaryMarkdown(w Week) string {
	var s strings.Builder
	fmt.Fprintf(&s, "**Delivery (DORA), week %s** (%s – %s UTC)\n\n", w.Week,
		w.From.Format("2006-01-02"), w.To.AddDate(0, 0, -1).Format("2006-01-02"))
	s.WriteString("| | mvp | stable |\n|---|---|---|\n")
	fmt.Fprintf(&s, "| Deployment frequency | %s | %s |\n", frequencyCell(w.MVP, "deploys", "a day"), frequencyCell(w.Stable, "releases", "a day"))
	fmt.Fprintf(&s, "| Lead time, merge → live (median / p90) | %s | %s |\n", envStat(w.MVP, w.MVP.MergeToLive), envStat(w.Stable, w.Stable.MergeToLive))
	fmt.Fprintf(&s, "| Lead time, first commit → live (median / p90) | %s | %s |\n", envStat(w.MVP, w.MVP.CommitToLive), envStat(w.Stable, w.Stable.CommitToLive))
	fmt.Fprintf(&s, "| Change failure rate | %s | %s |\n", failureCell(w.MVP), failureCell(w.Stable))
	fmt.Fprintf(&s, "| Time to restore (median / p90) | %s | %s |\n", restoreCell(w.MVP), restoreCell(w.Stable))
	fmt.Fprintf(&s, "\nMerged: %d PRs; first commit → merge %s.\n", w.Merged, statCell(w.CommitToMerge))
	for _, n := range []struct{ env, note string }{{"mvp", w.MVP.Note}, {"stable", w.Stable.Note}} {
		if n.note != "" {
			fmt.Fprintf(&s, "%s: %s.\n", n.env, n.note)
		}
	}
	return s.String()
}

// WeekMarkdown is <week>.md: the summary, then every PR merged or gone live
// in the week with its times, then the week's failures.
func WeekMarkdown(r Report, w Week) string {
	var s strings.Builder
	fmt.Fprintf(&s, "# Delivery (DORA) — %s\n\n", w.Week)
	s.WriteString(SummaryMarkdown(w))
	in := func(t *time.Time) bool { return t != nil && !t.Before(w.From) && t.Before(w.To) }
	s.WriteString("\n## Pull requests (merged or live this week)\n\n")
	s.WriteString("| PR | Epic | Title | Merged | Live on mvp | Live on stable | Failures |\n|---|---|---|---|---|---|---|\n")
	rows := 0
	for _, p := range r.PRs {
		merged := p.Merged
		if !in(&merged) && !in(p.MVPLive) && !in(p.StableLive) {
			continue
		}
		rows++
		stable := when(p.StableLive, p.StableNote)
		if p.Release != "" {
			stable += " (" + p.Release + ")"
		}
		fmt.Fprintf(&s, "| #%d | %s | %s | %s | %s | %s | %s |\n", p.Number, dash(p.Epic), cell(p.Title),
			merged.Format("2006-01-02 15:04"), when(p.MVPLive, p.MVPNote), stable, dash(strings.Join(p.Failures, ", ")))
	}
	if rows == 0 {
		s.WriteString("| — | | none | | | | |\n")
	}
	s.WriteString("\n## Failures (starting this week)\n\n")
	n := 0
	for _, f := range r.Failures {
		if !in(&f.At) {
			continue
		}
		n++
		fmt.Fprintf(&s, "- %s %s at %s: %s; restored %s\n", f.Env, f.Kind, f.At.Format("2006-01-02 15:04"), f.Detail,
			when(f.RestoredAt, "not yet"))
	}
	if n == 0 {
		s.WriteString("None.\n")
	}
	s.WriteString("\nTimes in UTC. Source: main's git history, gh's PRs and issues, <envs>/history/deploys.tsv and promotions.tsv (`cmd/dora`, #470). Nothing is estimated: a week before recording says \"not recorded\".\n")
	return s.String()
}

func frequencyCell(e EnvWeek, what, per string) string {
	if !e.Recorded {
		return e.Note
	}
	if e.PerDay == nil {
		return fmt.Sprintf("%d %s", e.Deploys, what)
	}
	return fmt.Sprintf("%d %s (%.2f %s) · %s", e.Deploys, what, *e.PerDay, per, e.FrequencyBand)
}

func envStat(e EnvWeek, st Stat) string {
	if !e.Recorded {
		return e.Note
	}
	return statCell(st)
}

func statCell(st Stat) string {
	if st.MedianHours == nil {
		return st.Note
	}
	out := hours(*st.MedianHours) + " / " + hours(*st.P90Hours) + fmt.Sprintf(" (n=%d)", st.N)
	if st.Band != "" {
		return out + " · " + st.Band
	}
	return out + " · " + st.Note
}

func failureCell(e EnvWeek) string {
	if !e.Recorded {
		return e.Note
	}
	if e.FailureRatePct == nil {
		return e.FailureNote
	}
	out := fmt.Sprintf("%.0f %% (%d of %d)", *e.FailureRatePct, e.Failed, e.Attempts)
	if e.FailureBand != "" {
		return out + " · " + e.FailureBand
	}
	return out + " · " + e.FailureNote
}

func restoreCell(e EnvWeek) string {
	if !e.Recorded {
		return e.Note
	}
	out := statCell(e.Restore)
	if e.Unrestored > 0 {
		out += fmt.Sprintf("; %d not restored yet", e.Unrestored)
	}
	return out
}

// hours prints a duration in minutes, hours or days.
func hours(h float64) string {
	switch {
	case h < 1:
		return fmt.Sprintf("%.0f min", h*60)
	case h < 48:
		return fmt.Sprintf("%.1f h", h)
	}
	return fmt.Sprintf("%.1f d", h/24)
}

func when(t *time.Time, note string) string {
	if t == nil {
		return dash(note)
	}
	return t.Format("2006-01-02 15:04")
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func cell(s string) string { return strings.ReplaceAll(s, "|", "\\|") }
