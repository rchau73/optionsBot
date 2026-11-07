package tests

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/assistantreview"
)

// Scenario: A closure is recorded. The row was on last week's list; the close
// goes through the file, and the next review reads the same file.
func TestAssistantClosure_ACloseIsRecorded(t *testing.T) {
	w := arWeek(t)
	log := arAnswer(t, "A-0042", w.From.Add(-72*time.Hour), "answered", "Why was my put closed?") +
		arFlag(t, "A-0042", w.From.Add(-71*time.Hour), "wrong rule")
	logPath, closures := arFiles(t, log)
	at := w.From.Add(24 * time.Hour)

	if _, err := assistantreview.Close(logPath, closures, "A-0042", "golden_case", " ng-consent-analytics ", at); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(closures)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(string(b)), "\n"); len(lines) != 1 {
		t.Fatalf("closures.jsonl %q, want one line", b)
	}
	var c assistantreview.Closure
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	if c.AnswerID != "A-0042" || c.How != "golden_case" || c.Ref != "ng-consent-analytics" || !c.ClosedAt.Equal(at) {
		t.Fatalf("closure %+v", c)
	}
	info, err := os.Stat(closures)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("closures.jsonl mode %v (%v), want 0600", info.Mode(), err)
	}

	r := arClosed(t, log, string(b), w.To)
	if len(r.Review) != 1 || r.Review[0].Closure == nil || r.Review[0].OpenDays != nil || r.Review[0].Overdue {
		t.Fatalf("review %+v", r.Review)
	}
	if md := arMarkdown(t, r); !strings.Contains(md, "— closed (golden_case)") || !strings.Contains(md, "ref ng-consent-analytics") {
		t.Fatalf("the row does not read closed:\n%s", md)
	}
}

// Scenario: An answer still open after 7 days is overdue — and the boundary:
// exactly 7 days, or 7 days and some hours, is not past the rule's 7 days.
func TestAssistantClosure_OpenAfter7DaysIsOverdue(t *testing.T) {
	w := arWeek(t)
	flagged := w.From.Add(time.Hour)
	log := arAnswer(t, "young", flagged.Add(-time.Hour), "dont_know", "Asked at the start of the week") +
		arAnswer(t, "old", w.From.Add(2*time.Hour), "answered", "Flagged after a day") +
		arFlag(t, "old", flagged, "")
	for _, c := range []struct {
		name    string
		age     time.Duration
		days    int
		overdue int
		reads   string
	}{
		{"one day", 24 * time.Hour, 1, 0, "open 1 day"},
		{"exactly 7 days", 7 * 24 * time.Hour, 7, 0, "open 7 days"},
		{"just under 8 days", 8*24*time.Hour - time.Minute, 7, 0, "open 7 days"},
		{"8 days", 8 * 24 * time.Hour, 8, 1, "open 8 days, overdue"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := arClosed(t, arAnswer(t, "x", flagged, "answered", "q")+arFlag(t, "x", flagged, ""), "", flagged.Add(c.age))
			if got := *r.Review[0].OpenDays; got != c.days {
				t.Fatalf("open days %d, want %d", got, c.days)
			}
			if s := assistantreview.Summary(r); s.Overdue != c.overdue || s.Open != 1 || s.FlaggedTotal != 1 {
				t.Fatalf("summary %+v", s)
			}
			if md := arMarkdown(t, r); !strings.Contains(md, "— "+c.reads+"\n") {
				t.Fatalf("no row reads %q:\n%s", c.reads, md)
			}
		})
	}

	// The clock of a flagged answer is its first flag; of an unflagged dont_know, the answer.
	// Overdue rows come first although "young" was asked first.
	r := arClosed(t, log+arFlag(t, "old", flagged.Add(time.Hour), ""), "", flagged.Add(8*24*time.Hour-30*time.Minute))
	if r.Review[0].ID != "young" || !r.Review[0].Overdue || r.Review[1].Overdue {
		t.Fatalf("review %+v", r.Review)
	}
	if got := *r.Review[1].OpenDays; got != 7 {
		t.Fatalf("flagged row open %d days, want 7", got)
	}
	r = arClosed(t, arAnswer(t, "a", w.From, "answered", "q")+arAnswer(t, "b", w.From.Add(time.Hour), "dont_know", "q")+
		arFlag(t, "b", w.From.Add(time.Hour), "")+arFlag(t, "a", w.From.Add(2*time.Hour), ""), "", w.From.Add(8*24*time.Hour+2*time.Hour))
	if r.Review[0].ID != "a" || r.Review[1].ID != "b" || !r.Review[0].Overdue || !r.Review[1].Overdue {
		t.Fatalf("both overdue stay in time order: %+v", r.Review)
	}
	// A clock before the flag never goes negative.
	r = arClosed(t, arAnswer(t, "a", w.From, "dont_know", "q"), "", w.From.Add(-time.Hour))
	if got := *r.Review[0].OpenDays; got != 0 {
		t.Fatalf("open days %d, want 0", got)
	}
}

