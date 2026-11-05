package tests

import (
	"strings"
	"testing"
	"time"

	"optionsbot/internal/usage"
)

// E11's measure (#642): the weekly report's Education section.

func educationReport(t *testing.T, lines []string) (usage.Report, string) {
	t.Helper()
	w := usageWeek(t, "2026-W43")
	r, _, err := usage.Run(writeUsageEvents(t, lines), w.From, w.To, w.Label, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var md strings.Builder
	if err := usage.Markdown(&md, r); err != nil {
		t.Fatal(err)
	}
	return r, md.String()
}

func TestUsageEducation_ReportShowsTheSectionWithTheResolvedShare(t *testing.T) {
	at := usageWeek(t, "2026-W43").From.Add(3 * time.Hour)
	tip := func(n int, d time.Duration, id string) string {
		return usageLine(t, "tip_opened", at.Add(d), n, map[string]any{"tip_id": id, "screen": "overview"})
	}
	lines := []string{
		usageLine(t, "session_started", at, 1, nil),
		usageLine(t, "session_started", at, 2, nil),
		tip(1, time.Minute, "tip.gamma_flip"),
		tip(1, 20*time.Minute, "tip.gamma_flip"),
		tip(2, time.Minute, "tip.gamma_flip"),
		usageLine(t, "assistant_ask", at.Add(3*time.Minute), 2, map[string]any{"entry": "menu"}),
		usageLine(t, "video_played", at.Add(time.Hour), 1, map[string]any{"video_id": "video.change_setting"}),
		usageLine(t, "video_completed", at.Add(time.Hour+time.Minute), 1, map[string]any{"video_id": "video.change_setting", "watched_ms": 50000}),
		usageLine(t, "help_resolved", at.Add(2*time.Hour), 1, map[string]any{"tip_id": "tip.gamma_flip", "helped": true}),
		usageLine(t, "help_resolved", at.Add(2*time.Hour), 2, map[string]any{"tip_id": "tip.gamma_flip", "helped": false}),
		usageLine(t, "assistant_helpful", at.Add(2*time.Hour), 2, map[string]any{"helped": true}),
	}
	r, md := educationReport(t, lines)
	e := r.Education
	if e == nil || e.NoData {
		t.Fatalf("education = %+v", e)
	}
	if *e.TipsPerPerson != 1.5 || *e.VideosPerPerson != 0.5 || *e.VideoCompletionPct != 100 {
		t.Fatalf("rates = %v %v %v", *e.TipsPerPerson, *e.VideosPerPerson, *e.VideoCompletionPct)
	}
	if *e.ResolvedByTipVideoPct != 50 { // 1 by a tip, 1 by the assistant
		t.Fatalf("resolved share = %v, want 50", *e.ResolvedByTipVideoPct)
	}
	// user 1's two opens have no question after; user 2's open is followed by one.
	if len(e.NoFollowUp) != 1 || e.NoFollowUp[0].Count != 2 || len(e.FollowedByQuestion) != 1 || e.FollowedByQuestion[0].Count != 1 {
		t.Fatalf("top tips = %+v / %+v", e.NoFollowUp, e.FollowedByQuestion)
	}
	if !strings.Contains(md, "## Education (Q5)") || !strings.Contains(md, "share of resolved help from tips and videos: 50 %") {
		t.Fatalf("markdown lacks the section:\n%s", md)
	}
}

func TestUsageEducation_NoDataIsNotZero(t *testing.T) {
	at := usageWeek(t, "2026-W43").From.Add(time.Hour)
	r, md := educationReport(t, []string{usageLine(t, "session_started", at, 1, nil)})
	e := r.Education
	if e == nil || !e.NoData || e.TipsPerPerson != nil || e.ResolvedByTipVideoPct != nil || e.VideoCompletionPct != nil {
		t.Fatalf("education = %+v, want no data and no rates", e)
	}
	if !strings.Contains(md, "No data yet") || strings.Contains(md, "per person: 0") {
		t.Fatalf("markdown:\n%s", md)
	}
}
