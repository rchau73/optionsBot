package tests

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"optionsbot/internal/api"
	"optionsbot/internal/config"
	"optionsbot/internal/helpkb"
	"optionsbot/internal/orders"
)

// The assistant's golden question set (#313, #258): every later piece of the
// assistant (retrieval, prompt, model) is graded against it, offline
// (scripts/product/assistant_grade.py). These tests keep the set itself
// consistent with the help it cites.

const goldenFile = "../docs/product/assistant/golden.jsonl"

type goldenJournal struct {
	Kind       string `json:"kind"` // close | skip | risk
	Reason     string `json:"reason"`
	OptionType string `json:"option_type,omitempty"`
}

type goldenQuestion struct {
	ID                string         `json:"id"`
	Category          string         `json:"category"`
	Question          string         `json:"question"`
	ExpectedBehaviour string         `json:"expected_behaviour"`
	ExpectedSources   []string       `json:"expected_sources"`
	MustSay           []string       `json:"must_say"`
	MustNotSay        []string       `json:"must_not_say"`
	Setting           string         `json:"setting,omitempty"`
	Journal           *goldenJournal `json:"journal,omitempty"`
}

// goldenMinimum is each category's minimum count; the behaviour each must
// expect is goldenBehaviour.
var goldenMinimum = map[string]int{
	"how_it_works":    15,
	"parameter":       10,
	"why_did_it":      10,
	"jargon":          5,
	"risk":            5,
	"refuse_advice":   5,
	"refuse_forecast": 5,
	"unknown":         5,
}

var goldenBehaviour = map[string]string{
	"how_it_works":    "answer",
	"parameter":       "answer",
	"why_did_it":      "answer",
	"jargon":          "answer",
	"risk":            "answer",
	"refuse_advice":   "refuse",
	"refuse_forecast": "refuse",
	"unknown":         "dont_know",
}

// journalEvent names the catalogue event of a journal fixture's kind.
var journalEvent = map[string]string{
	"close": orders.EventClosed,
	"skip":  orders.EventSkipped,
	"risk":  orders.EventRisk,
}

func loadGolden(t *testing.T) []goldenQuestion {
	t.Helper()
	data, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatal(err)
	}
	var out []goldenQuestion
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(line))
		dec.DisallowUnknownFields()
		var q goldenQuestion
		if err := dec.Decode(&q); err != nil {
			t.Fatalf("%s line %d: %v", goldenFile, n, err)
		}
		out = append(out, q)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// unresolvedSources lists "<question id>: <source>" for every expected source
// that resolves nowhere: an OptionStrategy.md anchor that is no heading of
// strategy, or another source (glossary, plain-words catalogue) with no
// passage in the index.
func unresolvedSources(qs []goldenQuestion, strategy []byte, index helpkb.File) []string {
	anchors := map[string]bool{}
	for _, a := range helpkb.Anchors(strategy) {
		anchors[a] = true
	}
	inIndex := map[string]bool{}
	for _, p := range index.Chunks {
		inIndex[p.Source] = true
	}
	var out []string
	for _, q := range qs {
		for _, s := range q.ExpectedSources {
			ok := inIndex[s]
			if a, isStrategy := strings.CutPrefix(s, helpkb.StrategyFile+"#"); isStrategy {
				ok = anchors[a]
			}
			if !ok {
				out = append(out, q.ID+": "+s)
			}
		}
	}
	return out
}

