package tests

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/assistantreview"
	"optionsbot/internal/evidence"
	"optionsbot/internal/helpkb"
)

// arLine is one assistant.jsonl line as frontend/lib/assistant written it.
func arLine(t *testing.T, fields map[string]any) string {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func arAnswer(t *testing.T, id string, ts time.Time, outcome, question string, extra ...any) string {
	f := map[string]any{"kind": "answer", "id": id, "ts": ts.Format(time.RFC3339Nano), "user": "alice.trader", "role": "user",
		"bot": "btc", "question": question, "sources": []string{"glossary:strangle"}, "citations": []string{"glossary:strangle"},
		"verdict": []string{}, "outcome": outcome, "answer": "The answer shown, 0.42 BTC", "flag": false}
	for i := 0; i+1 < len(extra); i += 2 {
		f[extra[i].(string)] = extra[i+1]
	}
	return arLine(t, f)
}

func arFlag(t *testing.T, id string, ts time.Time, note string) string {
	return arLine(t, map[string]any{"kind": "flag", "id": id, "ts": ts.Format(time.RFC3339Nano), "user": "bob.manager", "role": "manager", "note": note})
}

func arWeek(t *testing.T) evidence.Window {
	t.Helper()
	w, err := evidence.ParseWeek("2026-W44")
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func arBuild(t *testing.T, log string, ix *helpkb.Index) assistantreview.Report {
	t.Helper()
	lg, err := assistantreview.Parse(strings.NewReader(log))
	if err != nil {
		t.Fatal(err)
	}
	w := arWeek(t)
	return assistantreview.Build(lg, w.From, w.To, w.Label, ix)
}

func arKind(r assistantreview.Report, kind string) assistantreview.KindCount {
	for _, k := range r.ByOutcome {
		if k.Kind == kind {
			return k
		}
	}
	return assistantreview.KindCount{}
}

func arIndex() *helpkb.Index {
	return helpkb.NewIndex([]helpkb.Passage{
		{ID: "OptionStrategy.md#stop-loss/1", Source: "OptionStrategy.md#stop-loss", Title: "Stop-loss",
			Text: "The stop-loss buys a leg back at market when its price reaches twice the premium."},
		{ID: "glossary:strangle/1", Source: "glossary:strangle", Title: "Strangle", Text: "A call and a put sold on one expiry."},
	})
}

// Scenario: counts by answer kind.
func TestAssistantReview_CountsByAnswerKind(t *testing.T) {
	w := arWeek(t)
	var log strings.Builder
	kinds := []string{"answered", "answered", "answered", "answered", "answered", "answered", "dont_know", "dont_know", "refused", "rejected"}
	for i, k := range kinds {
		log.WriteString(arAnswer(t, fmt.Sprintf("a%d", i), w.From.Add(time.Duration(i)*time.Hour), k, "question"))
	}
	log.WriteString(arAnswer(t, "before", w.From.Add(-time.Minute), "answered", "q"))
	log.WriteString(arAnswer(t, "after", w.To, "dont_know", "q"))
	r := arBuild(t, log.String(), nil)
	if r.Answers != 10 {
		t.Fatalf("answers %d, want 10", r.Answers)
	}
	if got := arKind(r, "dont_know").Answers; got != 2 {
		t.Fatalf("dont_know %d, want 2", got)
	}
	if got := arKind(r, "refused").Answers; got != 1 {
		t.Fatalf("refused %d, want 1", got)
	}
	sum := 0
	for _, k := range r.ByOutcome {
		sum += k.Answers
	}
	if sum != 10 {
		t.Fatalf("kinds sum to %d, want 10", sum)
	}
	if len(r.ByOutcome) != len(assistantreview.Outcomes) {
		t.Fatalf("every kind is listed: %d of %d", len(r.ByOutcome), len(assistantreview.Outcomes))
	}
	if r.AnsweredPct == nil || *r.AnsweredPct != 60 {
		t.Fatalf("answered pct %v, want 60", r.AnsweredPct)
	}
}

func TestAssistantReview_UnknownOutcomeIsOther(t *testing.T) {
	w := arWeek(t)
	r := arBuild(t, arAnswer(t, "x", w.From, "brand_new", "q"), nil)
	if arKind(r, "other").Answers != 1 {
		t.Fatalf("an unknown outcome counts as other: %+v", r.ByOutcome)
	}
}

// Scenario: flagged answers are listed.
func TestAssistantReview_FlaggedAnswersAreListed(t *testing.T) {
	w := arWeek(t)
	log := arAnswer(t, "f1", w.From.Add(time.Hour), "answered", "Why was my put closed?",
		"sources", []string{"OptionStrategy.md#stop-loss/1", "journal:btc/12"}, "citations", []string{"journal:btc/12"}) +
		arAnswer(t, "ok", w.From.Add(2*time.Hour), "answered", "What is a strangle?") +
		arFlag(t, "f1", w.From.Add(3*time.Hour), "it named the wrong rule")
	r := arBuild(t, log, nil)
	if r.Flagged != 1 || r.FlagPct == nil || *r.FlagPct != 50 {
		t.Fatalf("flagged %d (%v), want 1 of 2", r.Flagged, r.FlagPct)
	}
	if k := arKind(r, "answered"); k.Flagged != 1 || k.FlagPct == nil || *k.FlagPct != 50 {
		t.Fatalf("flag rate by kind: %+v", k)
	}
	if len(r.Review) != 1 {
		t.Fatalf("review list %+v, want the flagged answer only", r.Review)
	}
	it := r.Review[0]
	if it.ID != "f1" || !it.Flagged || it.Question != "Why was my put closed?" ||
		!reflect.DeepEqual(it.Sources, []string{"OptionStrategy.md#stop-loss/1", "journal:btc/12"}) ||
		!reflect.DeepEqual(it.Citations, []string{"journal:btc/12"}) || !reflect.DeepEqual(it.Notes, []string{"it named the wrong rule"}) {
		t.Fatalf("review item %+v", it)
	}
}

func TestAssistantReview_FlagWithoutNoteAndLateFlag(t *testing.T) {
	w := arWeek(t)
	log := arAnswer(t, "old", w.From.Add(-48*time.Hour), "answered", "Asked last week") +
		arAnswer(t, "new", w.From.Add(time.Hour), "answered", "Asked this week") +
		arFlag(t, "old", w.From.Add(2*time.Hour), "") +
		arFlag(t, "new", w.To.Add(time.Hour), "") + // flagged after the week: still flagged
		arFlag(t, "ghost", w.From, "no such answer")
	r := arBuild(t, log, nil)
	if r.Answers != 1 || r.Flagged != 1 {
		t.Fatalf("answers %d flagged %d, want 1 and 1", r.Answers, r.Flagged)
	}
	if len(r.Review) != 2 || r.Review[0].ID != "old" || !r.Review[0].Late || !r.Review[0].Flagged || r.Review[1].ID != "new" || !r.Review[1].Flagged {
		t.Fatalf("review %+v", r.Review)
	}
	if s := assistantreview.Summary(r); s.LateFlags != 1 || s.ReviewItems != 2 {
		t.Fatalf("summary %+v", s)
	}
}

func TestAssistantReview_GuardReasonsCounted(t *testing.T) {
	w := arWeek(t)
	log := arAnswer(t, "r1", w.From, "rejected", "q", "verdict", []string{"ungrounded_number: 45 %", "ungrounded_number: 3", "advice: you should"}) +
		arAnswer(t, "r2", w.From, "no_citation", "q", "verdict", []string{"no_citation: none"}) +
		arAnswer(t, "r3", w.From, "draft", "q", "verdict", []string{"evidence_missing: x"}) +
		arAnswer(t, "r4", w.From, "rejected", "q", "verdict", []string{"points_to_document: see the guide"}) +
		arAnswer(t, "r5", w.From, "rejected", "q", "verdict", []string{"language: 0 of 12 words are English"})
	r := arBuild(t, log, nil)
	want := map[string]int{"no_citation": 1, "unknown_citation": 0, "ungrounded_number": 1, "advice": 1, "forecast": 0, "points_to_document": 1, "language": 1}
	if len(r.Reasons) != len(want) {
		t.Fatalf("reasons %+v", r.Reasons)
	}
	for _, rc := range r.Reasons {
		if want[rc.Reason] != rc.Answers {
			t.Fatalf("%s: %d, want %d", rc.Reason, rc.Answers, want[rc.Reason])
		}
	}
}

// Scenario: gap candidates point to a help passage.
func TestAssistantReview_GapCandidatesPointToAHelpPassage(t *testing.T) {
	w := arWeek(t)
	log := arAnswer(t, "d1", w.From, "dont_know", "When does the stop-loss buy back a leg at market?") +
		arAnswer(t, "d2", w.From.Add(time.Minute), "dont_know", "Can I withdraw to my bank account on Sunday?")
	r := arBuild(t, log, arIndex())
	if len(r.Gaps) != 2 {
		t.Fatalf("gaps %+v", r.Gaps)
	}
	if r.Gaps[0].Source != "OptionStrategy.md#stop-loss" || r.Gaps[0].Questions[0].ID != "d1" {
		t.Fatalf("first gap %+v, want the stop-loss passage", r.Gaps[0])
	}
	if r.Gaps[1].Source != assistantreview.NoPassage || r.Gaps[1].Questions[0].ID != "d2" {
		t.Fatalf("last gap %+v, want %q", r.Gaps[1], assistantreview.NoPassage)
	}
	var md bytes.Buffer
	if err := assistantreview.Markdown(&md, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md.String(), "`OptionStrategy.md#stop-loss` — Stop-loss") || !strings.Contains(md.String(), "### no passage close") {
		t.Fatalf("markdown names the passage and the no-passage group:\n%s", md.String())
	}
}

// The same scenario on the shipped help-index.json the command reads.
func TestAssistantReview_GapOnTheShippedHelpIndex(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "frontend", "content", "help-index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f helpkb.File
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	w := arWeek(t)
	r := arBuild(t, arAnswer(t, "d1", w.From, "dont_know", "What is a strangle?")+
		arAnswer(t, "d2", w.From, "dont_know", "zzqx wobbleflarp"), helpkb.NewIndex(f.Chunks))
	if len(r.Gaps) != 2 || r.Gaps[0].Source == assistantreview.NoPassage || r.Gaps[1].Source != assistantreview.NoPassage {
		t.Fatalf("gaps %+v", r.Gaps)
	}
}

// Scenario: the shareable summary holds no text.
func TestAssistantReview_ShareableSummaryHoldsNoText(t *testing.T) {
	w := arWeek(t)
	question := "Why did my 0.5 BTC put on BTC-27NOV26-60000-P lose $1,234?"
	log := arAnswer(t, "secretid123", w.From, "dont_know", question) +
		arAnswer(t, "x2", w.From, "answered", "Another private question", "verdict", []string{"advice: you should buy"}) +
		arFlag(t, "x2", w.From.Add(time.Hour), "alice.trader says wrong, lost 0.3 BTC") +
		arAnswer(t, "x3", w.From, "weird_kind", "free text kind")
	r := arBuild(t, log, arIndex())
	s := assistantreview.Summary(r)
	if s.Answers != 3 || s.Flagged != 1 || s.ReviewItems != 2 {
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
	for _, leak := range []string{question, "0.5", "BTC", "alice", "bob", "secretid123", "Another private", "you should", "weird_kind", "answer shown"} {
		if strings.Contains(strings.ToLower(string(js)), strings.ToLower(leak)) {
			t.Fatalf("summary carries %q: %s", leak, js)
		}
	}
}

// checkCountsOnly fails on any field that is not a count, a percentage or a
// kind/reason from the fixed lists.
func checkCountsOnly(t *testing.T, v reflect.Value, path string, allowed map[string]bool) {
	t.Helper()
	switch v.Kind() {
	case reflect.Int:
	case reflect.Float64:
	case reflect.Ptr:
		if v.Type().Elem().Kind() != reflect.Float64 {
			t.Fatalf("%s: a %s field (counts and percentages only)", path, v.Type())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			checkCountsOnly(t, v.Field(i), path+"."+v.Type().Field(i).Name, allowed)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			checkCountsOnly(t, v.Index(i), fmt.Sprintf("%s[%d]", path, i), allowed)
		}
	case reflect.String:
		if !allowed[v.String()] {
			t.Fatalf("%s holds text %q: only kinds and reasons from the fixed lists", path, v.String())
		}
	default:
		t.Fatalf("%s: a %s field (counts and percentages only)", path, v.Kind())
	}
}

// Scenario: nothing is written to the golden set.
func TestAssistantReview_NothingIsWrittenToTheGoldenSet(t *testing.T) {
	goldenPath := filepath.Join("..", "docs", "product", "assistant", "golden.jsonl")
	before, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	w := arWeek(t)
	log := arAnswer(t, "d1", w.From, "dont_know", "What is a calendar spread?") +
		arAnswer(t, "d2", w.From.Add(time.Minute), "dont_know", "  what is a CALENDAR spread? ") +
		arAnswer(t, "f1", w.From.Add(2*time.Minute), "answered", "Why was the put shed?") +
		arFlag(t, "f1", w.From.Add(time.Hour), "")
	r := arBuild(t, log, arIndex())
	dir := t.TempDir()
	paths, err := assistantreview.Write(dir, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 4 {
		t.Fatalf("paths %v", paths)
	}
	after, err := os.ReadFile(goldenPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("golden.jsonl changed")
	}
	cf, err := os.Open(filepath.Join(dir, "2026-W44.golden_candidates.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer cf.Close()
	var cands []map[string]any
	sc := bufio.NewScanner(cf)
	for sc.Scan() {
		var c map[string]any
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		cands = append(cands, c)
	}
	if len(cands) != 2 {
		t.Fatalf("candidates %v, want the two distinct questions", cands)
	}
	for _, c := range cands {
		for _, key := range []string{"id", "category", "question", "expected_behaviour", "expected_sources", "must_say", "must_not_say"} {
			if _, ok := c[key]; !ok {
				t.Fatalf("candidate %v lacks golden.jsonl's %q", c, key)
			}
		}
		if c["category"] != "" || c["expected_behaviour"] != "" || len(c["expected_sources"].([]any)) != 0 {
			t.Fatalf("candidate %v: category, behaviour and sources must be empty", c)
		}
	}
	if cands[0]["question"] != "What is a calendar spread?" || cands[1]["question"] != "Why was the put shed?" {
		t.Fatalf("candidates %v", cands)
	}
	// The summary file is the shareable part only.
	sb, err := os.ReadFile(filepath.Join(dir, "2026-W44.summary.json"))
	if err != nil || strings.Contains(string(sb), "calendar") {
		t.Fatalf("summary file %s (%v)", sb, err)
	}
}

// Scenario: unreadable lines.
func TestAssistantReview_UnreadableLinesAreSkipped(t *testing.T) {
	w := arWeek(t)
	log := arAnswer(t, "a1", w.From, "answered", "q") + "not json\n" + `{"kind":"answer"}` + "\n" +
		arAnswer(t, "a2", w.From, "dont_know", "q2") + `{"kind":"answer","id":"a3","ts":"2026-10-2`
	r := arBuild(t, log, nil)
	if r.Answers != 2 || r.Skipped != 3 {
		t.Fatalf("answers %d skipped %d, want 2 and 3", r.Answers, r.Skipped)
	}
	if assistantreview.Summary(r).Skipped != 3 {
		t.Fatal("the summary carries the skipped count")
	}
}

// Scenario: test questions stay out of the user-facing numbers (#536): a
// golden-set run line (test_run) is never counted, listed or flagged.
func TestAssistantReview_TestRunLinesAreLeftOut(t *testing.T) {
	w := arWeek(t)
	log := arAnswer(t, "p1", w.From, "answered", "What is a strangle?") +
		arAnswer(t, "g1", w.From.Add(time.Hour), "dont_know", "golden question", "user", "golden-run", "role", "test_run", "test_run", true) +
		arAnswer(t, "g2", w.From.Add(2*time.Hour), "answered", "golden question 2", "test_run", true) +
		arFlag(t, "g1", w.From.Add(3*time.Hour), "a flag on a test answer")
	lg, err := assistantreview.Parse(strings.NewReader(log))
	if err != nil {
		t.Fatal(err)
	}
	if lg.TestRuns != 2 || lg.Skipped != 0 {
		t.Fatalf("test runs %d skipped %d, want 2 and 0", lg.TestRuns, lg.Skipped)
	}
	r := arBuild(t, log, nil)
	if r.Answers != 1 || r.Flagged != 0 || len(r.Review) != 0 {
		t.Fatalf("answers %d flagged %d review %d: only the person's answer counts", r.Answers, r.Flagged, len(r.Review))
	}
}

func TestAssistantReview_EmptyWeek(t *testing.T) {
	r := arBuild(t, "", nil)
	if r.Answers != 0 || r.FlagPct != nil || r.AnsweredPct != nil || len(r.Review) != 0 || len(r.Gaps) != 0 {
		t.Fatalf("empty week %+v", r)
	}
	lg, err := assistantreview.Load(filepath.Join(t.TempDir(), "missing.jsonl"))
	if err != nil || len(lg.Entries) != 0 {
		t.Fatalf("a missing log is empty: %+v %v", lg, err)
	}
}
