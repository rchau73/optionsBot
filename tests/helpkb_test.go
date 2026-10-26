package tests

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"optionsbot/internal/helpkb"
)

// The help knowledge index's retrieval (#259, #190): keyword search over the
// passages of help-index.json, measured on a question set.

const helpQuestions = "testdata/helpkb_questions.yaml"

func helpIndex(t *testing.T) *helpkb.Index {
	t.Helper()
	return helpkb.NewIndex(buildHelpIndex(t, helpSources(t)).Chunks)
}

// Scenario: a passage is citable — every section and subsection of
// OptionStrategy.md is a passage with a stable anchor; parsing twice gives
// the same anchors.
func TestHelpKB_PassageIsCitable(t *testing.T) {
	md := helpSources(t).Strategy
	first := helpkb.Parse(helpkb.StrategyFile, md)
	if !reflect.DeepEqual(first, helpkb.Parse(helpkb.StrategyFile, md)) {
		t.Fatal("parsing twice gave different passages")
	}
	bySource := map[string]bool{}
	for _, p := range first {
		bySource[p.Source] = true
	}
	for _, a := range helpkb.Anchors(md) {
		if !bySource[helpkb.StrategyFile+"#"+a] {
			t.Errorf("section #%s is no passage", a)
		}
	}
	for _, want := range []string{
		"OptionStrategy.md#9-market-regimes-and-what-the-bot-does",
		"OptionStrategy.md#loss-a-sharp-drop-stop-loss",
		"OptionStrategy.md#4-how-a-trade-can-end",
	} {
		if !bySource[want] {
			t.Errorf("no passage cites %s", want)
		}
	}
	// A subsection's title names its section.
	for _, p := range first {
		if p.Source == "OptionStrategy.md#loss-a-sharp-drop-stop-loss" && p.Title != "6. Worked examples: losses › Loss A: sharp drop, stop-loss" {
			t.Errorf("Loss A title %q", p.Title)
		}
	}
}

// Parse: headings in a code fence are text, repeated headings get GitHub's
// "-1" suffix, a long table is split between rows with its header repeated,
// a long list between items, links keep their text and rules and images go.
func TestHelpKB_ParseSplitsLongBlocks(t *testing.T) {
	row := "| rule | " + strings.Repeat("word ", 60) + "|"
	var rows []string
	for i := 0; i < 10; i++ {
		rows = append(rows, row)
	}
	item := "- " + strings.Repeat("item ", 60)
	var items []string
	for i := 0; i < 8; i++ {
		items = append(items, item, "  - sub item")
	}
	md := strings.Join([]string{
		"# Doc", "", "Intro with a [link](https://example.com).", "", "---", "", "![img](a.png)", "",
		"## A", "", "| Rule | Text |", "|---|---|", strings.Join(rows, "\n"), "",
		"## A", "", "```", "## not a heading", "```", "",
		"### Sub", "", strings.Join(items, "\n"),
	}, "\n")
	ps := helpkb.Parse("doc.md", []byte(md))
	got := map[string][]helpkb.Passage{}
	for _, p := range ps {
		got[p.Source] = append(got[p.Source], p)
		if n := len([]rune(p.Text)); n > helpkb.MaxChunkChars {
			t.Errorf("%s: %d characters", p.ID, n)
		}
	}
	if intro := got["doc.md#doc"]; len(intro) != 1 || intro[0].Text != "Intro with a link." {
		t.Errorf("intro = %+v", intro)
	}
	table := got["doc.md#a"]
	if len(table) < 3 {
		t.Fatalf("long table in %d passages, want it split", len(table))
	}
	for _, p := range table {
		if !strings.HasPrefix(p.Text, "| Rule | Text |\n|---|---|\n| rule |") {
			t.Errorf("%s does not repeat the table header: %.40q", p.ID, p.Text)
		}
	}
	if fence := got["doc.md#a-1"]; len(fence) != 1 || !strings.Contains(fence[0].Text, "## not a heading") {
		t.Errorf("repeated heading / fence = %+v", fence)
	}
	sub := got["doc.md#sub"]
	if len(sub) < 2 || sub[0].Title != "A › Sub" {
		t.Fatalf("long list = %+v", sub)
	}
	for _, p := range sub {
		if !strings.HasPrefix(p.Text, "- item") || !strings.HasSuffix(p.Text, "  - sub item") {
			t.Errorf("%s split inside an item: %.30q … %q", p.ID, p.Text, p.Text[len(p.Text)-12:])
		}
	}
	if ps[0].ID != "doc.md#doc/1" || table[1].ID != "doc.md#a/2" {
		t.Errorf("ids %s, %s", ps[0].ID, table[1].ID)
	}
}