// Scenario: The shareable summary stays free of content.
func TestAssistantClosure_SummaryStaysFreeOfContent(t *testing.T) {
	w := arWeek(t)
	log := arAnswer(t, "secretid1", w.From, "dont_know", "Private question one") +
		arAnswer(t, "secretid2", w.From, "dont_know", "Private question two") +
		arAnswer(t, "secretid3", w.From, "dont_know", "Private question three") +
		arAnswer(t, "secretid4", w.From.Add(-30*24*time.Hour), "dont_know", "Private question four") +
		arAnswer(t, "secretid5", w.From, "dont_know", "Private question five")
	closures := arClosure(t, "secretid1", "help_fix", w.To) + arClosure(t, "secretid2", "golden_case", w.To) +
		arClosure(t, "secretid3", "answer_was_right", w.To)
	s := assistantreview.Summary(arClosed(t, log, closures, w.To))
	want := assistantreview.ClosedByHow{HelpFix: 1, GoldenCase: 1, AnswerWasRight: 1}
	if s.FlaggedTotal != 5 || s.Closed != 3 || s.Open != 2 || s.Overdue != 1 || s.ClosedByHow != want {
		t.Fatalf("summary %+v", s)
	}
	allowed := map[string]bool{}
	for _, k := range assistantreview.Outcomes {
		allowed[k] = true
	}
	for _, g := range assistantreview.GuardReasons {
		allowed[g] = true
	}
	checkCountsOnly(t, reflect.ValueOf(s), "Summary", allowed)
	js, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"flagged_total":5`, `"closed":3`, `"open":2`, `"overdue":1`, `"help_fix":1`, `"golden_case":1`, `"answer_was_right":1`} {
		if !strings.Contains(string(js), key) {
			t.Fatalf("summary json has no %s: %s", key, js)
		}
	}
	for _, leak := range []string{"secretid", "Private", "a ref"} {
		if strings.Contains(string(js), leak) {
			t.Fatalf("summary carries %q: %s", leak, js)
		}
	}
}

// Scenario: A bad closure is refused — nothing is written, and the message says
// which of the four it was.
func TestAssistantClosure_ABadClosureIsRefused(t *testing.T) {
	w := arWeek(t)
	log := arAnswer(t, "flagged", w.From, "answered", "q") + arFlag(t, "flagged", w.From, "") +
		arAnswer(t, "dontknow", w.From, "dont_know", "q") +
		arAnswer(t, "fine", w.From, "answered", "q") +
		arFlag(t, "orphan", w.From, "") +
		arAnswer(t, "test", w.From, "dont_know", "q", "test_run", true)
	for _, c := range []struct{ name, id, how, ref, says string }{
		{"unknown id", "nope", "help_fix", "#12", "unknown id"},
		{"empty id", " ", "help_fix", "#12", "unknown id"},
		{"an answer nobody flagged", "fine", "help_fix", "#12", "unknown id"},
		{"a flag with no answer", "orphan", "help_fix", "#12", "unknown id"},
		{"a test-run answer", "test", "help_fix", "#12", "unknown id"},
		{"unknown how", "flagged", "fixed", "#12", "unknown how"},
		{"empty how", "flagged", "", "#12", "unknown how"},
		{"empty ref", "flagged", "help_fix", "", "empty ref"},
		{"blank ref", "flagged", "help_fix", "   ", "empty ref"},
	} {
		t.Run(c.name, func(t *testing.T) {
			logPath, closures := arFiles(t, log)
			_, err := assistantreview.Close(logPath, closures, c.id, c.how, c.ref, w.To)
			if err == nil || !strings.Contains(err.Error(), c.says) {
				t.Fatalf("err %v, want it to say %q", err, c.says)
			}
			if _, statErr := os.Stat(closures); !os.IsNotExist(statErr) {
				t.Fatalf("a refused closure wrote the file: %v", statErr)
			}
		})
	}

	logPath, closures := arFiles(t, log)
	if _, err := assistantreview.Close(logPath, closures, "dontknow", "answer_was_right", "the help has nothing on it", w.To); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(closures)
	if err != nil {
		t.Fatal(err)
	}
	_, err = assistantreview.Close(logPath, closures, "dontknow", "help_fix", "#12", w.To)
	if err == nil || !strings.Contains(err.Error(), "already closed") || !strings.Contains(err.Error(), "answer_was_right") {
		t.Fatalf("second closure: %v", err)
	}
	if after, _ := os.ReadFile(closures); !bytes.Equal(before, after) {
		t.Fatalf("a refused closure changed the file: %s", after)
	}
	logBytes, _ := os.ReadFile(logPath)
	if string(logBytes) != log {
		t.Fatal("close wrote the review log")
	}
}

// Scenario: A question already turned into a golden case is not offered again:
// by its answer, and when a new answer asks the same words this week.
func TestAssistantClosure_AGoldenCaseIsNotOfferedAgain(t *testing.T) {
	w := arWeek(t)
	log := arAnswer(t, "last", w.From.Add(-72*time.Hour), "dont_know", "What is a calendar spread?") +
		arAnswer(t, "again", w.From.Add(time.Hour), "dont_know", "  what is a CALENDAR spread? ") +
		arAnswer(t, "other", w.From.Add(2*time.Hour), "dont_know", "Why was the put shed?") +
		arAnswer(t, "right", w.From.Add(3*time.Hour), "dont_know", "What is a butterfly?")
	closures := arClosure(t, "last", "golden_case", w.From.Add(-24*time.Hour)) +
		arClosure(t, "right", "answer_was_right", w.From.Add(4*time.Hour))
	cands := assistantreview.Candidates(arClosed(t, log, closures, w.To))
	if len(cands) != 2 || cands[0].Question != "Why was the put shed?" || cands[1].Question != "What is a butterfly?" {
		t.Fatalf("candidates %+v: only a golden_case closure drops a question", cands)
	}

	dir := t.TempDir()
	if _, err := assistantreview.Write(dir, arClosed(t, log, closures, w.To)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "2026-W44.golden_candidates.jsonl"))
	if err != nil || strings.Contains(strings.ToLower(string(b)), "calendar") {
		t.Fatalf("candidates file %s (%v)", b, err)
	}
}

// Scenario: Older unclosed rows are carried; a row closed before the week is
// not, one closed since shows once, and neither feeds this week's candidates or gaps.
func TestAssistantClosure_OlderUnclosedRowsAreCarried(t *testing.T) {
	w := arWeek(t)
	day := 24 * time.Hour
	log := arAnswer(t, "open-flag", w.From.Add(-3*day), "answered", "Open and flagged") +
		arFlag(t, "open-flag", w.From.Add(-2*day), "wrong") +
		arAnswer(t, "open-dk", w.From.Add(-20*day), "dont_know", "Open I don't know") +
		arAnswer(t, "closed-before", w.From.Add(-3*day), "dont_know", "Closed before the week") +
		arAnswer(t, "closed-in", w.From.Add(-3*day), "dont_know", "Closed during the week") +
		arAnswer(t, "never", w.From.Add(-3*day), "answered", "Nobody flagged it") +
		arAnswer(t, "flag-later", w.From.Add(-3*day), "answered", "Flagged after the week") +
		arFlag(t, "flag-later", w.To.Add(time.Hour), "") +
		arAnswer(t, "future", w.To.Add(time.Hour), "dont_know", "After the week") +
		arAnswer(t, "this-week", w.From.Add(time.Hour), "dont_know", "This week")
	closures := arClosure(t, "closed-before", "help_fix", w.From.Add(-time.Hour)) +
		arClosure(t, "closed-in", "help_fix", w.From.Add(time.Hour))
	r := arClosed(t, log, closures, w.To.Add(2*time.Hour))

	got := map[string]assistantreview.ReviewItem{}
	for _, it := range r.Review {
		got[it.ID] = it
	}
	if len(got) != 4 {
		t.Fatalf("review %+v", r.Review)
	}
	if it := got["open-flag"]; !it.Carried || !it.Flagged || it.Late || *it.OpenDays != 9 {
		t.Fatalf("open-flag %+v", it)
	}
	if it := got["open-dk"]; !it.Carried || !it.Overdue || *it.OpenDays != 27 {
		t.Fatalf("open-dk %+v", it)
	}
	if it := got["closed-in"]; !it.Carried || it.Closure == nil {
		t.Fatalf("closed-in %+v", it)
	}
	if it := got["this-week"]; it.Carried {
		t.Fatalf("this-week %+v", it)
	}
	if r.Review[0].ID != "open-dk" || r.Review[1].ID != "open-flag" {
		t.Fatalf("overdue rows first: %+v", r.Review)
	}
	md := arMarkdown(t, r)
	if !strings.Contains(md, "(dont_know, carried) — open 27 days, overdue") {
		t.Fatalf("the row is not marked carried:\n%s", md)
	}
	if r.Answers != 1 || r.Flagged != 0 {
		t.Fatalf("carried rows stay out of the week's answers: %d, %d", r.Answers, r.Flagged)
	}
	if len(r.Gaps) != 1 || len(r.Gaps[0].Questions) != 1 || r.Gaps[0].Questions[0].ID != "this-week" {
		t.Fatalf("gaps %+v", r.Gaps)
	}
	if c := assistantreview.Candidates(r); len(c) != 1 || c[0].AnswerID != "this-week" {
		t.Fatalf("candidates %+v", c)
	}

	// A late flag stays Late, not carried, and shows open too.
	log = arAnswer(t, "late", w.From.Add(-3*day), "answered", "q") + arFlag(t, "late", w.From.Add(day), "")
	r = arClosed(t, log, "", w.To)
	if len(r.Review) != 1 || !r.Review[0].Late || r.Review[0].Carried || *r.Review[0].OpenDays != 6 {
		t.Fatalf("late %+v", r.Review)
	}
}

// A week with no answers still keeps an open row: the job must see it (the
// quiet-week scenario needs no answers and no open rows).
func TestAssistantClosure_AQuietWeekKeepsItsOpenRows(t *testing.T) {
	w := arWeek(t)
	s := assistantreview.Summary(arClosed(t, arAnswer(t, "old", w.From.Add(-time.Hour), "dont_know", "q"), "", w.To))
	if s.Answers != 0 || s.Open != 1 {
		t.Fatalf("summary %+v", s)
	}
	s = assistantreview.Summary(arClosed(t, "", "", w.To))
	if s.FlaggedTotal != 0 || s.Open != 0 || s.Overdue != 0 {
		t.Fatalf("empty summary %+v", s)
	}
}

func TestAssistantClosure_DamagedLinesLeaveTheAnswerOpen(t *testing.T) {
	w := arWeek(t)
	cs, err := assistantreview.ParseClosures(strings.NewReader(
		"not json\n\n" +
			`{"answer_id":"a","how":"help_fix","ref":"x"}` + "\n" + // no time
			arClosure(t, "b", "invented", w.To) +
			arClosure(t, "", "help_fix", w.To) +
			arClosure(t, "c", "help_fix", w.To) +
			arClosure(t, "c", "golden_case", w.To) + // by hand: the first stays
			`{"answer_id":"d","closed_at":"2026-1`))
	if err != nil {
		t.Fatal(err)
	}
	if cs.Skipped != 5 || len(cs.ByID) != 1 || cs.ByID["c"].How != "help_fix" {
		t.Fatalf("closures %+v", cs)
	}
	r := arClosed(t, arAnswer(t, "a", w.From, "dont_know", "q"), "not json\n", w.To)
	if r.ClosuresSkipped != 1 || r.Review[0].Closure != nil || r.Review[0].OpenDays == nil {
		t.Fatalf("report %+v", r)
	}
	if md := arMarkdown(t, r); !strings.Contains(md, "Unreadable closure lines skipped: 1") {
		t.Fatalf("report does not say so:\n%s", md)
	}
	missing, err := assistantreview.LoadClosures(filepath.Join(t.TempDir(), "missing.jsonl"))
	if err != nil || len(missing.ByID) != 0 {
		t.Fatalf("a missing file holds no closure: %+v %v", missing, err)
	}
}

// The run's log gets the closure's date and how, never its id or ref.
func TestAssistantClosure_RunLogHoldsDateAndHow(t *testing.T) {
	w := arWeek(t)
	r := arClosed(t, arAnswer(t, "secretid", w.From, "dont_know", "q")+arAnswer(t, "o", w.From, "dont_know", "q"),
		arClosure(t, "secretid", "help_fix", time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)), w.To)
	want := []string{"closure 2026-11-02 help_fix"}
	if got := assistantreview.ClosureAudit(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("audit %v, want %v", got, want)
	}
}