// normText makes a phrase comparable with the help's markdown: no emphasis
// marks, one space, lower case.
func normText(s string) string {
	s = strings.NewReplacer("*", "", "`", "").Replace(s)
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// Scenario: the set is complete and consistent — every expected source
// resolves to a heading of OptionStrategy.md (or a passage of the index), ids
// are unique and each category meets its minimum count.
func TestAssistantQuestions_SetIsCompleteAndConsistent(t *testing.T) {
	qs := loadGolden(t)
	src := helpSources(t)
	if bad := unresolvedSources(qs, src.Strategy, buildHelpIndex(t, src)); len(bad) > 0 {
		t.Errorf("expected sources that resolve nowhere:\n%s", strings.Join(bad, "\n"))
	}

	seen := map[string]bool{}
	count := map[string]int{}
	for _, q := range qs {
		if q.ID == "" || seen[q.ID] {
			t.Errorf("id %q is empty or not unique", q.ID)
		}
		seen[q.ID] = true
		count[q.Category]++
		want, known := goldenBehaviour[q.Category]
		switch {
		case !known:
			t.Errorf("%s: unknown category %q", q.ID, q.Category)
		case q.ExpectedBehaviour != want:
			t.Errorf("%s: %s expects %q, has %q", q.ID, q.Category, want, q.ExpectedBehaviour)
		}
		if strings.TrimSpace(q.Question) == "" {
			t.Errorf("%s: no question", q.ID)
		}
		if answer := q.ExpectedBehaviour == "answer"; answer != (len(q.ExpectedSources) > 0) {
			t.Errorf("%s: an answer needs expected sources, a refusal or I-don't-know none (%d)", q.ID, len(q.ExpectedSources))
		}
		if q.ExpectedBehaviour == "answer" && len(q.MustSay) == 0 {
			t.Errorf("%s: an answer needs at least one must_say fact", q.ID)
		}
		if len(q.MustNotSay) == 0 {
			t.Errorf("%s: no must_not_say phrase", q.ID)
		}
		if (q.Category == "why_did_it") != (q.Journal != nil) {
			t.Errorf("%s: a journal fixture belongs to why_did_it questions only", q.ID)
		}
		if (q.Category == "parameter") != (q.Setting != "") {
			t.Errorf("%s: a setting belongs to parameter questions only", q.ID)
		}
	}
	for cat, min := range goldenMinimum {
		if count[cat] < min {
			t.Errorf("category %s has %d questions, needs at least %d", cat, count[cat], min)
		}
	}
	if n := count["refuse_advice"] + count["refuse_forecast"]; n < 10 {
		t.Errorf("%d refusals, needs at least 10", n)
	}
}

// Scenario: a heading is renamed — a question citing §10 fails, named by its
// id, once section 10's heading no longer gives the anchor.
func TestAssistantQuestions_RenamedHeadingNamesTheQuestion(t *testing.T) {
	qs := loadGolden(t)
	src := helpSources(t)
	index := buildHelpIndex(t, src)
	const heading = "## 10. The parameters, in plain words"
	if !bytes.Contains(src.Strategy, []byte(heading)) {
		t.Fatalf("OptionStrategy.md has no %q heading; update this test", heading)
	}
	renamed := bytes.Replace(src.Strategy, []byte(heading), []byte("## 10. Settings"), 1)
	bad := strings.Join(unresolvedSources(qs, renamed, index), "\n")
	for _, q := range qs {
		for _, s := range q.ExpectedSources {
			if s == "OptionStrategy.md#10-the-parameters-in-plain-words" && !strings.Contains(bad, q.ID+": "+s) {
				t.Errorf("renaming §10 did not name %s:\n%s", q.ID, bad)
			}
		}
	}
	if bad == "" {
		t.Fatal("renaming §10 resolved every source")
	}
}

// Scenario: every close reason can be asked about — each close reason
// PlainReason maps to plain words has a why_did_it question, else the test
// names the missing reason. Every fixture's reason is one the catalogue
// explains, and the question cites that sentence.
func TestAssistantQuestions_EveryCloseReasonCanBeAskedAbout(t *testing.T) {
	qs := loadGolden(t)
	asked := map[string]bool{}
	for _, q := range qs {
		if q.Journal == nil {
			continue
		}
		event, ok := journalEvent[q.Journal.Kind]
		if !ok {
			t.Errorf("%s: journal kind %q is not close, skip or risk", q.ID, q.Journal.Kind)
			continue
		}
		key := event + ":" + q.Journal.Reason
		asked[key] = true
		if !slices.Contains(q.ExpectedSources, "explain:"+key) {
			t.Errorf("%s: does not cite the plain-words sentence explain:%s", q.ID, key)
		}
		if q.Journal.Kind == "close" {
			trade := orders.Trade{Kind: "close", Reason: q.Journal.Reason, OptionType: q.Journal.OptionType}
			if _, unknown := api.ExplainTrade(trade); unknown != "" {
				t.Errorf("%s: PlainReason has no words for %s", q.ID, unknown)
			}
		}
	}
	var missing []string
	for _, e := range api.Catalogue() {
		if strings.HasPrefix(e.Key, orders.EventClosed+":") && !asked[e.Key] {
			missing = append(missing, strings.TrimPrefix(e.Key, orders.EventClosed+":"))
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("close reasons with no why_did_it question: %s", strings.Join(missing, ", "))
	}
}

// One parameter question per setting a config file may tune (CLAUDE.md "Few
// settings"; config.Tunables, the backtest-only and naming keys left out).
func TestAssistantQuestions_OneParameterQuestionPerSetting(t *testing.T) {
	asked := map[string]bool{}
	for _, q := range loadGolden(t) {
		asked[q.Setting] = true
	}
	for _, tn := range config.Tunables() {
		if tn.Policy == config.PolicyNone {
			continue
		}
		key, _, _ := strings.Cut(tn.Key, "[")
		if !asked[key] {
			t.Errorf("setting %s has no parameter question", key)
		}
	}
}

// must_say facts are the help's own words: each appears in a passage of one
// of the question's expected sources, so a correct answer can quote them.
func TestAssistantQuestions_MustSayComesFromSources(t *testing.T) {
	text := map[string]string{}
	for _, p := range buildHelpIndex(t, helpSources(t)).Chunks {
		text[p.Source] += "\n" + normText(p.Title+"\n"+p.Text)
	}
	for _, q := range loadGolden(t) {
		for _, phrase := range q.MustSay {
			found := false
			for _, s := range q.ExpectedSources {
				found = found || strings.Contains(text[s], normText(phrase))
			}
			if !found {
				t.Errorf("%s: must_say %q is in none of %v", q.ID, phrase, q.ExpectedSources)
			}
		}
	}
}