func TestHelpKB_Tokens(t *testing.T) {
	got := helpkb.Tokens("What happens when a leg's delta reaches 0.30? Stopped, stopping, selling, puts, policies, loss.")
	want := []string{"leg", "delta", "reache", "0.30", "stop", "stop", "sell", "put", "policy", "loss"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokens = %v, want %v", got, want)
	}
}

// Scenario: a known question finds its section — in OptionStrategy.md's
// passages (the scenario's corpus); on the whole index the glossary's
// "delta exit" may come first, which the fixture set accepts.
func TestHelpKB_KnownQuestionFindsItsSection(t *testing.T) {
	ix := helpkb.NewIndex(helpkb.Parse(helpkb.StrategyFile, helpSources(t).Strategy))
	hits := ix.Search("what happens when a leg's delta reaches 0.30", 3)
	if helpkb.NoAnswer(hits, helpkb.DefaultNoAnswer) {
		t.Fatalf("NoAnswer for a question the help answers: %+v", hits)
	}
	found := false
	for _, h := range hits {
		if strings.HasPrefix(h.Source, "OptionStrategy.md#4-how-a-trade-can-end") ||
			strings.HasPrefix(h.Source, "OptionStrategy.md#10-the-parameters-in-plain-words") {
			found = true
		}
	}
	if !found {
		t.Errorf("neither §4 nor §10 in the top 3: %v", sources(hits))
	}
}

// Scenario: an unanswerable question.
func TestHelpKB_UnanswerableQuestion(t *testing.T) {
	ix := helpIndex(t)
	hits := ix.Search("what are Binance's option fees", 3)
	if !helpkb.NoAnswer(hits, helpkb.DefaultNoAnswer) {
		t.Errorf("NoAnswer false: %v (coverage %v)", sources(hits), coverages(hits))
	}
	if !helpkb.NoAnswer(ix.Search("the and of", 3), helpkb.DefaultNoAnswer) {
		t.Error("a question of stop words only is answerable")
	}
	if !helpkb.NoAnswer(nil, helpkb.DefaultNoAnswer) {
		t.Error("no hits is answerable")
	}
}

// Scenario (#190): for 10 fixture questions the expected section is in the
// top 3.
func TestHelpKB_FixtureQuestionsInTop3(t *testing.T) {
	qs := helpQuestionSet(t)
	answerable := 0
	ix := helpIndex(t)
	for _, q := range qs {
		if q.DontKnow {
			continue
		}
		answerable++
		hits := ix.Search(q.Question, 3)
		ok := false
		for _, h := range hits {
			for _, e := range q.Expect {
				ok = ok || strings.HasPrefix(h.Source, e)
			}
		}
		if !ok {
			t.Errorf("%q: no expected source in the top 3: %v", q.Question, sources(hits))
		}
	}
	if answerable < 10 {
		t.Errorf("%d answerable fixture questions, want at least 10", answerable)
	}
}

// Scenario: retrieval quality is measured — hit@1, hit@3 and dont_know
// precision per category; the test fails if hit@3 drops below the recorded
// baseline (tests/testdata/helpkb_baseline.json; UPDATE_GOLDEN=1 records it).
func TestHelpKB_RetrievalQualityIsMeasured(t *testing.T) {
	ev := helpkb.Evaluate(helpIndex(t), helpQuestionSet(t), helpkb.DefaultNoAnswer)
	text := ev.Text()
	t.Log("\n" + text)
	for _, cat := range []string{"exits", "basics", "risk", "edge_cases", "journal", "off_topic", "all"} {
		if !strings.Contains(text, cat+" ") {
			t.Errorf("no line for category %s", cat)
		}
	}
	for _, col := range []string{"hit@1", "hit@3", "dont_know precision"} {
		if !strings.Contains(text, col) {
			t.Errorf("no %s column", col)
		}
	}
	path := filepath.Join("testdata", "helpkb_baseline.json")
	type baseline struct {
		HitAt1            float64 `json:"hit_at_1"`
		HitAt3            float64 `json:"hit_at_3"`
		DontKnowPrecision float64 `json:"dont_know_precision"`
	}
	if updatingHelp() {
		b, _ := json.MarshalIndent(baseline{ev.All.HitAt1, ev.All.HitAt3, ev.All.DontKnowPrecision}, "", "  ")
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var base baseline
	if err := json.Unmarshal(raw, &base); err != nil {
		t.Fatal(err)
	}
	const eps = 1e-9
	if ev.All.HitAt3 < base.HitAt3-eps {
		t.Errorf("hit@3 %.3f under the baseline %.3f: retrieval regressed (missed: %v)", ev.All.HitAt3, base.HitAt3, ev.All.Misses)
	}
	if ev.All.DontKnowPrecision < base.DontKnowPrecision-eps {
		t.Errorf("dont_know precision %.3f under the baseline %.3f", ev.All.DontKnowPrecision, base.DontKnowPrecision)
	}
	off := ev.Categories["off_topic"]
	if off.NoAnswers != off.Questions {
		t.Errorf("off-topic: %d of %d questions got NoAnswer", off.NoAnswers, off.Questions)
	}
}

// Evaluate on a hand-made index: ranks, precision and misses.
func TestHelpKB_EvaluateCounts(t *testing.T) {
	ix := helpkb.NewIndex([]helpkb.Passage{
		{Source: "a#stop", Title: "Stop-loss", Text: "A stop-loss buys back a leg at market."},
		{Source: "a#roll", Title: "Time roll", Text: "A time roll buys back a leg before expiry."},
	})
	ev := helpkb.Evaluate(ix, []helpkb.Question{
		{Question: "stop-loss", Category: "x", Expect: []string{"a#stop"}},
		{Question: "time roll expiry", Category: "x", Expect: []string{"a#stop"}}, // only the roll matches
		{Question: "dragons", Category: "y", DontKnow: true},
		{Question: "unicorns", Category: "y", Expect: []string{"a#roll"}}, // NoAnswer wrongly fires
	}, helpkb.DefaultNoAnswer)
	all := ev.All
	if all.Questions != 4 || all.Answerable != 3 || all.NoAnswers != 2 {
		t.Fatalf("counts %+v", all)
	}
	if math.Abs(all.HitAt1-1.0/3) > 1e-9 || math.Abs(all.DontKnowPrecision-0.5) > 1e-9 {
		t.Errorf("hit@1 %v, precision %v", all.HitAt1, all.DontKnowPrecision)
	}
	if !reflect.DeepEqual(all.Misses, []string{"time roll expiry", "unicorns"}) {
		t.Errorf("misses %v", all.Misses)
	}
	if _, err := helpkb.ParseQuestions([]byte("questions:\n  - question: q\n    category: c\n")); err == nil {
		t.Error("a question with neither expect nor dont_know was accepted")
	}
}

func helpQuestionSet(t *testing.T) []helpkb.Question {
	t.Helper()
	raw, err := os.ReadFile(helpQuestions)
	if err != nil {
		t.Fatal(err)
	}
	qs, err := helpkb.ParseQuestions(raw)
	if err != nil {
		t.Fatal(err)
	}
	return qs
}

func sources(hits []helpkb.Hit) []string {
	var out []string
	for _, h := range hits {
		out = append(out, h.Source)
	}
	return out
}

func coverages(hits []helpkb.Hit) []float64 {
	var out []float64
	for _, h := range hits {
		out = append(out, h.Coverage)
	}
	return out
}
